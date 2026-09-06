package refdata

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var refdataAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
var refdataDigest = strings.Repeat("a", 64)

func releaseFixture(t *testing.T, version string) Release {
	t.Helper()
	mandatory, err := NewMember("US", "United States", true, false, refdataAt, time.Time{}, map[string]string{"alpha": "A"})
	if err != nil {
		t.Fatal(err)
	}
	optional, err := NewMember("XX", "Example", false, false, refdataAt, time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := NewRelease(Release{
		DatasetID: "countries", Version: version,
		Source:       SourceEvidence{SourceRef: "iso", SourceVersion: "2026", SourceDigest: refdataDigest, SignatureRef: "sig:iso:2026", LicenseRef: "license:iso", Coverage: "country-codes", RetrievedAt: refdataAt},
		SchemaDigest: refdataDigest, Applicability: []string{"global", "tenant"}, Members: []Member{optional, mandatory},
		ConsumerRefs: []string{"calendar", "worker-location"}, AffectedIntents: []string{"promotion", "leave"}, EffectiveFrom: refdataAt, KnownAt: refdataAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	validation, err := ValidateRelease(release, refdataAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	validated, err := MarkValidated(release, validation)
	if err != nil {
		t.Fatal(err)
	}
	published, err := PublishRelease(validated, validation, refdataAt.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return published
}

func adoptFixture(t *testing.T, current *Adoption, release Release, rollback bool) Adoption {
	t.Helper()
	req := AdoptionRequest{TenantID: "tenant-a", EffectiveAt: refdataAt.Add(24 * time.Hour), Actor: "operator-a", RolloutDigest: "rollout:" + release.Digest, ImpactRefs: release.AffectedIntents, ConsumerRefs: release.ConsumerRefs, Rollback: rollback}
	a, err := Adopt(current, release, req, refdataAt.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestReferenceDatasetReleasePinsSourceValidityAdoptionImpactAndRollback is
// REFDATA-001's primary lifecycle proof.
func TestReferenceDatasetReleasePinsSourceValidityAdoptionImpactAndRollback(t *testing.T) {
	first := releaseFixture(t, "1.0.0")
	second := releaseFixture(t, "2.0.0")
	firstAdoption := adoptFixture(t, nil, first, false)
	secondAdoption := adoptFixture(t, &firstAdoption, second, false)
	if secondAdoption.PreviousVersion != first.Version || secondAdoption.ReleaseDigest != second.Digest || secondAdoption.Event != EventAdopted {
		t.Fatalf("second adoption lost pin or impact evidence: %+v", secondAdoption)
	}
	rolledBack, err := Rollback(secondAdoption, first, "operator-a", refdataAt.Add(48*time.Hour), refdataAt.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Event != EventRollback || rolledBack.PreviousVersion != second.Version || rolledBack.ReleaseDigest != first.Digest {
		t.Fatalf("rollback = %+v", rolledBack)
	}
	if err := ValidateAdoption(rolledBack, first); err != nil {
		t.Fatalf("rollback validation: %v", err)
	}
	if pin := Pin(secondAdoption); pin.ReleaseDigest != second.Digest || pin.Version != second.Version {
		t.Fatalf("historical execution pin changed: %+v", pin)
	}
	if Explain(first) == "" {
		t.Fatal("release explanation is empty")
	}
}

func TestTodo_REFDATA_001_Property(t *testing.T) {
	a := releaseFixture(t, "1.0.0")
	b := releaseFixture(t, "1.0.0")
	if a.Digest != b.Digest {
		t.Fatalf("equivalent releases have different digests: %s != %s", a.Digest, b.Digest)
	}
	if a.Members[0].Attributes != nil {
		a.Members[0].Attributes["mutated"] = "caller"
	}
	if b.Members[0].Attributes["mutated"] != "" {
		t.Fatal("release retained an aliased member map")
	}
}

func TestTodo_REFDATA_001_Golden(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	if len(r.Digest) != 64 || len(r.Members[0].Digest) != 64 {
		t.Fatalf("release digest is not sha256 content evidence: %+v", r)
	}
	if r.Source.SourceVersion == "" || r.Source.LicenseRef == "" || len(r.AffectedIntents) != 2 {
		t.Fatalf("source/impact evidence missing: %+v", r)
	}
}

func TestTodo_REFDATA_001_Integration(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	a := adoptFixture(t, nil, r, false)
	if got := Pin(a); got.ReleaseDigest != r.Digest || got.DatasetID != r.DatasetID || !got.EffectiveAt.Equal(a.EffectiveAt) {
		t.Fatalf("adoption did not produce an execution pin: %+v", got)
	}
	override := AdoptionRequest{TenantID: "tenant-b", EffectiveAt: refdataAt, Actor: "steward", RolloutDigest: "rollout", Overrides: []Override{{Code: "XX", Value: "Example (tenant)", Reason: "local naming", ApprovedBy: "steward"}}, ImpactRefs: r.AffectedIntents}
	if _, err := Adopt(nil, r, override, refdataAt); err != nil {
		t.Fatalf("permitted tenant override: %v", err)
	}
}

func TestTodo_REFDATA_001_Fault(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	r.Members[0].Digest = "changed"
	if _, err := NewRelease(r); !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("tampered member accepted: %v", err)
	}
	bad := releaseFixture(t, "1.1.0")
	bad.Members = append(bad.Members, bad.Members[0])
	if _, err := NewRelease(bad); !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("duplicate member accepted: %v", err)
	}
}

func TestTodo_REFDATA_001_Security(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	if _, err := Adopt(nil, r, AdoptionRequest{TenantID: "tenant-a", EffectiveAt: refdataAt, Actor: "operator", RolloutDigest: "rollout", Overrides: []Override{{Code: "US", Value: "", Reason: "weaken", ApprovedBy: "operator"}}}, refdataAt); !errors.Is(err, ErrMandatoryOverride) {
		t.Fatalf("mandatory override = %v", err)
	}
	withdrawn, err := WithdrawRelease(r, refdataAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(nil, withdrawn, AdoptionRequest{TenantID: "tenant-a", EffectiveAt: refdataAt, Actor: "operator", RolloutDigest: "rollout"}, refdataAt); !errors.Is(err, ErrReleaseWithdrawn) {
		t.Fatalf("withdrawn release adoption = %v", err)
	}
}

func TestTodo_REFDATA_001_Conformance(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	if _, err := Adopt(nil, r, AdoptionRequest{TenantID: "tenant-a", EffectiveAt: refdataAt, Actor: "operator"}, refdataAt); !errors.Is(err, ErrInvalidAdoption) {
		t.Fatalf("missing rollout gate accepted: %v", err)
	}
	if _, err := Adopt(nil, r, AdoptionRequest{TenantID: "tenant-a", EffectiveAt: refdataAt.Add(-time.Hour), Actor: "operator", RolloutDigest: "rollout"}, refdataAt); !errors.Is(err, ErrInvalidAdoption) {
		t.Fatalf("pre-effective adoption accepted: %v", err)
	}
}

func TestTodo_REFDATA_001_Recovery(t *testing.T) {
	first := releaseFixture(t, "1.0.0")
	second := releaseFixture(t, "2.0.0")
	firstAdoption := adoptFixture(t, nil, first, false)
	secondAdoption := adoptFixture(t, &firstAdoption, second, false)
	recovered, err := Rollback(secondAdoption, first, "repair-operator", refdataAt.Add(72*time.Hour), refdataAt.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if firstAdoption.Version != first.Version || secondAdoption.Version != second.Version || recovered.PreviousVersion != second.Version {
		t.Fatal("rollback mutated historical adoption records")
	}
}

func TestTodo_REFDATA_001_Mutation(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	mutated := r
	mutated.Source.SourceVersion = "other"
	if err := ValidateAdoption(Adoption{DatasetID: r.DatasetID, Version: r.Version, ReleaseDigest: r.Digest, Digest: "not-a-digest"}, mutated); err == nil {
		t.Fatal("changed source content was accepted as the original adoption")
	}
}

func FuzzTodo_REFDATA_001(f *testing.F) {
	f.Add("countries", "1.0.0", "label")
	f.Fuzz(func(t *testing.T, datasetID, version, label string) {
		member, err := NewMember("code", label, false, false, refdataAt, time.Time{}, nil)
		if err != nil {
			return
		}
		_, _ = NewRelease(Release{DatasetID: datasetID, Version: version, Source: SourceEvidence{SourceRef: "source", SourceVersion: "1", SourceDigest: refdataDigest, SignatureRef: "sig", LicenseRef: "license", Coverage: "coverage", RetrievedAt: refdataAt}, SchemaDigest: refdataDigest, Applicability: []string{"global"}, Members: []Member{member}, ConsumerRefs: []string{"consumer"}, AffectedIntents: []string{"intent"}, EffectiveFrom: refdataAt, KnownAt: refdataAt})
	})
}

func TestTodo_REFDATA_001_ModelBased(t *testing.T) {
	r := releaseFixture(t, "1.0.0")
	first := adoptFixture(t, nil, r, false)
	if _, err := Adopt(&first, r, AdoptionRequest{TenantID: "tenant-a", EffectiveAt: refdataAt, Actor: "operator", RolloutDigest: "rollout"}, refdataAt); !errors.Is(err, ErrDuplicateAdoption) {
		t.Fatalf("same state replay = %v", err)
	}
}
