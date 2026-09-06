package spi

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// Adapter is the whole external surface a connector implementation compiles
// against.
//
// Every method is a question, mirroring the parent [connectivity.Connector]:
// there is no method that states, sends, applies or commits anything, and no
// method hands back a value that could. A generated SDK built from
// [AdapterManifest] never emits more than these four methods, so an adapter
// implementation cannot grow a fifth "just this once" write path without
// leaving this interface entirely - which the conformance kit in spiconform
// checks by reflecting over the method set.
//
// Implementations must be safe for concurrent use: Probe and Describe may run
// alongside an in-flight ReadSnapshot or ObserveChanges.
type Adapter interface {
	// Describe returns the adapter build's self-declared manifest. Two calls
	// against an unchanged build return manifests with identical digests.
	Describe(ctx context.Context) (AdapterManifest, error)

	// Probe reports whether the adapter can currently reach its external
	// system, without reading or changing any object.
	Probe(ctx context.Context) (ProbeResult, error)

	// ReadSnapshot returns one bounded page of a pinned source snapshot. It
	// never mutates the external system.
	ReadSnapshot(ctx context.Context, req connectivity.ReadRequest) (Snapshot, error)

	// ObserveChanges returns a bounded set of external changes since a
	// watermark. It never mutates the external system.
	ObserveChanges(ctx context.Context, req ObserveRequest) (ChangeSet, error)
}

// Health is the coarse status a [ProbeResult] carries.
type Health string

// The declared health states.
const (
	HealthHealthy     Health = "HEALTHY"
	HealthDegraded    Health = "DEGRADED"
	HealthUnreachable Health = "UNREACHABLE"
)

// Valid reports whether h is one of the declared health states.
func (h Health) Valid() bool {
	switch h {
	case HealthHealthy, HealthDegraded, HealthUnreachable:
		return true
	default:
		return false
	}
}

// ProbeResult is the outcome of one read-only reachability probe.
type ProbeResult struct {
	// Health is the coarse status the probe observed.
	Health Health
	// CheckedAt is when the probe ran.
	CheckedAt time.Time
	// Latency is how long the probe took. It may be zero for a synthetic or
	// cached result.
	Latency time.Duration
	// Detail is a non-secret explanation, e.g. a classified failure summary.
	Detail string
}

// Validate reports whether the probe result is well-formed.
func (p ProbeResult) Validate() error {
	const op = "spi.ProbeResult.Validate"
	switch {
	case !p.Health.Valid():
		return connectivity.Fail(op, connectivity.ErrInvalid, "probe result has no health status")
	case p.CheckedAt.IsZero():
		return connectivity.Fail(op, connectivity.ErrInvalid, "probe result carries no check time")
	case p.Latency < 0:
		return connectivity.Fail(op, connectivity.ErrInvalid, "probe latency cannot be negative")
	}
	return nil
}

// Snapshot is one bounded page together with the digest that makes its
// content independently verifiable.
type Snapshot struct {
	Page   connectivity.Page
	Digest string
}

// Validate reports whether the snapshot's page is well-formed and its digest
// is present and matches the page's own content.
func (s Snapshot) Validate() error {
	const op = "spi.Snapshot.Validate"
	if err := s.Page.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Digest) == "" {
		return connectivity.Fail(op, connectivity.ErrInvalid, "snapshot carries no digest")
	}
	want, err := PageDigest(s.Page)
	if err != nil {
		return err
	}
	if want != s.Digest {
		return connectivity.Fail(op, connectivity.ErrSchema, "snapshot digest %q does not match its own page content (want %q)", s.Digest, want)
	}
	return nil
}

// ObserveRequest is one bounded change-observation request. It has no field
// that could express a command; Since and Limit only narrow what is read.
type ObserveRequest struct {
	// Object selects the record family.
	Object ObjectKind
	// Since bounds the change window: only changes at or after this instant
	// are eligible.
	Since time.Time
	// Limit is the requested maximum number of changes. It must be positive
	// and within the adapter's declared bounds.
	Limit int
}

// Validate checks the request against bounds before any external call is
// made.
func (r ObserveRequest) Validate(bounds Bounds) error {
	const op = "spi.ObserveRequest.Validate"
	switch {
	case !r.Object.Valid():
		return connectivity.Fail(op, connectivity.ErrInvalid, "unknown object kind %q", string(r.Object))
	case r.Since.IsZero():
		return connectivity.Fail(op, connectivity.ErrInvalid, "observe request requires a since instant")
	case r.Limit <= 0:
		return connectivity.Fail(op, connectivity.ErrInvalid, "limit must be positive, got %d", r.Limit)
	case r.Limit > bounds.MaxPageSize:
		return connectivity.Fail(op, connectivity.ErrBounds, "limit %d exceeds max page size %d", r.Limit, bounds.MaxPageSize)
	}
	return nil
}

// ChangeSet is one bounded response to an [ObserveRequest].
type ChangeSet struct {
	// Object echoes the requested object kind.
	Object ObjectKind
	// Changed are the observed changes, ordered by (SortKey, ExternalID).
	Changed []connectivity.Record
	// Watermark is the provider's own high-water mark for this response.
	Watermark time.Time
	// Complete states that no further change exists at or before Watermark.
	// A caller must not infer completeness from an empty Changed slice.
	Complete bool
	// RetrievedAt is when the adapter received the response.
	RetrievedAt time.Time
}

// Validate reports whether the change set is internally consistent.
func (c ChangeSet) Validate() error {
	const op = "spi.ChangeSet.Validate"
	if !c.Object.Valid() {
		return connectivity.Fail(op, connectivity.ErrInvalid, "change set has unknown object kind %q", string(c.Object))
	}
	if c.RetrievedAt.IsZero() {
		return connectivity.Fail(op, connectivity.ErrInvalid, "change set carries no retrieval time")
	}
	for _, r := range c.Changed {
		if err := r.Validate(); err != nil {
			return err
		}
	}
	return nil
}

const (
	pageDigestSchema  = "hcmnext.connectivity.spi.Snapshot"
	pageDigestVersion = 1
)

// PageDigest returns the deterministic content digest of a validated page.
// Two pages with the same object, schema version, snapshot, records (in
// order), continuation position and completeness flags digest identically on
// every machine; that equality is the conformance kit's proof that a read is
// idempotent, not a byte-for-byte comparison of the whole page.
func PageDigest(p connectivity.Page) (string, error) {
	const op = "spi.PageDigest"
	if err := p.Validate(); err != nil {
		return "", err
	}
	cursorToken, err := p.NextCursor.Token()
	if err != nil {
		return "", connectivity.Fail(op, connectivity.ErrCursor, "encode next cursor: %v", err)
	}
	w := canonicalbytes.New(pageDigestSchema, pageDigestVersion).
		String("object", string(p.Object)).
		String("schema_version", p.SchemaVersion).
		String("snapshot_id", p.SnapshotID).
		String("records", recordsString(p.Records)).
		String("next_cursor", cursorToken).
		Bool("complete", p.Complete).
		Bool("partial", p.Partial).
		Int("watermark_unix_nano", p.Watermark.UTC().UnixNano())
	raw, err := w.Bytes()
	if err != nil {
		return "", connectivity.Fail(op, connectivity.ErrInvalid, "encode page: %v", err)
	}
	return canonicalbytes.Digest(raw), nil
}

// recordsString renders records to one order-sensitive string. Field order
// within a record is sorted (a record's fields are a map); record order
// itself is preserved exactly as given, because page order is part of the
// contract [connectivity.Page.Validate] already enforces.
func recordsString(records []connectivity.Record) string {
	parts := make([]string, len(records))
	for i, r := range records {
		var fields strings.Builder
		for j, k := range r.FieldKeys() {
			if j > 0 {
				fields.WriteByte('\x1e')
			}
			fields.WriteString(k)
			fields.WriteByte('=')
			fields.WriteString(r.Fields[k])
		}
		parts[i] = strings.Join([]string{
			r.ExternalID,
			r.SortKey,
			r.SourceVersion,
			r.ObservedAt.UTC().Format(time.RFC3339Nano),
			fields.String(),
		}, "\x1f")
	}
	return strings.Join(parts, "\x1d")
}

// CheckContext preserves cancellation identity for adapters and generated
// SDKs, mirroring the parent package's own request validation style: a
// caller's own defect (a nil or already-canceled context) is reported the
// same way every adapter reports it, rather than each adapter inventing its
// own wording.
func CheckContext(ctx context.Context) error {
	const op = "spi.CheckContext"
	if ctx == nil {
		return connectivity.Fail(op, connectivity.ErrInvalid, "context is required")
	}
	select {
	case <-ctx.Done():
		return connectivity.Fail(op, connectivity.ErrTransient, "context ended: %v", ctx.Err())
	default:
		return nil
	}
}
