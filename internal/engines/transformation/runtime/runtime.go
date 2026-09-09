// Package runtime executes declarative transformation definitions without
// ambient state, code evaluation, persistence, or external effects.
package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrRejected = errors.New("XFORM_003_REJECTED")
	ErrLimit    = errors.New("transformation: resource limit exceeded")
	ErrType     = errors.New("transformation: value type mismatch")
)

var decimalRE = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)

// Value is a typed property. Non-VALUE states intentionally contain no value.
type Value struct {
	Type   transformation.Type
	State  values.PresenceState
	Data   any
	Reason string
}

func Present(t transformation.Type, data any) Value {
	return Value{Type: t, State: values.PresenceValue, Data: data}
}
func State(t transformation.Type, state values.PresenceState, reason string) Value {
	return Value{Type: t, State: state, Reason: reason}
}

type Record map[string]Value

// Rejection is stable, machine-readable failure metadata. Version is the
// transformation contract version, never an incidental process version.
type Rejection struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	State   string `json:"state"`
	Version int    `json:"version"`
	Reason  string `json:"reason,omitempty"`
}

func (r Rejection) Error() string {
	return fmt.Sprintf("%s field=%s state=%s version=%d: %s", r.Code, r.Field, r.State, r.Version, r.Reason)
}
func (r Rejection) Unwrap() error { return ErrRejected }

// Execute validates and executes a definition. Input and output are encoded
// canonically before limits are checked, making the boundary deterministic.
func Execute(def transformation.TransformationDefinition, input Record) (Record, error) {
	if err := def.Validate(); err != nil {
		return nil, reject(def, "", values.PresenceUnspecified, err)
	}
	inBytes, err := canonicalRecord(input)
	if err != nil {
		return nil, reject(def, "", values.PresenceUnspecified, err)
	}
	if int64(len(inBytes)) > def.Limits.MaxInputBytes {
		return nil, fmt.Errorf("%w: input", ErrLimit)
	}
	out := make(Record, len(def.Destination.Fields))
	for _, op := range def.Operations {
		v, field, err := evaluate(def, op, input)
		if err != nil {
			if def.Failure == transformation.FailureSkip {
				continue
			}
			return nil, err
		}
		if v.Type != field.Type {
			return nil, reject(def, field.Name, v.State, ErrType)
		}
		out[field.Name] = v
	}
	// Every destination field is represented. This prevents omission from
	// becoming an accidental NULL or zero value.
	for _, f := range def.Destination.Fields {
		if _, ok := out[f.Name]; !ok {
			out[f.Name] = State(f.Type, values.PresenceAbsent, "")
		}
	}
	outBytes, err := canonicalRecord(out)
	if err != nil {
		return nil, reject(def, "", values.PresenceUnspecified, err)
	}
	if int64(len(outBytes)) > def.Limits.MaxOutputBytes {
		return nil, fmt.Errorf("%w: output", ErrLimit)
	}
	return out, nil
}

func evaluate(def transformation.TransformationDefinition, op transformation.Operation, in Record) (Value, transformation.Field, error) {
	field := destinationField(def, op.Destination.Field)
	if field.Name == "" {
		return Value{}, field, reject(def, op.Destination.Field, values.PresenceUnspecified, errors.New("destination field is not declared"))
	}
	get := func(p transformation.Path) (Value, error) {
		v, ok := in[p.Field]
		if !ok {
			return State(p.Type, values.PresenceAbsent, ""), nil
		}
		if v.Type != p.Type {
			return Value{}, fmt.Errorf("%w: %s", ErrType, p.Field)
		}
		if err := v.validate(); err != nil {
			return Value{}, err
		}
		return v, nil
	}
	switch op.Kind {
	case transformation.OpDefault:
		v, _ := get(op.Destination)
		if v.State != values.PresenceAbsent {
			return v, field, nil
		}
		data, err := convert(op.Literal, transformation.TypeString, field.Type)
		if err != nil {
			return Value{}, field, reject(def, field.Name, v.State, err)
		}
		return Present(field.Type, data), field, nil
	case transformation.OpCopy, transformation.OpRename:
		v, err := get(*op.Source)
		if err != nil {
			return Value{}, field, reject(def, op.Source.Field, values.PresenceUnspecified, err)
		}
		if v.State != values.PresenceValue {
			v.Type = field.Type
			return v, field, nil
		}
		if op.Kind == transformation.OpCopy && v.Type != field.Type {
			return Value{}, field, reject(def, op.Source.Field, v.State, ErrType)
		}
		return Present(field.Type, v.Data), field, nil
	case transformation.OpConvert:
		v, err := get(*op.Source)
		if err != nil {
			return Value{}, field, reject(def, op.Source.Field, values.PresenceUnspecified, err)
		}
		if v.State != values.PresenceValue {
			v.Type = field.Type
			return v, field, nil
		}
		data, err := convert(v.Data, v.Type, op.TargetType)
		if err != nil {
			return Value{}, field, reject(def, op.Source.Field, v.State, err)
		}
		return Present(field.Type, data), field, nil
	case transformation.OpConcat:
		parts := make([]string, 0, len(op.Sources))
		for _, p := range op.Sources {
			v, err := get(p)
			if err != nil {
				return Value{}, field, reject(def, p.Field, values.PresenceUnspecified, err)
			}
			if v.State != values.PresenceValue {
				v.Type = field.Type
				return v, field, nil
			}
			s, ok := v.Data.(string)
			if !ok {
				return Value{}, field, reject(def, p.Field, v.State, ErrType)
			}
			parts = append(parts, s)
		}
		return Present(field.Type, strings.Join(parts, "")), field, nil
	}
	return Value{}, field, reject(def, "", values.PresenceUnspecified, errors.New("unknown operation"))
}

func (v Value) validate() error {
	if !v.State.Valid() {
		return errors.New("invalid presence state")
	}
	if v.State != values.PresenceValue && v.Data != nil {
		return errors.New("non-VALUE carries data")
	}
	return nil
}
func destinationField(d transformation.TransformationDefinition, name string) transformation.Field {
	for _, f := range d.Destination.Fields {
		if f.Name == name {
			return f
		}
	}
	return transformation.Field{}
}
func reject(d transformation.TransformationDefinition, field string, state values.PresenceState, err error) error {
	return Rejection{Code: "XFORM_003_REJECTED", Field: field, State: state.String(), Version: d.Version, Reason: err.Error()}
}

func convert(data any, from, to transformation.Type) (any, error) {
	if from == to {
		return data, nil
	}
	s := fmt.Sprint(data)
	switch to {
	case transformation.TypeString:
		return s, nil
	case transformation.TypeInt:
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, fmt.Errorf("invalid int %q", s)
		}
		if !n.IsInt64() {
			return nil, fmt.Errorf("int out of range %q", s)
		}
		return n.Int64(), nil
	case transformation.TypeDecimal:
		if !decimalRE.MatchString(s) {
			return nil, fmt.Errorf("invalid decimal %q", s)
		}
		return s, nil
	case transformation.TypeDate:
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, err
		}
		return s, nil
	case transformation.TypeTimestamp:
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, err
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case transformation.TypeBool:
		b, err := strconv.ParseBool(s)
		return b, err
	default:
		return nil, ErrType
	}
}

func canonicalRecord(r Record) ([]byte, error) {
	// JSON object key ordering is deterministic in encoding/json; normalize
	// values to a compact wire shape and reject malformed states first.
	type wire struct {
		Type   transformation.Type `json:"type"`
		State  string              `json:"state"`
		Data   any                 `json:"data,omitempty"`
		Reason string              `json:"reason,omitempty"`
	}
	m := make(map[string]wire, len(r))
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := r[k]
		if err := v.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		m[k] = wire{v.Type, v.State.String(), v.Data, v.Reason}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, b); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}
