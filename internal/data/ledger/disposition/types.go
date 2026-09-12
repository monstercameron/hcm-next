package disposition

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PayloadState is the explicit, caller-facing disposition of one ledger
// event's inline payload. There is no permissive zero value: "" is not one
// of the five declared states, so a caller that forgets to inspect State
// before reading Bytes or ArtifactRef gets a value that matches none of
// them rather than one that silently reads as "present".
type PayloadState string

const (
	// StatePresent is an ordinary event that still carries its original
	// inline payload, untouched by any disposition action.
	StatePresent PayloadState = "PRESENT"
	// StateReferenced is an event that never carried an inline payload at
	// all: its content lives at ArtifactRef. This is the state a caller
	// must be able to tell apart from StatePayloadErased and
	// StateRestricted -- all three would read identically as a nil Payload
	// through the raw internal/data/ledger.EventRecord, which is exactly
	// the bug LEDGER-011 exists to close.
	StateReferenced PayloadState = "REFERENCED"
	// StateHeld reports that an active legal hold grips the tracked copy
	// naming this event. Erase refuses to run while this is true.
	StateHeld PayloadState = "HELD"
	// StateRestricted is a crypto-erased payload: classification
	// ClassificationEncryptedAtRest, mechanism MechanismCryptoErasure. See
	// the package doc for the honest boundary this models.
	StateRestricted PayloadState = "RESTRICTED"
	// StatePayloadErased is an overwritten payload: classification
	// ClassificationStandard, mechanism MechanismOverwrite.
	StatePayloadErased PayloadState = "PAYLOAD_ERASED"
)

// Classification is the governance decision that selects how Erase disposes
// of a payload. It is supplied by the caller (a records/legal decision, not
// something this package infers), and it is the only input Classify
// consults.
type Classification string

const (
	// ClassificationStandard names a payload that was never required to be
	// encrypted at rest. Its content is destroyed by MechanismOverwrite,
	// producing StatePayloadErased.
	ClassificationStandard Classification = "STANDARD"
	// ClassificationEncryptedAtRest names a payload whose classification
	// requires encryption at rest. Its content is destroyed by
	// MechanismCryptoErasure (key destruction, not byte overwrite),
	// producing StateRestricted.
	ClassificationEncryptedAtRest Classification = "ENCRYPTED_AT_REST"
)

// Mechanism is the physical technique Erase used to carry out a
// classification's disposition decision.
type Mechanism string

const (
	// MechanismOverwrite destroys a payload directly.
	MechanismOverwrite Mechanism = "OVERWRITE"
	// MechanismCryptoErasure destroys the key that protects a payload
	// rather than the payload's bytes.
	MechanismCryptoErasure Mechanism = "CRYPTO_ERASURE"
)

// ErrClassificationRequired reports a Classification outside the two
// declared values. There is no default: an empty, misspelled or otherwise
// unrecognized classification is refused rather than treated as
// ClassificationStandard, because silently choosing the weaker mechanism for
// an unrecognized classification is precisely the failure this type exists
// to make impossible.
var ErrClassificationRequired = errors.New("disposition: classification must be STANDARD or ENCRYPTED_AT_REST")

// Classify is the one place LEDGER-011's REFACTOR clause lives: the boundary
// between "the payload is encrypted at rest, so erasure destroys a key" and
// "it is not, so erasure destroys the bytes" is this explicit, exhaustive
// function of Classification alone, not an implicit choice buried inside
// Erase. It has no default branch: every input either maps to exactly one
// (Mechanism, PayloadState) pair or is refused.
func Classify(c Classification) (Mechanism, PayloadState, error) {
	switch c {
	case ClassificationStandard:
		return MechanismOverwrite, StatePayloadErased, nil
	case ClassificationEncryptedAtRest:
		return MechanismCryptoErasure, StateRestricted, nil
	default:
		return "", "", fmt.Errorf("%w: got %q", ErrClassificationRequired, string(c))
	}
}

// View is the only supported way to learn an event's payload disposition.
// It deliberately does not embed internal/data/ledger.EventRecord: State
// must be inspected before Bytes, ArtifactRef or HoldID mean anything, and
// each is only ever populated for the one State it belongs to.
type View struct {
	Tenant    uuid.UUID
	StreamKey string
	Sequence  int64

	// State is always set; it is never the zero value on a View this
	// package returns without an error.
	State PayloadState

	// Bytes is the original inline payload. It is populated only when
	// State == StatePresent.
	Bytes []byte
	// ArtifactRef names the governed bytes this event references instead of
	// an inline payload. It is populated only when State == StateReferenced.
	ArtifactRef string
	// HoldID names the active hold gripping this event's tracked copy. It is
	// populated only when State == StateHeld, and may be uuid.Nil even then
	// if the copy's own hold_state says HELD but the specific intersection
	// could not be resolved (still reported as Held, never downgraded).
	HoldID uuid.UUID

	// Classification, Mechanism, Reason, Actor and RecordedAt describe the
	// disposition decision itself. They are populated only when State is
	// StateRestricted or StatePayloadErased.
	Classification Classification
	Mechanism      Mechanism
	Reason         string
	Actor          string
	RecordedAt     time.Time
}

// Disposition is the durable row Erase records in ledger_payload_disposition
// for one event. It is the source Disposition-shaped fields on View are read
// from.
type Disposition struct {
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	DeclarationID   uuid.UUID
	LinkID          uuid.UUID
	Classification  Classification
	Mechanism       Mechanism
	State           PayloadState
	Reason          string
	Actor           string
	KeyRef          string
	KeyDestroyedAt  *time.Time
	Digest          string
	DigestAlgorithm string
	RecordedAt      time.Time
}
