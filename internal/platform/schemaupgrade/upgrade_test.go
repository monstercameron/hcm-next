package schemaupgrade

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testPlan() Plan {
	return Plan{
		ID:                        "upgrade-people-v2",
		FromVersion:               1,
		ToVersion:                 2,
		Compatibility:             CompatibilityFull,
		SourceDigest:              strings.Repeat("1", 64),
		TargetDigest:              strings.Repeat("2", 64),
		RollbackBoundary:          "before-contract",
		RequiredAdoptionWatermark: 3,
		BackfillBatchSize:         2,
		Binaries: []Binary{
			{Name: "old", Reads: []int{1, 2}, Writes: []int{1}},
			{Name: "new", Reads: []int{1, 2}, Writes: []int{1, 2}},
		},
	}
}

func testRows() []Row {
	return []Row{{Key: "a", Digest: "da"}, {Key: "b", Digest: "db"}, {Key: "c", Digest: "dc"}}
}

func completeUpgrade(t *testing.T) State {
	t.Helper()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	state, err := New(testPlan(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for !state.Checkpoint.Completed {
		if _, err := state.ResumeBackfill(testRows(), 2, now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := state.CompareShadow(testRows(), testRows(), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	state.SetConsumerWatermark(3)
	if err := state.Cutover(3, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTodo_DB_021(t *testing.T) {
	state := completeUpgrade(t)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseContracted || len(state.History) != 6 {
		t.Fatalf("phase=%s history=%d, want CONTRACTED and six append-only events", state.Phase, len(state.History))
	}
}

func TestTodo_DB_021_Golden(t *testing.T) {
	state := completeUpgrade(t)
	if got, want := state.Checkpoint.Cursor, "c"; got != want {
		t.Fatalf("checkpoint cursor=%q, want %q", got, want)
	}
	if state.Checkpoint.SourceDigest != state.Checkpoint.CopiedDigest || state.Shadow.SourceDigest != state.Shadow.TargetDigest {
		t.Fatal("completed upgrade did not retain equal source, copied and shadow digests")
	}
}

func TestTodo_DB_021_Race(t *testing.T) {
	state := completeUpgrade(t)
	first := state.Snapshot()
	copy := state.Snapshot()
	copy.History[0].Detail = "tampered"
	if first.History[0].Detail == copy.History[0].Detail {
		t.Fatal("Snapshot exposed mutable history")
	}
}

func TestTodo_DB_021_Integration(t *testing.T) {
	state := completeUpgrade(t)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := state.Rollback(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseRolledBack || state.History[len(state.History)-1].Phase != PhaseRolledBack {
		t.Fatal("rollback did not append a new rollback event")
	}
}

func TestTodo_DB_021_Mutation(t *testing.T) {
	state, err := New(testPlan(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows := testRows()
	if _, err := state.ResumeBackfill(rows, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows[0].Digest = "changed"
	if _, err := state.ResumeBackfill(rows, 1, time.Now().UTC()); !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("ResumeBackfill error=%v, want checkpoint mismatch", err)
	}
}

func TestTodo_DB_021_Fault(t *testing.T) {
	state := completeUpgrade(t)
	state.SetConsumerWatermark(0)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := state.Rollback(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	lagging, err := New(testPlan(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := lagging.Expand(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for !lagging.Checkpoint.Completed {
		if _, err := lagging.ResumeBackfill(testRows(), 10, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lagging.CompareShadow(testRows(), []Row{{Key: "a", Digest: "wrong"}}, time.Now().UTC()); !errors.Is(err, ErrShadowMismatch) {
		t.Fatalf("CompareShadow error=%v, want shadow mismatch", err)
	}
}
