package workspace

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AuthorityElementID is the id of the shell's authority-context region: the
// frontend plan's page-anatomy region 3 (acting role and the other facts
// [Session] carries that describe who the viewer is acting as).
const AuthorityElementID = "workspace-authority"

// AuthorityState is the closed set of reasons a session's acting authority
// would need a persistent, shell-wide notice. It is a separate declaration
// from internal/humanwork/productui.AuthorityState (this package may not
// import internal/), holding the same closed set for the same reason:
// [AuthorityState.noticeworthy] names every safe (quiet) value explicitly
// and discloses for everything else, including a value it does not
// recognize at all, so a future authority mode this presentation layer has
// not been taught yet fails toward disclosure rather than toward silently
// impersonating the viewer's own authority.
type AuthorityState string

const (
	// AuthorityStateSelf is the one value [AuthorityState.noticeworthy]
	// treats as quiet: acting under the viewer's own, undelegated,
	// unelevated authority.
	AuthorityStateSelf AuthorityState = "self"
	// AuthorityStateDelegated is another principal's authority, exercised
	// on their behalf.
	AuthorityStateDelegated AuthorityState = "delegated"
	// AuthorityStateViewAs is a policy simulation under a viewed identity.
	AuthorityStateViewAs AuthorityState = "view_as"
	// AuthorityStateElevated is the viewer's own identity acting with
	// broadened, temporarily granted capability.
	AuthorityStateElevated AuthorityState = "elevated"
	// AuthorityStateBreakGlass is emergency access exercised under an
	// active break-glass grant.
	AuthorityStateBreakGlass AuthorityState = "break_glass"
)

// noticeworthy reports whether s calls for the persistent authority-context
// strip. AuthorityStateSelf is the only value that stays quiet; every other
// named state discloses, and -- with no permissive default -- so does
// anything this switch does not recognize at all, including the empty
// AuthorityState. Unlike internal/humanwork/productui's equivalent, this
// package has no companion boolean to fall back on for an empty value: a
// [Session] carries only this one authority signal, so an unset
// AuthorityState here really is "unknown", not "known false", and discloses
// accordingly.
func (s AuthorityState) noticeworthy() bool {
	switch s {
	case AuthorityStateSelf:
		return false
	case AuthorityStateDelegated, AuthorityStateViewAs, AuthorityStateElevated, AuthorityStateBreakGlass:
		return true
	default:
		return true
	}
}

// displayLabel renders a known, non-empty AuthorityState as user-facing
// text. The empty AuthorityState renders no label at all -- it means no
// caller has wired this field yet, not that something unexpected arrived --
// while a genuinely garbled non-empty value (anything [noticeworthy]
// discloses for but this switch does not otherwise name) still needs the
// strip to say something concrete rather than silently matching the empty
// case, but never by echoing the raw, unparsed wire value itself: that
// value is not vetted display text.
func (s AuthorityState) displayLabel() string {
	switch s {
	case AuthorityStateDelegated:
		return "Delegated"
	case AuthorityStateViewAs:
		return "View as"
	case AuthorityStateElevated:
		return "Elevated"
	case AuthorityStateBreakGlass:
		return "Break glass"
	case AuthorityStateSelf, AuthorityState(""):
		return ""
	default:
		return "Additional authority (unrecognized)"
	}
}

// SessionStrip renders s as the shell's authority-context region.
//
// A zero Session (see [Session.IsZero]) renders nothing at all, matching the
// page renderer's LiveRegionOff behavior: a session the reader carries no
// display fact for is not the same as a broken renderer, so it is an
// explicit empty fragment rather than a strip full of blank labels.
//
// UXAUDIT-007: beyond that, the strip stays quiet for AuthorityStateSelf --
// ordinary self context -- and discloses only for delegated, view-as,
// elevated, break-glass, or an authority state this reader does not
// recognize at all (see [AuthorityState.noticeworthy]). Purpose is
// deliberately never rendered here: task-specific review context belongs
// inside the affected workflow, not this persistent, page-independent
// region.
func SessionStrip(s Session) ui.Node {
	if s.IsZero() {
		return html.Fragment()
	}
	if !s.AuthorityState.noticeworthy() {
		return html.Fragment()
	}
	var items []ui.Node
	if s.Subject != "" {
		items = append(items, labelledListItem("Acting as", s.Subject))
	}
	if s.Tenant != "" {
		items = append(items, labelledListItem("Tenant", s.Tenant))
	}
	if len(s.Roles) > 0 {
		items = append(items, labelledListItem("Roles", strings.Join(s.Roles, ", ")))
	}
	if label := s.AuthorityState.displayLabel(); label != "" {
		items = append(items, labelledListItem("Authority", label))
	}
	return html.Section(html.Props{ID: AuthorityElementID, Aria: map[string]string{"label": "Authority context"}},
		html.Ul(html.Props{}, items...),
	)
}

func labelledListItem(label, value string) ui.Node {
	return html.Li(html.Props{},
		html.Strong(html.Props{}, ui.Text(label+": ")),
		ui.Text(value),
	)
}
