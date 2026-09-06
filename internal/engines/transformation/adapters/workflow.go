package adapters

import (
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
)

// This file mirrors the workflow TRANSFORM step's mapping form.
//
// Mirrored, field for field, from internal/workflow:
//
//	workflow.ValueType       -> WorkflowValueType (Kind, Nullable, Brand,
//	                            EnumRef, MessageRef, Element)
//	workflow.CompiledMapping -> WorkflowMapping   (Target, TargetType,
//	                            SourceKind, SourceNode, SourceCtx,
//	                            SourcePath, Constant, SourceType)
//	workflow.TransformSpec   -> WorkflowTransformStep (TransformRef, Version,
//	                            InlineCode, UsesClock, UsesRandom,
//	                            UsesNetwork, Lookups, NormalizationProfile,
//	                            Limits)
//	workflow.TransformLimits -> WorkflowTransformLimits
//
// Deliberately NOT mirrored, because none of them is part of the field
// mapping this package lowers, and each is named here so its absence is a
// decision rather than an oversight:
//
//	CompiledMapping.PinnedInput  - a compile-time pinning proof about where
//	                               a source came from, not a mapping step.
//	TransformSpec.InputTaint,
//	TransformSpec.OutputTaint,
//	TransformSpec.SanitizerReceiptRef
//	                             - taint carriage is owned by the shared
//	                               engine's taint package (XFORM-004), which
//	                               propagates per IR operation; lowering it a
//	                               second time here would create a second
//	                               authority for the same fact.
//	CompiledTransform.InputDigest,
//	CompiledTransform.Lineage    - evidence the workflow compiler mints about
//	                               a binding, not an operation to execute.

// WorkflowKind mirrors workflow.Kind: the base kind of a typed workflow
// value.
type WorkflowKind string

// The workflow value kinds, spelled exactly as workflow.Kind spells them.
const (
	WorkflowKindString    WorkflowKind = "STRING"
	WorkflowKindInteger   WorkflowKind = "INTEGER"
	WorkflowKindDecimal   WorkflowKind = "DECIMAL"
	WorkflowKindBool      WorkflowKind = "BOOL"
	WorkflowKindInstant   WorkflowKind = "INSTANT"
	WorkflowKindLocalDate WorkflowKind = "LOCAL_DATE"
	WorkflowKindMoney     WorkflowKind = "MONEY"
	WorkflowKindEnum      WorkflowKind = "ENUM"
	WorkflowKindMessage   WorkflowKind = "MESSAGE"
	WorkflowKindList      WorkflowKind = "LIST"
)

// WorkflowValueType mirrors workflow.ValueType.
type WorkflowValueType struct {
	Kind       WorkflowKind
	Nullable   bool
	Brand      string
	EnumRef    string
	MessageRef string
	Element    *WorkflowValueType
}

// String renders the type the way workflow.ValueType renders it, so a
// carrier or refusal names the type in the vocabulary the site uses.
func (t WorkflowValueType) String() string {
	s := string(t.Kind)
	switch t.Kind {
	case WorkflowKindEnum:
		s += "<" + t.EnumRef + ">"
	case WorkflowKindMessage:
		s += "<" + t.MessageRef + ">"
	case WorkflowKindList:
		if t.Element != nil {
			s += "<" + t.Element.String() + ">"
		} else {
			s += "<?>"
		}
	}
	if t.Brand != "" {
		s += "#" + t.Brand
	}
	if t.Nullable {
		s += "?"
	}
	return s
}

// WorkflowSourceKind mirrors workflow.SourceKind.
type WorkflowSourceKind string

// The workflow mapping source kinds, spelled as workflow spells them.
const (
	WorkflowSourceWorkflowInput WorkflowSourceKind = "WORKFLOW_INPUT"
	WorkflowSourceNodeOutput    WorkflowSourceKind = "NODE_OUTPUT"
	WorkflowSourceContext       WorkflowSourceKind = "CONTEXT"
	WorkflowSourceConstant      WorkflowSourceKind = "CONSTANT"
)

// WorkflowMapping mirrors workflow.CompiledMapping.
type WorkflowMapping struct {
	Target     string
	TargetType WorkflowValueType
	SourceKind WorkflowSourceKind
	SourceNode string
	SourceCtx  string
	SourcePath string
	Constant   string
	SourceType WorkflowValueType
}

// WorkflowLookup mirrors workflow.TransformLookup.
type WorkflowLookup struct {
	Ref            string
	SnapshotDigest string
}

// WorkflowTransformLimits mirrors workflow.TransformLimits.
type WorkflowTransformLimits struct {
	MaxInputBytes  uint64
	MaxOutputBytes uint64
	MaxSteps       uint64
}

// WorkflowTransformStep is one workflow TRANSFORM node's mapping form: the
// node identity, the transform binding, and the input mappings the compiler
// resolved for it.
type WorkflowTransformStep struct {
	NodeID               string
	TransformRef         string
	Version              uint32
	NormalizationProfile string
	InlineCode           string
	UsesClock            bool
	UsesRandom           bool
	UsesNetwork          bool
	Lookups              []WorkflowLookup
	Mappings             []WorkflowMapping
	Limits               WorkflowTransformLimits
}

// LowerWorkflowTransform lowers one workflow TRANSFORM step's input mapping
// onto the shared engine.
//
// Every mapping is a binding, so every mapping lowers to exactly one
// instruction: a projection for a workflow input or a predecessor node's
// output, and a default-literal map for a constant. A context read has no
// lowering at all -- it is precisely the ambient dependency the
// transformation contract refuses -- and is returned as a typed refusal.
func LowerWorkflowTransform(step WorkflowTransformStep) (Lowered, error) {
	site := SiteWorkflowTransform
	if step.NodeID == "" {
		return Lowered{}, refuse(site, FeatureInvalidMapping, "", "transform step declares no node id")
	}
	if len(step.Mappings) == 0 {
		return Lowered{}, refuse(site, FeatureInvalidMapping, step.NodeID, "transform step declares no input mappings")
	}
	if step.InlineCode != "" {
		return Lowered{}, refuse(site, FeatureNondeterministicTransform, step.NodeID,
			"transform %q carries inline code", step.TransformRef)
	}
	for label, used := range map[string]bool{"clock": step.UsesClock, "randomness": step.UsesRandom, "network": step.UsesNetwork} {
		if used {
			return Lowered{}, refuse(site, FeatureNondeterministicTransform, step.NodeID,
				"transform %q declares that it reads the %s", step.TransformRef, label)
		}
	}
	if len(step.Lookups) > 0 {
		return Lowered{}, refuse(site, FeaturePinnedLookup, step.Lookups[0].Ref,
			"transform %q performs %d pinned reference-data lookup(s); the IR reads only its declared typed inputs",
			step.TransformRef, len(step.Lookups))
	}

	name := "workflow.transform." + step.NodeID
	b, err := newBuilder(site, name, name+".source", name+".inputs")
	if err != nil {
		return Lowered{}, err
	}

	mappings := append([]WorkflowMapping(nil), step.Mappings...)
	sort.SliceStable(mappings, func(i, j int) bool { return mappings[i].Target < mappings[j].Target })

	sawInteger, sawBool := false, false
	for _, m := range mappings {
		if m.Target == "" {
			return Lowered{}, refuse(site, FeatureInvalidMapping, step.NodeID, "a mapping declares no target")
		}
		targetType, err := lowerWorkflowType(site, m.Target, m.TargetType, b)
		if err != nil {
			return Lowered{}, err
		}
		switch targetType {
		case transformation.TypeInt:
			sawInteger = true
		case transformation.TypeBool:
			sawBool = true
		}

		switch m.SourceKind {
		case WorkflowSourceContext:
			return Lowered{}, refuse(site, FeatureAmbientSource, m.Target,
				"mapping reads context %q field %q; the transformation contract refuses ambient dependencies",
				m.SourceCtx, m.SourcePath)
		case WorkflowSourceConstant:
			if err := b.literal(m.Target, m.Constant, targetType); err != nil {
				return Lowered{}, err
			}
		case WorkflowSourceWorkflowInput:
			if m.SourcePath == "" {
				return Lowered{}, refuse(site, FeatureInvalidMapping, m.Target, "workflow-input mapping declares no source path")
			}
			if err := b.project(m.Target, "input:"+m.SourcePath, targetType); err != nil {
				return Lowered{}, err
			}
		case WorkflowSourceNodeOutput:
			if m.SourceNode == "" || m.SourcePath == "" {
				return Lowered{}, refuse(site, FeatureInvalidMapping, m.Target, "node-output mapping declares no source node or path")
			}
			if err := b.project(m.Target, "node:"+m.SourceNode+":"+m.SourcePath, targetType); err != nil {
				return Lowered{}, err
			}
		default:
			return Lowered{}, refuse(site, FeatureInvalidMapping, m.Target,
				"mapping source kind %q is not a declared workflow source kind", string(m.SourceKind))
		}

		if m.SourceKind != WorkflowSourceConstant && m.SourceType.Kind != "" {
			sourceType, err := lowerWorkflowType(site, m.Target, m.SourceType, nil)
			if err != nil {
				return Lowered{}, err
			}
			if sourceType != targetType {
				return Lowered{}, refuse(site, FeatureInvalidMapping, m.Target,
					"mapping source type %s and target type %s lower to different IR types (%s vs %s)",
					m.SourceType, m.TargetType, sourceType, targetType)
			}
		}
	}

	b.setLimits(workflowLimits(step.Limits, len(mappings)))
	b.diverge(Divergence{
		Feature: "unresolved_source",
		Vector:  "a mapped source the run did not supply",
		Site:    "refuses the whole walk with UNRESOLVED_SOURCE naming the mapping",
		Lowered: "yields an ABSENT property for that target and continues",
	})
	if sawInteger {
		b.diverge(Divergence{
			Feature: "integer_recanonicalization",
			Vector:  `a non-canonical INTEGER text such as "007" or "+7"`,
			Site:    `carries the text verbatim ("007")`,
			Lowered: `parses to int64 and re-renders canonically ("7")`,
		})
	}
	if sawBool {
		b.diverge(Divergence{
			Feature: "bool_recanonicalization",
			Vector:  `a non-canonical BOOL text such as "TRUE" or "1"`,
			Site:    `carries the text verbatim ("TRUE")`,
			Lowered: `parses and re-renders canonically ("true")`,
		})
	}
	return b.finish()
}

// workflowLimits is the declared limits mapping for a workflow TRANSFORM
// step. A TRANSFORM node maps exactly one node input set, so the dataset
// bound is one row; the byte and step bounds are the node's own declared
// TransformLimits, never a number this package chose.
func workflowLimits(l WorkflowTransformLimits, operations int) LimitsMapping {
	notes := []string{
		"max_input_bytes/max_output_bytes: workflow.TransformSpec.Limits, verbatim",
		"exec max_steps: workflow.TransformSpec.Limits.MaxSteps, verbatim",
		"exec max_rows=1: a TRANSFORM node maps exactly one node input set",
		fmt.Sprintf("max_operations=%d: one instruction per declared input mapping", operations),
		"max_expansion=1: every workflow mapping binds exactly one source",
	}
	def := transformation.ResourceLimits{
		MaxOperations:  operations,
		MaxInputBytes:  int64(l.MaxInputBytes),
		MaxOutputBytes: int64(l.MaxOutputBytes),
		MaxExpansion:   1,
	}
	if def.MaxInputBytes <= 0 {
		def.MaxInputBytes = DefaultMaxBytes
		notes = append(notes, "max_input_bytes: the step declared none; adapters default applied")
	}
	if def.MaxOutputBytes <= 0 {
		def.MaxOutputBytes = DefaultMaxBytes
		notes = append(notes, "max_output_bytes: the step declared none; adapters default applied")
	}
	return LimitsMapping{
		Definition: def,
		Execution: execLimits{
			MaxRows:        1,
			MaxSteps:       int(l.MaxSteps),
			MaxOutputBytes: def.MaxOutputBytes,
		},
		Notes: notes,
	}
}

// DefaultMaxBytes is the byte bound applied when a site's mapping form
// declares none of its own. It is declared here, cited in every limits
// mapping that uses it, and never applied silently.
const DefaultMaxBytes int64 = 1 << 20

// lowerWorkflowType maps a workflow ValueType onto an IR type, recording a
// carrier whenever the IR type carries the value's canonical text rather
// than its full semantics. A nil builder means "resolve the type only" (used
// to compare a mapping's source and target types).
func lowerWorkflowType(site Site, target string, t WorkflowValueType, b *builder) (transformation.Type, error) {
	note := func(c Carrier) {
		if b != nil {
			b.carrier(c)
		}
	}
	if t.Nullable && b != nil {
		note(Carrier{Target: target, SiteType: t.String(), IRType: transformation.TypeString,
			Loses: "nullability is a declared property of the workflow type; the IR carries absence as a presence state on the value instead"})
	}
	switch t.Kind {
	case WorkflowKindString:
		if t.Brand != "" {
			note(Carrier{Target: target, SiteType: t.String(), IRType: transformation.TypeString,
				Loses: "the brand: the IR cannot refuse a differently branded value flowing into this field"})
		}
		return transformation.TypeString, nil
	case WorkflowKindEnum:
		note(Carrier{Target: target, SiteType: t.String(), IRType: transformation.TypeString,
			Loses: "the enum reference: the IR cannot refuse a value outside the published enum"})
		return transformation.TypeString, nil
	case WorkflowKindMoney:
		note(Carrier{Target: target, SiteType: t.String(), IRType: transformation.TypeString,
			Loses: "the money contract: amount, currency, scale and rounding ride as one canonical text, so the IR can carry the value but cannot compute with it"})
		return transformation.TypeString, nil
	case WorkflowKindDecimal:
		return transformation.TypeDecimal, nil
	case WorkflowKindInteger:
		return transformation.TypeInt, nil
	case WorkflowKindBool:
		return transformation.TypeBool, nil
	case WorkflowKindLocalDate:
		return transformation.TypeDate, nil
	case WorkflowKindInstant:
		return transformation.TypeTimestamp, nil
	case WorkflowKindList, WorkflowKindMessage:
		return "", refuse(site, FeatureCompositeValueType, target,
			"field type %s is composite; the IR's type vocabulary is scalar-only", t)
	case "":
		return "", refuse(site, FeatureInvalidMapping, target, "field declares no value kind")
	default:
		return "", refuse(site, FeatureInvalidMapping, target, "value kind %q is not a declared workflow kind", string(t.Kind))
	}
}
