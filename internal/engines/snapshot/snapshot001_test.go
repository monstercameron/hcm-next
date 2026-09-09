package snapshot_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_SNAPSHOT_001 is the PRIMARY test: a source-neutral resolve across
// a NATIVE_STATE input and an EXTERNAL_OBSERVATION input, both under one
// tenant and one known-at horizon, produces one immutable ReadSnapshot whose
// entries are exactly the ones requested, sorted, each still carrying its
// own authority untouched, plus a stable non-empty digest.
func TestTodo_SNAPSHOT_001(t *testing.T) {
	horizon := fixtureHorizon(t)
	native := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	external := validEntry(t, "connectivity.workday_compensation", fixtureTenant, horizon, snapshot.AuthorityExternalObservation)

	src := snapshot.NewFakeSource().Seed(native).Seed(external)
	requirement := snapshot.ConsistencyRequirement{Tenant: fixtureTenant, KnownAtHorizon: horizon}
	inputs := []snapshot.InputRequest{
		{Name: native.Name, RequiredAuthority: snapshot.AuthorityNativeState},
		{Name: external.Name, RequiredAuthority: snapshot.AuthorityExternalObservation},
	}

	snap, err := snapshot.Resolve(context.Background(), src, requirement, inputs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if snap.Tenant != fixtureTenant {
		t.Errorf("Tenant = %q, want %q", snap.Tenant, fixtureTenant)
	}
	if snap.KnownAtHorizon.Instant().Compare(horizon.Instant()) != 0 {
		t.Errorf("KnownAtHorizon = %s, want %s", snap.KnownAtHorizon, horizon)
	}
	if len(snap.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(snap.Entries))
	}
	if snap.Entries[0].Name > snap.Entries[1].Name {
		t.Errorf("Entries are not sorted by Name: %q before %q", snap.Entries[0].Name, snap.Entries[1].Name)
	}

	got, ok := snap.Lookup(native.Name)
	if !ok {
		t.Fatalf("Lookup(%q) not found", native.Name)
	}
	if got.Authority != snapshot.AuthorityNativeState {
		t.Errorf("native entry authority = %s, want NATIVE_STATE", got.Authority)
	}
	gotExternal, ok := snap.Lookup(external.Name)
	if !ok {
		t.Fatalf("Lookup(%q) not found", external.Name)
	}
	if gotExternal.Authority != snapshot.AuthorityExternalObservation {
		t.Errorf("external entry authority = %s, want EXTERNAL_OBSERVATION", gotExternal.Authority)
	}

	if snap.Digest == "" {
		t.Fatal("Digest is empty")
	}
	again, err := snapshot.Resolve(context.Background(), src, requirement, inputs)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if again.Digest != snap.Digest {
		t.Errorf("two resolves of the same fixtures produced different digests: %q vs %q", snap.Digest, again.Digest)
	}
}

// TestTodo_SNAPSHOT_001_Golden pins the exact digest a fixed single-entry
// fixture must produce. A change to field order, schema tag or encoding in
// entry.go or snapshot.go would move this value; that is the point of a
// golden test.
func TestTodo_SNAPSHOT_001_Golden(t *testing.T) {
	horizon := mustKnownAt(t, mustInstant(t, 2026, time.March, 1, 12, 0, 0))
	entry := snapshot.InputEntry{
		Name:      "people.worker_facts",
		Owner:     "people",
		Tenant:    "golden-tenant",
		Authority: snapshot.AuthorityNativeState,
		Source: snapshot.SourceRef{
			System:     "hcmnext.people",
			Connection: "primary",
			Ref:        "people.worker_facts",
		},
		EffectiveAt:    mustLocalDate(t, 2026, time.January, 1),
		KnownAt:        horizon,
		Revision:       mustRevision(t, "stream.people.worker_facts", 1),
		Head:           "head-1",
		Watermark:      mustRevision(t, "watermark.people.worker_facts", 1),
		Freshness:      mustRecordedAt(t, mustInstant(t, 2026, time.February, 28, 0, 0, 0)),
		Classification: snapshot.ClassificationInternal,
		Provenance: snapshot.Provenance{
			Source:      "hcmnext.people",
			EvidenceRef: "evd_people.worker_facts_r1",
			RecordedAt:  mustRecordedAt(t, mustInstant(t, 2026, time.February, 28, 0, 0, 0)),
		},
		ReferenceVersion: "v1",
	}
	requirement := snapshot.ConsistencyRequirement{Tenant: "golden-tenant", KnownAtHorizon: horizon}
	src := snapshot.NewFakeSource().Seed(entry)

	snap, err := snapshot.Resolve(context.Background(), src, requirement, []snapshot.InputRequest{{Name: entry.Name}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const wantDigest = "sha256:90e8e3bf6273815a08d422d92b810bbf9ea30f3f733b383d7e473cc0a7eff906"
	if snap.Digest != wantDigest {
		t.Fatalf("Digest = %q, want %q (pinned golden fixture)", snap.Digest, wantDigest)
	}
}

// TestTodo_SNAPSHOT_001_Security proves the three refusals SNAPSHOT-001's
// SECURITY line names: an entry lacking a required descriptor is refused, a
// tenant mismatch is refused, and an external observation cannot be
// presented to a caller that pinned NATIVE_STATE for that input.
func TestTodo_SNAPSHOT_001_Security(t *testing.T) {
	horizon := fixtureHorizon(t)
	requirement := snapshot.ConsistencyRequirement{Tenant: fixtureTenant, KnownAtHorizon: horizon}

	t.Run("entry lacking a required descriptor is refused", func(t *testing.T) {
		incomplete := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		incomplete.Owner = "" // strip one required descriptor
		src := snapshot.NewFakeSource().SeedRaw(incomplete)

		_, err := snapshot.Resolve(context.Background(), src, requirement,
			[]snapshot.InputRequest{{Name: incomplete.Name}})
		if !errors.Is(err, snapshot.ErrEntryIncomplete) {
			t.Fatalf("Resolve() = %v, want ErrEntryIncomplete", err)
		}
	})

	t.Run("tenant mismatch is refused", func(t *testing.T) {
		foreign := validEntry(t, "people.worker_facts", "someone-elses-tenant", horizon, snapshot.AuthorityNativeState)
		src := foreignTenantSource{entry: foreign}

		_, err := snapshot.Resolve(context.Background(), src, requirement,
			[]snapshot.InputRequest{{Name: foreign.Name}})
		if !errors.Is(err, snapshot.ErrTenantMismatch) {
			t.Fatalf("Resolve() = %v, want ErrTenantMismatch", err)
		}
	})

	t.Run("an external observation cannot be presented as native", func(t *testing.T) {
		masquerading := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityExternalObservation)
		src := snapshot.NewFakeSource().Seed(masquerading)

		_, err := snapshot.Resolve(context.Background(), src, requirement,
			[]snapshot.InputRequest{{Name: masquerading.Name, RequiredAuthority: snapshot.AuthorityNativeState}})
		if !errors.Is(err, snapshot.ErrAuthorityMismatch) {
			t.Fatalf("Resolve() = %v, want ErrAuthorityMismatch", err)
		}
	})
}

// foreignTenantSource is a minimal Source stub that always answers with a
// fixed entry, regardless of the tenant Resolve asked for. It exists to
// simulate a misbehaving or compromised Source that reports another
// tenant's data -- something a well-behaved fake like FakeSource, which
// indexes fixtures by tenant, cannot be made to do.
type foreignTenantSource struct{ entry snapshot.InputEntry }

func (s foreignTenantSource) Resolve(ctx context.Context, tenant values.TenantId, inputs []snapshot.InputRequest) ([]snapshot.InputEntry, error) {
	return []snapshot.InputEntry{s.entry}, nil
}

// TestTodo_SNAPSHOT_001_Mutation proves the MUTATION clause: a ReadSnapshot
// is a plain value, so altering a caller's own copy of an entry can never
// reach back into the snapshot that produced it or into a later resolve of
// the same source, and changing any part of an entry's content always
// changes the digest computed over it.
func TestTodo_SNAPSHOT_001_Mutation(t *testing.T) {
	horizon := fixtureHorizon(t)
	requirement := snapshot.ConsistencyRequirement{Tenant: fixtureTenant, KnownAtHorizon: horizon}
	original := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	src := snapshot.NewFakeSource().Seed(original)
	inputs := []snapshot.InputRequest{{Name: original.Name}}

	snap, err := snapshot.Resolve(context.Background(), src, requirement, inputs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	originalDigest := snap.Digest

	t.Run("mutating the caller's copy does not change the snapshot's digest or a later resolve", func(t *testing.T) {
		snap.Entries[0].Owner = "someone-else"
		snap.Entries[0].ReferenceVersion = "tampered"
		if snap.Digest != originalDigest {
			t.Fatalf("Digest field itself changed after mutating a copy's entry: %q vs %q", snap.Digest, originalDigest)
		}

		again, err := snapshot.Resolve(context.Background(), src, requirement, inputs)
		if err != nil {
			t.Fatalf("re-resolve: %v", err)
		}
		if again.Digest != originalDigest {
			t.Fatalf("re-resolve after tampering with a prior copy produced a different digest: %q vs %q",
				again.Digest, originalDigest)
		}
		if again.Entries[0].Owner != original.Owner {
			t.Fatalf("re-resolve observed the tampered owner %q; the fake source fixture was corrupted", again.Entries[0].Owner)
		}
	})

	t.Run("changing any one field of an entry changes the digest", func(t *testing.T) {
		mutate := func(name string, f func(e *snapshot.InputEntry)) {
			t.Run(name, func(t *testing.T) {
				altered := original
				f(&altered)
				if err := altered.Validate(); err != nil {
					t.Fatalf("mutated fixture is invalid: %v", err)
				}
				altSrc := snapshot.NewFakeSource().Seed(altered)
				altSnap, err := snapshot.Resolve(context.Background(), altSrc, requirement, inputs)
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				if altSnap.Digest == originalDigest {
					t.Fatalf("changing %s did not change the digest", name)
				}
			})
		}
		mutate("owner", func(e *snapshot.InputEntry) { e.Owner = "someone-else" })
		mutate("reference_version", func(e *snapshot.InputEntry) { e.ReferenceVersion = "v2" })
		mutate("classification", func(e *snapshot.InputEntry) { e.Classification = snapshot.ClassificationConfidential })
		mutate("head", func(e *snapshot.InputEntry) { e.Head = "head-2" })
		mutate("provenance_evidence_ref", func(e *snapshot.InputEntry) { e.Provenance.EvidenceRef = "evd_other" })
	})
}
