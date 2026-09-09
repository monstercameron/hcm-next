// POP-005: freeze an immutable PopulationSnapshot. A snapshot is evidence: it
// binds exactly the subjects (or, when membership itself is protected, the
// fact that it is protected) a decision authorized to disclose, together with
// the definition, revision, time context, watermarks and count that produced
// it, under one canonical digest. Nothing that happens to the definition, the
// facts or the restriction decision afterward can reach back and change an
// already-frozen snapshot.
package population

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrSnapshotIncomplete is returned when Freeze is asked to bind a snapshot
// that has no definition, no revision version, or no time context.
var ErrSnapshotIncomplete = errors.New("population: snapshot requires a definition, revision and time context")

// Snapshot is the immutable, digested result of freezing one resolution. Its
// zero value is not a legal snapshot; construct one with Freeze.
type Snapshot struct {
	DefinitionID     string
	DefinitionDigest string
	RevisionVersion  string
	AsOf             values.Instant
	KnownAt          values.KnownAt
	Watermarks       map[SubjectKind]values.Instant
	// SubjectIDs is the exact sorted set of canonical subject-ref text the
	// restriction decision authorized to disclose. It is empty when
	// MembershipProtected is true: a protected snapshot never carries a raw
	// member list, even an empty-looking one that a caller could mistake for
	// "zero members" instead of "not disclosed".
	SubjectIDs []string
	// MembershipProtected reports whether raw membership was withheld by the
	// restriction decision that produced this snapshot.
	MembershipProtected bool
	Count               values.Presence[int]
	Completeness        Completeness
	Digest              string
}

// SubjectIDList returns a defensive copy of the snapshot's bound subject IDs.
func (s Snapshot) SubjectIDList() []string {
	cp := make([]string, len(s.SubjectIDs))
	copy(cp, s.SubjectIDs)
	return cp
}

// WatermarkFor returns the recorded watermark for kind and whether one was
// bound into the snapshot.
func (s Snapshot) WatermarkFor(kind SubjectKind) (values.Instant, bool) {
	w, ok := s.Watermarks[kind]
	return w, ok
}

// Freeze binds restricted into an immutable Snapshot citing def, revision and
// the resolution's time context. Two calls with byte-identical inputs produce
// byte-identical digests; any change to def, the resolved facts (reflected in
// restricted.Members) or the restriction decision (reflected in
// restricted.Versions and disclosure) changes the digest.
func Freeze(def Definition, revisionVersion string, restricted RestrictedResult, asOf values.Instant, knownAt values.KnownAt, watermarks map[SubjectKind]values.Instant) (Snapshot, error) {
	if err := def.Validate(); err != nil {
		return Snapshot{}, err
	}
	if revisionVersion == "" {
		return Snapshot{}, ErrSnapshotIncomplete
	}
	if err := asOf.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: as-of: %v", ErrSnapshotIncomplete, err)
	}
	if err := knownAt.Instant().Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: known-at: %v", ErrSnapshotIncomplete, err)
	}

	protected := restricted.Members == nil
	var ids []string
	if !protected {
		ids = make([]string, 0, len(restricted.Members))
		for _, m := range restricted.Members {
			if err := m.Subject.Validate(); err != nil {
				return Snapshot{}, fmt.Errorf("population: snapshot subject: %w", err)
			}
			ids = append(ids, m.Subject.String())
		}
		sort.Strings(ids)
	}

	wm := make(map[SubjectKind]values.Instant, len(watermarks))
	wmKeys := make([]string, 0, len(watermarks))
	for k, v := range watermarks {
		if err := v.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("population: snapshot watermark %s: %w", k, err)
		}
		wm[k] = v
		wmKeys = append(wmKeys, string(k))
	}
	sort.Strings(wmKeys)

	snap := Snapshot{
		DefinitionID:        def.ID,
		DefinitionDigest:    canonicalbytesDigest(def.Canonical()),
		RevisionVersion:     revisionVersion,
		AsOf:                asOf,
		KnownAt:             knownAt,
		Watermarks:          wm,
		SubjectIDs:          ids,
		MembershipProtected: protected,
		Count:               restricted.Count,
		Completeness:        restricted.Completeness,
	}

	w := writerFor(snapshotSchema).
		String("definition_id", snap.DefinitionID).
		String("definition_digest", snap.DefinitionDigest).
		String("revision_version", snap.RevisionVersion).
		Value("as_of", snap.AsOf).
		Value("known_at", snap.KnownAt.Instant()).
		String("completeness", snap.Completeness.String()).
		Bool("membership_protected", snap.MembershipProtected).
		Count("subjects", len(ids))
	for _, id := range ids {
		w.String("subject", id)
	}
	countText := snap.Count.State().String()
	if v, ok := snap.Count.Get(); ok {
		countText = fmt.Sprintf("VALUE:%d", v)
	}
	w.String("count", countText)
	w.Count("watermarks", len(wmKeys))
	for _, k := range wmKeys {
		w.String("watermark.kind", k)
		w.Value("watermark.at", wm[SubjectKind(k)])
	}
	digest, err := w.Digest()
	if err != nil {
		return Snapshot{}, fmt.Errorf("population: snapshot digest: %w", err)
	}
	snap.Digest = digest
	return snap, nil
}

// canonicalbytesDigest returns the sha256 digest over raw, or "" for a nil
// canonical encoding (an invalid value never contributes silently).
func canonicalbytesDigest(raw []byte) string {
	if raw == nil {
		return ""
	}
	return canonicalDigest(raw)
}
