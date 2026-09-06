// Package hrcasestore persists the immutable, tenant-isolated HR case
// revision stream from migration 00094.
package hrcasestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/hrcase"
)

// DB is the database capability required by Store. The adapter owns a short
// transaction so tenant context is established before any row is touched.
type DB interface{ dbport.Beginner }

// Store implements [hrcase.Store] over migration 00094's append-only table.
type Store struct{ db DB }

const maxInt64 = uint64(1<<63 - 1)

var _ hrcase.Store = (*Store)(nil)

// New returns a PostgreSQL HR case store over db.
func New(db DB) *Store { return &Store{db: db} }

type storedDefinition struct {
	Type           string `json:"type"`
	Version        string `json:"version"`
	Service        string `json:"service"`
	Purpose        string `json:"purpose"`
	Classification string `json:"classification"`
	Retention      string `json:"retention"`
}

type storedParticipant struct {
	Role      string `json:"role"`
	Principal string `json:"principal"`
}

func invalid(detail string) error {
	return &hrcase.StoreError{Code: hrcase.StoreInvalidCode, Detail: detail}
}

func duplicate(detail string) error {
	return &hrcase.StoreError{Code: hrcase.StoreDuplicateCode, Detail: detail}
}

func notFound(detail string) error {
	return &hrcase.StoreError{Code: hrcase.StoreNotFoundCode, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &hrcase.StoreError{Code: hrcase.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a valid uuid", tenantID))
	}
	return tid, nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func normalizeRevision(revision hrcase.CaseRevision) (hrcase.CaseRevision, error) {
	if revision.Digest == "" {
		normalized, err := hrcase.NewCaseRevision(revision)
		if err != nil {
			return hrcase.CaseRevision{}, invalid(err.Error())
		}
		return normalized, nil
	}
	if err := revision.Validate(); err != nil {
		return hrcase.CaseRevision{}, invalid(err.Error())
	}
	if !revision.Verify() {
		return hrcase.CaseRevision{}, invalid("revision digest does not match its content")
	}
	return revision, nil
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("hrcasestore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("hrcasestore: commit transaction: %w", err)
	}
	return nil
}

// AppendRevision appends one immutable revision at eventSequence. The
// revision's Previous field must name the immediately preceding revision and
// the event sequence must be gap-free for that case.
func (s *Store) AppendRevision(ctx context.Context, tenantID string, revision hrcase.CaseRevision, eventSequence uint64) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if eventSequence == 0 || eventSequence > maxInt64 {
		return invalid("event sequence must be a positive int64")
	}
	revision, err = normalizeRevision(revision)
	if err != nil {
		return err
	}
	if revision.Revision == 0 || revision.Revision > maxInt64 || revision.Previous > maxInt64 {
		return invalid("revision counters must fit PostgreSQL bigint")
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		return appendTx(ctx, tx, tid, revision, eventSequence)
	})
}

func appendTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, revision hrcase.CaseRevision, eventSequence uint64) error {
	var existingSequence int64
	err := tx.QueryRow(ctx, `
		SELECT event_sequence
		FROM hr_case_revision
		WHERE tenant_id = $1 AND case_id = $2 AND revision = $3
		LIMIT 1`, tenantID, revision.CaseID, int64(revision.Revision)).Scan(&existingSequence)
	if err == nil {
		return duplicate(fmt.Sprintf("case %s revision %d already exists at event sequence %d", revision.CaseID, revision.Revision, existingSequence))
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("hrcasestore: check duplicate case %s revision %d: %w", revision.CaseID, revision.Revision, err)
	}

	var currentRevision, currentSequence int64
	err = tx.QueryRow(ctx, `
		SELECT revision, event_sequence
		FROM hr_case_revision
		WHERE tenant_id = $1 AND case_id = $2
		ORDER BY event_sequence DESC
		LIMIT 1`, tenantID, revision.CaseID).Scan(&currentRevision, &currentSequence)
	if errors.Is(err, dbport.ErrNoRows) {
		currentRevision, currentSequence = 0, 0
	} else if err != nil {
		return fmt.Errorf("hrcasestore: read current case %s: %w", revision.CaseID, err)
	}
	if currentRevision < 0 || currentSequence < 0 {
		return invalid("stored revision counters are negative")
	}

	if uint64(currentSequence) == maxInt64 || eventSequence != uint64(currentSequence)+1 {
		return stale(uint64(currentSequence)+1, eventSequence, fmt.Sprintf("case %s event sequence is not the next sequence", revision.CaseID))
	}
	if currentRevision == 0 {
		if revision.Revision != 1 || revision.Previous != 0 {
			return stale(0, revision.Previous, fmt.Sprintf("case %s must begin at revision one", revision.CaseID))
		}
	} else if revision.Revision != uint64(currentRevision)+1 || revision.Previous != uint64(currentRevision) {
		return stale(uint64(currentRevision), revision.Previous, fmt.Sprintf("case %s revision does not extend the current revision", revision.CaseID))
	}

	definitionJSON, err := json.Marshal(storedDefinition{
		Type: revision.Definition.Type, Version: revision.Definition.Version, Service: revision.Definition.Service,
		Purpose: revision.Definition.Purpose, Classification: revision.Definition.Classification, Retention: revision.Definition.Retention,
	})
	if err != nil {
		return fmt.Errorf("hrcasestore: encode case definition: %w", err)
	}
	participants := make([]storedParticipant, 0, len(revision.Participants))
	for _, participant := range revision.Participants {
		participants = append(participants, storedParticipant{Role: participant.Role, Principal: participant.Principal})
	}
	participantsJSON, err := json.Marshal(participants)
	if err != nil {
		return fmt.Errorf("hrcasestore: encode case participants: %w", err)
	}

	var previous any
	if revision.Previous != 0 {
		previous = int64(revision.Previous)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO hr_case_revision (
			row_id, tenant_id, case_id, revision, definition,
			requester, subject, purpose, classification, retention,
			participants, state, previous_revision, disposition, digest, event_sequence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16)`,
		uuid.New(), tenantID, revision.CaseID, int64(revision.Revision), definitionJSON,
		revision.Requester, revision.Subject, revision.Purpose, revision.Classification, revision.Retention,
		participantsJSON, string(revision.State), previous, revision.Disposition, revision.Digest, int64(eventSequence))
	if err != nil {
		if isUniqueViolation(err) {
			return duplicate(fmt.Sprintf("case %s event sequence %d", revision.CaseID, eventSequence))
		}
		return fmt.Errorf("hrcasestore: append case %s revision %d: %w", revision.CaseID, revision.Revision, err)
	}
	return nil
}

// LoadRevision returns one immutable revision by event sequence.
func (s *Store) LoadRevision(ctx context.Context, tenantID, caseID string, eventSequence uint64) (hrcase.CaseRevision, error) {
	if err := validateContext(ctx); err != nil {
		return hrcase.CaseRevision{}, err
	}
	tid, err := parseTenant(tenantID)
	if err != nil {
		return hrcase.CaseRevision{}, err
	}
	if strings.TrimSpace(caseID) == "" || eventSequence == 0 || eventSequence > maxInt64 {
		return hrcase.CaseRevision{}, invalid("case id and event sequence are required")
	}
	var out hrcase.CaseRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		out, err = loadTx(ctx, tx, tid, caseID, eventSequence)
		return err
	})
	return out, err
}

// Current returns the row with the highest event sequence for a case.
func (s *Store) Current(ctx context.Context, tenantID, caseID string) (hrcase.CaseRevision, error) {
	if err := validateContext(ctx); err != nil {
		return hrcase.CaseRevision{}, err
	}
	tid, err := parseTenant(tenantID)
	if err != nil {
		return hrcase.CaseRevision{}, err
	}
	if strings.TrimSpace(caseID) == "" {
		return hrcase.CaseRevision{}, invalid("case id is required")
	}
	var out hrcase.CaseRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var eventSequence int64
		if err := tx.QueryRow(ctx, `
			SELECT event_sequence
			FROM hr_case_revision
			WHERE tenant_id = $1 AND case_id = $2
			ORDER BY event_sequence DESC
			LIMIT 1`, tid, caseID).Scan(&eventSequence); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("case %s", caseID))
			}
			return fmt.Errorf("hrcasestore: read current case %s: %w", caseID, err)
		}
		if eventSequence <= 0 {
			return invalid("stored event sequence is not positive")
		}
		var err error
		out, err = loadTx(ctx, tx, tid, caseID, uint64(eventSequence))
		return err
	})
	return out, err
}

// ListRevisions returns the complete immutable case stream in event order.
func (s *Store) ListRevisions(ctx context.Context, tenantID, caseID string) ([]hrcase.CaseRevision, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(caseID) == "" {
		return nil, invalid("case id is required")
	}
	var out []hrcase.CaseRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT event_sequence
			FROM hr_case_revision
			WHERE tenant_id = $1 AND case_id = $2
			ORDER BY event_sequence`, tid, caseID)
		if err != nil {
			return fmt.Errorf("hrcasestore: list case %s: %w", caseID, err)
		}
		defer rows.Close()
		sequences := make([]uint64, 0)
		for rows.Next() {
			var eventSequence int64
			if err := rows.Scan(&eventSequence); err != nil {
				return fmt.Errorf("hrcasestore: scan case %s sequence: %w", caseID, err)
			}
			if eventSequence <= 0 {
				return invalid("stored event sequence is not positive")
			}
			sequences = append(sequences, uint64(eventSequence))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("hrcasestore: list case %s: %w", caseID, err)
		}
		if len(sequences) == 0 {
			return notFound(fmt.Sprintf("case %s", caseID))
		}
		for _, eventSequence := range sequences {
			revision, err := loadTx(ctx, tx, tid, caseID, eventSequence)
			if err != nil {
				return err
			}
			out = append(out, revision)
		}
		return nil
	})
	return out, err
}

func loadTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, caseID string, eventSequence uint64) (hrcase.CaseRevision, error) {
	var (
		rowID, storedTenant                   uuid.UUID
		storedCase, state, requester, subject string
		purpose, classification, retention    string
		disposition, digest                   string
		revision, storedSequence              int64
		previous                              *int64
		definitionJSON, participantsJSON      []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, tenant_id, case_id, revision, definition,
			requester, subject, purpose, classification, retention,
			participants, state, previous_revision, disposition, digest, event_sequence
		FROM hr_case_revision
		WHERE tenant_id = $1 AND case_id = $2 AND event_sequence = $3`,
		tenantID, caseID, int64(eventSequence)).Scan(
		&rowID, &storedTenant, &storedCase, &revision, &definitionJSON,
		&requester, &subject, &purpose, &classification, &retention,
		&participantsJSON, &state, &previous, &disposition, &digest, &storedSequence)
	if errors.Is(err, dbport.ErrNoRows) {
		return hrcase.CaseRevision{}, notFound(fmt.Sprintf("case %s event sequence %d", caseID, eventSequence))
	}
	if err != nil {
		return hrcase.CaseRevision{}, fmt.Errorf("hrcasestore: load case %s event sequence %d: %w", caseID, eventSequence, err)
	}
	if rowID == uuid.Nil || storedTenant != tenantID || storedCase != caseID || storedSequence <= 0 {
		return hrcase.CaseRevision{}, invalid("stored case identity is inconsistent")
	}
	if revision <= 0 {
		return hrcase.CaseRevision{}, invalid("stored revision is not positive")
	}
	var definition storedDefinition
	if err := json.Unmarshal(definitionJSON, &definition); err != nil {
		return hrcase.CaseRevision{}, invalid(fmt.Sprintf("stored definition is invalid: %v", err))
	}
	var participants []storedParticipant
	if err := json.Unmarshal(participantsJSON, &participants); err != nil {
		return hrcase.CaseRevision{}, invalid(fmt.Sprintf("stored participants are invalid: %v", err))
	}
	out := hrcase.CaseRevision{
		CaseID: storedCase, Revision: uint64(revision),
		Definition: hrcase.CaseDefinition{Type: definition.Type, Version: definition.Version, Service: definition.Service, Purpose: definition.Purpose, Classification: definition.Classification, Retention: definition.Retention},
		Requester:  requester, Subject: subject, Purpose: purpose, Classification: classification, Retention: retention,
		State: hrcase.CaseState(state), Disposition: disposition, Digest: digest,
		Participants: make([]hrcase.Participant, 0, len(participants)),
	}
	if previous != nil {
		if *previous < 0 {
			return hrcase.CaseRevision{}, invalid("stored previous revision is negative")
		}
		out.Previous = uint64(*previous)
	}
	for _, participant := range participants {
		out.Participants = append(out.Participants, hrcase.Participant{Role: participant.Role, Principal: participant.Principal})
	}
	if err := out.Validate(); err != nil || !out.Verify() {
		if err != nil {
			return hrcase.CaseRevision{}, invalid(fmt.Sprintf("stored revision failed validation: %v", err))
		}
		return hrcase.CaseRevision{}, invalid("stored revision digest does not match its content")
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
