package iac

import (
	"strings"
	"testing"
)

func validSelection() Selection {
	a := func(name string) Artifact {
		return Artifact{Name: name, Version: "1.2.3", Digest: "sha256:" + strings.Repeat("a", 64), Source: "https://source.example.test/" + name, License: "Apache-2.0", VulnerabilityRef: "scan-2026-09", Owner: "platform", UpdateSLA: "90d", ReplacementPath: "review-and-roll-forward"}
	}
	return Selection{Engine: a("engine"), Providers: []Artifact{a("provider")}, Modules: []Artifact{a("module")}, Images: []Artifact{a("image")}, State: StateAuthority{Backend: "isolated-state", KeyReference: "kms://key", Encrypted: true, Isolated: true, Locking: true, Backup: true, RestoreTested: true}, PlanRole: "planner", ApplyRole: "applier", ReviewerRole: "reviewer", SignedManifest: true, PlanVerified: true, SecretOutputFree: true, ReproducibleApply: true, DriftExplicit: true, Recovery: RecoveryProfile{PlanApplyDestroyRestoreTested: true, StateLossRunbook: "runbook-1", RestoreEvidence: "restore-1"}}
}

func TestIaCToolchainSelectionRequiresPinnedEngineProvidersStateFencingAndRecovery(t *testing.T) {
	if err := Check(validSelection()); err != nil {
		t.Fatal(err)
	}
	bad := validSelection()
	bad.Engine.Digest = "latest"
	bad.State.Encrypted = false
	bad.PlanRole = bad.ApplyRole
	bad.Recovery.RestoreEvidence = ""
	if err := Check(bad); err == nil {
		t.Fatal("unsafe selection was accepted")
	}
}

func TestTodo_IAC_013_Property(t *testing.T) {
	selection := validSelection()
	first, err := Digest(selection)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Digest(selection)
	if err != nil || first != second {
		t.Fatalf("selection digest not deterministic: %q/%q", first, second)
	}
}

func TestTodo_IAC_013_Golden(t *testing.T) {
	a, err := CanonicalJSON(validSelection())
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(validSelection())
	if err != nil || string(a) != string(b) {
		t.Fatal("canonical selection changed between identical inputs")
	}
}

func TestTodo_IAC_013_Integration(t *testing.T) {
	report := Qualify(validSelection())
	if !report.OK() || !strings.HasPrefix(report.SelectionDigest, "sha256:") {
		t.Fatalf("qualified report = %+v", report)
	}
}

func TestTodo_IAC_013_Fault(t *testing.T) {
	selection := validSelection()
	selection.State.Locking = false
	if err := Check(selection); err == nil {
		t.Fatal("unlocked state was accepted")
	}
}

func TestTodo_IAC_013_Security(t *testing.T) {
	selection := validSelection()
	selection.SecretOutputFree = false
	if err := Check(selection); err == nil {
		t.Fatal("secret-bearing output was accepted")
	}
}

func TestTodo_IAC_013_Conformance(t *testing.T) {
	placeholder := Placeholder()
	if err := Check(placeholder); err == nil {
		t.Fatal("placeholder decision unexpectedly qualified")
	}
	if !strings.Contains(placeholder.Engine.Name, "PLACEHOLDER") {
		t.Fatal("placeholder did not label the human-supplied engine")
	}
}

func TestTodo_IAC_013_Recovery(t *testing.T) {
	selection := validSelection()
	selection.State.RestoreTested = false
	if err := Check(selection); err == nil {
		t.Fatal("untested restore was accepted")
	}
}

func TestTodo_IAC_013_Mutation(t *testing.T) {
	base, err := Digest(validSelection())
	if err != nil {
		t.Fatal(err)
	}
	mutated := validSelection()
	mutated.ApplyRole = "different-applier"
	changed, err := Digest(mutated)
	if err != nil || base == changed {
		t.Fatalf("material role mutation did not change digest: %q/%q", base, changed)
	}
}
