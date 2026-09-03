package provenance

import (
	"strconv"
	"time"

	"github.com/google/uuid"
)

// SourceKind names what a provenance record is provenance for. There are
// exactly two: the two things DATA-014 requires a record for.
type SourceKind string

const (
	// SourceLedgerEvent is provenance for one appended ledger event
	// (internal/data/ledger.EventRecord).
	SourceLedgerEvent SourceKind = "LEDGER_EVENT"
	// SourceExternalObservation is provenance for one persisted external
	// observation (INTG-009).
	SourceExternalObservation SourceKind = "EXTERNAL_OBSERVATION"
)

// Valid reports whether k is one of the two declared kinds.
func (k SourceKind) Valid() bool {
	switch k {
	case SourceLedgerEvent, SourceExternalObservation:
		return true
	default:
		return false
	}
}

// Digest is one named digest a provenance record cites - a ledger event's
// own canonical digest, a request digest, a material proposal digest, and
// so on. Kind names which one; nothing here recomputes it.
type Digest struct {
	Kind      string `json:"kind"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}

// PublishRequest is everything [Publish] needs to record and distribute one
// provenance edge.
type PublishRequest struct {
	Tenant uuid.UUID
	// IntentRef is the intent this provenance belongs to - the key
	// [Lineage] queries by. It is a string rather than a uuid.UUID because
	// not every subject a provenance record can be published for is
	// necessarily an IntentInstance in every future caller.
	IntentRef string
	// SourceKind and SourceRef together are this record's idempotency
	// identity: publishing twice for the same (Tenant, SourceKind,
	// SourceRef) is a no-op, never a second edge. SourceRef is the
	// caller's own stable name for the source - LedgerEventSourceRef
	// builds the conventional one for a ledger event.
	SourceKind SourceKind
	SourceRef  string

	// StreamKey, Sequence and EventID are populated for SourceLedgerEvent;
	// ObservationRef is populated for SourceExternalObservation. Both may
	// be set for a hybrid case (an observation published under a ledger
	// event's own provenance); neither is required for the other's kind.
	StreamKey      string
	Sequence       int64
	EventID        uuid.UUID
	ObservationRef string
	// ConnectorRef names the connector/connection that produced an
	// observation, when applicable.
	ConnectorRef string

	// SourceAuthority names who or what is answerable for this fact (an
	// authority_assignment ref, a connector definition ref, or similar).
	SourceAuthority string
	// PrincipalRef names the principal whose action caused this record to
	// exist.
	PrincipalRef string
	// EvidenceIDs must be non-empty: DATA-014's named RED clause is that
	// provenance is never published without its evidence id.
	EvidenceIDs []string
	// Digests must be non-empty: a provenance record with no digest could
	// never be checked against the fact it claims to explain.
	Digests []Digest

	// PublishedAt is when this provenance was recorded. A zero value means
	// "now" - see [Publish].
	PublishedAt time.Time
}

// Record is one durable provenance_record row, as published or as read
// back by [Lineage].
type Record struct {
	Tenant          uuid.UUID
	RecordID        uuid.UUID
	IntentRef       string
	SourceKind      SourceKind
	SourceRef       string
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	ObservationRef  string
	ConnectorRef    string
	SourceAuthority string
	PrincipalRef    string
	EvidenceIDs     []string
	Digests         []Digest
	PublishedAt     time.Time
	RecordedAt      time.Time
	// Published is false when this call found an existing record for the
	// same (Tenant, SourceKind, SourceRef) rather than creating a new one -
	// the "duplicate publish is a no-op" contract.
	Published bool
}

// LedgerEventSourceRef is the conventional SourceRef for a ledger event:
// stable, and readable enough to debug without a lookup.
func LedgerEventSourceRef(streamKey string, sequence int64) string {
	return streamKey + "@" + strconv.FormatInt(sequence, 10)
}
