package quarantine

import (
	"context"
	"time"
)

// QuarantinedRecord is the intake fact [Store.RecordQuarantined] persists:
// the bytes this package received, their digest, their declared and sniffed
// content types, and the evidence naming the intake event that produced
// them. It is written exactly once per content id, before any verdict is
// reached.
type QuarantinedRecord struct {
	ContentID           string
	DigestAlgorithm     string
	ByteSize            int64
	DeclaredContentType ContentType
	SniffedCategory     Category
	CreatorPrincipalRef string
	EvidenceID          string
	Content             []byte
}

// VerdictRecord is the fact [Store.RecordVerdict] persists: the state
// [Upload] reached for a content id already recorded by
// [Store.RecordQuarantined], which declared scanner produced it (empty for
// neither field is valid once State is [Admitted] or [Rejected]), and --
// for [Rejected] -- why.
type VerdictRecord struct {
	ContentID      string
	State          State
	ScannerID      string
	ScannerVersion string
	Reason         string
	EvidenceID     string
}

// StateRecord is one content id's current quarantine state, as [Store]
// reports it: the most recently recorded row, never an aggregate or a
// mutable column.
type StateRecord struct {
	ContentID      string
	State          State
	ScannerID      string
	ScannerVersion string
	Reason         string
	EvidenceID     string
	RecordedAt     time.Time
}

// Store is the persistence port [Upload] and [Use] speak. It is the seam
// between this package's pure policy logic and internal/data/artifacts'
// PostgreSQL-backed implementation
// (migrations/00030_artifact_quarantine.sql): every method takes and returns
// only this package's own types, never a database driver type, so this
// package -- and anything that only ever calls it through a fake for a test
// -- never needs to know PostgreSQL exists.
type Store interface {
	// RecordQuarantined persists rec as the initial [Quarantined] fact for
	// its content id, for a given tenant. It is idempotent by content id:
	// recording the same content id twice with identical bytes and
	// declared/sniffed type is a no-op, not a second row.
	RecordQuarantined(ctx context.Context, tenant string, rec QuarantinedRecord) error
	// RecordVerdict appends rec -- whose State is always [Admitted] or
	// [Rejected], never [Quarantined] -- to the verdict log for a content
	// id [RecordQuarantined] already recorded, for a given tenant.
	RecordVerdict(ctx context.Context, tenant string, rec VerdictRecord) error
	// CurrentState returns the most recently recorded state for a content
	// id, for a given tenant. It reports [ErrNotFound] when no
	// [RecordQuarantined] call has ever been made for that content id and
	// tenant, including a syntactically well-formed but never-uploaded
	// digest.
	CurrentState(ctx context.Context, tenant, contentID string) (StateRecord, error)
}
