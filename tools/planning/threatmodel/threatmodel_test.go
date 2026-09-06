package threatmodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/planning/riskbinding"
)

func TestTodo_SECARCH_006(t *testing.T) {
	root := repoRoot(t)
	model, err := ValidateRepository(root, fixturePath, layoutPath, riskPath, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(model.Surfaces); got != 4 {
		t.Fatalf("surface count = %d, want 4", got)
	}
	if model.Digest == "" || model.Explain() == "" {
		t.Fatal("validated model must have a digest and audit-safe explanation")
	}
}

func TestTodo_SECARCH_006_Golden(t *testing.T) {
	model, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "threat-model.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Release  string `json:"release"`
		Surfaces []struct {
			Name       string `json:"name"`
			Revision   int    `json:"revision"`
			Threats    int    `json:"threats"`
			Acceptance int    `json:"acceptance_criteria"`
			Gaps       int    `json:"accepted_gaps"`
		} `json:"surfaces"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if want.Release != model.Release || len(want.Surfaces) != len(model.Surfaces) {
		t.Fatalf("golden header = %+v, model release=%q surfaces=%d", want, model.Release, len(model.Surfaces))
	}
	for i, surface := range model.Surfaces {
		got := want.Surfaces[i]
		if got.Name != surface.Name || got.Revision != surface.Revision || got.Threats != len(surface.Threats) || got.Acceptance != len(surface.SecurityAcceptanceCriteria) || got.Gaps != len(surface.AcceptedGaps) {
			t.Fatalf("golden surface %d = %+v, model = %+v", i, got, surface)
		}
	}
}

func TestTodo_SECARCH_006_Security(t *testing.T) {
	model, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mutated := model
	mutated.Surfaces = append([]Surface(nil), model.Surfaces...)
	mutated.Surfaces[0].Threats = append([]Threat(nil), model.Surfaces[0].Threats...)
	mutated.Surfaces[0].Threats[0].RiskID = "RISK-999"
	mutated = withDigests(mutated)
	if err := mutated.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), []string{"authn", "data", "transport", "trust"}, riskTable(t)); err == nil || !strings.Contains(err.Error(), "risk_id") {
		t.Fatalf("risk substitution error = %v, want typed risk_id refusal", err)
	}
	if strings.Contains(model.Explain(), "RISK-") || strings.Contains(model.Explain(), "authn") {
		t.Fatalf("Explain exposed an identifier: %q", model.Explain())
	}
}

func TestTodo_SECARCH_006_Integration(t *testing.T) {
	root := repoRoot(t)
	if _, err := ValidateRepository(root, fixturePath, layoutPath, riskPath, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_SECARCH_006_Conformance(t *testing.T) {
	root := repoRoot(t)
	model, err := ValidateRepository(root, fixturePath, layoutPath, riskPath, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, surface := range model.Surfaces {
		seen[surface.Name] = true
	}
	for _, name := range []string{"transport", "authn", "trust", "data"} {
		if !seen[name] {
			t.Fatalf("layout release surface %q has no record", name)
		}
	}
}

func TestTodo_SECARCH_006_Mutation(t *testing.T) {
	model, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	model.Surfaces[0].Purpose = "mutated"
	if err := model.VerifyDigest(); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("mutation error = %v, want digest refusal", err)
	}
	model, err = Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateAt(time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), []string{"authn", "data", "transport", "trust"}, riskTable(t)); err == nil || !strings.Contains(err.Error(), "expires") {
		t.Fatalf("expired exception error = %v, want expiry refusal", err)
	}
}

const (
	fixturePath = "tools/planning/threatmodel/testdata/threat-model.yaml"
	layoutPath  = "definitions/architecture/repository-layout.yaml"
	riskPath    = "tools/planning/riskbinding/testdata/risk-table.json"
)

func riskTable(t *testing.T) riskbinding.Table {
	t.Helper()
	table, err := riskbinding.Load(filepath.Join(repoRoot(t), riskPath))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve package path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}
