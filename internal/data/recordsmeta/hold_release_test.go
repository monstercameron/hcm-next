package recordsmeta_test

// RECORDS-HOLD-001: propagate and release legal holds across all copies.
//
// This file closes the four gaps DB-015 (migration 00032) and DATA-018
// (migration 00056) left open, verified against a real PostgreSQL instance
// through internal/data/pgtest (TestMain lives in recordsmeta_test.go,
// shared by every file in this package):
//
//  1. PropagateHold now writes one hold_intersection per tracked copy,
//     naming the record_copy_link row it grips and a deterministic,
//     explainable reason.
//  2. ReleaseHold walks every intersection a hold's scope names, recalculates
//     each affected copy's eligibility, and never resurrects a copy a
//     second active hold still covers.
//  3. legal_hold_notice and legal_hold_acknowledgement (migration 00284) let
//     a hold's issuance and acknowledgement be proven.
//  4. DisposeCopy proves a held copy cannot be destroyed, of any copy_type
//     record_copy_link tracks, while an unrelated declaration keeps
//     disposing normally.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
)

// recordCopyTypes is every copy_type record_copy_link_type_allowed
// (migration 00056) permits. TestTodo_RECORDS_HOLD_001_Mutation uses all
// eight to prove disposition cannot destroy a held copy "of any type".
var recordCopyTypes = []string{"CANONICAL", "PROJECTION", "EXPORT", "ARTIFACT", "OUTBOX", "BACKUP", "PROVIDER", "SEARCH"}

// registerRecordsCopySchema satisfies outbox_schema: PropagateHold's outbox
// row cites RecordsCopySchemaRef, and the outbox table's FK to
// payload_schema requires that reference to exist for this tenant before
// any hold can be propagated.
func registerRecordsCopySchema(t *testing.T, db *pgtest.DB, tenant uuid.UUID) {
	t.Helper()
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,$2,1,$2,'PROTOBUF','EVIDENCE_MANIFEST')`, tenant, recordsmeta.RecordsCopySchemaRef)
}

func registerCopiesInOrder(t *testing.T, tx dbport.Tx, tenant, declarationID uuid.UUID, types []string) []recordsmeta.CopyLink {
	t.Helper()
	copies := make([]recordsmeta.CopyLink, 0, len(types))
	for _, copyType := range types {
		link, err := recordsmeta.RegisterCopy(context.Background(), tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declarationID, CopyType: copyType,
			StoreRef: "store:" + strings.ToLower(copyType),
		})
		if err != nil {
			t.Fatalf("register %s copy: %v", copyType, err)
		}
		copies = append(copies, link)
	}
	return copies
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden fixture %s: %v", name, err)
	}
	return strings.TrimRight(string(data), "\n")
}

// isRetryableTxError reports a transient PostgreSQL serialization or
// deadlock failure -- the only outcome concurrent, correctly-ordered lock
// acquisition should ever produce under load, and safe to retry as a whole
// new transaction.
func isRetryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}

func inTenantTxRetrying(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		lastErr = inTenantTxErr(conn, tenant, fn)
		if lastErr == nil || !isRetryableTxError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001 -- the primary test
// ---------------------------------------------------------------------------

func TestTodo_RECORDS_HOLD_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-primary")
	registerRecordsCopySchema(t, db, tenant)

	declaration := newDeclaration(tenant)
	hold := newHold(tenant)
	digest, err := recordsmeta.CanonicalScopeDigest(hold.ScopePredicate)
	if err != nil {
		t.Fatalf("canonical scope digest: %v", err)
	}
	hold.ScopeDigest = digest
	types := []string{"CANONICAL", "EXPORT", "PROVIDER", "BACKUP", "SEARCH"}

	var propagation recordsmeta.HoldPropagation
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, tenant, declaration.DeclarationID, types)
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		var err error
		propagation, err = recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, hold.HoldID, fixedInstant)
		return err
	})

	if len(propagation.Copies) != len(types) {
		t.Fatalf("propagated %d copies, want %d", len(propagation.Copies), len(types))
	}
	for _, c := range propagation.Copies {
		if c.HoldState != "HELD" || c.DispositionState != "HELD" {
			t.Errorf("copy %s (%s) is %s/%s, want HELD/HELD", c.LinkID, c.CopyType, c.HoldState, c.DispositionState)
		}
	}

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		intersections, err := recordsmeta.ListHoldIntersections(ctx, tx, tenant, hold.HoldID)
		if err != nil {
			return err
		}
		if len(intersections) != len(types) {
			return fmt.Errorf("%d hold_intersection rows recorded, want one per copy (%d)", len(intersections), len(types))
		}
		seenLinks := map[uuid.UUID]bool{}
		for _, isec := range intersections {
			if isec.State != "ACTIVE" {
				return fmt.Errorf("intersection %s is %s, want ACTIVE", isec.IntersectionID, isec.State)
			}
			if isec.LinkID == nil {
				return fmt.Errorf("intersection %s names no record_copy_link row", isec.IntersectionID)
			}
			if isec.MatchedReason == "" {
				return fmt.Errorf("intersection %s has no matched reason", isec.IntersectionID)
			}
			seenLinks[*isec.LinkID] = true
		}
		if len(seenLinks) != len(types) {
			return fmt.Errorf("intersections grip %d distinct copies, want %d (per-copy intersections are incomplete)", len(seenLinks), len(types))
		}
		loadedHold, err := recordsmeta.LoadLegalHold(ctx, tx, tenant, hold.HoldID)
		if err != nil {
			return err
		}
		if loadedHold.ScopeDigest != digest {
			return fmt.Errorf("scope snapshot lost: recorded digest %s, want %s", loadedHold.ScopeDigest, digest)
		}
		copies, err := recordsmeta.ListCopies(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return err
		}
		for _, c := range copies {
			if c.HoldState != "HELD" {
				return fmt.Errorf("copy %s (%s) is %s, want HELD", c.LinkID, c.CopyType, c.HoldState)
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Property
// ---------------------------------------------------------------------------

// TestTodo_RECORDS_HOLD_001_Property proves the REFACTOR clause: the same
// scope predicate over the same copy set (same copy_type/store_ref pairs)
// always yields the same matched set and the same MatchedReason, whatever
// order the copies were registered in and whichever declaration they belong
// to.
func TestTodo_RECORDS_HOLD_001_Property(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-property")
	registerRecordsCopySchema(t, db, tenant)

	hold := newHold(tenant)
	digest, err := recordsmeta.CanonicalScopeDigest(hold.ScopePredicate)
	if err != nil {
		t.Fatalf("canonical scope digest: %v", err)
	}
	hold.ScopeDigest = digest
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return recordsmeta.InsertLegalHold(ctx, tx, hold)
	})

	types := []string{"CANONICAL", "EXPORT", "PROVIDER", "BACKUP", "SEARCH"}
	orderings := [][]string{
		{"CANONICAL", "EXPORT", "PROVIDER", "BACKUP", "SEARCH"},
		{"SEARCH", "BACKUP", "PROVIDER", "EXPORT", "CANONICAL"},
		{"PROVIDER", "CANONICAL", "SEARCH", "EXPORT", "BACKUP"},
	}

	var baseline map[string]string
	for round, order := range orderings {
		declaration := newDeclaration(tenant)
		var propagation recordsmeta.HoldPropagation
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
				return err
			}
			registerCopiesInOrder(t, tx, tenant, declaration.DeclarationID, order)
			var err error
			propagation, err = recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, hold.HoldID, fixedInstant.Add(time.Duration(round)*time.Minute))
			return err
		})
		linkType := map[uuid.UUID]string{}
		for _, c := range propagation.Copies {
			linkType[c.LinkID] = c.CopyType
		}

		reasonsThisRound := map[string]string{}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			intersections, err := recordsmeta.ListHoldIntersections(ctx, tx, tenant, hold.HoldID)
			if err != nil {
				return err
			}
			for _, isec := range intersections {
				if isec.DeclarationID != declaration.DeclarationID || isec.LinkID == nil {
					continue
				}
				copyType, ok := linkType[*isec.LinkID]
				if !ok {
					continue
				}
				reasonsThisRound[copyType] = isec.MatchedReason
			}
			return nil
		})

		if len(reasonsThisRound) != len(types) {
			t.Fatalf("ordering %d matched %d copy types, want %d", round, len(reasonsThisRound), len(types))
		}
		if baseline == nil {
			baseline = reasonsThisRound
			continue
		}
		for copyType, reason := range baseline {
			if reasonsThisRound[copyType] != reason {
				t.Fatalf("ordering %d: copy_type %s matched reason %q, want %q (matching is order-dependent)", round, copyType, reasonsThisRound[copyType], reason)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Golden
// ---------------------------------------------------------------------------

var (
	goldenHoldID        = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	goldenDeclarationID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// TestTodo_RECORDS_HOLD_001_Golden pins the scope digest a fixed predicate
// canonicalizes to, the matched reason recorded for a fixed copy under it,
// and the exact propagation payload bytes for a fixed hold/declaration pair.
func TestTodo_RECORDS_HOLD_001_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-golden")
	registerRecordsCopySchema(t, db, tenant)

	predicateKeysAsWritten := json.RawMessage(`{"record_series":"HR-100","record_class":"PERSONNEL","effective_from":"2026-01-01T00:00:00Z"}`)
	predicateKeysReordered := json.RawMessage(`{"effective_from":"2026-01-01T00:00:00Z","record_class":"PERSONNEL","record_series":"HR-100"}`)
	digest, err := recordsmeta.CanonicalScopeDigest(predicateKeysAsWritten)
	if err != nil {
		t.Fatalf("canonical scope digest: %v", err)
	}
	reordered, err := recordsmeta.CanonicalScopeDigest(predicateKeysReordered)
	if err != nil {
		t.Fatalf("canonical scope digest (reordered): %v", err)
	}
	if digest != reordered {
		t.Fatalf("re-ordering the same predicate's keys changed its digest: %s vs %s", digest, reordered)
	}
	if want := readGolden(t, "records_hold_001_scope_digest.golden"); digest != want {
		t.Fatalf("scope digest = %s, want pinned %s", digest, want)
	}

	declaration := newDeclaration(tenant)
	declaration.DeclarationID = goldenDeclarationID
	hold := newHold(tenant)
	hold.HoldID = goldenHoldID
	hold.ScopePredicate = predicateKeysAsWritten
	hold.ScopeDigest = digest

	var propagation recordsmeta.HoldPropagation
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		if _, err := recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL", StoreRef: "store:canonical",
		}); err != nil {
			return err
		}
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		var err error
		propagation, err = recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, hold.HoldID, fixedInstant)
		return err
	})

	if want := readGolden(t, "records_hold_001_propagation_payload.golden"); string(propagation.Outbox.Payload) != want {
		t.Fatalf("propagation payload = %s, want pinned %s", propagation.Outbox.Payload, want)
	}

	var reason string
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		intersections, err := recordsmeta.ListHoldIntersections(ctx, tx, tenant, hold.HoldID)
		if err != nil {
			return err
		}
		if len(intersections) != 1 {
			return fmt.Errorf("%d intersections, want 1", len(intersections))
		}
		reason = intersections[0].MatchedReason
		return nil
	})
	if want := readGolden(t, "records_hold_001_matched_reason.golden"); reason != want {
		t.Fatalf("matched reason = %q, want pinned %q", reason, want)
	}
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Race
// ---------------------------------------------------------------------------

// TestTodo_RECORDS_HOLD_001_Race runs real goroutines: several concurrent
// placements of the same hold racing a concurrent release of a different,
// pre-existing hold over the same copies. It fails on wrong results --
// a duplicated intersection, a lost copy, or a resurrected hold_state --
// rather than relying on -race, which is unavailable on this machine.
func TestTodo_RECORDS_HOLD_001_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	setup := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-race")
	registerRecordsCopySchema(t, db, tenant)

	declaration := newDeclaration(tenant)
	types := []string{"CANONICAL", "EXPORT", "PROVIDER", "BACKUP"}
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, tenant, declaration.DeclarationID, types)
		return nil
	})

	h1 := newHold(tenant)
	h2 := newHold(tenant)
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertLegalHold(ctx, tx, h1); err != nil {
			return err
		}
		if _, err := recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, h1.HoldID, fixedInstant); err != nil {
			return err
		}
		return recordsmeta.InsertLegalHold(ctx, tx, h2)
	})

	const placers = 3
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	errs := make(chan error, placers+1)

	for range placers {
		done.Add(1)
		go func() {
			defer done.Done()
			conn := appConn(t, db)
			start.Wait()
			errs <- inTenantTxRetrying(conn, tenant, func(tx dbport.Tx) error {
				_, err := recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, h2.HoldID, fixedInstant.Add(time.Hour))
				return err
			})
		}()
	}
	done.Add(1)
	go func() {
		defer done.Done()
		conn := appConn(t, db)
		start.Wait()
		errs <- inTenantTxRetrying(conn, tenant, func(tx dbport.Tx) error {
			_, err := recordsmeta.ReleaseHold(ctx, tx, tenant, h1.HoldID, uuid.Nil, "principal:legal", "matter closed", fixedInstant.Add(2*time.Hour))
			return err
		})
	}()
	start.Done()
	done.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent placement/release returned an error: %v", err)
		}
	}

	var h1Loaded, h2Loaded recordsmeta.LegalHold
	var h1Intersections, h2Intersections []recordsmeta.HoldIntersection
	var finalCopies []recordsmeta.CopyLink
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		var err error
		if h1Loaded, err = recordsmeta.LoadLegalHold(ctx, tx, tenant, h1.HoldID); err != nil {
			return err
		}
		if h2Loaded, err = recordsmeta.LoadLegalHold(ctx, tx, tenant, h2.HoldID); err != nil {
			return err
		}
		if h1Intersections, err = recordsmeta.ListHoldIntersections(ctx, tx, tenant, h1.HoldID); err != nil {
			return err
		}
		if h2Intersections, err = recordsmeta.ListHoldIntersections(ctx, tx, tenant, h2.HoldID); err != nil {
			return err
		}
		finalCopies, err = recordsmeta.ListCopies(ctx, tx, tenant, declaration.DeclarationID)
		return err
	})

	if h1Loaded.Status != "RELEASED" {
		t.Errorf("H1 status = %s, want RELEASED", h1Loaded.Status)
	}
	if h2Loaded.Status != "ACTIVE" {
		t.Errorf("H2 status = %s, want ACTIVE", h2Loaded.Status)
	}
	if len(h1Intersections) != len(types) {
		t.Fatalf("H1 has %d intersections, want %d", len(h1Intersections), len(types))
	}
	for _, isec := range h1Intersections {
		if isec.State != "RELEASED" {
			t.Errorf("H1 intersection %s is %s, want RELEASED", isec.IntersectionID, isec.State)
		}
	}
	if len(h2Intersections) != len(types) {
		t.Fatalf("H2 has %d intersections after %d concurrent placements, want exactly %d (a double-apply)", len(h2Intersections), placers, len(types))
	}
	for _, isec := range h2Intersections {
		if isec.State != "ACTIVE" {
			t.Errorf("H2 intersection %s is %s, want ACTIVE", isec.IntersectionID, isec.State)
		}
	}
	if len(finalCopies) != len(types) {
		t.Fatalf("%d copies remain, want %d (a copy was lost)", len(finalCopies), len(types))
	}
	for _, c := range finalCopies {
		if c.HoldState != "HELD" || c.DispositionState != "HELD" {
			t.Errorf("copy %s (%s) is %s/%s after releasing H1, want HELD/HELD: H2 still grips it", c.LinkID, c.CopyType, c.HoldState, c.DispositionState)
		}
	}

	var outboxRows int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE tenant_id=$1 AND effect_identity=$2`, tenant, fmt.Sprintf("records.hold.propagated:%s:%s", h2.HoldID, declaration.DeclarationID)).Scan(&outboxRows); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if outboxRows != 1 {
		t.Fatalf("%d outbox rows for H2's propagation, want exactly 1 despite %d concurrent placements", outboxRows, placers)
	}
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Integration
// ---------------------------------------------------------------------------

func TestTodo_RECORDS_HOLD_001_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	writer := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-integration")
	registerRecordsCopySchema(t, db, tenant)

	declaration := newDeclaration(tenant)
	types := []string{"CANONICAL", "EXPORT", "PROVIDER"}
	hold := newHold(tenant)
	digest, err := recordsmeta.CanonicalScopeDigest(hold.ScopePredicate)
	if err != nil {
		t.Fatalf("canonical scope digest: %v", err)
	}
	hold.ScopeDigest = digest
	notice := recordsmeta.HoldNotice{
		TenantID: tenant, NoticeID: uuid.New(), HoldID: hold.HoldID,
		RecipientRef: "principal:custodian", RecipientRole: "CUSTODIAN",
		IssuedBy: "principal:legal", IssuedAt: fixedInstant, Method: "EMAIL",
		ContentDigest: digestOf("notice"),
	}
	ack := recordsmeta.HoldAcknowledgement{
		TenantID: tenant, AcknowledgementID: uuid.New(), NoticeID: notice.NoticeID, HoldID: hold.HoldID,
		AcknowledgedBy: "principal:custodian", AcknowledgedAt: fixedInstant.Add(time.Hour),
		Method: "EMAIL", ContentDigest: digestOf("ack"),
	}

	inTenantTx(t, writer, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, tenant, declaration.DeclarationID, types)
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		if _, err := recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, hold.HoldID, fixedInstant); err != nil {
			return err
		}
		if err := recordsmeta.InsertHoldNotice(ctx, tx, notice); err != nil {
			return err
		}
		return recordsmeta.InsertHoldAcknowledgement(ctx, tx, ack)
	})

	// Everything above is committed. A fresh connection reconstructs the
	// whole hold/copy/notice/acknowledgement picture from durable state.
	reader := appConn(t, db)
	inTenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		loadedHold, err := recordsmeta.LoadLegalHold(ctx, tx, tenant, hold.HoldID)
		if err != nil {
			return fmt.Errorf("legal_hold: %w", err)
		}
		if loadedHold.ScopeDigest != digest {
			return fmt.Errorf("scope digest did not survive the commit")
		}
		intersections, err := recordsmeta.ListHoldIntersections(ctx, tx, tenant, hold.HoldID)
		if err != nil {
			return fmt.Errorf("hold_intersection: %w", err)
		}
		if len(intersections) != len(types) {
			return fmt.Errorf("%d intersections survived the commit, want %d", len(intersections), len(types))
		}
		copies, err := recordsmeta.ListCopies(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return fmt.Errorf("record_copy_link: %w", err)
		}
		for _, c := range copies {
			if c.HoldState != "HELD" {
				return fmt.Errorf("copy %s is %s after the commit, want HELD", c.LinkID, c.HoldState)
			}
		}
		loadedNotice, err := recordsmeta.LoadHoldNotice(ctx, tx, tenant, notice.NoticeID)
		if err != nil {
			return fmt.Errorf("legal_hold_notice: %w", err)
		}
		if loadedNotice.RecipientRef != notice.RecipientRef {
			return fmt.Errorf("notice recipient did not survive the commit")
		}
		acknowledged, at, err := recordsmeta.NoticeAcknowledged(ctx, tx, tenant, notice.NoticeID)
		if err != nil {
			return fmt.Errorf("acknowledgement lookup: %w", err)
		}
		if !acknowledged || at == nil || !at.Equal(ack.AcknowledgedAt) {
			return fmt.Errorf("notice %s acknowledgement did not survive the commit: acked=%v at=%v", notice.NoticeID, acknowledged, at)
		}
		loadedAck, err := recordsmeta.LoadHoldAcknowledgement(ctx, tx, tenant, ack.AcknowledgementID)
		if err != nil {
			return fmt.Errorf("legal_hold_acknowledgement: %w", err)
		}
		if loadedAck.AcknowledgedBy != ack.AcknowledgedBy {
			return fmt.Errorf("acknowledgement actor did not survive the commit")
		}
		var outboxRows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE tenant_id=$1 AND effect_identity=$2`, tenant, fmt.Sprintf("records.hold.propagated:%s:%s", hold.HoldID, declaration.DeclarationID)).Scan(&outboxRows); err != nil {
			return err
		}
		if outboxRows != 1 {
			return fmt.Errorf("%d outbox rows for the propagation, want 1", outboxRows)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Security
// ---------------------------------------------------------------------------

func TestTodo_RECORDS_HOLD_001_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	alpha := insertTenant(t, db, "hold001-alpha")
	beta := insertTenant(t, db, "hold001-beta")
	registerRecordsCopySchema(t, db, alpha)

	declaration := newDeclaration(alpha)
	hold := newHold(alpha)
	var propagation recordsmeta.HoldPropagation
	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, alpha, declaration.DeclarationID, []string{"CANONICAL", "EXPORT"})
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		var err error
		propagation, err = recordsmeta.PropagateHold(ctx, tx, alpha, declaration.DeclarationID, hold.HoldID, fixedInstant)
		return err
	})
	if len(propagation.Copies) == 0 {
		t.Fatal("setup: no copies propagated")
	}

	t.Run("another tenant sees none of this hold's intersections", func(t *testing.T) {
		err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			list, err := recordsmeta.ListHoldIntersections(ctx, tx, beta, hold.HoldID)
			if err != nil {
				return err
			}
			if len(list) != 0 {
				return fmt.Errorf("beta saw %d of alpha's intersections by naming alpha's hold_id", len(list))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("listing under beta's session failed: %v", err)
		}
	})

	t.Run("another tenant cannot release this hold", func(t *testing.T) {
		err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			_, err := recordsmeta.ReleaseHold(ctx, tx, beta, hold.HoldID, uuid.Nil, "principal:legal", "cross tenant", fixedInstant.Add(time.Hour))
			return err
		})
		if !errors.Is(err, recordsmeta.ErrHoldNotFound) {
			t.Fatalf("beta released alpha's hold: %v", err)
		}
	})

	t.Run("another tenant cannot dispose of this tenant's copy", func(t *testing.T) {
		linkID := propagation.Copies[0].LinkID
		err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			_, err := recordsmeta.DisposeCopy(ctx, tx, beta, declaration.DeclarationID, linkID, fixedInstant.Add(time.Hour))
			return err
		})
		if !errors.Is(err, recordsmeta.ErrCopyNotFound) {
			t.Fatalf("beta disposed of alpha's copy: %v", err)
		}
	})

	t.Run("another tenant cannot issue a notice against this hold", func(t *testing.T) {
		notice := recordsmeta.HoldNotice{
			TenantID: beta, NoticeID: uuid.New(), HoldID: hold.HoldID,
			RecipientRef: "principal:custodian", RecipientRole: "CUSTODIAN",
			IssuedBy: "principal:legal", IssuedAt: fixedInstant, Method: "EMAIL",
			ContentDigest: digestOf("cross-tenant-notice"),
		}
		if err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			return recordsmeta.InsertHoldNotice(ctx, tx, notice)
		}); err == nil {
			t.Fatal("beta issued a notice against alpha's hold")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Recovery
// ---------------------------------------------------------------------------

// TestTodo_RECORDS_HOLD_001_Recovery covers the release path: after release,
// eligibility is recalculated for every affected copy, a copy still covered
// by a second active hold stays held, and a hold whose scope spans several
// declarations can be released for one of them at a time
// (legal_hold.status PARTIALLY_RELEASED) before finally reaching RELEASED.
func TestTodo_RECORDS_HOLD_001_Recovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-recovery")
	registerRecordsCopySchema(t, db, tenant)

	declaration := newDeclaration(tenant)
	types := []string{"CANONICAL", "EXPORT", "SEARCH"}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, tenant, declaration.DeclarationID, types)
		return nil
	})
	h1 := newHold(tenant)
	h2 := newHold(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertLegalHold(ctx, tx, h1); err != nil {
			return err
		}
		if _, err := recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, h1.HoldID, fixedInstant); err != nil {
			return err
		}
		if err := recordsmeta.InsertLegalHold(ctx, tx, h2); err != nil {
			return err
		}
		_, err := recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, h2.HoldID, fixedInstant.Add(time.Hour))
		return err
	})

	var firstRelease recordsmeta.HoldReleaseResult
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		firstRelease, err = recordsmeta.ReleaseHold(ctx, tx, tenant, h1.HoldID, uuid.Nil, "principal:legal", "matter closed", fixedInstant.Add(2*time.Hour))
		return err
	})
	if len(firstRelease.Recalculated) != 0 {
		t.Fatalf("releasing H1 recalculated %d copies, want 0: H2 still grips every one of them", len(firstRelease.Recalculated))
	}
	if len(firstRelease.StillHeld) != len(types) {
		t.Fatalf("releasing H1 reported %d copies still held, want %d", len(firstRelease.StillHeld), len(types))
	}
	if firstRelease.Status != "RELEASED" {
		t.Fatalf("H1 status after release = %s, want RELEASED", firstRelease.Status)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		copies, err := recordsmeta.ListCopies(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return err
		}
		for _, c := range copies {
			if c.HoldState != "HELD" || c.DispositionState != "HELD" {
				return fmt.Errorf("copy %s is %s/%s after releasing H1, want HELD/HELD: H2 is still active", c.LinkID, c.HoldState, c.DispositionState)
			}
		}
		return nil
	})

	var secondRelease recordsmeta.HoldReleaseResult
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		secondRelease, err = recordsmeta.ReleaseHold(ctx, tx, tenant, h2.HoldID, uuid.Nil, "principal:legal", "matter closed", fixedInstant.Add(3*time.Hour))
		return err
	})
	if len(secondRelease.Recalculated) != len(types) {
		t.Fatalf("releasing H2 recalculated %d copies, want %d", len(secondRelease.Recalculated), len(types))
	}
	if len(secondRelease.StillHeld) != 0 {
		t.Fatalf("releasing H2 left %d copies still held, want 0", len(secondRelease.StillHeld))
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		copies, err := recordsmeta.ListCopies(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return err
		}
		for _, c := range copies {
			if c.HoldState != "RELEASED" || c.DispositionState != "PENDING" {
				return fmt.Errorf("copy %s is %s/%s after every hold released, want RELEASED/PENDING (eligibility recalculated)", c.LinkID, c.HoldState, c.DispositionState)
			}
		}
		return nil
	})

	// Idempotent: releasing H2 again finds nothing left to release, and --
	// because that row is legal evidence -- must not rewrite who released it
	// or when. A replayed release carrying a different principal, reason and
	// clock must leave the first release's attribution exactly as it was.
	var firstReleasedBy, firstReleaseReason string
	var firstReleasedAt time.Time
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT released_by, released_at, release_reason FROM legal_hold WHERE tenant_id=$1 AND hold_id=$2`, tenant, h2.HoldID).
			Scan(&firstReleasedBy, &firstReleasedAt, &firstReleaseReason)
	})
	if firstReleasedBy == "" || firstReleasedAt.IsZero() {
		t.Fatalf("H2 release was not attributed: by=%q at=%v", firstReleasedBy, firstReleasedAt)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		result, err := recordsmeta.ReleaseHold(ctx, tx, tenant, h2.HoldID, uuid.Nil, "principal:impostor", "replayed retry", fixedInstant.Add(4*time.Hour))
		if err != nil {
			return err
		}
		if len(result.Released) != 0 || result.Status != "RELEASED" {
			return fmt.Errorf("re-releasing H2 = %+v, want a no-op RELEASED result", result)
		}
		return nil
	})
	var replayedBy, replayedReason string
	var replayedAt time.Time
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT released_by, released_at, release_reason FROM legal_hold WHERE tenant_id=$1 AND hold_id=$2`, tenant, h2.HoldID).
			Scan(&replayedBy, &replayedAt, &replayedReason)
	})
	if replayedBy != firstReleasedBy || replayedReason != firstReleaseReason || !replayedAt.Equal(firstReleasedAt) {
		t.Fatalf("replayed release rewrote the attribution: by %q->%q, reason %q->%q, at %v->%v",
			firstReleasedBy, replayedBy, firstReleaseReason, replayedReason, firstReleasedAt, replayedAt)
	}

	// A hold spanning two declarations can be released for one of them at a
	// time; the hold is PARTIALLY_RELEASED until both are done.
	declarationA := newDeclaration(tenant)
	declarationB := newDeclaration(tenant)
	h3 := newHold(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declarationA); err != nil {
			return err
		}
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declarationB); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, tenant, declarationA.DeclarationID, []string{"CANONICAL"})
		registerCopiesInOrder(t, tx, tenant, declarationB.DeclarationID, []string{"CANONICAL"})
		if err := recordsmeta.InsertLegalHold(ctx, tx, h3); err != nil {
			return err
		}
		if _, err := recordsmeta.PropagateHold(ctx, tx, tenant, declarationA.DeclarationID, h3.HoldID, fixedInstant); err != nil {
			return err
		}
		_, err := recordsmeta.PropagateHold(ctx, tx, tenant, declarationB.DeclarationID, h3.HoldID, fixedInstant)
		return err
	})

	var partial recordsmeta.HoldReleaseResult
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		partial, err = recordsmeta.ReleaseHold(ctx, tx, tenant, h3.HoldID, declarationA.DeclarationID, "principal:legal", "scope A resolved", fixedInstant.Add(time.Hour))
		return err
	})
	if partial.Status != "PARTIALLY_RELEASED" {
		t.Fatalf("H3 status after releasing only declaration A = %s, want PARTIALLY_RELEASED", partial.Status)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		loaded, err := recordsmeta.LoadLegalHold(ctx, tx, tenant, h3.HoldID)
		if err != nil {
			return err
		}
		if loaded.Status != "PARTIALLY_RELEASED" {
			return fmt.Errorf("persisted H3 status = %s, want PARTIALLY_RELEASED", loaded.Status)
		}
		copiesA, err := recordsmeta.ListCopies(ctx, tx, tenant, declarationA.DeclarationID)
		if err != nil {
			return err
		}
		if copiesA[0].HoldState != "RELEASED" {
			return fmt.Errorf("declaration A's copy is %s, want RELEASED", copiesA[0].HoldState)
		}
		copiesB, err := recordsmeta.ListCopies(ctx, tx, tenant, declarationB.DeclarationID)
		if err != nil {
			return err
		}
		if copiesB[0].HoldState != "HELD" {
			return fmt.Errorf("declaration B's copy is %s, want HELD: it is still in H3's scope", copiesB[0].HoldState)
		}
		return nil
	})

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		final, err := recordsmeta.ReleaseHold(ctx, tx, tenant, h3.HoldID, declarationB.DeclarationID, "principal:legal", "scope B resolved", fixedInstant.Add(2*time.Hour))
		if err != nil {
			return err
		}
		if final.Status != "RELEASED" {
			return fmt.Errorf("H3 status after releasing both declarations = %s, want RELEASED", final.Status)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestTodo_RECORDS_HOLD_001_Mutation
// ---------------------------------------------------------------------------

// TestTodo_RECORDS_HOLD_001_Mutation makes the RED scenario concrete: a held
// canonical fact must not survive while a related export/provider/backup
// copy is destroyed. It proves disposition cannot destroy a held copy of
// any copy_type record_copy_link tracks, that the schema refuses the same
// thing independent of the Go layer, and that an unrelated declaration
// keeps disposing normally throughout.
func TestTodo_RECORDS_HOLD_001_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "hold001-mutation")
	registerRecordsCopySchema(t, db, tenant)

	declaration := newDeclaration(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		registerCopiesInOrder(t, tx, tenant, declaration.DeclarationID, recordCopyTypes)
		return nil
	})
	hold := newHold(tenant)
	var propagation recordsmeta.HoldPropagation
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		var err error
		propagation, err = recordsmeta.PropagateHold(ctx, tx, tenant, declaration.DeclarationID, hold.HoldID, fixedInstant)
		return err
	})
	if len(propagation.Copies) != len(recordCopyTypes) {
		t.Fatalf("propagated %d copies, want %d", len(propagation.Copies), len(recordCopyTypes))
	}

	for _, c := range propagation.Copies {
		linkID := c.LinkID
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := recordsmeta.DisposeCopy(ctx, tx, tenant, declaration.DeclarationID, linkID, fixedInstant.Add(time.Hour))
			return err
		})
		if !errors.Is(err, recordsmeta.ErrCopyHeld) {
			t.Fatalf("disposing held copy %s (%s) returned %v, want ErrCopyHeld", linkID, c.CopyType, err)
		}
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		copies, err := recordsmeta.ListCopies(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return err
		}
		for _, c := range copies {
			if c.DispositionState != "HELD" {
				return fmt.Errorf("copy %s (%s) disposition_state is %s; a refused disposal should not have changed it", c.LinkID, c.CopyType, c.DispositionState)
			}
		}
		return nil
	})

	// The schema refuses the same thing directly, without the Go layer.
	if err := db.ExecErr(`UPDATE record_copy_link SET disposition_state='DISPOSED' WHERE tenant_id=$1 AND declaration_id=$2 AND hold_state='HELD'`, tenant, declaration.DeclarationID); err == nil {
		t.Fatal("a raw UPDATE disposed a held copy")
	}
	var dispositionedEvents int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM record_copy_event WHERE tenant_id=$1 AND declaration_id=$2 AND event_type='DISPOSITIONED'`, tenant, declaration.DeclarationID).Scan(&dispositionedEvents); err != nil {
		t.Fatalf("count DISPOSITIONED events: %v", err)
	}
	if dispositionedEvents != 0 {
		t.Fatalf("%d DISPOSITIONED events recorded while the hold was never released", dispositionedEvents)
	}

	// Releasing the hold lets normal disposition resume, for every type.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := recordsmeta.ReleaseHold(ctx, tx, tenant, hold.HoldID, uuid.Nil, "principal:legal", "matter closed", fixedInstant.Add(2*time.Hour))
		return err
	})
	for _, c := range propagation.Copies {
		linkID, copyType := c.LinkID, c.CopyType
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			disposed, err := recordsmeta.DisposeCopy(ctx, tx, tenant, declaration.DeclarationID, linkID, fixedInstant.Add(3*time.Hour))
			if err != nil {
				return err
			}
			if disposed.DispositionState != "DISPOSED" {
				return fmt.Errorf("copy %s (%s) is %s after disposal, want DISPOSED", linkID, copyType, disposed.DispositionState)
			}
			return nil
		})
	}

	// Gap 4: a hold on this declaration never touched an unrelated one,
	// which keeps disposing normally throughout.
	unrelated := newDeclaration(tenant)
	unrelatedDisposition := newDisposition(tenant, unrelated.DeclarationID)
	var unrelatedCopies []recordsmeta.CopyLink
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, unrelated); err != nil {
			return err
		}
		unrelatedCopies = registerCopiesInOrder(t, tx, tenant, unrelated.DeclarationID, []string{"CANONICAL"})
		return recordsmeta.InsertRetentionDisposition(ctx, tx, unrelatedDisposition)
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return recordsmeta.ExecuteDisposition(ctx, tx, tenant, unrelatedDisposition.DispositionID, "CRYPTO_ERASE", digestOf("unrelated-receipt"), fixedInstant.Add(48*time.Hour))
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		loaded, err := recordsmeta.LoadRetentionDisposition(ctx, tx, tenant, unrelatedDisposition.DispositionID)
		if err != nil {
			return err
		}
		if loaded.Status != "EXECUTED" {
			return fmt.Errorf("unrelated declaration's disposition is %s, want EXECUTED: a hold on a different declaration must not freeze it", loaded.Status)
		}
		return nil
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		disposed, err := recordsmeta.DisposeCopy(ctx, tx, tenant, unrelated.DeclarationID, unrelatedCopies[0].LinkID, fixedInstant.Add(48*time.Hour))
		if err != nil {
			return err
		}
		if disposed.DispositionState != "DISPOSED" {
			return fmt.Errorf("unrelated copy disposal = %+v, want DISPOSED", disposed)
		}
		return nil
	})
}
