package checkpoint

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Querier is the minimal database capability a checkpoint read needs. It is
// internal/data/ledger.Querier itself, so whatever handle a caller already
// reads the ledger through satisfies this one unchanged.
type Querier = datalogger.Querier

// AlgorithmEd25519 is the only signature algorithm this package produces or
// accepts. It is recorded on every signature rather than assumed, so adding
// a second algorithm later is a visible change in stored evidence.
const AlgorithmEd25519 = "ed25519"

// DigestAlgorithm names the digest algorithm the root and manifest digests
// use.
const DigestAlgorithm = "sha256"

// ManifestSchemaVersion is the version of the manifest's own canonical
// projection. It is inside the signed bytes, so a manifest signed under one
// version can never be verified as though it were another.
const ManifestSchemaVersion = 1

// StreamHead is one included stream's position at the checkpoint: how far it
// had advanced, and what its hash chain proved at that point.
//
// Sequence and ChainHash are both required. A head without its chain hash
// records where a stream stood but proves nothing about what came before it,
// which is the entire purpose of the checkpoint.
type StreamHead struct {
	StreamKey      string
	Sequence       int64
	ChainHash      string
	ChainAlgorithm string
}

// SchemaRelease binds a checkpoint to the physical schema it was taken
// under: the highest applied migration version, and the digest over the
// whole migration tree (migrations.ArtifactDigest). Without it, a manifest
// could be replayed against a schema in which its stream keys, partitions or
// constraints mean something different.
type SchemaRelease struct {
	Version int64
	Digest  string
}

// Signature is an Ed25519 signature over a manifest's canonical digest,
// together with the identity of the key that produced it. The key ID and
// public key are also inside the signed bytes (see manifest.go), so a
// signature cannot be re-attributed to a different key by editing the
// envelope around it.
type Signature struct {
	Algorithm string
	KeyID     string
	PublicKey string
	Value     string
	SignedAt  time.Time
}

// Manifest is one signed checkpoint.
type Manifest struct {
	SchemaVersion int
	Tenant        uuid.UUID

	EpochID     uuid.UUID
	EpochNumber int64
	// PreviousEpochID and PreviousManifestDigest chain this epoch to the one
	// before it. Both are empty on the first epoch of a tenant and both are
	// required on every later one, so a removed epoch leaves a detectable
	// hole rather than a silently closing gap.
	PreviousEpochID        uuid.UUID
	PreviousManifestDigest string

	// CorrectsEpochID and CorrectsReason are set only on a corrective epoch
	// (see [Supersede]). The corrected epoch's own signature is never
	// touched.
	CorrectsEpochID uuid.UUID
	CorrectsReason  string

	Schema SchemaRelease

	// Streams is every stream the checkpoint covers, ordered by stream key.
	Streams []StreamHead
	// RootDigest folds every included head into one value. It is recomputed
	// and compared by [Manifest.Validate], never trusted as given.
	RootDigest          string
	RootDigestAlgorithm string

	// CoversFrom/CoversTo is the half-open recorded-time window the
	// checkpoint makes its claim over: every event recorded in
	// [CoversFrom, CoversTo) is accounted for by the heads above.
	CoversFrom time.Time
	CoversTo   time.Time
	CreatedAt  time.Time

	Signature *Signature
}

// KeyStatus is what a [KeyDirectory] knows about one signing key: the public
// key itself, the half-open window it is permitted to sign in, and whether
// and when it was revoked.
type KeyStatus struct {
	KeyID     string
	PublicKey string
	NotBefore time.Time
	// NotAfter is the exclusive end of the key's validity window. A zero
	// value means the key does not expire on a schedule.
	NotAfter time.Time
	// RevokedAt is when the key was revoked; zero means it was not. Revoking
	// is not the same as expiring: a revoked key is unusable from RevokedAt
	// onward, and a signature it produced before that instant remains
	// verifiable, because the fact that it was validly made is history.
	RevokedAt time.Time
}

// UsableAt reports whether the key was permitted to sign at the given
// instant. The window is half-open [NotBefore, NotAfter), matching every
// other business interval in this repository, and revocation closes it at
// RevokedAt.
func (k KeyStatus) UsableAt(at time.Time) bool {
	if !k.NotBefore.IsZero() && at.Before(k.NotBefore) {
		return false
	}
	if !k.NotAfter.IsZero() && !at.Before(k.NotAfter) {
		return false
	}
	if !k.RevokedAt.IsZero() && !at.Before(k.RevokedAt) {
		return false
	}
	return true
}

// ErrManifestInvalid reports a manifest that is missing something a
// checkpoint must bind, or that states something it cannot support. Missing
// lists every problem rather than only the first, so a caller building a
// manifest is told everything that is wrong in one pass.
type ErrManifestInvalid struct {
	EpochNumber int64
	Missing     []string
}

func (ErrManifestInvalid) Code() string { return "LEDGER_CHECKPOINT_MANIFEST_INVALID" }

func (e ErrManifestInvalid) Error() string {
	return fmt.Sprintf("%s: epoch %d: %s", e.Code(), e.EpochNumber, strings.Join(e.Missing, "; "))
}

// ErrIncompleteCoverage reports that a checkpoint would have omitted streams
// that hold events, or streams whose hash chain could not be read. A
// checkpoint over part of a ledger claims more than it proves, so this is
// refused rather than annotated.
type ErrIncompleteCoverage struct {
	Tenant  uuid.UUID
	Missing []string
	Reason  string
}

func (ErrIncompleteCoverage) Code() string { return "LEDGER_CHECKPOINT_INCOMPLETE_COVERAGE" }

func (e ErrIncompleteCoverage) Error() string {
	return fmt.Sprintf("%s: tenant %s: %s: %s", e.Code(), e.Tenant, e.Reason, strings.Join(e.Missing, ", "))
}

// ErrKeyNotUsable reports a signing key that is unknown to the directory, or
// that was expired or revoked at the instant it was asked to sign.
type ErrKeyNotUsable struct {
	KeyID  string
	At     time.Time
	Reason string
}

func (ErrKeyNotUsable) Code() string { return "LEDGER_CHECKPOINT_KEY_NOT_USABLE" }

func (e ErrKeyNotUsable) Error() string {
	return fmt.Sprintf("%s: key %s %s at %s", e.Code(), e.KeyID, e.Reason, e.At.UTC().Format(time.RFC3339))
}

// ErrSignatureInvalid reports a manifest whose signature does not verify
// against the digest and public key it names.
type ErrSignatureInvalid struct {
	EpochNumber int64
	KeyID       string
	Reason      string
}

func (ErrSignatureInvalid) Code() string { return "LEDGER_CHECKPOINT_SIGNATURE_INVALID" }

func (e ErrSignatureInvalid) Error() string {
	return fmt.Sprintf("%s: epoch %d signed by %s: %s", e.Code(), e.EpochNumber, e.KeyID, e.Reason)
}

// ErrRootDigestMismatch reports a manifest whose recorded root digest is not
// the digest its own stream heads produce. It is what catches a head that
// was edited after the root was computed but before the manifest was signed.
type ErrRootDigestMismatch struct {
	EpochNumber int64
	Expected    string
	Actual      string
}

func (ErrRootDigestMismatch) Code() string { return "LEDGER_CHECKPOINT_ROOT_DIGEST_MISMATCH" }

func (e ErrRootDigestMismatch) Error() string {
	return fmt.Sprintf("%s: epoch %d records root digest %s, but its heads produce %s",
		e.Code(), e.EpochNumber, e.Actual, e.Expected)
}

// ErrEpochChainBroken reports the exact epoch at which a sequence of
// checkpoints stopped chaining: a gap in the numbering, a previous-epoch
// reference that does not name its predecessor, or a previous-manifest
// digest that does not reproduce.
type ErrEpochChainBroken struct {
	Tenant      uuid.UUID
	EpochNumber int64
	Reason      string
	Expected    string
	Actual      string
}

func (ErrEpochChainBroken) Code() string { return "LEDGER_CHECKPOINT_EPOCH_CHAIN_BROKEN" }

func (e ErrEpochChainBroken) Error() string {
	if e.Expected == "" && e.Actual == "" {
		return fmt.Sprintf("%s: tenant %s epoch %d: %s", e.Code(), e.Tenant, e.EpochNumber, e.Reason)
	}
	return fmt.Sprintf("%s: tenant %s epoch %d: %s (expected %s, got %s)",
		e.Code(), e.Tenant, e.EpochNumber, e.Reason, e.Expected, e.Actual)
}

// ErrEpochAlreadyRecorded reports a second attempt to record an epoch number
// a tenant already has. Epochs are append-only, exactly like the events they
// attest to.
type ErrEpochAlreadyRecorded struct {
	Tenant      uuid.UUID
	EpochNumber int64
}

func (ErrEpochAlreadyRecorded) Code() string { return "LEDGER_CHECKPOINT_EPOCH_ALREADY_RECORDED" }

func (e ErrEpochAlreadyRecorded) Error() string {
	return fmt.Sprintf("%s: tenant %s already has an epoch numbered %d", e.Code(), e.Tenant, e.EpochNumber)
}

// ErrEpochNotFound reports a read for an epoch that does not exist.
type ErrEpochNotFound struct {
	Tenant      uuid.UUID
	EpochNumber int64
}

func (ErrEpochNotFound) Code() string { return "LEDGER_CHECKPOINT_EPOCH_NOT_FOUND" }

func (e ErrEpochNotFound) Error() string {
	return fmt.Sprintf("%s: tenant %s has no epoch numbered %d", e.Code(), e.Tenant, e.EpochNumber)
}
