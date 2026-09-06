package snapshot_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/snapshot"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestResolveRefusesEachContractBreakDistinctly exercises the refusal paths
// TestTodo_SNAPSHOT_001[_Security|_Mutation] do not already cover, each with
// its own sentinel error so a caller can tell exactly which contract broke.
func TestResolveRefusesEachContractBreakDistinctly(t *testing.T) {
	horizon := fixtureHorizon(t)
	requirement := snapshot.ConsistencyRequirement{Tenant: fixtureTenant, KnownAtHorizon: horizon}

	t.Run("nil source", func(t *testing.T) {
		_, err := snapshot.Resolve(context.Background(), nil, requirement, []snapshot.InputRequest{{Name: "x"}})
		if !errors.Is(err, snapshot.ErrSourceFailed) {
			t.Fatalf("Resolve() = %v, want ErrSourceFailed", err)
		}
	})

	t.Run("invalid requirement", func(t *testing.T) {
		bad := requirement
		bad.Tenant = ""
		_, err := snapshot.Resolve(context.Background(), snapshot.NewFakeSource(), bad, []snapshot.InputRequest{{Name: "x"}})
		if !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Resolve() = %v, want ErrRequirementIncomplete", err)
		}
	})

	t.Run("no inputs requested", func(t *testing.T) {
		_, err := snapshot.Resolve(context.Background(), snapshot.NewFakeSource(), requirement, nil)
		if !errors.Is(err, snapshot.ErrNoInputsRequested) {
			t.Fatalf("Resolve() = %v, want ErrNoInputsRequested", err)
		}
	})

	t.Run("duplicate input request", func(t *testing.T) {
		_, err := snapshot.Resolve(context.Background(), snapshot.NewFakeSource(), requirement,
			[]snapshot.InputRequest{{Name: "x"}, {Name: "x"}})
		if !errors.Is(err, snapshot.ErrDuplicateInputRequest) {
			t.Fatalf("Resolve() = %v, want ErrDuplicateInputRequest", err)
		}
	})

	t.Run("invalid input request propagates", func(t *testing.T) {
		_, err := snapshot.Resolve(context.Background(), snapshot.NewFakeSource(), requirement,
			[]snapshot.InputRequest{{Name: ""}})
		if !errors.Is(err, snapshot.ErrRequestIncomplete) {
			t.Fatalf("Resolve() = %v, want ErrRequestIncomplete", err)
		}
	})

	t.Run("source failure is wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		src := snapshot.NewFakeSource()
		src.Err = boom
		_, err := snapshot.Resolve(context.Background(), src, requirement, []snapshot.InputRequest{{Name: "x"}})
		if !errors.Is(err, snapshot.ErrSourceFailed) || !errors.Is(err, boom) {
			t.Fatalf("Resolve() = %v, want wrapping both ErrSourceFailed and the source's own error", err)
		}
	})

	t.Run("missing entry", func(t *testing.T) {
		_, err := snapshot.Resolve(context.Background(), snapshot.NewFakeSource(), requirement,
			[]snapshot.InputRequest{{Name: "unseeded"}})
		if !errors.Is(err, snapshot.ErrMissingEntry) {
			t.Fatalf("Resolve() = %v, want ErrMissingEntry", err)
		}
	})

	t.Run("unrequested entry", func(t *testing.T) {
		entry := validEntry(t, "extra.input", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		src := extraEntrySource{extra: entry}
		_, err := snapshot.Resolve(context.Background(), src, requirement,
			[]snapshot.InputRequest{{Name: "requested.input"}})
		if !errors.Is(err, snapshot.ErrUnrequestedEntry) {
			t.Fatalf("Resolve() = %v, want ErrUnrequestedEntry", err)
		}
	})

	t.Run("duplicate entry", func(t *testing.T) {
		entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		src := duplicateEntrySource{entry: entry}
		_, err := snapshot.Resolve(context.Background(), src, requirement,
			[]snapshot.InputRequest{{Name: entry.Name}})
		if !errors.Is(err, snapshot.ErrDuplicateEntry) {
			t.Fatalf("Resolve() = %v, want ErrDuplicateEntry", err)
		}
	})

	t.Run("known-at horizon mismatch", func(t *testing.T) {
		otherHorizon := mustKnownAt(t, mustInstant(t, 2020, 1, 1, 0, 0, 0))
		entry := validEntry(t, "people.worker_facts", fixtureTenant, otherHorizon, snapshot.AuthorityNativeState)
		src := snapshot.NewFakeSource().Seed(entry)
		_, err := snapshot.Resolve(context.Background(), src, requirement, []snapshot.InputRequest{{Name: entry.Name}})
		if !errors.Is(err, snapshot.ErrKnownAtHorizonMismatch) {
			t.Fatalf("Resolve() = %v, want ErrKnownAtHorizonMismatch", err)
		}
	})

	t.Run("watermark below minimum", func(t *testing.T) {
		entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		entry.Watermark = mustRevision(t, "watermark.people.worker_facts", 1)
		src := snapshot.NewFakeSource().Seed(entry)
		req := requirement
		req.MinWatermarks = []snapshot.InputWatermarkFloor{
			{InputName: entry.Name, Minimum: mustRevision(t, "watermark.people.worker_facts", 5)},
		}
		_, err := snapshot.Resolve(context.Background(), src, req, []snapshot.InputRequest{{Name: entry.Name}})
		if !errors.Is(err, snapshot.ErrWatermarkBelowMinimum) {
			t.Fatalf("Resolve() = %v, want ErrWatermarkBelowMinimum", err)
		}
	})

	t.Run("watermark incomparable across streams", func(t *testing.T) {
		entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		entry.Watermark = mustRevision(t, "watermark.people.worker_facts", 5)
		src := snapshot.NewFakeSource().Seed(entry)
		req := requirement
		req.MinWatermarks = []snapshot.InputWatermarkFloor{
			{InputName: entry.Name, Minimum: mustRevision(t, "a-completely-different-stream", 1)},
		}
		_, err := snapshot.Resolve(context.Background(), src, req, []snapshot.InputRequest{{Name: entry.Name}})
		if !errors.Is(err, snapshot.ErrWatermarkIncomparable) {
			t.Fatalf("Resolve() = %v, want ErrWatermarkIncomparable", err)
		}
	})

	t.Run("watermark floor names an unresolved input", func(t *testing.T) {
		entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		src := snapshot.NewFakeSource().Seed(entry)
		req := requirement
		req.MinWatermarks = []snapshot.InputWatermarkFloor{
			{InputName: "not-requested-input", Minimum: mustRevision(t, "x", 1)},
		}
		_, err := snapshot.Resolve(context.Background(), src, req, []snapshot.InputRequest{{Name: entry.Name}})
		if !errors.Is(err, snapshot.ErrMissingEntry) {
			t.Fatalf("Resolve() = %v, want ErrMissingEntry", err)
		}
	})

	t.Run("watermark at exactly the minimum satisfies the floor", func(t *testing.T) {
		entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
		entry.Watermark = mustRevision(t, "watermark.people.worker_facts", 5)
		src := snapshot.NewFakeSource().Seed(entry)
		req := requirement
		req.MinWatermarks = []snapshot.InputWatermarkFloor{
			{InputName: entry.Name, Minimum: mustRevision(t, "watermark.people.worker_facts", 5)},
		}
		if _, err := snapshot.Resolve(context.Background(), src, req, []snapshot.InputRequest{{Name: entry.Name}}); err != nil {
			t.Fatalf("Resolve() = %v, want nil (watermark equal to the floor must satisfy it)", err)
		}
	})
}

// extraEntrySource always answers with one entry the caller never requested.
type extraEntrySource struct{ extra snapshot.InputEntry }

func (s extraEntrySource) Resolve(ctx context.Context, tenant values.TenantId, inputs []snapshot.InputRequest) ([]snapshot.InputEntry, error) {
	return []snapshot.InputEntry{s.extra}, nil
}

// duplicateEntrySource always answers one requested name twice.
type duplicateEntrySource struct{ entry snapshot.InputEntry }

func (s duplicateEntrySource) Resolve(ctx context.Context, tenant values.TenantId, inputs []snapshot.InputRequest) ([]snapshot.InputEntry, error) {
	return []snapshot.InputEntry{s.entry, s.entry}, nil
}

// TestReadSnapshotLookupMissing proves Lookup reports absence rather than a
// zero-value entry that could be mistaken for a resolved one.
func TestReadSnapshotLookupMissing(t *testing.T) {
	horizon := fixtureHorizon(t)
	entry := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	src := snapshot.NewFakeSource().Seed(entry)
	requirement := snapshot.ConsistencyRequirement{Tenant: fixtureTenant, KnownAtHorizon: horizon}

	snap, err := snapshot.Resolve(context.Background(), src, requirement, []snapshot.InputRequest{{Name: entry.Name}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := snap.Lookup("does.not.exist"); ok {
		t.Fatal("Lookup of an unresolved name reported found = true")
	}
}
