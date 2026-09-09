package evidence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// The wire projections below are what a part's bytes actually are. They are
// separate from the Go types in types.go for the same reason
// internal/data/ledger/checkpoint's manifestPayload is separate from its
// Manifest: the bytes are evidence and must not change when a Go field is
// renamed, reordered or given a friendlier type. Every instant crosses as
// UTC nanoseconds and every identifier as a string, so no encoder's zone or
// formatting choice can move a digest.
//
// None of them contains a map. A map's iteration order is unspecified, and
// encoding/json sorts map keys but not the values under them; a slice of
// structs is ordered by construction and therefore reproducible.

type headerFile struct {
	LayoutVersion        int            `json:"layout_version"`
	Tenant               string         `json:"tenant"`
	CoversFromNS         int64          `json:"covers_from_ns"`
	CoversToNS           int64          `json:"covers_to_ns"`
	SchemaReleaseVersion int64          `json:"schema_release_version"`
	SchemaReleaseDigest  string         `json:"schema_release_digest"`
	Streams              []headerStream `json:"streams"`
	Epochs               []headerEpoch  `json:"epochs"`
}

type headerStream struct {
	StreamKey      string `json:"stream_key"`
	HeadSequence   int64  `json:"head_sequence"`
	ChainHash      string `json:"chain_hash"`
	ChainAlgorithm string `json:"chain_algorithm"`
	ChainPath      string `json:"chain_path"`
	EventsPath     string `json:"events_path"`
	CoveredEvents  int    `json:"covered_events"`
}

type headerEpoch struct {
	EpochNumber  int64  `json:"epoch_number"`
	EpochID      string `json:"epoch_id"`
	CoversFromNS int64  `json:"covers_from_ns"`
	CoversToNS   int64  `json:"covers_to_ns"`
	Path         string `json:"path"`
}

type chainFile struct {
	Tenant       string            `json:"tenant"`
	StreamKey    string            `json:"stream_key"`
	HeadSequence int64             `json:"head_sequence"`
	Links        []chainLinkFile   `json:"links"`
	Digests      []eventDigestFile `json:"event_digests"`
}

type chainLinkFile struct {
	Sequence  int64  `json:"sequence"`
	EventID   string `json:"event_id"`
	PrevHash  string `json:"prev_hash"`
	ChainHash string `json:"chain_hash"`
	Algorithm string `json:"algorithm"`
}

type eventDigestFile struct {
	Sequence int64  `json:"sequence"`
	EventID  string `json:"event_id"`
	Digest   string `json:"digest"`
}

type eventsFile struct {
	Tenant    string      `json:"tenant"`
	StreamKey string      `json:"stream_key"`
	Events    []eventFile `json:"events"`
}

type eventFile struct {
	Tenant            string `json:"tenant"`
	StreamKey         string `json:"stream_key"`
	Sequence          int64  `json:"sequence"`
	EventID           string `json:"event_id"`
	AssertionClass    string `json:"assertion_class"`
	Authority         string `json:"authority_ref"`
	SourceRef         string `json:"source_ref"`
	SchemaRef         string `json:"schema_ref"`
	Payload           []byte `json:"payload"`
	ArtifactRef       string `json:"artifact_ref"`
	CanonicalLength   int    `json:"canonical_length"`
	Digest            string `json:"digest"`
	DigestAlgorithm   string `json:"digest_algorithm"`
	OccurredAtNS      int64  `json:"occurred_at_ns"`
	EffectiveAtNS     int64  `json:"effective_at_ns"`
	RecordedAtNS      int64  `json:"recorded_at_ns"`
	CorrelationID     string `json:"correlation_id"`
	CausationID       string `json:"causation_id"`
	IdempotencyKey    string `json:"idempotency_key"`
	CorrectsStreamKey string `json:"corrects_stream_key"`
	CorrectsSequence  int64  `json:"corrects_sequence"`
}

type manifestFile struct {
	SchemaVersion        int        `json:"schema_version"`
	LayoutVersion        int        `json:"layout_version"`
	Tenant               string     `json:"tenant"`
	CoversFromNS         int64      `json:"covers_from_ns"`
	CoversToNS           int64      `json:"covers_to_ns"`
	SchemaReleaseVersion int64      `json:"schema_release_version"`
	SchemaReleaseDigest  string     `json:"schema_release_digest"`
	Parts                []partFile `json:"parts"`
	Digest               string     `json:"digest"`
	DigestAlgorithm      string     `json:"digest_algorithm"`
}

type partFile struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Length    int    `json:"length"`
	Digest    string `json:"digest"`
	Algorithm string `json:"algorithm"`
}

// encodePart marshals one projection into the bytes that go into the
// package. A trailing newline is appended so a part is a well-formed text
// line-oriented file when a human opens it; it is part of the digested bytes
// like everything else.
func encodePart(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("evidence: encode part: %w", err)
	}
	return append(body, '\n'), nil
}

func decodePart(path string, raw []byte, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return ErrPackageMalformed{Path: path, Reason: err.Error()}
	}
	return nil
}

func nanos(t time.Time) int64 { return Truncate(t).UnixNano() }

func fromNanos(ns int64) time.Time { return time.Unix(0, ns).UTC() }

// uuidText renders an identifier, and the empty string for the nil UUID, so
// "absent" and "the all-zero identifier" are the same absence in the bytes
// rather than two spellings of it.
func uuidText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

type uuidTextCache map[uuid.UUID]string

func (c uuidTextCache) text(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	if text, ok := c[id]; ok {
		return text
	}
	text := id.String()
	c[id] = text
	return text
}

func parseUUID(path, field, text string) (uuid.UUID, error) {
	if text == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, ErrPackageMalformed{Path: path, Reason: fmt.Sprintf("%s is not a uuid: %v", field, err)}
	}
	return id, nil
}

func projectEvent(e Event, tenantText string, cache uuidTextCache) eventFile {
	out := eventFile{
		Tenant:          tenantText,
		StreamKey:       e.StreamKey,
		Sequence:        e.Sequence,
		EventID:         cache.text(e.EventID),
		AssertionClass:  string(e.AssertionClass),
		Authority:       e.Authority,
		SourceRef:       e.SourceRef,
		SchemaRef:       e.SchemaRef,
		Payload:         e.Payload,
		ArtifactRef:     e.ArtifactRef,
		CanonicalLength: e.CanonicalLength,
		Digest:          e.Digest,
		DigestAlgorithm: e.DigestAlgorithm,
		OccurredAtNS:    nanos(e.OccurredAt),
		EffectiveAtNS:   nanos(e.EffectiveAt),
		RecordedAtNS:    nanos(e.RecordedAt),
		CorrelationID:   cache.text(e.CorrelationID),
		CausationID:     cache.text(e.CausationID),
		IdempotencyKey:  e.IdempotencyKey,
	}
	if e.Corrects != nil {
		out.CorrectsStreamKey = e.Corrects.StreamKey
		out.CorrectsSequence = e.Corrects.Sequence
	}
	return out
}

func restoreEvent(path string, f eventFile) (Event, error) {
	tenant, err := parseUUID(path, "tenant", f.Tenant)
	if err != nil {
		return Event{}, err
	}
	eventID, err := parseUUID(path, "event_id", f.EventID)
	if err != nil {
		return Event{}, err
	}
	correlation, err := parseUUID(path, "correlation_id", f.CorrelationID)
	if err != nil {
		return Event{}, err
	}
	causation, err := parseUUID(path, "causation_id", f.CausationID)
	if err != nil {
		return Event{}, err
	}
	out := Event{
		Tenant:          tenant,
		StreamKey:       f.StreamKey,
		Sequence:        f.Sequence,
		EventID:         eventID,
		AssertionClass:  datalogger.AssertionClass(f.AssertionClass),
		Authority:       f.Authority,
		SourceRef:       f.SourceRef,
		SchemaRef:       f.SchemaRef,
		Payload:         f.Payload,
		ArtifactRef:     f.ArtifactRef,
		CanonicalLength: f.CanonicalLength,
		Digest:          f.Digest,
		DigestAlgorithm: f.DigestAlgorithm,
		OccurredAt:      fromNanos(f.OccurredAtNS),
		EffectiveAt:     fromNanos(f.EffectiveAtNS),
		RecordedAt:      fromNanos(f.RecordedAtNS),
		CorrelationID:   correlation,
		CausationID:     causation,
		IdempotencyKey:  f.IdempotencyKey,
	}
	if f.CorrectsStreamKey != "" || f.CorrectsSequence != 0 {
		out.Corrects = &EventRef{StreamKey: f.CorrectsStreamKey, Sequence: f.CorrectsSequence}
	}
	return out, nil
}

func projectChain(tenant uuid.UUID, s Stream, cache uuidTextCache) chainFile {
	out := chainFile{
		Tenant:       cache.text(tenant),
		StreamKey:    s.StreamKey,
		HeadSequence: s.Head.Sequence,
		Links:        make([]chainLinkFile, 0, len(s.Links)),
		Digests:      make([]eventDigestFile, 0, len(s.Digests)),
	}
	for _, link := range s.Links {
		out.Links = append(out.Links, chainLinkFile{
			Sequence:  link.Sequence,
			EventID:   cache.text(link.EventID),
			PrevHash:  link.PrevHash,
			ChainHash: link.ChainHash,
			Algorithm: link.Algorithm,
		})
	}
	for _, d := range s.Digests {
		out.Digests = append(out.Digests, eventDigestFile{
			Sequence: d.Sequence, EventID: cache.text(d.EventID), Digest: d.Digest,
		})
	}
	return out
}

// restoreChain rebuilds the links and digests a chain part carries. The
// stream key is taken from the part itself rather than from the header, so a
// link that names a different stream is visible to
// [hashchain.Digester.VerifyLinks] instead of being silently relabelled.
func restoreChain(path string, f chainFile) (links []ChainLink, digests []EventDigest, tenant uuid.UUID, err error) {
	tenant, err = parseUUID(path, "tenant", f.Tenant)
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	for _, l := range f.Links {
		id, idErr := parseUUID(path, "links.event_id", l.EventID)
		if idErr != nil {
			return nil, nil, uuid.Nil, idErr
		}
		links = append(links, ChainLink{
			StreamKey: f.StreamKey, Sequence: l.Sequence, EventID: id,
			PrevHash: l.PrevHash, ChainHash: l.ChainHash, Algorithm: l.Algorithm,
		})
	}
	for _, d := range f.Digests {
		id, idErr := parseUUID(path, "event_digests.event_id", d.EventID)
		if idErr != nil {
			return nil, nil, uuid.Nil, idErr
		}
		digests = append(digests, EventDigest{Sequence: d.Sequence, EventID: id, Digest: d.Digest})
	}
	return links, digests, tenant, nil
}

// encodeEpoch writes a signed checkpoint manifest into the package.
//
// It marshals internal/data/ledger/checkpoint.Manifest directly rather than
// re-projecting it field by field. A re-projection here would be a second,
// driftable definition of a signed structure this package does not own, and
// it would buy nothing: what makes an epoch part trustworthy is not its JSON
// shape but the epoch's own canonical digest and Ed25519 signature, which
// [checkpoint.Verify] recomputes from the decoded value. All this encoding
// has to be is reproducible, which a struct - never a map - is.
func encodeEpoch(m Epoch) ([]byte, error) {
	return encodePart(normalizeEpoch(m))
}

func decodeEpoch(path string, raw []byte) (Epoch, error) {
	var m Epoch
	if err := decodePart(path, raw, &m); err != nil {
		return Epoch{}, err
	}
	return normalizeEpoch(m), nil
}

// normalizeEpoch puts every instant back into UTC at storage precision. A
// signed manifest's canonical digest hashes its instants; a copy that came
// back from JSON in another zone would digest identically only by luck.
func normalizeEpoch(m Epoch) Epoch {
	m.CoversFrom = Truncate(m.CoversFrom)
	m.CoversTo = Truncate(m.CoversTo)
	m.CreatedAt = Truncate(m.CreatedAt)
	if m.Signature != nil {
		sig := *m.Signature
		sig.SignedAt = Truncate(sig.SignedAt)
		m.Signature = &sig
	}
	return m
}
