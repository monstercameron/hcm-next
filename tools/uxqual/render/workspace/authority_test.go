package workspace

import (
	"strings"
	"testing"
)

func TestSessionStripZeroSessionRendersNothing(t *testing.T) {
	out := renderNode(t, SessionStrip(Session{}))
	if out != "" {
		t.Errorf("SessionStrip(zero Session) = %q, want an empty render", out)
	}
}

func TestSessionStripRendersEveryPresentField(t *testing.T) {
	s := Session{
		Tenant:  "northwind",
		Subject: "avery.okafor@northwind.example",
		Roles:   []string{"hr.business_partner", "promotion.approver"},
		Purpose: "promotion_review",
	}
	out := renderNode(t, SessionStrip(s))
	for _, want := range []string{
		`id="` + AuthorityElementID + `"`,
		`aria-label="Authority context"`,
		"avery.okafor@northwind.example",
		"northwind",
		"hr.business_partner, promotion.approver",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SessionStrip missing %q: %s", want, out)
		}
	}
	// UXAUDIT-007: Purpose is task-specific review context, not an acting-
	// authority fact, so this persistent, page-independent region never
	// renders it -- this case previously asserted the opposite (that
	// "promotion_review" appears here), which was the same pattern the live
	// audit flagged as RED for the production shell's "Compensation Review"
	// badge. Purpose belongs inside the affected workflow page instead.
	if strings.Contains(out, "promotion_review") {
		t.Errorf("SessionStrip rendered task-specific Purpose in the global authority strip: %s", out)
	}
}

func TestSessionStripOmitsFieldsNotPresent(t *testing.T) {
	out := renderNode(t, SessionStrip(Session{Subject: "avery.okafor@northwind.example"}))
	if !strings.Contains(out, "Acting as") {
		t.Errorf("SessionStrip omitted the one present field: %s", out)
	}
	for _, absent := range []string{"Tenant:", "Purpose:", "Roles:"} {
		if strings.Contains(out, absent) {
			t.Errorf("SessionStrip rendered a label for an absent field %q: %s", absent, out)
		}
	}
}

// UXAUDIT-007: ordinary self authority is quiet even when other display
// facts (subject, tenant, roles) are fully known. Self is a known, safe
// answer, not "nothing to say".
func TestSessionStripSelfAuthorityIsQuiet(t *testing.T) {
	s := Session{
		Tenant:         "northwind",
		Subject:        "avery.okafor@northwind.example",
		Roles:          []string{"hr.business_partner"},
		AuthorityState: AuthorityStateSelf,
	}
	out := renderNode(t, SessionStrip(s))
	if out != "" {
		t.Errorf("SessionStrip(self authority) = %q, want an empty render", out)
	}
}

// Each named non-self state discloses and names itself.
func TestSessionStripDisclosesNamedAuthorityStates(t *testing.T) {
	cases := map[AuthorityState]string{
		AuthorityStateDelegated:  "Delegated",
		AuthorityStateViewAs:     "View as",
		AuthorityStateElevated:   "Elevated",
		AuthorityStateBreakGlass: "Break glass",
	}
	for state, label := range cases {
		s := Session{Subject: "avery.okafor@northwind.example", AuthorityState: state}
		out := renderNode(t, SessionStrip(s))
		if !strings.Contains(out, "Authority: ") || !strings.Contains(out, label) {
			t.Errorf("state %q: SessionStrip = %q, want an Authority label naming %q", state, out, label)
		}
	}
}

// An authority state this reader has never been taught -- not any of the
// four named non-self states, and not empty -- still discloses rather than
// being silently dropped, and it never echoes the raw unparsed value: a
// safe, generic disclosure stands in for it instead.
func TestSessionStripUnrecognizedAuthorityStateStillDiscloses(t *testing.T) {
	s := Session{Subject: "avery.okafor@northwind.example", AuthorityState: AuthorityState("quantum_delegation_v2")}
	out := renderNode(t, SessionStrip(s))
	if out == "" {
		t.Fatal("SessionStrip(unrecognized authority state) rendered nothing, want a disclosure")
	}
	if strings.Contains(out, "quantum_delegation_v2") {
		t.Errorf("SessionStrip echoed the raw unrecognized state string verbatim: %s", out)
	}
	if !strings.Contains(out, "Additional authority (unrecognized)") {
		t.Errorf("SessionStrip did not disclose the unrecognized state: %s", out)
	}
}

// A Session with nothing to say except a resolved authority state is not
// IsZero, and that state still gets to disclose: an authority-only
// projection cannot be swallowed by the "nothing known" short circuit.
func TestSessionStripAuthorityOnlySessionStillDiscloses(t *testing.T) {
	s := Session{AuthorityState: AuthorityStateElevated}
	if s.IsZero() {
		t.Fatal("Session with only AuthorityState set reports IsZero() = true")
	}
	out := renderNode(t, SessionStrip(s))
	if !strings.Contains(out, "Elevated") {
		t.Errorf("SessionStrip(authority-only session) = %q, want it to disclose Elevated", out)
	}
}

// Direct proof the enum is exhaustive with no permissive default: the one
// safe value is quiet, and every other input -- named, empty, or garbled --
// discloses.
func TestAuthorityStateNoticeworthyHasNoPermissiveDefault(t *testing.T) {
	if AuthorityStateSelf.noticeworthy() {
		t.Error("AuthorityStateSelf.noticeworthy() = true, want false")
	}
	for _, state := range []AuthorityState{
		AuthorityStateDelegated, AuthorityStateViewAs, AuthorityStateElevated, AuthorityStateBreakGlass,
		AuthorityState(""), AuthorityState("quantum_delegation_v2"), AuthorityState("SELF"),
	} {
		if !state.noticeworthy() {
			t.Errorf("AuthorityState(%q).noticeworthy() = false, want true (fail open)", state)
		}
	}
}
