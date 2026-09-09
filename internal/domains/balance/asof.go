package balance

// This file owns BAL-003: the authorized-balance calculation over one
// account's entry ledger, as-of a business instant and known-at a system
// instant (bitemporal), excluding any entry not yet authorized. It reads
// BalanceEntry values (see entries.go) and never persists, queues, or emits
// anything -- it is a pure function of the entries it is given.
//
// Bitemporal model:
//
//   - as-of (EffectiveAsOf): only entries whose EffectiveAt is on or before
//     EffectiveAsOf can count. An entry with no declared EffectiveAt can
//     never be placed on the business timeline, so it is excluded.
//   - known-at (KnownAt): only entries recorded on or before KnownAt (their
//     RecordedAt) AND authorized on or before KnownAt (their AuthorizedAt)
//     can count. Both gates use the same KnownAt instant because "known by
//     KnownAt" means both "the ledger had the entry" and "the ledger knew it
//     was authorized" -- an entry recorded but pending authorization is not
//     yet an authorized fact, however long ago it was recorded.
//
// Every one of these three gates only ever becomes *more* permissive as its
// governing instant (EffectiveAsOf for the first, KnownAt for the other two)
// moves later: none of them can turn a counted entry into an excluded one.
// That is the monotonicity TestTodo_BAL_003_Property proves: a later
// known-at never removes an authorized entry that an earlier known-at had
// already counted, and likewise for a later as-of.
import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrBalanceRequestInvalid identifies a request that cannot be calculated.
var ErrBalanceRequestInvalid = errors.New("balance: invalid authorized-balance request")

// AdjustmentEntryType is the entry type this package treats as a correction
// or adjustment to a balance -- rather than an ordinary ledger movement --
// when it explains a computed balance. It matches the "CORRECTION" entry
// type an AccumulatorDefinition's EntryTypes may declare.
const AdjustmentEntryType = "CORRECTION"

// ExclusionReason is the closed vocabulary of reasons an entry did not count
// toward an authorized balance.
type ExclusionReason string

const (
	// ExcludedNotRecorded means the entry's RecordedAt is unset, or is after
	// KnownAt: the ledger did not yet have this entry as of known-at.
	ExcludedNotRecorded ExclusionReason = "NOT_RECORDED_BY_KNOWN_AT"
	// ExcludedNoEffectiveTime means the entry never declared an EffectiveAt,
	// so it can never be placed on the as-of business timeline.
	ExcludedNoEffectiveTime ExclusionReason = "EFFECTIVE_AT_UNSET"
	// ExcludedFutureEffective means the entry's EffectiveAt is strictly
	// after the requested EffectiveAsOf.
	ExcludedFutureEffective ExclusionReason = "EFFECTIVE_AFTER_AS_OF"
	// ExcludedNotAuthorized means the entry's AuthorizedAt is unset, or is
	// after KnownAt: the entry is not yet an authorized fact as of known-at.
	ExcludedNotAuthorized ExclusionReason = "NOT_AUTHORIZED_BY_KNOWN_AT"
)

// ExcludedEntry names one ledger entry left out of an authorized balance,
// and exactly why.
type ExcludedEntry struct {
	Entry  BalanceEntry
	Reason ExclusionReason
}

// AuthorizedBalanceRequest names the account, the bitemporal instants, the
// opening balance and the balance kind's declared scale and rounding rule
// under which CalculateAuthorizedBalance runs.
type AuthorizedBalanceRequest struct {
	AccountID     string
	EffectiveAsOf values.Instant
	KnownAt       values.Instant
	// Opening is the balance carried into this calculation, already declared
	// at Scale and Rounding. It is the accumulator's own opening fact (from
	// an earlier period close, or zero at the account's inception) -- this
	// calculation never derives it.
	Opening  values.Decimal
	Scale    int32
	Rounding values.RoundingMode
}

func (req AuthorizedBalanceRequest) validate() error {
	if strings.TrimSpace(req.AccountID) == "" {
		return fmt.Errorf("%w: account_id is required", ErrBalanceRequestInvalid)
	}
	if !req.EffectiveAsOf.IsSet() {
		return fmt.Errorf("%w: effective_as_of is required", ErrBalanceRequestInvalid)
	}
	if !req.KnownAt.IsSet() {
		return fmt.Errorf("%w: known_at is required", ErrBalanceRequestInvalid)
	}
	if err := req.Opening.Validate(); err != nil {
		return fmt.Errorf("%w: opening: %v", ErrBalanceRequestInvalid, err)
	}
	if req.Opening.Scale() != req.Scale {
		return fmt.Errorf("%w: opening scale %d does not match declared scale %d", ErrBalanceRequestInvalid, req.Opening.Scale(), req.Scale)
	}
	if req.Rounding == values.RoundingUnspecified || !req.Rounding.Valid() {
		return fmt.Errorf("%w: rounding must be a declared mode", ErrBalanceRequestInvalid)
	}
	return nil
}

// AuthorizedBalance is the explainable result of one bitemporal, authorized
// balance calculation: the opening balance, the counted entries (split into
// ordinary Entries and CORRECTION Adjustments, each in ledger order), every
// Excluded entry with its reason, the calculated Ending balance, and a
// Digest over the whole derivation.
type AuthorizedBalance struct {
	AccountID     string
	EffectiveAsOf values.Instant
	KnownAt       values.Instant
	Opening       values.Decimal
	Entries       []BalanceEntry
	Adjustments   []BalanceEntry
	Excluded      []ExcludedEntry
	Ending        values.Decimal
	Digest        string
}

// exclusionReason reports why e cannot count toward req, or ("", false) when
// e counts.
func exclusionReason(e BalanceEntry, req AuthorizedBalanceRequest) (ExclusionReason, bool) {
	if !e.RecordedAt.IsSet() || e.RecordedAt.After(req.KnownAt) {
		return ExcludedNotRecorded, true
	}
	if !e.EffectiveAt.IsSet() {
		return ExcludedNoEffectiveTime, true
	}
	if e.EffectiveAt.After(req.EffectiveAsOf) {
		return ExcludedFutureEffective, true
	}
	if !e.AuthorizedAt.IsSet() || e.AuthorizedAt.After(req.KnownAt) {
		return ExcludedNotAuthorized, true
	}
	return "", false
}

// contribution returns e's signed, scale-normalized contribution to a
// running total at the balance kind's declared scale and rounding rule.
// Credit entries add; debit entries subtract (Amount itself is always
// unsigned; see BalanceEntry).
func contribution(e BalanceEntry, scale int32, mode values.RoundingMode) (values.Decimal, error) {
	amt := e.Amount
	if err := amt.Validate(); err != nil {
		return values.Decimal{}, fmt.Errorf("amount: %w", err)
	}
	if amt.Scale() != scale {
		var err error
		amt, err = amt.Quantize(scale, mode)
		if err != nil {
			return values.Decimal{}, fmt.Errorf("quantize to declared scale %d: %w", scale, err)
		}
	}
	switch e.Kind {
	case Credit:
		return amt, nil
	case Debit:
		return amt.Neg()
	default:
		return values.Decimal{}, fmt.Errorf("entry kind %q is neither DEBIT nor CREDIT", e.Kind)
	}
}

// CalculateAuthorizedBalance computes the authorized balance for one account
// as-of req.EffectiveAsOf, using only what was known -- recorded and
// authorized -- by req.KnownAt. entries must be in ledger append order (the
// order EntryStore.Entries returns); that order is preserved in the result's
// Entries/Adjustments/Excluded lists, but the calculated total never depends
// on it (see TestTodo_BAL_003_Property).
func CalculateAuthorizedBalance(req AuthorizedBalanceRequest, entries []BalanceEntry) (AuthorizedBalance, error) {
	if err := req.validate(); err != nil {
		return AuthorizedBalance{}, err
	}
	running := req.Opening
	result := AuthorizedBalance{
		AccountID:     req.AccountID,
		EffectiveAsOf: req.EffectiveAsOf,
		KnownAt:       req.KnownAt,
		Opening:       req.Opening,
	}
	for _, e := range entries {
		if e.AccountID != req.AccountID {
			continue // defensive: this calculation is scoped to one account.
		}
		if reason, excluded := exclusionReason(e, req); excluded {
			result.Excluded = append(result.Excluded, ExcludedEntry{Entry: e.copy(), Reason: reason})
			continue
		}
		delta, err := contribution(e, req.Scale, req.Rounding)
		if err != nil {
			return AuthorizedBalance{}, fmt.Errorf("%w: entry %s/%s: %v", ErrBalanceRequestInvalid, e.AccountID, e.IdempotencyKey, err)
		}
		next, err := running.Add(delta)
		if err != nil {
			return AuthorizedBalance{}, fmt.Errorf("%w: entry %s/%s: %v", ErrBalanceRequestInvalid, e.AccountID, e.IdempotencyKey, err)
		}
		running = next
		if strings.EqualFold(e.EntryType, AdjustmentEntryType) {
			result.Adjustments = append(result.Adjustments, e.copy())
		} else {
			result.Entries = append(result.Entries, e.copy())
		}
	}
	ending, err := running.Quantize(req.Scale, req.Rounding)
	if err != nil {
		return AuthorizedBalance{}, fmt.Errorf("%w: ending: %v", ErrBalanceRequestInvalid, err)
	}
	result.Ending = ending
	digest, err := result.canonicalDigest()
	if err != nil {
		return AuthorizedBalance{}, fmt.Errorf("%w: digest: %v", ErrBalanceRequestInvalid, err)
	}
	result.Digest = digest
	return result, nil
}

// canonicalDigest returns the deterministic digest of r's full derivation:
// the request identity, every counted entry's own digest (in the order
// counted), every excluded entry's digest and reason, and the ending
// balance. Two calculations over the same logical inputs digest identically
// regardless of slice capacity or intermediate allocation.
func (r AuthorizedBalance) canonicalDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.balance.AuthorizedBalance", 1).
		String("account_id", r.AccountID).
		Value("effective_as_of", r.EffectiveAsOf).
		Value("known_at", r.KnownAt).
		Value("opening", r.Opening).
		Count("entries", len(r.Entries))
	for _, e := range r.Entries {
		w = w.String("entries.digest", e.Digest())
	}
	w = w.Count("adjustments", len(r.Adjustments))
	for _, e := range r.Adjustments {
		w = w.String("adjustments.digest", e.Digest())
	}
	w = w.Count("excluded", len(r.Excluded))
	for _, x := range r.Excluded {
		w = w.String("excluded.digest", x.Entry.Digest()).String("excluded.reason", string(x.Reason))
	}
	w = w.Value("ending", r.Ending)
	return w.Digest()
}
