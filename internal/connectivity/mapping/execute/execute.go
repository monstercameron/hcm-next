// Package execute applies a compiled mapping profile using the shared,
// bounded transformation IR.  It has no provider, clock, locale, database,
// or callback surface: every result is a pure function of the profile, the
// pinned programs, and the source record.
package execute

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/connectivity/mapping"
	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/exec"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/lineage"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/taint"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const contractVersion = 1

// Version reports this package's contract version (ARCH-GO-009).
func Version() int { return contractVersion }

var (
	ErrInvalidProfile = errors.New("mapping/execute: invalid compiled profile")
	ErrMissingProgram = errors.New("mapping/execute: compiled transformation is missing")
	ErrDigestMismatch = errors.New("mapping/execute: transformation IR digest mismatch")
	ErrMissingSource  = errors.New("mapping/execute: required source field is missing")
)

// Result is the stable output of one profile execution.  Output contains only
// target keys; Lineage contains no source or output values.
type Result struct {
	Output        exec.Record
	Lineage       lineage.Graph
	Diagnostics   []Diagnostic
	PayloadDigest string
}

// Diagnostic is reserved for non-fatal, audit-safe execution notes.  The
// current typed IR rejects fatal issues before returning a Result.
type Diagnostic struct {
	Target string `json:"target"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Programs is a digest-indexed immutable-at-call-boundary collection of
// compiled transformation programs.  The identity token is handled by this
// package and is not a map key.
type Programs map[string]ir.Program

// Execute applies every field mapping in profile.  A program map is required
// for non-identity transformations, and each supplied program must digest to
// the exact IR digest pinned by its field mapping.  An optional execution limit
// is forwarded unchanged to transformation/exec and lineage/taint.
func Execute(profile mapping.MappingProfileVersion, source exec.Record, programs map[string]ir.Program, limits ...exec.Limits) (Result, error) {
	if profile.Digest() == "" || len(profile.Mappings()) == 0 {
		return Result{}, ErrInvalidProfile
	}
	if len(limits) > 1 {
		return Result{}, fmt.Errorf("%w: more than one execution limit", ErrInvalidProfile)
	}
	var limit exec.Limits
	if len(limits) == 1 {
		limit = limits[0]
	}

	mappings := profile.Mappings()
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].TargetField < mappings[j].TargetField })
	plans := make([]plan, 0, len(mappings))
	for _, field := range mappings {
		if field.TargetField == "" || field.SourceField == "" {
			return Result{}, fmt.Errorf("%w: source and target are required", ErrInvalidProfile)
		}
		if field.TransformationIRDigest == mapping.IdentityTransformation {
			value, ok := sourceValue(source, field.SourceField)
			if !ok && field.Required {
				return Result{}, fmt.Errorf("%w: %s", ErrMissingSource, field.SourceField)
			}
			typ := transformation.TypeString
			if ok {
				typ = value.Type
			}
			plans = append(plans, plan{field: field, program: identityProgram(field, typ)})
			continue
		}
		program, ok := programs[field.TransformationIRDigest]
		if !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrMissingProgram, field.TransformationIRDigest)
		}
		digest, err := program.Digest()
		if err != nil {
			return Result{}, fmt.Errorf("%w: %s: %v", ErrDigestMismatch, field.TargetField, err)
		}
		if digest != field.TransformationIRDigest {
			return Result{}, fmt.Errorf("%w: target %s pins %s, compiled program is %s", ErrDigestMismatch, field.TargetField, field.TransformationIRDigest, digest)
		}
		if _, ok := sourceValue(source, field.SourceField); !ok && field.Required {
			return Result{}, fmt.Errorf("%w: %s", ErrMissingSource, field.SourceField)
		}
		plans = append(plans, plan{field: field, program: program})
	}

	output := make(exec.Record, len(plans))
	graphs := make([]lineage.Graph, 0, len(plans))
	for _, item := range plans {
		prepared := prepareRecord(source, item.field.SourceField, item.program)
		tainted, err := toTaintRecord(prepared)
		if err != nil {
			return Result{}, fmt.Errorf("%w: input: %v", ErrInvalidProfile, err)
		}
		execution, err := lineage.Execute(item.program, tainted, limit)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %s: %v", exec.ErrRefused, item.field.TargetField, err)
		}
		valuesOut, err := exec.Execute(item.program, []exec.Record{prepared}, limit)
		if err != nil || len(valuesOut) != 1 {
			if err == nil {
				err = errors.New("no output row")
			}
			return Result{}, fmt.Errorf("%w: %s: %v", exec.ErrRefused, item.field.TargetField, err)
		}
		value, ok := selectOutput(valuesOut[0], item.program, item.field.TargetField)
		if !ok {
			return Result{}, fmt.Errorf("%w: target %s is not produced by IR", ErrInvalidProfile, item.field.TargetField)
		}
		output[item.field.TargetField] = value
		graphs = append(graphs, remapGraph(execution.Graph, item.field.TargetField, item.program))
	}

	graph, err := mergeGraphs(graphs)
	if err != nil {
		return Result{}, err
	}
	digest, err := exec.Digest([]exec.Record{output})
	if err != nil {
		return Result{}, err
	}
	return Result{Output: output, Lineage: graph, PayloadDigest: digest}, nil
}

type plan struct {
	field   mapping.FieldMapping
	program ir.Program
}

func identityProgram(field mapping.FieldMapping, typ transformation.Type) ir.Program {
	schema, name := splitPath(field.SourceField, "source")
	targetSchema, targetName := splitPath(field.TargetField, "target")
	return ir.Program{
		IRVersion:      ir.IRVersion,
		DefinitionName: "mapping-identity",
		Instructions: []ir.Instruction{{
			Op:          ir.OpProject,
			Sources:     []transformation.Path{{Schema: schema, Field: name, Type: typ}},
			Destination: transformation.Path{Schema: targetSchema, Field: targetName, Type: typ},
		}},
		Limits: ir.Limits{MaxSteps: 1, MaxFanOut: 1},
	}
}

func splitPath(value, defaultSchema string) (string, string) {
	index := strings.LastIndex(value, ".")
	if index < 0 {
		return defaultSchema, value
	}
	return value[:index], value[index+1:]
}

func sourceValue(source exec.Record, name string) (exec.Value, bool) {
	if value, ok := source[name]; ok {
		return value, true
	}
	base := name
	if index := strings.LastIndex(name, "."); index >= 0 {
		base = name[index+1:]
	}
	var found exec.Value
	var foundKey string
	for key, value := range source {
		if key == base || strings.HasSuffix(key, "."+base) {
			if foundKey != "" && foundKey != key {
				return exec.Value{}, false
			}
			found, foundKey = value, key
		}
	}
	return found, foundKey != ""
}

func prepareRecord(source exec.Record, profileSource string, program ir.Program) exec.Record {
	prepared := make(exec.Record, len(source)+len(program.Instructions))
	for key, value := range source {
		prepared[key] = value
	}
	value, found := sourceValue(source, profileSource)
	if !found {
		return prepared
	}
	for _, instruction := range program.Instructions {
		for _, path := range instruction.Sources {
			if _, exists := prepared[pathKey(path)]; exists {
				continue
			}
			base := profileSource
			if index := strings.LastIndex(base, "."); index >= 0 {
				base = base[index+1:]
			}
			if path.Field == base {
				prepared[pathKey(path)] = value
			}
		}
	}
	return prepared
}

func pathKey(path transformation.Path) string { return path.Schema + "." + path.Field }

func toTaintRecord(source exec.Record) (taint.Record, error) {
	result := make(taint.Record, len(source))
	for key, value := range source {
		var presence taint.PresenceState
		switch value.State {
		case values.PresenceValue:
			presence = taint.PresencePresent
		case values.PresenceAbsent:
			presence = taint.PresenceAbsent
		case values.PresenceUnknown:
			presence = taint.PresenceUnknown
		default:
			return nil, fmt.Errorf("%s: invalid presence %s", key, value.State)
		}
		field := taint.Field{Type: value.Type, Presence: presence, Classification: taint.ClassificationPublic, Provenance: []string{"source:" + key}}
		if presence == taint.PresencePresent {
			if value.Data == nil {
				return nil, fmt.Errorf("%s: present value has no data", key)
			}
			field.Data = value.Data
		} else {
			field.Reason = value.Reason
		}
		if err := field.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		result[key] = field
	}
	return result, nil
}

func selectOutput(row exec.Record, program ir.Program, target string) (exec.Value, bool) {
	if value, ok := row[target]; ok {
		return value, true
	}
	if len(program.Instructions) == 1 {
		key := pathKey(program.Instructions[0].Destination)
		value, ok := row[key]
		return value, ok
	}
	if len(row) == 1 {
		for _, value := range row {
			return value, true
		}
	}
	return exec.Value{}, false
}

func remapGraph(graph lineage.Graph, target string, program ir.Program) lineage.Graph {
	if len(program.Instructions) != 1 {
		return graph
	}
	old := "output:" + pathKey(program.Instructions[0].Destination)
	newID := "output:" + target
	if old == newID {
		return graph
	}
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == old {
			graph.Nodes[i].ID = newID
			graph.Nodes[i].Field = target
		}
	}
	for i := range graph.Edges {
		if graph.Edges[i].From == old {
			graph.Edges[i].From = newID
		}
		if graph.Edges[i].To == old {
			graph.Edges[i].To = newID
		}
	}
	for i := range graph.Outputs {
		if graph.Outputs[i] == old {
			graph.Outputs[i] = newID
		}
	}
	return graph
}

func mergeGraphs(graphs []lineage.Graph) (lineage.Graph, error) {
	merged := lineage.Graph{}
	nodes := make(map[string]lineage.Node)
	edges := make(map[lineage.Edge]struct{})
	outputs := make(map[string]struct{})
	for _, graph := range graphs {
		if err := graph.Validate(); err != nil {
			return lineage.Graph{}, err
		}
		for _, node := range graph.Nodes {
			if existing, ok := nodes[node.ID]; ok && fmt.Sprintf("%v", existing) != fmt.Sprintf("%v", node) {
				return lineage.Graph{}, fmt.Errorf("%w: conflicting node %s", ErrInvalidProfile, node.ID)
			}
			nodes[node.ID] = node
		}
		for _, edge := range graph.Edges {
			edges[edge] = struct{}{}
		}
		for _, output := range graph.Outputs {
			outputs[output] = struct{}{}
		}
	}
	for _, node := range nodes {
		merged.Nodes = append(merged.Nodes, node)
	}
	for edge := range edges {
		merged.Edges = append(merged.Edges, edge)
	}
	for output := range outputs {
		merged.Outputs = append(merged.Outputs, output)
	}
	sort.Slice(merged.Nodes, func(i, j int) bool { return merged.Nodes[i].ID < merged.Nodes[j].ID })
	sort.Slice(merged.Edges, func(i, j int) bool {
		if merged.Edges[i].From != merged.Edges[j].From {
			return merged.Edges[i].From < merged.Edges[j].From
		}
		return merged.Edges[i].To < merged.Edges[j].To
	})
	sort.Strings(merged.Outputs)
	if err := merged.Validate(); err != nil {
		return lineage.Graph{}, err
	}
	return merged, nil
}

// Explain renders an audit-safe summary of a mapping result.
func Explain(result Result) string { return result.Explain() }

// Explain names the stable payload and lineage digests without repeating
// source or target values.
func (result Result) Explain() string {
	return fmt.Sprintf("mapping execution v%d payload=%s lineage=%s fields=%d", Version(), result.PayloadDigest, result.Lineage.Digest(), len(result.Output))
}
