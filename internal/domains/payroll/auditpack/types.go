package auditpack

import (
	"fmt"
	"sort"
	"strings"
	"time"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TotalKind is the closed vocabulary of the four totals a payroll run's
// reconciliation binds. There is no fifth.
type TotalKind string

const (
	KindRegister             TotalKind = "REGISTER"
	KindBankFile             TotalKind = "BANK_FILE"
	KindTaxLiability         TotalKind = "TAX_LIABILITY"
	KindFilingAcknowledgment TotalKind = "FILING_ACKNOWLEDGMENT"
)

// Kinds lists the four declared kinds, in a fixed order used everywhere this
// package needs one: folding a digest, rendering a report, walking a map
// deterministically.
func Kinds() []TotalKind {
	return []TotalKind{KindRegister, KindBankFile, KindTaxLiability, KindFilingAcknowledgment}
}

// Valid reports whether k is one of the four declared kinds.
func (k TotalKind) Valid() bool {
	switch k {
	case KindRegister, KindBankFile, KindTaxLiability, KindFilingAcknowledgment:
		return true
	default:
		return false
	}
}

// LineSchemaRef names the registered payload schema a contributing line's
// ledger event carries. It is the only schema [ResolveFromContent] reads
// amounts from; an event under any other schema is not a reconciliation
// input and is ignored, not misread.
const LineSchemaRef = "hcmnext.domains.payroll.auditpack.Line.v1"

// BindingSchemaRef names the registered payload schema [Bind] records its
// resolved reconciliation under.
const BindingSchemaRef = "hcmnext.domains.payroll.auditpack.Reconciliation.v1"

// SourceRef is the provenance every event this package appends records
// itself under.
const SourceRef = "hcmnext:domains:payroll:auditpack"

// StreamKey is the ledger stream a run's contributing lines and its
// reconciliation binding are recorded on. One stream per run is deliberate:
// it is what lets a single checkpoint/evidence export over the run's window
// cover every input the reconciliation depends on with one signature.
func StreamKey(runID string) string {
	return "payroll.auditpack." + runID
}

// LineFact is one contributing ledger fact behind a resolved total: the
// amount plus the exact ledger position it was read from, so an auditor can
// point at the one event a total is built from.
type LineFact struct {
	Kind     TotalKind
	RunID    string
	Amount   values.Decimal
	Currency string
	Ref      datalogger.EventRef
}

// Validate reports whether the fact is complete enough to contribute to a
// total.
func (l LineFact) Validate() error {
	if !l.Kind.Valid() {
		return fmt.Errorf("%w: %q is not one of REGISTER, BANK_FILE, TAX_LIABILITY, FILING_ACKNOWLEDGMENT", ErrInvalidLine, l.Kind)
	}
	if strings.TrimSpace(l.RunID) == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidLine)
	}
	if err := l.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrInvalidLine, err)
	}
	if strings.TrimSpace(l.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidLine)
	}
	return nil
}

// RunTotals is the resolved reconciliation input for one payroll run: the
// four totals, each folded from the [LineFact]s that contributed to it,
// entirely from ledger checkpoint/evidence data.
type RunTotals struct {
	Tenant     TenantID
	RunID      string
	CoversFrom time.Time
	CoversTo   time.Time
	// Totals holds exactly the four declared kinds once [ResolveFromContent]
	// returns without error.
	Totals map[TotalKind]values.Decimal
	// Lines are every contributing fact, ordered by kind then by stream
	// key/sequence, so two resolutions over the same content list identically.
	Lines []LineFact
}

// Total returns the resolved total for k, and false if it was never
// resolved.
func (t RunTotals) Total(k TotalKind) (values.Decimal, bool) {
	v, ok := t.Totals[k]
	return v, ok
}

// LinesOf returns every contributing fact for k, in stable order.
func (t RunTotals) LinesOf(k TotalKind) []LineFact {
	var out []LineFact
	for _, l := range t.Lines {
		if l.Kind == k {
			out = append(out, l)
		}
	}
	return out
}

// PairResult is one of the two exact relationships [Reconcile] checks: two
// named sides, each an amount or a sum of amounts, and whether they agree.
type PairResult struct {
	Name        string
	Left        TotalKind
	Right       []TotalKind
	LeftAmount  values.Decimal
	RightAmount values.Decimal
	Difference  values.Decimal
	OK          bool
}

// Decision is the outcome of [Reconcile]: every pair it checked, and whether
// every one of them agreed.
type Decision struct {
	Pairs []PairResult
}

// OK reports whether every checked pair agreed exactly.
func (d Decision) OK() bool {
	for _, p := range d.Pairs {
		if !p.OK {
			return false
		}
	}
	return len(d.Pairs) > 0
}

// Err joins one [ErrVarianceUnexplained] per failing pair, or returns nil
// when the decision is clean.
func (d Decision) Err() error {
	var errs []error
	for _, p := range d.Pairs {
		if !p.OK {
			errs = append(errs, ErrVarianceUnexplained{
				Pair: p.Name, Left: p.LeftAmount.String(), Right: p.RightAmount.String(),
				Difference: p.Difference.String(),
			})
		}
	}
	return joinErrors(errs)
}

// joinErrors is errors.Join without importing it twice across files; it
// exists here so package.go and reconcile.go share one definition.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	sort.Strings(msgs)
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}

// ---- errors ---------------------------------------------------------------

// ErrInvalidLine reports a contributing fact that cannot be resolved into a
// total.
var ErrInvalidLine = fmt.Errorf("auditpack: invalid contributing line")

// ErrMissingTotal reports a run with no contributing line at all for one of
// the four declared kinds. A reconciliation over three totals is not a
// reconciliation; the RED case this package exists to close is exactly a
// total nobody produced going unnoticed.
type ErrMissingTotal struct {
	RunID string
	Kind  TotalKind
}

func (ErrMissingTotal) Code() string { return "PAYROLL_AUDITPACK_MISSING_TOTAL" }

func (e ErrMissingTotal) Error() string {
	return fmt.Sprintf("%s: run %s has no contributing line for %s", e.Code(), e.RunID, e.Kind)
}

// ErrScaleMismatch reports two lines contributing to the same total under
// different declared decimal scales. Reconciling two different declared
// scales is a schema decision this package refuses to make silently.
type ErrScaleMismatch struct {
	Kind TotalKind
	Want int32
	Got  int32
}

func (ErrScaleMismatch) Code() string { return "PAYROLL_AUDITPACK_SCALE_MISMATCH" }

func (e ErrScaleMismatch) Error() string {
	return fmt.Sprintf("%s: %s lines declare scale %d and %d", e.Code(), e.Kind, e.Want, e.Got)
}

// ErrVarianceUnexplained is the typed refusal [Decision.Err] and [Bind]
// return when a reconciliation pair does not agree exactly: it names the
// pair and the exact amount by which the two sides differ.
type ErrVarianceUnexplained struct {
	Pair       string
	Left       string
	Right      string
	Difference string
}

func (ErrVarianceUnexplained) Code() string { return "PAYROLL_AUDITPACK_VARIANCE_UNEXPLAINED" }

func (e ErrVarianceUnexplained) Error() string {
	return fmt.Sprintf("%s: %s: %s vs %s differ by %s", e.Code(), e.Pair, e.Left, e.Right, e.Difference)
}

// ErrTenantLeak reports a resolved line naming a tenant other than the one
// the resolution was asked for. Under row level security this can only
// happen if a caller hands [ResolveFromContent] content it read for a
// different tenant than it claims.
type ErrTenantLeak struct {
	Expected TenantID
	Found    TenantID
}

func (ErrTenantLeak) Code() string { return "PAYROLL_AUDITPACK_TENANT_LEAK" }

func (e ErrTenantLeak) Error() string {
	return fmt.Sprintf("%s: content names tenant %s, but resolution was requested for %s",
		e.Code(), formatUUID(e.Found), formatUUID(e.Expected))
}
