package designownership

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func ownershipFixture() (accepted []string, snap Snapshot) {
	accepted = []string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1"}
	snap = Snapshot{
		Designs: []DesignRef{
			{Definition: "hcmnext.t.alpha/v1", Intent: "Alpha", Engines: []string{"owned-engine", "wild-engine"}, Phase: "P0"},
			{Definition: "hcmnext.t.beta/v1", Intent: "Beta", Engines: []string{"postgres-direct"}, Phase: "GATE_A"},
		},
		EngineOwners: map[string]EngineOwner{
			"owned-engine": {Engine: "owned-engine", Package: "internal/engines/owned", HasVersion: true, HasExplain: true},
			"stale-engine": {Engine: "stale-engine", Package: "internal/engines/stale", HasVersion: false, HasExplain: true},
		},
		EntityOwners: map[string]EntityOwner{
			"Proposal": {Entity: "Proposal", Owner: "GOVERNANCE", Source: "model-sources.yaml#ProposalRevision/v1"},
		},
		DescriptorEntities: map[string][]string{
			"hcmnext.t.alpha/v1": {"Proposal", "Ghost"},
			"hcmnext.t.beta/v1":  {"Proposal"},
		},
		DescriptorProperties: map[string][]string{
			"hcmnext.t.alpha/v1": {"proposal_id"},
			"hcmnext.t.beta/v1":  {"proposal_id"},
		},
		DescriptorAuthorities: map[string][]string{
			"hcmnext.t.alpha/v1": {"tenant_policy"},
			"hcmnext.t.beta/v1":  {"postgres-primary"},
		},
	}
	return accepted, snap
}

func findingCodes(findings []Finding, definition string) map[string]int {
	codes := make(map[string]int)
	for _, finding := range findings {
		if finding.Definition == definition {
			codes[finding.Code]++
		}
	}
	return codes
}

func candidateKinds(candidates []Candidate, kind string) []Candidate {
	var out []Candidate
	for _, candidate := range candidates {
		if candidate.Kind == kind {
			out = append(out, candidate)
		}
	}
	return out
}

// TestWorkflowDesignReferencesResolveToSingleSemanticOwners is the
// WF-DISC-009 primary oracle: owned references resolve exactly once,
// and every missing contract emits an atomic todo candidate blocking
// CONTRACTED maturity.
func TestWorkflowDesignReferencesResolveToSingleSemanticOwners(t *testing.T) {
	accepted, snap := ownershipFixture()
	ownership, findings := Compile(accepted, snap)
	if len(findingCodes(findings, "hcmnext.t.alpha/v1")) == 0 {
		t.Fatal("alpha gaps accepted silently")
	}
	byRef := make(map[string]Resolution)
	for _, resolution := range ownership.Resolutions {
		key := resolution.Kind + "\x00" + resolution.Name
		if _, dup := byRef[key]; dup {
			t.Fatalf("reference %s resolved twice", key)
		}
		byRef[key] = resolution
		if resolution.Owner == "" {
			t.Errorf("resolution %+v names no owner", resolution)
		}
	}
	owned, ok := byRef["engine\x00owned-engine"]
	if !ok || owned.Owner != "internal/engines/owned" {
		t.Errorf("owned engine unresolved: %+v", ownership.Resolutions)
	}
	if _, ok := byRef["entity\x00Proposal"]; !ok {
		t.Errorf("owned entity unresolved: %+v", ownership.Resolutions)
	}
	if codes := findingCodes(findings, "hcmnext.t.alpha/v1"); codes[UnownedEngine] == 0 {
		t.Errorf("wild engine accepted: %+v", findings)
	}
	if codes := findingCodes(findings, "hcmnext.t.alpha/v1"); codes[UnownedEntity] == 0 {
		t.Errorf("ghost entity accepted: %+v", findings)
	}
	if codes := findingCodes(findings, "hcmnext.t.alpha/v1"); codes[UnownedProperty] == 0 {
		t.Errorf("unbound property accepted: %+v", findings)
	}
	if codes := findingCodes(findings, "hcmnext.t.beta/v1"); codes[DirectAdapterCall] == 0 {
		t.Errorf("adapter engine accepted: %+v", findings)
	}
	if codes := findingCodes(findings, "hcmnext.t.beta/v1"); codes[PhysicalOwner] == 0 {
		t.Errorf("physical authority accepted: %+v", findings)
	}
	engines := candidateKinds(ownership.Candidates, "engine")
	if len(engines) != 1 || engines[0].Name != "wild-engine" {
		t.Errorf("engine candidates = %+v, want exactly wild-engine", engines)
	}
	entities := candidateKinds(ownership.Candidates, "entity")
	if len(entities) != 1 || entities[0].Name != "Ghost" {
		t.Errorf("entity candidates = %+v, want exactly Ghost", entities)
	}
	for _, candidate := range ownership.Candidates {
		if candidate.ID == "" || candidate.Owner == "" || candidate.Phase == "" || candidate.Red == "" || candidate.Green == "" {
			t.Errorf("candidate %+v lacks an exact oracle", candidate)
		}
		if candidate.Blocks != "CONTRACTED" {
			t.Errorf("candidate %+v blocks %q instead of CONTRACTED", candidate, candidate.Blocks)
		}
	}
	ids := make(map[string]bool)
	for _, candidate := range ownership.Candidates {
		if ids[candidate.ID] {
			t.Errorf("duplicate candidate id %s", candidate.ID)
		}
		ids[candidate.ID] = true
	}
	reverse := make(map[string][]string)
	for _, resolution := range ownership.Resolutions {
		reverse[resolution.Owner] = append(reverse[resolution.Owner], resolution.Name)
	}
	for _, candidate := range ownership.Candidates {
		reverse["candidate:"+candidate.Name] = candidate.Refs
	}
	if len(reverse["internal/engines/owned"]) != 1 || len(reverse["candidate:wild-engine"]) != 1 {
		t.Errorf("reverse index owner->intents broken: %+v", reverse)
	}
	if ownership.Digest == "" {
		t.Error("ownership carries no digest")
	}
	again, _ := Compile(accepted, snap)
	if again.Digest != ownership.Digest {
		t.Error("ownership digest not deterministic")
	}
}

func TestTodo_WF_DISC_009_Property(t *testing.T) {
	t.Run("broken engine contract blocks", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		owner := snap.EngineOwners["owned-engine"]
		owner.HasVersion = false
		snap.EngineOwners["owned-engine"] = owner
		_, findings := Compile(accepted, snap)
		if findingCodes(findings, "hcmnext.t.alpha/v1")[EngineContract] == 0 {
			t.Errorf("versionless engine accepted: %+v", findings)
		}
	})
	t.Run("registered engine resolves silently", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.Designs[0].Engines = []string{"owned-engine"}
		snap.DescriptorEntities["hcmnext.t.alpha/v1"] = []string{"Proposal"}
		snap.DescriptorProperties["hcmnext.t.alpha/v1"] = nil
		_, findings := Compile(accepted, snap)
		if codes := findingCodes(findings, "hcmnext.t.alpha/v1"); len(codes) > 0 {
			t.Errorf("bound design raised findings: %+v", findings)
		}
	})
	t.Run("unknown definition stays visible", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.Designs = append(snap.Designs, DesignRef{Definition: "hcmnext.t.rogue/v1", Intent: "Rogue", Engines: []string{"owned-engine"}, Phase: "P0"})
		_, findings := Compile(accepted, snap)
		if findingCodes(findings, "hcmnext.t.rogue/v1")[UnknownDefinition] == 0 {
			t.Errorf("rogue design accepted: %+v", findings)
		}
	})
	t.Run("empty snapshot fails nothing silently", func(t *testing.T) {
		ownership, findings := Compile([]string{"hcmnext.t.alpha/v1"}, Snapshot{})
		if len(findings) != 0 {
			t.Errorf("empty snapshot raised findings: %+v", findings)
		}
		if ownership.Digest == "" {
			t.Error("empty ownership carries no digest")
		}
	})
}

func TestTodo_WF_DISC_009_Golden(t *testing.T) {
	accepted, snap := ownershipFixture()
	ownership, findings := Compile(accepted, snap)
	got, err := MarshalOwnership(ownership, findings)
	if err != nil {
		t.Fatal(err)
	}
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_009_Security(t *testing.T) {
	t.Run("contract engine resolves once", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.Designs[0].Engines = []string{"owned-engine", "owned-engine"}
		ownership, findings := Compile(accepted, snap)
		count := 0
		for _, resolution := range ownership.Resolutions {
			if resolution.Kind == "engine" && resolution.Name == "owned-engine" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("owned engine resolved %d times: %+v", count, findings)
		}
	})
	t.Run("candidate ids resist collision", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.Designs = append(snap.Designs,
			DesignRef{Definition: "hcmnext.t.beta/v1", Intent: "Beta2", Engines: []string{"wild-engine"}, Phase: "GATE_A"})
		ownership, _ := Compile(accepted, snap)
		ids := make(map[string]int)
		for _, candidate := range ownership.Candidates {
			ids[candidate.ID]++
		}
		for id, count := range ids {
			if count > 1 {
				t.Errorf("candidate id %s emitted %d times", id, count)
			}
		}
		wild := candidateKinds(ownership.Candidates, "engine")
		refs := 0
		for _, candidate := range wild {
			if candidate.Name == "wild-engine" {
				refs += len(candidate.Refs)
			}
		}
		if refs != 2 {
			t.Errorf("wild-engine candidate refs = %d, want both consuming definitions", refs)
		}
	})
	t.Run("physical patterns do not match semantics", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.DescriptorAuthorities["hcmnext.t.alpha/v1"] = []string{"tenant_policy", "postgres-replica"}
		_, findings := Compile(accepted, snap)
		if findingCodes(findings, "hcmnext.t.alpha/v1")[PhysicalOwner] == 0 {
			t.Errorf("provider authority accepted: %+v", findings)
		}
	})
}

func TestTodo_WF_DISC_009_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, accepted, err := LoadSnapshot(root)
	if err != nil {
		t.Fatalf("load live snapshot: %v", err)
	}
	if len(accepted) != 14 {
		t.Fatalf("accepted definitions = %d, want 14", len(accepted))
	}
	first, firstFindings := Compile(accepted, snap)
	second, _ := Compile(accepted, snap)
	if first.Digest != second.Digest {
		t.Fatal("live ownership digest not deterministic")
	}
	for _, finding := range firstFindings {
		switch finding.Code {
		case UnownedEngine, EngineContract, DirectAdapterCall, UnownedEntity, UnownedProperty, PhysicalOwner, UnknownDefinition:
		default:
			t.Fatalf("unknown code %q", finding.Code)
		}
		if finding.Definition == "" || finding.Name == "" {
			t.Fatalf("unlocated finding: %+v", finding)
		}
	}
	covered := make(map[string]bool)
	for _, resolution := range first.Resolutions {
		if resolution.Owner == "" {
			t.Errorf("resolution %+v names no owner", resolution)
		}
		covered[resolution.Kind+"\x00"+resolution.Name] = true
	}
	for _, candidate := range first.Candidates {
		if candidate.Blocks != "CONTRACTED" || candidate.Red == "" || candidate.Green == "" {
			t.Errorf("candidate %+v cannot block maturity", candidate)
		}
	}
	byDef := make(map[string][]string)
	for _, design := range snap.Designs {
		byDef[design.Definition] = append(byDef[design.Definition], design.Engines...)
	}
	for def, engines := range byDef {
		for _, engine := range engines {
			if !covered["engine\x00"+engine] && !candidateCovers(first.Candidates, "engine", engine, def) {
				t.Errorf("%s engine %s neither resolved nor candidate-covered", def, engine)
			}
		}
		for _, entity := range snap.DescriptorEntities[def] {
			if !covered["entity\x00"+entity] && !candidateCovers(first.Candidates, "entity", entity, def) {
				t.Errorf("%s entity %s neither resolved nor candidate-covered", def, entity)
			}
		}
		for _, property := range snap.DescriptorProperties[def] {
			if !covered["property\x00"+property] && !candidateCovers(first.Candidates, "property", property, def) {
				t.Errorf("%s property %s neither resolved nor candidate-covered", def, property)
			}
		}
	}
	counts := make(map[string]int)
	for _, finding := range firstFindings {
		counts[finding.Code]++
	}
	t.Logf("live ownership: %d resolutions, %d candidates, findings %v", len(first.Resolutions), len(first.Candidates), counts)
}

func candidateCovers(candidates []Candidate, kind, name, def string) bool {
	for _, candidate := range candidates {
		if candidate.Kind != kind || candidate.Name != name {
			continue
		}
		for _, ref := range candidate.Refs {
			if ref == def {
				return true
			}
		}
	}
	return false
}

func TestTodo_WF_DISC_009_Mutation(t *testing.T) {
	accepted, snap := ownershipFixture()
	base, _ := Compile(accepted, snap)
	t.Run("dropping the owner emits a candidate", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		delete(snap.EngineOwners, "owned-engine")
		_, findings := Compile(accepted, snap)
		if findingCodes(findings, "hcmnext.t.alpha/v1")[UnownedEngine] == 0 {
			t.Errorf("ownerless engine accepted: %+v", findings)
		}
	})
	t.Run("adapter engine is forbidden, not unowned", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.Designs[0].Engines = []string{"owned-engine", "s3-bucket"}
		_, findings := Compile(accepted, snap)
		codes := findingCodes(findings, "hcmnext.t.alpha/v1")
		if codes[DirectAdapterCall] == 0 {
			t.Errorf("adapter engine accepted: %+v", findings)
		}
		if codes[UnownedEngine] > 0 {
			t.Errorf("adapter engine double-reported as unowned: %+v", findings)
		}
	})
	t.Run("ownership change moves the digest", func(t *testing.T) {
		accepted, snap := ownershipFixture()
		snap.EntityOwners["Proposal"] = EntityOwner{Entity: "Proposal", Owner: "CHANGED", Source: "model-sources.yaml#ProposalRevision/v1"}
		changed, _ := Compile(accepted, snap)
		if changed.Digest == base.Digest {
			t.Error("ownership change left the digest unchanged")
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}
