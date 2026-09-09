package ledger

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	checkpointadapter "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
)

// Checkpoint value types, re-exported so a business package needs only this
// port import to take, read or verify a signed ledger checkpoint. They are
// plain data and two seams - a signer and a key directory - neither of which
// depends on PostgreSQL, so aliasing rather than redeclaring them keeps one
// definition (the same reasoning as the append-path aliases in port.go).
type (
	CheckpointManifest      = checkpointadapter.Manifest
	CheckpointRequest       = checkpointadapter.CreateRequest
	CheckpointStreamHead    = checkpointadapter.StreamHead
	CheckpointSchemaRelease = checkpointadapter.SchemaRelease
	CheckpointSignature     = checkpointadapter.Signature
	CheckpointSigner        = checkpointadapter.Signer
	CheckpointKeyDirectory  = checkpointadapter.KeyDirectory
	CheckpointKeyStatus     = checkpointadapter.KeyStatus
	CheckpointAnchor        = checkpointadapter.Anchor
)

// Checkpointer takes signed checkpoints over a tenant's ledger and records
// them as append-only integrity epochs.
//
// Both methods run inside the caller's own transaction, for the same reason
// [Appender] does: a checkpoint attests to a ledger state, and reading that
// state and recording the attestation must not be separable by a concurrent
// append. internal/data/ledger/checkpoint.Service implements it.
type Checkpointer interface {
	// Create takes a checkpoint over every stream that holds an event
	// recorded before the request's instant, and refuses rather than
	// degrades when coverage is incomplete or the signing key is not usable.
	Create(ctx context.Context, tx dbport.Tx, req CheckpointRequest) (CheckpointManifest, error)
	// Supersede records a corrective epoch naming the epoch it corrects and
	// why. The corrected epoch's signature is never rewritten.
	Supersede(ctx context.Context, tx dbport.Tx, req CheckpointRequest, correctsEpoch int64, reason string) (CheckpointManifest, error)
}

// VerifyCheckpoint checks one signed manifest offline: it needs the manifest
// and a key directory and touches no database. It re-folds the stream heads,
// re-projects the canonical digest, re-checks the signature, and refuses a
// signature made by a key that was not permitted to make it at the instant
// the manifest claims it was made.
func VerifyCheckpoint(m CheckpointManifest, dir CheckpointKeyDirectory) error {
	return checkpointadapter.Verify(m, dir)
}

// VerifyCheckpointChain verifies every epoch a tenant has, independently and
// in order, and returns how many were checked. A removed epoch breaks the
// chain at a named number rather than closing the gap silently.
func VerifyCheckpointChain(ctx context.Context, q Querier, tenant uuid.UUID, dir CheckpointKeyDirectory) (int, error) {
	return checkpointadapter.VerifyChain(ctx, q, tenant, dir)
}

// NewCheckpointer builds the default [Checkpointer] over a signer and a key
// directory. Both are required: without a directory nothing could refuse a
// revoked key.
func NewCheckpointer(signer CheckpointSigner, dir CheckpointKeyDirectory) (Checkpointer, error) {
	return checkpointadapter.NewService(signer, dir)
}
