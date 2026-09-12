package evidence

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/evidence"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrReceiptNotFound reports a receipt_id with no visible record. It is
	// deliberately the same sentinel whether the record does not exist at
	// all, belongs to another tenant, or was authorized for a different
	// purpose: existence itself is not disclosed to a caller outside those
	// boundaries.
	ErrReceiptNotFound = errors.New("evidence: receipt not found or not visible")

	// ErrLineageNotFound reports an intent_id with no recorded execution
	// lineage for this tenant.
	ErrLineageNotFound = errors.New("evidence: intent execution lineage not found")

	// ErrLineageIncomplete reports that one of the six named causal hops
	// (intent, proposal, approvals, transaction-heads, observations,
	// reconciliation) is not PRESENT in the recorded lineage. A closed,
	// executed intent must be able to name all six; anything less is
	// refused rather than exported as a partial truth.
	ErrLineageIncomplete = errors.New("evidence: execution lineage is missing a required causal hop")

	// ErrPurposeNotConfigured reports a purpose with no reviewed
	// [PurposePolicy] entry. An unrecognized purpose is refused, never
	// treated as "allow everything" or "allow nothing" by default.
	ErrPurposeNotConfigured = errors.New("evidence: purpose has no reviewed field-visibility policy")

	// ErrNotConfigured reports a server composed without one of its
	// required dependencies.
	ErrNotConfigured = errors.New("evidence: server is not fully configured")

	// ErrArtifactNotFound reports a missing export artifact.
	ErrArtifactNotFound = errors.New("evidence: export artifact not found")

	// ErrPackageTampered reports a package that fails offline verification:
	// a flipped byte, a stripped signature, or a digest mismatch.
	ErrPackageTampered = errors.New("evidence: export package failed offline verification")
)

// RequiredLineageHops is the exact six-hop causal chain EP-EVID-001's RED
// clause names: "omits intent→proposal→approval→transaction→observation→
// reconciliation lineage". Every name here must resolve to one of
// evidence.Dimensions, and each must be evidence.StatusPresent in an
// exportable lineage -- named individually, not merely "non-empty", so a
// five-of-six lineage fails exactly like the zero-of-six case.
var RequiredLineageHops = []string{
	"intent",
	"proposal",
	"approvals",
	"transaction-heads",
	"observations",
	"reconciliation",
}

// DimensionFact is one caller-recorded terminal dimension, exactly as it was
// written when the intent closed. It is the transport-side mirror of
// evidence.DimensionInput; kept separate so this package never has to import
// evidence.Status's zero value as a default.
type DimensionFact struct {
	Name   string
	Status evidence.Status
	Digest string
	Note   string
}

// LineageSnapshot is the complete, immutable set of facts a [LineageSource]
// hands back for one closed intent. Every field must already have been true
// at the instant the intent closed; nothing here may be recomputed from
// current domain state.
type LineageSnapshot struct {
	Tenant           string
	IntentRef        string
	LineageDigest    string
	AuthorityLineage []string
	Dimensions       []DimensionFact
	ClosedAt         time.Time
}

// LineageSource supplies the frozen dimension facts recorded when an intent
// closed. Implementations own exactly one read method and no write method,
// which is structural, not incidental: a port that cannot mutate cannot be
// the source of an unauthorized domain write, and a port with only this one
// method cannot answer with anything but what was recorded at closure --
// there is no "give me the current row" method to reach for instead.
type LineageSource interface {
	Snapshot(ctx context.Context, tenant, intentID string) (LineageSnapshot, error)
}

// Receipt is the wire-adjacent, already-immutable zero-effect receipt
// GetExecutionReceipt serves (see doc.go for why this is not
// BusinessExecutionReceipt). Owner, when non-empty, additionally scopes
// visibility to one subject; Purpose always scopes visibility to callers
// authorized for that exact purpose.
type Receipt struct {
	ReceiptID      string
	TenantID       string
	Owner          string
	Purpose        string
	IssuedAt       time.Time
	IntentType     string
	IntentVersion  string
	Mode           string
	RequestState   string
	ExecutionState string
	Controls       []ControlVersion
	InputsDigest   string
	ResultDigest   string
}

// ControlVersion names one pinned control artifact a receipt cites.
type ControlVersion struct{ Name, Version string }

// ReceiptStore serves already-assembled, immutable receipts by id. It has
// no write method for the same structural reason [LineageSource] does not:
// GetExecutionReceipt is a pure read and must never be the path by which a
// receipt is created or changed.
type ReceiptStore interface {
	Get(ctx context.Context, tenant, receiptID string) (Receipt, error)
}

// PurposePolicy names, per declared purpose of processing, exactly which
// BusinessExecutionReceipt dimension names (evidence.Dimensions) a caller
// exporting evidence under that purpose may see unredacted. An unrecognized
// purpose must return ok=false; there is no default set this package will
// fall back to.
type PurposePolicy interface {
	AllowedDimensions(purpose string) (allowed []string, ok bool)
}

// ArtifactSink persists exactly one export artifact's bytes and returns an
// opaque reference to it. It is the only thing besides the operation record
// ExportIntentEvidence is permitted to write to.
type ArtifactSink interface {
	Put(ctx context.Context, tenant, artifactID string, content []byte) (ref string, err error)
	Get(ctx context.Context, tenant, artifactID string) ([]byte, error)
}
