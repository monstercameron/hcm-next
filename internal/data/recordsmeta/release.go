package recordsmeta

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrHoldNotFound is returned when ReleaseHold names a hold this tenant
// cannot see, or that does not exist.
var ErrHoldNotFound = errors.New("recordsmeta: hold not found")

// HoldReleaseResult reports what one ReleaseHold call actually changed.
type HoldReleaseResult struct {
	HoldID uuid.UUID
	// Released is every hold_intersection this call moved from ACTIVE to
	// RELEASED. An already-released scope leaves this empty without error.
	Released []HoldIntersection
	// Recalculated is every tracked copy whose hold_state/disposition_state
	// this call actually changed (no other active hold still gripped it).
	Recalculated []CopyLink
	// StillHeld is every tracked copy this call touched but left HELD,
	// because a second active hold (this one or another) still grips the
	// same record_copy_link row.
	StillHeld []CopyLink
	// Status is legal_hold.status after this call: RELEASED once no
	// ACTIVE intersection remains anywhere under the hold, PARTIALLY_RELEASED
	// while some other declaration in the hold's scope is still gripped.
	Status string
}

// ReleaseHold lifts one hold's grip, either across every declaration it
// intersects (scopeDeclarationID == uuid.Nil) or over one named declaration
// only. A narrower scope is what can leave legal_hold.status at
// PARTIALLY_RELEASED: the rest of the hold's scope is untouched and stays
// ACTIVE.
//
// For every tracked copy an ACTIVE intersection in scope names, ReleaseHold
// recalculates eligibility from scratch inside the same statement that
// releases it: if no ACTIVE intersection -- from this hold or any other --
// still grips the same record_copy_link row, the copy's hold_state moves to
// RELEASED and disposition_state returns to PENDING, and a HOLD_RELEASED
// event is recorded; if another hold still grips it, the copy is left
// exactly as it was. This is RECORDS-HOLD-001's central invariant: releasing
// one hold must never resurrect a copy a second active hold still covers.
//
// ReleaseHold is idempotent. Releasing an already-released scope finds no
// ACTIVE intersections, changes no copy, and still recomputes and returns
// the hold's current status rather than erroring.
func ReleaseHold(ctx context.Context, tx dbport.Tx, tenantID, holdID, scopeDeclarationID uuid.UUID, releasedBy, releaseReason string, at time.Time) (HoldReleaseResult, error) {
	if tenantID == uuid.Nil || holdID == uuid.Nil {
		return HoldReleaseResult{}, ErrNilTenant
	}
	if releasedBy == "" || releaseReason == "" {
		return HoldReleaseResult{}, ErrUnattributedRelease
	}
	if at.IsZero() {
		return HoldReleaseResult{}, fmt.Errorf("%w: release time is required", ErrCopyInvalid)
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return HoldReleaseResult{}, err
	}

	// Lock the hold row so two concurrent releases of the same hold
	// serialize on legal_hold.status instead of racing to overwrite it.
	var currentStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM legal_hold WHERE tenant_id=$1 AND hold_id=$2 FOR UPDATE`, tenantID, holdID).Scan(&currentStatus); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return HoldReleaseResult{}, ErrHoldNotFound
		}
		return HoldReleaseResult{}, fmt.Errorf("recordsmeta: load hold %s: %w", holdID, err)
	}

	args := []any{tenantID, holdID, at.UTC()}
	query := `UPDATE hold_intersection SET state='RELEASED', released_at=$3 WHERE tenant_id=$1 AND hold_id=$2 AND state='ACTIVE'`
	if scopeDeclarationID != uuid.Nil {
		query += ` AND declaration_id=$4`
		args = append(args, scopeDeclarationID)
	}
	query += ` RETURNING intersection_id, declaration_id, copy_id, link_id, matched_reason, matched_at, state`

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return HoldReleaseResult{}, fmt.Errorf("recordsmeta: release intersections for hold %s: %w", holdID, err)
	}
	var released []HoldIntersection
	linkSet := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var i HoldIntersection
		var copyID, linkID *uuid.UUID
		if err := rows.Scan(&i.IntersectionID, &i.DeclarationID, &copyID, &linkID, &i.MatchedReason, &i.MatchedAt, &i.State); err != nil {
			rows.Close()
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: scan released intersection: %w", err)
		}
		i.TenantID, i.HoldID = tenantID, holdID
		i.CopyID, i.LinkID = copyID, linkID
		releasedAt := at.UTC()
		i.ReleasedAt = &releasedAt
		released = append(released, i)
		if linkID != nil {
			linkSet[*linkID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return HoldReleaseResult{}, fmt.Errorf("recordsmeta: release intersections for hold %s: %w", holdID, err)
	}
	rows.Close()

	// Process affected links in a stable, ascending order so a concurrent
	// PropagateHold or ReleaseHold touching the same record_copy_link rows
	// always acquires its locks in the same order and the two serialize
	// instead of deadlocking.
	links := make([]uuid.UUID, 0, len(linkSet))
	for id := range linkSet {
		links = append(links, id)
	}
	sort.Slice(links, func(a, b int) bool { return links[a].String() < links[b].String() })

	result := HoldReleaseResult{HoldID: holdID, Released: released}
	for _, linkID := range links {
		var link CopyLink
		var declarationID uuid.UUID
		var storedOutboxID *uuid.UUID

		// Take the row lock first, as its own statement. The previous version
		// folded the surviving-grip test into correlated EXISTS subqueries
		// inside the UPDATE's SET list. Under READ COMMITTED a statement that
		// blocks on a concurrent writer's row lock re-evaluates through
		// EvalPlanQual, and relying on a SET-list subquery to be recomputed
		// against the post-lock snapshot is exactly the kind of subtlety that
		// produced "copy ... is RELEASED/PENDING after releasing H1, want
		// HELD/HELD: H2 still grips it" under the race detector in CI: a
		// concurrent PropagateHold for a second hold had inserted its ACTIVE
		// intersection, and the release still resurrected the copy.
		//
		// Locking the row, then asking the question, then writing a literal
		// answer makes the ordering explicit rather than implied.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM record_copy_link WHERE tenant_id=$1 AND link_id=$2 FOR UPDATE`, tenantID, linkID); err != nil {
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: lock copy %s: %w", linkID, err)
		}
		var stillGripped bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hold_intersection WHERE tenant_id=$1 AND link_id=$2 AND state='ACTIVE')`, tenantID, linkID).Scan(&stillGripped); err != nil {
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: surviving grips for copy %s: %w", linkID, err)
		}
		err := tx.QueryRow(ctx, `
			UPDATE record_copy_link SET
				hold_state = CASE WHEN $4 THEN hold_state ELSE 'RELEASED' END,
				disposition_state = CASE WHEN $4 THEN disposition_state ELSE 'PENDING' END,
				updated_at = $3
			WHERE tenant_id=$1 AND link_id=$2
			RETURNING declaration_id, copy_type, store_ref, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state, exception_reason`,
			tenantID, linkID, at.UTC(), stillGripped).
			Scan(&declarationID, &link.CopyType, &link.StoreRef, &link.ArtifactRef, &link.LedgerStream, &link.LedgerSequence, &storedOutboxID, &link.HoldState, &link.DispositionState, &link.ExceptionReason)
		if err != nil {
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: recalculate copy %s: %w", linkID, err)
		}
		link.TenantID, link.DeclarationID, link.LinkID = tenantID, declarationID, linkID
		if storedOutboxID != nil {
			link.OutboxID = *storedOutboxID
		}
		if link.HoldState == "HELD" {
			result.StillHeld = append(result.StillHeld, link)
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO record_copy_event (tenant_id, event_id, link_id, declaration_id, event_type, hold_id, detail, recorded_at) VALUES ($1,$2,$3,$4,'HOLD_RELEASED',$5,$6,$7)`,
			tenantID, uuid.New(), linkID, declarationID, holdID, []byte(`{"hold":"RELEASED"}`), at.UTC()); err != nil {
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: record release of copy %s: %w", linkID, err)
		}
		result.Recalculated = append(result.Recalculated, link)
	}

	var existsActive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hold_intersection WHERE tenant_id=$1 AND hold_id=$2 AND state='ACTIVE')`, tenantID, holdID).Scan(&existsActive); err != nil {
		return HoldReleaseResult{}, fmt.Errorf("recordsmeta: check remaining grips for hold %s: %w", holdID, err)
	}
	if existsActive {
		result.Status = "PARTIALLY_RELEASED"
		if _, err := tx.Exec(ctx, `UPDATE legal_hold SET status='PARTIALLY_RELEASED' WHERE tenant_id=$1 AND hold_id=$2 AND status <> 'RELEASED'`, tenantID, holdID); err != nil {
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: mark hold %s partially released: %w", holdID, err)
		}
	} else {
		result.Status = "RELEASED"
		// `AND status <> 'RELEASED'` makes the first release's attribution
		// win. Without it a replayed call -- an at-least-once retry, a
		// duplicate operator action -- silently rewrites released_by,
		// released_at and release_reason on a hold that was already
		// released, moving the recorded release time forward and losing who
		// actually lifted it. For a legal hold that row is evidence, so the
		// attribution is write-once even though status transitions are not.
		if _, err := tx.Exec(ctx, `UPDATE legal_hold SET status='RELEASED', released_by=$3, released_at=$4, release_reason=$5 WHERE tenant_id=$1 AND hold_id=$2 AND status <> 'RELEASED'`,
			tenantID, holdID, releasedBy, at.UTC(), releaseReason); err != nil {
			return HoldReleaseResult{}, fmt.Errorf("recordsmeta: mark hold %s released: %w", holdID, err)
		}
	}
	return result, nil
}

// ListHoldIntersections returns every intersection recorded for one hold,
// in a stable order, regardless of state.
func ListHoldIntersections(ctx context.Context, q dbport.Querier, tenantID, holdID uuid.UUID) ([]HoldIntersection, error) {
	rows, err := q.Query(ctx, `SELECT tenant_id, intersection_id, hold_id, declaration_id, copy_id, link_id, matched_reason, matched_at, released_at, state FROM hold_intersection WHERE tenant_id=$1 AND hold_id=$2 ORDER BY intersection_id`, tenantID, holdID)
	if err != nil {
		return nil, fmt.Errorf("recordsmeta: list intersections for hold %s: %w", holdID, err)
	}
	defer rows.Close()
	var result []HoldIntersection
	for rows.Next() {
		var i HoldIntersection
		var copyID, linkID *uuid.UUID
		var released *time.Time
		if err := rows.Scan(&i.TenantID, &i.IntersectionID, &i.HoldID, &i.DeclarationID, &copyID, &linkID, &i.MatchedReason, &i.MatchedAt, &released, &i.State); err != nil {
			return nil, fmt.Errorf("recordsmeta: scan intersection for hold %s: %w", holdID, err)
		}
		i.CopyID, i.LinkID, i.ReleasedAt = copyID, linkID, released
		result = append(result, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recordsmeta: list intersections for hold %s: %w", holdID, err)
	}
	return result, nil
}
