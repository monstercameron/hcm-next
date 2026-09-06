package rewards

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const PayBandPositionReadScope = "compensation.band.read"

var (
	ErrPositionRequestInvalid   = errors.New("rewards: pay-band position request is invalid")
	ErrPositionCurrencyMismatch = errors.New("rewards: pay-band position currency mismatch")
)

// PositionField is the field-level disclosure vocabulary for COMP-003.
type PositionField string

const (
	PositionFieldBand             PositionField = "compensation.position.band"
	PositionFieldAmount           PositionField = "compensation.position.amount"
	PositionFieldClass            PositionField = "compensation.position.class"
	PositionFieldBoundary         PositionField = "compensation.position.boundary"
	PositionFieldCompaRatio       PositionField = "compensation.position.compa_ratio"
	PositionFieldRangePenetration PositionField = "compensation.position.range_penetration"
)

var positionFields = []PositionField{
	PositionFieldBand, PositionFieldAmount, PositionFieldClass,
	PositionFieldBoundary, PositionFieldCompaRatio, PositionFieldRangePenetration,
}

func (f PositionField) Valid() bool {
	for _, known := range positionFields {
		if f == known {
			return true
		}
	}
	return false
}

type PositionFieldRuling struct {
	Effect people.Access
	Reason string
}

// PayBandPositionAuthorization mirrors COMP-001's subject and field-level
// disclosure rules while keeping pay-band fields closed and explicit.
type PayBandPositionAuthorization struct {
	PolicyVersion       string
	Purpose             string
	SubjectDisclosable  bool
	SubjectDenialReason string
	Scopes              []string
	Fields              map[PositionField]PositionFieldRuling
}

func (a PayBandPositionAuthorization) HasScope(scope string) bool {
	for _, declared := range a.Scopes {
		if declared == scope {
			return true
		}
	}
	return false
}

func (a PayBandPositionAuthorization) permitted() bool {
	return a.HasScope(PayBandPositionReadScope) || a.HasScope(CompensationReadScope)
}

func (a PayBandPositionAuthorization) Validate() error {
	if a.PolicyVersion == "" || a.Purpose == "" {
		return fmt.Errorf("%w: policy version and purpose are required", ErrPositionRequestInvalid)
	}
	if !a.SubjectDisclosable && a.SubjectDenialReason == "" && a.permitted() {
		return fmt.Errorf("%w: non-disclosable subject needs a reason", ErrPositionRequestInvalid)
	}
	for field, ruling := range a.Fields {
		if !field.Valid() || (ruling.Effect != people.AccessAuthorized && ruling.Effect != people.AccessDenied) {
			return fmt.Errorf("%w: invalid ruling for %s", ErrPositionRequestInvalid, field)
		}
		if ruling.Effect == people.AccessDenied && ruling.Reason == "" {
			return fmt.Errorf("%w: denial for %s needs a reason", ErrPositionRequestInvalid, field)
		}
	}
	return nil
}

func (a PayBandPositionAuthorization) ruling(field PositionField) PositionFieldRuling {
	if ruling, ok := a.Fields[field]; ok {
		return ruling
	}
	return PositionFieldRuling{Effect: people.AccessAuthorized}
}

func (a PayBandPositionAuthorization) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.rewards.PayBandPositionAuthorization", rewardsSchemaVer).
		String("policy_version", a.PolicyVersion).String("purpose", a.Purpose).
		Bool("subject_disclosable", a.SubjectDisclosable).String("subject_denial_reason", a.SubjectDenialReason)
	for _, field := range positionFields {
		ruling := a.ruling(field)
		w.String("field", string(field)).String("effect", ruling.Effect.String()).String("reason", ruling.Reason)
	}
	return mustWriterBytes(w)
}

// PayBandPositionRequest evaluates one annualized amount against one pinned
// pay-band version. Band identity and all three bounds are carried by
// payband.Band; there is no catalog lookup or implicit current version.
type PayBandPositionRequest struct {
	Band          payband.Band
	Annualized    values.Money
	Authorization PayBandPositionAuthorization
}

func (r PayBandPositionRequest) Validate() error {
	if err := r.Band.Validate(); err != nil {
		return fmt.Errorf("%w: band: %w", ErrPositionRequestInvalid, err)
	}
	if err := r.Annualized.Validate(); err != nil {
		return fmt.Errorf("%w: annualized amount: %w", ErrPositionRequestInvalid, err)
	}
	if r.Annualized.Currency() != r.Band.Currency() {
		return fmt.Errorf("%w: amount %s, band %s", ErrPositionCurrencyMismatch, r.Annualized.Currency(), r.Band.Currency())
	}
	return r.Authorization.Validate()
}

func (r PayBandPositionRequest) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.rewards.PayBandPositionRequest", rewardsSchemaVer).
		Value("band", r.Band).Value("annualized", r.Annualized).Value("authorization", r.Authorization)
	return mustWriterBytes(w)
}

func mustWriterBytes(w *canonicalbytes.Writer) []byte {
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// PositionClass is the business-facing, three-state classification. The
// boundary is carried separately so BELOW and ABOVE never lose which bound
// was crossed.
type PositionClass uint8

const (
	PositionClassUnspecified PositionClass = iota
	PositionClassBelow
	PositionClassWithin
	PositionClassAbove
)

func (p PositionClass) String() string {
	switch p {
	case PositionClassBelow:
		return "BELOW"
	case PositionClassWithin:
		return "WITHIN"
	case PositionClassAbove:
		return "ABOVE"
	default:
		return "POSITION_CLASS_UNSPECIFIED"
	}
}

func (p PositionClass) Valid() bool { return p >= PositionClassBelow && p <= PositionClassAbove }

type PositionBoundary uint8

const (
	PositionBoundaryUnspecified PositionBoundary = iota
	PositionBoundaryNone
	PositionBoundaryMinimum
	PositionBoundaryMaximum
)

func (b PositionBoundary) String() string {
	switch b {
	case PositionBoundaryNone:
		return "NONE"
	case PositionBoundaryMinimum:
		return "MINIMUM"
	case PositionBoundaryMaximum:
		return "MAXIMUM"
	default:
		return "BOUNDARY_UNSPECIFIED"
	}
}

type PositionDecimalField struct {
	Access       people.Access
	Value        values.Presence[values.Decimal]
	DenialReason string
}

// PayBandPosition is the disclosed result of COMP-003. Direct numeric fields
// are populated only when authorized; the presence fields retain explicit
// DENIED states for callers that need the COMP-001 field contract.
type PayBandPosition struct {
	Disclosure            people.Disclosure
	WithheldReason        string
	BandID                string
	BandVersion           string
	Currency              string
	Annualized            values.Money
	Class                 PositionClass
	Boundary              PositionBoundary
	CompaRatio            values.Decimal
	RangePenetration      values.Decimal
	CompaRatioField       PositionDecimalField
	RangePenetrationField PositionDecimalField
	InputsDigest          string
	ResultDigest          string
}

func deniedPositionDecimal(reason string) PositionDecimalField {
	return PositionDecimalField{Access: people.AccessDenied, Value: values.Redacted[values.Decimal](reason), DenialReason: reason}
}

func authorizedPositionDecimal(v values.Decimal) PositionDecimalField {
	return PositionDecimalField{Access: people.AccessAuthorized, Value: values.Value(v)}
}

func (p PayBandPosition) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.rewards.PayBandPosition", rewardsSchemaVer).
		String("disclosure", p.Disclosure.String()).String("withheld_reason", p.WithheldReason).
		String("band_id", p.BandID).String("band_version", p.BandVersion).String("currency", p.Currency).
		String("class", p.Class.String()).String("boundary", p.Boundary.String()).
		Bool("annualized?", p.Annualized.Validate() == nil)
	if p.Annualized.Validate() == nil {
		w.Value("annualized", p.Annualized)
	}
	w.String("compa_access", p.CompaRatioField.Access.String()).String("compa_reason", p.CompaRatioField.DenialReason).
		Bool("compa?", p.CompaRatioField.Access == people.AccessAuthorized)
	if p.CompaRatioField.Access == people.AccessAuthorized {
		w.Value("compa_ratio", p.CompaRatio)
	}
	w.String("penetration_access", p.RangePenetrationField.Access.String()).String("penetration_reason", p.RangePenetrationField.DenialReason).
		Bool("penetration?", p.RangePenetrationField.Access == people.AccessAuthorized)
	if p.RangePenetrationField.Access == people.AccessAuthorized {
		w.Value("range_penetration", p.RangePenetration)
	}
	return w.Bytes()
}

func (p PayBandPosition) Canonical() []byte {
	body, err := p.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.PayBandPositionEnvelope", rewardsSchemaVer).
		Field("body", body).String("inputs_digest", p.InputsDigest).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r PayBandPositionRequest) inputDigest() (string, error) {
	return canonicalbytes.New("hcmnext.domains.rewards.EvaluateCompensationPositionInput", rewardsSchemaVer).
		Value("band", r.Band).Value("annualized", r.Annualized).Value("authorization", r.Authorization).Digest()
}

// EvaluateCompensationPayBandPosition applies the pure payband engine and
// then applies disclosure. It never converts currencies or substitutes a
// different band version.
func EvaluateCompensationPayBandPosition(r PayBandPositionRequest) (PayBandPosition, error) {
	if err := r.Validate(); err != nil {
		return PayBandPosition{}, err
	}
	inputs, err := r.inputDigest()
	if err != nil {
		return PayBandPosition{}, err
	}
	if !r.Authorization.permitted() || !r.Authorization.SubjectDisclosable {
		reason := r.Authorization.SubjectDenialReason
		if reason == "" {
			reason = "missing_scope:compensation.band.read"
		}
		result := PayBandPosition{Disclosure: people.DisclosureWithheld, WithheldReason: reason, InputsDigest: inputs}
		return finishPayBandPosition(result)
	}
	position, err := payband.Evaluate(r.Band, r.Annualized)
	if err != nil {
		return PayBandPosition{}, err
	}
	class, boundary := PositionClassWithin, PositionBoundaryNone
	switch position.Placement {
	case payband.PlacementBelowMinimum:
		class, boundary = PositionClassBelow, PositionBoundaryMinimum
	case payband.PlacementAboveMaximum:
		class, boundary = PositionClassAbove, PositionBoundaryMaximum
	}
	result := PayBandPosition{
		Disclosure: people.DisclosureFull, BandID: r.Band.ID, BandVersion: r.Band.Version,
		Currency: r.Band.Currency(), Annualized: r.Annualized, Class: class, Boundary: boundary,
		CompaRatio: position.CompaRatio, RangePenetration: position.RangePenetration,
		CompaRatioField:       authorizedPositionDecimal(position.CompaRatio),
		RangePenetrationField: authorizedPositionDecimal(position.RangePenetration), InputsDigest: inputs,
	}
	denied := 0
	for field, ruling := range r.Authorization.Fields {
		if ruling.Effect != people.AccessDenied {
			continue
		}
		denied++
		switch field {
		case PositionFieldBand:
			result.BandID, result.BandVersion, result.Currency = "", "", ""
		case PositionFieldAmount:
			result.Annualized = values.Money{}
		case PositionFieldClass:
			result.Class = PositionClassUnspecified
		case PositionFieldBoundary:
			result.Boundary = PositionBoundaryUnspecified
		case PositionFieldCompaRatio:
			result.CompaRatio, result.CompaRatioField = values.Decimal{}, deniedPositionDecimal(ruling.Reason)
		case PositionFieldRangePenetration:
			result.RangePenetration, result.RangePenetrationField = values.Decimal{}, deniedPositionDecimal(ruling.Reason)
		}
	}
	if denied > 0 {
		result.Disclosure = people.DisclosurePartial
	}
	return finishPayBandPosition(result)
}

func finishPayBandPosition(result PayBandPosition) (PayBandPosition, error) {
	body, err := result.canonicalBody()
	if err != nil {
		return PayBandPosition{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)
	return result, nil
}

// EvaluateAnnualizedPayBandPosition is a descriptive alias for callers whose
// input variable is already an annualized amount.
func EvaluateAnnualizedPayBandPosition(r PayBandPositionRequest) (PayBandPosition, error) {
	return EvaluateCompensationPayBandPosition(r)
}

// EvaluateCompensationPosition is a concise alias for the disclosed
// annualized pay-band calculation.
func EvaluateCompensationPosition(r PayBandPositionRequest) (PayBandPosition, error) {
	return EvaluateCompensationPayBandPosition(r)
}

// Explain is the disclosure-safe position trace. It contains no amount or
// ratio when the whole result is WITHHELD, and no denied metric value when a
// field is DENIED.
type PositionFieldExplanation struct {
	Field        PositionField
	Access       people.Access
	DenialReason string
}

type PayBandPositionExplanation struct {
	Disclosure       people.Disclosure
	WithheldReason   string
	BandID           string
	BandVersion      string
	Class            PositionClass
	Boundary         PositionBoundary
	CompaRatio       values.Decimal
	RangePenetration values.Decimal
	Fields           []PositionFieldExplanation
	InputsDigest     string
	ResultDigest     string
}

func (p PayBandPosition) Explain() PayBandPositionExplanation {
	x := PayBandPositionExplanation{
		Disclosure: p.Disclosure, WithheldReason: p.WithheldReason,
		BandID: p.BandID, BandVersion: p.BandVersion, Class: p.Class, Boundary: p.Boundary,
		InputsDigest: p.InputsDigest, ResultDigest: p.ResultDigest,
	}
	if p.CompaRatioField.Access == people.AccessAuthorized {
		x.CompaRatio = p.CompaRatio
	}
	if p.RangePenetrationField.Access == people.AccessAuthorized {
		x.RangePenetration = p.RangePenetration
	}
	if p.Disclosure == people.DisclosureWithheld {
		return x
	}
	for _, field := range positionFields {
		var access people.Access
		var reason string
		switch field {
		case PositionFieldBand:
			access = people.AccessAuthorized
		case PositionFieldAmount:
			access = people.AccessAuthorized
		case PositionFieldClass:
			access = people.AccessAuthorized
		case PositionFieldBoundary:
			access = people.AccessAuthorized
		case PositionFieldCompaRatio:
			access, reason = p.CompaRatioField.Access, p.CompaRatioField.DenialReason
		case PositionFieldRangePenetration:
			access, reason = p.RangePenetrationField.Access, p.RangePenetrationField.DenialReason
		}
		if access != people.AccessUnspecified {
			x.Fields = append(x.Fields, PositionFieldExplanation{Field: field, Access: access, DenialReason: reason})
		}
	}
	return x
}

func ExplainPayBandPosition(p PayBandPosition) PayBandPositionExplanation { return p.Explain() }
