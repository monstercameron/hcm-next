package truststore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/trust/accessreview"
)

// AccessReviewGrantRecord is the storage projection of one effective grant
// included in an access-review schedule. Capabilities is deliberately JSON:
// the access-review domain owns its capability vocabulary.
type AccessReviewGrantRecord struct {
	TenantID     uuid.UUID
	RowID        uuid.UUID
	GrantID      string
	PrincipalID  string
	Capabilities json.RawMessage
	GrantedAt    time.Time
	ExpiresAt    time.Time
	Revoked      bool
}

// PutAccessReviewGrant stores one current access-review grant. A grant is
// referenced by schedule entries, so the schedule writer verifies that its
// review cadence covers this durable grant before accepting a non-empty
// schedule.
func (s *Store) PutAccessReviewGrant(ctx context.Context, tenantID uuid.UUID, in AccessReviewGrantRecord) error {
	if tenantID == uuid.Nil || (in.TenantID != uuid.Nil && in.TenantID != tenantID) {
		return failure(CodeTenantRequired, "accessreview_grant", in.GrantID, errors.New("tenant is missing or mismatched"))
	}
	if in.GrantID == "" || in.PrincipalID == "" || in.GrantedAt.IsZero() || len(in.Capabilities) == 0 {
		return invalid("accessreview_grant", in.GrantID, "grant id, principal, capabilities and granted_at are required")
	}
	if !json.Valid(in.Capabilities) {
		return invalid("accessreview_grant", in.GrantID, "capabilities must be valid JSON")
	}
	if !in.ExpiresAt.IsZero() && !in.ExpiresAt.After(in.GrantedAt) {
		return invalid("accessreview_grant", in.GrantID, "expires_at must be after granted_at")
	}
	in.TenantID = tenantID
	in.RowID = nonNilUUID(in.RowID)
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO accessreview_grant
				(tenant_id,row_id,grant_id,principal_id,capabilities,granted_at,expires_at,revoked)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			in.TenantID, in.RowID, in.GrantID, in.PrincipalID, in.Capabilities,
			in.GrantedAt.UTC(), nullableTime(in.ExpiresAt), in.Revoked)
		if err != nil {
			return classifyWriteError("accessreview_grant", in.GrantID, err)
		}
		return nil
	})
}

// SaveAccessReviewGrant is an alias for PutAccessReviewGrant.
func (s *Store) SaveAccessReviewGrant(ctx context.Context, tenantID uuid.UUID, in AccessReviewGrantRecord) error {
	return s.PutAccessReviewGrant(ctx, tenantID, in)
}

// SaveDomainGrant stores the normalized grant emitted by the access-review
// coordinator. The caller supplies the capability payload because the domain
// grant intentionally carries only a scope digest.
func (s *Store) SaveDomainGrant(ctx context.Context, tenantID uuid.UUID, grant accessreview.Grant, capabilities any) error {
	payload, err := jsonValue(capabilities)
	if err != nil {
		return failure(CodeInvalid, "accessreview_grant", grant.ID, err)
	}
	return s.PutAccessReviewGrant(ctx, tenantID, AccessReviewGrantRecord{
		TenantID: tenantID, GrantID: grant.ID, PrincipalID: grant.Holder,
		Capabilities: payload, GrantedAt: grant.GrantedAt, ExpiresAt: grant.ExpiresAt,
	})
}

// LoadAccessReviewGrant loads one current grant by its tenant-scoped identity.
func (s *Store) LoadAccessReviewGrant(ctx context.Context, tenantID uuid.UUID, grantID string) (AccessReviewGrantRecord, error) {
	var out AccessReviewGrantRecord
	if tenantID == uuid.Nil {
		return out, failure(CodeTenantRequired, "accessreview_grant", grantID, errors.New("tenant is required"))
	}
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var expiresAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT tenant_id,row_id,grant_id,principal_id,capabilities,granted_at,expires_at,revoked
			FROM accessreview_grant WHERE tenant_id=$1 AND grant_id=$2`, tenantID, grantID).Scan(
			&out.TenantID, &out.RowID, &out.GrantID, &out.PrincipalID, &out.Capabilities,
			&out.GrantedAt, &expiresAt, &out.Revoked)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeNotFound, "accessreview_grant", grantID, errors.New("grant not found"))
		}
		if err != nil {
			return failure(CodeDatabase, "accessreview_grant", grantID, err)
		}
		out.GrantedAt = out.GrantedAt.UTC()
		if expiresAt != nil {
			out.ExpiresAt = expiresAt.UTC()
		}
		return nil
	})
	return out, err
}

// AccessReviewScheduleRecord is the storage projection of one immutable
// schedule snapshot. Entries is the serialized accessreview.Schedule.
type AccessReviewScheduleRecord struct {
	TenantID   uuid.UUID
	RowID      uuid.UUID
	ScheduleID string
	Entries    json.RawMessage
	Digest     string
}

// PutAccessReviewSchedule stores one schedule identity. A schedule is a
// control snapshot: the same identity cannot be replaced without a new
// schedule_id, even if its content differs.
func (s *Store) PutAccessReviewSchedule(ctx context.Context, tenantID uuid.UUID, scheduleID string, schedule accessreview.Schedule) (AccessReviewScheduleRecord, error) {
	if tenantID == uuid.Nil {
		return AccessReviewScheduleRecord{}, failure(CodeTenantRequired, "accessreview_schedule", scheduleID, errors.New("tenant is required"))
	}
	if scheduleID == "" || schedule.Digest == "" {
		return AccessReviewScheduleRecord{}, invalid("accessreview_schedule", scheduleID, "schedule id and digest are required")
	}
	if err := requireDigest("accessreview_schedule", "digest", schedule.Digest); err != nil {
		return AccessReviewScheduleRecord{}, err
	}
	entries, err := jsonValue(schedule.Entries)
	if err != nil {
		return AccessReviewScheduleRecord{}, failure(CodeInvalid, "accessreview_schedule", scheduleID, err)
	}
	out := AccessReviewScheduleRecord{TenantID: tenantID, RowID: uuid.New(), ScheduleID: scheduleID, Entries: entries, Digest: schedule.Digest}
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if err := validateScheduleCoverage(ctx, tx, tenantID, schedule.Entries); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO accessreview_schedule (tenant_id,row_id,schedule_id,entries,digest)
			VALUES ($1,$2,$3,$4,$5)`, tenantID, out.RowID, out.ScheduleID, out.Entries, out.Digest)
		if err != nil {
			return classifyWriteError("accessreview_schedule", scheduleID, err)
		}
		return nil
	})
	if err != nil {
		return AccessReviewScheduleRecord{}, err
	}
	return out, nil
}

// LoadAccessReviewSchedule returns the stored schedule payload and digest.
func (s *Store) LoadAccessReviewSchedule(ctx context.Context, tenantID uuid.UUID, scheduleID string) (AccessReviewScheduleRecord, error) {
	var out AccessReviewScheduleRecord
	if tenantID == uuid.Nil {
		return out, failure(CodeTenantRequired, "accessreview_schedule", scheduleID, errors.New("tenant is required"))
	}
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT tenant_id,row_id,schedule_id,entries,digest
			FROM accessreview_schedule WHERE tenant_id=$1 AND schedule_id=$2`, tenantID, scheduleID).Scan(
			&out.TenantID, &out.RowID, &out.ScheduleID, &out.Entries, &out.Digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeNotFound, "accessreview_schedule", scheduleID, errors.New("schedule not found"))
		}
		if err != nil {
			return failure(CodeDatabase, "accessreview_schedule", scheduleID, err)
		}
		return nil
	})
	return out, err
}

func validateScheduleCoverage(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, entries []accessreview.ScheduleEntry) error {
	for _, entry := range entries {
		if entry.Grant.ID == "" || entry.ReviewDue.IsZero() {
			return invalid("accessreview_schedule", "entries", "every entry needs a grant and review due time")
		}
		var grantedAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT granted_at FROM accessreview_grant
			WHERE tenant_id=$1 AND grant_id=$2`, tenantID, entry.Grant.ID).Scan(&grantedAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeNotFound, "accessreview_grant", entry.Grant.ID, errors.New("schedule entry has no durable grant"))
		}
		if err != nil {
			return failure(CodeDatabase, "accessreview_schedule", entry.Grant.ID, err)
		}
		if entry.ReviewDue.Before(grantedAt) {
			return invalid("accessreview_schedule", entry.Grant.ID, "review due time is outside the grant validity window")
		}
	}
	return nil
}

// AccessReviewReviewRecord is one immutable access-review decision row.
type AccessReviewReviewRecord struct {
	TenantID      uuid.UUID
	RowID         uuid.UUID
	ScheduleID    string
	ReviewerID    string
	Decision      string
	TicketRef     string
	Justification string
	Capabilities  json.RawMessage
	At            time.Time
	EventSequence uint64
}

// AppendAccessReviewRecord records one immutable review decision. The caller
// supplies the event sequence so retries and out-of-order writes are typed
// faults rather than silently reordered evidence.
func (s *Store) AppendAccessReviewRecord(ctx context.Context, in AccessReviewReviewRecord) error {
	if in.TenantID == uuid.Nil || in.RowID == uuid.Nil {
		return failure(CodeTenantRequired, "accessreview_review_record", in.ScheduleID, errors.New("tenant and row id are required"))
	}
	if in.ScheduleID == "" || in.ReviewerID == "" || in.Decision == "" || in.At.IsZero() || in.EventSequence == 0 {
		return invalid("accessreview_review_record", in.ScheduleID, "schedule, reviewer, decision, timestamp and positive sequence are required")
	}
	return s.withTenant(ctx, in.TenantID, func(tx dbport.Tx) error {
		var scheduleExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM accessreview_schedule WHERE tenant_id=$1 AND schedule_id=$2)`,
			in.TenantID, in.ScheduleID).Scan(&scheduleExists); err != nil {
			return failure(CodeDatabase, "accessreview_schedule", in.ScheduleID, err)
		}
		if !scheduleExists {
			return failure(CodeNotFound, "accessreview_schedule", in.ScheduleID, errors.New("review schedule not found"))
		}
		var latest int64
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(event_sequence),0) FROM accessreview_review_record
			WHERE tenant_id=$1 AND schedule_id=$2`, in.TenantID, in.ScheduleID).Scan(&latest)
		if err != nil {
			return failure(CodeDatabase, "accessreview_review_record", in.ScheduleID, err)
		}
		if in.EventSequence <= uint64(latest) {
			return failure(CodeDuplicateEvent, "accessreview_review_record", in.ScheduleID, errors.New("event sequence already recorded"))
		}
		if in.EventSequence != uint64(latest)+1 {
			return failure(CodeVersionConflict, "accessreview_review_record", in.ScheduleID, errors.New("event sequence is not the next event"))
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO accessreview_review_record
				(tenant_id,row_id,schedule_id,reviewer_id,decision,ticket_ref,justification,capabilities,at,event_sequence)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10)`,
			in.TenantID, in.RowID, in.ScheduleID, in.ReviewerID, in.Decision, in.TicketRef,
			in.Justification, nullableJSON(in.Capabilities), in.At.UTC(), int64(in.EventSequence))
		if err != nil {
			return classifyWriteError("accessreview_review_record", in.ScheduleID, err)
		}
		return nil
	})
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// AppendReviewRecord is a concise alias for AppendAccessReviewRecord.
func (s *Store) AppendReviewRecord(ctx context.Context, in AccessReviewReviewRecord) error {
	return s.AppendAccessReviewRecord(ctx, in)
}

// AppendDomainReview stores the review fields emitted by the pure domain
// coordinator, keeping the schedule identity explicit at the storage edge.
func (s *Store) AppendDomainReview(ctx context.Context, tenantID uuid.UUID, scheduleID string, review accessreview.ReviewRecord, eventSequence uint64) error {
	capabilities, err := jsonValue(review.NarrowTo)
	if err != nil {
		return failure(CodeInvalid, "accessreview_review_record", review.GrantID, err)
	}
	return s.AppendAccessReviewRecord(ctx, AccessReviewReviewRecord{
		TenantID: tenantID, RowID: uuid.New(), ScheduleID: scheduleID, ReviewerID: review.Reviewer,
		Decision: string(review.Action), Justification: review.Justification, Capabilities: capabilities,
		At: review.ReviewedAt, EventSequence: eventSequence,
	})
}

// ListAccessReviewRecords returns review evidence in sequence order.
func (s *Store) ListAccessReviewRecords(ctx context.Context, tenantID uuid.UUID, scheduleID string) ([]AccessReviewReviewRecord, error) {
	if tenantID == uuid.Nil {
		return nil, failure(CodeTenantRequired, "accessreview_review_record", scheduleID, errors.New("tenant is required"))
	}
	var out []AccessReviewReviewRecord
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tenant_id,row_id,schedule_id,reviewer_id,decision,COALESCE(ticket_ref,''),
				COALESCE(justification,''),capabilities,at,event_sequence
			FROM accessreview_review_record
			WHERE tenant_id=$1 AND schedule_id=$2 ORDER BY event_sequence`, tenantID, scheduleID)
		if err != nil {
			return failure(CodeDatabase, "accessreview_review_record", scheduleID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var row AccessReviewReviewRecord
			var sequence int64
			if err := rows.Scan(&row.TenantID, &row.RowID, &row.ScheduleID, &row.ReviewerID, &row.Decision,
				&row.TicketRef, &row.Justification, &row.Capabilities, &row.At, &sequence); err != nil {
				return failure(CodeDatabase, "accessreview_review_record", scheduleID, err)
			}
			row.EventSequence = uint64(sequence)
			row.At = row.At.UTC()
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return failure(CodeDatabase, "accessreview_review_record", scheduleID, err)
		}
		return nil
	})
	return out, err
}
