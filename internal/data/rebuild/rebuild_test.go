package rebuild_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/data/rebuild"
)

func target(tenant uuid.UUID, stream string) rebuild.Target {
	return rebuild.Target{Tenant: tenant, Stream: stream, Projection: "worker_state"}
}

// TestValidationRefusesBadTargetsAndReducers proves the pre-flight contract:
// a rebuild never opens a transaction for a target without every required
// field, or a reducer without a legal table name, a Fold and a Digest. A nil
// Beginner stands in for the database - if validation ever let one of these
// through, the call would panic on the nil instead of returning an error.
func TestValidationRefusesBadTargetsAndReducers(t *testing.T) {
	okFold := func(ctx context.Context, table string, tx dbport.Tx, ev ledger.EventRecord) error { return nil }
	okDigest := func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) (int64, string, error) {
		return 0, "", nil
	}
	valid := target(uuid.New(), "rebuild:v")
	cases := []struct {
		name      string
		reqTarget rebuild.Target
		reducer   rebuild.Reducer
		want      string
	}{
		{"missing tenant", rebuild.Target{Stream: "s", Projection: "p"}, fixtureReducer(), "target tenant"},
		{"missing stream", rebuild.Target{Tenant: uuid.New(), Projection: "p"}, fixtureReducer(), "target stream"},
		{"missing projection", rebuild.Target{Tenant: uuid.New(), Stream: "s"}, fixtureReducer(), "target projection"},
		{"unsafe table", valid, rebuild.Reducer{Table: projTable + `"; DROP TABLE tenant;`, Fold: okFold, Digest: okDigest}, "unsafe"},
		{"nil fold", valid, rebuild.Reducer{Table: projTable, Digest: okDigest}, "Fold"},
		{"nil digest", valid, rebuild.Reducer{Table: projTable, Fold: okFold}, "Digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := rebuild.New(nil, nil)
			_, err := executor.Rebuild(context.Background(), tc.reqTarget, tc.reducer)
			if err == nil {
				t.Fatal("rebuilt without an error, one is required")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestRebuildPromotesOnExactMatch proves LEDGER-013's GREEN path end to end
// against the real ledger: every event passes, the rebuilt shadow matches the
// stored projection exactly, so the run promotes - the shadow content swaps
// into the projection table and the checkpoint advances to the source head -
// and leaves no shadow table behind.
func TestRebuildPromotesOnExactMatch(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rebuild:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)

	r1 := f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	r2 := f.appendEvent(t, tenant, stream, 1, ledger.TransactionFact, nil)
	if r1.Sequence != 1 || r2.Sequence != 2 {
		t.Fatalf("ledger allocated sequences %d, %d, want 1, 2", r1.Sequence, r2.Sequence)
	}
	// The checkpoint starts at zero, and promote advances it one step at a time;
	// it stands one event behind the head, as a live applier's would.
	inTx(t, f.db, func(tx dbport.Tx) error {
		_, err := projection.Apply(context.Background(), tx, projection.ApplyRequest{
			Tenant: tenant, ProjectionName: "worker_state", StreamKey: stream,
			Sequence: 1, Digest: r1.Digest,
		})
		return err
	})
	f.seedRow(t, tenant, stream, 1, "value-1")
	f.seedRow(t, tenant, stream, 2, "value-2")

	executor := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn)
	report, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if !report.Match || !report.Promoted {
		t.Fatalf("report = %+v, want a matched, promoted rebuild", report)
	}
	if report.Replayed != 2 || report.SourceHead != 2 {
		t.Fatalf("replayed %d of head %d, want 2 of 2", report.Replayed, report.SourceHead)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("clean rebuild carried findings: %v", report.Findings)
	}
	if report.Current.Rows != 2 || report.Shadow.Rows != 2 || report.Current.Digest != report.Shadow.Digest {
		t.Fatalf("comparison facts differ: current %+v shadow %+v", report.Current, report.Shadow)
	}

	cp, err := projection.Read(context.Background(), f.db.Conn, tenant, "worker_state", stream)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if cp.LastAppliedSequence != 2 || cp.LastAppliedDigest != r2.Digest {
		t.Fatalf("checkpoint = %+v, want sequence 2 with the head event's digest", cp)
	}
	if got := f.rowCount(t, tenant, stream); got != 2 {
		t.Fatalf("projection table holds %d row(s) after promotion, want 2", got)
	}
	if f.valueAt(t, tenant, stream, 1) != "value-1" || f.valueAt(t, tenant, stream, 2) != "value-2" {
		t.Fatal("promotion changed the projection content")
	}
	if got := f.leftoverShadowTables(t); got != 0 {
		t.Fatalf("%d shadow table(s) left behind, want none", got)
	}
}

// TestRebuildRefusesContentDrift proves the RED compare: the same row count
// with different content fails the digest comparison, the rebuild does not
// promote, the stored state is not touched, and the checkpoint does not
// advance.
func TestRebuildRefusesContentDrift(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rebuild:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)

	f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	f.appendEvent(t, tenant, stream, 1, ledger.TransactionFact, nil)
	f.seedRow(t, tenant, stream, 1, "value-1")
	f.seedRow(t, tenant, stream, 2, "tampered-value-2")

	executor := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn)
	report, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if report.Promoted {
		t.Fatal("a drifted rebuild promoted")
	}
	if report.Match {
		t.Fatalf("a drifted rebuild reported a match: %+v", report)
	}
	if !slices.Contains(findingCodes(report), rebuild.CodeDigestMismatch) {
		t.Fatalf("findings = %v, want %s", findingCodes(report), rebuild.CodeDigestMismatch)
	}
	if slices.Contains(findingCodes(report), rebuild.CodeRowCountMismatch) {
		t.Fatalf("findings = %v, a same-count drift must not report a row-count mismatch", findingCodes(report))
	}
	if got := f.valueAt(t, tenant, stream, 2); got != "tampered-value-2" {
		t.Fatalf("a refused rebuild rewrote the stored row to %q", got)
	}
	cp, err := projection.Read(context.Background(), f.db.Conn, tenant, "worker_state", stream)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if cp.LastAppliedSequence != 0 {
		t.Fatalf("a refused rebuild advanced the checkpoint to %d", cp.LastAppliedSequence)
	}
}

// TestRebuildRefusesRowCountMismatch proves the RED compare for a missing
// row: the count and the digest both disagree, and the rebuild refuses.
func TestRebuildRefusesRowCountMismatch(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rebuild:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)

	f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	f.appendEvent(t, tenant, stream, 1, ledger.TransactionFact, nil)

	executor := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn)
	report, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if report.Promoted || report.Match {
		t.Fatalf("an empty projection rebuilt as matched and promoted: %+v", report)
	}
	codes := findingCodes(report)
	if !slices.Contains(codes, rebuild.CodeRowCountMismatch) || !slices.Contains(codes, rebuild.CodeDigestMismatch) {
		t.Fatalf("findings = %v, want %s and %s", codes, rebuild.CodeRowCountMismatch, rebuild.CodeDigestMismatch)
	}
}

// scenario registers one tenant's stream, seeds the stored projection, and
// runs one scripted rebuild over it.
func (f fixture) scenario(t *testing.T, tenant uuid.UUID, stream string,
	src fakeSource, seedRows []int64, invariants ...rebuild.Invariant) rebuild.Report {
	t.Helper()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	for _, seq := range seedRows {
		f.seedRow(t, tenant, stream, seq, fmt.Sprintf("value-%d", seq))
	}
	executor := rebuild.New(src, f.db.Conn)
	report, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer(), invariants...)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	return report
}

// TestRebuildRefusesReplayDefects proves every replay defect a bad feed can
// produce is named in the report, keeps the rebuild from promoting, and does
// not let the defective event into the shadow.
func TestRebuildRefusesReplayDefects(t *testing.T) {
	f := newFixture(t)

	t.Run("sequence gap", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		// The fake stream's head equals its event count, so only the gap is a
		// finding; the source and the ledger are internally consistent here.
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       2,
			events:     []ledger.EventRecord{ev(tenant, stream, 1), ev(tenant, stream, 3)},
			registered: map[string]bool{schemaRef: true},
		}, []int64{1})
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeSequenceGap}) {
			t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), rebuild.CodeSequenceGap)
		}
		if report.Replayed != 1 || report.Promoted {
			t.Fatalf("gap run: replayed %d, promoted %v, want 1 and false", report.Replayed, report.Promoted)
		}
	})

	t.Run("sequence duplicate", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       2,
			events:     []ledger.EventRecord{ev(tenant, stream, 1), ev(tenant, stream, 1)},
			registered: map[string]bool{schemaRef: true},
		}, []int64{1})
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeSequenceDuplicate}) {
			t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), rebuild.CodeSequenceDuplicate)
		}
		if report.Replayed != 1 || report.Promoted {
			t.Fatalf("duplicate run: replayed %d, promoted %v, want 1 and false", report.Replayed, report.Promoted)
		}
	})

	t.Run("reordered replays in sequence position", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       3,
			events:     []ledger.EventRecord{ev(tenant, stream, 1), ev(tenant, stream, 3), ev(tenant, stream, 2)},
			registered: map[string]bool{schemaRef: true},
		}, []int64{1, 2})
		// Sequence 3 is reported as a gap and never folds; it does not move
		// the expected cursor, so sequence 2 still folds when it arrives and
		// the shadow ends up holding exactly the well-ordered prefix.
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeSequenceGap}) {
			t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), rebuild.CodeSequenceGap)
		}
		if report.Replayed != 2 || report.Promoted {
			t.Fatalf("reorder run: replayed %d, promoted %v, want 2 and false", report.Replayed, report.Promoted)
		}
	})

	for _, tc := range []struct {
		name     string
		mutate   func(*ledger.EventRecord)
		wantCode string
	}{
		{"foreign tenant", func(e *ledger.EventRecord) { e.Tenant = uuid.New() }, rebuild.CodeTenantForeign},
		{"foreign stream", func(e *ledger.EventRecord) { e.StreamKey = "other:" + uuid.NewString() }, rebuild.CodeStreamForeign},
		{"missing identity", func(e *ledger.EventRecord) { e.EventID = uuid.Nil }, rebuild.CodeKeyMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant := insertTenant(t, f.db)
			stream := "rebuild:" + uuid.NewString()
			e := ev(tenant, stream, 1)
			tc.mutate(&e)
			report := f.scenario(t, tenant, stream, fakeSource{
				head:       1,
				events:     []ledger.EventRecord{e},
				registered: map[string]bool{schemaRef: true},
			}, nil)
			// These defects are reported for what they are: exactly one
			// finding, nothing cascaded from the later checks.
			if !slices.Equal(findingCodes(report), []string{tc.wantCode}) {
				t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), tc.wantCode)
			}
			if report.Replayed != 0 || report.Promoted {
				t.Fatalf("%s run: replayed %d, promoted %v, want 0 and false", tc.name, report.Replayed, report.Promoted)
			}
		})
	}

	t.Run("unregistered schema", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       1,
			events:     []ledger.EventRecord{ev(tenant, stream, 1)},
			registered: map[string]bool{},
		}, nil)
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeSchemaMissing}) {
			t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), rebuild.CodeSchemaMissing)
		}
		if report.Replayed != 0 || report.Promoted {
			t.Fatalf("schema run: replayed %d, promoted %v, want 0 and false", report.Replayed, report.Promoted)
		}
	})

	// correctionScenario replays a one-event stream whose only event is a
	// correction. When ref is nil the live source finds no corrects reference at
	// all; otherwise it reports that reference.
	correctionScenario := func(t *testing.T, tenant uuid.UUID, stream string, ref *rebuild.CorrectionRef) rebuild.Report {
		t.Helper()
		e := ev(tenant, stream, 1)
		e.AssertionClass = ledger.Correction
		src := fakeSource{
			head:       1,
			events:     []ledger.EventRecord{e},
			registered: map[string]bool{schemaRef: true},
		}
		if ref != nil {
			src.corrections = map[string]rebuild.CorrectionRef{correctionKey(e): *ref}
		}
		return f.scenario(t, tenant, stream, src, nil)
	}
	assertCorrectionRefused := func(t *testing.T, report rebuild.Report) {
		t.Helper()
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeCorrectionMismatch}) {
			t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), rebuild.CodeCorrectionMismatch)
		}
		if report.Promoted {
			t.Fatal("a correction mismatch promoted")
		}
	}

	t.Run("correction without a reference", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		assertCorrectionRefused(t, correctionScenario(t, tenant, stream, nil))
	})

	t.Run("correction to an assertion not on the ledger", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		ref := rebuild.CorrectionRef{HasReference: true, Stream: stream, Sequence: 9}
		assertCorrectionRefused(t, correctionScenario(t, tenant, stream, &ref))
	})

	t.Run("correction to its own or a later assertion", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		ref := rebuild.CorrectionRef{
			HasReference: true, Stream: stream, Sequence: 1,
			Resolved: true, Class: string(ledger.TransactionFact),
		}
		assertCorrectionRefused(t, correctionScenario(t, tenant, stream, &ref))
	})

	t.Run("head unreached", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       5,
			events:     []ledger.EventRecord{ev(tenant, stream, 1), ev(tenant, stream, 2), ev(tenant, stream, 3)},
			registered: map[string]bool{schemaRef: true},
		}, []int64{1, 2, 3})
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeHeadUnreached}) {
			t.Fatalf("findings = %v, want exactly [%s]", findingCodes(report), rebuild.CodeHeadUnreached)
		}
		if report.Replayed != 3 || report.Promoted {
			t.Fatalf("head run: replayed %d, promoted %v, want 3 and false", report.Replayed, report.Promoted)
		}
	})

	t.Run("invariant violation blocks promotion", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       1,
			events:     []ledger.EventRecord{ev(tenant, stream, 1)},
			registered: map[string]bool{schemaRef: true},
		}, []int64{1}, rebuild.Invariant{
			Name: "nonnegative",
			Check: func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) ([]string, error) {
				return []string{"row at sequence 1 violates nonnegative"}, nil
			},
		})
		if !slices.Equal(findingCodes(report), []string{rebuild.CodeInvariantPrefix + "/nonnegative"}) {
			t.Fatalf("findings = %v, want exactly [invariant/nonnegative]", findingCodes(report))
		}
		if report.Match || report.Promoted {
			t.Fatalf("an invariant violation matched and promoted: %+v", report)
		}
		if len(report.InvariantsRun) != 1 || report.InvariantsRun[0] != "nonnegative" {
			t.Fatalf("invariants run = %v, want [nonnegative]", report.InvariantsRun)
		}
	})

	t.Run("passing invariants gate a clean promotion in order", func(t *testing.T) {
		tenant := insertTenant(t, f.db)
		stream := "rebuild:" + uuid.NewString()
		pass := func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) ([]string, error) {
			return nil, nil
		}
		report := f.scenario(t, tenant, stream, fakeSource{
			head:       1,
			events:     []ledger.EventRecord{ev(tenant, stream, 1)},
			registered: map[string]bool{schemaRef: true},
		}, []int64{1},
			rebuild.Invariant{Name: "first", Check: pass},
			rebuild.Invariant{Name: "second", Check: pass},
		)
		if !report.Match || !report.Promoted {
			t.Fatalf("invariants passed but the rebuild did not promote: %+v", report)
		}
		if !slices.Equal(report.InvariantsRun, []string{"first", "second"}) {
			t.Fatalf("invariants run = %v, want [first second]", report.InvariantsRun)
		}
	})
}

// TestRebuildMidRunFailureLeavesNoTrace proves the all-or-nothing contract:
// a fold failure mid-run rolls the whole transaction back - the projection
// table is untouched, the checkpoint does not move, and the shadow table goes
// away with the transaction. There is no half-rebuilt state that can survive.
func TestRebuildMidRunFailureLeavesNoTrace(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rebuild:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")

	f.seedRow(t, tenant, stream, 1, "value-1")
	f.seedRow(t, tenant, stream, 2, "value-2")

	reducer := fixtureReducer()
	folds := 0
	original := reducer.Fold
	reducer.Fold = func(ctx context.Context, table string, tx dbport.Tx, ev ledger.EventRecord) error {
		folds++
		if folds == 2 {
			return errors.New("fold exploded")
		}
		return original(ctx, table, tx, ev)
	}

	executor := rebuild.New(fakeSource{
		head:       2,
		events:     []ledger.EventRecord{ev(tenant, stream, 1), ev(tenant, stream, 2)},
		registered: map[string]bool{schemaRef: true},
	}, f.db.Conn)
	_, err := executor.Rebuild(context.Background(), target(tenant, stream), reducer)
	if err == nil || !strings.Contains(err.Error(), "fold sequence 2") {
		t.Fatalf("rebuild error = %v, want the failed fold named", err)
	}
	if got := f.rowCount(t, tenant, stream); got != 2 {
		t.Fatalf("the failed run left %d row(s) in the projection table, want the original 2", got)
	}
	if f.valueAt(t, tenant, stream, 2) != "value-2" {
		t.Fatal("the failed run rewrote a stored row")
	}
	cp, err := projection.Read(context.Background(), f.db.Conn, tenant, "worker_state", stream)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if cp.LastAppliedSequence != 0 {
		t.Fatalf("the failed run advanced the checkpoint to %d", cp.LastAppliedSequence)
	}
	if got := f.leftoverShadowTables(t); got != 0 {
		t.Fatalf("%d shadow table(s) survived the rollback, want none", got)
	}
}

// TestInvariantContractViolationsAreErrors proves the executor validates its
// invariants instead of silently skipping an unnameable or check-less one:
// an invariant without a name or a check fails the run.
func TestInvariantContractViolationsAreErrors(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rebuild:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.seedRow(t, tenant, stream, 1, "value-1")

	src := fakeSource{
		head:       1,
		events:     []ledger.EventRecord{ev(tenant, stream, 1)},
		registered: map[string]bool{schemaRef: true},
	}
	executor := rebuild.New(src, f.db.Conn)

	t.Run("invariant without a name", func(t *testing.T) {
		_, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer(),
			rebuild.Invariant{Check: func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) ([]string, error) {
				return nil, nil
			}})
		if err == nil || !strings.Contains(err.Error(), "invariant") {
			t.Fatalf("error = %v, want the invariant contract named", err)
		}
	})
	t.Run("invariant without a check", func(t *testing.T) {
		_, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer(),
			rebuild.Invariant{Name: "no check"})
		if err == nil || !strings.Contains(err.Error(), "invariant") {
			t.Fatalf("error = %v, want the invariant contract named", err)
		}
	})
}
