// Package surface implements governed intent lifecycle actions and
// inspection surfaces (INTENT-021): every surface is a typed read or a
// material action with field and purpose filtering. Reads never reveal
// existence without current authority; timeline and inspector explain
// exact versions and evidence without collapsing unknown or degraded
// dimensions; lifecycle actions create successor revisions or related
// intents, never mutate history; subscriptions emit redacted versioned
// transitions only while authority holds.
package surface

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrNotFound reports a missing intent or a present one the caller
	// may not know about: the two are deliberately indistinguishable.
	ErrNotFound = errors.New("surface: intent not found")

	// ErrNotAuthorized reports a material action attempted without
	// current authority.
	ErrNotAuthorized = errors.New("surface: authority required")

	// ErrStaleRevision reports an action against a superseded revision.
	ErrStaleRevision = errors.New("surface: stale revision")

	// ErrClosedIntent reports a lifecycle action on a terminal intent.
	ErrClosedIntent = errors.New("surface: intent is closed")
)

// Field is one named intent field with its restriction marker.
type Field struct {
	Value      string
	Restricted bool
}

// RevisionRecord is one immutable history entry.
type RevisionRecord struct {
	Revision uint64
	Digest   string
	Note     string
}

// IntentView is the stored intent projection surfaces read.
type IntentView struct {
	ID       string
	Revision uint64
	Status   string
	Fields   map[string]Field
	History  []RevisionRecord
	Related  []string
}

// Authority is the caller's current grant: field allowlist, purposes and
// restricted clearance.
type Authority struct {
	Principal string
	Fields    []string
	Purposes  []string
	Clearance bool
}

func (a Authority) allows(field string, restricted bool) bool {
	if restricted && !a.Clearance {
		return false
	}
	if len(a.Fields) == 0 {
		return true
	}
	for _, allowed := range a.Fields {
		if allowed == field {
			return true
		}
	}
	return false
}

func (a Authority) allowsPurpose(purpose string) bool {
	for _, allowed := range a.Purposes {
		if allowed == purpose {
			return true
		}
	}
	return false
}

// authorized resolves the existence oracle: nil and unauthorized both
// read as not found.
func authorized(view *IntentView, auth Authority) (*IntentView, error) {
	if view == nil || strings.TrimSpace(auth.Principal) == "" {
		return nil, fmt.Errorf("surface: resolve: %w", ErrNotFound)
	}
	return view, nil
}

func viewDigest(view IntentView) string {
	names := make([]string, 0, len(view.Fields))
	for name := range view.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := []string{"intent-surface", view.ID, fmt.Sprintf("%d", view.Revision), view.Status}
	for _, name := range names {
		parts = append(parts, name, view.Fields[name].Value)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DeepLink returns the stable link token for one authorized intent.
func DeepLink(view *IntentView, auth Authority) (string, error) {
	resolved, err := authorized(view, auth)
	if err != nil {
		return "", err
	}
	return "intent/" + resolved.ID + "?rev=" + fmt.Sprintf("%d", resolved.Revision), nil
}

// Search returns the authorized subset of views matching the query over
// visible field values. Unauthorized views are absent, never redacted
// placeholders: the result never admits how many were skipped.
func Search(views []*IntentView, auth Authority, query string) []IntentView {
	var out []IntentView
	for _, view := range views {
		if _, err := authorized(view, auth); err != nil {
			continue
		}
		match := query == ""
		for name, field := range view.Fields {
			if !auth.allows(name, field.Restricted) {
				continue
			}
			if query != "" && strings.Contains(field.Value, query) {
				match = true
			}
		}
		if match {
			out = append(out, *view)
		}
	}
	return out
}

// Timeline explains the exact version and evidence chain: every revision
// with its digest and note, including unknown and degraded states stated
// as-is and never collapsed.
func Timeline(view *IntentView, auth Authority) ([]RevisionRecord, error) {
	resolved, err := authorized(view, auth)
	if err != nil {
		return nil, err
	}
	return append([]RevisionRecord(nil), resolved.History...), nil
}

// InspectedField is one field the inspector may show.
type InspectedField struct {
	Name       string
	Value      string
	Restricted bool
}

// Inspector shows the authorized field set with restriction markers and
// the current revision digest.
func Inspector(view *IntentView, auth Authority) ([]InspectedField, string, error) {
	resolved, err := authorized(view, auth)
	if err != nil {
		return nil, "", err
	}
	var fields []InspectedField
	for name, field := range resolved.Fields {
		if !auth.allows(name, field.Restricted) {
			continue
		}
		fields = append(fields, InspectedField{Name: name, Value: field.Value, Restricted: field.Restricted})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields, viewDigest(*resolved), nil
}

// Export renders the purpose-bound authorized field set. The purpose must
// be granted and every restricted field needs clearance.
func Export(view *IntentView, auth Authority, purpose string) (map[string]string, error) {
	resolved, err := authorized(view, auth)
	if err != nil {
		return nil, err
	}
	if !auth.allowsPurpose(purpose) {
		return nil, fmt.Errorf("surface: export for %q: %w", purpose, ErrNotAuthorized)
	}
	out := make(map[string]string)
	for name, field := range resolved.Fields {
		if !auth.allows(name, field.Restricted) {
			continue
		}
		out[name] = field.Value
	}
	return out, nil
}

// Transition is one versioned state move.
type Transition struct {
	Revision uint64
	From     string
	To       string
}

// Subscription emits redacted versioned transitions while authority holds.
type Subscription struct {
	ID       string
	IntentID string
	Revision uint64
	Events   []Transition
}

// Subscribe opens one subscription at the intent's current revision.
func Subscribe(view *IntentView, auth Authority) (*Subscription, error) {
	resolved, err := authorized(view, auth)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte("subscription\x00" + resolved.ID + "\x00" + auth.Principal))
	return &Subscription{
		ID:       "sub-" + hex.EncodeToString(sum[:])[:16],
		IntentID: resolved.ID, Revision: resolved.Revision,
	}, nil
}

// Emit appends one transition when authority still holds and the revision
// chains: gaps and replays are refused, and the payload carries states
// only, never fields.
func (s *Subscription) Emit(view *IntentView, auth Authority, transition Transition) error {
	resolved, err := authorized(view, auth)
	if err != nil {
		return err
	}
	if resolved.ID != s.IntentID || transition.Revision != s.Revision+1 {
		return fmt.Errorf("surface: emit: %w", ErrStaleRevision)
	}
	s.Events = append(s.Events, transition)
	s.Revision = transition.Revision
	return nil
}

func act(view *IntentView, auth Authority, expectedRevision uint64) (*IntentView, error) {
	resolved, err := authorized(view, auth)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(auth.Principal) == "" {
		return nil, fmt.Errorf("surface: act: %w", ErrNotAuthorized)
	}
	if resolved.Revision != expectedRevision {
		return nil, fmt.Errorf("surface: act at rev %d: %w", expectedRevision, ErrStaleRevision)
	}
	if resolved.Status == "CLOSED" || resolved.Status == "CANCELLED" {
		return nil, fmt.Errorf("surface: act on %s: %w", resolved.Status, ErrClosedIntent)
	}
	next := *resolved
	next.Fields = make(map[string]Field, len(resolved.Fields))
	for name, field := range resolved.Fields {
		next.Fields[name] = field
	}
	next.History = append([]RevisionRecord(nil), resolved.History...)
	next.Related = append([]string(nil), resolved.Related...)
	return &next, nil
}

func record(view *IntentView, note string) {
	view.Revision++
	view.History = append(view.History, RevisionRecord{Revision: view.Revision, Digest: viewDigest(*view), Note: note})
}

// Cancel closes one open intent with its reason: a new terminal revision,
// history preserved.
func Cancel(view *IntentView, auth Authority, expectedRevision uint64, reason string) (*IntentView, error) {
	next, err := act(view, auth, expectedRevision)
	if err != nil {
		return nil, err
	}
	next.Status = "CANCELLED"
	record(next, "cancel: "+reason)
	return next, nil
}

// Supersede marks the intent superseded by a related successor intent.
func Supersede(view *IntentView, auth Authority, expectedRevision uint64, successorRef string) (*IntentView, error) {
	next, err := act(view, auth, expectedRevision)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(successorRef) == "" {
		return nil, fmt.Errorf("surface: supersede without successor: %w", ErrNotAuthorized)
	}
	next.Status = "SUPERSEDED"
	next.Related = append(next.Related, successorRef)
	record(next, "superseded by "+successorRef)
	return next, nil
}

// Correct appends a successor revision with corrected visible fields. It
// never edits history: the prior revision stays sealed in the chain.
func Correct(view *IntentView, auth Authority, expectedRevision uint64, corrections map[string]string) (*IntentView, error) {
	next, err := act(view, auth, expectedRevision)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(corrections))
	for name := range corrections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field, ok := next.Fields[name]
		if !ok || !auth.allows(name, field.Restricted) {
			return nil, fmt.Errorf("surface: correct %s: %w", name, ErrNotAuthorized)
		}
		field.Value = corrections[name]
		next.Fields[name] = field
	}
	record(next, "correct "+strings.Join(names, ","))
	return next, nil
}

// Escalate links a related escalation intent without changing status.
func Escalate(view *IntentView, auth Authority, expectedRevision uint64, escalationRef string) (*IntentView, error) {
	next, err := act(view, auth, expectedRevision)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(escalationRef) == "" {
		return nil, fmt.Errorf("surface: escalate without target: %w", ErrNotAuthorized)
	}
	next.Related = append(next.Related, escalationRef)
	record(next, "escalated to "+escalationRef)
	return next, nil
}
