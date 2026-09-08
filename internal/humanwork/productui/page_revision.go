package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// PageDefinitionSnapshot is the render-free, storable projection of one
// product surface: every PageDefinition field except the render func,
// which cannot be compared, digested, or persisted. Snapshots are the
// unit the revision log versions.
type PageDefinitionSnapshot struct {
	Page        PageID   `json:"page"`
	Route       string   `json:"route"`
	Label       string   `json:"label"`
	Icon        string   `json:"icon"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	LabelKey    string   `json:"label_key"`
	TitleKey    string   `json:"title_key"`
	SubtitleKey string   `json:"subtitle_key"`
	SearchTerms []string `json:"search_terms"`
	PrimaryNav  bool     `json:"primary_nav"`
	ParentNav   PageID   `json:"parent_nav"`
	RenderOrder int      `json:"render_order"`
}

// SnapshotPageDefinition projects one registry definition into its
// snapshot. Search terms are deep-copied so later registry or caller
// mutation cannot reach recorded revisions.
func SnapshotPageDefinition(definition PageDefinition) PageDefinitionSnapshot {
	return PageDefinitionSnapshot{
		Page: definition.ID, Route: definition.Route, Label: definition.Label, Icon: definition.Icon,
		Title: definition.Title, Subtitle: definition.Subtitle,
		LabelKey: definition.LabelKey, TitleKey: definition.TitleKey, SubtitleKey: definition.SubtitleKey,
		SearchTerms: append([]string(nil), definition.SearchTerms...),
		PrimaryNav:  definition.PrimaryNav, ParentNav: definition.ParentNav, RenderOrder: definition.RenderOrder,
	}
}

// revisionCanonical is the digest-bound encoding: snapshot fields plus
// the bound version, in declaration order.
type revisionCanonical struct {
	Page        PageID   `json:"page"`
	Route       string   `json:"route"`
	Label       string   `json:"label"`
	Icon        string   `json:"icon"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	LabelKey    string   `json:"label_key"`
	TitleKey    string   `json:"title_key"`
	SubtitleKey string   `json:"subtitle_key"`
	SearchTerms []string `json:"search_terms"`
	PrimaryNav  bool     `json:"primary_nav"`
	ParentNav   PageID   `json:"parent_nav"`
	RenderOrder int      `json:"render_order"`
	Version     int64    `json:"version"`
}

// CanonicalRevisionBytes encodes one snapshot plus its bound version.
// encoding/json marshals structs deterministically in field order, so
// identical snapshots always produce identical bytes.
func CanonicalRevisionBytes(snapshot PageDefinitionSnapshot, version int64) []byte {
	encoded, err := json.Marshal(revisionCanonical{
		Page: snapshot.Page, Route: snapshot.Route, Label: snapshot.Label, Icon: snapshot.Icon,
		Title: snapshot.Title, Subtitle: snapshot.Subtitle,
		LabelKey: snapshot.LabelKey, TitleKey: snapshot.TitleKey, SubtitleKey: snapshot.SubtitleKey,
		SearchTerms: snapshot.SearchTerms,
		PrimaryNav:  snapshot.PrimaryNav, ParentNav: snapshot.ParentNav, RenderOrder: snapshot.RenderOrder,
		Version: version,
	})
	if err != nil {
		return nil
	}
	return encoded
}

// DigestRevision digests one snapshot bound to its version.
func DigestRevision(snapshot PageDefinitionSnapshot, version int64) string {
	digest := sha256.Sum256(CanonicalRevisionBytes(snapshot, version))
	return hex.EncodeToString(digest[:])
}

// PageDefinitionRevision is one immutable published version of a page:
// the snapshot, the bound version, and the content digest addressing
// it. Values are deep copies; mutating a retrieved revision never
// reaches the log.
type PageDefinitionRevision struct {
	Snapshot PageDefinitionSnapshot
	Version  int64
	Digest   string
}

// persistedRevision is the stored form: canonical content plus digest.
type persistedRevision struct {
	Snapshot PageDefinitionSnapshot `json:"snapshot"`
	Version  int64                  `json:"version"`
	Digest   string                 `json:"digest"`
}

// MarshalRevision encodes one revision into persistable bytes.
func MarshalRevision(revision PageDefinitionRevision) []byte {
	encoded, err := json.Marshal(persistedRevision{Snapshot: revision.Snapshot, Version: revision.Version, Digest: revision.Digest})
	if err != nil {
		return nil
	}
	return encoded
}

// ParseRevision decodes persisted bytes and re-verifies the digest.
// Tampered, truncated, or foreign bytes fail closed.
func ParseRevision(encoded []byte) (PageDefinitionRevision, error) {
	var stored persistedRevision
	if err := json.Unmarshal(encoded, &stored); err != nil {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision bytes do not parse: %w", err)
	}
	if stored.Digest == "" || stored.Digest != DigestRevision(stored.Snapshot, stored.Version) {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision digest mismatch for page %q version %d", stored.Snapshot.Page, stored.Version)
	}
	stored.Snapshot.SearchTerms = append([]string(nil), stored.Snapshot.SearchTerms...)
	return PageDefinitionRevision{Snapshot: stored.Snapshot, Version: stored.Version, Digest: stored.Digest}, nil
}

// PageRevisionLog is the presentation projection of published page
// revisions: append-only, keyed by page and version, addressed by
// digest. It records what the governed service published; durable
// truth stays with the owning service. The zero value is ready to
// record.
type PageRevisionLog struct {
	revisions map[PageID]map[int64]PageDefinitionRevision
}

// Record persists one snapshot at one version. Re-recording the
// identical snapshot is idempotent; re-recording different content at
// an existing version is a conflict and refused — published history
// is immutable. Non-positive versions are refused.
func (log *PageRevisionLog) Record(page PageID, snapshot PageDefinitionSnapshot, version int64) (PageDefinitionRevision, error) {
	if version <= 0 {
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision version must be positive, got %d", version)
	}
	snapshot.SearchTerms = append([]string(nil), snapshot.SearchTerms...)
	revision := PageDefinitionRevision{Snapshot: snapshot, Version: version, Digest: DigestRevision(snapshot, version)}
	if log.revisions == nil {
		log.revisions = make(map[PageID]map[int64]PageDefinitionRevision)
	}
	versions := log.revisions[page]
	if versions == nil {
		versions = make(map[int64]PageDefinitionRevision)
		log.revisions[page] = versions
	}
	if existing, ok := versions[version]; ok {
		if existing.Digest != revision.Digest {
			return PageDefinitionRevision{}, fmt.Errorf("productui: revision conflict for page %q version %d", page, version)
		}
		return existing, nil
	}
	versions[version] = revision
	return revision, nil
}

// Revision returns one recorded revision. Returned snapshots are deep
// copies.
func (log *PageRevisionLog) Revision(page PageID, version int64) (PageDefinitionRevision, bool) {
	revision, ok := log.revisions[page][version]
	if !ok {
		return PageDefinitionRevision{}, false
	}
	revision.Snapshot.SearchTerms = append([]string(nil), revision.Snapshot.SearchTerms...)
	return revision, true
}

// Latest returns the maximum recorded version of one page.
func (log *PageRevisionLog) Latest(page PageID) (PageDefinitionRevision, bool) {
	versions := log.revisions[page]
	var latest PageDefinitionRevision
	found := false
	for version, revision := range versions {
		if !found || version > latest.Version {
			latest, found = revision, true
		}
	}
	if !found {
		return PageDefinitionRevision{}, false
	}
	latest.Snapshot.SearchTerms = append([]string(nil), latest.Snapshot.SearchTerms...)
	return latest, true
}

// Pages inventories every page carrying at least one revision, sorted.
func (log *PageRevisionLog) Pages() []PageID {
	pages := make([]PageID, 0, len(log.revisions))
	for page, versions := range log.revisions {
		if len(versions) > 0 {
			pages = append(pages, page)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i] < pages[j] })
	return pages
}
