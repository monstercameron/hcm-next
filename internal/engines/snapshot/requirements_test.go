package snapshot_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/snapshot"
)

// TestTodo_SNAPSHOT_002 proves that required, optional, and conditional
// requirements are evaluated independently against watermark and freshness
// floors, with the known-at-derived horizon recorded in the result.
func TestTodo_SNAPSHOT_002(t *testing.T) {
	horizon := fixtureHorizon(t)
	good := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	optionalStale := validEntry(t, "people.optional_profile", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	optionalStale.Freshness = mustRecordedAt(t, mustInstant(t, 2026, time.February, 1, 0, 0, 0))
	src := snapshot.NewFakeSource().Seed(good).Seed(optionalStale)
	request := snapshot.SnapshotRequest{Tenant: fixtureTenant, KnownAtHorizon: horizon, Inputs: []snapshot.InputRequirement{
		{Name: good.Name, Policy: snapshot.InputRequired, RequiredAuthority: snapshot.AuthorityNativeState, MinimumWatermark: mustRevision(t, "watermark."+good.Name, 1), MaximumStaleness: 72 * time.Hour},
		{Name: optionalStale.Name, Policy: snapshot.InputOptional, RequiredAuthority: snapshot.AuthorityNativeState, MaximumStaleness: 24 * time.Hour},
		{Name: "people.conditional_profile", Policy: snapshot.InputConditional, RequiredAuthority: snapshot.AuthorityNativeState},
	}}
	result, err := snapshot.ResolveWithRequirements(context.Background(), src, request)
	if err != nil {
		t.Fatalf("ResolveWithRequirements: %v", err)
	}
	if len(result.Dispositions) != 3 {
		t.Fatalf("dispositions = %d, want 3", len(result.Dispositions))
	}
	if result.Dispositions[0].Status != snapshot.RequirementSatisfied || !result.Dispositions[0].Satisfied {
		t.Fatalf("required disposition = %+v, want SATISFIED", result.Dispositions[0])
	}
	if result.Dispositions[1].Status != snapshot.RequirementStale || result.Dispositions[1].Satisfied {
		t.Fatalf("optional stale disposition = %+v, want unsatisfied STALE", result.Dispositions[1])
	}
	if result.Dispositions[1].FreshnessHorizon.Compare(mustInstant(t, 2026, time.February, 28, 12, 0, 0)) != 0 {
		t.Fatalf("freshness horizon = %s, want 2026-02-28T12:00:00Z", result.Dispositions[1].FreshnessHorizon)
	}
	if result.Dispositions[2].Status != snapshot.RequirementNotApplicable || !result.Dispositions[2].Satisfied {
		t.Fatalf("inactive conditional disposition = %+v, want NOT_APPLICABLE", result.Dispositions[2])
	}
	if result.Digest == "" || result.Snapshot.Digest == "" {
		t.Fatal("resolution and snapshot digests must be present")
	}
}

func TestTodo_SNAPSHOT_002_Golden(t *testing.T) {
	horizon := fixtureHorizon(t)
	entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	request := snapshot.SnapshotRequest{Tenant: fixtureTenant, KnownAtHorizon: horizon, Inputs: []snapshot.InputRequirement{{
		Name: entry.Name, Policy: snapshot.InputRequired, RequiredAuthority: snapshot.AuthorityNativeState, MinimumWatermark: mustRevision(t, "watermark."+entry.Name, 1), MaximumStaleness: 72 * time.Hour,
	}}}
	result, err := snapshot.ResolveWithRequirements(context.Background(), snapshot.NewFakeSource().Seed(entry), request)
	if err != nil {
		t.Fatalf("ResolveWithRequirements: %v", err)
	}
	const wantDigest = "sha256:c4268590297341689d22127bc64ec45b24800c6f023b72fd80380115e74525c8"
	if result.Digest != wantDigest {
		t.Fatalf("Digest = %q, want %q", result.Digest, wantDigest)
	}
}

func TestTodo_SNAPSHOT_002_Mutation(t *testing.T) {
	horizon := fixtureHorizon(t)
	entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	request := snapshot.SnapshotRequest{Tenant: fixtureTenant, KnownAtHorizon: horizon, Inputs: []snapshot.InputRequirement{{
		Name: entry.Name, Policy: snapshot.InputRequired, RequiredAuthority: snapshot.AuthorityNativeState, MaximumStaleness: 72 * time.Hour,
	}}}
	src := snapshot.NewFakeSource().Seed(entry)
	first, err := snapshot.ResolveWithRequirements(context.Background(), src, request)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	digest := first.Digest
	first.Dispositions[0].Status = snapshot.RequirementStale
	first.Snapshot.Entries[0].Owner = "tampered"
	second, err := snapshot.ResolveWithRequirements(context.Background(), src, request)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.Digest != digest || second.Snapshot.Entries[0].Owner == "tampered" {
		t.Fatalf("resolution was affected by mutation: first=%q second=%q", digest, second.Digest)
	}
	changed := request
	changed.Inputs = append([]snapshot.InputRequirement(nil), request.Inputs...)
	changed.Inputs[0].MaximumStaleness = 24 * time.Hour
	if changedResult, err := snapshot.ResolveWithRequirements(context.Background(), src, changed); err == nil && changedResult.Digest == digest {
		t.Fatal("changing a requirement did not change the digest")
	}
}

func TestTodo_SNAPSHOT_002_Fault(t *testing.T) {
	horizon := fixtureHorizon(t)
	entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	entry.Freshness = mustRecordedAt(t, mustInstant(t, 2026, time.January, 1, 0, 0, 0))
	request := snapshot.SnapshotRequest{Tenant: fixtureTenant, KnownAtHorizon: horizon, Inputs: []snapshot.InputRequirement{{
		Name: entry.Name, Policy: snapshot.InputRequired, RequiredAuthority: snapshot.AuthorityNativeState, MaximumStaleness: 24 * time.Hour,
	}}}
	_, err := snapshot.ResolveWithRequirements(context.Background(), snapshot.NewFakeSource().Seed(entry), request)
	if !errors.Is(err, snapshot.ErrFreshnessTooOld) || snapshot.RequirementInputOf(err) != entry.Name {
		t.Fatalf("stale error = %v, want ErrFreshnessTooOld naming %q", err, entry.Name)
	}

	missing := request
	missing.Inputs = []snapshot.InputRequirement{{Name: "people.missing", Policy: snapshot.InputRequired, RequiredAuthority: snapshot.AuthorityNativeState}}
	_, err = snapshot.ResolveWithRequirements(context.Background(), snapshot.NewFakeSource(), missing)
	if !errors.Is(err, snapshot.ErrInputUnavailable) || snapshot.RequirementInputOf(err) != "people.missing" {
		t.Fatalf("missing error = %v, want ErrInputUnavailable naming input", err)
	}
}
