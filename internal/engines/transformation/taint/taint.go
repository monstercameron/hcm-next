// Package taint executes transformation programs while carrying the semantic
// envelope that values alone cannot express: presence, classification,
// provenance and taint. The package is deliberately a pure companion to
// transformation/exec; it has no storage, clock, network or user callbacks.
package taint

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/exec"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidMetadata reports an envelope that is not safe to propagate.
	ErrInvalidMetadata = errors.New("transformation/taint: invalid metadata")
	// ErrMissingSource reports an operation whose source metadata was absent
	// from the record and could not be represented as an explicit ABSENT field.
	ErrMissingSource = errors.New("transformation/taint: missing source metadata")
	// ErrUnknownOperation reports an opcode for which no propagation rule is
	// declared.
	ErrUnknownOperation = errors.New("transformation/taint: unknown operation")
	// ErrRefused is the sentinel every Refusal unwraps to.
	ErrRefused = errors.New("XFORM_004_REFUSED")
)

// PresenceState is the deliberately small transformation presence lattice.
// PRESENT is readable data, ABSENT was not supplied, and UNKNOWN exists but
// cannot be established by this execution.
type PresenceState string

const (
	PresencePresent PresenceState = "PRESENT"
	PresenceAbsent  PresenceState = "ABSENT"
	PresenceUnknown PresenceState = "UNKNOWN"

	// Short aliases keep the wire vocabulary visible to callers.
	Present PresenceState = PresencePresent
	Absent  PresenceState = PresenceAbsent
	Unknown PresenceState = PresenceUnknown
)

func (p PresenceState) Valid() bool {
	return p == PresencePresent || p == PresenceAbsent || p == PresenceUnknown
}

// Classification is ordered from least to most sensitive. Propagation always
// uses the maximum classification of its inputs.
type Classification string

const (
	ClassificationPublic       Classification = "PUBLIC"
	ClassificationInternal     Classification = "INTERNAL"
	ClassificationConfidential Classification = "CONFIDENTIAL"
	ClassificationRestricted   Classification = "RESTRICTED"
	ClassificationSecret       Classification = "SECRET"
)

var classificationRank = map[Classification]int{
	ClassificationPublic:       0,
	ClassificationInternal:     1,
	ClassificationConfidential: 2,
	ClassificationRestricted:   3,
	ClassificationSecret:       4,
}

func (c Classification) Valid() bool { _, ok := classificationRank[c]; return ok }

// Metadata is the non-value envelope carried by each field. Provenance and
// Taint are sets represented in canonical sorted order. A provenance ref is
// required even for an ABSENT field; the executor uses source:<schema.field>
// for an absent source that was not present in the input record.
type Metadata struct {
	Presence       PresenceState  `json:"presence"`
	Classification Classification `json:"classification"`
	Provenance     []string       `json:"provenance"`
	Taint          []string       `json:"taint"`
	Reason         string         `json:"reason,omitempty"`
}

// Validate checks all semantic envelope invariants.
func (m Metadata) Validate() error {
	if !m.Presence.Valid() {
		return fmt.Errorf("%w: presence %q", ErrInvalidMetadata, m.Presence)
	}
	if !m.Classification.Valid() {
		return fmt.Errorf("%w: classification %q", ErrInvalidMetadata, m.Classification)
	}
	if len(m.Provenance) == 0 {
		return fmt.Errorf("%w: at least one provenance ref is required", ErrInvalidMetadata)
	}
	if err := validateSet("provenance", m.Provenance); err != nil {
		return err
	}
	return validateSet("taint", m.Taint)
}

func validateSet(name string, items []string) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("%w: %s contains an empty ref", ErrInvalidMetadata, name)
		}
		if _, ok := seen[item]; ok {
			return fmt.Errorf("%w: %s contains duplicate %q", ErrInvalidMetadata, name, item)
		}
		seen[item] = struct{}{}
	}
	return nil
}

// Field is a typed value plus its semantic envelope.
type Field struct {
	Type           transformation.Type `json:"type"`
	Data           any                 `json:"data,omitempty"`
	Presence       PresenceState       `json:"presence"`
	Classification Classification      `json:"classification"`
	Provenance     []string            `json:"provenance"`
	Taint          []string            `json:"taint"`
	Reason         string              `json:"reason,omitempty"`
}

// Metadata returns a defensive copy of a field's envelope.
func (f Field) Metadata() Metadata {
	return Metadata{Presence: f.Presence, Classification: f.Classification,
		Provenance: append([]string(nil), f.Provenance...), Taint: append([]string(nil), f.Taint...), Reason: f.Reason}
}

// Validate checks the field type, presence/data invariant and envelope.
func (f Field) Validate() error {
	if f.Type == "" {
		return fmt.Errorf("%w: field type is required", ErrInvalidMetadata)
	}
	if err := f.Metadata().Validate(); err != nil {
		return err
	}
	if f.Presence == PresencePresent && f.Data == nil {
		return fmt.Errorf("%w: PRESENT field has no data", ErrInvalidMetadata)
	}
	if f.Presence != PresencePresent && f.Data != nil {
		return fmt.Errorf("%w: non-PRESENT field carries data", ErrInvalidMetadata)
	}
	return nil
}

// Record is a row keyed by the same schema.field spelling used by exec.
type Record map[string]Field

// PropagationRule describes the declared metadata behavior of an IR opcode.
// The strings are stable review/audit vocabulary, not parsing contracts.
type PropagationRule struct {
	Operation      ir.OpCode `json:"operation"`
	Presence       string    `json:"presence"`
	Classification string    `json:"classification"`
	Provenance     string    `json:"provenance"`
	Taint          string    `json:"taint"`
}

// RuleFor returns the closed propagation rule for an IR opcode.
func RuleFor(op ir.OpCode) (PropagationRule, error) {
	switch op {
	case ir.OpMap:
		return PropagationRule{op, "source state", "keep source", "keep and operation lineage", "union"}, nil
	case ir.OpFilter:
		return PropagationRule{op, "filtered PRESENT becomes ABSENT; UNKNOWN stays UNKNOWN", "keep source", "keep and operation lineage", "union"}, nil
	case ir.OpProject:
		return PropagationRule{op, "keep source", "keep source", "keep and operation lineage", "union"}, nil
	case ir.OpJoinByKey:
		return PropagationRule{op, "join inputs", "maximum", "union and operation lineage", "union"}, nil
	case ir.OpAggregate:
		return PropagationRule{op, "join inputs", "maximum", "union and operation lineage", "union"}, nil
	case ir.OpCoerce:
		return PropagationRule{op, "keep source", "keep source", "keep and operation lineage", "keep"}, nil
	default:
		return PropagationRule{}, fmt.Errorf("%w: %q", ErrUnknownOperation, op)
	}
}

// Propagate applies an opcode's metadata rule to source envelopes. It is
// useful to callers that execute their own values; Execute below uses the same
// function beside the canonical exec interpreter.
func Propagate(op ir.OpCode, sources []Metadata) (Metadata, error) {
	if _, err := RuleFor(op); err != nil {
		return Metadata{}, err
	}
	if len(sources) == 0 {
		return Metadata{}, ErrMissingSource
	}
	result := Metadata{Presence: PresencePresent, Classification: ClassificationPublic}
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return Metadata{}, err
		}
		if classificationRank[source.Classification] > classificationRank[result.Classification] {
			result.Classification = source.Classification
		}
		result.Provenance = append(result.Provenance, source.Provenance...)
		result.Taint = append(result.Taint, source.Taint...)
		result.Presence = joinPresence(result.Presence, source.Presence)
	}
	return canonical(result), nil
}

func joinPresence(a, b PresenceState) PresenceState {
	if a == PresenceUnknown || b == PresenceUnknown {
		return PresenceUnknown
	}
	if a == PresencePresent || b == PresencePresent {
		return PresencePresent
	}
	return PresenceAbsent
}

func canonical(m Metadata) Metadata {
	m.Provenance = uniqueSorted(m.Provenance)
	m.Taint = uniqueSorted(m.Taint)
	return m
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	result := out[:0]
	for _, item := range out {
		if len(result) == 0 || result[len(result)-1] != item {
			result = append(result, item)
		}
	}
	return result
}

func withOperation(m Metadata, op ir.Instruction) Metadata {
	m.Provenance = append(m.Provenance, "operation:"+string(op.Op)+":"+op.Destination.Schema+"."+op.Destination.Field)
	return canonical(m)
}

// CheckOutput verifies that an output has not downgraded classification or
// discarded any source provenance/taint. A caller can use it to guard a
// custom operation implementation; Execute invokes it for every instruction.
func CheckOutput(op ir.OpCode, sources []Metadata, output Metadata) error {
	if _, err := RuleFor(op); err != nil {
		return err
	}
	if err := output.Validate(); err != nil {
		return refusal("output", "", err)
	}
	if len(sources) == 0 {
		if op == ir.OpMap {
			return nil
		}
		return refusal("output", "", ErrMissingSource)
	}
	want, err := Propagate(op, sources)
	if err != nil {
		return refusal("output", "", err)
	}
	if classificationRank[output.Classification] < classificationRank[want.Classification] {
		return refusal("output", "", fmt.Errorf("classification %s loses input classification %s", output.Classification, want.Classification))
	}
	if missing := setDifference(want.Provenance, output.Provenance); len(missing) > 0 {
		return refusal("output", "", fmt.Errorf("provenance lost: %s", strings.Join(missing, ", ")))
	}
	if missing := setDifference(want.Taint, output.Taint); len(missing) > 0 {
		return refusal("output", "", fmt.Errorf("taint lost: %s", strings.Join(missing, ", ")))
	}
	return nil
}

func setDifference(want, got []string) []string {
	set := make(map[string]struct{}, len(got))
	for _, item := range got {
		set[item] = struct{}{}
	}
	var missing []string
	for _, item := range want {
		if _, ok := set[item]; !ok {
			missing = append(missing, item)
		}
	}
	return missing
}

// Refusal is a typed, stable refusal for metadata loss. It names the row,
// instruction and output field whenever execution is the source of the error.
type Refusal struct {
	Code   string `json:"code"`
	Step   string `json:"step"`
	Field  string `json:"field,omitempty"`
	Reason string `json:"reason"`
}

func (r Refusal) Error() string {
	return fmt.Sprintf("%s step=%q field=%q: %s", r.Code, r.Step, r.Field, r.Reason)
}
func (r Refusal) Unwrap() error { return ErrRefused }

func refusal(step, field string, err error) error {
	return Refusal{Code: "XFORM_004_REFUSED", Step: step, Field: field, Reason: err.Error()}
}

// Limits is an alias of exec.Limits so both interpreters use identical
// resource boundaries.
type Limits = exec.Limits

// Interpreter executes values through exec and metadata through this package
// in lockstep. It contains no mutable execution state and is safe to reuse.
type Interpreter struct {
	inner   *exec.Interpreter
	program ir.Program
	order   []int
}

// New validates a program and prepares both execution orders.
func New(program ir.Program) (*Interpreter, error) {
	inner, err := exec.New(program)
	if err != nil {
		return nil, err
	}
	order, err := metadataOrder(program)
	if err != nil {
		return nil, err
	}
	return &Interpreter{inner: inner, program: program, order: order}, nil
}

// Program returns the compiled program used by the interpreter.
func (in *Interpreter) Program() ir.Program { return in.program }

// Execute runs values through exec and applies the metadata rules to every
// output. A metadata refusal returns no partial output.
func Execute(program ir.Program, dataset []Record, limits Limits) ([]Record, error) {
	in, err := New(program)
	if err != nil {
		return nil, err
	}
	return in.Execute(dataset, limits)
}

// Execute runs the paired value and metadata interpreters.
func (in *Interpreter) Execute(dataset []Record, limits Limits) ([]Record, error) {
	valuesIn := make([]exec.Record, len(dataset))
	for i, row := range dataset {
		converted, err := toExecRecord(row)
		if err != nil {
			return nil, refusal(fmt.Sprintf("row %d input", i), "", err)
		}
		valuesIn[i] = converted
	}
	valuesOut, err := in.inner.Execute(valuesIn, limits)
	if err != nil {
		return nil, err
	}
	out := make([]Record, len(dataset))
	for i, row := range dataset {
		metadata, err := in.metadataRow(row, fmt.Sprintf("row %d", i))
		if err != nil {
			return nil, err
		}
		result := make(Record, len(valuesOut[i]))
		for key, value := range valuesOut[i] {
			field, ok := metadata[key]
			if !ok {
				return nil, refusal(fmt.Sprintf("row %d", i), key, ErrMissingSource)
			}
			field.Type = value.Type
			if field.Presence == PresencePresent && value.State.String() != "VALUE" {
				return nil, refusal(fmt.Sprintf("row %d", i), key, errors.New("metadata says PRESENT but exec produced no value"))
			}
			if field.Presence == PresencePresent {
				field.Data = value.Data
			} else {
				field.Data = nil
			}
			result[key] = field
		}
		out[i] = result
	}
	return out, nil
}

func toExecRecord(row Record) (exec.Record, error) {
	result := make(exec.Record, len(row))
	for key, field := range row {
		if err := field.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		state, err := execPresence(field.Presence)
		if err != nil {
			return nil, err
		}
		if state == values.PresenceValue {
			result[key] = exec.Present(field.Type, field.Data)
		} else {
			result[key] = exec.NonValue(field.Type, state, field.Reason)
		}
	}
	return result, nil
}

func execPresence(state PresenceState) (values.PresenceState, error) {
	switch state {
	case PresencePresent:
		return values.PresenceValue, nil
	case PresenceAbsent:
		return values.PresenceAbsent, nil
	case PresenceUnknown:
		return values.PresenceUnknown, nil
	default:
		return values.PresenceUnspecified, fmt.Errorf("%w: presence %q", ErrInvalidMetadata, state)
	}
}

func pathKey(path transformation.Path) string { return path.Schema + "." + path.Field }

func metadataOrder(program ir.Program) ([]int, error) {
	destOf := make(map[string]int, len(program.Instructions))
	for i, instruction := range program.Instructions {
		destOf[pathKey(instruction.Destination)] = i
	}
	color := make([]uint8, len(program.Instructions))
	order := make([]int, 0, len(program.Instructions))
	var visit func(int) error
	visit = func(index int) error {
		if color[index] == 1 {
			return fmt.Errorf("%w: metadata dependency cycle", ErrRefused)
		}
		if color[index] == 2 {
			return nil
		}
		color[index] = 1
		for _, source := range program.Instructions[index].Sources {
			if dependency, ok := destOf[pathKey(source)]; ok && dependency != index {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		color[index] = 2
		order = append(order, index)
		return nil
	}
	for index := range program.Instructions {
		if err := visit(index); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (in *Interpreter) metadataRow(input Record, rowLabel string) (Record, error) {
	work := make(Record, len(input)+len(in.program.Instructions))
	for key, field := range input {
		if err := field.Validate(); err != nil {
			return nil, refusal(rowLabel+" input", key, err)
		}
		work[key] = cloneField(field)
	}
	for _, index := range in.order {
		instruction := in.program.Instructions[index]
		step := fmt.Sprintf("%s instruction %d", rowLabel, index)
		sources := make([]Metadata, len(instruction.Sources))
		for i, source := range instruction.Sources {
			field, ok := work[pathKey(source)]
			if !ok {
				field = Field{Type: source.Type, Presence: PresenceAbsent,
					Classification: ClassificationPublic, Provenance: []string{"source:" + pathKey(source)}}
			}
			if field.Type != source.Type {
				return nil, refusal(step, pathKey(instruction.Destination), fmt.Errorf("source type mismatch: %s", pathKey(source)))
			}
			if err := field.Validate(); err != nil {
				return nil, refusal(step, pathKey(source), err)
			}
			sources[i] = field.Metadata()
		}
		metadata, err := metadataForInstruction(instruction, sources, work)
		if err != nil {
			return nil, refusal(step, pathKey(instruction.Destination), err)
		}
		metadata = withOperation(metadata, instruction)
		if err := CheckOutput(instruction.Op, sources, metadata); err != nil {
			return nil, refusal(step, pathKey(instruction.Destination), err)
		}
		work[pathKey(instruction.Destination)] = Field{Type: instruction.Destination.Type,
			Presence: metadata.Presence, Classification: metadata.Classification,
			Provenance: metadata.Provenance, Taint: metadata.Taint, Reason: metadata.Reason}
	}
	result := make(Record, len(in.program.Instructions))
	for _, instruction := range in.program.Instructions {
		result[pathKey(instruction.Destination)] = cloneField(work[pathKey(instruction.Destination)])
	}
	return result, nil
}

func metadataForInstruction(instruction ir.Instruction, sources []Metadata, work Record) (Metadata, error) {
	if instruction.Op == ir.OpMap && len(instruction.Sources) == 0 {
		return Metadata{Presence: PresencePresent, Classification: ClassificationPublic,
			Provenance: []string{"literal:" + pathKey(instruction.Destination)}}, nil
	}
	metadata, err := Propagate(instruction.Op, sources)
	if err != nil {
		return Metadata{}, err
	}
	switch instruction.Op {
	case ir.OpFilter:
		// exec's not_null predicate produces ABSENT for a known non-value,
		// but UNKNOWN must remain UNKNOWN rather than being mistaken for a
		// failed predicate. The actual predicate result is not metadata loss.
		if sources[0].Presence == PresenceUnknown {
			metadata.Presence = PresenceUnknown
		} else if instruction.Function == ir.FuncEquals && sources[0].Presence == PresencePresent {
			field := work[pathKey(instruction.Sources[0])]
			if fmt.Sprint(field.Data) != instruction.Literal {
				metadata.Presence = PresenceAbsent
				metadata.Reason = "filtered: not equal"
			}
		} else if instruction.Function == ir.FuncNotNull && sources[0].Presence != PresencePresent {
			metadata.Presence = PresenceAbsent
			metadata.Reason = "filtered: not_null"
		}
	case ir.OpJoinByKey:
		if sources[0].Presence == PresencePresent && sources[1].Presence == PresencePresent {
			left, lok := work[pathKey(instruction.Sources[0])]
			right, rok := work[pathKey(instruction.Sources[1])]
			if lok && rok && fmt.Sprint(left.Data) != fmt.Sprint(right.Data) {
				metadata.Presence = PresenceUnknown
				metadata.Reason = "join sources disagree"
			}
		}
	}
	return canonical(metadata), nil
}

func cloneField(field Field) Field {
	field.Provenance = append([]string(nil), field.Provenance...)
	field.Taint = append([]string(nil), field.Taint...)
	return field
}
