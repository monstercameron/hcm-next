package adapters_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/adapters"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// The workflow migration proof.
//
// internal/workflow is imported HERE ONLY, in a test. The adapters package
// itself must not import it: internal/engines is rank 1 and internal/workflow
// is rank 4 in definitions/architecture/package-dependency-policy.yaml, and
// tools/planning/boundarytests pins the committed layer graph, which contains
// no "engines -> workflow" edge. A test import is not a package import in
// `go list -json`'s Imports, so this proof can compare against the real site
// code without adding that edge -- which is exactly the point: the comparison
// runs the site's own current path, unmodified.

// stepFromCompiled translates a compiled workflow TRANSFORM node into the
// adapters mirror. It is the whole "site adapts into the engine's request"
// direction, written out, and it is deliberately mechanical: every field it
// reads is named, so a workflow field it forgot would be visible here.
func stepFromCompiled(t *testing.T, node workflow.CompiledNode) adapters.WorkflowTransformStep {
	t.Helper()
	if node.Transform == nil {
		t.Fatalf("node %s is not a bound TRANSFORM node", node.ID)
	}
	step := adapters.WorkflowTransformStep{
		NodeID:               node.ID,
		TransformRef:         node.Transform.TransformRef,
		Version:              node.Transform.Version,
		NormalizationProfile: node.Transform.NormalizationProfile,
		Limits: adapters.WorkflowTransformLimits{
			MaxInputBytes:  node.Transform.Limits.MaxInputBytes,
			MaxOutputBytes: node.Transform.Limits.MaxOutputBytes,
			MaxSteps:       node.Transform.Limits.MaxSteps,
		},
	}
	for _, l := range node.Transform.Lookups {
		step.Lookups = append(step.Lookups, adapters.WorkflowLookup{Ref: l.Ref, SnapshotDigest: l.SnapshotDigest})
	}
	for _, m := range node.Mappings {
		step.Mappings = append(step.Mappings, adapters.WorkflowMapping{
			Target:     m.Target,
			TargetType: valueTypeFrom(m.TargetType),
			SourceKind: adapters.WorkflowSourceKind(m.SourceKind),
			SourceNode: m.SourceNode,
			SourceCtx:  m.SourceCtx,
			SourcePath: m.SourcePath,
			Constant:   m.Constant,
			SourceType: valueTypeFrom(m.SourceType),
		})
	}
	return step
}

func valueTypeFrom(t workflow.ValueType) adapters.WorkflowValueType {
	out := adapters.WorkflowValueType{
		Kind:       adapters.WorkflowKind(t.Kind),
		Nullable:   t.Nullable,
		Brand:      t.Brand,
		EnumRef:    t.EnumRef,
		MessageRef: t.MessageRef,
	}
	if t.Element != nil {
		el := valueTypeFrom(*t.Element)
		out.Element = &el
	}
	return out
}

// recordingTransforms captures the input Bag the workflow's own mapping
// resolution produced for each TRANSFORM node, then delegates to the real
// transform port so the walk proceeds exactly as it otherwise would.
type recordingTransforms struct {
	inner simulate.TransformPort
	bags  map[string]simulate.Bag
}

func (r *recordingTransforms) Transform(ctx context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	if r.bags == nil {
		r.bags = map[string]simulate.Bag{}
	}
	copied := make(simulate.Bag, len(req.Inputs))
	for path, value := range req.Inputs {
		copied[path] = value
	}
	r.bags[req.NodeID] = copied
	return r.inner.Transform(ctx, req)
}

func promotionTransformNodes(t *testing.T) ([]workflow.CompiledNode, map[string]simulate.Bag) {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("NewPromotionSetup: %v", err)
	}
	rec := &recordingTransforms{inner: setup.Options.Transforms}
	setup.Options.Transforms = rec
	if _, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options); err != nil {
		t.Fatalf("simulate.Run: %v", err)
	}
	var nodes []workflow.CompiledNode
	for _, n := range setup.Plan.Nodes {
		if n.Type == workflow.StepTransform {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		t.Fatal("the promotion plan compiled no TRANSFORM node")
	}
	return nodes, rec.bags
}

func TestTodo_XFORM_008_MigrationProof_Workflow(t *testing.T) {
	nodes, bags := promotionTransformNodes(t)

	for _, node := range nodes {
		t.Run(node.ID, func(t *testing.T) {
			lowered, err := adapters.LowerWorkflowTransform(stepFromCompiled(t, node))
			if err != nil {
				t.Fatalf("LowerWorkflowTransform: %v", err)
			}

			// (1) Wiring parity against the site's own compiled mappings.
			byTarget := map[string]adapters.Binding{}
			for _, b := range lowered.Bindings {
				byTarget[b.Target] = b
			}
			if len(byTarget) != len(node.Mappings) {
				t.Fatalf("lowered %d bindings for %d compiled mappings", len(byTarget), len(node.Mappings))
			}
			for _, m := range node.Mappings {
				b, ok := byTarget[m.Target]
				if !ok {
					t.Fatalf("no lowered binding for compiled mapping target %q", m.Target)
				}
				want := ""
				switch m.SourceKind {
				case workflow.SourceWorkflowInput:
					want = "workflow.transform." + node.ID + ".source.input:" + m.SourcePath
				case workflow.SourceNodeOutput:
					want = "workflow.transform." + node.ID + ".source.node:" + m.SourceNode + ":" + m.SourcePath
				case workflow.SourceConstant:
					want = ""
				default:
					t.Fatalf("unexpected compiled source kind %q", m.SourceKind)
				}
				if b.SourceKey != want {
					t.Fatalf("target %q lowered to source key %q, want %q", m.Target, b.SourceKey, want)
				}
			}

			// (2) Execution parity against the bag the site's own mapping
			// resolution produced. Every promotion TRANSFORM mapping is a
			// projection, so the recorded target value IS the source value the
			// site read; placing it at the lowered source key replays the same
			// bytes through the shared engine rather than inventing new ones.
			bag, ok := bags[node.ID]
			if !ok {
				t.Fatalf("the walk never dispatched TRANSFORM node %q", node.ID)
			}
			row := map[string]string{}
			siteOutput := map[string]string{}
			for _, path := range bag.Paths() {
				siteOutput[path] = bag[path].Text
				b, ok := byTarget[path]
				if !ok {
					t.Fatalf("the site resolved input %q, which the lowering does not bind", path)
				}
				if b.SourceKey != "" {
					row[b.SourceKey] = bag[path].Text
				}
			}
			results, err := lowered.Run([]map[string]string{row})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("Run returned %d rows, want 1", len(results))
			}
			got, err := lowered.Texts(results[0])
			if err != nil {
				t.Fatalf("Texts: %v", err)
			}
			assertByteIdentical(t, "workflow "+node.ID, siteOutput, got)

			// (3) The declared carriers must account for every non-plain-string
			// workflow type the node maps, so nothing rides a string silently.
			carried := map[string]bool{}
			for _, c := range lowered.Carriers {
				carried[c.Target] = true
			}
			for _, m := range node.Mappings {
				switch m.TargetType.Kind {
				case workflow.KindMoney, workflow.KindEnum:
					if !carried[m.Target] {
						t.Fatalf("target %q is %s but no carrier was declared", m.Target, m.TargetType)
					}
				case workflow.KindString:
					if m.TargetType.Brand != "" && !carried[m.Target] {
						t.Fatalf("target %q is branded %s but no carrier was declared", m.Target, m.TargetType)
					}
				}
			}
			t.Logf("%s", lowered.Explain())
		})
	}
}

// assertByteIdentical compares the site's own output with the lowered
// program's output as canonical JSON bytes, so the assertion is on bytes and
// not on a field-by-field comparison that could quietly ignore an extra key.
func assertByteIdentical(t *testing.T, label string, site, lowered map[string]string) {
	t.Helper()
	wantBytes, err := json.Marshal(site)
	if err != nil {
		t.Fatalf("marshal site output: %v", err)
	}
	gotBytes, err := json.Marshal(lowered)
	if err != nil {
		t.Fatalf("marshal lowered output: %v", err)
	}
	if string(wantBytes) != string(gotBytes) {
		t.Fatalf("%s: lowered output is not byte-identical to the site's current path\n site:    %s\n lowered: %s",
			label, wantBytes, gotBytes)
	}
}

func TestLowerWorkflowTransformRefusals(t *testing.T) {
	base := func() adapters.WorkflowTransformStep {
		return adapters.WorkflowTransformStep{
			NodeID:       "build_proposal",
			TransformRef: "transforms.promotion.build_proposal",
			Version:      1,
			Limits:       adapters.WorkflowTransformLimits{MaxInputBytes: 65536, MaxOutputBytes: 65536, MaxSteps: 5000},
			Mappings: []adapters.WorkflowMapping{{
				Target:     "worker_id",
				TargetType: adapters.WorkflowValueType{Kind: adapters.WorkflowKindString, Brand: "WorkerID"},
				SourceKind: adapters.WorkflowSourceNodeOutput,
				SourceNode: "snapshot_worker",
				SourcePath: "worker_id",
			}},
		}
	}
	if _, err := adapters.LowerWorkflowTransform(base()); err != nil {
		t.Fatalf("the unmutated step must lower: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*adapters.WorkflowTransformStep)
		feature string
	}{
		{"context source", func(s *adapters.WorkflowTransformStep) {
			s.Mappings[0].SourceKind = adapters.WorkflowSourceContext
			s.Mappings[0].SourceCtx = "worker_snapshot"
		}, adapters.FeatureAmbientSource},
		{"list type", func(s *adapters.WorkflowTransformStep) {
			el := adapters.WorkflowValueType{Kind: adapters.WorkflowKindString}
			s.Mappings[0].TargetType = adapters.WorkflowValueType{Kind: adapters.WorkflowKindList, Element: &el}
			s.Mappings[0].SourceType = adapters.WorkflowValueType{}
		}, adapters.FeatureCompositeValueType},
		{"message type", func(s *adapters.WorkflowTransformStep) {
			s.Mappings[0].TargetType = adapters.WorkflowValueType{Kind: adapters.WorkflowKindMessage, MessageRef: "hcmnext.Proposal"}
			s.Mappings[0].SourceType = adapters.WorkflowValueType{}
		}, adapters.FeatureCompositeValueType},
		{"pinned lookup", func(s *adapters.WorkflowTransformStep) {
			s.Lookups = []adapters.WorkflowLookup{{Ref: "reference.pay_band", SnapshotDigest: "sha256:abc"}}
		}, adapters.FeaturePinnedLookup},
		{"inline code", func(s *adapters.WorkflowTransformStep) { s.InlineCode = "return x" },
			adapters.FeatureNondeterministicTransform},
		{"reads the clock", func(s *adapters.WorkflowTransformStep) { s.UsesClock = true },
			adapters.FeatureNondeterministicTransform},
		{"empty constant", func(s *adapters.WorkflowTransformStep) {
			s.Mappings[0].SourceKind = adapters.WorkflowSourceConstant
			s.Mappings[0].Constant = ""
		}, adapters.FeatureEmptyLiteral},
		{"duplicate target", func(s *adapters.WorkflowTransformStep) {
			s.Mappings = append(s.Mappings, s.Mappings[0])
		}, adapters.FeatureDuplicateTarget},
		{"unrepresentable target name", func(s *adapters.WorkflowTransformStep) {
			s.Mappings[0].Target = "worker id"
		}, adapters.FeatureUnrepresentableName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := base()
			tc.mutate(&step)
			_, err := adapters.LowerWorkflowTransform(step)
			var refusal adapters.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error = %v, want an adapters.Refusal", err)
			}
			if !errors.Is(err, adapters.ErrNoIREquivalent) {
				t.Fatalf("refusal does not unwrap to ErrNoIREquivalent: %v", err)
			}
			if refusal.Feature != tc.feature {
				t.Fatalf("refusal feature = %q, want %q (%v)", refusal.Feature, tc.feature, err)
			}
			if refusal.Site != adapters.SiteWorkflowTransform {
				t.Fatalf("refusal site = %q", refusal.Site)
			}
		})
	}
}

func TestLowerWorkflowTransformTypeTable(t *testing.T) {
	cases := []struct {
		kind adapters.WorkflowKind
		want transformation.Type
	}{
		{adapters.WorkflowKindString, transformation.TypeString},
		{adapters.WorkflowKindEnum, transformation.TypeString},
		{adapters.WorkflowKindMoney, transformation.TypeString},
		{adapters.WorkflowKindDecimal, transformation.TypeDecimal},
		{adapters.WorkflowKindInteger, transformation.TypeInt},
		{adapters.WorkflowKindBool, transformation.TypeBool},
		{adapters.WorkflowKindLocalDate, transformation.TypeDate},
		{adapters.WorkflowKindInstant, transformation.TypeTimestamp},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			typ := adapters.WorkflowValueType{Kind: tc.kind}
			if tc.kind == adapters.WorkflowKindEnum {
				typ.EnumRef = "hcmnext.BandPosition"
			}
			step := adapters.WorkflowTransformStep{
				NodeID: "n", TransformRef: "t", Version: 1,
				Limits: adapters.WorkflowTransformLimits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxSteps: 8},
				Mappings: []adapters.WorkflowMapping{{
					Target: "f", TargetType: typ, SourceKind: adapters.WorkflowSourceWorkflowInput, SourcePath: "f",
				}},
			}
			lowered, err := adapters.LowerWorkflowTransform(step)
			if err != nil {
				t.Fatalf("lower %s: %v", tc.kind, err)
			}
			if lowered.Bindings[0].Type != tc.want {
				t.Fatalf("%s lowered to %s, want %s", tc.kind, lowered.Bindings[0].Type, tc.want)
			}
		})
	}
}

// TestWorkflowIntegerAndBoolRecanonicalization pins the divergence the
// lowering declares rather than hiding it: exec holds INTEGER and BOOL as Go
// values, so a non-canonical text the workflow carries verbatim comes back
// re-rendered.
func TestWorkflowIntegerAndBoolRecanonicalization(t *testing.T) {
	step := adapters.WorkflowTransformStep{
		NodeID: "n", TransformRef: "t", Version: 1,
		Limits: adapters.WorkflowTransformLimits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxSteps: 8},
		Mappings: []adapters.WorkflowMapping{
			{Target: "count", TargetType: adapters.WorkflowValueType{Kind: adapters.WorkflowKindInteger},
				SourceKind: adapters.WorkflowSourceWorkflowInput, SourcePath: "count"},
			{Target: "flag", TargetType: adapters.WorkflowValueType{Kind: adapters.WorkflowKindBool},
				SourceKind: adapters.WorkflowSourceWorkflowInput, SourcePath: "flag"},
		},
	}
	lowered, err := adapters.LowerWorkflowTransform(step)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	features := map[string]bool{}
	for _, d := range lowered.Divergences {
		features[d.Feature] = true
	}
	for _, want := range []string{"integer_recanonicalization", "bool_recanonicalization", "unresolved_source"} {
		if !features[want] {
			t.Fatalf("divergence %q was not declared: %+v", want, lowered.Divergences)
		}
	}

	rows, err := lowered.Run([]map[string]string{{
		"workflow.transform.n.source.input:count": "007",
		"workflow.transform.n.source.input:flag":  "TRUE",
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := lowered.Texts(rows[0])
	if err != nil {
		t.Fatalf("Texts: %v", err)
	}
	if got["count"] != "7" || got["flag"] != "true" {
		t.Fatalf("recanonicalization changed: %+v", got)
	}

	// The unresolved-source divergence: the site refuses; the lowering
	// yields ABSENT, which Texts reports by omitting the target.
	rows, err = lowered.Run([]map[string]string{{}})
	if err != nil {
		t.Fatalf("Run with no sources: %v", err)
	}
	got, err = lowered.Texts(rows[0])
	if err != nil {
		t.Fatalf("Texts: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an unsupplied source produced %+v, want no present targets", got)
	}
}
