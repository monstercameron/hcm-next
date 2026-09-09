// Package paygl owns payroll-to-general-ledger mapping and pure posting
// derivation. It has no persistence or provider authority.
package paygl

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

// ComponentKind is the closed payroll accounting component vocabulary.
type ComponentKind string

const (
	ComponentEarning              ComponentKind = "EARNING"
	ComponentDeduction            ComponentKind = "DEDUCTION"
	ComponentTax                  ComponentKind = "TAX"
	ComponentEmployerContribution ComponentKind = "EMPLOYER_CONTRIBUTION"

	Earning              = ComponentEarning
	Deduction            = ComponentDeduction
	Tax                  = ComponentTax
	EmployerContribution = ComponentEmployerContribution
)

func (k ComponentKind) Valid() bool {
	switch k {
	case ComponentEarning, ComponentDeduction, ComponentTax, ComponentEmployerContribution:
		return true
	default:
		return false
	}
}

// SuspensePolicy says whether an unmappable component can be routed to a
// governed suspense account. The default is deliberately invalid.
type SuspensePolicy string

const (
	SuspensePolicyReject   SuspensePolicy = "REJECT"
	SuspensePolicyAllow    SuspensePolicy = "ALLOW_SUSPENSE"
	SuspensePolicyRequired SuspensePolicy = "REQUIRE_SUSPENSE"
)

func (p SuspensePolicy) Valid() bool {
	return p == SuspensePolicyReject || p == SuspensePolicyAllow || p == SuspensePolicyRequired
}

var (
	ErrInvalidAccountingRule  = errors.New("paygl: invalid accounting rule")
	ErrAccountingRuleConflict = errors.New("paygl: accounting rule successor must be a new version")
	ErrPostingRejected        = errors.New("PAYGL_001_REJECTED")
	ErrNoAccountingRule       = errors.New("paygl: no unique accounting rule matched line")
	ErrUnbalancedPostings     = errors.New("paygl: postings are not balanced")
)

// AccountingRule maps one payroll component and labor dimension to the two
// accounts needed for a balanced journal. Effective and rounding policy are
// part of the digested version.
type AccountingRule struct {
	ID        string
	Version   string
	Effective values.EffectiveInterval

	ComponentKind ComponentKind
	ComponentCode string
	Code          string // descriptive alias for ComponentCode

	// Dimension is the compact one-dimension form. Dimensions permits a
	// future composite mapping while preserving deterministic validation.
	Dimension       labor.Dimension
	Dimensions      []labor.Dimension
	LaborDimensions []labor.Dimension

	DebitAccount     string
	CreditAccount    string
	DebitAccountRef  string
	CreditAccountRef string
	Currency         string
	Rounding         values.RoundingMode
	SuspensePolicy   SuspensePolicy
	Supersedes       string
	CanonicalDigest  string
}

// PayrollAccountingRule is a descriptive alias.
type PayrollAccountingRule = AccountingRule

func (r AccountingRule) componentCode() string {
	if r.ComponentCode != "" {
		return r.ComponentCode
	}
	return r.Code
}

func account(primary, alias string) (string, error) {
	if primary != "" && alias != "" && primary != alias {
		return "", fmt.Errorf("%w: account aliases disagree", ErrInvalidAccountingRule)
	}
	if primary != "" {
		return primary, nil
	}
	return alias, nil
}

func (r AccountingRule) dimensions() []labor.Dimension {
	if len(r.Dimensions) > 0 {
		return append([]labor.Dimension(nil), r.Dimensions...)
	}
	if len(r.LaborDimensions) > 0 {
		return append([]labor.Dimension(nil), r.LaborDimensions...)
	}
	if r.Dimension.Kind != "" || r.Dimension.Value != "" || r.Dimension.Version != "" {
		return []labor.Dimension{r.Dimension}
	}
	return nil
}

func (r AccountingRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidAccountingRule)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidAccountingRule, err)
	}
	if !r.ComponentKind.Valid() || strings.TrimSpace(r.componentCode()) == "" {
		return fmt.Errorf("%w: component kind and code are required", ErrInvalidAccountingRule)
	}
	debit, err := account(r.DebitAccount, r.DebitAccountRef)
	if err != nil || strings.TrimSpace(debit) == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: debit account is required", ErrInvalidAccountingRule)
	}
	credit, err := account(r.CreditAccount, r.CreditAccountRef)
	if err != nil || strings.TrimSpace(credit) == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: credit account is required", ErrInvalidAccountingRule)
	}
	dimensions := r.dimensions()
	if len(dimensions) == 0 {
		return fmt.Errorf("%w: at least one labor dimension is required", ErrInvalidAccountingRule)
	}
	seen := make(map[string]struct{}, len(dimensions))
	for _, dimension := range dimensions {
		if err := dimension.Validate(); err != nil {
			return fmt.Errorf("%w: dimension: %v", ErrInvalidAccountingRule, err)
		}
		key := string(dimension.Kind) + "\x00" + dimension.Value + "\x00" + dimension.Version
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate labor dimension", ErrInvalidAccountingRule)
		}
		seen[key] = struct{}{}
	}
	if strings.TrimSpace(r.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidAccountingRule)
	}
	if !r.Rounding.Valid() {
		return fmt.Errorf("%w: rounding policy is required", ErrInvalidAccountingRule)
	}
	if !r.SuspensePolicy.Valid() {
		return fmt.Errorf("%w: suspense policy is required", ErrInvalidAccountingRule)
	}
	if r.Supersedes == r.Version && r.Supersedes != "" {
		return fmt.Errorf("%w: a rule cannot supersede itself", ErrInvalidAccountingRule)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidAccountingRule)
	}
	return nil
}

func (r AccountingRule) body() []byte {
	debit, _ := account(r.DebitAccount, r.DebitAccountRef)
	credit, _ := account(r.CreditAccount, r.CreditAccountRef)
	dimensions := r.dimensions()
	sort.Slice(dimensions, func(i, j int) bool {
		return string(dimensions[i].Kind)+"\x00"+dimensions[i].Value+"\x00"+dimensions[i].Version < string(dimensions[j].Kind)+"\x00"+dimensions[j].Value+"\x00"+dimensions[j].Version
	})
	w := canonicalbytes.New("hcmnext.domains.paygl.AccountingRule", schemaVersion).
		String("id", r.ID).String("version", r.Version).Value("effective", r.Effective).
		String("component_kind", string(r.ComponentKind)).String("component_code", r.componentCode()).
		String("debit_account", debit).String("credit_account", credit).String("currency", r.Currency).
		String("rounding", r.Rounding.String()).String("suspense_policy", string(r.SuspensePolicy)).
		String("supersedes", r.Supersedes).Count("dimensions", len(dimensions))
	for _, dimension := range dimensions {
		w.Value("dimension", dimension)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r AccountingRule) computedDigest() string { return canonicalbytes.Digest(r.body()) }

// NewAccountingRule validates and fills the immutable rule digest.
func NewAccountingRule(rule AccountingRule) (AccountingRule, error) {
	rule.CanonicalDigest = ""
	if err := rule.Validate(); err != nil {
		return AccountingRule{}, err
	}
	rule.CanonicalDigest = rule.computedDigest()
	return rule, nil
}

// Canonical returns the validated canonical rule bytes.
func (r AccountingRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of a validated accounting rule.
func (r AccountingRule) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// NewVersion makes a new effective-dated revision without changing r.
func (r AccountingRule) NewVersion(version string, effective values.EffectiveInterval) (AccountingRule, error) {
	next := r
	next.Version, next.Effective, next.Supersedes, next.CanonicalDigest = version, effective, r.Version, ""
	return NewAccountingRule(next)
}

func (r AccountingRule) Successor(next AccountingRule) (AccountingRule, error) {
	if err := r.Validate(); err != nil {
		return AccountingRule{}, err
	}
	if next.ID != r.ID || next.Version == r.Version || next.Supersedes != r.Version {
		return AccountingRule{}, fmt.Errorf("%w: successor must retain id, use a new version and supersede %s", ErrAccountingRuleConflict, r.Version)
	}
	return NewAccountingRule(next)
}

// CalculatedLine is the minimal exact-decimal line required to derive GL
// postings from a CALCULATED payroll run.
type CalculatedLine struct {
	ID             string
	ComponentKind  ComponentKind
	ComponentCode  string
	Amount         values.Decimal
	Currency       string
	Dimension      labor.Dimension
	LaborDimension labor.Dimension
	RuleID         string
	RuleVersion    string
	EffectiveDate  values.LocalDate
}

// PayrollLine is a descriptive alias.
type PayrollLine = CalculatedLine

func (l CalculatedLine) dimension() labor.Dimension {
	if l.Dimension.Kind != "" || l.Dimension.Value != "" || l.Dimension.Version != "" {
		return l.Dimension
	}
	return l.LaborDimension
}

func (l CalculatedLine) Validate() error {
	if strings.TrimSpace(l.ID) == "" || !l.ComponentKind.Valid() || strings.TrimSpace(l.ComponentCode) == "" {
		return fmt.Errorf("%w: line id, component kind and code are required", ErrPostingRejected)
	}
	if err := l.Amount.Validate(); err != nil || l.Amount.Sign() < 0 {
		if err != nil {
			return fmt.Errorf("%w: line amount: %v", ErrPostingRejected, err)
		}
		return fmt.Errorf("%w: line amount cannot be negative", ErrPostingRejected)
	}
	if strings.TrimSpace(l.Currency) == "" {
		return fmt.Errorf("%w: line currency is required", ErrPostingRejected)
	}
	if err := l.dimension().Validate(); err != nil {
		return fmt.Errorf("%w: line dimension: %v", ErrPostingRejected, err)
	}
	if (l.RuleID == "") != (l.RuleVersion == "") {
		return fmt.Errorf("%w: line rule id and version must be paired", ErrPostingRejected)
	}
	if l.EffectiveDate.IsSet() {
		if err := l.EffectiveDate.Validate(); err != nil {
			return fmt.Errorf("%w: effective date: %v", ErrPostingRejected, err)
		}
	}
	return nil
}

// PostingDirection is the closed debit/credit vocabulary.
type PostingDirection string

const (
	Debit  PostingDirection = "DEBIT"
	Credit PostingDirection = "CREDIT"
)

type Posting struct {
	LineID      string
	Account     string
	Direction   PostingDirection
	Amount      values.Decimal
	Currency    string
	Dimension   labor.Dimension
	RuleID      string
	RuleVersion string
}

type PostingDerivation struct {
	RunID        string
	RunRevision  uint64
	Postings     []Posting
	TotalDebits  values.Decimal
	TotalCredits values.Decimal
	Balanced     bool
	RuleDigests  []string
	Digest       string
}

func (d PostingDerivation) Validate() error {
	if strings.TrimSpace(d.RunID) == "" || d.RunRevision == 0 || len(d.Postings) == 0 {
		return fmt.Errorf("%w: run and postings are required", ErrPostingRejected)
	}
	if err := d.TotalDebits.Validate(); err != nil {
		return fmt.Errorf("%w: debit total: %v", ErrPostingRejected, err)
	}
	if err := d.TotalCredits.Validate(); err != nil {
		return fmt.Errorf("%w: credit total: %v", ErrPostingRejected, err)
	}
	if !d.Balanced || !d.TotalDebits.Equal(d.TotalCredits) {
		return ErrUnbalancedPostings
	}
	return nil
}

// DerivePostings turns calculated lines into a balanced, immutable posting
// set. A run must be CALCULATED; no persistence or publication is performed.
func DerivePostings(run payroll.PayrollRun, rules []AccountingRule, lines []CalculatedLine) (PostingDerivation, error) {
	if err := run.Validate(); err != nil {
		return PostingDerivation{}, err
	}
	if run.State != payroll.PayrollRunStateCalculated {
		return PostingDerivation{}, fmt.Errorf("%w: run state must be CALCULATED", ErrPostingRejected)
	}
	if len(lines) == 0 {
		return PostingDerivation{}, fmt.Errorf("%w: calculated lines are required", ErrPostingRejected)
	}
	validatedRules := make([]AccountingRule, len(rules))
	for i, rule := range rules {
		if err := rule.Validate(); err != nil {
			return PostingDerivation{}, fmt.Errorf("%w: rule %d: %v", ErrPostingRejected, i, err)
		}
		validatedRules[i] = rule
	}
	result := PostingDerivation{RunID: run.RunID, RunRevision: run.Revision}
	var debitTotal, creditTotal values.Decimal
	for i, line := range lines {
		if err := line.Validate(); err != nil {
			return PostingDerivation{}, fmt.Errorf("%w: line %d: %v", ErrPostingRejected, i, err)
		}
		var match *AccountingRule
		for j := range validatedRules {
			rule := &validatedRules[j]
			if rule.ComponentKind != line.ComponentKind || rule.componentCode() != line.ComponentCode || rule.Currency != line.Currency || !sameDimension(rule.dimensions(), line.dimension()) {
				continue
			}
			if line.RuleID != "" && (rule.ID != line.RuleID || rule.Version != line.RuleVersion) {
				continue
			}
			if line.EffectiveDate.IsSet() {
				inEffect, err := rule.Effective.ContainsDate(line.EffectiveDate)
				if err != nil {
					return PostingDerivation{}, fmt.Errorf("%w: effective date: %v", ErrPostingRejected, err)
				}
				if !inEffect {
					continue
				}
			}
			if match != nil {
				return PostingDerivation{}, fmt.Errorf("%w: %w: line %q has ambiguous rule versions", ErrPostingRejected, ErrNoAccountingRule, line.ID)
			}
			match = rule
		}
		if match == nil {
			return PostingDerivation{}, fmt.Errorf("%w: %w: line %q", ErrPostingRejected, ErrNoAccountingRule, line.ID)
		}
		debit, _ := account(match.DebitAccount, match.DebitAccountRef)
		credit, _ := account(match.CreditAccount, match.CreditAccountRef)
		result.Postings = append(result.Postings,
			Posting{LineID: line.ID, Account: debit, Direction: Debit, Amount: line.Amount, Currency: line.Currency, Dimension: line.dimension(), RuleID: match.ID, RuleVersion: match.Version},
			Posting{LineID: line.ID, Account: credit, Direction: Credit, Amount: line.Amount, Currency: line.Currency, Dimension: line.dimension(), RuleID: match.ID, RuleVersion: match.Version})
		var err error
		if i == 0 {
			debitTotal, creditTotal = line.Amount, line.Amount
		} else {
			debitTotal, err = debitTotal.Add(line.Amount)
			if err != nil {
				return PostingDerivation{}, err
			}
			creditTotal, err = creditTotal.Add(line.Amount)
			if err != nil {
				return PostingDerivation{}, err
			}
		}
		result.RuleDigests = append(result.RuleDigests, match.CanonicalDigest)
	}
	result.TotalDebits, result.TotalCredits, result.Balanced = debitTotal, creditTotal, debitTotal.Equal(creditTotal)
	if !result.Balanced {
		return PostingDerivation{}, ErrUnbalancedPostings
	}
	result.RuleDigests = uniqueSorted(result.RuleDigests)
	result.Digest = result.computedDigest()
	return result, nil
}

func sameDimension(ruleDimensions []labor.Dimension, line labor.Dimension) bool {
	if len(ruleDimensions) != 1 {
		return false
	}
	return ruleDimensions[0] == line
}

func uniqueSorted(in []string) []string {
	sorted := append([]string(nil), in...)
	sort.Strings(sorted)
	out := sorted[:0]
	for _, v := range sorted {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

func (d PostingDerivation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paygl.PostingDerivation", schemaVersion).
		String("run_id", d.RunID).Int("run_revision", int64(d.RunRevision)).
		Value("total_debits", d.TotalDebits).Value("total_credits", d.TotalCredits).Bool("balanced", d.Balanced).
		SortedStrings("rule_digest", d.RuleDigests).Count("postings", len(d.Postings))
	for _, p := range d.Postings {
		w.String("line_id", p.LineID).String("account", p.Account).String("direction", string(p.Direction)).
			Value("amount", p.Amount).String("currency", p.Currency).Value("dimension", p.Dimension).
			String("rule_id", p.RuleID).String("rule_version", p.RuleVersion)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (d PostingDerivation) computedDigest() string { return canonicalbytes.Digest(d.body()) }

// Explain returns an audit-safe deterministic description of the derivation.
func (d PostingDerivation) Explain() string {
	return fmt.Sprintf("payroll GL run %s@%d: %d postings, debits %s, credits %s, balanced %t, digest %s", d.RunID, d.RunRevision, len(d.Postings), d.TotalDebits.String(), d.TotalCredits.String(), d.Balanced, d.Digest)
}

// Explain is the package-level spelling for the paygl contract.
func Explain(d PostingDerivation) string { return d.Explain() }
