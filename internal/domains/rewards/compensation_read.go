package rewards

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	AuthorizedCompensationReadIntentType    = "hcmnext.rewards.authorized_compensation.read"
	AuthorizedCompensationReadIntentVersion = "v1"
	AuthorizedCompensationReadRulePack      = "rewards.authorized_compensation/2026.1"
	CompensationReadScope                   = "compensation.read"
)

var (
	ErrCompensationReadInvalid = errors.New("rewards: compensation read request is invalid")
	ErrCompensationFactInvalid = errors.New("rewards: compensation fact is invalid")
	ErrCompensationReader      = errors.New("rewards: compensation reader failed")
	ErrCompensationSubject     = errors.New("rewards: compensation reader answered about another worker")
	ErrCompensationAuth        = errors.New("rewards: compensation authorization is invalid")
	ErrCompensationStale       = errors.New("rewards: compensation projection is stale")
	ErrCompensationDisagreeing = errors.New("rewards: compensation projections disagree")
)

// CompensationField is the closed field mask owned by this read. Payroll
// accounts, bank data and raw incumbent payloads have no field in this mask.
type CompensationField string

const (
	FieldBasePay    CompensationField = "compensation.base_pay"
	FieldPayBandRef CompensationField = "compensation.pay_band_ref"
	FieldComponents CompensationField = "compensation.components"
	FieldCurrency   CompensationField = "compensation.currency"
	FieldPayBasis   CompensationField = "compensation.pay_basis"
	FieldFrequency  CompensationField = "compensation.frequency"
)

var compensationFields = []CompensationField{
	FieldBasePay, FieldPayBandRef, FieldComponents, FieldCurrency, FieldPayBasis, FieldFrequency,
}

func (f CompensationField) Valid() bool {
	for _, known := range compensationFields {
		if f == known {
			return true
		}
	}
	return false
}

func (f CompensationField) String() string { return string(f) }

// CompensationFields returns the immutable projection in canonical order.
func CompensationFields() []CompensationField {
	return append([]CompensationField(nil), compensationFields...)
}

// CompensationComponent is a safe, typed component. It contains no bank,
// payroll-account or provider-specific payload.
type CompensationComponent struct {
	ID     string
	Kind   string
	Amount values.Money
}

func (c CompensationComponent) Validate() error {
	if c.ID == "" || c.Kind == "" {
		return fmt.Errorf("%w: component id and kind are required", ErrCompensationFactInvalid)
	}
	if err := c.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: component %s amount: %v", ErrCompensationFactInvalid, c.ID, err)
	}
	return nil
}

// CompensationFact is one effective-dated compensation assertion.
type CompensationFact struct {
	Worker     values.EntityRef
	BasePay    values.Money
	PayBandRef string
	Components []CompensationComponent
	Currency   string
	PayBasis   PayBasis
	Frequency  string

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

func (f CompensationFact) Validate() error {
	if err := f.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrCompensationFactInvalid, err)
	}
	if f.Worker.Kind != people.KindWorker {
		return fmt.Errorf("%w: subject is not a worker", ErrCompensationFactInvalid)
	}
	if err := f.BasePay.Validate(); err != nil {
		return fmt.Errorf("%w: base pay: %v", ErrCompensationFactInvalid, err)
	}
	if f.Currency == "" || f.Currency != f.BasePay.Currency() {
		return fmt.Errorf("%w: explicit currency does not match base pay", ErrCompensationFactInvalid)
	}
	if f.PayBandRef == "" || f.Frequency == "" || !f.PayBasis.Valid() {
		return fmt.Errorf("%w: pay band, basis and frequency are required", ErrCompensationFactInvalid)
	}
	for _, c := range f.Components {
		if err := c.Validate(); err != nil {
			return err
		}
		if c.Amount.Currency() != f.Currency {
			return fmt.Errorf("%w: component %s has a different currency", ErrCompensationFactInvalid, c.ID)
		}
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrCompensationFactInvalid, err)
	}
	if f.Effective.Kind() != values.IntervalKindInstant {
		return fmt.Errorf("%w: effective interval must be INSTANT", ErrCompensationFactInvalid)
	}
	if f.KnownAt.Canonical() == nil || !f.Revision.IsSpecified() {
		return fmt.Errorf("%w: known-at and revision are required", ErrCompensationFactInvalid)
	}
	if err := f.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %v", ErrCompensationFactInvalid, err)
	}
	if err := f.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %v", ErrCompensationFactInvalid, err)
	}
	return values.ValidateKnowledgeOrder(f.KnownAt, f.Provenance.RecordedAt, false)
}

// CompensationFactsQuery is the fixed-mask, as-of instant port request.
type CompensationFactsQuery struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   values.Instant
	Fields []CompensationField
}

func (q CompensationFactsQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrCompensationReadInvalid, err)
	}
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrCompensationReadInvalid, err)
	}
	if q.Worker.Tenant != q.Tenant || q.Worker.Kind != people.KindWorker {
		return fmt.Errorf("%w: worker is outside tenant or is not a worker", ErrCompensationReadInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrCompensationReadInvalid, err)
	}
	if len(q.Fields) == 0 {
		return fmt.Errorf("%w: empty fixed field mask", ErrCompensationReadInvalid)
	}
	seen := map[CompensationField]bool{}
	for _, field := range q.Fields {
		if !field.Valid() {
			return fmt.Errorf("%w: unknown field %s", ErrCompensationReadInvalid, field)
		}
		if seen[field] {
			return fmt.Errorf("%w: duplicate field %s", ErrCompensationReadInvalid, field)
		}
		seen[field] = true
	}
	return nil
}

// CompensationFactSet is one consistent answer from a compensation adapter.
type CompensationFactSet struct {
	Worker        values.EntityRef
	Exists        bool
	Fact          CompensationFact
	Watermark     values.RevisionToken
	PolicyVersion string
	Stale         bool
	Disagreeing   bool
}

func (s CompensationFactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrCompensationFactInvalid, err)
	}
	if !s.Exists {
		return nil
	}
	if !s.Watermark.IsSpecified() || s.PolicyVersion == "" {
		return fmt.Errorf("%w: existing fact needs watermark and policy version", ErrCompensationFactInvalid)
	}
	if s.Fact.Worker != s.Worker {
		return fmt.Errorf("%w: fact worker mismatch", ErrCompensationSubject)
	}
	return s.Fact.Validate()
}

type CompensationFacts interface {
	CompensationFactsAt(context.Context, CompensationFactsQuery) (CompensationFactSet, error)
}

// CompensationFieldRuling is the caller's field-level disclosure decision.
type CompensationFieldRuling struct {
	Effect people.Effect
	Reason string
}

// CompensationAuthorization is supplied by the caller's already-evaluated
// policy. The compensation scope is mandatory; field rulings can narrow it.
type CompensationAuthorization struct {
	PolicyVersion       string
	Purpose             string
	SubjectDisclosable  bool
	SubjectDenialReason string
	Scopes              []string
	Fields              map[CompensationField]CompensationFieldRuling
}

func (a CompensationAuthorization) HasScope(scope string) bool {
	for _, declared := range a.Scopes {
		if declared == scope {
			return true
		}
	}
	return false
}

func (a CompensationAuthorization) Validate() error {
	if a.PolicyVersion == "" || a.Purpose == "" {
		return fmt.Errorf("%w: policy version and purpose are required", ErrCompensationAuth)
	}
	if !a.SubjectDisclosable && a.SubjectDenialReason == "" && a.HasScope(CompensationReadScope) {
		return fmt.Errorf("%w: non-disclosable subject needs a reason", ErrCompensationAuth)
	}
	for field, ruling := range a.Fields {
		if !field.Valid() || !ruling.Effect.Valid() {
			return fmt.Errorf("%w: invalid ruling for %s", ErrCompensationAuth, field)
		}
		if ruling.Effect == people.EffectDeny && ruling.Reason == "" {
			return fmt.Errorf("%w: denial for %s needs a reason", ErrCompensationAuth, field)
		}
	}
	return nil
}

func (a CompensationAuthorization) ruling(field CompensationField) CompensationFieldRuling {
	if ruling, ok := a.Fields[field]; ok {
		return ruling
	}
	return CompensationFieldRuling{Effect: people.EffectAllow}
}

type MoneyField struct {
	Access       people.Access
	Value        values.Presence[values.Money]
	DenialReason string
}

type TextField struct {
	Access       people.Access
	Value        values.Presence[string]
	DenialReason string
}

type ComponentsField struct {
	Access       people.Access
	Value        []CompensationComponent
	DenialReason string
}

// CompensationRead is the typed result of the closed compensation read.
type CompensationRead struct {
	Worker         values.EntityRef
	Disclosure     people.Disclosure
	Presence       people.SubjectPresence
	WithheldReason string
	AsOf           values.Instant
	BasePay        MoneyField
	PayBandRef     TextField
	Components     ComponentsField
	Currency       TextField
	PayBasis       TextField
	Frequency      TextField
	Effective      values.EffectiveInterval
	KnownAt        values.KnownAt
	Revision       values.RevisionToken
	Authority      evidence.SourceAuthority
	Provenance     evidence.Provenance
	Watermark      values.RevisionToken
	PolicyVersion  string
	InputsDigest   string
	ResultDigest   string
	Effects        evidence.EffectCounters
	Receipt        evidence.ZeroEffectReceipt
}

func (r CompensationRead) Value(field CompensationField) (string, bool) {
	switch field {
	case FieldBasePay:
		if r.BasePay.Access != people.AccessAuthorized {
			return "", false
		}
		m, ok := r.BasePay.Value.Get()
		if !ok {
			return "", false
		}
		return m.String(), true
	case FieldPayBandRef:
		return r.PayBandRef.Value.Get()
	case FieldCurrency:
		return r.Currency.Value.Get()
	case FieldPayBasis:
		return r.PayBasis.Value.Get()
	case FieldFrequency:
		return r.Frequency.Value.Get()
	default:
		return "", false
	}
}

func canonicalTextField(w *canonicalbytes.Writer, name string, field TextField) {
	w.String(name+".access", field.Access.String()).String(name+".reason", field.DenialReason).
		String(name+".state", field.Value.State().String())
	if value, ok := field.Value.Get(); ok {
		w.String(name, value)
	}
}

func (r CompensationRead) canonicalBody() []byte {
	w := canonicalbytes.New("hcmnext.domains.rewards.CompensationRead", rewardsSchemaVer).
		Value("worker", r.Worker).
		String("disclosure", r.Disclosure.String()).
		String("presence", r.Presence.String()).
		String("withheld_reason", r.WithheldReason).
		Value("as_of", r.AsOf).
		String("policy_version", r.PolicyVersion).
		Bool("watermark?", r.Watermark.IsSpecified())
	if r.Watermark.IsSpecified() {
		w.Value("watermark", r.Watermark)
	}
	if r.Effective.Validate() == nil {
		w.Value("effective", r.Effective).Value("known_at", r.KnownAt).Value("revision", r.Revision).
			Value("authority", r.Authority).Value("provenance", r.Provenance)
	}
	w.String("base_pay.access", r.BasePay.Access.String()).String("base_pay.reason", r.BasePay.DenialReason).
		String("base_pay.state", r.BasePay.Value.State().String())
	if amount, ok := r.BasePay.Value.Get(); ok {
		w.Value("base_pay", amount)
	}
	canonicalTextField(w, "pay_band_ref", r.PayBandRef)
	canonicalTextField(w, "currency", r.Currency)
	canonicalTextField(w, "pay_basis", r.PayBasis)
	canonicalTextField(w, "frequency", r.Frequency)
	w.String("components.access", r.Components.Access.String()).String("components.reason", r.Components.DenialReason).
		Count("components", len(r.Components.Value))
	for _, component := range r.Components.Value {
		w.String("component.id", component.ID).String("component.kind", component.Kind).Value("component.amount", component.Amount)
	}
	w.Value("effects", r.Effects)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r CompensationRead) Canonical() []byte {
	body := r.canonicalBody()
	if len(body) == 0 {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.CompensationReadEnvelope", rewardsSchemaVer).
		Field("body", body).String("inputs_digest", r.InputsDigest).Value("receipt", r.Receipt).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func compensationInputDigest(req CompensationReadRequest) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.rewards.CompensationReadRequest", rewardsSchemaVer).
		String("tenant", string(req.Tenant)).Value("worker", req.Worker).Value("as_of", req.AsOf).
		String("policy_version", req.Authorization.PolicyVersion).String("purpose", req.Authorization.Purpose)
	for _, field := range compensationFields {
		ruling := req.Authorization.ruling(field)
		w.String("field", string(field)).String("effect", ruling.Effect.String()).String("reason", ruling.Reason)
	}
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

func finishCompensationRead(r CompensationRead) (CompensationRead, error) {
	body := r.canonicalBody()
	if len(body) == 0 {
		return CompensationRead{}, fmt.Errorf("%w: result is not canonical", ErrCompensationReadInvalid)
	}
	r.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		AuthorizedCompensationReadIntentType, AuthorizedCompensationReadIntentVersion,
		evidence.ModeSimulate, evidence.RequestStateSimulated,
		[]evidence.ControlVersion{{Name: "authorization_policy", Version: r.PolicyVersion}, {Name: "compensation_read_rules", Version: AuthorizedCompensationReadRulePack}},
		r.InputsDigest, r.ResultDigest, r.Effects)
	if err != nil {
		return CompensationRead{}, err
	}
	r.Receipt = receipt
	return r, nil
}

// CompensationReadRequest is the caller-scoped request for the fixed read.
type CompensationReadRequest struct {
	Tenant        values.TenantId
	Worker        values.EntityRef
	AsOf          values.Instant
	Authorization CompensationAuthorization
}

func (r CompensationReadRequest) Validate() error {
	q := CompensationFactsQuery{Tenant: r.Tenant, Worker: r.Worker, AsOf: r.AsOf, Fields: compensationFields}
	if err := q.Validate(); err != nil {
		return err
	}
	return r.Authorization.Validate()
}

func deniedMoney(reason string) MoneyField {
	return MoneyField{Access: people.AccessDenied, Value: values.Redacted[values.Money](reason), DenialReason: reason}
}

func deniedText(reason string) TextField {
	return TextField{Access: people.AccessDenied, Value: values.Redacted[string](reason), DenialReason: reason}
}

func deniedComponents(reason string) ComponentsField {
	return ComponentsField{Access: people.AccessDenied, DenialReason: reason}
}

func unknownMoney(reason string) MoneyField {
	return MoneyField{Access: people.AccessAuthorized, Value: values.Unknown[values.Money](reason)}
}

func unknownText(reason string) TextField {
	return TextField{Access: people.AccessAuthorized, Value: values.Unknown[string](reason)}
}

// ReadCompensation is COMP-001's closed specialization. Missing scope is a
// whole-subject WITHHELD result and therefore never calls the facts port.
func ReadCompensation(ctx context.Context, reader CompensationFacts, req CompensationReadRequest) (CompensationRead, error) {
	if err := req.Validate(); err != nil {
		return CompensationRead{}, err
	}
	inputs, err := compensationInputDigest(req)
	if err != nil {
		return CompensationRead{}, err
	}
	result := CompensationRead{Worker: req.Worker, AsOf: req.AsOf, PolicyVersion: req.Authorization.PolicyVersion, InputsDigest: inputs, Effects: evidence.ZeroEffects()}
	if !req.Authorization.HasScope(CompensationReadScope) || !req.Authorization.SubjectDisclosable {
		reason := req.Authorization.SubjectDenialReason
		if reason == "" {
			reason = "missing_scope:compensation.read"
		}
		result.Disclosure = people.DisclosureWithheld
		result.WithheldReason = reason
		return finishCompensationRead(result)
	}
	fields := compensationFields
	for _, field := range compensationFields {
		if req.Authorization.ruling(field).Effect == people.EffectDeny {
			fields = removeCompensationField(fields, field)
		}
	}
	if reader == nil && len(fields) > 0 {
		return CompensationRead{}, fmt.Errorf("%w: no compensation facts reader", ErrCompensationReadInvalid)
	}
	var set CompensationFactSet
	if len(fields) > 0 {
		var readErr error
		set, readErr = reader.CompensationFactsAt(ctx, CompensationFactsQuery{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf, Fields: fields})
		if readErr != nil {
			return CompensationRead{}, fmt.Errorf("%w: %v", ErrCompensationReader, readErr)
		}
		if err := set.Validate(); err != nil {
			return CompensationRead{}, err
		}
		if set.Worker != req.Worker {
			return CompensationRead{}, fmt.Errorf("%w: asked %s, answered %s", ErrCompensationSubject, req.Worker, set.Worker)
		}
		if set.Stale {
			return CompensationRead{}, ErrCompensationStale
		}
		if set.Disagreeing {
			return CompensationRead{}, ErrCompensationDisagreeing
		}
	} else {
		set = CompensationFactSet{Worker: req.Worker, Exists: true}
	}
	result.Presence = people.SubjectPresent
	if !set.Exists {
		result.Presence = people.SubjectAbsent
	}
	result.Watermark = set.Watermark
	if set.Exists {
		fact := set.Fact
		result.Effective, result.KnownAt, result.Revision = fact.Effective, fact.KnownAt, fact.Revision
		result.Authority, result.Provenance = fact.Authority, fact.Provenance
		result.BasePay = MoneyField{Access: people.AccessAuthorized, Value: values.Value(fact.BasePay)}
		result.PayBandRef = TextField{Access: people.AccessAuthorized, Value: values.Value(fact.PayBandRef)}
		result.Components = ComponentsField{Access: people.AccessAuthorized, Value: append([]CompensationComponent(nil), fact.Components...)}
		result.Currency = TextField{Access: people.AccessAuthorized, Value: values.Value(fact.Currency)}
		result.PayBasis = TextField{Access: people.AccessAuthorized, Value: values.Value(fact.PayBasis.String())}
		result.Frequency = TextField{Access: people.AccessAuthorized, Value: values.Value(fact.Frequency)}
	} else {
		result.BasePay = unknownMoney("subject_absent_at_requested_coordinate")
		result.PayBandRef = unknownText("subject_absent_at_requested_coordinate")
		result.Currency = unknownText("subject_absent_at_requested_coordinate")
		result.PayBasis = unknownText("subject_absent_at_requested_coordinate")
		result.Frequency = unknownText("subject_absent_at_requested_coordinate")
		result.Components = ComponentsField{Access: people.AccessAuthorized}
	}
	denied := 0
	for field := range req.Authorization.Fields {
		if req.Authorization.ruling(field).Effect != people.EffectDeny {
			continue
		}
		denied++
		reason := req.Authorization.ruling(field).Reason
		switch field {
		case FieldBasePay:
			result.BasePay = deniedMoney(reason)
		case FieldPayBandRef:
			result.PayBandRef = deniedText(reason)
		case FieldComponents:
			result.Components = deniedComponents(reason)
		case FieldCurrency:
			result.Currency = deniedText(reason)
		case FieldPayBasis:
			result.PayBasis = deniedText(reason)
		case FieldFrequency:
			result.Frequency = deniedText(reason)
		}
	}
	result.Disclosure = people.DisclosureFull
	if denied > 0 {
		result.Disclosure = people.DisclosurePartial
	}
	return finishCompensationRead(result)
}

func removeCompensationField(fields []CompensationField, denied CompensationField) []CompensationField {
	out := make([]CompensationField, 0, len(fields)-1)
	for _, field := range fields {
		if field != denied {
			out = append(out, field)
		}
	}
	return out
}

// ReadAuthorizedCompensation is a descriptive alias for callers that prefer
// the capability name in their composition root.
func ReadAuthorizedCompensation(ctx context.Context, reader CompensationFacts, req CompensationReadRequest) (CompensationRead, error) {
	return ReadCompensation(ctx, reader, req)
}

type CompensationFieldExplanation struct {
	Field        CompensationField
	Access       people.Access
	DenialReason string
}

type CompensationExplanation struct {
	Worker         values.EntityRef
	Disclosure     people.Disclosure
	Presence       people.SubjectPresence
	WithheldReason string
	Fields         []CompensationFieldExplanation
	Effective      values.EffectiveInterval
	KnownAt        values.KnownAt
	Revision       values.RevisionToken
	Authority      evidence.SourceAuthority
	Provenance     evidence.Provenance
	Watermark      values.RevisionToken
	PolicyVersion  string
	Inputs         []string
}

// Explain returns disclosure shape and evidence coordinates only. In
// particular, it never renders base pay, component amounts or any other value.
func (r CompensationRead) Explain() CompensationExplanation {
	x := CompensationExplanation{
		Worker: r.Worker, Disclosure: r.Disclosure, Presence: r.Presence,
		WithheldReason: r.WithheldReason, Watermark: r.Watermark, PolicyVersion: r.PolicyVersion,
		Effective: r.Effective, KnownAt: r.KnownAt, Revision: r.Revision,
		Authority: r.Authority, Provenance: r.Provenance,
		Inputs: []string{"worker_ref", "as_of_instant", "fixed_field_mask", "authorization_scope", "effective_interval", "known_at", "source_authority"},
	}
	for _, field := range compensationFields {
		var access people.Access
		var reason string
		switch field {
		case FieldBasePay:
			access, reason = r.BasePay.Access, r.BasePay.DenialReason
		case FieldPayBandRef:
			access, reason = r.PayBandRef.Access, r.PayBandRef.DenialReason
		case FieldComponents:
			access, reason = r.Components.Access, r.Components.DenialReason
		case FieldCurrency:
			access, reason = r.Currency.Access, r.Currency.DenialReason
		case FieldPayBasis:
			access, reason = r.PayBasis.Access, r.PayBasis.DenialReason
		case FieldFrequency:
			access, reason = r.Frequency.Access, r.Frequency.DenialReason
		}
		if access != people.AccessUnspecified {
			x.Fields = append(x.Fields, CompensationFieldExplanation{Field: field, Access: access, DenialReason: reason})
		}
	}
	return x
}

func Explain(r CompensationRead) CompensationExplanation { return r.Explain() }
