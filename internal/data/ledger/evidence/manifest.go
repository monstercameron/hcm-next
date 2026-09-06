package evidence

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// DigestProfile names what the manifest digest is a digest of. It is bound
// into the preimage so a manifest digest can never be mistaken for, or
// substituted with, any other sha256 value this platform computes.
const DigestProfile = "hcmnext.canonical.LEDGER_EVIDENCE_MANIFEST.v1"

// folder builds a digest over length-framed fields. Every field is preceded
// by its 64-bit big-endian length, the same discipline
// internal/data/ledger.SHA256Digester, internal/data/ledger/hashchain and
// internal/data/ledger/checkpoint.ComputeRootDigest use: without it, moving a
// character across a field boundary would leave the preimage unchanged.
type folder struct {
	h interface{ Write([]byte) (int, error) }
}

func newFolder() (*folder, func() string) {
	h := sha256.New()
	return &folder{h: h}, func() string { return hex.EncodeToString(h.Sum(nil)) }
}

func (f *folder) bytes(b []byte) {
	var frame [8]byte
	binary.BigEndian.PutUint64(frame[:], uint64(len(b)))
	_, _ = f.h.Write(frame[:])
	_, _ = f.h.Write(b)
}

func (f *folder) text(s string) { f.bytes([]byte(s)) }

func (f *folder) number(n int64) {
	var v [8]byte
	binary.BigEndian.PutUint64(v[:], uint64(n))
	f.bytes(v[:])
}

// ComputeDigest folds a manifest's coverage claim and every one of its part
// rows into one value. Parts are folded in path order, so the digest is a
// function of the package's content and not of the order the parts happened
// to be assembled in.
//
// The manifest's own Digest field is not folded: a digest can never cover its
// own bytes.
func ComputeDigest(m Manifest) (string, error) {
	parts := orderedParts(m.Parts)
	f, sum := newFolder()
	f.text(DigestProfile)
	f.number(int64(m.SchemaVersion))
	f.number(int64(m.LayoutVersion))
	f.text(m.Tenant.String())
	f.number(nanos(m.CoversFrom))
	f.number(nanos(m.CoversTo))
	f.number(m.Schema.Version)
	f.text(m.Schema.Digest)
	f.number(int64(len(parts)))
	for i, p := range parts {
		if i > 0 && p.Path == parts[i-1].Path {
			return "", fmt.Errorf("evidence: part %s is listed twice", p.Path)
		}
		f.text(p.Path)
		f.text(string(p.Kind))
		f.number(int64(p.Length))
		f.text(p.Digest)
		f.text(p.Algorithm)
	}
	return sum(), nil
}

func orderedParts(parts []Part) []Part {
	out := make([]Part, len(parts))
	copy(out, parts)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// partDigest is the digest of one part's bytes, under the same framing as
// everything else so an empty part and an absent part cannot collide.
func partDigest(path string, raw []byte) string {
	f, sum := newFolder()
	f.text(DigestProfile + "/part")
	f.text(path)
	f.bytes(raw)
	return sum()
}

// Package is one assembled evidence package: the manifest, and the bytes at
// every path it lists.
//
// It is immutable by construction. [Package.Files] copies, so a caller that
// mutates what it is handed changes nothing about the package it was handed
// it from - which matters here more than usual, because the whole point of
// the value is that its bytes are what was attested to.
type Package struct {
	Manifest Manifest

	manifestBytes []byte
	paths         []string
	parts         map[string][]byte
}

// Files returns every path in the package, including [ManifestPath], with a
// copy of its bytes. It is what a caller writes to disk, streams to an
// auditor, or hands straight back to [Verify].
func (p Package) Files() map[string][]byte {
	out := make(map[string][]byte, len(p.parts)+1)
	for path, raw := range p.parts {
		out[path] = append([]byte(nil), raw...)
	}
	if p.manifestBytes != nil {
		out[ManifestPath] = append([]byte(nil), p.manifestBytes...)
	}
	return out
}

// Paths returns every path in the package in sorted order, [ManifestPath]
// included. Two packages with the same paths in the same order and the same
// bytes at each are the same package.
func (p Package) Paths() []string {
	out := make([]string, 0, len(p.paths)+1)
	out = append(out, p.paths...)
	if p.manifestBytes != nil {
		out = append(out, ManifestPath)
	}
	sort.Strings(out)
	return out
}

// Part returns one path's bytes.
func (p Package) Part(path string) ([]byte, bool) {
	if path == ManifestPath {
		if p.manifestBytes == nil {
			return nil, false
		}
		return append([]byte(nil), p.manifestBytes...), true
	}
	raw, ok := p.parts[path]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), raw...), true
}

// TotalBytes is the size of the whole package, manifest included.
func (p Package) TotalBytes() int {
	total := len(p.manifestBytes)
	for _, raw := range p.parts {
		total += len(raw)
	}
	return total
}

// Build turns content into a package: it validates that the content is
// evidence, encodes every part, digests each one, and folds them into a
// manifest.
//
// It is pure. No clock, no identifier source and no database is involved, so
// the same content always produces the same bytes - which is what makes two
// concurrent exports of one window byte-identical and what lets a golden
// test pin a digest.
func Build(c Content) (Package, error) {
	c = normalizeContent(c)
	if err := validateContent(c); err != nil {
		return Package{}, err
	}

	parts := make([]Part, 0, 2*len(c.Streams)+len(c.Epochs)+1)
	bytesByPath := make(map[string][]byte, cap(parts))
	add := func(path string, kind PartKind, raw []byte) {
		bytesByPath[path] = raw
		parts = append(parts, Part{
			Path: path, Kind: kind, Length: len(raw),
			Digest: partDigest(path, raw), Algorithm: DigestAlgorithm,
		})
	}

	header := headerFile{
		LayoutVersion:        LayoutVersion,
		Tenant:               c.Tenant.String(),
		CoversFromNS:         nanos(c.CoversFrom),
		CoversToNS:           nanos(c.CoversTo),
		SchemaReleaseVersion: c.Schema.Version,
		SchemaReleaseDigest:  c.Schema.Digest,
		Streams:              make([]headerStream, 0, len(c.Streams)),
		Epochs:               make([]headerEpoch, 0, len(c.Epochs)),
	}

	for i, s := range c.Streams {
		chainPath, eventsPath := StreamChainPath(i), StreamEventsPath(i)
		chainRaw, err := encodePart(projectChain(c.Tenant, s))
		if err != nil {
			return Package{}, err
		}
		events := eventsFile{
			Tenant: c.Tenant.String(), StreamKey: s.StreamKey,
			Events: make([]eventFile, 0, len(s.Events)),
		}
		for _, e := range s.Events {
			events.Events = append(events.Events, projectEvent(e))
		}
		eventsRaw, err := encodePart(events)
		if err != nil {
			return Package{}, err
		}
		add(chainPath, PartStreamChain, chainRaw)
		add(eventsPath, PartStreamEvents, eventsRaw)
		header.Streams = append(header.Streams, headerStream{
			StreamKey:      s.StreamKey,
			HeadSequence:   s.Head.Sequence,
			ChainHash:      s.Head.ChainHash,
			ChainAlgorithm: s.Head.ChainAlgorithm,
			ChainPath:      chainPath,
			EventsPath:     eventsPath,
			CoveredEvents:  len(s.Events),
		})
	}

	for i, epoch := range c.Epochs {
		path := EpochPath(i)
		raw, err := encodeEpoch(epoch)
		if err != nil {
			return Package{}, err
		}
		add(path, PartEpoch, raw)
		header.Epochs = append(header.Epochs, headerEpoch{
			EpochNumber:  epoch.EpochNumber,
			EpochID:      uuidText(epoch.EpochID),
			CoversFromNS: nanos(epoch.CoversFrom),
			CoversToNS:   nanos(epoch.CoversTo),
			Path:         path,
		})
	}

	headerRaw, err := encodePart(header)
	if err != nil {
		return Package{}, err
	}
	add(HeaderPath, PartHeader, headerRaw)

	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		LayoutVersion:   LayoutVersion,
		Tenant:          c.Tenant,
		CoversFrom:      c.CoversFrom,
		CoversTo:        c.CoversTo,
		Schema:          c.Schema,
		Parts:           orderedParts(parts),
		DigestAlgorithm: DigestAlgorithm,
	}
	digest, err := ComputeDigest(manifest)
	if err != nil {
		return Package{}, err
	}
	manifest.Digest = digest

	manifestRaw, err := encodePart(projectManifest(manifest))
	if err != nil {
		return Package{}, err
	}

	paths := make([]string, 0, len(bytesByPath))
	for path := range bytesByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return Package{
		Manifest:      manifest,
		manifestBytes: manifestRaw,
		paths:         paths,
		parts:         bytesByPath,
	}, nil
}

// Open reads package bytes back into a [Package] without checking anything
// beyond "the manifest parses". Everything a package can be wrong about is a
// [Finding] from [Verify], not a parse error, so opening a tampered package
// succeeds and verifying it is what says so.
func Open(files map[string][]byte) (Package, error) {
	manifestRaw, ok := files[ManifestPath]
	if !ok {
		return Package{}, ErrPackageMalformed{Path: ManifestPath, Reason: "the package has no manifest"}
	}
	var mf manifestFile
	if err := decodePart(ManifestPath, manifestRaw, &mf); err != nil {
		return Package{}, err
	}
	manifest, err := restoreManifest(mf)
	if err != nil {
		return Package{}, err
	}

	parts := make(map[string][]byte, len(files))
	paths := make([]string, 0, len(files))
	for path, raw := range files {
		if path == ManifestPath {
			continue
		}
		parts[path] = append([]byte(nil), raw...)
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return Package{
		Manifest:      manifest,
		manifestBytes: append([]byte(nil), manifestRaw...),
		paths:         paths,
		parts:         parts,
	}, nil
}

func projectManifest(m Manifest) manifestFile {
	out := manifestFile{
		SchemaVersion:        m.SchemaVersion,
		LayoutVersion:        m.LayoutVersion,
		Tenant:               m.Tenant.String(),
		CoversFromNS:         nanos(m.CoversFrom),
		CoversToNS:           nanos(m.CoversTo),
		SchemaReleaseVersion: m.Schema.Version,
		SchemaReleaseDigest:  m.Schema.Digest,
		Parts:                make([]partFile, 0, len(m.Parts)),
		Digest:               m.Digest,
		DigestAlgorithm:      m.DigestAlgorithm,
	}
	for _, p := range orderedParts(m.Parts) {
		out.Parts = append(out.Parts, partFile{
			Path: p.Path, Kind: string(p.Kind), Length: p.Length,
			Digest: p.Digest, Algorithm: p.Algorithm,
		})
	}
	return out
}

func restoreManifest(f manifestFile) (Manifest, error) {
	tenant, err := parseUUID(ManifestPath, "tenant", f.Tenant)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		SchemaVersion:   f.SchemaVersion,
		LayoutVersion:   f.LayoutVersion,
		Tenant:          tenant,
		CoversFrom:      fromNanos(f.CoversFromNS),
		CoversTo:        fromNanos(f.CoversToNS),
		Schema:          SchemaRelease{Version: f.SchemaReleaseVersion, Digest: f.SchemaReleaseDigest},
		Parts:           make([]Part, 0, len(f.Parts)),
		Digest:          f.Digest,
		DigestAlgorithm: f.DigestAlgorithm,
	}
	for _, p := range f.Parts {
		m.Parts = append(m.Parts, Part{
			Path: p.Path, Kind: PartKind(p.Kind), Length: p.Length,
			Digest: p.Digest, Algorithm: p.Algorithm,
		})
	}
	return m, nil
}

// normalizeContent puts the content into the one order and the one time
// resolution a package is defined in, so a caller cannot change a digest by
// reading streams in a different order or handing over nanosecond instants
// PostgreSQL never stored.
func normalizeContent(c Content) Content {
	c.CoversFrom = Truncate(c.CoversFrom)
	c.CoversTo = Truncate(c.CoversTo)

	streams := make([]Stream, len(c.Streams))
	copy(streams, c.Streams)
	sort.Slice(streams, func(i, j int) bool { return streams[i].StreamKey < streams[j].StreamKey })
	for i := range streams {
		events := make([]Event, len(streams[i].Events))
		copy(events, streams[i].Events)
		sort.Slice(events, func(a, b int) bool { return events[a].Sequence < events[b].Sequence })
		streams[i].Events = events
	}
	c.Streams = streams

	epochs := make([]Epoch, len(c.Epochs))
	for i, epoch := range c.Epochs {
		epochs[i] = normalizeEpoch(epoch)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i].EpochNumber < epochs[j].EpochNumber })
	c.Epochs = epochs
	return c
}

// validateContent refuses content that is not evidence. It reports every
// problem it finds rather than the first, except a cross-tenant row, which
// is returned on its own as [ErrTenantLeak]: that one is not a
// completeness problem to be listed alongside others, it is a refusal.
func validateContent(c Content) error {
	var missing []string
	add := func(format string, args ...any) { missing = append(missing, fmt.Sprintf(format, args...)) }

	if c.Tenant == uuid.Nil {
		add("tenant is required")
	}
	if c.CoversFrom.IsZero() || c.CoversTo.IsZero() {
		add("the covered recorded-time window is required")
	} else if !c.CoversFrom.Before(c.CoversTo) {
		add("the covered window [%s, %s) is empty or inverted",
			c.CoversFrom.Format(time.RFC3339Nano), c.CoversTo.Format(time.RFC3339Nano))
	}
	if c.Schema.Version < 1 {
		add("schema release version is required")
	}
	if c.Schema.Digest == "" {
		add("schema release digest is required")
	}
	if len(c.Streams) == 0 {
		add("an evidence package must cover at least one stream")
	}

	covered := 0
	seen := make(map[string]bool, len(c.Streams))
	for _, s := range c.Streams {
		if s.StreamKey == "" {
			add("a covered stream carries no stream key")
			continue
		}
		if seen[s.StreamKey] {
			add("stream %s is included twice", s.StreamKey)
			continue
		}
		seen[s.StreamKey] = true
		if err := validateStream(c, s, add); err != nil {
			return err
		}
		covered += len(s.Events)
	}
	if len(c.Streams) > 0 && covered == 0 {
		add("no event was recorded in the covered window; the package would prove nothing")
	}

	if len(c.Epochs) == 0 {
		add("an evidence package must carry the signed checkpoint epochs covering its window")
	}
	for _, epoch := range c.Epochs {
		if epoch.Tenant != c.Tenant {
			return ErrTenantLeak{
				Expected: c.Tenant, Found: epoch.Tenant,
				Where: fmt.Sprintf("checkpoint epoch %d", epoch.EpochNumber),
			}
		}
		if epoch.Signature == nil {
			add("checkpoint epoch %d carries no signature", epoch.EpochNumber)
		}
	}

	if len(missing) > 0 {
		return ErrContentInvalid{Tenant: c.Tenant, Missing: missing}
	}
	if gapFrom, gapUntil, ok := epochCoverageGap(c.CoversFrom, c.CoversTo, c.Epochs); !ok {
		return ErrIncompleteEpochCoverage{Tenant: c.Tenant, GapFrom: gapFrom, GapUntil: gapUntil}
	}
	return nil
}

func validateStream(c Content, s Stream, add func(string, ...any)) error {
	if s.Head.Sequence < 1 {
		add("stream %s has head sequence %d; a covered stream holds at least one event", s.StreamKey, s.Head.Sequence)
	}
	if s.Head.ChainHash == "" || s.Head.ChainAlgorithm == "" {
		add("stream %s does not record the chain hash its head proves", s.StreamKey)
	}
	if int64(len(s.Links)) != s.Head.Sequence || int64(len(s.Digests)) != s.Head.Sequence {
		add("stream %s carries %d links and %d event digests for a head at sequence %d; the chain must run unbroken from genesis",
			s.StreamKey, len(s.Links), len(s.Digests), s.Head.Sequence)
		return nil
	}
	for i := range s.Links {
		want := int64(i + 1)
		if s.Links[i].Sequence != want || s.Digests[i].Sequence != want {
			add("stream %s has a gap or reordering at sequence %d", s.StreamKey, want)
			return nil
		}
	}
	if s.Head.Sequence > 0 && s.Links[len(s.Links)-1].ChainHash != s.Head.ChainHash {
		add("stream %s records a head chain hash its own last link does not produce", s.StreamKey)
	}

	for _, e := range s.Events {
		if e.Tenant != c.Tenant {
			return ErrTenantLeak{
				Expected: c.Tenant, Found: e.Tenant,
				Where: fmt.Sprintf("event %s@%d", e.StreamKey, e.Sequence),
			}
		}
		if e.StreamKey != s.StreamKey {
			add("event %s@%d was filed under stream %s", e.StreamKey, e.Sequence, s.StreamKey)
			continue
		}
		if e.Sequence < 1 || e.Sequence > s.Head.Sequence {
			add("event %s@%d lies outside the chain this package carries", e.StreamKey, e.Sequence)
			continue
		}
		if recorded := Truncate(e.RecordedAt); recorded.Before(c.CoversFrom) || !recorded.Before(c.CoversTo) {
			add("event %s@%d was recorded at %s, outside the covered window",
				e.StreamKey, e.Sequence, recorded.Format(time.RFC3339Nano))
		}
		if digest := s.Digests[e.Sequence-1]; digest.Digest != e.Digest || digest.EventID != e.EventID {
			add("event %s@%d does not match the chain's own record of it", e.StreamKey, e.Sequence)
		}
	}
	return nil
}

// epochCoverageGap walks the epoch windows in order and reports the first
// instant of [from, to) no epoch covers. Epochs are contiguous by
// construction (each epoch's window starts where its predecessor's ended),
// so this walk is exact rather than an approximation of an interval union.
func epochCoverageGap(from, to time.Time, epochs []Epoch) (gapFrom, gapUntil time.Time, ok bool) {
	ordered := make([]Epoch, len(epochs))
	copy(ordered, epochs)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CoversFrom.Before(ordered[j].CoversFrom) })

	cursor := from
	for _, epoch := range ordered {
		if !cursor.Before(to) {
			break
		}
		if epoch.CoversFrom.After(cursor) {
			break
		}
		if epoch.CoversTo.After(cursor) {
			cursor = epoch.CoversTo
		}
	}
	if cursor.Before(to) {
		return cursor, to, false
	}
	return time.Time{}, time.Time{}, true
}
