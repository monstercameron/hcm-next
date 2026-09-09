// Package inbox is the tenant- and subject-scoped store for the minimal
// inbox record migration 00033 creates (MSG-005).
//
// MSG-005 is deliberately the MINIMAL CONTRACT: one row per (tenant,
// recipient subject, recipient_message) carrying read/unread state, an
// archived flag, a pinned flag and the instant its state last changed. There
// is no separate channel, thread, preference or provider machinery here --
// that already exists (migration 00031's message_intent / recipient_message)
// or is DESIGN (the full secure-inbox channel MSG-011 names).
//
// # Subject isolation
//
// Row-level security on inbox_record and inbox_state_event only ever knows
// the session's tenant, never which subject the application authenticated
// as. So the isolation MSG-005's RED clause requires -- "a wrong
// principal ... reads the record" must fail -- is this package's own job,
// not the schema's: every statement below names subject_ref in its WHERE
// clause, never only inbox_record_id. A record that exists for a different
// subject is therefore indistinguishable from one that does not exist at
// all: [Store.Load] and [Store.List] simply find nothing, and
// [Store.MarkRead], [Store.Archive] and [Store.Pin] all fail with
// [ErrVersionConflict] because their own UPDATE matched zero rows.
//
// # Compare-and-swap
//
// inbox_record carries a version column. [Store.MarkRead], [Store.Archive]
// and [Store.Pin] are the only ways to change read/archived/pinned state,
// and all three require the caller's expected version; a stale or wrong
// version -- like a wrong subject or a record that does not exist -- reports
// [ErrVersionConflict] without writing anything. Every accepted transition
// also appends one row to inbox_state_event, in the same transaction as the
// state change itself, so the two rows commit or roll back together and the
// log can never record a change the CAS path did not actually make.
//
// # Executor
//
// Executor is the minimal database capability this store needs: exec and
// query, nothing more. A [dbport.Tx] and a [dbport.Conn] both satisfy it.
// Every method takes it explicitly rather than holding a handle, because
// inbox_record and inbox_state_event are row-level-security protected: the
// caller must have scoped its transaction with internal/data/tenancy.WithTenant
// before calling anything here. A store that opened its own connection could
// not guarantee that.
package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The sentinels this package classifies failures with.
var (
	// ErrInvalid reports a row or argument that is not internally
	// consistent. It is returned before any statement runs.
	ErrInvalid = errors.New("inbox: invalid row")

	// ErrDuplicate reports a second inbox record for a (subject,
	// recipient_message) pair that already has one.
	ErrDuplicate = errors.New("inbox: duplicate row")

	// ErrNotFound reports a record that does not exist for the given
	// tenant and subject. It is also what a wrong subject observes for a
	// record that does exist under a different subject: the two cases are
	// indistinguishable by construction (see the package doc).
	ErrNotFound = errors.New("inbox: not found")

	// ErrVersionConflict reports a compare-and-swap whose expected
	// version, subject or record id did not match a live row. Nothing was
	// written. As with ErrNotFound, a wrong subject and a stale version
	// report identically on purpose.
	ErrVersionConflict = errors.New("inbox: version conflict")
)

// Read states inbox_record.read_state may hold.
const (
	Unread = "UNREAD"
	Read   = "READ"
)

// Event types inbox_state_event.event_type may hold. Only the transitions
// this package's Store actually performs are named; the migration's CHECK
// constraint also allows MARKED_UNREAD for a future reversible read state,
// which this package does not yet produce.
const (
	EventMarkedRead = "MARKED_READ"
	EventArchived   = "ARCHIVED"
	EventUnarchived = "UNARCHIVED"
	EventPinned     = "PINNED"
	EventUnpinned   = "UNPINNED"
)

// Executor is the minimal database capability the inbox store needs. See the
// package doc for why the caller, not this package, is responsible for
// tenant scoping.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

func isNoRows(err error) bool { return errors.Is(err, dbport.ErrNoRows) }

func invalid(field, detail string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, field, detail)
}

// Record is one subject's inbox entry for one recipient_message.
type Record struct {
	TenantID           uuid.UUID
	InboxRecordID      uuid.UUID
	SubjectRef         string
	RecipientMessageID uuid.UUID

	ReadState      string
	Archived       bool
	Pinned         bool
	StateChangedAt time.Time
	Version        uint64

	CreatedAt time.Time
}

// Store writes and advances inbox_record and its inbox_state_event log.
type Store struct{}

// Create inserts one inbox record for a subject's copy of a recipient
// message, at version 1. A repeated (subject, recipient_message) pair is
// [ErrDuplicate]: MSG-005 declares exactly one row per that pair, never a
// second view of the same message for the same subject.
func (s Store) Create(ctx context.Context, ex Executor, in Record) error {
	if in.TenantID == uuid.Nil || in.InboxRecordID == uuid.Nil || in.RecipientMessageID == uuid.Nil {
		return invalid("inbox_record_id", "tenant, record and recipient message id are required")
	}
	if in.SubjectRef == "" {
		return invalid("subject_ref", "an inbox record always names its owning subject")
	}
	if in.ReadState == "" {
		in.ReadState = Unread
	}
	if in.ReadState != Unread && in.ReadState != Read {
		return invalid("read_state", "state is not a declared inbox read state")
	}
	if in.CreatedAt.IsZero() {
		return invalid("created_at", "timestamp is unset")
	}
	changedAt := in.StateChangedAt
	if changedAt.IsZero() {
		changedAt = in.CreatedAt
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO inbox_record (
			tenant_id, inbox_record_id, subject_ref, recipient_message_id,
			read_state, archived, pinned, state_changed_at, version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.InboxRecordID, in.SubjectRef, in.RecipientMessageID,
		in.ReadState, in.Archived, in.Pinned, changedAt.UTC(), in.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("inbox: create record %s: %w", in.InboxRecordID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: inbox_record subject %s message %s", ErrDuplicate, in.SubjectRef, in.RecipientMessageID)
	}
	return nil
}

// Load returns one subject's inbox record. See the package doc: a record
// belonging to another subject reports [ErrNotFound], identically to a
// record that does not exist.
func (s Store) Load(ctx context.Context, ex Executor, tenantID uuid.UUID, subjectRef string, recordID uuid.UUID) (Record, error) {
	if tenantID == uuid.Nil || recordID == uuid.Nil {
		return Record{}, invalid("inbox_record_id", "tenant and record id are required")
	}
	if subjectRef == "" {
		return Record{}, invalid("subject_ref", "a load always names the subject it reads for")
	}
	var (
		out     Record
		version int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, inbox_record_id, subject_ref, recipient_message_id,
			read_state, archived, pinned, state_changed_at, version, created_at
		FROM inbox_record
		WHERE tenant_id = $1 AND subject_ref = $2 AND inbox_record_id = $3`,
		tenantID, subjectRef, recordID).Scan(
		&out.TenantID, &out.InboxRecordID, &out.SubjectRef, &out.RecipientMessageID,
		&out.ReadState, &out.Archived, &out.Pinned, &out.StateChangedAt, &version, &out.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return Record{}, fmt.Errorf("%w: inbox_record %s", ErrNotFound, recordID)
		}
		return Record{}, fmt.Errorf("inbox: load record %s: %w", recordID, err)
	}
	out.Version = uint64(version)
	out.StateChangedAt = out.StateChangedAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	return out, nil
}

// List returns every inbox record belonging to one subject, most recently
// changed first. It never returns another subject's rows: the query itself
// names the subject in its WHERE clause, not merely the tenant RLS enforces.
func (s Store) List(ctx context.Context, ex Executor, tenantID uuid.UUID, subjectRef string) ([]Record, error) {
	if tenantID == uuid.Nil {
		return nil, invalid("tenant_id", "tenant is required")
	}
	if subjectRef == "" {
		return nil, invalid("subject_ref", "a list always names the subject it reads for")
	}
	rows, err := ex.Query(ctx, `
		SELECT tenant_id, inbox_record_id, subject_ref, recipient_message_id,
			read_state, archived, pinned, state_changed_at, version, created_at
		FROM inbox_record
		WHERE tenant_id = $1 AND subject_ref = $2
		ORDER BY state_changed_at DESC, inbox_record_id`,
		tenantID, subjectRef)
	if err != nil {
		return nil, fmt.Errorf("inbox: list records for %s: %w", subjectRef, err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var (
			rec     Record
			version int64
		)
		if err := rows.Scan(&rec.TenantID, &rec.InboxRecordID, &rec.SubjectRef, &rec.RecipientMessageID,
			&rec.ReadState, &rec.Archived, &rec.Pinned, &rec.StateChangedAt, &version, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("inbox: scan record for %s: %w", subjectRef, err)
		}
		rec.Version = uint64(version)
		rec.StateChangedAt = rec.StateChangedAt.UTC()
		rec.CreatedAt = rec.CreatedAt.UTC()
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inbox: list records for %s: %w", subjectRef, err)
	}
	return out, nil
}

// checkTransitionArgs validates the arguments every CAS transition shares.
func checkTransitionArgs(tenantID, recordID uuid.UUID, subjectRef string, expectedVersion uint64, at time.Time) error {
	if tenantID == uuid.Nil || recordID == uuid.Nil {
		return invalid("inbox_record_id", "tenant and record id are required")
	}
	if subjectRef == "" {
		return invalid("subject_ref", "a transition always names the subject making it")
	}
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if at.IsZero() {
		return invalid("occurred_at", "timestamp is unset")
	}
	return nil
}

// applyEvent appends one row to inbox_state_event after a CAS UPDATE has
// already returned successfully in the same transaction, so the state
// change and its ledger entry commit or roll back together.
func (s Store) applyEvent(ctx context.Context, ex Executor, tenantID, recordID uuid.UUID, subjectRef, eventType string, previousVersion, newVersion int64, at time.Time) error {
	_, err := ex.Exec(ctx, `
		INSERT INTO inbox_state_event (
			tenant_id, event_id, inbox_record_id, subject_ref, event_type,
			previous_version, new_version, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		tenantID, uuid.New(), recordID, subjectRef, eventType, previousVersion, newVersion, at.UTC())
	if err != nil {
		return fmt.Errorf("inbox: record %s transition event %s: %w", recordID, eventType, err)
	}
	return nil
}

// MarkRead moves a subject's own record to READ under compare-and-swap. A
// wrong subject, a wrong expected version or a record that does not exist
// all fail identically with [ErrVersionConflict]: the UPDATE's WHERE clause
// names tenant, subject, record and version together, so none of those
// mismatches touches any row.
func (s Store) MarkRead(ctx context.Context, ex Executor, tenantID uuid.UUID, subjectRef string, recordID uuid.UUID, expectedVersion uint64, at time.Time) error {
	if err := checkTransitionArgs(tenantID, recordID, subjectRef, expectedVersion, at); err != nil {
		return err
	}
	row := ex.QueryRow(ctx, `
		UPDATE inbox_record
		SET read_state = 'READ', state_changed_at = $5, version = version + 1
		WHERE tenant_id = $1 AND subject_ref = $2 AND inbox_record_id = $3 AND version = $4
		RETURNING version - 1, version`,
		tenantID, subjectRef, recordID, int64(expectedVersion), at.UTC())
	var previous, next int64
	if err := row.Scan(&previous, &next); err != nil {
		if isNoRows(err) {
			return fmt.Errorf("%w: inbox_record %s expected version %d", ErrVersionConflict, recordID, expectedVersion)
		}
		return fmt.Errorf("inbox: mark read %s: %w", recordID, err)
	}
	return s.applyEvent(ctx, ex, tenantID, recordID, subjectRef, EventMarkedRead, previous, next, at)
}

// Archive sets a subject's own record's archived flag under compare-and-swap,
// recording ARCHIVED or UNARCHIVED depending on the target value. The same
// wrong-subject / wrong-version / not-found cases as [Store.MarkRead] all
// report [ErrVersionConflict].
func (s Store) Archive(ctx context.Context, ex Executor, tenantID uuid.UUID, subjectRef string, recordID uuid.UUID, archived bool, expectedVersion uint64, at time.Time) error {
	if err := checkTransitionArgs(tenantID, recordID, subjectRef, expectedVersion, at); err != nil {
		return err
	}
	row := ex.QueryRow(ctx, `
		UPDATE inbox_record
		SET archived = $5, state_changed_at = $6, version = version + 1
		WHERE tenant_id = $1 AND subject_ref = $2 AND inbox_record_id = $3 AND version = $4
		RETURNING version - 1, version`,
		tenantID, subjectRef, recordID, int64(expectedVersion), archived, at.UTC())
	var previous, next int64
	if err := row.Scan(&previous, &next); err != nil {
		if isNoRows(err) {
			return fmt.Errorf("%w: inbox_record %s expected version %d", ErrVersionConflict, recordID, expectedVersion)
		}
		return fmt.Errorf("inbox: archive %s: %w", recordID, err)
	}
	eventType := EventUnarchived
	if archived {
		eventType = EventArchived
	}
	return s.applyEvent(ctx, ex, tenantID, recordID, subjectRef, eventType, previous, next, at)
}

// Pin sets a subject's own record's pinned flag under compare-and-swap,
// recording PINNED or UNPINNED depending on the target value. The same
// wrong-subject / wrong-version / not-found cases as [Store.MarkRead] all
// report [ErrVersionConflict].
func (s Store) Pin(ctx context.Context, ex Executor, tenantID uuid.UUID, subjectRef string, recordID uuid.UUID, pinned bool, expectedVersion uint64, at time.Time) error {
	if err := checkTransitionArgs(tenantID, recordID, subjectRef, expectedVersion, at); err != nil {
		return err
	}
	row := ex.QueryRow(ctx, `
		UPDATE inbox_record
		SET pinned = $5, state_changed_at = $6, version = version + 1
		WHERE tenant_id = $1 AND subject_ref = $2 AND inbox_record_id = $3 AND version = $4
		RETURNING version - 1, version`,
		tenantID, subjectRef, recordID, int64(expectedVersion), pinned, at.UTC())
	var previous, next int64
	if err := row.Scan(&previous, &next); err != nil {
		if isNoRows(err) {
			return fmt.Errorf("%w: inbox_record %s expected version %d", ErrVersionConflict, recordID, expectedVersion)
		}
		return fmt.Errorf("inbox: pin %s: %w", recordID, err)
	}
	eventType := EventUnpinned
	if pinned {
		eventType = EventPinned
	}
	return s.applyEvent(ctx, ex, tenantID, recordID, subjectRef, eventType, previous, next, at)
}
