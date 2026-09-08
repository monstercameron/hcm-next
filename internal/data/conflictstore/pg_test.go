package conflictstore_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/conflictstore"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transaction/conflict"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	store  *conflictstore.Store
}

func newFixture(t *testing.T) fixture {
	f := fixture{db: pgtest.New(t), tenant: uuid.New(), store: conflictstore.New()}
	f.db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-conflict','Conflict Test','ACTIVE',now())`, f.tenant, "conflict-"+f.tenant.String())
	return f
}

func (f fixture) tx(conn *pgxadapter.Conn, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenant.String()); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (f fixture) ensureStream(t *testing.T, key string) {
	f.db.Exec(t, `INSERT INTO ledger_stream (tenant_id,stream_key,stream_kind,subject_ref) VALUES ($1,$2,'TRANSACTION',$2)`, f.tenant, key)
	f.db.Exec(t, `INSERT INTO stream_head (tenant_id,stream_key,head_sequence) VALUES ($1,$2,0)`, f.tenant, key)
}

func (f fixture) footprint(t *testing.T, stream, field string, start, end time.Time) conflict.WriteFootprint {
	resource, err := values.NewResourceKey(values.TenantId(f.tenant.String()), values.Kind("assignment"), "worker", "9001")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewInstantInterval(values.NewInstant(start), values.NewInstant(end))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision(stream, 0)
	if err != nil {
		t.Fatal(err)
	}
	return conflict.WriteFootprint{Resource: resource, Field: conflict.FieldPath(field), Interval: interval, Operation: conflict.OperationUpdate, ExpectedRevision: revision, Authority: conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local/v1"}}
}

func intentFor(f fixture, id string, footprints ...conflict.WriteFootprint) conflict.WriteIntent {
	return conflict.WriteIntent{TenantID: f.tenant.String(), ID: id, ProposalID: "proposal-" + id, SnapshotDigest: "snapshot-1", Footprints: footprints}
}

func commitRequest(in conflict.WriteIntent) conflict.CommitRequest {
	streams := make(map[string]uint64)
	writes := make([]conflict.WriteBaseline, 0, len(in.Footprints))
	digests := make([]string, 0, len(in.Footprints))
	for _, footprint := range in.Footprints {
		sequence, _ := footprint.ExpectedRevision.Sequence()
		streams[footprint.ExpectedRevision.Stream()] = sequence
		writes = append(writes, conflict.WriteBaseline{ResourceCanonical: footprint.Resource.String(), FieldPath: footprint.Field, StreamKey: footprint.ExpectedRevision.Stream(), AuthorityDomain: footprint.Authority.Domain, SourceAuthorityDecision: footprint.Authority.PolicyRef, Operation: footprint.Operation, EffectiveInterval: footprint.Interval})
		digests = append(digests, footprint.ScopeDigest())
	}
	baselines := make([]conflict.StreamBaseline, 0, len(streams))
	for stream, sequence := range streams {
		baselines = append(baselines, conflict.StreamBaseline{StreamKey: stream, ExpectedSequence: sequence})
	}
	return conflict.CommitRequest{TenantID: in.TenantID, IntentID: in.ID, SnapshotDigest: in.SnapshotDigest, Streams: baselines, Writes: writes, FootprintDigests: digests}
}

func TestTodo_CONFLICT_003_Integration(t *testing.T) {
	f := newFixture(t)
	f.ensureStream(t, "people:9001")
	base := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	parent := f.footprint(t, "people:9001", "employment.assignment", base, base.Add(48*time.Hour))
	child := f.footprint(t, "people:9001", "employment.assignment.grade", base.Add(24*time.Hour), base.Add(72*time.Hour))
	intents := []conflict.WriteIntent{intentFor(f, "left", parent), intentFor(f, "right", child)}
	for _, in := range intents {
		if err := f.tx(f.db.Conn, func(tx dbport.Tx) error { return f.store.Register(context.Background(), tx, in) }); err != nil {
			t.Fatalf("register %s: %v", in.ID, err)
		}
	}

	connections := []*pgxadapter.Conn{f.db.NewConn(t), f.db.NewConn(t)}
	type outcome struct {
		result conflict.CommitResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for i := range connections {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var result conflict.CommitResult
			err := f.tx(connections[i], func(tx dbport.Tx) error {
				var e error
				result, e = f.store.ValidateAtCommit(context.Background(), tx, commitRequest(intents[i]))
				return e
			})
			outcomes <- outcome{result, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(outcomes)
	wins, stale := 0, 0
	var winner conflict.CommitResult
	for got := range outcomes {
		if got.err == nil {
			wins++
			winner = got.result
			continue
		}
		if errors.Is(got.err, conflict.ErrStaleBaseline) {
			stale++
			continue
		}
		t.Fatalf("unexpected contender error: %v", got.err)
	}
	if wins != 1 || stale != 1 {
		t.Fatalf("outcomes wins=%d stale=%d, want one each", wins, stale)
	}
	if winner.Fence == 0 || winner.Intent.Status != conflict.IntentCommitted {
		t.Fatalf("winner = %+v", winner)
	}

	restart := f.db.NewConn(t)
	for i := 0; i < 2; i++ {
		if err := f.tx(restart, func(tx dbport.Tx) error {
			return f.store.Release(context.Background(), tx, f.tenant.String(), winner.Intent.ID, winner.Fence)
		}); err != nil {
			t.Fatalf("release %d: %v", i, err)
		}
	}
	var status string
	if err := restart.QueryRow(context.Background(), `SELECT status FROM conflict_write_intent WHERE tenant_id=$1 AND intent_id=$2`, f.tenant, winner.Intent.ID).Scan(&status); err != nil || status != "RELEASED" {
		t.Fatalf("released status=%q err=%v", status, err)
	}
}

func TestTodo_CONFLICT_003_RegistrationAndIntervals(t *testing.T) {
	f := newFixture(t)
	f.ensureStream(t, "people:9001")
	base := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	first := f.footprint(t, "people:9001", "employment.assignment.grade", base, base.Add(24*time.Hour))
	touching := f.footprint(t, "people:9001", "employment.assignment.grade", base.Add(24*time.Hour), base.Add(48*time.Hour))
	in := intentFor(f, "cardinality", first, touching)
	for i := 0; i < 2; i++ {
		if err := f.tx(f.db.Conn, func(tx dbport.Tx) error { return f.store.Register(context.Background(), tx, in) }); err != nil {
			t.Fatalf("registration %d: %v", i, err)
		}
	}
	short := in
	short.Footprints = short.Footprints[:1]
	if err := f.tx(f.db.Conn, func(tx dbport.Tx) error { return f.store.Register(context.Background(), tx, short) }); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("short registration = %v", err)
	}
	var count int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM conflict_write_footprint WHERE tenant_id=$1 AND intent_id=$2`, f.tenant, in.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("footprint count=%d err=%v", count, err)
	}

	// [a,b) and [b,c) are disjoint and may both commit.
	for _, candidate := range []conflict.WriteIntent{intentFor(f, "single", first), intentFor(f, "touching", touching)} {
		if err := f.tx(f.db.Conn, func(tx dbport.Tx) error { return f.store.Register(context.Background(), tx, candidate) }); err != nil {
			t.Fatal(err)
		}
		if err := f.tx(f.db.Conn, func(tx dbport.Tx) error {
			_, err := f.store.ValidateAtCommit(context.Background(), tx, commitRequest(candidate))
			return err
		}); err != nil {
			t.Fatalf("fence %s: %v", candidate.ID, err)
		}
	}

	dateStart, err := values.NewLocalDate(2027, time.March, 1)
	if err != nil {
		t.Fatal(err)
	}
	dateEnd, err := values.NewLocalDate(2027, time.March, 10)
	if err != nil {
		t.Fatal(err)
	}
	dateInterval, err := values.NewLocalDateInterval(dateStart, dateEnd, values.CalendarRef{Ref: "gregorian", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	dateFootprint := first
	dateFootprint.Interval = dateInterval
	dateIntent := intentFor(f, "local-date", dateFootprint)
	if err := f.tx(f.db.Conn, func(tx dbport.Tx) error { return f.store.Register(context.Background(), tx, dateIntent) }); err != nil {
		t.Fatalf("register local-date footprint: %v", err)
	}
	zoned, err := dateInterval.WithZone(values.ZoneRef{ID: "America/New_York", TzdbVersion: "2027a"}, values.DisambiguationRejectGap)
	if err != nil {
		t.Fatal(err)
	}
	mutated := commitRequest(dateIntent)
	mutated.Writes[0].EffectiveInterval = zoned
	if err := f.tx(f.db.Conn, func(tx dbport.Tx) error {
		_, err := f.store.ValidateAtCommit(context.Background(), tx, mutated)
		return err
	}); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("same boundaries with transplanted zone = %v, want intent conflict", err)
	}
	if err := f.tx(f.db.Conn, func(tx dbport.Tx) error {
		_, err := f.store.ValidateAtCommit(context.Background(), tx, commitRequest(dateIntent))
		return err
	}); err != nil {
		t.Fatalf("fence local-date footprint: %v", err)
	}
}

func TestConflictStoreRejectsMissingStreamWithoutLeakingFence(t *testing.T) {
	f := newFixture(t)
	base := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	in := intentFor(f, "missing-stream", f.footprint(t, "absent:stream", "employment.assignment.grade", base, base.Add(time.Hour)))
	if err := f.tx(f.db.Conn, func(tx dbport.Tx) error { return f.store.Register(context.Background(), tx, in) }); err != nil {
		t.Fatal(err)
	}
	err := f.tx(f.db.Conn, func(tx dbport.Tx) error {
		_, err := f.store.ValidateAtCommit(context.Background(), tx, commitRequest(in))
		return err
	})
	if err == nil {
		t.Fatal("missing authoritative stream head was accepted")
	}
	var count int
	if scanErr := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM conflict_scope_fence WHERE tenant_id=$1`, f.tenant).Scan(&count); scanErr != nil || count != 0 {
		t.Fatalf("partial fences=%d err=%v after %v", count, scanErr, err)
	}
}
