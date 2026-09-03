package workitem_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
)

// WORK-010's RED clause: a completed approval or task work item stores only
// a digest and a principal id, so the reason, authority ref and typed
// submission fields it decided with are unrecoverable, or stored content can
// be rewritten after completion. These tests build a completed work item
// directly (this package's own store, not steps/approval or steps/task,
// which are proven separately) and drive [workitem.RecordDecision] and
// [workitem.LoadDecision] against it.

// singleCandidateResolutionAssignment wraps [singleCandidateResolution] as a
// full [workitem.Assignment] ready for [workitem.Store.Route].
func singleCandidateResolutionAssignment(principal string) workitem.Assignment {
	return workitem.Assignment{
		Resolution: singleCandidateResolution(principal),
		Trigger:    workitem.TriggerInitialRouting, ChosenOwner: principal,
	}
}

// completeTaskItem drives one TASK work item through create -> route ->
// claim -> in-progress -> completed, the only legal path to COMPLETED
// [status.go] declares, and returns the exact COMPLETED row -- the same
// shape a caller of [workitem.RecordDecision] holds after its own
// [workitem.Store.Complete] call.
func completeTaskItem(
	t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant, instance uuid.UUID, completedBy, digest string,
) workitem.WorkItem {
	t.Helper()
	store := workitem.Store{}
	var completed workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		in, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
		if err != nil {
			return err
		}
		created, err := store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		if err != nil {
			return err
		}
		routed, err := store.Route(ctx, tx, tenant, created.WorkItemID, created.ItemVersion,
			singleCandidateResolutionAssignment(completedBy), meta("workitem.routed"))
		if err != nil {
			return err
		}
		claimed, err := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: routed.WorkItemID, ExpectedVersion: routed.ItemVersion,
			ClaimantPrincipalID: completedBy, ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
			Meta: meta("workitem.claimed"),
		})
		if err != nil {
			return err
		}
		started, err := store.Start(ctx, tx, tenant, claimed.WorkItemID, claimed.ItemVersion, fixedInstant, meta("workitem.started"))
		if err != nil {
			return err
		}
		completed, err = store.Complete(ctx, tx, workitem.CompleteInput{
			TenantID: tenant, WorkItemID: started.WorkItemID, ExpectedVersion: started.ItemVersion,
			CompletedBy: completedBy, CompletedOutputDigest: digest,
			Now: fixedInstant, Meta: meta("workitem.completed"),
		})
		return err
	})
	if completed.Status != workitem.StatusCompleted {
		t.Fatalf("test setup: item status = %s, want COMPLETED", completed.Status)
	}
	return completed
}

func decisionBodyFixture() json.RawMessage {
	return json.RawMessage(`{"reason":"reason.promotion_supported/v1","authority_decision_ref":"authz:decision:work010-fixture"}`)
}

// TestTodo_WORK_010 is the PRIMARY case: a completed work item's full decision
// content is recoverable from storage, verified against the same digest the
// work item itself recorded, and is refused when it disagrees.
func TestTodo_WORK_010(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work010-primary")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)

	digest := "sha256:" + repeatHex("7")
	completed := completeTaskItem(t, ctx, conn, tenant, instance, "principal:test-completer", digest)

	body := decisionBodyFixture()
	var rec workitem.DecisionRecord
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		rec, err = workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
			Item: completed, Kind: workitem.DecisionKindTask, Body: body, BodyDigest: digest,
			DecidedBy: "principal:test-completer", DecidedAt: fixedInstant,
		})
		return err
	})
	if rec.DecisionID == uuid.Nil {
		t.Fatal("RecordDecision minted no decision id")
	}
	if rec.WorkItemID != completed.WorkItemID || rec.WorkflowInstanceID != instance {
		t.Fatalf("decision record identity = %+v, want work item %s / instance %s", rec, completed.WorkItemID, instance)
	}
	if rec.BodyDigest != digest {
		t.Fatalf("decision record digest = %q, want %q", rec.BodyDigest, digest)
	}

	t.Run("GREEN: the full body is recoverable through LoadDecision", func(t *testing.T) {
		var loaded workitem.DecisionRecord
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			loaded, err = workitem.LoadDecision(ctx, tx, tenant, completed.WorkItemID)
			return err
		})
		var wantCompact, gotCompact bytes.Buffer
		if err := json.Compact(&wantCompact, body); err != nil {
			t.Fatalf("compact fixture body: %v", err)
		}
		if err := json.Compact(&gotCompact, loaded.Body); err != nil {
			t.Fatalf("compact loaded body: %v", err)
		}
		if wantCompact.String() != gotCompact.String() {
			t.Fatalf("loaded body = %s, want %s (JSON content must round-trip losslessly; jsonb reformats whitespace only)", gotCompact.String(), wantCompact.String())
		}
		if loaded.DecisionID != rec.DecisionID || loaded.Kind != workitem.DecisionKindTask {
			t.Fatalf("loaded record = %+v, want decision %s / kind TASK", loaded, rec.DecisionID)
		}
	})

	t.Run("RED: a body digest that disagrees with the work item's own is refused", func(t *testing.T) {
		otherItem := completed
		otherItem.WorkItemID = uuid.New() // never persisted; only Validate-adjacent fields matter here
		otherItem.CompletedOutputDigest = "sha256:" + repeatHex("8")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
				Item: otherItem, Kind: workitem.DecisionKindTask, Body: body, BodyDigest: digest,
				DecidedBy: "principal:test-completer", DecidedAt: fixedInstant,
			})
			return err
		})
		if code := workitem.CodeOf(err); code != workitem.CodeInvalidRecord {
			t.Fatalf("mismatched-digest refusal code = %q, want %q (%v)", code, workitem.CodeInvalidRecord, err)
		}
	})

	t.Run("RED: a decision may not be recorded for an item that is not COMPLETED", func(t *testing.T) {
		notCompleted := completed
		notCompleted.Status = workitem.StatusInProgress
		notCompleted.CompletedOutputDigest = ""
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
				Item: notCompleted, Kind: workitem.DecisionKindTask, Body: body, BodyDigest: digest,
				DecidedBy: "principal:test-completer", DecidedAt: fixedInstant,
			})
			return err
		})
		if code := workitem.CodeOf(err); code != workitem.CodeInvalidRecord {
			t.Fatalf("not-completed refusal code = %q, want %q (%v)", code, workitem.CodeInvalidRecord, err)
		}
	})

	t.Run("RED: a decision kind that does not match the item's own kind is refused", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
				Item: completed, Kind: workitem.DecisionKindApproval, Body: body, BodyDigest: digest,
				DecidedBy: "principal:test-completer", DecidedAt: fixedInstant,
			})
			return err
		})
		if code := workitem.CodeOf(err); code != workitem.CodeInvalidRecord {
			t.Fatalf("kind-mismatch refusal code = %q, want %q (%v)", code, workitem.CodeInvalidRecord, err)
		}
	})
}

// TestTodo_WORK_010_Golden pins the exact recoverable shape: the stored body
// is exactly the caller's bytes (modulo JSON whitespace), never re-derived,
// truncated or reformatted into something else, and every identity field
// [DecisionRecord] carries matches the completed work item it was recorded
// for.
func TestTodo_WORK_010_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work010-golden")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)

	digest := "sha256:" + repeatHex("9")
	body := json.RawMessage(`{"a":1,"b":"two","c":[true,false,null],"nested":{"x":"y"}}`)
	completed := completeTaskItem(t, ctx, conn, tenant, instance, "principal:golden", digest)

	var rec, loaded workitem.DecisionRecord
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		rec, err = workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
			Item: completed, Kind: workitem.DecisionKindTask, Body: body, BodyDigest: digest,
			DecidedBy: "principal:golden", DecidedAt: fixedInstant,
		})
		if err != nil {
			return err
		}
		loaded, err = workitem.LoadDecision(ctx, tx, tenant, completed.WorkItemID)
		return err
	})

	var wantParsed, gotParsed map[string]any
	if err := json.Unmarshal(body, &wantParsed); err != nil {
		t.Fatalf("unmarshal fixture body: %v", err)
	}
	if err := json.Unmarshal(loaded.Body, &gotParsed); err != nil {
		t.Fatalf("unmarshal stored body: %v", err)
	}
	if len(wantParsed) != len(gotParsed) {
		t.Fatalf("stored body has %d top-level fields, want %d", len(gotParsed), len(wantParsed))
	}
	for k, v := range wantParsed {
		gv, ok := gotParsed[k]
		if !ok {
			t.Fatalf("stored body is missing field %q", k)
		}
		wantJSON, _ := json.Marshal(v)
		gotJSON, _ := json.Marshal(gv)
		if !bytes.Equal(wantJSON, gotJSON) {
			t.Fatalf("stored body field %q = %s, want %s", k, gotJSON, wantJSON)
		}
	}

	if loaded.TenantID != tenant || loaded.WorkItemID != completed.WorkItemID ||
		loaded.WorkflowInstanceID != instance || loaded.ItemVersion != completed.ItemVersion {
		t.Fatalf("loaded identity = %+v, want tenant=%s item=%s instance=%s version=%d",
			loaded, tenant, completed.WorkItemID, instance, completed.ItemVersion)
	}
	if loaded.Kind != workitem.DecisionKindTask || loaded.BodyDigest != digest {
		t.Fatalf("loaded kind/digest = %s/%s, want TASK/%s", loaded.Kind, loaded.BodyDigest, digest)
	}
	if loaded.DecidedBy != "principal:golden" || !loaded.DecidedAt.Equal(fixedInstant) {
		t.Fatalf("loaded decided-by/at = %s/%s, want principal:golden/%s", loaded.DecidedBy, loaded.DecidedAt, fixedInstant)
	}
	if loaded.DecisionID != rec.DecisionID {
		t.Fatalf("loaded decision id = %s, want %s", loaded.DecisionID, rec.DecisionID)
	}
	if loaded.RecordedAt.Before(fixedInstant) {
		t.Fatalf("recorded_at = %s, want at or after the business instant %s", loaded.RecordedAt, fixedInstant)
	}
}

// TestTodo_WORK_010_Security proves work_item_decision inherits the same
// tenant isolation migration 00017's own tables declare: another tenant
// cannot read a decision, and an unscoped transaction sees nothing.
func TestTodo_WORK_010_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alice := insertTenant(t, db, "work010-security-a")
	bob := insertTenant(t, db, "work010-security-b")
	instance := uuid.New()
	insertInstance(t, db, alice, instance)
	conn := appConn(t, db)

	digest := "sha256:" + repeatHex("a")
	completed := completeTaskItem(t, ctx, conn, alice, instance, "principal:alice-completer", digest)
	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		_, err := workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
			Item: completed, Kind: workitem.DecisionKindTask, Body: decisionBodyFixture(), BodyDigest: digest,
			DecidedBy: "principal:alice-completer", DecidedAt: fixedInstant,
		})
		return err
	})

	t.Run("another tenant cannot read the decision", func(t *testing.T) {
		err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
			_, txErr := workitem.LoadDecision(ctx, tx, alice, completed.WorkItemID)
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
			t.Fatalf("cross-tenant read code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
		}
	})

	t.Run("an unscoped transaction sees nothing", func(t *testing.T) {
		err := inTxErr(conn, func(tx dbport.Tx) error {
			_, txErr := workitem.LoadDecision(ctx, tx, alice, completed.WorkItemID)
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
			t.Fatalf("unscoped read code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
		}
	})

	t.Run("another tenant's instance decisions are invisible", func(t *testing.T) {
		var seen []workitem.DecisionRecord
		inTenantTx(t, conn, bob, func(tx dbport.Tx) error {
			var err error
			seen, err = workitem.LoadDecisionsForInstance(ctx, tx, alice, instance)
			return err
		})
		if len(seen) != 0 {
			t.Fatalf("another tenant read %d decision rows", len(seen))
		}
	})
}

// TestTodo_WORK_010_Mutation kills the mutants that would make "immutable,
// append-once evidence" unfalsifiable: a raw UPDATE or DELETE against
// work_item_decision, and a second RecordDecision attempt for the same
// already-decided item.
func TestTodo_WORK_010_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work010-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)

	digest := "sha256:" + repeatHex("b")
	completed := completeTaskItem(t, ctx, conn, tenant, instance, "principal:mutation", digest)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
			Item: completed, Kind: workitem.DecisionKindTask, Body: decisionBodyFixture(), BodyDigest: digest,
			DecidedBy: "principal:mutation", DecidedAt: fixedInstant,
		})
		return err
	})

	t.Run("a raw UPDATE against work_item_decision is refused by the trigger", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, execErr := tx.Exec(ctx,
				`UPDATE work_item_decision SET decided_by = 'principal:attacker' WHERE tenant_id = $1 AND work_item_id = $2`,
				tenant, completed.WorkItemID)
			return execErr
		})
		if err == nil {
			t.Fatal("a raw UPDATE against work_item_decision succeeded; the append-only trigger must refuse it")
		}
	})

	t.Run("a raw DELETE against work_item_decision is refused by the trigger", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, execErr := tx.Exec(ctx,
				`DELETE FROM work_item_decision WHERE tenant_id = $1 AND work_item_id = $2`,
				tenant, completed.WorkItemID)
			return execErr
		})
		if err == nil {
			t.Fatal("a raw DELETE against work_item_decision succeeded; the append-only trigger must refuse it")
		}
	})

	t.Run("a second RecordDecision for the same item never overwrites the first", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, recErr := workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
				Item: completed, Kind: workitem.DecisionKindTask,
				Body: json.RawMessage(`{"different":"content"}`), BodyDigest: digest,
				DecidedBy: "principal:mutation", DecidedAt: fixedInstant.Add(time.Hour),
			})
			return recErr
		})
		if err == nil {
			t.Fatal("a second RecordDecision for the same work item succeeded; work_item_decision's primary key must refuse it")
		}
		var loaded workitem.DecisionRecord
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			loaded, loadErr = workitem.LoadDecision(ctx, tx, tenant, completed.WorkItemID)
			return loadErr
		})
		var parsed map[string]any
		if jsonErr := json.Unmarshal(loaded.Body, &parsed); jsonErr != nil {
			t.Fatalf("unmarshal stored body: %v", jsonErr)
		}
		if _, ok := parsed["different"]; ok {
			t.Fatal("the second RecordDecision's content leaked into the stored row")
		}
	})
}
