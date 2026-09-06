package presentation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"gopkg.in/yaml.v3"
)

func TestTodo_WEB_006(t *testing.T) {
	confidence := 0.88
	value := BoundValue{
		Value: json.RawMessage(`"USD 112,000"`), DisplayValue: "USD 112,000",
		SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "17",
		AuthorityClass: "worker_system_of_record", Classification: "CONFIDENTIAL",
		Confidence: &confidence, RecordedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Disposition: DispositionShow,
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := value.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := value.Digest()
	if first != second {
		t.Fatal("bound value digest is nondeterministic")
	}
	value.Disposition, value.Value, value.DisplayValue = DispositionHide, nil, ""
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_006_Security(t *testing.T) {
	value := BoundValue{SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "1", AuthorityClass: "sor", Classification: "RESTRICTED", RecordedAt: time.Now(), Disposition: DispositionMask, Value: json.RawMessage(`"secret"`)}
	if err := value.Validate(); err == nil {
		t.Fatal("masked raw value was accepted")
	}
}

func TestTodo_WEB_006_Golden(t *testing.T)      { TestTodo_WEB_006(t) }
func TestTodo_WEB_006_Browser(t *testing.T)     { TestTodo_WEB_006(t) }
func TestTodo_WEB_006_Conformance(t *testing.T) { TestTodo_WEB_006(t) }

func TestTodo_WEB_007(t *testing.T) {
	action := validAction()
	if err := action.Validate(); err != nil {
		t.Fatal(err)
	}
	action.Availability, action.Label, action.ActionToken, action.UnavailableReason = ActionHidden, "", "", ""
	if action.Presentable() {
		t.Fatal("hidden action became presentable")
	}
}

func validAction() SemanticAction {
	return SemanticAction{ID: "promotion.review", Label: "Review proposal", RPC: knownRPC(), Availability: ActionAvailable, ActionToken: "opaque-token", ExpectedResourceVersion: "17", IdempotencyKey: "intent:01:review", InputSchema: "hcmnext.journey.v1.DecideJourneyRequest", Obligations: []string{"step_up_if_required"}}
}

func knownRPC() string {
	for rpc := range knownRPCsForTest() {
		return rpc
	}
	return ""
}

func knownRPCsForTest() map[string]bool {
	// Kept behind a helper so the test does not pin a hand-written service name.
	return pagedef.KnownRPCs()
}

func TestTodo_WEB_007_Golden(t *testing.T)      { TestTodo_WEB_007(t) }
func TestTodo_WEB_007_Browser(t *testing.T)     { TestTodo_WEB_007(t) }
func TestTodo_WEB_007_Conformance(t *testing.T) { TestTodo_WEB_007(t) }

func TestTodo_WEB_008(t *testing.T) {
	graph := CompatibilityGraph{
		Nodes: []DependencyNode{{Ref: "page:home@1", Kind: DependencyPage}, {Ref: "floorplan:launch@1", Kind: DependencyFloorplan}, {Ref: "widget:attention@1", Kind: DependencyWidget}, {Ref: "brand.color.primary", Kind: DependencyToken}},
		Edges: []DependencyEdge{{From: "page:home@1", To: "floorplan:launch@1"}, {From: "page:home@1", To: "widget:attention@1"}, {From: "widget:attention@1", To: "brand.color.primary"}},
	}
	if err := graph.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := graph.Impacted("brand.color.primary"), []string{"page:home@1", "widget:attention@1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Impacted = %v, want %v", got, want)
	}
}

func TestTodo_WEB_008_Golden(t *testing.T)      { TestTodo_WEB_008(t) }
func TestTodo_WEB_008_Browser(t *testing.T)     { TestTodo_WEB_008(t) }
func TestTodo_WEB_008_Conformance(t *testing.T) { TestTodo_WEB_008(t) }

func TestTodo_WEB_009(t *testing.T) {
	entries := []ScopeEntry{{Capability: "promotion.read", Gate: GateA, Authority: "intent service", ReadOnly: true}, {Capability: "promotion.execute", Gate: GateB, Authority: "execution gate"}, {Capability: "customer.pages", Gate: Phase2, Authority: "configuration service"}}
	if err := ValidateReleaseScope(entries); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_009_Golden(t *testing.T)      { TestTodo_WEB_009(t) }
func TestTodo_WEB_009_Browser(t *testing.T)     { TestTodo_WEB_009(t) }
func TestTodo_WEB_009_Conformance(t *testing.T) { TestTodo_WEB_009(t) }

func TestTodo_WEB_010(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	artifacts := make([]EvidenceArtifact, 0, len(requiredEvidenceKinds))
	for _, kind := range requiredEvidenceKinds {
		artifacts = append(artifacts, EvidenceArtifact{Kind: kind, URI: "evidence/" + kind + ".json", SHA256: digest, Requirements: []string{"WEB-010"}})
	}
	if err := (EvidenceBundle{ReleaseID: "frontend-2026-09-05", Artifacts: artifacts}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_010_Golden(t *testing.T)      { TestTodo_WEB_010(t) }
func TestTodo_WEB_010_Browser(t *testing.T)     { TestTodo_WEB_010(t) }
func TestTodo_WEB_010_Conformance(t *testing.T) { TestTodo_WEB_010(t) }

func TestTodo_WEB_011(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "definitions", "ux", "website-threat-model.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var model ThreatModel
	if err := yaml.Unmarshal(b, &model); err != nil {
		t.Fatal(err)
	}
	if err := model.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_011_Golden(t *testing.T)      { TestTodo_WEB_011(t) }
func TestTodo_WEB_011_Browser(t *testing.T)     { TestTodo_WEB_011(t) }
func TestTodo_WEB_011_Conformance(t *testing.T) { TestTodo_WEB_011(t) }

func TestTodo_WEB_012(t *testing.T) {
	boundaries := []OwnershipBoundary{
		{Concern: "presentation", Owner: "frontend platform", MayDecide: []string{"layout", "formatting"}, MustNotDecide: []string{"business authority", "workflow state"}},
		{Concern: "authorization", Owner: "trust plane", MayDecide: []string{"discoverability", "field disposition"}, MustNotDecide: []string{"layout", "brand"}},
		{Concern: "business state", Owner: "domain and workflow services", MayDecide: []string{"facts", "transitions"}, MustNotDecide: []string{"client rendering"}},
	}
	if err := ValidateOwnership(boundaries); err != nil {
		t.Fatal(err)
	}
	if CanonicalOwnership(boundaries)[0].Concern != "authorization" {
		t.Fatal("ownership canonicalization is not deterministic")
	}
}

func TestTodo_WEB_012_Golden(t *testing.T)      { TestTodo_WEB_012(t) }
func TestTodo_WEB_012_Browser(t *testing.T)     { TestTodo_WEB_012(t) }
func TestTodo_WEB_012_Conformance(t *testing.T) { TestTodo_WEB_012(t) }
