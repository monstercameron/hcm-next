package promotion

// PROMOUX-006: "Show the authorized compensation baseline and exact entry
// guardrail before submit."
//
// RED: the promotion form shows current base as "—" while enforcing a
// hidden percentage rule, or expresses only "0.0500..0.1800" after
// rejection -- a raw float range standing in for money and percentages that
// a reviewer had to reverse-engineer instead of being told outright.
//
// GREEN: after target-role selection, authorized purpose-bound data
// displays current exact Money, permitted increase percent, exact minimum
// and maximum annual Money, band position, currency and effective-date
// basis; the input constrains and explains the range without
// floating-point arithmetic; unauthorized viewers receive a typed
// unavailable action rather than inferred pay.
//
// REFACTOR: formatting uses shared Money and Percentage value types and
// locale services; the client never recomputes the authoritative
// guardrail -- every field here is server-computed exact decimal, and
// internal/humanwork/productui only localizes and renders what this file
// hands it.
//
// This file follows the shape target_manager.go (PROMOUX-005) established:
// a server-resolved verdict with an explicit typed state for "cannot show
// this", which the presentation layer consumes rather than recomputing.
// Both the unauthorized case and the case where the target band could not
// be resolved fail closed to the identical GuardrailStatusUnavailable
// state, because this program's proof standard is that an unresolved or
// unauthorized compensation baseline must never degrade to a blank a client
// could read as "no constraint" -- see [EvaluateCompensationGuardrail].
//
// This is deliberately a pre-submit read, not a re-implementation of
// PROMO-003's simcomp. simcomp evaluates the compensation and budget effects
// a promotion would make once a proposed amount already exists; this file
// answers "what would be allowed here" before that amount is ever entered,
// so the range it publishes is the exact number a "0.0500..0.1800" literal
// range used to stand in for -- derived from the resolved target band and
// this worker's own disclosed current pay, never a policy constant.
import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrGuardrailRequestInvalid is returned for a malformed guardrail request --
// a contract failure the caller must fix. A business reason the guardrail
// cannot be shown is never this error; it is the typed
// GuardrailStatusUnavailable state on the result.
var ErrGuardrailRequestInvalid = errors.New("promotion: compensation guardrail request is invalid")

// permittedIncreasePercentScale is the declared fractional-digit count the
// permitted-increase fraction is computed and rounded at. It is declared
// once here so the guardrail and anything that later replays it can never
// disagree about how many digits of precision the answer carries.
const permittedIncreasePercentScale int32 = 6

// GuardrailStatus is the closed, exhaustive verdict one compensation
// guardrail evaluation reaches. There is no permissive default: every
// branch of [EvaluateCompensationGuardrail] returns one of these explicitly.
type GuardrailStatus uint8

const (
	// GuardrailStatusUnspecified is the zero value and is never legal on a
	// result [EvaluateCompensationGuardrail] returns.
	GuardrailStatusUnspecified GuardrailStatus = iota
	// GuardrailStatusAvailable means every data field on the result was
	// populated from a governed, already-authorized read and a resolved pay
	// band.
	GuardrailStatusAvailable
	// GuardrailStatusUnavailable means the guardrail cannot be shown to this
	// caller for this subject: either the compensation baseline was not
	// authorized for disclosure, or the target band could not be resolved.
	// Both grounds collapse to this identical typed status -- see Reason for
	// which one -- so neither ever degrades to a blank a client could read
	// as "no constraint".
	GuardrailStatusUnavailable
)

var guardrailStatusWire = map[GuardrailStatus]string{
	GuardrailStatusAvailable:   "AVAILABLE",
	GuardrailStatusUnavailable: "UNAVAILABLE",
}

// String returns the stable wire token, or "GUARDRAIL_STATUS_UNSPECIFIED".
func (s GuardrailStatus) String() string {
	if wire, ok := guardrailStatusWire[s]; ok {
		return wire
	}
	return "GUARDRAIL_STATUS_UNSPECIFIED"
}

// Valid reports whether s is a legal status.
func (s GuardrailStatus) Valid() bool { _, ok := guardrailStatusWire[s]; return ok }

// GuardrailUnavailableReason names why GuardrailStatusUnavailable was
// reached. It is closed and exhaustive; [EvaluateCompensationGuardrail]
// never returns GuardrailStatusUnavailable without one of these.
type GuardrailUnavailableReason string

const (
	// GuardrailReasonNotAuthorized means the caller's own governed read of
	// the subject's current compensation did not disclose a value: denied,
	// redacted, unknown, unavailable, not applicable or simply never
	// supplied all collapse to this one reason.
	GuardrailReasonNotAuthorized GuardrailUnavailableReason = "NOT_AUTHORIZED"
	// GuardrailReasonBandUnresolved means the target role's pay band could
	// not be resolved from the catalog -- a structural catalog gap, not an
	// authorization refusal.
	GuardrailReasonBandUnresolved GuardrailUnavailableReason = "BAND_UNRESOLVED"
)

// CompensationGuardrailRequest is the input to one guardrail evaluation.
type CompensationGuardrailRequest struct {
	// Current is the caller's own governed read of the subject's current
	// compensation. Base's Presence state is the single authorization
	// signal this file trusts -- only a VALUE presence proves disclosure --
	// exactly the boundary [rewards.CompensationSnapshot] already draws for
	// PROMO-003's simulation. This file makes no authorization decision of
	// its own; it only classifies the one that already happened.
	Current rewards.CompensationSnapshot
	// Target is the band question the selected target role implies: job,
	// grade, pay zone, currency and the business date the band must be
	// effective on.
	Target rewards.BandQuery
	// Catalog answers Target. A nil catalog is a request-shape error, not a
	// finding: a caller that reached this file already selected a target
	// role and must have a catalog to ask.
	Catalog rewards.PayBandCatalog
	// Annualization is the declared rule every amount here is expressed
	// under -- the same [rewards.AnnualizationRule] promotion.PreflightRequest
	// carries -- so this guardrail and the preflight it precedes can never
	// disagree about what "annualized" means for this worker's pay basis.
	Annualization rewards.AnnualizationRule
}

// Validate reports whether the request is well formed. This is a contract
// check only; whether the guardrail can actually be shown is decided by
// [EvaluateCompensationGuardrail], never here.
func (r CompensationGuardrailRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return fmt.Errorf("%w: target band query: %w", ErrGuardrailRequestInvalid, err)
	}
	if r.Catalog == nil {
		return fmt.Errorf("%w: no pay band catalog", ErrGuardrailRequestInvalid)
	}
	if err := r.Annualization.Validate(); err != nil {
		return fmt.Errorf("%w: annualization rule: %w", ErrGuardrailRequestInvalid, err)
	}
	if !r.Current.PayBasis.Valid() {
		return fmt.Errorf("%w: current pay basis is required", ErrGuardrailRequestInvalid)
	}
	return nil
}

// CompensationGuardrail is GREEN's server-computed guardrail: everything a
// promotion form needs to constrain and explain a proposed amount, with no
// field the client could instead derive by recomputing a percentage from a
// raw amount.
//
// Every Money and Percentage here is the kernel's exact fixed-point type;
// there is no float64 anywhere on this path. See
// [EvaluateCompensationGuardrail] and permittedIncreasePercent.
type CompensationGuardrail struct {
	// Status is never GuardrailStatusUnspecified on a value this package
	// returns.
	Status GuardrailStatus
	// Reason is set only when Status is GuardrailStatusUnavailable.
	Reason GuardrailUnavailableReason

	// The fields below are populated only when Status is
	// GuardrailStatusAvailable; see [CompensationGuardrail.Available]. On an
	// Unavailable result every one of them is the zero value, and
	// [CompensationGuardrail.Canonical] never encodes them, which is what
	// makes an unauthorized and a band-unresolved refusal indistinguishable
	// from each other in every respect but Reason -- no partial guardrail
	// is ever assembled.
	CurrentAnnualized values.Money
	MinimumAnnualized values.Money
	MaximumAnnualized values.Money
	// PermittedIncreasePercent is (MaximumAnnualized -
	// CurrentAnnualized) / CurrentAnnualized, computed entirely in exact
	// decimal. It is the one number a hardcoded "0.0500..0.1800" range used
	// to stand in for: the true permitted headroom to the top of the band,
	// derived from this specific worker's current pay, never a policy
	// constant.
	PermittedIncreasePercent values.Percentage
	// BandPosition is where CurrentAnnualized sits in the target band -- the
	// band position GREEN requires alongside the range, so a reviewer sees
	// not just the range but where the starting point falls in it.
	BandPosition payband.Placement
	// Currency is the ISO-4217 code every Money field above is denominated
	// in.
	Currency string
	// EffectiveDateBasis is the business date the band was resolved as of --
	// the "effective-date basis" GREEN requires alongside the range, so a
	// reviewer knows which catalog vintage the numbers came from.
	EffectiveDateBasis values.LocalDate
	// BandID and BandVersion cite the exact band version the range was
	// computed from, so a later simulation or audit can replay it.
	BandID      string
	BandVersion string
}

// Available reports whether the caller may render CompensationGuardrail's
// data fields.
func (g CompensationGuardrail) Available() bool { return g.Status == GuardrailStatusAvailable }

// unavailable builds the one shape every refusal ground returns: no data
// field is ever set alongside GuardrailStatusUnavailable.
func unavailable(reason GuardrailUnavailableReason) CompensationGuardrail {
	return CompensationGuardrail{Status: GuardrailStatusUnavailable, Reason: reason}
}

// EvaluateCompensationGuardrail computes GREEN's guardrail as a pure
// function of an already-governed compensation read and the resolved target
// pay band. It makes no authorization decision of its own -- Current.Base's
// Presence state is that decision, already made by whatever governed
// compensation read produced it -- and it performs no simulation: this is
// the pre-submit "what would be allowed" answer PROMOUX-006 puts in front of
// the money field, not the post-submit compensation revision PROMO-003's
// simcomp evaluates once a proposed amount actually exists.
//
// Only a genuine contract failure is returned as an error: a malformed
// request, a catalog that fails for a reason other than "no such band", or a
// resolved band that disagrees with the request about currency or declared
// scale. Every business outcome -- available or not -- is the returned
// [CompensationGuardrail]'s own Status.
func EvaluateCompensationGuardrail(ctx context.Context, req CompensationGuardrailRequest) (CompensationGuardrail, error) {
	if err := req.Validate(); err != nil {
		return CompensationGuardrail{}, err
	}

	// No zero value meaning permissive: only an actually-disclosed value
	// proves authorization. Absent, null, redacted, unknown, unavailable and
	// not-applicable all fail closed to the identical typed action -- Get
	// returns ok=false for every one of them.
	current, ok := req.Current.Base.Get()
	if !ok {
		return unavailable(GuardrailReasonNotAuthorized), nil
	}
	if err := current.Validate(); err != nil {
		return unavailable(GuardrailReasonNotAuthorized), nil
	}

	record, err := req.Catalog.LookupBand(ctx, req.Target)
	switch {
	case err == nil:
		// fall through to the band-position and range computation below.
	case errors.Is(err, rewards.ErrBandNotFound):
		return unavailable(GuardrailReasonBandUnresolved), nil
	default:
		return CompensationGuardrail{}, fmt.Errorf("promotion: compensation guardrail: catalog lookup: %w", err)
	}
	if err := record.Validate(); err != nil {
		return unavailable(GuardrailReasonBandUnresolved), nil
	}
	band := record.Band
	if band.Currency() != req.Target.Currency {
		return CompensationGuardrail{}, fmt.Errorf("%w: catalog answered a band in %s for a %s query",
			rewards.ErrBandScopeMismatch, band.Currency(), req.Target.Currency)
	}

	currentAnnualized, err := req.Annualization.Annualize(current, req.Current.PayBasis)
	if err != nil {
		return CompensationGuardrail{}, fmt.Errorf("promotion: compensation guardrail: annualize current pay: %w", err)
	}

	// payband.Evaluate is where a currency or declared-scale disagreement
	// between the current pay and the band bounds surfaces (ErrAmountCurrency,
	// ErrScaleMismatch): a genuine composition fault, not a business finding.
	position, err := payband.Evaluate(band, currentAnnualized)
	if err != nil {
		return CompensationGuardrail{}, fmt.Errorf("promotion: compensation guardrail: evaluate band position: %w", err)
	}

	permitted, err := permittedIncreasePercent(currentAnnualized, band.Maximum)
	if err != nil {
		return CompensationGuardrail{}, fmt.Errorf("promotion: compensation guardrail: %w", err)
	}

	return CompensationGuardrail{
		Status:                   GuardrailStatusAvailable,
		CurrentAnnualized:        currentAnnualized,
		MinimumAnnualized:        band.Minimum,
		MaximumAnnualized:        band.Maximum,
		PermittedIncreasePercent: permitted,
		BandPosition:             position.Placement,
		Currency:                 band.Currency(),
		EffectiveDateBasis:       req.Target.AsOf,
		BandID:                   band.ID,
		BandVersion:              band.Version,
	}, nil
}

// permittedIncreasePercent computes (maximum - current) / current as an
// exact [values.Percentage]. Every step is fixed-point decimal from
// internal/kernel/values -- Money.Sub over declared-scale integers, then
// Decimal.Div rounded once at permittedIncreasePercentScale -- so there is
// no float64 conversion anywhere on this path, which is the clause RED's
// "0.0500..0.1800" raw-float range violates.
//
// A current amount already at or above the band maximum yields a
// non-positive permitted percent rather than an error: that is a real,
// exact answer -- "there is no headroom, or a negative one" -- not a
// failure to compute one.
func permittedIncreasePercent(current, maximum values.Money) (values.Percentage, error) {
	headroom, err := maximum.Sub(current)
	if err != nil {
		return values.Percentage{}, fmt.Errorf("headroom: %w", err)
	}
	fraction, err := headroom.Amount().Div(current.Amount(), permittedIncreasePercentScale, values.RoundingHalfEven)
	if err != nil {
		return values.Percentage{}, fmt.Errorf("permitted fraction: %w", err)
	}
	return values.NewPercentage(fraction.String(), permittedIncreasePercentScale, values.RoundingHalfEven)
}

// Canonical returns the canonical byte encoding of the guardrail, so a
// guardrail cited by a later proposal, simulation or audit is
// byte-comparable. An Unavailable result encodes only its status and
// reason: no data field is ever mixed into the same encoding, which is what
// makes two different Unavailable causes distinguishable only by Reason and
// never by anything resembling a range.
func (g CompensationGuardrail) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.CompensationGuardrail", promotionSchemaVer).
		String("status", g.Status.String()).
		String("reason", string(g.Reason))
	if g.Available() {
		w.Value("current_annualized", g.CurrentAnnualized).
			Value("minimum_annualized", g.MinimumAnnualized).
			Value("maximum_annualized", g.MaximumAnnualized).
			Value("permitted_increase_percent", g.PermittedIncreasePercent).
			String("band_position", g.BandPosition.String()).
			String("currency", g.Currency).
			Value("effective_date_basis", g.EffectiveDateBasis).
			String("band_id", g.BandID).
			String("band_version", g.BandVersion)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
