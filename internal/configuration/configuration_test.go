package configuration

import (
	"strings"
	"testing"
)

func seedBundle(t *testing.T) (Registry, Bundle) {
	t.Helper()
	reg := NewRegistry()
	def := Definition{Kind: "rule", Name: "overtime-cap", Source: "definitions/operations/overtime.yaml"}
	snap, err := SnapshotDefinition(def, []byte("max_hours: 12"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := reg.Assemble("overtime-rollout", []Snapshot{snap}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg, bundle
}

func TestConfigurationActivationRequiresApproval(t *testing.T) {
	reg, bundle := seedBundle(t)
	if _, err := reg.Activate(bundle.Digest, Approval{}); err == nil {
		t.Fatal("activation without approval was accepted")
	}
	approval, err := reg.Approve(bundle.Digest, "release-captains", "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	record, err := reg.Activate(bundle.Digest, approval)
	if err != nil {
		t.Fatalf("approved activation rejected: %v", err)
	}
	if record.Bundle != bundle.Digest {
		t.Fatalf("activation bound %q, want bundle %q", record.Bundle, bundle.Digest)
	}
}

func TestConfigurationActivationBindsImmutableDigest(t *testing.T) {
	reg, bundle := seedBundle(t)
	approval, err := reg.Approve(bundle.Digest, "release-captains", "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Activate("sha256:tampered", approval); err == nil {
		t.Fatal("activation with tampered digest was accepted")
	}
	foreignSnap, err := SnapshotDefinition(Definition{Kind: "rule", Name: "meal-break", Source: "definitions/operations/meals.yaml"}, []byte("minutes: 30"))
	if err != nil {
		t.Fatal(err)
	}
	foreignBundle, err := reg.Assemble("meal-rollout", []Snapshot{foreignSnap}, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := reg.Approve(foreignBundle.Digest, "release-captains", "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Activate(bundle.Digest, other); err == nil {
		t.Fatal("activation with foreign approval was accepted")
	}
}

func TestConfigurationRollbackTargetsPriorDigest(t *testing.T) {
	reg, bundle := seedBundle(t)
	first, err := reg.Approve(bundle.Digest, "release-captains", "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Activate(bundle.Digest, first); err != nil {
		t.Fatal(err)
	}
	def := Definition{Kind: "rule", Name: "overtime-cap", Source: "definitions/operations/overtime.yaml"}
	next, err := SnapshotDefinition(def, []byte("max_hours: 10"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Assemble("overtime-rollout", []Snapshot{next}, []Dependency{{Bundle: bundle.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := reg.Approve(second.Digest, "release-captains", "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Activate(second.Digest, approval); err != nil {
		t.Fatal(err)
	}
	back, err := reg.Approve(bundle.Digest, "release-captains", "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	record, err := reg.Rollback(second.Digest, bundle.Digest, back)
	if err != nil {
		t.Fatalf("approved rollback rejected: %v", err)
	}
	if record.Bundle != bundle.Digest {
		t.Fatalf("rollback bound %q, want prior %q", record.Bundle, bundle.Digest)
	}
	if _, err := reg.Rollback(second.Digest, bundle.Digest, Approval{}); err == nil {
		t.Fatal("rollback without approval was accepted")
	}
}

func TestConfigurationValidationRejectsUnknownBundle(t *testing.T) {
	reg, _ := seedBundle(t)
	if findings := reg.Validate("sha256:missing"); len(findings) == 0 {
		t.Fatal("unknown bundle validated clean")
	}
}

func TestConfigurationDiffIsDeterministic(t *testing.T) {
	reg, bundle := seedBundle(t)
	a := reg.Diff(bundle.Digest, bundle.Digest)
	b := reg.Diff(bundle.Digest, bundle.Digest)
	if len(a) != 0 || len(a) != len(b) {
		t.Fatalf("identical digests differ: %+v vs %+v", a, b)
	}
	def := Definition{Kind: "rule", Name: "overtime-cap", Source: "definitions/operations/overtime.yaml"}
	next, err := SnapshotDefinition(def, []byte("max_hours: 10"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Assemble("overtime-rollout", []Snapshot{next}, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes := reg.Diff(bundle.Digest, second.Digest)
	if len(changes) == 0 {
		t.Fatal("changed bundle produced no diff")
	}
	for _, c := range changes {
		if strings.TrimSpace(string(c)) == "" {
			t.Fatal("empty diff entry")
		}
	}
}
