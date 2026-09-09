package connectivity

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ObjectKind names an external record family this plane can observe. P1A
// observes three; the list is closed so that a mapping profile, a schema
// snapshot and an observation all agree on the same vocabulary.
type ObjectKind string

// The observable object kinds.
const (
	// ObjectWorker is a person's employment record at the incumbent.
	ObjectWorker ObjectKind = "WORKER"
	// ObjectPosition is a seat in the incumbent's org structure.
	ObjectPosition ObjectKind = "POSITION"
	// ObjectCompensation is a pay record or band assignment.
	ObjectCompensation ObjectKind = "COMPENSATION"
)

// ObjectKinds returns every declared kind in canonical order.
func ObjectKinds() []ObjectKind {
	return []ObjectKind{ObjectWorker, ObjectPosition, ObjectCompensation}
}

// Valid reports whether k is one of the declared kinds.
func (k ObjectKind) Valid() bool {
	switch k {
	case ObjectWorker, ObjectPosition, ObjectCompensation:
		return true
	default:
		return false
	}
}

func (k ObjectKind) String() string { return string(k) }

// ReadMode selects how much of the source a run intends to traverse. All three
// modes share one checkpoint contract, which is why the mode is a field on the
// request rather than three separate methods.
type ReadMode string

// The read modes.
const (
	// ReadFull traverses every record in the pinned snapshot.
	ReadFull ReadMode = "FULL"
	// ReadIncremental traverses records changed at or after Since.
	ReadIncremental ReadMode = "INCREMENTAL"
	// ReadDelta traverses the provider's own change feed since the cursor.
	ReadDelta ReadMode = "DELTA"
)

// Valid reports whether m is one of the declared modes.
func (m ReadMode) Valid() bool {
	switch m {
	case ReadFull, ReadIncremental, ReadDelta:
		return true
	default:
		return false
	}
}

func (m ReadMode) String() string { return string(m) }

// Bounds is the negotiated read envelope. A connector publishes it, a
// connection may only narrow it, and a request that exceeds it fails with
// [ErrBounds] rather than being silently clamped: silently returning fewer
// records than asked for is how a "complete" observation quietly becomes
// partial.
type Bounds struct {
	// MaxPageSize is the largest number of records one Read may return.
	MaxPageSize int
	// MaxPagesPerRun caps how many pages one observation run may consume.
	MaxPagesPerRun int
	// MaxRecordsPerRun caps the total records one observation run may consume.
	MaxRecordsPerRun int
	// MaxRecordBytes caps one record's canonical size.
	MaxRecordBytes int
	// MinRequestInterval is the smallest gap between two reads on one
	// connection. Zero means the provider declares no floor.
	MinRequestInterval time.Duration
}

// Validate reports whether the bounds are internally coherent.
func (b Bounds) Validate() error {
	const op = "connectivity.Bounds.Validate"
	switch {
	case b.MaxPageSize <= 0:
		return newError(op, ErrInvalid, "max page size must be positive")
	case b.MaxPagesPerRun <= 0:
		return newError(op, ErrInvalid, "max pages per run must be positive")
	case b.MaxRecordsPerRun <= 0:
		return newError(op, ErrInvalid, "max records per run must be positive")
	case b.MaxRecordBytes <= 0:
		return newError(op, ErrInvalid, "max record bytes must be positive")
	case b.MinRequestInterval < 0:
		return newError(op, ErrInvalid, "min request interval cannot be negative")
	case b.MaxRecordsPerRun < b.MaxPageSize:
		return newError(op, ErrInvalid, "max records per run (%d) is below one page (%d)",
			b.MaxRecordsPerRun, b.MaxPageSize)
	}
	return nil
}

// Narrows reports whether b is no looser than outer on every axis. A
// connection's bounds must narrow its definition's, never widen them.
func (b Bounds) Narrows(outer Bounds) bool {
	return b.MaxPageSize <= outer.MaxPageSize &&
		b.MaxPagesPerRun <= outer.MaxPagesPerRun &&
		b.MaxRecordsPerRun <= outer.MaxRecordsPerRun &&
		b.MaxRecordBytes <= outer.MaxRecordBytes &&
		b.MinRequestInterval >= outer.MinRequestInterval
}

// Cursor is an opaque continuation token in structured form.
//
// It pins the source snapshot the page sequence is drawn from and the exact
// tie-break position within it. Ordering is by (SortKey, ExternalID): a sort
// key alone is not unique in any real HRIS, and an unstable tie-break is how a
// paginated read silently skips or repeats a record.
type Cursor struct {
	// SnapshotID pins the source version the whole page sequence reads.
	SnapshotID string
	// LastSortKey is the sort key of the last record already consumed.
	LastSortKey string
	// LastExternalID breaks ties within an equal sort key.
	LastExternalID string
	// Page is the number of pages already consumed under this snapshot.
	Page uint64
	// ExpiresAt is when the provider stops honouring this token. Zero means
	// the provider declares no expiry.
	ExpiresAt time.Time
}

// StartCursor returns the cursor that begins a fresh traversal of snapshot.
func StartCursor(snapshotID string) Cursor {
	return Cursor{SnapshotID: snapshotID}
}

// IsStart reports whether c addresses the beginning of its snapshot.
func (c Cursor) IsStart() bool {
	return c.Page == 0 && c.LastSortKey == "" && c.LastExternalID == ""
}

// Expired reports whether c is past its expiry as at now.
func (c Cursor) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}

const (
	cursorSchema      = "hcmnext.connectivity.Cursor"
	cursorVersion     = 1
	cursorTokenPrefix = "cur1"
	cursorCheckHex    = 16
)

// Canonical returns the cursor's canonical bytes. Two cursors with the same
// meaning produce the same bytes on every machine, which is what lets an
// observation digest bind the position it was read from.
func (c Cursor) Canonical() ([]byte, error) {
	w := canonicalbytes.New(cursorSchema, cursorVersion).
		String("snapshot_id", c.SnapshotID).
		String("last_sort_key", c.LastSortKey).
		String("last_external_id", c.LastExternalID).
		Int("page", int64(c.Page)).
		Bool("has_expiry", !c.ExpiresAt.IsZero())
	if !c.ExpiresAt.IsZero() {
		w = w.Int("expires_at_unix_nano", c.ExpiresAt.UTC().UnixNano())
	}
	return w.Bytes()
}

// Token encodes the cursor as the opaque string a caller round-trips. The
// encoding carries an integrity check so a truncated or edited token is
// rejected as [ErrCursor] rather than silently addressing a different
// position.
func (c Cursor) Token() (string, error) {
	const op = "connectivity.Cursor.Token"
	raw, err := c.Canonical()
	if err != nil {
		return "", newError(op, ErrInvalid, "encode cursor: %v", err)
	}
	digest := canonicalbytes.Digest(raw)
	check := strings.TrimPrefix(digest, canonicalbytes.DigestAlgorithm+":")
	return cursorTokenPrefix + "." +
		base64.RawURLEncoding.EncodeToString(raw) + "." +
		check[:cursorCheckHex], nil
}

// MustToken is Token for a cursor known to be encodable. It is used where a
// cursor was just constructed from validated parts.
func (c Cursor) MustToken() string {
	token, err := c.Token()
	if err != nil {
		return ""
	}
	return token
}

// ParseCursor decodes a token produced by [Cursor.Token]. An empty token
// decodes to the zero cursor, which addresses the start of an unpinned
// snapshot.
func ParseCursor(token string) (Cursor, error) {
	const op = "connectivity.ParseCursor"
	if token == "" {
		return Cursor{}, nil
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != cursorTokenPrefix {
		return Cursor{}, newError(op, ErrCursor, "token is not a %s cursor", cursorTokenPrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Cursor{}, newError(op, ErrCursor, "token body is not base64url")
	}
	check := strings.TrimPrefix(canonicalbytes.Digest(raw), canonicalbytes.DigestAlgorithm+":")
	if len(parts[2]) != cursorCheckHex || check[:cursorCheckHex] != parts[2] {
		return Cursor{}, newError(op, ErrCursor, "token integrity check failed")
	}
	cursor, err := decodeCursorBody(raw)
	if err != nil {
		return Cursor{}, newError(op, ErrCursor, "token body is malformed")
	}
	return cursor, nil
}

// Record is one observed external record in normalized form.
//
// Fields is deliberately map[string]string rather than a typed struct: this
// plane observes, it does not interpret. Turning these strings into domain
// values is the mapping profile's job (INTG-006), downstream of here.
type Record struct {
	// ExternalID is the provider's own identifier, unique within the object.
	ExternalID string
	// SortKey is the provider-stable ordering key; equal keys tie-break on
	// ExternalID.
	SortKey string
	// SourceVersion is the provider's version/etag for this record.
	SourceVersion string
	// ObservedAt is when the provider says the record was last changed.
	ObservedAt time.Time
	// Fields are the record's normalized values.
	Fields map[string]string
}

// Validate reports whether the record carries the identity an observation
// needs to be attributable.
func (r Record) Validate() error {
	const op = "connectivity.Record.Validate"
	switch {
	case strings.TrimSpace(r.ExternalID) == "":
		return newError(op, ErrInvalid, "record has no external id")
	case strings.TrimSpace(r.SortKey) == "":
		return newError(op, ErrInvalid, "record %q has no sort key", r.ExternalID)
	case strings.TrimSpace(r.SourceVersion) == "":
		return newError(op, ErrInvalid, "record %q has no source version", r.ExternalID)
	case r.ObservedAt.IsZero():
		return newError(op, ErrInvalid, "record %q has no observed time", r.ExternalID)
	}
	return nil
}

// FieldKeys returns the record's field names in ascending order.
func (r Record) FieldKeys() []string {
	keys := make([]string, 0, len(r.Fields))
	for k := range r.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ReadRequest is one bounded page request. It has no field that could express
// a change, and no free-form body a connector could reinterpret as a command.
type ReadRequest struct {
	// Object selects the record family.
	Object ObjectKind
	// Mode selects full, incremental or delta traversal.
	Mode ReadMode
	// Cursor is the continuation position. The zero cursor starts a traversal.
	Cursor Cursor
	// Limit is the requested page size. It must be positive and within the
	// connector's MaxPageSize.
	Limit int
	// Since bounds an incremental read. It is ignored for other modes.
	Since time.Time
}

// Validate checks the request against bounds before any external call is made.
func (r ReadRequest) Validate(bounds Bounds) error {
	const op = "connectivity.ReadRequest.Validate"
	if !r.Object.Valid() {
		return newError(op, ErrInvalid, "unknown object kind %q", string(r.Object))
	}
	if !r.Mode.Valid() {
		return newError(op, ErrInvalid, "unknown read mode %q", string(r.Mode))
	}
	if r.Limit <= 0 {
		return newError(op, ErrInvalid, "limit must be positive, got %d", r.Limit)
	}
	if r.Limit > bounds.MaxPageSize {
		return newError(op, ErrBounds, "limit %d exceeds max page size %d", r.Limit, bounds.MaxPageSize)
	}
	if r.Mode == ReadIncremental && r.Since.IsZero() {
		return newError(op, ErrInvalid, "incremental read requires a since instant")
	}
	return nil
}

// Page is one bounded response. Complete is the connector's own statement that
// the traversal is finished; a caller must not infer completeness from an
// empty record slice, because a throttled provider can legitimately return
// zero records and more pages.
type Page struct {
	// Object echoes the requested object kind.
	Object ObjectKind
	// SchemaVersion is the external schema version this page was read under.
	// It changing mid-traversal is schema drift, not a new page.
	SchemaVersion string
	// SnapshotID is the source snapshot the page was drawn from.
	SnapshotID string
	// Records are the observed records, ordered by (SortKey, ExternalID).
	Records []Record
	// NextCursor addresses the position after the last record.
	NextCursor Cursor
	// Complete states that no further page exists under this snapshot.
	Complete bool
	// Partial states that the provider returned less than it was asked for
	// because of its own limits, not because the traversal ended.
	Partial bool
	// RetrievedAt is when the connector received the page.
	RetrievedAt time.Time
	// Watermark is the provider's own high-water mark for this page, used to
	// judge freshness downstream.
	Watermark time.Time
}

// Validate checks the page's internal consistency: ordering, tie-break
// stability, record identity and cursor agreement.
func (p Page) Validate() error {
	const op = "connectivity.Page.Validate"
	if !p.Object.Valid() {
		return newError(op, ErrInvalid, "page has unknown object kind %q", string(p.Object))
	}
	if strings.TrimSpace(p.SchemaVersion) == "" {
		return newError(op, ErrSchema, "page carries no schema version")
	}
	if strings.TrimSpace(p.SnapshotID) == "" {
		return newError(op, ErrInvalid, "page carries no snapshot id")
	}
	if p.RetrievedAt.IsZero() {
		return newError(op, ErrInvalid, "page carries no retrieval time")
	}
	if !p.Complete && p.NextCursor.SnapshotID != p.SnapshotID {
		return newError(op, ErrCursor,
			"next cursor snapshot %q does not match page snapshot %q",
			p.NextCursor.SnapshotID, p.SnapshotID)
	}
	var prevSort, prevID string
	for i, rec := range p.Records {
		if err := rec.Validate(); err != nil {
			return err
		}
		if i > 0 && !ascending(prevSort, prevID, rec.SortKey, rec.ExternalID) {
			return newError(op, ErrInvalid,
				"page ordering is unstable at index %d: (%q,%q) does not follow (%q,%q)",
				i, rec.SortKey, rec.ExternalID, prevSort, prevID)
		}
		prevSort, prevID = rec.SortKey, rec.ExternalID
	}
	return nil
}

// ascending reports whether (sortB, idB) strictly follows (sortA, idA).
func ascending(sortA, idA, sortB, idB string) bool {
	if sortA != sortB {
		return sortA < sortB
	}
	return idA < idB
}

// Descriptor identifies the connector version and the external system instance
// behind one connector value. It is what an observation records as its source.
type Descriptor struct {
	// ConnectorID is the definition identity.
	ConnectorID string
	// Version is the definition version.
	Version Version
	// ConnectionID is the tenant-scoped connection this connector serves.
	ConnectionID string
	// SourceRef names the external system instance, e.g.
	// "workday://harborcare/prod".
	SourceRef string
	// AuthorityRef names the source-authority assignment under which the
	// external system's statements are recorded. An observation without it
	// cannot be attributed, so it is required here rather than optional.
	AuthorityRef string
}

// Validate reports whether the descriptor can attribute an observation.
func (d Descriptor) Validate() error {
	const op = "connectivity.Descriptor.Validate"
	switch {
	case strings.TrimSpace(d.ConnectorID) == "":
		return newError(op, ErrInvalid, "descriptor has no connector id")
	case strings.TrimSpace(d.ConnectionID) == "":
		return newError(op, ErrInvalid, "descriptor has no connection id")
	case strings.TrimSpace(d.SourceRef) == "":
		return newError(op, ErrInvalid, "descriptor has no source ref")
	case strings.TrimSpace(d.AuthorityRef) == "":
		return newError(op, ErrInvalid, "descriptor has no authority ref")
	case d.Version.IsZero():
		return newError(op, ErrInvalid, "descriptor has no connector version")
	}
	return nil
}

// Connector is the whole external surface of the P1A connectivity plane.
//
// Every method is a question. There is no method that states, sends, applies
// or commits anything, and no method hands back an object that could: Page,
// Descriptor and Bounds are plain values. This is the read-only claim of the
// release expressed as a type, and a test reflects over this interface to keep
// it that way.
//
// Implementations must be safe for concurrent use: one connection is read by
// one observation run at a time, but health probes and schema reads run
// alongside it.
type Connector interface {
	// Descriptor identifies the connector version and external system.
	Descriptor() Descriptor

	// Bounds reports the read envelope this connector honours.
	Bounds() Bounds

	// Capabilities reports the object/operation pairs this connector serves.
	Capabilities() []Capability

	// SchemaVersion reports the external schema version currently serving the
	// object. A change between two reads of one traversal is schema drift.
	SchemaVersion(ctx context.Context, object ObjectKind) (string, error)

	// Snapshot pins a source snapshot for a traversal and returns its id. Two
	// calls may return different ids; one traversal uses exactly one.
	Snapshot(ctx context.Context, object ObjectKind) (string, error)

	// Read returns one bounded page. It never mutates the external system.
	Read(ctx context.Context, req ReadRequest) (Page, error)
}

// errCursorBody reports a structurally invalid cursor body. It never reaches a
// caller unwrapped; ParseCursor reclassifies it as [ErrCursor].
var errCursorBody = errors.New("connectivity: malformed cursor body")

// decodeCursorBody parses the canonical byte stream [Cursor.Canonical]
// produces. It is the only reader of that framing, so the two stay together.
func decodeCursorBody(raw []byte) (Cursor, error) {
	fields, err := decodeCanonicalFields(raw)
	if err != nil {
		return Cursor{}, err
	}
	if string(fields["$schema"]) != cursorSchema {
		return Cursor{}, errCursorBody
	}
	page, err := decodeZigZag(fields["page"])
	if err != nil {
		return Cursor{}, err
	}
	if page < 0 {
		return Cursor{}, errCursorBody
	}
	cursor := Cursor{
		SnapshotID:     string(fields["snapshot_id"]),
		LastSortKey:    string(fields["last_sort_key"]),
		LastExternalID: string(fields["last_external_id"]),
		Page:           uint64(page),
	}
	if hasExpiry, ok := fields["has_expiry"]; ok && len(hasExpiry) == 1 && hasExpiry[0] == 1 {
		nanos, expErr := decodeZigZag(fields["expires_at_unix_nano"])
		if expErr != nil {
			return Cursor{}, expErr
		}
		cursor.ExpiresAt = time.Unix(0, nanos).UTC()
	}
	return cursor, nil
}

// decodeCanonicalFields reads the tag/length framing canonicalbytes writes.
// Repeated tags keep their last value; the cursor stream has none.
func decodeCanonicalFields(raw []byte) (map[string][]byte, error) {
	fields := make(map[string][]byte, 8)
	for len(raw) > 0 {
		tagLen, n := binary.Uvarint(raw)
		if n <= 0 || uint64(len(raw)-n) < tagLen {
			return nil, errCursorBody
		}
		raw = raw[n:]
		tag := string(raw[:tagLen])
		raw = raw[tagLen:]

		payloadLen, n := binary.Uvarint(raw)
		if n <= 0 || uint64(len(raw)-n) < payloadLen {
			return nil, errCursorBody
		}
		raw = raw[n:]
		fields[tag] = raw[:payloadLen]
		raw = raw[payloadLen:]
	}
	return fields, nil
}

// decodeZigZag reverses the zigzag uvarint encoding canonicalbytes.Int writes.
func decodeZigZag(payload []byte) (int64, error) {
	if len(payload) == 0 {
		return 0, errCursorBody
	}
	raw, n := binary.Uvarint(payload)
	if n != len(payload) {
		return 0, errCursorBody
	}
	return int64(raw>>1) ^ -int64(raw&1), nil
}
