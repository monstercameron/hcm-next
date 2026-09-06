package snapshot_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/snapshot"
)

func TestFakeSourceAnswersOnlySeededFixtures(t *testing.T) {
	t.Parallel()
	horizon := fixtureHorizon(t)
	src := snapshot.NewFakeSource().Seed(validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState))

	entries, err := src.Resolve(context.Background(), fixtureTenant, []snapshot.InputRequest{
		{Name: "people.worker_facts"},
		{Name: "unseeded.input"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Resolve returned %d entries, want 1 (unseeded input must be absent, not invented)", len(entries))
	}
	if entries[0].Name != "people.worker_facts" {
		t.Fatalf("Resolve returned %q, want people.worker_facts", entries[0].Name)
	}
}

func TestFakeSourceIsolatesFixturesByTenant(t *testing.T) {
	t.Parallel()
	horizon := fixtureHorizon(t)
	other := fixtureTenant + "-other"
	src := snapshot.NewFakeSource().
		Seed(validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState))

	entries, err := src.Resolve(context.Background(), other, []snapshot.InputRequest{{Name: "people.worker_facts"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Resolve for a different tenant returned %d entries, want 0", len(entries))
	}
}

func TestFakeSourceErrShortCircuits(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	src := snapshot.NewFakeSource()
	src.Err = boom

	_, err := src.Resolve(context.Background(), fixtureTenant, []snapshot.InputRequest{{Name: "x"}})
	if !errors.Is(err, boom) {
		t.Fatalf("Resolve() = %v, want boom", err)
	}
}

func TestFakeSourceSeedPanicsOnInvalidFixture(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("Seed of an invalid entry did not panic")
		}
	}()
	snapshot.NewFakeSource().Seed(snapshot.InputEntry{})
}

func TestFakeSourceSeedRawAllowsAnInvalidFixture(t *testing.T) {
	t.Parallel()
	horizon := fixtureHorizon(t)
	broken := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	broken.Owner = "" // deliberately incomplete

	src := snapshot.NewFakeSource().SeedRaw(broken)
	entries, err := src.Resolve(context.Background(), fixtureTenant, []snapshot.InputRequest{{Name: "people.worker_facts"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Resolve returned %d entries, want 1", len(entries))
	}
	if err := entries[0].Validate(); err == nil {
		t.Fatal("SeedRaw fixture unexpectedly validates; test no longer proves what it claims")
	}
}
