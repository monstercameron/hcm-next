package simulate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// moneyScale and moneyRounding are the declared money contract of a simulated
// dataflow. They match the shared P1A domain corpus so a value that crosses
// from the interpreter into a domain calculation is not requantized on the way.
const (
	moneyScale                        = 2
	moneyRounding values.RoundingMode = values.RoundingHalfEven
	// decimalScale is the declared scale of a ratio or percentage carried
	// between nodes. It matches rules.IncreasePercentScale so the threshold
	// table compares against exactly the digits the simulation produced.
	decimalScale int32 = 4
)

// Value is one typed value flowing between nodes of a simulated plan.
//
// The payload is canonical text rather than an `any`, for two reasons. It
// keeps the receipt digest stable without a bespoke encoder per Go type, and
// it makes an implicit coercion impossible: a MONEY value carries its currency
// in its own text, so it cannot be silently read as a bare decimal by a node
// that declared a different type. There is no float anywhere, per the workflow
// context contract.
type Value struct {
	Type workflow.ValueType `json:"type"`
	Text string             `json:"text"`
}

// NewString returns an unbranded STRING value.
func NewString(s string) Value {
	return Value{Type: workflow.ValueType{Kind: workflow.KindString}, Text: s}
}

// NewBranded returns a branded STRING value, for example a WorkerID.
func NewBranded(brand, s string) Value {
	return Value{Type: workflow.ValueType{Kind: workflow.KindString, Brand: brand}, Text: s}
}

// NewBool returns a BOOL value.
func NewBool(b bool) Value {
	return Value{Type: workflow.ValueType{Kind: workflow.KindBool}, Text: strconv.FormatBool(b)}
}

// NewDecimal returns a DECIMAL value from an already-canonical decimal.
func NewDecimal(d values.Decimal) Value {
	return Value{Type: workflow.ValueType{Kind: workflow.KindDecimal}, Text: d.String()}
}

// NewMoney returns a MONEY value rendered as "<amount> <CURRENCY>".
func NewMoney(m values.Money) Value {
	return Value{Type: workflow.ValueType{Kind: workflow.KindMoney}, Text: m.String()}
}

// NewLocalDate returns a LOCAL_DATE value.
func NewLocalDate(d values.LocalDate) Value {
	return Value{Type: workflow.ValueType{Kind: workflow.KindLocalDate}, Text: d.String()}
}

// Nullable returns a copy of v marked nullable, so it may flow into a nullable
// target field.
func (v Value) Nullable() Value {
	v.Type.Nullable = true
	return v
}

// IsZero reports whether the value was never set.
func (v Value) IsZero() bool { return v.Type.Kind == "" && v.Text == "" }

// String renders the value for a diagnostic. It is never the digest input.
func (v Value) String() string { return v.Type.String() + "(" + v.Text + ")" }

// Money parses the value as money.
func (v Value) Money() (values.Money, error) {
	if v.Type.Kind != workflow.KindMoney {
		return values.Money{}, fmt.Errorf("%w: %s is not MONEY", ErrValueType, v.Type)
	}
	amount, currency, ok := strings.Cut(v.Text, " ")
	if !ok {
		return values.Money{}, fmt.Errorf("%w: money %q has no currency", ErrValueType, v.Text)
	}
	return values.NewMoney(amount, currency, moneyScale, moneyRounding)
}

// Decimal parses the value as a decimal at the declared simulation scale.
func (v Value) Decimal() (values.Decimal, error) {
	if v.Type.Kind != workflow.KindDecimal {
		return values.Decimal{}, fmt.Errorf("%w: %s is not DECIMAL", ErrValueType, v.Type)
	}
	return values.NewDecimal(v.Text, decimalScale, moneyRounding)
}

// LocalDate parses the value as a business date.
func (v Value) LocalDate() (values.LocalDate, error) {
	if v.Type.Kind != workflow.KindLocalDate {
		return values.LocalDate{}, fmt.Errorf("%w: %s is not LOCAL_DATE", ErrValueType, v.Type)
	}
	return values.ParseLocalDate(v.Text)
}

// Bool parses the value as a boolean.
func (v Value) Bool() (bool, error) {
	if v.Type.Kind != workflow.KindBool {
		return false, fmt.Errorf("%w: %s is not BOOL", ErrValueType, v.Type)
	}
	return strconv.ParseBool(v.Text)
}

// Bag is one node's typed input or output set, keyed by declared field path.
type Bag map[string]Value

// Get returns one field, or a typed refusal naming the node that wanted it. A
// missing field is never a zero value: a simulation that defaulted a missing
// input would produce a number nobody supplied.
func (b Bag) Get(path string) (Value, error) {
	v, ok := b[path]
	if !ok {
		return Value{}, fmt.Errorf("%w: %q", ErrMissingInput, path)
	}
	return v, nil
}

// Text returns one field's canonical text.
func (b Bag) Text(path string) (string, error) {
	v, err := b.Get(path)
	if err != nil {
		return "", err
	}
	return v.Text, nil
}

// Paths returns the field paths in sorted order, so anything derived from a
// bag is derived in one fixed order.
func (b Bag) Paths() []string {
	out := make([]string, 0, len(b))
	for path := range b {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// clone deep-copies a bag so a handler can never reach back into interpreter
// state through the map it was handed.
func (b Bag) clone() Bag {
	if b == nil {
		return nil
	}
	out := make(Bag, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// assignableTo reports whether every value in the bag may flow into the node's
// declared input fields. It is a runtime restatement of the compiler's own
// rule: the compiler proved the mappings type-check, and this proves the
// values the interpreter actually produced agree with them.
func (b Bag) assignableTo(fields []workflow.Field) error {
	declared := make(map[string]workflow.ValueType, len(fields))
	for _, f := range fields {
		declared[f.Path] = f.Type
	}
	for _, path := range b.Paths() {
		want, ok := declared[path]
		if !ok {
			return fmt.Errorf("%w: no declared field %q", ErrValueType, path)
		}
		if err := b[path].Type.AssignableTo(want); err != nil {
			return fmt.Errorf("%w: field %q: %w", ErrValueType, path, err)
		}
	}
	return nil
}
