// Package conformance owns deterministic golden vectors for the closed
// transformation IR. The checked-in set is data, while GenerateVectors is the
// pure generator used by the drift test to prove that data remains current.
package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/exec"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	VectorSchema  = "hcmnext.transformation.conformance.v1"
	VectorVersion = 1
	// PinnedDigest is updated only when the generator intentionally changes.
	// TestTodo_XFORM_006 also compares the complete checked-in file byte-for-byte.
	PinnedDigest = "sha256:dd1832b20fb61dae587db2366c44d21cf24dcd2afe6f49d0b4d5f3b4d9d71a10"
)

// Value is the stable, type-directed wire representation used by vectors.
// Keeping data textual avoids JSON number decoding changing int64 values into
// float64 values on a runner.
type Value struct {
	Type   transformation.Type `json:"type"`
	State  string              `json:"state"`
	Data   string              `json:"data,omitempty"`
	Reason string              `json:"reason,omitempty"`
}

type Record map[string]Value

// Vector is one positive, boundary, unknown or invalid execution case.
type Vector struct {
	Name          string    `json:"name"`
	Kind          ir.OpCode `json:"kind"`
	ProgramDigest string    `json:"program_digest"`
	Input         []Record  `json:"input"`
	Expected      []Record  `json:"expected,omitempty"`
	WantError     string    `json:"want_error,omitempty"`
}

// VectorSet is the complete content-addressed suite. Digest covers Schema,
// Version and Vectors, never the digest field itself.
type VectorSet struct {
	Schema  string   `json:"schema"`
	Version int      `json:"version"`
	Digest  string   `json:"digest"`
	Vectors []Vector `json:"vectors"`
}

// Verdict is the per-vector runner result.
type Verdict struct {
	Name  string    `json:"name"`
	Kind  ir.OpCode `json:"kind"`
	Pass  bool      `json:"pass"`
	Error string    `json:"error,omitempty"`
}

// ProgramFor returns the minimal valid program exercising one IR opcode.
func ProgramFor(kind ir.OpCode) (ir.Program, error) {
	path := func(schema, field string, typ transformation.Type) transformation.Path {
		return transformation.Path{Schema: schema, Field: field, Type: typ}
	}
	instruction := ir.Instruction{Op: kind, Destination: path("out", "value", transformation.TypeString)}
	switch kind {
	case ir.OpMap:
		instruction.Function, instruction.Literal = ir.FuncDefault, "mapped"
	case ir.OpFilter:
		instruction.Sources = []transformation.Path{path("in", "name", transformation.TypeString)}
		instruction.Function = ir.FuncNotNull
	case ir.OpProject:
		instruction.Sources = []transformation.Path{path("in", "name", transformation.TypeString)}
	case ir.OpJoinByKey:
		instruction.Sources = []transformation.Path{path("in", "left", transformation.TypeString), path("in", "right", transformation.TypeString)}
		instruction.JoinKey = "person_id"
	case ir.OpAggregate:
		instruction.Sources = []transformation.Path{path("in", "left", transformation.TypeInt), path("in", "right", transformation.TypeInt)}
		instruction.Destination.Type = transformation.TypeInt
		instruction.Function = ir.FuncSum
	case ir.OpCoerce:
		instruction.Sources = []transformation.Path{path("in", "number", transformation.TypeInt)}
		instruction.TargetType = transformation.TypeString
	default:
		return ir.Program{}, fmt.Errorf("unsupported vector opcode %q", kind)
	}
	program := ir.Program{IRVersion: ir.IRVersion, DefinitionName: "conformance", Instructions: []ir.Instruction{instruction}, Limits: ir.Limits{MaxSteps: 4, MaxFanOut: 4}}
	if err := program.Validate(); err != nil {
		return ir.Program{}, err
	}
	return program, nil
}

// GenerateVectors builds every vector and computes the suite digest without
// reading external state.
func GenerateVectors() (VectorSet, error) {
	definitions := []struct {
		name  string
		kind  ir.OpCode
		input []Record
		err   string
	}{
		{"map_literal", ir.OpMap, []Record{{}}, ""},
		{"filter_present", ir.OpFilter, []Record{{"in.name": present(transformation.TypeString, "Ada")}}, ""},
		{"filter_unknown", ir.OpFilter, []Record{{"in.name": unknown(transformation.TypeString, "source offline")}}, ""},
		{"project_absent", ir.OpProject, []Record{{}}, ""},
		{"join_equal", ir.OpJoinByKey, []Record{{"in.left": present(transformation.TypeString, "same"), "in.right": present(transformation.TypeString, "same")}}, ""},
		{"join_disagree", ir.OpJoinByKey, []Record{{"in.left": present(transformation.TypeString, "left"), "in.right": present(transformation.TypeString, "right")}}, ""},
		{"aggregate_sum", ir.OpAggregate, []Record{{"in.left": present(transformation.TypeInt, "7"), "in.right": present(transformation.TypeInt, "5")}}, ""},
		{"aggregate_unknown", ir.OpAggregate, []Record{{"in.left": unknown(transformation.TypeInt, "source offline"), "in.right": present(transformation.TypeInt, "5")}}, ""},
		{"coerce_int_to_string", ir.OpCoerce, []Record{{"in.number": present(transformation.TypeInt, "42")}}, ""},
		{"coerce_invalid", ir.OpCoerce, []Record{{"in.number": present(transformation.TypeString, "not-an-int")}}, "type mismatch"},
	}
	set := VectorSet{Schema: VectorSchema, Version: VectorVersion, Vectors: make([]Vector, 0, len(definitions))}
	for _, definition := range definitions {
		program, err := ProgramFor(definition.kind)
		if err != nil {
			return VectorSet{}, err
		}
		digest, err := program.Digest()
		if err != nil {
			return VectorSet{}, err
		}
		vector := Vector{Name: definition.name, Kind: definition.kind, ProgramDigest: digest, Input: definition.input, WantError: definition.err}
		output, runErr := exec.Execute(program, toExecRecords(definition.input), exec.Limits{})
		if definition.err == "" {
			if runErr != nil {
				return VectorSet{}, fmt.Errorf("generate %s: %w", definition.name, runErr)
			}
			vector.Expected = fromExecRecords(output)
		} else if runErr == nil || !strings.Contains(runErr.Error(), definition.err) {
			return VectorSet{}, fmt.Errorf("generate %s: expected error containing %q, got %v", definition.name, definition.err, runErr)
		}
		set.Vectors = append(set.Vectors, vector)
	}
	digest, err := digestSet(set)
	if err != nil {
		return VectorSet{}, err
	}
	set.Digest = digest
	return set, nil
}

func digestSet(set VectorSet) (string, error) {
	payload := struct {
		Schema  string   `json:"schema"`
		Version int      `json:"version"`
		Vectors []Vector `json:"vectors"`
	}{set.Schema, set.Version, set.Vectors}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

// MarshalVectors emits the checked-in representation with stable indentation.
func MarshalVectors(set VectorSet) ([]byte, error) {
	if err := ValidateSet(set); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ParseVectors decodes and validates a checked-in suite.
func ParseVectors(b []byte) (VectorSet, error) {
	var set VectorSet
	if err := json.Unmarshal(b, &set); err != nil {
		return VectorSet{}, err
	}
	if err := ValidateSet(set); err != nil {
		return VectorSet{}, err
	}
	return set, nil
}

func ValidateSet(set VectorSet) error {
	if set.Schema != VectorSchema || set.Version != VectorVersion || len(set.Vectors) == 0 {
		return errors.New("conformance: invalid vector set header")
	}
	computed, err := digestSet(set)
	if err != nil {
		return err
	}
	if set.Digest != computed {
		return fmt.Errorf("conformance: digest %s, want %s", set.Digest, computed)
	}
	seen := map[string]bool{}
	seenKinds := map[ir.OpCode]bool{}
	for _, vector := range set.Vectors {
		if vector.Name == "" || seen[vector.Name] {
			return fmt.Errorf("conformance: duplicate or empty vector name %q", vector.Name)
		}
		seen[vector.Name] = true
		seenKinds[vector.Kind] = true
		for _, record := range append(append([]Record(nil), vector.Input...), vector.Expected...) {
			for field, value := range record {
				if err := validateVectorValue(value); err != nil {
					return fmt.Errorf("conformance: vector %s field %s: %w", vector.Name, field, err)
				}
			}
		}
		program, err := ProgramFor(vector.Kind)
		if err != nil {
			return err
		}
		digest, err := program.Digest()
		if err != nil || digest != vector.ProgramDigest {
			return fmt.Errorf("conformance: vector %s program digest drifted", vector.Name)
		}
	}
	for _, kind := range []ir.OpCode{ir.OpMap, ir.OpFilter, ir.OpProject, ir.OpJoinByKey, ir.OpAggregate, ir.OpCoerce} {
		if !seenKinds[kind] {
			return fmt.Errorf("conformance: missing opcode vector %s", kind)
		}
	}
	return nil
}

func validateVectorValue(value Value) error {
	switch value.Type {
	case transformation.TypeString, transformation.TypeBool, transformation.TypeInt,
		transformation.TypeDecimal, transformation.TypeDate, transformation.TypeTimestamp:
	default:
		return fmt.Errorf("unsupported type %q", value.Type)
	}
	switch value.State {
	case "PRESENT":
		return nil
	case "ABSENT", "UNKNOWN":
		if value.Data != "" {
			return errors.New("non-PRESENT value carries data")
		}
		return nil
	default:
		return fmt.Errorf("unsupported state %q", value.State)
	}
}

// Run executes each vector through the canonical exec interpreter and returns
// a verdict for every vector, including invalid vectors.
func Run(set VectorSet) ([]Verdict, error) {
	if err := ValidateSet(set); err != nil {
		return nil, err
	}
	verdicts := make([]Verdict, 0, len(set.Vectors))
	for _, vector := range set.Vectors {
		program, err := ProgramFor(vector.Kind)
		if err != nil {
			return nil, err
		}
		actual, runErr := exec.Execute(program, toExecRecords(vector.Input), exec.Limits{})
		verdict := Verdict{Name: vector.Name, Kind: vector.Kind, Pass: true}
		switch {
		case vector.WantError != "":
			verdict.Pass = runErr != nil && strings.Contains(runErr.Error(), vector.WantError)
			if runErr != nil {
				verdict.Error = runErr.Error()
			}
		case runErr != nil:
			verdict.Pass, verdict.Error = false, runErr.Error()
		default:
			want, marshalErr := json.Marshal(vector.Expected)
			got, gotErr := json.Marshal(fromExecRecords(actual))
			verdict.Pass = gotErr == nil && marshalErr == nil && string(got) == string(want)
			if !verdict.Pass {
				verdict.Error = fmt.Sprintf("output mismatch: got %s want %s", got, want)
			}
		}
		verdicts = append(verdicts, verdict)
	}
	return verdicts, nil
}

func present(typ transformation.Type, data string) Value {
	return Value{Type: typ, State: "PRESENT", Data: data}
}
func unknown(typ transformation.Type, reason string) Value {
	return Value{Type: typ, State: "UNKNOWN", Reason: reason}
}

func toExecRecords(records []Record) []exec.Record {
	result := make([]exec.Record, len(records))
	for i, record := range records {
		result[i] = make(exec.Record, len(record))
		for key, value := range record {
			state := values.PresenceAbsent
			switch value.State {
			case "PRESENT":
				state = values.PresenceValue
			case "UNKNOWN":
				state = values.PresenceUnknown
			}
			if state == values.PresenceValue {
				data, err := parseData(value.Type, value.Data)
				if err != nil {
					result[i][key] = exec.NonValue(value.Type, values.PresenceUnknown, err.Error())
					continue
				}
				result[i][key] = exec.Present(value.Type, data)
			} else {
				result[i][key] = exec.NonValue(value.Type, state, value.Reason)
			}
		}
	}
	return result
}

func parseData(typ transformation.Type, data string) (any, error) {
	switch typ {
	case transformation.TypeInt:
		return strconv.ParseInt(data, 10, 64)
	case transformation.TypeBool:
		return strconv.ParseBool(data)
	case transformation.TypeString, transformation.TypeDecimal, transformation.TypeDate, transformation.TypeTimestamp:
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported vector type %s", typ)
	}
}

func fromExecRecords(records []exec.Record) []Record {
	result := make([]Record, len(records))
	for i, record := range records {
		result[i] = make(Record, len(record))
		for key, value := range record {
			state := value.State.String()
			if state == "VALUE" {
				state = "PRESENT"
			}
			wire := Value{Type: value.Type, State: state, Reason: value.Reason}
			if value.State == values.PresenceValue {
				wire.Data = fmt.Sprint(value.Data)
			}
			result[i][key] = wire
		}
	}
	return result
}
