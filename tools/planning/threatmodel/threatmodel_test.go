package threatmodel

import (
	"encoding/json"
	"errors"
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

func TestThreatModel_PublicHelpersAndLoadErrors(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version() = %d", Version())
	}
	if got := (ValidationError{Field: "release", Reason: "required"}).Error(); got != "threatmodel: field release: required" {
		t.Fatalf("ValidationError.Error() = %q", got)
	}
	model, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil || Explain(model) != model.Explain() || !strings.Contains(model.Explain(), "4 release surfaces") {
		t.Fatalf("Explain = %q, package=%q, err=%v", model.Explain(), Explain(model), err)
	}
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "read fixture") {
		t.Fatalf("missing Load error = %v", err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("schema_version: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "parse fixture") {
		t.Fatalf("malformed Load error = %v", err)
	}
	unknown := filepath.Join(dir, "unknown.yaml")
	if err := os.WriteFile(unknown, []byte("schema_version: 1\nrelease: r\nsurfaces: []\nextra: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown); err == nil || !strings.Contains(err.Error(), "parse fixture") {
		t.Fatalf("unknown-field Load error = %v", err)
	}
}

func TestThreatModel_ShapeAndLinkRefusals(t *testing.T) {
	base, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Model)
		field  string
	}{
		{"schema", func(m *Model) { m.SchemaVersion = 2 }, "schema_version"},
		{"release", func(m *Model) { m.Release = " " }, "release"},
		{"no surfaces", func(m *Model) { m.Surfaces = nil }, "surfaces"},
		{"empty surface name", func(m *Model) { m.Surfaces[0].Name = "" }, "surfaces[0].name"},
		{"nonpositive revision", func(m *Model) { m.Surfaces[0].Revision = 0 }, "revision"},
		{"missing purpose", func(m *Model) { m.Surfaces[0].Purpose = "" }, "purpose"},
		{"missing asset", func(m *Model) { m.Surfaces[0].Assets = nil }, "assets"},
		{"missing asset field", func(m *Model) { m.Surfaces[0].Assets[0].Name = "" }, "assets[0]"},
		{"missing boundary", func(m *Model) { m.Surfaces[0].TrustBoundaries = nil }, "trust_boundaries"},
		{"missing boundary control", func(m *Model) { m.Surfaces[0].TrustBoundaries[0].Controls = nil }, "trust_boundaries[0]"},
		{"missing threats", func(m *Model) { m.Surfaces[0].Threats = nil }, "threats"},
		{"invalid stride", func(m *Model) { m.Surfaces[0].Threats[0].STRIDE = "unknown" }, "stride"},
		{"missing mitigation", func(m *Model) { m.Surfaces[0].Threats[0].Mitigations = nil }, "mitigations"},
		{"missing mitigation test", func(m *Model) { m.Surfaces[0].Threats[0].Mitigations[0].Tests = []string{""} }, "tests[0]"},
		{"missing acceptance", func(m *Model) { m.Surfaces[0].SecurityAcceptanceCriteria = nil }, "security_acceptance_criteria"},
		{"missing accepted gap field", func(m *Model) { m.Surfaces[0].AcceptedGaps = []AcceptedGap{{}} }, "accepted_gaps[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated, err := Load(filepath.Join("testdata", "threat-model.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&mutated)
			shapeErr := validateShape(mutated)
			var refusal ValidationError
			if !errors.As(shapeErr, &refusal) || !strings.Contains(refusal.Field, tc.field) {
				t.Fatalf("validateShape = %T %v, want field containing %q", shapeErr, shapeErr, tc.field)
			}
		})
	}

	duplicate := base
	duplicate.Surfaces = append([]Surface(nil), base.Surfaces[0], base.Surfaces[0])
	if err := validateShape(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate release surface") {
		t.Fatalf("duplicate surfaces error = %v", err)
	}
}

func TestThreatModel_DigestsAndValidateAtBoundaries(t *testing.T) {
	model, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyDigest(); err != nil {
		t.Fatal(err)
	}
	missing := model
	missing.Surfaces = append([]Surface(nil), model.Surfaces...)
	missing.Surfaces[0].Digest = ""
	if err := missing.VerifyDigest(); err == nil || !strings.Contains(err.Error(), "surfaces[0].digest") {
		t.Fatalf("missing surface digest error = %v", err)
	}
	for _, mutate := range []func(*Model){func(m *Model) { m.Surfaces[0].Digest = "forged" }, func(m *Model) { m.Digest = "" }, func(m *Model) { m.Digest = "forged" }} {
		mutated := model
		mutated.Surfaces = append([]Surface(nil), model.Surfaces...)
		mutate(&mutated)
		if err := mutated.VerifyDigest(); err == nil || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("digest mutation was accepted: %v", err)
		}
	}

	riskTable := riskTable(t)
	if err := model.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), []string{"authn", "data", "transport", "trust"}, riskTable); err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), []string{"missing"}, riskTable); err == nil || !strings.Contains(err.Error(), "missing release-surface") {
		t.Fatalf("missing required surface error = %v", err)
	}
	duplicate := model
	duplicate.Surfaces = append(append([]Surface(nil), model.Surfaces...), model.Surfaces[0])
	if err := duplicate.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), nil, riskTable); err == nil || !strings.Contains(err.Error(), "duplicate release surface") {
		t.Fatalf("duplicate ValidateAt error = %v", err)
	}

	linkModel := model
	linkModel.Surfaces = append([]Surface(nil), model.Surfaces...)
	linkModel.Surfaces[0].Threats = append([]Threat(nil), model.Surfaces[0].Threats...)
	linkModel.Surfaces[0].Threats[0].RiskID = "RISK-NOT-DECLARED"
	linkModel = withDigests(linkModel)
	if err := linkModel.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), nil, riskTable); err == nil || !strings.Contains(err.Error(), "absent from riskbinding") {
		t.Fatalf("unknown risk link error = %v", err)
	}
	unbound := riskbinding.Table{Risks: []riskbinding.Risk{{ID: "RISK-UNBOUND"}}}
	linkModel = model
	linkModel.Surfaces = append([]Surface(nil), model.Surfaces...)
	linkModel.Surfaces[0].Threats = append([]Threat(nil), model.Surfaces[0].Threats...)
	linkModel.Surfaces[0].Threats[0].RiskID = "RISK-UNBOUND"
	linkModel = withDigests(linkModel)
	if err := linkModel.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), nil, unbound); err == nil || !strings.Contains(err.Error(), "no binding row") {
		t.Fatalf("unbound risk link error = %v", err)
	}
}

func TestThreatModel_AcceptedGapDatesAndRepositoryErrors(t *testing.T) {
	model, err := Load(filepath.Join("testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	riskTable := riskTable(t)
	riskTable.Risks = append(riskTable.Risks, riskbinding.Risk{ID: "RISK-GAP"})
	riskTable.Bindings = append(riskTable.Bindings, riskbinding.Binding{RiskID: "RISK-GAP"})
	for _, tc := range []struct {
		name                    string
		reviewed, expires, want string
	}{
		{"future review", "2026-09-06", "2026-12-31", "review date is in the future"},
		{"expired", "2026-09-01", "2026-09-04", "release exception is expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := model
			mutated.Surfaces = append([]Surface(nil), model.Surfaces...)
			mutated.Surfaces[0].AcceptedGaps = []AcceptedGap{{ID: "GAP-1", Description: "reviewed gap", RiskID: "RISK-GAP", Exception: Exception{Reviewer: "reviewer", Reviewed: tc.reviewed, Expires: tc.expires, Rationale: "compensating control"}}}
			mutated = withDigests(mutated)
			err := mutated.ValidateAt(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), nil, riskTable)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateAt = %v, want %q", err, tc.want)
			}
		})
	}
	root := repoRoot(t)
	if _, err := ValidateRepository(root, "missing.yaml", layoutPath, riskPath, time.Time{}); err == nil || !strings.Contains(err.Error(), "read fixture") {
		t.Fatalf("ValidateRepository missing fixture error = %v", err)
	}
}
