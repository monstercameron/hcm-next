// Package productui presents durable draft autosave status.
// The presenter resolves one draft plus its durable save
// record into status copy: saved when the record holds the
// draft's digest, unsaved when the working copy moved on or
// no record exists, failed when the record carries an error.
// Timestamps render in the locale's time zone. Records bound
// to another page — or to no page — never present as this
// draft's save. Durability itself belongs to the caller that
// records digests; this presenter only reads them.
package productui

import (
	"strings"
	"time"
)

// DraftAutosaveState is the autosave condition one draft
// presents. The zero value is unsaved so a draft with no
// save record never presents as saved.
type DraftAutosaveState int

const (
	// DraftAutosaveUnsaved marks a working copy newer than its record.
	DraftAutosaveUnsaved DraftAutosaveState = iota
	// DraftAutosaveSaved marks a working copy matching its record.
	DraftAutosaveSaved
	// DraftAutosaveFailed marks a record carrying an attempt error.
	DraftAutosaveFailed
)

// DraftAutosaveRecord is the durable save fact one draft
// presents against: the page the save belongs to, the
// working-copy digest it stored, when it stored it, and the
// latest attempt error. A recorder clears Err on success.
type DraftAutosaveRecord struct {
	Page    PageID
	Digest  string
	SavedAt time.Time
	Err     string
}

// DraftAutosaveStatus is the presented autosave line: its
// state with localized text and detail.
type DraftAutosaveStatus struct {
	State  DraftAutosaveState
	Text   string
	Detail string
}

// ResolveDraftAutosave presents one draft's autosave status
// against its durable save record. A recorded error wins;
// then digest equality with a page-bound record; everything
// else is unsaved changes.
func ResolveDraftAutosave(locale LocaleContext, draft PageDraft, saved DraftAutosaveRecord) DraftAutosaveStatus {
	if err := strings.TrimSpace(saved.Err); err != "" {
		return DraftAutosaveStatus{State: DraftAutosaveFailed, Text: locale.Text("draft.autosave_failed"), Detail: err}
	}
	if saved.Page != "" && saved.Page == draft.Page && saved.Digest == DraftDigest(draft) {
		return DraftAutosaveStatus{
			State:  DraftAutosaveSaved,
			Text:   locale.Text("draft.autosave_saved"),
			Detail: locale.Text("draft.autosave_last_saved", map[string]string{"time": formatAutosaveTime(locale, saved.SavedAt)}),
		}
	}
	status := DraftAutosaveStatus{State: DraftAutosaveUnsaved, Text: locale.Text("draft.autosave_unsaved")}
	if saved.Page != "" && saved.Page == draft.Page && !saved.SavedAt.IsZero() {
		status.Detail = locale.Text("draft.autosave_last_saved", map[string]string{"time": formatAutosaveTime(locale, saved.SavedAt)})
	}
	return status
}

// formatAutosaveTime renders a save timestamp the way dated
// presentation does elsewhere: the locale date with the
// clock in the locale's time zone.
func formatAutosaveTime(locale LocaleContext, value time.Time) string {
	locale = locale.normalized()
	location, err := time.LoadLocation(locale.TimeZone)
	if err != nil {
		location = time.UTC
	}
	local := value.In(location)
	return locale.FormatDate(value) + " · " + local.Format("15:04 MST")
}
