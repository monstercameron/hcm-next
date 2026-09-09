package critical

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

// Diff names one disagreement Verify found between the ledger's own record
// and what intent_instance or proposal_revision actually stores.
type Diff struct {
	Table  string
	Key    string
	Detail string
}

// VerifyResult is everything Verify recomputed and found. A clean projection
// has a zero-length Diffs.
type VerifyResult struct {
	Tenant     uuid.UUID
	StreamKey  string
	Checkpoint projection.Checkpoint
	Diffs      []Diff
}

// OK reports whether Verify found no disagreement.
func (r VerifyResult) OK() bool { return len(r.Diffs) == 0 }

// Verify recomputes intent_instance and proposal_revision for one stream by
// replaying every event on it through mapper - the same decoding [Apply]
// uses - and diffs the recomputed rows against what is actually stored,
// plus the checkpoint's own watermark against the last event it recomputed
// (DATA-006: "Verify... recomputes the projection from the ledger and
// diffs"). It performs no writes; q may be a bare connection, a pool or an
// open transaction.
func Verify(ctx context.Context, q ledger.Querier, reader *ledger.Reader, mapper Mapper, tenant uuid.UUID, streamKey string) (VerifyResult, error) {
	events, err := reader.ReadStream(ctx, q, tenant, streamKey)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("critical: verify: read stream %s: %w", streamKey, err)
	}

	var (
		expectedIntent    *IntentInstanceRow
		expectedRevisions = map[int64]ProposalRevisionRow{}
		lastMappedSeq     int64
	)
	for _, ev := range events {
		if row, ok, mapErr := mapper.MapIntentInstance(ev.SchemaRef, ev.Payload); mapErr != nil {
			return VerifyResult{}, fmt.Errorf("critical: verify: map sequence %d: %w", ev.Sequence, mapErr)
		} else if ok {
			row.Tenant = tenant
			expectedIntent = &row
			lastMappedSeq = ev.Sequence
			continue
		}
		if row, ok, mapErr := mapper.MapProposalRevision(ev.SchemaRef, ev.Payload); mapErr != nil {
			return VerifyResult{}, fmt.Errorf("critical: verify: map sequence %d: %w", ev.Sequence, mapErr)
		} else if ok {
			row.Tenant = tenant
			expectedRevisions[row.Revision] = row
			lastMappedSeq = ev.Sequence
			continue
		}
	}

	cp, err := projection.Read(ctx, q, tenant, ProjectionName, streamKey)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("critical: verify: read checkpoint: %w", err)
	}

	var diffs []Diff
	if cp.LastAppliedSequence != lastMappedSeq {
		diffs = append(diffs, Diff{
			Table: "projection_checkpoint",
			Key:   streamKey,
			Detail: fmt.Sprintf("checkpoint is at sequence %d, the ledger's last recognized event is at %d",
				cp.LastAppliedSequence, lastMappedSeq),
		})
	}

	if expectedIntent != nil {
		actual, found, err := readIntentInstance(ctx, q, tenant, expectedIntent.IntentID)
		if err != nil {
			return VerifyResult{}, err
		}
		if !found {
			diffs = append(diffs, Diff{Table: "intent_instance", Key: expectedIntent.IntentID.String(), Detail: "the ledger has this intent; intent_instance has no row"})
		} else {
			diffs = append(diffs, diffIntentInstance(*expectedIntent, actual)...)
		}
	}

	for revision, expected := range expectedRevisions {
		actual, found, err := readProposalRevision(ctx, q, tenant, expected.IntentID, revision)
		if err != nil {
			return VerifyResult{}, err
		}
		key := fmt.Sprintf("%s/%d", expected.IntentID, revision)
		if !found {
			diffs = append(diffs, Diff{Table: "proposal_revision", Key: key, Detail: "the ledger has this revision; proposal_revision has no row"})
			continue
		}
		diffs = append(diffs, diffProposalRevision(expected, actual)...)
	}

	return VerifyResult{Tenant: tenant, StreamKey: streamKey, Checkpoint: cp, Diffs: diffs}, nil
}

func readIntentInstance(ctx context.Context, q ledger.Querier, tenant uuid.UUID, intentID uuid.UUID) (IntentInstanceRow, bool, error) {
	var row IntentInstanceRow
	row.Tenant, row.IntentID = tenant, intentID
	err := q.QueryRow(ctx, `
		SELECT definition_ref, definition_version, request_digest, request_digest_algorithm,
			idempotency_key, request_state, execution_state, business_state, consistency_state,
			obligation_state, instance_version, created_at, recorded_at, last_transition_at
		FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		tenant, intentID).Scan(
		&row.DefinitionRef, &row.DefinitionVersion, &row.RequestDigest, &row.RequestDigestAlgorithm,
		&row.IdempotencyKey, &row.RequestState, &row.ExecutionState, &row.BusinessState, &row.ConsistencyState,
		&row.ObligationState, &row.InstanceVersion, &row.CreatedAt, &row.RecordedAt, &row.LastTransitionAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return IntentInstanceRow{}, false, nil
	}
	if err != nil {
		return IntentInstanceRow{}, false, fmt.Errorf("critical: verify: read intent_instance %s: %w", intentID, err)
	}
	return row, true, nil
}

func readProposalRevision(ctx context.Context, q ledger.Querier, tenant uuid.UUID, intentID uuid.UUID, revision int64) (ProposalRevisionRow, bool, error) {
	var (
		row         ProposalRevisionRow
		artifactRef *string
	)
	row.Tenant, row.IntentID, row.Revision = tenant, intentID, revision
	err := q.QueryRow(ctx, `
		SELECT proposal_digest, material_digest, digest_algorithm, schema_ref, payload, artifact_ref, produced_by, produced_at
		FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3`,
		tenant, intentID, revision).Scan(
		&row.ProposalDigest, &row.MaterialDigest, &row.DigestAlgorithm, &row.SchemaRef,
		&row.Payload, &artifactRef, &row.ProducedBy, &row.ProducedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return ProposalRevisionRow{}, false, nil
	}
	if err != nil {
		return ProposalRevisionRow{}, false, fmt.Errorf("critical: verify: read proposal_revision %s/%d: %w", intentID, revision, err)
	}
	if artifactRef != nil {
		row.ArtifactRef = *artifactRef
	}
	return row, true, nil
}

func diffIntentInstance(expected, actual IntentInstanceRow) []Diff {
	var diffs []Diff
	add := func(field, want, got string) {
		if want != got {
			diffs = append(diffs, Diff{
				Table:  "intent_instance",
				Key:    expected.IntentID.String(),
				Detail: fmt.Sprintf("%s: want %q, stored %q", field, want, got),
			})
		}
	}
	add("definition_ref", expected.DefinitionRef, actual.DefinitionRef)
	add("definition_version", fmt.Sprint(expected.DefinitionVersion), fmt.Sprint(actual.DefinitionVersion))
	add("request_digest", expected.RequestDigest, actual.RequestDigest)
	add("request_digest_algorithm", expected.RequestDigestAlgorithm, actual.RequestDigestAlgorithm)
	add("idempotency_key", expected.IdempotencyKey, actual.IdempotencyKey)
	add("request_state", expected.RequestState, actual.RequestState)
	add("execution_state", expected.ExecutionState, actual.ExecutionState)
	add("business_state", expected.BusinessState, actual.BusinessState)
	add("consistency_state", expected.ConsistencyState, actual.ConsistencyState)
	add("obligation_state", expected.ObligationState, actual.ObligationState)
	add("instance_version", fmt.Sprint(expected.InstanceVersion), fmt.Sprint(actual.InstanceVersion))
	if !sameInstant(expected.CreatedAt, actual.CreatedAt) {
		add("created_at", expected.CreatedAt.String(), actual.CreatedAt.String())
	}
	if !sameInstant(expected.RecordedAt, actual.RecordedAt) {
		add("recorded_at", expected.RecordedAt.String(), actual.RecordedAt.String())
	}
	if !sameInstant(expected.LastTransitionAt, actual.LastTransitionAt) {
		add("last_transition_at", expected.LastTransitionAt.String(), actual.LastTransitionAt.String())
	}
	return diffs
}

func diffProposalRevision(expected, actual ProposalRevisionRow) []Diff {
	var diffs []Diff
	key := fmt.Sprintf("%s/%d", expected.IntentID, expected.Revision)
	add := func(field, want, got string) {
		if want != got {
			diffs = append(diffs, Diff{Table: "proposal_revision", Key: key, Detail: fmt.Sprintf("%s: want %q, stored %q", field, want, got)})
		}
	}
	add("proposal_digest", expected.ProposalDigest, actual.ProposalDigest)
	add("material_digest", expected.MaterialDigest, actual.MaterialDigest)
	add("digest_algorithm", expected.DigestAlgorithm, actual.DigestAlgorithm)
	add("schema_ref", expected.SchemaRef, actual.SchemaRef)
	add("produced_by", expected.ProducedBy, actual.ProducedBy)
	if string(expected.Payload) != string(actual.Payload) {
		diffs = append(diffs, Diff{Table: "proposal_revision", Key: key, Detail: "payload bytes differ"})
	}
	if !sameInstant(expected.ProducedAt, actual.ProducedAt) {
		add("produced_at", expected.ProducedAt.String(), actual.ProducedAt.String())
	}
	return diffs
}

// sameInstant compares two instants at microsecond precision, matching
// PostgreSQL's timestamptz resolution: a protobuf-sourced time.Time can
// carry sub-microsecond nanoseconds that a stored-and-reread value never
// will, and that is not a real disagreement.
func sameInstant(a, b time.Time) bool {
	return a.UTC().Truncate(time.Microsecond).Equal(b.UTC().Truncate(time.Microsecond))
}
