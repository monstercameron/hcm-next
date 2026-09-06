package abuse_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

func TestRegistryPublishIsIdempotent(t *testing.T) {
	reg := abuse.NewRegistry()
	v := validDetectorVersion()

	first, err := reg.Publish(v)
	if err != nil {
		t.Fatalf("first publish rejected: %v", err)
	}
	second, err := reg.Publish(v)
	if err != nil {
		t.Fatalf("idempotent republish of the identical body was rejected: %v", err)
	}
	fd, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if fd != sd {
		t.Fatalf("idempotent republish returned a different receipt: %q vs %q", fd, sd)
	}
}

func TestRegistryPublishRefusesChangedBodyUnderSameVersion(t *testing.T) {
	reg := abuse.NewRegistry()
	v := validDetectorVersion()
	if _, err := reg.Publish(v); err != nil {
		t.Fatalf("first publish rejected: %v", err)
	}

	changed := v
	changed.DeclaredOutputs = []string{"CONTAIN"}
	if _, err := reg.Publish(changed); err == nil || !abuse.IsRegistryRejected(err) {
		t.Fatalf("changed body under the same (detector id, semver) was not refused: %v", err)
	}

	// The originally published version must remain intact and active.
	active, ok := reg.Active(v.DetectorID)
	if !ok {
		t.Fatal("original publication was lost after the refused republish")
	}
	origDigest, err := v.Digest()
	if err != nil {
		t.Fatal(err)
	}
	activeDigest, err := active.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if origDigest != activeDigest {
		t.Fatal("original detector version was mutated by the refused republish")
	}
}

func TestRegistryHasExactlyOneActiveVersionPerDetector(t *testing.T) {
	reg := abuse.NewRegistry()
	v1 := validDetectorVersion()
	v2 := v1
	v2.Semver = "2.0.0"

	if _, err := reg.Publish(v1); err != nil {
		t.Fatalf("publish v1 rejected: %v", err)
	}
	if _, err := reg.Publish(v2); err != nil {
		t.Fatalf("publish v2 rejected: %v", err)
	}

	active, ok := reg.Active(v1.DetectorID)
	if !ok {
		t.Fatal("no active version for detector after two publications")
	}
	if active.Semver != v2.Semver {
		t.Fatalf("active version = %s, want the most recently published %s", active.Semver, v2.Semver)
	}

	// The superseded version is still on file (audit/Explain), it is just
	// no longer the one Active() returns.
	if _, ok := reg.Get(v1.DetectorID, v1.Semver); !ok {
		t.Fatal("superseded detector version was discarded rather than retained for audit")
	}
}

func TestRegistryPublishRejectsInvalidDetectorVersion(t *testing.T) {
	reg := abuse.NewRegistry()
	v := validDetectorVersion()
	v.DeclaredInputs = nil
	if _, err := reg.Publish(v); err == nil || !abuse.IsRegistryRejected(err) {
		t.Fatalf("invalid detector version was published: %v", err)
	}
}

func TestRegistryDifferentDetectorsDoNotInterfere(t *testing.T) {
	reg := abuse.NewRegistry()
	a := validDetectorVersion()
	b := validDetectorVersion()
	b.DetectorID = "bulk-export-detector"
	b.DeclaredInputs = []abuse.SignalKind{abuse.SignalKindBulkExport}

	if _, err := reg.Publish(a); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Publish(b); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Active(a.DetectorID); !ok {
		t.Fatal("detector a lost its active version")
	}
	if _, ok := reg.Active(b.DetectorID); !ok {
		t.Fatal("detector b lost its active version")
	}
}
