package schemasnapshot

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
)

// Algorithm is the only digest algorithm a snapshot's identity is computed
// under, matching internal/data/artifacts.Algorithm: canonical_digest and
// artifact_ref are the same sha256 content id, named twice under two
// different intentions (see migrations/00025_schema_snapshot.sql).
const Algorithm = artifacts.Algorithm

// ValidContentID reports whether id is a syntactically well-formed sha256
// content id, delegating to the artifact store's own definition so the two
// packages can never silently drift apart on what a digest looks like.
func ValidContentID(id string) bool { return artifacts.ValidContentID(id) }

// State is a schema snapshot's position in its one-way admission lifecycle.
type State string

// The declared states. There is no fourth: a snapshot is either under
// review, or it has been decided one way or the other, forever.
const (
	// StateQuarantined is the only state [Ingest] ever creates a row in. A
	// quarantined snapshot has not been validated to a verdict yet, or its
	// verdict was lost before being recorded (e.g. a crash between the insert
	// and the decision) and validation is expected to resume.
	StateQuarantined State = "QUARANTINED"
	// StateAdmitted is a terminal, positive verdict: every configured
	// validator passed. Only a snapshot in this state may ever satisfy
	// [SchemaSnapshot.RequireAdmitted].
	StateAdmitted State = "ADMITTED"
	// StateRejected is a terminal, negative verdict: at least one configured
	// validator failed.
	StateRejected State = "REJECTED"
)

// States returns every declared state, in canonical order.
func States() []State { return []State{StateQuarantined, StateAdmitted, StateRejected} }

// Valid reports whether s is a declared state.
func (s State) Valid() bool {
	switch s {
	case StateQuarantined, StateAdmitted, StateRejected:
		return true
	default:
		return false
	}
}

// Terminal reports whether no transition leaves s. Both verdicts are
// terminal: this package never re-decides a snapshot, on the theory that a
// changed mind about identical bytes belongs in a new ingest under new
// validators, not a mutated verdict on the old one.
func (s State) Terminal() bool { return s == StateAdmitted || s == StateRejected }

func (s State) String() string { return string(s) }

// IsLegalTransition reports whether from -> to is permitted. The table has
// exactly two edges, both leaving StateQuarantined; nothing leaves a
// terminal state.
func IsLegalTransition(from, to State) bool {
	return from == StateQuarantined && (to == StateAdmitted || to == StateRejected)
}

// DeclaredFormat is the shape a captured schema claims to be in. It is
// "declared" because nothing upstream of [Ingest] is trusted to have
// verified it; well-formedness validation checks the claim against the
// bytes.
type DeclaredFormat string

// The declared formats this package can validate the well-formedness of.
// planning/specs/integration-platform.md's schema-discovery coverage is a
// MINIMAL CONTRACT, so GraphQL SDL and Protobuf get only the generic
// well-formed-text check ([checkPlainText]); JSON_SCHEMA/OPENAPI get real
// JSON parsing and XSD/WSDL get real XML tokenization.
const (
	FormatJSONSchema DeclaredFormat = "JSON_SCHEMA"
	FormatOpenAPI    DeclaredFormat = "OPENAPI"
	FormatXSD        DeclaredFormat = "XSD"
	FormatWSDL       DeclaredFormat = "WSDL"
	FormatCSVHeader  DeclaredFormat = "CSV_HEADER"
	FormatGraphQLSDL DeclaredFormat = "GRAPHQL_SDL"
	FormatProtobuf   DeclaredFormat = "PROTOBUF"
)

// DeclaredFormats returns every declared format, in canonical order. It is
// also exactly the set migrations/00025's
// integration_schema_snapshot_format_allowed CHECK constraint permits.
func DeclaredFormats() []DeclaredFormat {
	return []DeclaredFormat{
		FormatJSONSchema, FormatOpenAPI, FormatXSD, FormatWSDL,
		FormatCSVHeader, FormatGraphQLSDL, FormatProtobuf,
	}
}

// Valid reports whether f is a declared format.
func (f DeclaredFormat) Valid() bool {
	for _, known := range DeclaredFormats() {
		if f == known {
			return true
		}
	}
	return false
}

func (f DeclaredFormat) String() string { return string(f) }

// ProviderRef names the external system a schema snapshot was captured from:
// which connector, which tenant connection, which source instance. It
// mirrors the attribution internal/connectivity/observe.Observation carries
// (ConnectorID/ConnectionID/SourceRef), because a schema snapshot is
// evidence about the same external system an observation reads from.
type ProviderRef struct {
	ConnectorID  string
	ConnectionID string
	SourceRef    string
}

// Validate reports whether the provider reference is complete.
func (p ProviderRef) Validate() error {
	const op = "schemasnapshot.ProviderRef.Validate"
	switch {
	case strings.TrimSpace(p.ConnectorID) == "":
		return newError(op, ErrIncomplete, "provider has no connector id")
	case strings.TrimSpace(p.ConnectionID) == "":
		return newError(op, ErrIncomplete, "provider has no connection id")
	case strings.TrimSpace(p.SourceRef) == "":
		return newError(op, ErrIncomplete, "provider has no source ref")
	}
	return nil
}

// Equal reports whether p and o name the same provider.
func (p ProviderRef) Equal(o ProviderRef) bool {
	return p.ConnectorID == o.ConnectorID &&
		p.ConnectionID == o.ConnectionID &&
		p.SourceRef == o.SourceRef
}

// snapshotNamespace seeds the deterministic snapshot identity. It is a fixed
// UUID so the same (tenant, provider, digest) always yields the same
// snapshot id, on every machine and every rerun, the same technique
// internal/connectivity/observe.PageIdentity uses for observation identity.
var snapshotNamespace = uuid.MustParse("2f6a8c1e-9b3d-4e7a-8f1c-5d2b9a4e6c73")

// Identity is the tuple a snapshot's id is derived from. Two ingests of
// byte-identical content for the same provider share it, which is what
// makes re-ingest idempotent rather than duplicative.
type Identity struct {
	TenantID        string
	Provider        ProviderRef
	CanonicalDigest string
}

// SnapshotID returns the deterministic identity for this tuple.
func (i Identity) SnapshotID() uuid.UUID {
	name := strings.Join([]string{
		i.TenantID, i.Provider.ConnectorID, i.Provider.ConnectionID, i.Provider.SourceRef,
		i.CanonicalDigest,
	}, "\x1f")
	return uuid.NewSHA1(snapshotNamespace, []byte(name))
}

// SchemaSnapshot is one immutable capture of an external schema, plus its
// mutable-exactly-once admission state.
//
// Everything except State, StateReason and DecidedAt is identity: it is
// fixed at [Ingest] time and migrations/00025's
// schema_snapshot_forbid_identity_mutation trigger refuses any UPDATE that
// touches it. The raw bytes themselves never live on this row -- ArtifactRef
// points at the content-addressed artifact internal/data/artifacts stores
// them under (CanonicalDigest is that same content id, named as the
// snapshot's own identity rather than as a storage pointer).
type SchemaSnapshot struct {
	SnapshotID      uuid.UUID
	TenantID        string
	Provider        ProviderRef
	CapturedAt      time.Time
	DeclaredFormat  DeclaredFormat
	DigestAlgorithm string
	CanonicalDigest string
	ArtifactRef     string
	ByteSize        int64
	// Supersedes, when set, names a prior snapshot of the same provider that
	// this one supersedes. Immutable once ingested.
	Supersedes *uuid.UUID
	State      State
	// StateReason and DecidedAt are populated together with State, exactly
	// once, when Ingest's validators reach a verdict. Both are zero while
	// State is StateQuarantined.
	StateReason string
	DecidedAt   time.Time
	// RecordedAt is when this row was first written (system time), distinct
	// from CapturedAt (the business time of the external capture).
	RecordedAt time.Time
}

// Validate reports whether the snapshot carries everything its own identity
// and current state require.
func (s SchemaSnapshot) Validate() error {
	const op = "schemasnapshot.SchemaSnapshot.Validate"
	switch {
	case s.SnapshotID == uuid.Nil:
		return newError(op, ErrIncomplete, "snapshot has no id")
	case strings.TrimSpace(s.TenantID) == "":
		return newError(op, ErrIncomplete, "snapshot has no tenant")
	}
	if err := s.Provider.Validate(); err != nil {
		return err
	}
	switch {
	case s.CapturedAt.IsZero():
		return newError(op, ErrIncomplete, "snapshot has no capture time")
	case !s.DeclaredFormat.Valid():
		return newError(op, ErrIncomplete, "snapshot declares no recognized format")
	case s.DigestAlgorithm != Algorithm:
		return newError(op, ErrIncomplete, "snapshot digest algorithm %q is not %q", s.DigestAlgorithm, Algorithm)
	case !ValidContentID(s.CanonicalDigest):
		return newError(op, ErrIncomplete, "snapshot has no valid canonical digest")
	case !ValidContentID(s.ArtifactRef):
		return newError(op, ErrIncomplete, "snapshot has no valid artifact reference")
	case s.ByteSize <= 0:
		return newError(op, ErrIncomplete, "snapshot has no positive byte size")
	case !s.State.Valid():
		return newError(op, ErrIncomplete, "snapshot has no recognized state")
	}
	if s.Supersedes != nil && *s.Supersedes == s.SnapshotID {
		return newError(op, ErrInvalid, "snapshot cannot supersede itself")
	}
	if s.State == StateQuarantined {
		if s.StateReason != "" || !s.DecidedAt.IsZero() {
			return newError(op, ErrInvalid, "a quarantined snapshot must not carry a decision")
		}
		return nil
	}
	switch {
	case strings.TrimSpace(s.StateReason) == "":
		return newError(op, ErrIncomplete, "a decided snapshot has no reason")
	case s.DecidedAt.IsZero():
		return newError(op, ErrIncomplete, "a decided snapshot has no decision time")
	}
	return nil
}

// RequireAdmitted reports why the snapshot may not be used as a mapping
// input, or nil when it may. It is the one call INTG-005/006-style consumers
// need to enforce "a snapshot never becomes a mapping input while
// quarantined" -- and, symmetrically, never once it has been rejected.
func (s SchemaSnapshot) RequireAdmitted() error {
	const op = "schemasnapshot.SchemaSnapshot.RequireAdmitted"
	if s.State == StateAdmitted {
		return nil
	}
	return newError(op, ErrNotAdmitted,
		"snapshot %s is %s, not ADMITTED; it cannot be used as a mapping input", s.SnapshotID, s.State)
}

// sameIdentity reports whether a and b agree on every identity field: the
// fields migrations/00025's schema_snapshot_forbid_identity_mutation trigger
// protects, mirrored here so [MemoryStore] enforces the identical rule an
// ON CONFLICT re-read enforces in Postgres.
func sameIdentity(a, b SchemaSnapshot) bool {
	if !a.Provider.Equal(b.Provider) {
		return false
	}
	if !a.CapturedAt.Equal(b.CapturedAt) {
		return false
	}
	aSupersedes, bSupersedes := "", ""
	if a.Supersedes != nil {
		aSupersedes = a.Supersedes.String()
	}
	if b.Supersedes != nil {
		bSupersedes = b.Supersedes.String()
	}
	return a.DeclaredFormat == b.DeclaredFormat &&
		a.DigestAlgorithm == b.DigestAlgorithm &&
		a.CanonicalDigest == b.CanonicalDigest &&
		a.ArtifactRef == b.ArtifactRef &&
		a.ByteSize == b.ByteSize &&
		aSupersedes == bSupersedes
}

func cloneSnapshot(s SchemaSnapshot) SchemaSnapshot {
	out := s
	if s.Supersedes != nil {
		id := *s.Supersedes
		out.Supersedes = &id
	}
	return out
}
