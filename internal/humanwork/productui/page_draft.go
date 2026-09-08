package productui

import "fmt"

// PageDraft is the mutable working copy in the page lifecycle: bound
// to the base revision it edits (or to nothing for a from-scratch
// page), freely editable by its owner, publishable exactly once its
// lineage is current. Drafts never touch the revision log until
// PublishDraft records them; discarding a draft drops the struct.
type PageDraft struct {
	Page PageID
	// BaseVersion and BaseDigest bind the revision this draft edits.
	// Both zero mark a from-scratch draft for a page carrying no
	// versions yet.
	BaseVersion int64
	BaseDigest  string
	// Snapshot is the working copy. Owners mutate it directly; the
	// log only ever sees it through PublishDraft.
	Snapshot PageDefinitionSnapshot
}

// NewPageDraft opens a draft on one recorded revision. The working
// copy starts identical to its base.
func NewPageDraft(base PageDefinitionRevision) PageDraft {
	return PageDraft{Page: base.Snapshot.Page, BaseVersion: base.Version, BaseDigest: base.Digest, Snapshot: base.Snapshot}
}

// NewPageDraftFromScratch opens a draft for a page carrying no
// versions yet. The working copy starts blank.
func NewPageDraftFromScratch(page PageID) PageDraft {
	return PageDraft{Page: page, Snapshot: PageDefinitionSnapshot{Page: page}}
}

// DraftDigest identifies the working-copy state: the snapshot bound
// to its base version. Identical drafts always share a digest.
func DraftDigest(draft PageDraft) string {
	return DigestRevision(draft.Snapshot, draft.BaseVersion)
}

// PublishDraft records one draft as a new revision. Rules, in order:
// identical replays are idempotent; conflicting content at an
// existing version is refused; a draft publishes only onto its base
// while the base is still latest (a stale base refuses, so lineage
// never forks silently); a from-scratch draft publishes only onto a
// page carrying no versions yet.
func PublishDraft(log *PageRevisionLog, draft PageDraft, version int64) (PageDefinitionRevision, error) {
	if version <= 0 {
		return PageDefinitionRevision{}, fmt.Errorf("productui: publish version must be positive, got %d", version)
	}
	if draft.Snapshot.Page != draft.Page {
		return PageDefinitionRevision{}, fmt.Errorf("productui: draft working copy targets page %q, draft binds page %q", draft.Snapshot.Page, draft.Page)
	}
	if existing, ok := log.Revision(draft.Page, version); ok {
		if existing.Digest == DigestRevision(draft.Snapshot, version) {
			return existing, nil
		}
		return PageDefinitionRevision{}, fmt.Errorf("productui: revision conflict for page %q version %d", draft.Page, version)
	}
	if draft.BaseVersion == 0 {
		if _, ok := log.Latest(draft.Page); ok {
			return PageDefinitionRevision{}, fmt.Errorf("productui: from-scratch draft refuses versioned page %q: draft from latest instead", draft.Page)
		}
	} else {
		latest, ok := log.Latest(draft.Page)
		if !ok || latest.Version != draft.BaseVersion || latest.Digest != draft.BaseDigest {
			return PageDefinitionRevision{}, fmt.Errorf("productui: stale base for page %q: draft edits version %d, latest differs", draft.Page, draft.BaseVersion)
		}
	}
	return log.Record(draft.Page, draft.Snapshot, version)
}
