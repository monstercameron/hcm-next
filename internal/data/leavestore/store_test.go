package leavestore_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/leavestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func itoa(n int) string { return strconv.Itoa(n) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newLeaveDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 281); err != nil {
		t.Fatalf("apply migrations through 00281: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	c := db.NewConn(t)
	if _, err := c.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return c
}

func withTenant(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// withTenantAborted runs fn inside a transaction that always rolls back, so
// a negative probe that poisons its transaction never touches committed state.
func withTenantAborted(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
}

func seedChain(t *testing.T, store leavestore.Store, conn *pgxadapter.Conn, tenant uuid.UUID) (requestID, recordID uuid.UUID) {
	t.Helper()
	requestID = uuid.New()
	recordID = uuid.New()
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		if _, err := store.CreateRequest(ctx, tx, tenant, leavestore.Request{RequestID: requestID, Revision: 1, State: "APPROVED", ProposalDigest: "sha256:proposal", IdempotencyKey: "req:w1:1"}); err != nil {
			return err
		}
		if err := store.AppendRecord(ctx, tx, tenant, leavestore.Record{RecordID: recordID, RequestID: requestID, RequestRevision: 1, Revision: 1, State: "LEAVE_ACTIVE", ProposalDigest: "sha256:proposal"}); err != nil {
			return err
		}
		if err := store.PutEligibility(ctx, tx, tenant, recordID, 1, leavestore.Eligibility{ProgramID: "fmla", Authority: "statutory", Result: "ELIGIBLE", RuleID: "tenure-12mo", RuleVersion: "v3", Digest: "sha256:elig"}); err != nil {
			return err
		}
		if err := store.PutSegments(ctx, tx, tenant, recordID, 1, []leavestore.Segment{{StartDay: 10, EndDay: 17, Kind: "paid", Hours: 64, Programs: []string{"fmla"}}}); err != nil {
			return err
		}
		if err := store.LinkAbsence(ctx, tx, tenant, recordID, 1, "absence:w1"); err != nil {
			return err
		}
		if _, err := store.AppendAvailability(ctx, tx, tenant, leavestore.Availability{WorkerRef: "w1", State: "UNAVAILABLE", IntentRef: "intent:leave:w1", ProposalDigest: "sha256:proposal", Reason: "leave-start", LeaveEventID: "leave:w1:sep", EffectiveStart: 10, EffectiveEnd: 17, Digest: "sha256:avail"}); err != nil {
			return err
		}
		if _, err := store.PostBalance(ctx, tx, tenant, recordID, 1, leavestore.BalancePosting{AccountRef: "bucket:pto", Opening: "72.00", Ending: "48.00", Remainder: "48.00", ReceiptDigest: "sha256:receipt", IdempotencyKey: "post:w1:1"}); err != nil {
			return err
		}
		if err := store.PutEvidenceRef(ctx, tx, tenant, recordID, 1, leavestore.EvidenceRef{Ref: "evidence:note-8", Compartment: "medical", Quarantined: true, Classified: true, AuthorityCurrent: true}); err != nil {
			return err
		}
		if err := store.PutRestriction(ctx, tx, tenant, recordID, 1, "no-lifting", "readiness:rev-2"); err != nil {
			return err
		}
		if err := store.PutObligation(ctx, tx, tenant, recordID, 1, "protect:job", "OPEN"); err != nil {
			return err
		}
		return store.LinkIntent(ctx, tx, tenant, recordID, 1, "sha256:child-intent", "child")
	})
	return requestID, recordID
}

func TestTodo_DB_023(t *testing.T) {
	db := newLeaveDB(t)
	tenant := insertTenant(t, db, "leave-materialize")
	conn := appConn(t, db)
	store := leavestore.New()
	_, recordID := seedChain(t, store, conn, tenant)
	// Every table materializes its rows for the record.
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		for table, want := range map[string]int{
			"leave_record": 1, "leave_program_eligibility": 1, "leave_entitlement_segment": 1,
			"leave_absence_link": 1, "leave_evidence_ref": 1, "leave_work_restriction": 1,
			"leave_obligation": 1, "leave_intent_link": 1, "leave_balance_posting": 1,
		} {
			count, err := store.CountRevisions(ctx, tx, tenant, table, recordID)
			if err != nil || count != want {
				t.Fatalf("%s = %d, want %d (err %v)", table, count, want, err)
			}
		}
		return nil
	})
	// Orphan children cannot commit: the FK refuses them.
	withTenantAborted(t, conn, tenant, func(tx dbport.Tx) error {
		if err := store.PutObligation(context.Background(), tx, tenant, uuid.New(), 1, "protect:job", "OPEN"); err == nil {
			t.Fatal("orphan obligation committed")
		}
		return nil
	})
}

func TestTodo_DB_023_Property(t *testing.T) {
	db := newLeaveDB(t)
	tenant := insertTenant(t, db, "leave-property")
	conn := appConn(t, db)
	store := leavestore.New()
	// Idempotent replays return the existing rows: nothing posts twice.
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		requestID := uuid.New()
		existed, err := store.CreateRequest(ctx, tx, tenant, leavestore.Request{RequestID: requestID, Revision: 1, State: "DRAFT", ProposalDigest: "sha256:p", IdempotencyKey: "req:replay"})
		if err != nil || existed {
			t.Fatalf("existed=%v err=%v", existed, err)
		}
		existed, err = store.CreateRequest(ctx, tx, tenant, leavestore.Request{RequestID: uuid.New(), Revision: 1, State: "DRAFT", ProposalDigest: "sha256:p", IdempotencyKey: "req:replay"})
		if err != nil || !existed {
			t.Fatalf("replay existed=%v err=%v", existed, err)
		}
		return nil
	})
	// Stale compare-and-swap refuses: revisions advance exactly.
	requestID, recordID := seedChain(t, store, conn, tenant)
	withTenantAborted(t, conn, tenant, func(tx dbport.Tx) error {
		err := store.AppendRecord(context.Background(), tx, tenant, leavestore.Record{RecordID: recordID, RequestID: requestID, RequestRevision: 1, Revision: 1, State: "CLOSED", ProposalDigest: "sha256:p"})
		if err == nil {
			t.Fatal("stale revision appended")
		}
		return nil
	})
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		return store.AppendRecord(context.Background(), tx, tenant, leavestore.Record{RecordID: recordID, RequestID: requestID, RequestRevision: 1, Revision: 2, State: "CLOSED", ProposalDigest: "sha256:p"})
	})
}

func TestTodo_DB_023_Golden(t *testing.T) {
	db := newLeaveDB(t)
	tenant := insertTenant(t, db, "leave-golden")
	conn := appConn(t, db)
	store := leavestore.New()
	_, recordID := seedChain(t, store, conn, tenant)
	// The seeded chain renders one deterministic snapshot: per-table row
	// counts plus the literal values that pin the kernel contract.
	var lines []string
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		tables := []string{"leave_record", "leave_program_eligibility", "leave_entitlement_segment", "leave_absence_link", "leave_medical_detail", "leave_evidence_ref", "leave_work_restriction", "leave_obligation", "leave_intent_link", "leave_balance_posting"}
		for _, table := range tables {
			count, err := store.CountRevisions(ctx, tx, tenant, table, recordID)
			if err != nil {
				return err
			}
			lines = append(lines, table+"="+itoa(count))
		}
		var availCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM leave_availability_revision WHERE tenant_id=$1 AND leave_event_id='leave:w1:sep'`, tenant).Scan(&availCount); err != nil {
			return err
		}
		lines = append(lines, "leave_availability_revision="+itoa(availCount))
		var state, result, kind, absence, avail, ending, ref, restriction, obligation, intentKind string
		var hours float64
		if err := tx.QueryRow(ctx, `SELECT state FROM leave_record WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&state); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT result FROM leave_program_eligibility WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&result); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT kind, hours FROM leave_entitlement_segment WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&kind, &hours); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT absence_id FROM leave_absence_link WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&absence); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT state FROM leave_availability_revision WHERE tenant_id=$1 AND leave_event_id='leave:w1:sep'`, tenant).Scan(&avail); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT ending FROM leave_balance_posting WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&ending); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT ref FROM leave_evidence_ref WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&ref); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT restriction FROM leave_work_restriction WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&restriction); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT state FROM leave_obligation WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&obligation); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT kind FROM leave_intent_link WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&intentKind); err != nil {
			return err
		}
		lines = append(lines,
			"record.state="+state,
			"eligibility.result="+result,
			"segment.kind="+kind,
			"segment.hours="+ftoa(hours),
			"absence.link="+absence,
			"availability.state="+avail,
			"balance.ending="+ending,
			"evidence.ref="+ref,
			"restriction="+restriction,
			"obligation.state="+obligation,
			"intent.kind="+intentKind,
		)
		return nil
	})
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave_chain.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_DB_023_Race(t *testing.T) {
	db := newLeaveDB(t)
	tenant := insertTenant(t, db, "leave-race")
	conn := appConn(t, db)
	store := leavestore.New()
	_, recordID := seedChain(t, store, conn, tenant)
	// One idempotency receipt posts once: every other concurrent racer
	// observes the replay instead of posting a second receipt.
	const workers = 8
	type outcome struct {
		posted bool
		err    error
	}
	out := make([]outcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := db.NewConn(t)
			ctx := context.Background()
			if _, err := c.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
				out[i].err = err
				return
			}
			tx, err := c.Begin(ctx)
			if err != nil {
				out[i].err = err
				return
			}
			if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
				_ = tx.Rollback(ctx)
				out[i].err = err
				return
			}
			ok, err := store.PostBalance(ctx, tx, tenant, recordID, 1, leavestore.BalancePosting{AccountRef: "bucket:pto", Opening: "72.00", Ending: "24.00", Remainder: "24.00", ReceiptDigest: "sha256:r", IdempotencyKey: "post:race"})
			if err != nil {
				_ = tx.Rollback(ctx)
				out[i].err = err
				return
			}
			out[i].posted = ok
			if err := tx.Commit(ctx); err != nil {
				out[i].err = err
			}
		}(i)
	}
	wg.Wait()
	wins := 0
	for i, o := range out {
		if o.err != nil {
			t.Fatalf("racer %d: %v", i, o.err)
		}
		if o.posted {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1", wins)
	}
}

func TestTodo_DB_023_Integration(t *testing.T) {
	db := newLeaveDB(t)
	tenant := insertTenant(t, db, "leave-integration")
	conn := appConn(t, db)
	store := leavestore.New()
	requestID, recordID := seedChain(t, store, conn, tenant)
	// The full chain reads back inside one transaction: the worker's leave
	// posture joins the request, the active record, the availability row
	// the leave event pinned, the balance receipt and the open obligation.
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		var requestState, recordState, availState, availWorker, ending, obligation string
		var start, end int64
		row := tx.QueryRow(ctx, `SELECT q.state, r.state
			FROM leave_request q JOIN leave_record r USING (tenant_id, request_id)
			WHERE q.tenant_id=$1 AND q.request_id=$2 AND r.record_id=$3`,
			tenant, requestID, recordID)
		if err := row.Scan(&requestState, &recordState); err != nil {
			return err
		}
		if requestState != "APPROVED" || recordState != "LEAVE_ACTIVE" {
			t.Fatalf("posture = %s/%s, want APPROVED/LEAVE_ACTIVE", requestState, recordState)
		}
		row = tx.QueryRow(ctx, `SELECT worker_ref, state, effective_start, effective_end
			FROM leave_availability_revision WHERE tenant_id=$1 AND leave_event_id='leave:w1:sep'`,
			tenant)
		if err := row.Scan(&availWorker, &availState, &start, &end); err != nil {
			return err
		}
		if availWorker != "w1" || availState != "UNAVAILABLE" || start != 10 || end != 17 {
			t.Fatalf("availability = %s/%s/%d/%d", availWorker, availState, start, end)
		}
		if err := tx.QueryRow(ctx, `SELECT ending FROM leave_balance_posting
			WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&ending); err != nil {
			return err
		}
		if ending != "48.00" {
			t.Fatalf("ending = %s, want 48.00", ending)
		}
		if err := tx.QueryRow(ctx, `SELECT state FROM leave_obligation
			WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID).Scan(&obligation); err != nil {
			return err
		}
		if obligation != "OPEN" {
			t.Fatalf("obligation = %s, want OPEN", obligation)
		}
		return nil
	})
}

func TestTodo_DB_023_Security(t *testing.T) {
	db := newLeaveDB(t)
	tenant := insertTenant(t, db, "leave-security")
	conn := appConn(t, db)
	store := leavestore.New()
	requestID, recordID := seedChain(t, store, conn, tenant)
	// Overlapping incompatible active leave refuses.
	withTenantAborted(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.AppendAvailability(context.Background(), tx, tenant, leavestore.Availability{WorkerRef: "w1", State: "RESTRICTED", IntentRef: "intent:x", ProposalDigest: "sha256:p", Reason: "clash", LeaveEventID: "leave:w1:clash", EffectiveStart: 12, EffectiveEnd: 14, Digest: "sha256:c"})
		if err == nil {
			t.Fatal("overlapping incompatible leave committed")
		}
		return nil
	})
	// Employment inactivation refuses at the constraint.
	withTenantAborted(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO leave_record
			(tenant_id, record_id, request_id, request_revision, revision, state, employment_state, proposal_digest)
			VALUES ($1,$2,$3,1,1,'OPEN','TERMINATED','sha256:p')`, tenant, uuid.New(), requestID)
		if err == nil {
			t.Fatal("inactivating transition committed")
		}
		return nil
	})
	// Medical detail is role-gated: managers have no reader path.
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		if err := store.PutMedicalDetail(ctx, tx, tenant, recordID, 1, "medical:note-7"); err != nil {
			return err
		}
		if _, err := store.MedicalDetail(ctx, tx, tenant, recordID, "manager"); err == nil {
			t.Fatal("manager read medical detail")
		}
		details, err := store.MedicalDetail(ctx, tx, tenant, recordID, "leave-administrator")
		if err != nil || len(details) != 1 || details[0] != "medical:note-7" {
			t.Fatalf("details=%v err=%v", details, err)
		}
		return nil
	})
	// Mutable revisions refuse: the trigger blocks UPDATE.
	withTenantAborted(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE leave_obligation SET state='SATISFIED'
			WHERE tenant_id=$1 AND record_id=$2`, tenant, recordID)
		if err == nil {
			t.Fatal("mutable revision committed")
		}
		return nil
	})
}
