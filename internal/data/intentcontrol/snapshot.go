package intentcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Snapshot purposes, matching the schema's closed vocabulary.
const (
	PurposePreflight    = "PREFLIGHT"
	PurposeSimulation   = "SIMULATION"
	PurposeRevalidation = "REVALIDATION"
	PurposeRepair       = "REPAIR"
)

// Simulation statuses.
const (
	SimulationReady        = "READY"
	SimulationBlocked      = "BLOCKED"
	SimulationRejected     = "REJECTED"
	SimulationInconclusive = "INCONCLUSIVE"
)

// requireJSONObject rejects a body that is not a JSON object. The schema's
// jsonb_typeof check says the same thing; saying it here too means a caller
// that passed a bare array or a naked string learns which field was wrong
// instead of reading a constraint name out of a driver error.
func requireJSONObject(field string, body json.RawMessage) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return invalid(field, "body is absent")
	}
	if !json.Valid(trimmed) {
		return invalid(field, "body is not valid JSON")
	}
	if trimmed[0] != '{' {
		return invalid(field, "body is not a JSON object")
	}
	return nil
}

// InputSnapshot is one intent_input_snapshot row: the exact baseline a
// preflight, simulation, revalidation or repair read, kept so that a later
// disagreement about what the data said is answered from the record.
type InputSnapshot struct {
	TenantID   uuid.UUID
	SnapshotID uuid.UUID
	IntentID   uuid.UUID

	Purpose  string
	Sequence uint64

	ObservedAt time.Time
	Digest     string

	SchemaRef string
	Body      json.RawMessage

	RecordedAt time.Time
}

// Validate rejects a snapshot that could not be stored.
func (s InputSnapshot) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", s.TenantID},
		{"snapshot_id", s.SnapshotID},
		{"intent_id", s.IntentID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	switch s.Purpose {
	case PurposePreflight, PurposeSimulation, PurposeRevalidation, PurposeRepair:
	default:
		return invalid("snapshot_purpose", "purpose is not a declared snapshot purpose")
	}
	if s.Sequence == 0 {
		return invalid("snapshot_sequence", "sequence starts at 1")
	}
	if err := requireInstant("observed_at", s.ObservedAt); err != nil {
		return err
	}
	if err := requireDigest("snapshot_digest", s.Digest); err != nil {
		return err
	}
	if err := requireText("schema_ref", s.SchemaRef); err != nil {
		return err
	}
	return requireJSONObject("snapshot_body", s.Body)
}

// SnapshotStore reads and writes intent_input_snapshot.
type SnapshotStore struct{}

// Record inserts one immutable input snapshot.
//
// A repeated (intent, purpose, sequence) is [ErrDuplicate], not an overwrite:
// re-reading a baseline produces the next sequence, and the previous answer
// stays exactly as it was recorded.
func (s SnapshotStore) Record(ctx context.Context, ex Executor, in InputSnapshot) (InputSnapshot, error) {
	if err := in.Validate(); err != nil {
		return InputSnapshot{}, err
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO intent_input_snapshot (
			tenant_id, snapshot_id, intent_id,
			snapshot_purpose, snapshot_sequence,
			observed_at, snapshot_digest, schema_ref, snapshot_body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT DO NOTHING
		RETURNING `+snapshotColumns,
		in.TenantID, in.SnapshotID, in.IntentID,
		in.Purpose, int64(in.Sequence),
		in.ObservedAt.UTC(), in.Digest, in.SchemaRef, string(in.Body))

	stored, err := scanSnapshot(row)
	if err != nil {
		if isNoRows(err) {
			return InputSnapshot{}, fmt.Errorf("%w: intent_input_snapshot %s/%s/%d",
				ErrDuplicate, in.IntentID, in.Purpose, in.Sequence)
		}
		return InputSnapshot{}, fmt.Errorf("intentcontrol: record input snapshot %s: %w", in.SnapshotID, err)
	}
	return stored, nil
}

// Load returns one snapshot by id.
func (s SnapshotStore) Load(ctx context.Context, ex Executor, tenantID, snapshotID uuid.UUID) (InputSnapshot, error) {
	row := ex.QueryRow(ctx, `
		SELECT `+snapshotColumns+`
		FROM intent_input_snapshot
		WHERE tenant_id = $1 AND snapshot_id = $2`, tenantID, snapshotID)
	stored, err := scanSnapshot(row)
	if err != nil {
		if isNoRows(err) {
			return InputSnapshot{}, fmt.Errorf("%w: intent_input_snapshot %s", ErrNotFound, snapshotID)
		}
		return InputSnapshot{}, fmt.Errorf("intentcontrol: load input snapshot %s: %w", snapshotID, err)
	}
	return stored, nil
}

const snapshotColumns = `tenant_id, snapshot_id, intent_id,
	snapshot_purpose, snapshot_sequence,
	observed_at, snapshot_digest, schema_ref, snapshot_body::text, recorded_at`

func scanSnapshot(src scanner) (InputSnapshot, error) {
	var (
		out      InputSnapshot
		sequence int64
		body     string
	)
	if err := src.Scan(
		&out.TenantID, &out.SnapshotID, &out.IntentID,
		&out.Purpose, &sequence,
		&out.ObservedAt, &out.Digest, &out.SchemaRef, &body, &out.RecordedAt,
	); err != nil {
		return InputSnapshot{}, err
	}
	out.Sequence = uint64(sequence)
	out.Body = json.RawMessage(body)
	out.ObservedAt = out.ObservedAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

// SimulationResult is one intent_simulation_result row: what a simulation of a
// specific proposal revision produced, bound to the input snapshot it consumed.
type SimulationResult struct {
	TenantID     uuid.UUID
	SimulationID uuid.UUID
	IntentID     uuid.UUID
	Revision     uint64
	Sequence     uint64

	InputSnapshotID uuid.UUID

	Status         string
	ResultDigest   string
	ProposalDigest string
	ControlDigest  string

	SchemaRef string
	Body      json.RawMessage

	SimulatedAt time.Time
	RecordedAt  time.Time
}

// Validate rejects a simulation result that could not be stored.
func (r SimulationResult) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", r.TenantID},
		{"simulation_id", r.SimulationID},
		{"intent_id", r.IntentID},
		{"input_snapshot_id", r.InputSnapshotID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	if r.Revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	if r.Sequence == 0 {
		return invalid("simulation_sequence", "sequence starts at 1")
	}
	switch r.Status {
	case SimulationReady, SimulationBlocked, SimulationRejected, SimulationInconclusive:
	default:
		return invalid("simulation_status", "status is not a declared simulation status")
	}
	for _, req := range []struct{ field, value string }{
		{"result_digest", r.ResultDigest},
		{"proposal_digest", r.ProposalDigest},
		{"control_digest", r.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireText("schema_ref", r.SchemaRef); err != nil {
		return err
	}
	if err := requireInstant("simulated_at", r.SimulatedAt); err != nil {
		return err
	}
	return requireJSONObject("result_body", r.Body)
}

// SimulationStore reads and writes intent_simulation_result.
type SimulationStore struct{}

// RecordResult inserts one immutable simulation result.
//
// Before inserting it re-reads the declared input snapshot and refuses a result
// whose snapshot belongs to a different intent ([ErrSnapshotMismatch]). The
// foreign key alone cannot catch that: it proves the snapshot exists in this
// tenant, not that it is this intent's snapshot, and a result explained by
// someone else's baseline explains nothing. The read and the insert run in the
// caller's transaction, so the snapshot cannot change between them.
func (s SimulationStore) RecordResult(ctx context.Context, ex Executor, in SimulationResult) (SimulationResult, error) {
	if err := in.Validate(); err != nil {
		return SimulationResult{}, err
	}
	snapshot, err := SnapshotStore{}.Load(ctx, ex, in.TenantID, in.InputSnapshotID)
	if err != nil {
		return SimulationResult{}, err
	}
	if snapshot.IntentID != in.IntentID {
		return SimulationResult{}, fmt.Errorf("%w: snapshot %s belongs to intent %s, not %s",
			ErrSnapshotMismatch, in.InputSnapshotID, snapshot.IntentID, in.IntentID)
	}

	row := ex.QueryRow(ctx, `
		INSERT INTO intent_simulation_result (
			tenant_id, simulation_id, intent_id, revision, simulation_sequence,
			input_snapshot_id, simulation_status,
			result_digest, proposal_digest, control_digest,
			schema_ref, result_body, simulated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13)
		ON CONFLICT DO NOTHING
		RETURNING `+simulationColumns,
		in.TenantID, in.SimulationID, in.IntentID, int64(in.Revision), int64(in.Sequence),
		in.InputSnapshotID, in.Status,
		in.ResultDigest, in.ProposalDigest, in.ControlDigest,
		in.SchemaRef, string(in.Body), in.SimulatedAt.UTC())

	stored, scanErr := scanSimulation(row)
	if scanErr != nil {
		if isNoRows(scanErr) {
			return SimulationResult{}, fmt.Errorf("%w: intent_simulation_result %s/%d/%d",
				ErrDuplicate, in.IntentID, in.Revision, in.Sequence)
		}
		return SimulationResult{}, fmt.Errorf("intentcontrol: record simulation %s: %w", in.SimulationID, scanErr)
	}
	return stored, nil
}

// Load returns one simulation result by id.
func (s SimulationStore) Load(ctx context.Context, ex Executor, tenantID, simulationID uuid.UUID) (SimulationResult, error) {
	row := ex.QueryRow(ctx, `
		SELECT `+simulationColumns+`
		FROM intent_simulation_result
		WHERE tenant_id = $1 AND simulation_id = $2`, tenantID, simulationID)
	stored, err := scanSimulation(row)
	if err != nil {
		if isNoRows(err) {
			return SimulationResult{}, fmt.Errorf("%w: intent_simulation_result %s", ErrNotFound, simulationID)
		}
		return SimulationResult{}, fmt.Errorf("intentcontrol: load simulation %s: %w", simulationID, err)
	}
	return stored, nil
}

const simulationColumns = `tenant_id, simulation_id, intent_id, revision, simulation_sequence,
	input_snapshot_id, simulation_status,
	result_digest, proposal_digest, control_digest,
	schema_ref, result_body::text, simulated_at, recorded_at`

func scanSimulation(src scanner) (SimulationResult, error) {
	var (
		out      SimulationResult
		revision int64
		sequence int64
		body     string
	)
	if err := src.Scan(
		&out.TenantID, &out.SimulationID, &out.IntentID, &revision, &sequence,
		&out.InputSnapshotID, &out.Status,
		&out.ResultDigest, &out.ProposalDigest, &out.ControlDigest,
		&out.SchemaRef, &body, &out.SimulatedAt, &out.RecordedAt,
	); err != nil {
		return SimulationResult{}, err
	}
	out.Revision = uint64(revision)
	out.Sequence = uint64(sequence)
	out.Body = json.RawMessage(body)
	out.SimulatedAt = out.SimulatedAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}
