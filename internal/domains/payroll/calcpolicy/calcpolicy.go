// Package calcpolicy owns the versioned exact-decimal contract for payroll,
// tax, deduction and rate calculations. It is pure: no persistence, clock,
// randomness, floating point, or provider authority is involved.
package calcpolicy

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidPolicy   = errors.New("calcpolicy: invalid policy")
	ErrInvalidInput    = errors.New("calcpolicy: invalid input")
	ErrInvalidOutput   = errors.New("calcpolicy: invalid output")
	ErrInvalidReceipt  = errors.New("calcpolicy: invalid receipt")
	ErrUnknownKind     = errors.New("calcpolicy: unknown calculation kind")
	ErrUnknownCurrency = errors.New("calcpolicy: currency is not permitted by policy")
	ErrAllocation      = errors.New("calcpolicy: allocation is invalid")
	ErrNegative        = errors.New("calcpolicy: negative value is not permitted")
	ErrZero            = errors.New("calcpolicy: zero value is not permitted")
)

// FieldError is a typed refusal. Field is always the contract field that
// caused the refusal; callers need not parse an error string.
type FieldError struct {
	Field string
	Cause error
}

func (e *FieldError) Error() string { return fmt.Sprintf("calcpolicy: field=%s: %v", e.Field, e.Cause) }
func (e *FieldError) Unwrap() error { return e.Cause }

func fieldError(field string, cause error) error { return &FieldError{Field: field, Cause: cause} }

// CalculationKind identifies the calculation family whose policy is applied.
type CalculationKind string

const (
	KindPayroll   CalculationKind = "PAYROLL"
	KindTax       CalculationKind = "TAX"
	KindDeduction CalculationKind = "DEDUCTION"
	KindRate      CalculationKind = "RATE"

	Payroll   = KindPayroll
	Tax       = KindTax
	Deduction = KindDeduction
	Rate      = KindRate
)

func (k CalculationKind) Valid() bool {
	switch k {
	case KindPayroll, KindTax, KindDeduction, KindRate:
		return true
	default:
		return false
	}
}

func (k CalculationKind) String() string { return string(k) }

// AllocationOrder declares how residual minor units are assigned.
type AllocationOrder string

const (
	AllocationLargestRemainder AllocationOrder = "LARGEST_REMAINDER"
	AllocationDeclared         AllocationOrder = "DECLARED"

	LargestRemainder = AllocationLargestRemainder
	Declared         = AllocationDeclared
)

func (o AllocationOrder) Valid() bool {
	return o == AllocationLargestRemainder || o == AllocationDeclared
}
func (o AllocationOrder) String() string { return string(o) }

// NegativeHandling is the policy decision for negative operands and results.
type NegativeHandling string

const (
	NegativeReject   NegativeHandling = "REJECT"
	NegativePermit   NegativeHandling = "PERMIT"
	NegativePreserve NegativeHandling = "PRESERVE"
)

func (h NegativeHandling) Valid() bool {
	return h == NegativeReject || h == NegativePermit || h == NegativePreserve
}

func (h NegativeHandling) String() string { return string(h) }

// ZeroHandling is the policy decision for zero operands and results.
type ZeroHandling string

const (
	ZeroAllow  ZeroHandling = "ALLOW"
	ZeroReject ZeroHandling = "REJECT"
)

func (h ZeroHandling) Valid() bool    { return h == ZeroAllow || h == ZeroReject }
func (h ZeroHandling) String() string { return string(h) }

// CurrencyRule binds a permitted currency to its output minor-unit scale.
// Scale is the scale at which the calculation result is quantized.
type CurrencyRule struct {
	Code  string
	Scale int32
}

func (r CurrencyRule) Validate() error {
	if len(r.Code) != 3 {
		return fieldError("currency.code", ErrUnknownCurrency)
	}
	for i := range r.Code {
		if r.Code[i] < 'A' || r.Code[i] > 'Z' {
			return fieldError("currency.code", ErrUnknownCurrency)
		}
	}
	if r.Scale < 0 || r.Scale > values.MaxScale {
		return fieldError("currency.scale", values.ErrScaleRange)
	}
	return nil
}

func (r CurrencyRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.CurrencyRule", schemaVersion).
		String("code", r.Code).Int("scale", int64(r.Scale)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CalculationRule contains every choice that can change a result.
type CalculationRule struct {
	Scale      int32
	Rounding   values.RoundingMode
	Allocation AllocationOrder
	Negative   NegativeHandling
	Zero       ZeroHandling
}

func (r CalculationRule) Validate() error {
	if r.Scale < 0 || r.Scale > values.MaxScale {
		return fieldError("rule.scale", values.ErrScaleRange)
	}
	if !r.Rounding.Valid() {
		return fieldError("rule.rounding", values.ErrRoundingMode)
	}
	if !r.Allocation.Valid() {
		return fieldError("rule.allocation", ErrAllocation)
	}
	if !r.Negative.Valid() {
		return fieldError("rule.negative", ErrNegative)
	}
	if !r.Zero.Valid() {
		return fieldError("rule.zero", ErrZero)
	}
	return nil
}

func (r CalculationRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.CalculationRule", schemaVersion).
		Int("scale", int64(r.Scale)).String("rounding", r.Rounding.String()).
		String("allocation", r.Allocation.String()).String("negative", r.Negative.String()).
		String("zero", r.Zero.String()).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Policy is one immutable, versioned calculation-policy revision. The maps
// are copied by NewPolicy and are treated as caller-owned after construction.
type Policy struct {
	ID                 string
	Version            string
	Revision           uint64
	Rules              map[CalculationKind]CalculationRule
	CurrencyRules      map[string]CurrencyRule
	SupersedesDigest   string
	SupersedesRevision uint64
	CanonicalDigest    string
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.Version) == "" {
		return fieldError("policy.version", ErrInvalidPolicy)
	}
	if p.Revision == 0 {
		return fieldError("policy.revision", ErrInvalidPolicy)
	}
	if len(p.Rules) != 4 {
		return fieldError("policy.rules", ErrInvalidPolicy)
	}
	for _, kind := range []CalculationKind{KindPayroll, KindTax, KindDeduction, KindRate} {
		rule, ok := p.Rules[kind]
		if !ok {
			return fieldError("policy.rules."+kind.String(), ErrUnknownKind)
		}
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	if len(p.CurrencyRules) == 0 {
		return fieldError("policy.currency_rules", ErrInvalidPolicy)
	}
	for code, rule := range p.CurrencyRules {
		if code != rule.Code {
			return fieldError("policy.currency_rules.code", ErrUnknownCurrency)
		}
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	if p.SupersedesRevision >= p.Revision {
		return fieldError("policy.supersedes_revision", ErrInvalidPolicy)
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fieldError("policy.canonical_digest", ErrInvalidPolicy)
	}
	return nil
}

func (p Policy) body() []byte {
	kinds := make([]string, 0, len(p.Rules))
	for kind := range p.Rules {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	currencies := make([]string, 0, len(p.CurrencyRules))
	for code := range p.CurrencyRules {
		currencies = append(currencies, code)
	}
	sort.Strings(currencies)
	w := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.Policy", schemaVersion).
		String("id", p.ID).String("version", p.Version).Int("revision", int64(p.Revision)).
		String("supersedes_digest", p.SupersedesDigest).Int("supersedes_revision", int64(p.SupersedesRevision)).
		Count("rules", len(kinds))
	for _, token := range kinds {
		kind := CalculationKind(token)
		w.String("rule_kind", token).Value("rule", p.Rules[kind])
	}
	w.Count("currencies", len(currencies))
	for _, code := range currencies {
		w.Value("currency", p.CurrencyRules[code])
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p Policy) computedDigest() string { return canonicalbytes.Digest(p.body()) }

// NewPolicy validates, detaches and digests a policy revision.
func NewPolicy(policy Policy) (Policy, error) {
	policy.Rules = cloneRules(policy.Rules)
	policy.CurrencyRules = cloneCurrencies(policy.CurrencyRules)
	policy.CanonicalDigest = ""
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	policy.CanonicalDigest = policy.computedDigest()
	return policy, nil
}

func cloneRules(in map[CalculationKind]CalculationRule) map[CalculationKind]CalculationRule {
	out := make(map[CalculationKind]CalculationRule, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneCurrencies(in map[string]CurrencyRule) map[string]CurrencyRule {
	out := make(map[string]CurrencyRule, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (p Policy) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

func (p Policy) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}

// NewVersion returns a successor policy and leaves p unchanged.
func (p Policy) NewVersion(version string) (Policy, error) {
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	next := p
	next.Version, next.Revision = version, p.Revision+1
	next.SupersedesDigest, next.SupersedesRevision = p.CanonicalDigest, p.Revision
	next.CanonicalDigest = ""
	return NewPolicy(next)
}

// Allocation gives one participant a non-negative weight. Key is an opaque
// deterministic tie-break token; it is never included in an Explanation.
type Allocation struct {
	Key    string
	Order  int
	Weight values.Decimal
}

func (a Allocation) Validate() error {
	if strings.TrimSpace(a.Key) == "" {
		return fieldError("input.allocations.key", ErrAllocation)
	}
	if a.Order < 0 {
		return fieldError("input.allocations.order", ErrAllocation)
	}
	if err := a.Weight.Validate(); err != nil {
		return fieldError("input.allocations.weight", err)
	}
	if a.Weight.Sign() < 0 {
		return fieldError("input.allocations.weight", ErrAllocation)
	}
	return nil
}

func (a Allocation) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.Allocation", schemaVersion).
		String("key", a.Key).Int("order", int64(a.Order)).Value("weight", a.Weight).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Input is a typed calculation request. Supply Amount for a direct calculation
// or Base and Rate for a rate calculation. Amount plus allocations is the only
// form that can produce an allocation trace.
type Input struct {
	Revision        uint64
	CalculationID   string
	Kind            CalculationKind
	Currency        string
	Amount          values.Decimal
	Base            values.Decimal
	Rate            values.Decimal
	Allocations     []Allocation
	CanonicalDigest string
}

func (in Input) direct() bool { return in.Amount.Validate() == nil }
func (in Input) rate() bool   { return in.Base.Validate() == nil || in.Rate.Validate() == nil }

func (in Input) Validate() error {
	if in.Revision == 0 {
		return fieldError("input.revision", ErrInvalidInput)
	}
	if !in.Kind.Valid() {
		return fieldError("input.kind", ErrUnknownKind)
	}
	if strings.TrimSpace(in.Currency) == "" {
		return fieldError("input.currency", ErrInvalidInput)
	}
	amountSet := in.Amount.Validate() == nil
	baseSet, rateSet := in.Base.Validate() == nil, in.Rate.Validate() == nil
	if amountSet && (baseSet || rateSet) {
		return fieldError("input.amount", ErrInvalidInput)
	}
	if !amountSet && !(baseSet && rateSet) {
		return fieldError("input.base_rate", ErrInvalidInput)
	}
	if in.Kind == KindRate && !(baseSet && rateSet) {
		return fieldError("input.base_rate", ErrInvalidInput)
	}
	if in.Kind != KindRate && !(amountSet || (baseSet && rateSet)) {
		return fieldError("input.amount", ErrInvalidInput)
	}
	if amountSet && len(in.Allocations) > 0 {
		seen := map[string]struct{}{}
		orders := map[int]struct{}{}
		for _, a := range in.Allocations {
			if err := a.Validate(); err != nil {
				return err
			}
			if _, ok := seen[a.Key]; ok {
				return fieldError("input.allocations.key", ErrAllocation)
			}
			if _, ok := orders[a.Order]; ok {
				return fieldError("input.allocations.order", ErrAllocation)
			}
			seen[a.Key], orders[a.Order] = struct{}{}, struct{}{}
		}
	}
	if in.CanonicalDigest != "" && in.CanonicalDigest != in.computedDigest() {
		return fieldError("input.canonical_digest", ErrInvalidInput)
	}
	return nil
}

func (in Input) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.Input", schemaVersion).
		Int("revision", int64(in.Revision)).String("calculation_id", in.CalculationID).
		String("kind", in.Kind.String()).String("currency", in.Currency).
		Bool("amount_present", in.Amount.Validate() == nil).Bool("base_present", in.Base.Validate() == nil).
		Bool("rate_present", in.Rate.Validate() == nil)
	if in.Amount.Validate() == nil {
		w.Value("amount", in.Amount)
	}
	if in.Base.Validate() == nil {
		w.Value("base", in.Base)
	}
	if in.Rate.Validate() == nil {
		w.Value("rate", in.Rate)
	}
	w.Count("allocations", len(in.Allocations))
	for _, a := range in.Allocations {
		w.Value("allocation", a)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (in Input) computedDigest() string { return canonicalbytes.Digest(in.body()) }

// NewInput validates and digests an immutable input revision.
func NewInput(in Input) (Input, error) {
	if in.Revision == 0 {
		in.Revision = 1
	}
	in.Allocations = append([]Allocation(nil), in.Allocations...)
	in.CanonicalDigest = ""
	if err := in.Validate(); err != nil {
		return Input{}, err
	}
	in.CanonicalDigest = in.computedDigest()
	return in, nil
}

func (in Input) Canonical() []byte {
	if in.Validate() != nil {
		return nil
	}
	return in.body()
}

func (in Input) Digest() (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	return in.computedDigest(), nil
}

// AllocationResult records the exact minor-unit amount assigned to one key.
type AllocationResult struct {
	Key    string
	Order  int
	Weight values.Decimal
	Amount values.Decimal
}

func (r AllocationResult) Validate() error {
	if strings.TrimSpace(r.Key) == "" {
		return fieldError("output.allocations.key", ErrInvalidOutput)
	}
	if r.Order < 0 {
		return fieldError("output.allocations.order", ErrInvalidOutput)
	}
	if err := r.Weight.Validate(); err != nil {
		return fieldError("output.allocations.weight", err)
	}
	if err := r.Amount.Validate(); err != nil {
		return fieldError("output.allocations.amount", err)
	}
	return nil
}

func (r AllocationResult) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.AllocationResult", schemaVersion).
		String("key", r.Key).Int("order", int64(r.Order)).Value("weight", r.Weight).Value("amount", r.Amount).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Output is the exact result at the policy's currency scale.
type Output struct {
	Amount      values.Decimal
	Currency    string
	Allocations []AllocationResult
}

func (o Output) Validate() error {
	if err := o.Amount.Validate(); err != nil {
		return fieldError("output.amount", err)
	}
	if strings.TrimSpace(o.Currency) == "" {
		return fieldError("output.currency", ErrInvalidOutput)
	}
	seen := map[string]struct{}{}
	for _, a := range o.Allocations {
		if err := a.Validate(); err != nil {
			return err
		}
		if _, ok := seen[a.Key]; ok {
			return fieldError("output.allocations.key", ErrInvalidOutput)
		}
		seen[a.Key] = struct{}{}
	}
	return nil
}

func (o Output) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.Output", schemaVersion).
		Value("amount", o.Amount).String("currency", o.Currency).Count("allocations", len(o.Allocations))
	for _, a := range o.Allocations {
		w.Value("allocation", a)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Receipt is an immutable calculation revision containing the typed input,
// exact output, policy reference and all digests needed for replay.
type Receipt struct {
	Revision        uint64
	PolicyVersion   string
	PolicyRevision  uint64
	PolicyDigest    string
	Input           Input
	Output          Output
	InputDigest     string
	OutputDigest    string
	CanonicalDigest string
}

func (r Receipt) Validate() error {
	if r.Revision == 0 {
		return fieldError("receipt.revision", ErrInvalidReceipt)
	}
	if strings.TrimSpace(r.PolicyVersion) == "" {
		return fieldError("receipt.policy_version", ErrInvalidReceipt)
	}
	if r.PolicyRevision == 0 || r.PolicyDigest == "" {
		return fieldError("receipt.policy", ErrInvalidReceipt)
	}
	if err := r.Input.Validate(); err != nil {
		return fieldError("receipt.input", err)
	}
	if err := r.Output.Validate(); err != nil {
		return fieldError("receipt.output", err)
	}
	if r.InputDigest != r.Input.computedDigest() {
		return fieldError("receipt.input_digest", ErrInvalidReceipt)
	}
	if r.OutputDigest != canonicalbytes.Digest(r.Output.Canonical()) {
		return fieldError("receipt.output_digest", ErrInvalidReceipt)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fieldError("receipt.canonical_digest", ErrInvalidReceipt)
	}
	return nil
}

func (r Receipt) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.payroll.calcpolicy.Receipt", schemaVersion).
		Int("revision", int64(r.Revision)).String("policy_version", r.PolicyVersion).
		Int("policy_revision", int64(r.PolicyRevision)).String("policy_digest", r.PolicyDigest).
		Value("input", r.Input).Value("output", r.Output).
		String("input_digest", r.InputDigest).String("output_digest", r.OutputDigest)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r Receipt) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r Receipt) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r Receipt) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// Explanation is deliberately bounded and contains no calculation IDs,
// allocation keys, account numbers, secrets, or raw input values.
type Explanation struct {
	PolicyVersion   string
	PolicyRevision  uint64
	InputDigest     string
	OutputDigest    string
	ReceiptDigest   string
	Currency        string
	Scale           int32
	Rounding        values.RoundingMode
	Allocation      AllocationOrder
	AllocationCount int
}

// Explain returns an audit-safe summary of a receipt.
func (r Receipt) Explain() (Explanation, error) {
	if err := r.Validate(); err != nil {
		return Explanation{}, err
	}
	if len(r.Output.Allocations) == 0 {
		return Explanation{PolicyVersion: r.PolicyVersion, PolicyRevision: r.PolicyRevision, InputDigest: r.InputDigest, OutputDigest: r.OutputDigest, ReceiptDigest: r.CanonicalDigest, Currency: r.Output.Currency, Scale: r.Output.Amount.Scale(), Rounding: r.Output.Amount.Rounding()}, nil
	}
	return Explanation{PolicyVersion: r.PolicyVersion, PolicyRevision: r.PolicyRevision, InputDigest: r.InputDigest, OutputDigest: r.OutputDigest, ReceiptDigest: r.CanonicalDigest, Currency: r.Output.Currency, Scale: r.Output.Amount.Scale(), Rounding: r.Output.Amount.Rounding(), Allocation: AllocationLargestRemainder, AllocationCount: len(r.Output.Allocations)}, nil
}

// Explain is the package-level spelling for the receipt contract.
func Explain(r Receipt) (Explanation, error) { return r.Explain() }

func ruleFor(p Policy, kind CalculationKind) (CalculationRule, error) {
	rule, ok := p.Rules[kind]
	if !ok {
		return CalculationRule{}, fieldError("input.kind", ErrUnknownKind)
	}
	return rule, nil
}

// Calculate applies exactly one validated policy revision to one typed input.
func Calculate(policy Policy, input Input) (Receipt, error) {
	if err := policy.Validate(); err != nil {
		return Receipt{}, err
	}
	normalized, err := NewInput(input)
	if err != nil {
		return Receipt{}, err
	}
	rule, err := ruleFor(policy, normalized.Kind)
	if err != nil {
		return Receipt{}, err
	}
	currency, ok := policy.CurrencyRules[normalized.Currency]
	if !ok {
		return Receipt{}, fieldError("input.currency", ErrUnknownCurrency)
	}
	if currency.Scale != rule.Scale {
		return Receipt{}, fieldError("policy.scale", fmt.Errorf("currency %s scale=%d and rule scale=%d disagree", currency.Code, currency.Scale, rule.Scale))
	}
	if normalized.Amount.Validate() == nil {
		if err := checkSign("input.amount", normalized.Amount, rule); err != nil {
			return Receipt{}, err
		}
	} else {
		if err := checkSign("input.base", normalized.Base, rule); err != nil {
			return Receipt{}, err
		}
		if err := checkSign("input.rate", normalized.Rate, rule); err != nil {
			return Receipt{}, err
		}
	}
	amount, err := calculateAmount(normalized, rule)
	if err != nil {
		return Receipt{}, err
	}
	if err := checkSign("output.amount", amount, rule); err != nil {
		return Receipt{}, err
	}
	if amount.IsZero() && rule.Zero == ZeroReject {
		return Receipt{}, fieldError("output.amount", ErrZero)
	}
	output := Output{Amount: amount, Currency: currency.Code}
	if len(normalized.Allocations) > 0 {
		if normalized.Amount.Validate() != nil {
			return Receipt{}, fieldError("input.allocations", ErrAllocation)
		}
		output.Allocations, err = allocate(amount, normalized.Allocations, rule.Allocation, rule.Rounding)
		if err != nil {
			return Receipt{}, err
		}
	}
	if err := output.Validate(); err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{Revision: 1, PolicyVersion: policy.Version, PolicyRevision: policy.Revision, PolicyDigest: policy.CanonicalDigest, Input: normalized, Output: output, InputDigest: normalized.computedDigest(), OutputDigest: canonicalbytes.Digest(output.Canonical())}
	receipt.CanonicalDigest = receipt.computedDigest()
	return receipt, nil
}

func checkSign(field string, d values.Decimal, rule CalculationRule) error {
	if d.Sign() < 0 && rule.Negative == NegativeReject {
		return fieldError(field, ErrNegative)
	}
	if d.IsZero() && rule.Zero == ZeroReject {
		return fieldError(field, ErrZero)
	}
	return nil
}

func calculateAmount(in Input, rule CalculationRule) (values.Decimal, error) {
	if in.Amount.Validate() == nil {
		out, err := in.Amount.Quantize(rule.Scale, rule.Rounding)
		if err != nil {
			return values.Decimal{}, fieldError("input.amount", err)
		}
		return out, nil
	}
	out, err := in.Base.Mul(in.Rate, rule.Scale, rule.Rounding)
	if err != nil {
		return values.Decimal{}, fieldError("input.base_rate", err)
	}
	return out, nil
}

type allocationCandidate struct {
	input     Allocation
	quotient  *big.Int
	remainder *big.Int
	amount    *big.Int
}

func allocate(total values.Decimal, inputs []Allocation, order AllocationOrder, mode values.RoundingMode) ([]AllocationResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	weightScale := int32(0)
	for _, a := range inputs {
		if a.Weight.Scale() > weightScale {
			weightScale = a.Weight.Scale()
		}
	}
	weights := make([]*big.Int, len(inputs))
	sum := new(big.Int)
	for i, a := range inputs {
		weights[i] = a.Weight.Unscaled()
		if scale := weightScale - a.Weight.Scale(); scale > 0 {
			weights[i].Mul(weights[i], pow10(scale))
		}
		sum.Add(sum, weights[i])
	}
	if sum.Sign() == 0 {
		return nil, fieldError("input.allocations.weight", ErrAllocation)
	}
	minor := total.Unscaled()
	denominator := new(big.Int).Set(sum)
	candidates := make([]allocationCandidate, len(inputs))
	base := new(big.Int)
	for i, in := range inputs {
		numerator := new(big.Int).Mul(minor, weights[i])
		quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
		candidates[i] = allocationCandidate{input: in, quotient: quotient, remainder: remainder, amount: new(big.Int).Set(quotient)}
		base.Add(base, quotient)
	}
	residual := new(big.Int).Sub(minor, base)
	if residual.Sign() < 0 || !residual.IsInt64() || residual.Int64() > int64(len(candidates)) {
		return nil, fieldError("input.allocations", ErrAllocation)
	}
	if order == AllocationLargestRemainder {
		sort.SliceStable(candidates, func(i, j int) bool {
			if c := candidates[i].remainder.Cmp(candidates[j].remainder); c != 0 {
				return c > 0
			}
			if candidates[i].input.Key != candidates[j].input.Key {
				return candidates[i].input.Key < candidates[j].input.Key
			}
			return candidates[i].input.Order < candidates[j].input.Order
		})
	} else {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].input.Order != candidates[j].input.Order {
				return candidates[i].input.Order < candidates[j].input.Order
			}
			return candidates[i].input.Key < candidates[j].input.Key
		})
	}
	for i := int64(0); i < residual.Int64(); i++ {
		candidates[i].amount.Add(candidates[i].amount, big.NewInt(1))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].input.Order != candidates[j].input.Order {
			return candidates[i].input.Order < candidates[j].input.Order
		}
		return candidates[i].input.Key < candidates[j].input.Key
	})
	result := make([]AllocationResult, 0, len(candidates))
	var sumAmount values.Decimal
	for i, c := range candidates {
		if total.Sign() < 0 {
			c.amount.Neg(c.amount)
		}
		amount, err := decimalFromMinor(c.amount, total.Scale(), mode)
		if err != nil {
			return nil, fieldError("output.allocations.amount", err)
		}
		result = append(result, AllocationResult{Key: c.input.Key, Order: c.input.Order, Weight: c.input.Weight, Amount: amount})
		if i == 0 {
			sumAmount = amount
		} else {
			sumAmount, err = sumAmount.Add(amount)
			if err != nil {
				return nil, fieldError("output.allocations.amount", err)
			}
		}
	}
	if !sumAmount.Equal(total) {
		return nil, fieldError("output.allocations.amount", ErrAllocation)
	}
	return result, nil
}

func decimalFromMinor(unscaled *big.Int, scale int32, mode values.RoundingMode) (values.Decimal, error) {
	text := unscaled.String()
	if scale > 0 {
		negative := strings.HasPrefix(text, "-")
		if negative {
			text = strings.TrimPrefix(text, "-")
		}
		if int32(len(text)) <= scale {
			text = strings.Repeat("0", int(scale)-len(text)+1) + text
		}
		cut := len(text) - int(scale)
		text = text[:cut] + "." + text[cut:]
		if negative {
			text = "-" + text
		}
	}
	return values.NewDecimal(text, scale, mode)
}

func pow10(n int32) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
