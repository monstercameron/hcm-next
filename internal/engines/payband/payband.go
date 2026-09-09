// Package payband is the pure pay-band position engine: given one versioned
// band and one amount, it answers where the amount sits in the band.
//
// Semantic owner: shared-engines. Phase: P1A.
//
// The engine is the arithmetic only. It does not read a catalog, decide
// whether a band check is advisory or blocking, or know what a promotion is;
// those are Rewards-domain decisions. Keeping the math here is what lets the
// same numbers be produced by a simulation, a workflow step and a UI without
// three implementations drifting apart.
//
// All arithmetic is exact fixed-point decimal from internal/kernel/values.
// There is no float64 anywhere in this package, and every rounding point is
// declared: ratios are produced at RatioScale under RatioRounding, and the
// money operands must already agree on a declared scale, because reconciling
// two different declared scales is a schema decision rather than an
// arithmetic one.
package payband

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Rounding contract for every ratio this engine produces. Both are part of the
// result contract: a caller that stores a compa-ratio stores it knowing the
// scale and the tie rule that produced it.
const (
	// RatioScale is the declared number of fractional digits on compa-ratio and
	// range penetration. Four digits distinguishes 0.9999 from 1.0000, which is
	// the boundary a band-exception rule actually cares about.
	RatioScale int32 = 4
	// RatioRounding is the declared tie rule for both ratios.
	RatioRounding = values.RoundingHalfEven
)

// schemaID and schemaVersion tag the canonical stream of this engine's types.
const (
	bandSchema     = "hcmnext.engines.payband.Band"
	positionSchema = "hcmnext.engines.payband.Position"
	schemaVersion  = 1
)

// Version reports this engine's own package contract version: the schema
// version every canonical encoder in this package agrees on (see
// bandSchema/positionSchema above). It is part of the ARCH-GO-009 engine
// package contract, not a business-facing evaluation input.
func Version() int { return schemaVersion }

// Engine errors. All are matchable with errors.Is.
var (
	// ErrBandIdentity is returned when a band carries no id or no version. An
	// unversioned band cannot be cited as evidence, so it is not usable.
	ErrBandIdentity = errors.New("payband: band requires an id and a version")
	// ErrBandCurrency is returned when the band's own bounds disagree on currency.
	ErrBandCurrency = errors.New("payband: band bounds are in different currencies")
	// ErrAmountCurrency is returned when the amount is in a different currency
	// from the band. There is no implicit FX conversion.
	ErrAmountCurrency = errors.New("payband: amount currency differs from band currency")
	// ErrBandOrder is returned when the bounds are not minimum <= midpoint <= maximum.
	ErrBandOrder = errors.New("payband: band bounds are not ordered minimum <= midpoint <= maximum")
	// ErrBandDegenerate is returned when minimum equals maximum, which makes
	// range penetration undefined rather than zero.
	ErrBandDegenerate = errors.New("payband: band minimum equals maximum, range penetration is undefined")
	// ErrBandMidpointZero is returned when the midpoint is zero, which makes
	// compa-ratio undefined rather than infinite.
	ErrBandMidpointZero = errors.New("payband: band midpoint is zero, compa-ratio is undefined")
	// ErrScaleMismatch is returned when the amount and the band bounds declare
	// different decimal scales.
	ErrScaleMismatch = errors.New("payband: amount and band bounds declare different decimal scales")
	// ErrCatalogDuplicateScope is returned by Compile when two bands in the
	// same catalog publish the same (job, grade, pay zone) scope. A catalog
	// lookup answers "the band for this scope," and two candidate answers is
	// a publish-time contradiction, not something Evaluate should have to
	// guess its way through.
	ErrCatalogDuplicateScope = errors.New("payband: catalog has more than one band for the same scope")
)

// Placement is where an amount sits relative to a band's bounds.
type Placement uint8

// Placements.
const (
	// PlacementUnspecified is the zero value and is never a legal result.
	PlacementUnspecified Placement = iota
	// PlacementBelowMinimum means the amount is under the band minimum.
	PlacementBelowMinimum
	// PlacementInBand means minimum <= amount <= maximum.
	PlacementInBand
	// PlacementAboveMaximum means the amount is over the band maximum.
	PlacementAboveMaximum
)

var placementWire = map[Placement]string{
	PlacementBelowMinimum: "BELOW_MINIMUM",
	PlacementInBand:       "IN_BAND",
	PlacementAboveMaximum: "ABOVE_MAXIMUM",
}

// String returns the stable wire token, or "PLACEMENT_UNSPECIFIED".
func (p Placement) String() string {
	if s, ok := placementWire[p]; ok {
		return s
	}
	return "PLACEMENT_UNSPECIFIED"
}

// Valid reports whether p is a legal placement.
func (p Placement) Valid() bool { _, ok := placementWire[p]; return ok }

// Scope is the band's addressing scope: the job, grade, pay zone and currency
// the band was published for. It is carried on the band so a result can be
// audited against the question that was asked.
type Scope struct {
	JobCode string
	Grade   string
	PayZone string
}

// Validate reports whether the scope is fully specified. A band that does not
// say which job, grade and zone it governs cannot be matched to a worker.
func (s Scope) Validate() error {
	switch {
	case s.JobCode == "":
		return fmt.Errorf("payband: scope requires a job code")
	case s.Grade == "":
		return fmt.Errorf("payband: scope requires a grade")
	case s.PayZone == "":
		return fmt.Errorf("payband: scope requires a pay zone")
	}
	return nil
}

// Band is one versioned pay band. Identity is (ID, Version): republishing a
// band with different bounds is a new version, never an edit, because a
// simulation cites the exact version it evaluated against.
type Band struct {
	ID       string
	Version  string
	Scope    Scope
	Minimum  values.Money
	Midpoint values.Money
	Maximum  values.Money
}

// Currency returns the band's currency, or "" when the band is unset.
func (b Band) Currency() string { return b.Minimum.Currency() }

// Validate reports whether the band is internally consistent and usable for
// both ratios. It refuses a degenerate range and a zero midpoint rather than
// letting Evaluate return an arithmetically meaningless number.
func (b Band) Validate() error {
	if b.ID == "" || b.Version == "" {
		return fmt.Errorf("%w: id %q version %q", ErrBandIdentity, b.ID, b.Version)
	}
	if err := b.Scope.Validate(); err != nil {
		return err
	}
	for _, m := range []values.Money{b.Minimum, b.Midpoint, b.Maximum} {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("payband: band %s@%s: %w", b.ID, b.Version, err)
		}
	}
	cur := b.Minimum.Currency()
	if b.Midpoint.Currency() != cur || b.Maximum.Currency() != cur {
		return fmt.Errorf("%w: %s/%s/%s", ErrBandCurrency,
			b.Minimum.Currency(), b.Midpoint.Currency(), b.Maximum.Currency())
	}
	scale := b.Minimum.Amount().Scale()
	if b.Midpoint.Amount().Scale() != scale || b.Maximum.Amount().Scale() != scale {
		return fmt.Errorf("%w: band bounds declare %d/%d/%d", ErrScaleMismatch,
			scale, b.Midpoint.Amount().Scale(), b.Maximum.Amount().Scale())
	}
	if b.Minimum.Amount().Cmp(b.Midpoint.Amount()) > 0 || b.Midpoint.Amount().Cmp(b.Maximum.Amount()) > 0 {
		return fmt.Errorf("%w: %s / %s / %s", ErrBandOrder, b.Minimum, b.Midpoint, b.Maximum)
	}
	if b.Minimum.Amount().Cmp(b.Maximum.Amount()) == 0 {
		return fmt.Errorf("%w: %s", ErrBandDegenerate, b.Minimum)
	}
	if b.Midpoint.Amount().IsZero() {
		return ErrBandMidpointZero
	}
	return nil
}

// Canonical returns the canonical byte encoding of the band, or nil when the
// band fails Validate.
func (b Band) Canonical() []byte {
	if b.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(bandSchema, schemaVersion).
		String("id", b.ID).
		String("version", b.Version).
		String("scope.job_code", b.Scope.JobCode).
		String("scope.grade", b.Scope.Grade).
		String("scope.pay_zone", b.Scope.PayZone).
		Value("minimum", b.Minimum).
		Value("midpoint", b.Midpoint).
		Value("maximum", b.Maximum).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// String returns a compact human form used in errors and logs.
func (b Band) String() string {
	if b.Validate() != nil {
		return ""
	}
	return fmt.Sprintf("%s@%s [%s..%s..%s]", b.ID, b.Version, b.Minimum, b.Midpoint, b.Maximum)
}

// Catalog is a validated, immutable set of pay bands published together,
// indexed by scope so a caller can look up the one band that governs a
// given job/grade/pay-zone without re-validating or re-scanning the whole
// publish on every Evaluate call. Build one with Compile; the zero Catalog
// has no bands.
type Catalog struct {
	bands map[string]Band
}

// scopeKey renders a scope as a stable map key. It is not itself part of any
// wire format - Band.Canonical already frames scope field-by-field - it only
// needs to be collision-free for the three strings a scope is made of.
func scopeKey(s Scope) string {
	return s.JobCode + "\x00" + s.Grade + "\x00" + s.PayZone
}

// Lookup returns the band published for scope, or false when the catalog has
// no band for it.
func (c Catalog) Lookup(scope Scope) (Band, bool) {
	b, ok := c.bands[scopeKey(scope)]
	return b, ok
}

// Len returns the number of bands in the catalog.
func (c Catalog) Len() int { return len(c.bands) }

// Compile is the band catalog validation step: it validates every band in
// bands and assembles them into a Catalog keyed by scope, failing the whole
// publish when any one band is internally invalid or when two bands claim
// the same scope. Both are failures a caller wants to catch once, at
// publish time, rather than discover later as a wrong or ambiguous
// Evaluate lookup.
//
// Compile is a pure function of bands: the same slice, in the same order,
// always either fails with the same error or produces a Catalog whose
// Lookup answers are the same regardless of Go's slice iteration being in
// declared order.
func Compile(bands []Band) (Catalog, error) {
	out := make(map[string]Band, len(bands))
	for _, b := range bands {
		if err := b.Validate(); err != nil {
			return Catalog{}, fmt.Errorf("payband: compile: %w", err)
		}
		key := scopeKey(b.Scope)
		if _, dup := out[key]; dup {
			return Catalog{}, fmt.Errorf("%w: job %q grade %q zone %q",
				ErrCatalogDuplicateScope, b.Scope.JobCode, b.Scope.Grade, b.Scope.PayZone)
		}
		out[key] = b
	}
	return Catalog{bands: out}, nil
}

// Position is the evaluated position of one amount in one band. It carries the
// band identity and version it was computed from, so the result is auditable
// on its own without re-resolving the catalog.
type Position struct {
	BandID      string
	BandVersion string
	Scope       Scope
	Amount      values.Money

	Placement Placement
	// CompaRatio is amount / midpoint at RatioScale.
	CompaRatio values.Decimal
	// RangePenetration is (amount - minimum) / (maximum - minimum) at
	// RatioScale. It is negative below the band and above 1 over the band; it
	// is not clamped, because clamping would hide the size of an exception.
	RangePenetration values.Decimal
	// Quartile is 1..4 for an in-band amount and 0 outside the band.
	Quartile int
	// DistanceToMinimum is amount - minimum; negative below the band.
	DistanceToMinimum values.Money
	// DistanceToMaximum is maximum - amount; negative above the band.
	DistanceToMaximum values.Money
}

// Validate reports whether the position is a complete result.
func (p Position) Validate() error {
	if p.BandID == "" || p.BandVersion == "" {
		return fmt.Errorf("%w: position cites id %q version %q", ErrBandIdentity, p.BandID, p.BandVersion)
	}
	if !p.Placement.Valid() {
		return fmt.Errorf("payband: position has no placement")
	}
	if err := p.Amount.Validate(); err != nil {
		return err
	}
	if err := p.CompaRatio.Validate(); err != nil {
		return err
	}
	return p.RangePenetration.Validate()
}

// Canonical returns the canonical byte encoding of the position, or nil when
// the position fails Validate.
func (p Position) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(positionSchema, schemaVersion).
		String("band_id", p.BandID).
		String("band_version", p.BandVersion).
		String("scope.job_code", p.Scope.JobCode).
		String("scope.grade", p.Scope.Grade).
		String("scope.pay_zone", p.Scope.PayZone).
		Value("amount", p.Amount).
		String("placement", p.Placement.String()).
		Value("compa_ratio", p.CompaRatio).
		Value("range_penetration", p.RangePenetration).
		Int("quartile", int64(p.Quartile)).
		Value("distance_to_minimum", p.DistanceToMinimum).
		Value("distance_to_maximum", p.DistanceToMaximum).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explain renders a compact, human-readable narrative of where the position
// sits in the band it was computed from: the band it cites, the placement,
// the compa-ratio, the range penetration and the quartile. It is meant for
// audit logs and review screens; branch on Placement/Quartile/CompaRatio
// for anything programmatic.
func (p Position) Explain() string {
	return fmt.Sprintf("band %s@%s: amount %s is %s (compa-ratio %s, range penetration %s, quartile %d)",
		p.BandID, p.BandVersion, p.Amount, p.Placement, p.CompaRatio, p.RangePenetration, p.Quartile)
}

// Evaluate returns the position of amount in band.
//
// It is a total function over valid inputs: an amount outside the band is a
// result with a placement and an unclamped ratio, not an error. Errors are
// reserved for inputs that make the arithmetic undefined - a different
// currency, a degenerate range, a zero midpoint, a mismatched declared scale -
// because those are contract failures a caller must fix rather than findings a
// reviewer can weigh.
func Evaluate(band Band, amount values.Money) (Position, error) {
	if err := band.Validate(); err != nil {
		return Position{}, err
	}
	if err := amount.Validate(); err != nil {
		return Position{}, fmt.Errorf("payband: amount: %w", err)
	}
	if amount.Currency() != band.Currency() {
		return Position{}, fmt.Errorf("%w: amount %s, band %s", ErrAmountCurrency,
			amount.Currency(), band.Currency())
	}
	if amount.Amount().Scale() != band.Minimum.Amount().Scale() {
		return Position{}, fmt.Errorf("%w: amount declares %d, band declares %d",
			ErrScaleMismatch, amount.Amount().Scale(), band.Minimum.Amount().Scale())
	}

	compaRatio, err := amount.Amount().Div(band.Midpoint.Amount(), RatioScale, RatioRounding)
	if err != nil {
		return Position{}, fmt.Errorf("payband: compa-ratio: %w", err)
	}

	span, err := band.Maximum.Amount().Sub(band.Minimum.Amount())
	if err != nil {
		return Position{}, fmt.Errorf("payband: band span: %w", err)
	}
	overMinimum, err := amount.Amount().Sub(band.Minimum.Amount())
	if err != nil {
		return Position{}, fmt.Errorf("payband: distance above minimum: %w", err)
	}
	penetration, err := overMinimum.Div(span, RatioScale, RatioRounding)
	if err != nil {
		return Position{}, fmt.Errorf("payband: range penetration: %w", err)
	}

	toMinimum, err := amount.Sub(band.Minimum)
	if err != nil {
		return Position{}, fmt.Errorf("payband: distance to minimum: %w", err)
	}
	toMaximum, err := band.Maximum.Sub(amount)
	if err != nil {
		return Position{}, fmt.Errorf("payband: distance to maximum: %w", err)
	}

	placement := PlacementInBand
	switch {
	case amount.Amount().Cmp(band.Minimum.Amount()) < 0:
		placement = PlacementBelowMinimum
	case amount.Amount().Cmp(band.Maximum.Amount()) > 0:
		placement = PlacementAboveMaximum
	}

	quartile := 0
	if placement == PlacementInBand {
		quartile = quartileOf(penetration)
	}

	return Position{
		BandID:            band.ID,
		BandVersion:       band.Version,
		Scope:             band.Scope,
		Amount:            amount,
		Placement:         placement,
		CompaRatio:        compaRatio,
		RangePenetration:  penetration,
		Quartile:          quartile,
		DistanceToMinimum: toMinimum,
		DistanceToMaximum: toMaximum,
	}, nil
}

// quartileBounds are the three interior cut points of the band, expressed at
// RatioScale so they compare exactly against a penetration value.
var quartileBounds = [3]values.Decimal{
	values.MustDecimal("0.2500", RatioScale, RatioRounding),
	values.MustDecimal("0.5000", RatioScale, RatioRounding),
	values.MustDecimal("0.7500", RatioScale, RatioRounding),
}

// quartileOf maps an in-band penetration to 1..4. The cut points are
// left-closed: exactly 0.2500 is the first value of the second quartile, so a
// worker sitting on a boundary is reported the same way every time.
func quartileOf(penetration values.Decimal) int {
	for i, bound := range quartileBounds {
		if penetration.Cmp(bound) < 0 {
			return i + 1
		}
	}
	return 4
}
