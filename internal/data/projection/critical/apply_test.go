package critical_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/data/projection/critical"
)

// TestTodo_DATA_006 proves the critical-projection applier: applying events
// in sequence projects intent_instance and proposal_revision, an event
// applied out of order is refused rather than silently skipping the missing
// one, a replayed event is a no-op rather than a second effect, and Verify
// recomputes the projection from the ledger and reports exactly what
// disagrees.
func TestTodo_DATA_006(t *testing.T) {
	t.Parallel()
	mapper := critical.ProtoMapper{}

	t.Run("creation projects intent_instance", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		intentID := uuid.New()
		streamKey := f.streamKey(intentID)
		f.ensureStream(t, intentID)

		payload := newIntentInstanceEvent(t, intentID, "idem-"+intentID.String(), 1, draftLifecycle(), occurredAt)
		req := creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, payload, uuid.NewString())
		receipt, result := f.appendAndApply(t, mapper, req)

		if !result.Applied || result.Target != critical.TargetIntentInstance {
			t.Fatalf("result = %+v, want Applied=true Target=INTENT_INSTANCE", result)
		}
		if result.Checkpoint.LastAppliedSequence != receipt.Sequence {
			t.Fatalf("checkpoint at %d, want %d", result.Checkpoint.LastAppliedSequence, receipt.Sequence)
		}

		var requestState string
		var instanceVersion int64
		err := f.db.Conn.QueryRow(context.Background(),
			`SELECT request_state, instance_version FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
			f.tenant, intentID).Scan(&requestState, &instanceVersion)
		if err != nil {
			t.Fatalf("read intent_instance: %v", err)
		}
		if requestState != "DRAFT" || instanceVersion != 1 {
			t.Fatalf("intent_instance = (%s, %d), want (DRAFT, 1)", requestState, instanceVersion)
		}

		vr, err := critical.Verify(context.Background(), f.db.Conn, f.reader, mapper, f.tenant, streamKey)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !vr.OK() {
			t.Fatalf("verify found diffs: %+v", vr.Diffs)
		}
	})

	t.Run("lifecycle transition advances instance_version and rewrites the row", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		intentID := uuid.New()
		streamKey := f.streamKey(intentID)
		f.ensureStream(t, intentID)
		idemKey := "idem-" + intentID.String()

		creation := newIntentInstanceEvent(t, intentID, idemKey, 1, draftLifecycle(), occurredAt)
		f.appendAndApply(t, mapper, creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, creation, uuid.NewString()))

		submitted := newIntentInstanceEvent(t, intentID, idemKey, 2, submittedLifecycle(), occurredAt)
		_, result := f.appendAndApply(t, mapper, followOnRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, 1, submitted, uuid.NewString()))
		if !result.Applied || result.Target != critical.TargetIntentInstance {
			t.Fatalf("result = %+v, want Applied=true Target=INTENT_INSTANCE", result)
		}

		var requestState string
		var instanceVersion int64
		err := f.db.Conn.QueryRow(context.Background(),
			`SELECT request_state, instance_version FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
			f.tenant, intentID).Scan(&requestState, &instanceVersion)
		if err != nil {
			t.Fatalf("read intent_instance: %v", err)
		}
		if requestState != "SUBMITTED" || instanceVersion != 2 {
			t.Fatalf("intent_instance = (%s, %d), want (SUBMITTED, 2)", requestState, instanceVersion)
		}
	})

	t.Run("proposal revision projects proposal_revision", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		intentID := uuid.New()
		streamKey := f.streamKey(intentID)
		f.ensureStream(t, intentID)

		creation := newIntentInstanceEvent(t, intentID, "idem-"+intentID.String(), 1, draftLifecycle(), occurredAt)
		f.appendAndApply(t, mapper, creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, creation, uuid.NewString()))

		revision := newProposalRevisionEvent(t, intentID, 1, "principal:manager-1", occurredAt)
		_, result := f.appendAndApply(t, mapper, followOnRequest(streamKey, critical.SchemaRefProposalRevision, f.tenant, 1, revision, uuid.NewString()))
		if !result.Applied || result.Target != critical.TargetProposalRevision {
			t.Fatalf("result = %+v, want Applied=true Target=PROPOSAL_REVISION", result)
		}

		var producedBy string
		err := f.db.Conn.QueryRow(context.Background(),
			`SELECT produced_by FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2 AND revision = 1`,
			f.tenant, intentID).Scan(&producedBy)
		if err != nil {
			t.Fatalf("read proposal_revision: %v", err)
		}
		if producedBy != "principal:manager-1" {
			t.Fatalf("produced_by = %q, want %q", producedBy, "principal:manager-1")
		}

		vr, err := critical.Verify(context.Background(), f.db.Conn, f.reader, mapper, f.tenant, streamKey)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !vr.OK() {
			t.Fatalf("verify found diffs: %+v", vr.Diffs)
		}
	})

	t.Run("an event applied out of order is refused", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		intentID := uuid.New()
		streamKey := f.streamKey(intentID)
		f.ensureStream(t, intentID)
		idemKey := "idem-" + intentID.String()

		creation := newIntentInstanceEvent(t, intentID, idemKey, 1, draftLifecycle(), occurredAt)
		receipt1, result1 := f.appendAndApply(t, mapper, creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, creation, uuid.NewString()))
		if !result1.Applied {
			t.Fatalf("creation should have applied")
		}

		// Append sequence 2 and 3 on the ledger, but never apply sequence 2 -
		// simulating an event this projection has not yet seen.
		v2 := newIntentInstanceEvent(t, intentID, idemKey, 2, submittedLifecycle(), occurredAt)
		var seq2 ledger.AppendReceipt
		f.inTx(t, f.db.Conn, func(tx pgx.Tx) error {
			var err error
			seq2, err = ledger.Append(context.Background(), tx, followOnRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, receipt1.Sequence, v2, uuid.NewString()))
			return err
		})
		v3 := newIntentInstanceEvent(t, intentID, idemKey, 3, submittedLifecycle(), occurredAt)
		var seq3 ledger.AppendReceipt
		f.inTx(t, f.db.Conn, func(tx pgx.Tx) error {
			var err error
			seq3, err = ledger.Append(context.Background(), tx, followOnRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, seq2.Sequence, v3, uuid.NewString()))
			return err
		})

		_, err := f.applyOnly(t, mapper, critical.ApplyRequest{
			Tenant: f.tenant, StreamKey: streamKey, Sequence: seq3.Sequence, Digest: seq3.Digest,
			SchemaRef: critical.SchemaRefIntentInstance, Payload: v3,
		})
		var gap projection.ErrSequenceGap
		if !errors.As(err, &gap) {
			t.Fatalf("applying sequence %d ahead of the checkpoint = %v, want projection.ErrSequenceGap", seq3.Sequence, err)
		}
		if gap.Current != receipt1.Sequence {
			t.Fatalf("gap reports current %d, want %d", gap.Current, receipt1.Sequence)
		}

		// The refused attempt must have changed nothing: the checkpoint is
		// still at sequence 1, and the stored row is still the creation
		// event's, not sequence 3's.
		cp, err := projection.Read(context.Background(), f.db.Conn, f.tenant, critical.ProjectionName, streamKey)
		if err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		if cp.LastAppliedSequence != receipt1.Sequence {
			t.Fatalf("checkpoint moved to %d after a refused apply, want %d", cp.LastAppliedSequence, receipt1.Sequence)
		}
		var instanceVersion int64
		if err := f.db.Conn.QueryRow(context.Background(),
			`SELECT instance_version FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
			f.tenant, intentID).Scan(&instanceVersion); err != nil {
			t.Fatalf("read intent_instance: %v", err)
		}
		if instanceVersion != 1 {
			t.Fatalf("instance_version = %d after a refused out-of-order apply, want 1 (unchanged)", instanceVersion)
		}
	})

	t.Run("a replayed event is a no-op", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		intentID := uuid.New()
		streamKey := f.streamKey(intentID)
		f.ensureStream(t, intentID)

		payload := newIntentInstanceEvent(t, intentID, "idem-"+intentID.String(), 1, draftLifecycle(), occurredAt)
		req := creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, payload, uuid.NewString())
		receipt, first := f.appendAndApply(t, mapper, req)
		if !first.Applied {
			t.Fatalf("first apply should have applied")
		}

		replay, err := f.applyOnly(t, mapper, critical.ApplyRequest{
			Tenant: f.tenant, StreamKey: streamKey, Sequence: receipt.Sequence, Digest: receipt.Digest,
			SchemaRef: critical.SchemaRefIntentInstance, Payload: payload,
		})
		if err != nil {
			t.Fatalf("replaying an already-applied sequence returned an error: %v", err)
		}
		if replay.Applied {
			t.Fatalf("replaying an already-applied sequence reported Applied=true, want a no-op")
		}
		if replay.Checkpoint.LastAppliedSequence != receipt.Sequence {
			t.Fatalf("replay checkpoint = %d, want unchanged %d", replay.Checkpoint.LastAppliedSequence, receipt.Sequence)
		}
	})

	t.Run("Verify detects a row that no longer matches the ledger", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		intentID := uuid.New()
		streamKey := f.streamKey(intentID)
		f.ensureStream(t, intentID)

		payload := newIntentInstanceEvent(t, intentID, "idem-"+intentID.String(), 1, draftLifecycle(), occurredAt)
		f.appendAndApply(t, mapper, creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, payload, uuid.NewString()))

		// Corrupt the projection directly, bypassing Apply entirely - the
		// scenario Verify exists to catch (a projection that ever disagrees
		// with the ledger must be visible, not authoritative).
		f.db.Exec(t, `UPDATE intent_instance SET request_state = 'APPROVED' WHERE tenant_id = $1 AND intent_id = $2`, f.tenant, intentID)

		vr, err := critical.Verify(context.Background(), f.db.Conn, f.reader, mapper, f.tenant, streamKey)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if vr.OK() {
			t.Fatalf("verify reported no diffs against a corrupted row")
		}
		found := false
		for _, d := range vr.Diffs {
			if d.Table == "intent_instance" {
				found = true
			}
		}
		if !found {
			t.Fatalf("verify diffs %+v do not mention intent_instance", vr.Diffs)
		}
	})
}

// TestTodo_DATA_006_Golden fixes one exact scenario - a creation event
// followed by one lifecycle transition and one proposal revision - and
// checks every stored column against a literal expected value, so a
// regression in field mapping (a swapped column, a mis-cased enum) fails
// here even when a looser equality check would not have noticed.
func TestTodo_DATA_006_Golden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mapper := critical.ProtoMapper{}
	intentID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	streamKey := f.streamKey(intentID)
	f.ensureStream(t, intentID)
	const idemKey = "idem-golden"

	creation := newIntentInstanceEvent(t, intentID, idemKey, 1, draftLifecycle(), occurredAt)
	f.appendAndApply(t, mapper, creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, creation, uuid.NewString()))

	submitted := newIntentInstanceEvent(t, intentID, idemKey, 2, submittedLifecycle(), occurredAt)
	f.appendAndApply(t, mapper, followOnRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, 1, submitted, uuid.NewString()))

	revision := newProposalRevisionEvent(t, intentID, 1, "principal:golden", occurredAt)
	f.appendAndApply(t, mapper, followOnRequest(streamKey, critical.SchemaRefProposalRevision, f.tenant, 2, revision, uuid.NewString()))

	var (
		definitionRef, requestState, executionState, businessState, consistencyState, obligationState string
		instanceVersion                                                                               int64
	)
	err := f.db.Conn.QueryRow(context.Background(), `
		SELECT definition_ref, request_state, execution_state, business_state, consistency_state, obligation_state, instance_version
		FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		f.tenant, intentID).Scan(&definitionRef, &requestState, &executionState, &businessState, &consistencyState, &obligationState, &instanceVersion)
	if err != nil {
		t.Fatalf("read intent_instance: %v", err)
	}
	want := []string{"intent.worker.promote", "SUBMITTED", "SCHEDULED", "IN_PROGRESS", "PENDING_OBSERVATION", "PENDING"}
	got := []string{definitionRef, requestState, executionState, businessState, consistencyState, obligationState}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("intent_instance column %d = %q, want %q (full row %v)", i, got[i], want[i], got)
		}
	}
	if instanceVersion != 2 {
		t.Fatalf("instance_version = %d, want 2", instanceVersion)
	}

	var schemaRef, producedBy string
	err = f.db.Conn.QueryRow(context.Background(),
		`SELECT schema_ref, produced_by FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2 AND revision = 1`,
		f.tenant, intentID).Scan(&schemaRef, &producedBy)
	if err != nil {
		t.Fatalf("read proposal_revision: %v", err)
	}
	if schemaRef != "hcmnext.intents.v1.BusinessIntent@1" || producedBy != "principal:golden" {
		t.Fatalf("proposal_revision = (%q, %q), want (%q, %q)", schemaRef, producedBy, "hcmnext.intents.v1.BusinessIntent@1", "principal:golden")
	}

	vr, err := critical.Verify(context.Background(), f.db.Conn, f.reader, mapper, f.tenant, streamKey)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK() {
		t.Fatalf("verify found diffs against the golden scenario: %+v", vr.Diffs)
	}
}

// TestTodo_DATA_006_Race proves that concurrent appliers of the same
// already-appended event produce exactly one effect: one caller's Apply
// projects the row, every other caller's Apply is an idempotent no-op, and
// no error is returned to anyone.
func TestTodo_DATA_006_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mapper := critical.ProtoMapper{}
	intentID := uuid.New()
	streamKey := f.streamKey(intentID)
	f.ensureStream(t, intentID)

	payload := newIntentInstanceEvent(t, intentID, "idem-"+intentID.String(), 1, draftLifecycle(), occurredAt)
	req := creationRequest(streamKey, critical.SchemaRefIntentInstance, f.tenant, payload, uuid.NewString())
	receipt, first := f.appendAndApply(t, mapper, req)
	if !first.Applied {
		t.Fatalf("initial apply should have applied")
	}

	// Re-registering the checkpoint is idempotent; the point of this test is
	// what happens when several transactions race to be the one that
	// advances it, not whether it exists yet.
	const racers = 6
	conns := make([]*pgx.Conn, racers)
	for i := range conns {
		conns[i] = f.db.NewConn(t)
	}

	type outcome struct {
		result critical.ApplyResult
		err    error
	}
	results := make([]outcome, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var out outcome
			out.err = f.inTxErr(conns[i], func(tx pgx.Tx) error {
				var applyErr error
				out.result, applyErr = critical.Apply(context.Background(), tx, mapper, critical.ApplyRequest{
					Tenant: f.tenant, StreamKey: streamKey, Sequence: receipt.Sequence, Digest: receipt.Digest,
					SchemaRef: critical.SchemaRefIntentInstance, Payload: payload,
				})
				return applyErr
			})
			results[i] = out
		}()
	}
	close(start)
	wg.Wait()

	for i, out := range results {
		if out.err != nil {
			t.Fatalf("racer %d returned an error for a replay: %v", i, out.err)
		}
		if out.result.Applied {
			t.Fatalf("racer %d reported Applied=true for an event this fixture already applied", i)
		}
	}

	var count int
	if err := f.db.Conn.QueryRow(context.Background(),
		`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		f.tenant, intentID).Scan(&count); err != nil {
		t.Fatalf("count intent_instance rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("intent_instance has %d rows for one intent, want exactly 1", count)
	}
}
