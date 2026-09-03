package gateevidence

import (
	"bytes"
	"testing"
)

func sampleReport() Report {
	return Report{
		ManifestTodoID: "NEXT-002",
		ManifestDigest: "deadbeef",
		AsOf:           "2026-09-03",
		Decision:       GateDecisionClear,
		Findings: []Finding{
			{TodoID: "NEXT-004", Test: "TestFoo", Package: "./test/bootstrap", Verdict: VerdictOK, Detail: "2026-09-03 PASS"},
			{TodoID: "NEXT-005", Test: "TestBar", Package: "./test/bootstrap", Verdict: VerdictOK, Detail: "2026-09-03 PASS"},
		},
	}
}

func TestRenderMarkdownIsDeterministic(t *testing.T) {
	r := sampleReport()
	a := RenderMarkdown(r)
	b := RenderMarkdown(r)
	if !bytes.Equal(a, b) {
		t.Fatalf("RenderMarkdown is not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	if !bytes.Contains(a, []byte("GATE_CLEAR")) {
		t.Errorf("expected rendered markdown to contain the decision, got:\n%s", a)
	}
}

func TestRenderJSONIsDeterministic(t *testing.T) {
	r := sampleReport()
	a, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	b, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("RenderJSON is not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

func TestRenderMarkdownEscapesPipesInDetail(t *testing.T) {
	r := Report{Findings: []Finding{{TodoID: "X", Test: "T", Package: "P", Verdict: VerdictOK, Detail: "a | b"}}}
	out := RenderMarkdown(r)
	if !bytes.Contains(out, []byte(`a \| b`)) {
		t.Errorf("expected escaped pipe in table cell, got:\n%s", out)
	}
}

// TestP1AEvidenceReportArtifactsAreCurrent proves the checked-in
// definitions/planning/gates/p1a-evidence-report.{md,json} are exactly what
// compiling the real, checked-in manifest and results file produces as of
// the manifest's own signed date - the same drift-enforcement pattern
// TOOL-010 applies to generated code.
func TestP1AEvidenceReportArtifactsAreCurrent(t *testing.T) {
	m := mustLoadP1AManifest(t)
	results, err := LoadResults(repoRoot + "/definitions/planning/gates/p1a-evidence-results.json")
	if err != nil {
		t.Fatalf("LoadResults: %v", err)
	}
	report, err := Compile(m, results, CompileOptions{RepoRoot: repoRoot, Now: mustParseDate(t, "2026-09-03")})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	wantMD := RenderMarkdown(*report)
	gotMD := mustReadFile(t, repoRoot+"/definitions/planning/gates/p1a-evidence-report.md")
	if !bytes.Equal(wantMD, gotMD) {
		t.Errorf("definitions/planning/gates/p1a-evidence-report.md is stale; regenerate it from the current manifest/results (see this test for the exact bytes it must contain)")
	}

	wantJSON, err := RenderJSON(*report)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	gotJSON := mustReadFile(t, repoRoot+"/definitions/planning/gates/p1a-evidence-report.json")
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Errorf("definitions/planning/gates/p1a-evidence-report.json is stale; regenerate it from the current manifest/results")
	}
}
