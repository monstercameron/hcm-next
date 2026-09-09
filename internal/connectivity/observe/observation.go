// Package observe turns bounded reads of an external system into immutable,
// replayable observation evidence.
//
// Semantic owner: connectivity. Phase: P1A.
//
// # What an observation is, and is not
//
// An [Observation] is a record that a named external authority said something
// at a named moment, under a named schema version, at a named position in a
// named source snapshot. It is never a domain fact. Nothing here promotes an
// observation to truth; that is a governed decision made elsewhere, and this
// package deliberately gives it no help.
//
// Every observation therefore carries its own attribution: source, authority,
// schema version, snapshot, cursor, retrieval time, watermark and content
// digest. An observation missing any of those cannot be constructed, because a
// record that cannot say where it came from is not evidence.
//
// # Freshness is reported, not assumed
//
// [Freshness] has five values and one of them is UNKNOWN. A provider that does
// not timestamp its records produces observations that honestly say so, rather
// than observations that quietly claim to be current.
//
// # Bounded traversal and fenced resume
//
// [Runner] walks pages within the connection's bounds and commits a
// [Checkpoint] after each page is persisted. A restart resumes from the last
// committed cursor. Because the observation identity is derived from
// (tenant, connection, object, snapshot, page), a page that was persisted but
// not checkpointed is re-appended identically rather than duplicated, and a
// stale worker's commit is rejected by the checkpoint fence.
package observe

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// Sentinel causes. Classify with [errors.Is].
var (
	// ErrIncomplete reports an observation missing required attribution.
	ErrIncomplete = errors.New("observe: observation is structurally incomplete")
	// ErrImmutable reports an attempt to store different content under an
	// observation identity that already exists.
	ErrImmutable = errors.New("observe: observation is immutable")
	// ErrNotFound reports an unknown observation.
	ErrNotFound = errors.New("observe: observation not found")
	// ErrDigestMismatch reports a stored observation whose payload no longer
	// reproduces its digest.
	ErrDigestMismatch = errors.New("observe: recomputed digest does not match")
	// ErrSequenceGap reports a replay whose page sequence is not contiguous.
	ErrSequenceGap = errors.New("observe: observation page sequence has a gap")
	// ErrFenced reports a checkpoint commit behind the stored fence. It is how
	// a resumed run refuses to be overwritten by the worker it replaced.
	ErrFenced = errors.New("observe: checkpoint fence is stale")
	// ErrStore reports a storage-layer failure.
	ErrStore = errors.New("observe: store failed")
)

// Error is the single error type this package returns.
type Error struct {
	Op     string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Detail != "" {
		msg = msg + ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}

// Freshness is how much an observation may be relied upon. UNKNOWN and
// UNAVAILABLE are values, not failures: reporting them is the honest answer,
// and forcing a caller to handle them is the point.
type Freshness string

// The freshness values.
const (
	// FreshnessFresh means the watermark is within the connector's budget.
	FreshnessFresh Freshness = "FRESH"
	// FreshnessStale means the source's own watermark is older than budget.
	FreshnessStale Freshness = "STALE"
	// FreshnessPartial means the provider served less than it was asked for.
	FreshnessPartial Freshness = "PARTIAL"
	// FreshnessUnknown means the provider gave no usable watermark.
	FreshnessUnknown Freshness = "UNKNOWN"
	// FreshnessUnavailable means the object could not be read at all.
	FreshnessUnavailable Freshness = "UNAVAILABLE"
)

// Valid reports whether f is a declared value.
func (f Freshness) Valid() bool {
	switch f {
	case FreshnessFresh, FreshnessStale, FreshnessPartial, FreshnessUnknown, FreshnessUnavailable:
		return true
	default:
		return false
	}
}

// Classify judges a page's freshness against a budget. A partial page is
// partial regardless of its watermark: the caller's next question is "did I see
// everything", and answering "fresh" would be a lie of omission.
func Classify(page connectivity.Page, budget time.Duration) Freshness {
	switch {
	case page.Partial:
		return FreshnessPartial
	case page.Watermark.IsZero() || budget <= 0:
		return FreshnessUnknown
	case page.RetrievedAt.Sub(page.Watermark) <= budget:
		return FreshnessFresh
	default:
		return FreshnessStale
	}
}

// Classification is the assertion class an observation is recorded under. It
// is a one-value type on purpose: this package cannot produce a domain fact,
// and giving the field a second legal value would be the first step towards
// one.
type Classification string

// ClassificationExternalObservation is the only class this package records.
const ClassificationExternalObservation Classification = "EXTERNAL_OBSERVATION"

// observationNamespace seeds the deterministic observation identity. It is a
// fixed UUID so that the same page always yields the same observation id, on
// every machine and every rerun.
var observationNamespace = uuid.MustParse("8f3d1a4e-5f0b-4c2a-9d7e-6b1c0a2f5d34")

const (
	pageSchema  = "hcmnext.connectivity.observe.ObservationPage"
	pageVersion = 1
)

// Observation is one immutable, persisted page of external evidence.
//
// Payload holds the canonical bytes of the normalized page, not the provider's
// raw response. Raw provider payloads follow their own retention policy and
// appear here only as RawArtifactRef, so that deleting a raw response under a
// retention rule never destroys the evidence that the observation happened.
type Observation struct {
	// ObservationID is derived from the page identity, so a re-read of the
	// same page is the same observation rather than a second one.
	ObservationID uuid.UUID
	// TenantID scopes the observation. Nothing is stored without it.
	TenantID string
	// ConnectionID, ConnectorID and ConnectorVersion attribute the reader.
	ConnectionID     string
	ConnectorID      string
	ConnectorVersion string
	// SourceRef names the external system instance.
	SourceRef string
	// AuthorityRef names the source-authority assignment the external system's
	// statements are recorded under.
	AuthorityRef string
	// Object and SchemaVersion say what was read and under what shape.
	Object        connectivity.ObjectKind
	SchemaVersion string
	// SnapshotID pins the source version; PageSequence is the 1-based page
	// number within it.
	SnapshotID   string
	PageSequence uint64
	// StartCursor and NextCursor are the opaque tokens either side of the page.
	StartCursor string
	NextCursor  string
	// RecordCount is how many records the page carried.
	RecordCount int
	// Complete states the traversal ended at this page.
	Complete bool
	// RetrievedAt is when the page was received; Watermark is the source's own
	// high-water mark for it. They are distinct times and neither substitutes
	// for the other.
	RetrievedAt time.Time
	Watermark   time.Time
	// Freshness is the honest reliability verdict.
	Freshness Freshness
	// Classification is always EXTERNAL_OBSERVATION.
	Classification Classification
	// ContentDigest is "sha256:<hex>" over Payload.
	ContentDigest string
	// Payload is the canonical bytes of the normalized page.
	Payload []byte
	// RawArtifactRef points at separately retained raw provider bytes, when a
	// retention policy kept them. It is never the evidence itself.
	RawArtifactRef *string
}

// PageIdentity is the tuple an observation identity is derived from. Two reads
// of the same page under the same snapshot share it, which is what makes a
// re-read idempotent instead of duplicative.
type PageIdentity struct {
	TenantID     string
	ConnectionID string
	Object       connectivity.ObjectKind
	SnapshotID   string
	PageSequence uint64
}

// ObservationID returns the deterministic identity for a page.
func (p PageIdentity) ObservationID() uuid.UUID {
	name := strings.Join([]string{
		p.TenantID, p.ConnectionID, string(p.Object), p.SnapshotID,
		fmt.Sprintf("%d", p.PageSequence),
	}, "\x1f")
	return uuid.NewSHA1(observationNamespace, []byte(name))
}

// RecordOptions supplies what the page itself cannot: who read it, for whom,
// and how fresh the connector says its data may be.
type RecordOptions struct {
	TenantID        string
	Descriptor      connectivity.Descriptor
	PageSequence    uint64
	StartCursor     connectivity.Cursor
	FreshnessBudget time.Duration
	RawArtifactRef  *string
}

// Record builds an immutable observation from one page.
//
// The content digest covers the page's meaning - object, schema version,
// snapshot, cursor position and every record - and deliberately excludes the
// retrieval time. Two reads of the same page at different moments must produce
// the same digest, or "idempotent re-read" would be untestable.
func Record(page connectivity.Page, opts RecordOptions) (Observation, error) {
	const op = "observe.Record"
	if err := page.Validate(); err != nil {
		return Observation{}, err
	}
	if strings.TrimSpace(opts.TenantID) == "" {
		return Observation{}, newError(op, ErrIncomplete, "observation has no tenant")
	}
	if err := opts.Descriptor.Validate(); err != nil {
		return Observation{}, err
	}
	if opts.PageSequence == 0 {
		return Observation{}, newError(op, ErrIncomplete, "page sequence is 1-based")
	}

	payload, err := canonicalPage(page, opts)
	if err != nil {
		return Observation{}, err
	}
	startToken, err := opts.StartCursor.Token()
	if err != nil {
		return Observation{}, newError(op, ErrIncomplete, "encode start cursor: %v", err)
	}
	nextToken, err := page.NextCursor.Token()
	if err != nil {
		return Observation{}, newError(op, ErrIncomplete, "encode next cursor: %v", err)
	}

	identity := PageIdentity{
		TenantID:     opts.TenantID,
		ConnectionID: opts.Descriptor.ConnectionID,
		Object:       page.Object,
		SnapshotID:   page.SnapshotID,
		PageSequence: opts.PageSequence,
	}
	return Observation{
		ObservationID:    identity.ObservationID(),
		TenantID:         opts.TenantID,
		ConnectionID:     opts.Descriptor.ConnectionID,
		ConnectorID:      opts.Descriptor.ConnectorID,
		ConnectorVersion: opts.Descriptor.Version.String(),
		SourceRef:        opts.Descriptor.SourceRef,
		AuthorityRef:     opts.Descriptor.AuthorityRef,
		Object:           page.Object,
		SchemaVersion:    page.SchemaVersion,
		SnapshotID:       page.SnapshotID,
		PageSequence:     opts.PageSequence,
		StartCursor:      startToken,
		NextCursor:       nextToken,
		RecordCount:      len(page.Records),
		Complete:         page.Complete,
		RetrievedAt:      page.RetrievedAt.UTC(),
		Watermark:        page.Watermark.UTC(),
		Freshness:        Classify(page, opts.FreshnessBudget),
		Classification:   ClassificationExternalObservation,
		ContentDigest:    canonicalbytes.Digest(payload),
		Payload:          payload,
		RawArtifactRef:   opts.RawArtifactRef,
	}, nil
}

// canonicalPage encodes the page's material content. Field order is program
// order and every repeated group is counted, so a truncated page can never
// digest the same as a shorter complete one.
func canonicalPage(page connectivity.Page, opts RecordOptions) ([]byte, error) {
	const op = "observe.Record"
	w := canonicalbytes.New(pageSchema, pageVersion).
		String("tenant_id", opts.TenantID).
		String("connection_id", opts.Descriptor.ConnectionID).
		String("connector_id", opts.Descriptor.ConnectorID).
		String("connector_version", opts.Descriptor.Version.String()).
		String("source_ref", opts.Descriptor.SourceRef).
		String("authority_ref", opts.Descriptor.AuthorityRef).
		String("object", string(page.Object)).
		String("schema_version", page.SchemaVersion).
		String("snapshot_id", page.SnapshotID).
		Int("page_sequence", int64(opts.PageSequence)).
		String("start_cursor", opts.StartCursor.MustToken()).
		String("next_cursor", page.NextCursor.MustToken()).
		Bool("complete", page.Complete).
		Bool("partial", page.Partial).
		Count("record", len(page.Records))

	for _, rec := range page.Records {
		w.String("record.external_id", rec.ExternalID).
			String("record.sort_key", rec.SortKey).
			String("record.source_version", rec.SourceVersion).
			Int("record.observed_at_unix_nano", rec.ObservedAt.UTC().UnixNano()).
			Count("record.field", len(rec.Fields))
		for _, key := range rec.FieldKeys() {
			w.String("record.field.key", key).String("record.field.value", rec.Fields[key])
		}
	}

	raw, err := w.Bytes()
	if err != nil {
		return nil, newError(op, ErrIncomplete, "encode page: %v", err)
	}
	return raw, nil
}

// Validate reports whether the observation carries everything evidence needs.
func (o Observation) Validate() error {
	const op = "observe.Observation.Validate"
	switch {
	case o.ObservationID == uuid.Nil:
		return newError(op, ErrIncomplete, "observation has no id")
	case strings.TrimSpace(o.TenantID) == "":
		return newError(op, ErrIncomplete, "observation has no tenant")
	case strings.TrimSpace(o.ConnectionID) == "":
		return newError(op, ErrIncomplete, "observation has no connection")
	case strings.TrimSpace(o.ConnectorID) == "":
		return newError(op, ErrIncomplete, "observation has no connector")
	case strings.TrimSpace(o.ConnectorVersion) == "":
		return newError(op, ErrIncomplete, "observation has no connector version")
	case strings.TrimSpace(o.SourceRef) == "":
		return newError(op, ErrIncomplete, "observation has no source ref")
	case strings.TrimSpace(o.AuthorityRef) == "":
		return newError(op, ErrIncomplete, "observation has no authority ref")
	case !o.Object.Valid():
		return newError(op, ErrIncomplete, "observation has no object kind")
	case strings.TrimSpace(o.SchemaVersion) == "":
		return newError(op, ErrIncomplete, "observation has no schema version")
	case strings.TrimSpace(o.SnapshotID) == "":
		return newError(op, ErrIncomplete, "observation has no snapshot")
	case o.PageSequence == 0:
		return newError(op, ErrIncomplete, "observation has no page sequence")
	case strings.TrimSpace(o.StartCursor) == "":
		return newError(op, ErrIncomplete, "observation has no start cursor")
	case o.RecordCount < 0:
		return newError(op, ErrIncomplete, "observation has a negative record count")
	case o.RetrievedAt.IsZero():
		return newError(op, ErrIncomplete, "observation has no retrieval time")
	case !o.Freshness.Valid():
		return newError(op, ErrIncomplete, "observation has no freshness verdict")
	case o.Classification != ClassificationExternalObservation:
		return newError(op, ErrIncomplete,
			"observation classification %q is not %q", string(o.Classification),
			string(ClassificationExternalObservation))
	case strings.TrimSpace(o.ContentDigest) == "":
		return newError(op, ErrIncomplete, "observation has no content digest")
	case len(o.Payload) == 0:
		return newError(op, ErrIncomplete, "observation has no canonical payload")
	}
	return nil
}

// Verify recomputes the digest over the stored payload. It is the replay
// check: an observation that no longer reproduces its own digest has been
// tampered with or corrupted, and must not be read as evidence.
func (o Observation) Verify() error {
	const op = "observe.Observation.Verify"
	if err := o.Validate(); err != nil {
		return err
	}
	if got := canonicalbytes.Digest(o.Payload); got != o.ContentDigest {
		return newError(op, ErrDigestMismatch,
			"observation %s stores digest %s but its payload digests to %s",
			o.ObservationID, o.ContentDigest, got)
	}
	return nil
}

// SameContent reports whether two observations carry identical evidence. It is
// what makes an idempotent re-append distinguishable from a rewrite attempt.
func (o Observation) SameContent(other Observation) bool {
	return o.ContentDigest == other.ContentDigest &&
		o.SchemaVersion == other.SchemaVersion &&
		o.SnapshotID == other.SnapshotID &&
		o.PageSequence == other.PageSequence &&
		o.RecordCount == other.RecordCount &&
		o.Complete == other.Complete
}

// SplitDigest separates "sha256:<hex>" into its algorithm and hex halves. A
// store whose column type is a bare hex digest needs the two apart; keeping
// the split here means no adapter invents its own parsing.
func SplitDigest(digest string) (algorithm, hex string, err error) {
	const op = "observe.SplitDigest"
	algorithm, hex, found := strings.Cut(digest, ":")
	if !found || algorithm == "" || hex == "" {
		return "", "", newError(op, ErrIncomplete, "digest %q is not algorithm:hex", digest)
	}
	return algorithm, hex, nil
}

// JoinDigest is the inverse of [SplitDigest].
func JoinDigest(algorithm, hex string) string { return algorithm + ":" + hex }
