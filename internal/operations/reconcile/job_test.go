package reconcile

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

func TestStatus_ValidIsExactlyTheClosedSet(t *testing.T) {
	want := []Status{
		StatusPending, StatusObserving, StatusPass, StatusMismatch,
		StatusPartial, StatusUnknown, StatusExpired, StatusRepairRequired,
	}
	if len(want) != 8 {
		t.Fatalf("test declares %d statuses, RECON-001 names 8", len(want))
	}
	for _, s := range want {
		if !s.Valid() {
			t.Fatalf("%q is not reported valid", s)
		}
	}
	for _, s := range []Status{"", "BOGUS", "passed", "Pass"} {
		if s.Valid() {
			t.Fatalf("%q is reported valid; it is not one of the declared eight", s)
		}
	}
}

func TestStatus_TerminalSplitsRestingFromClosed(t *testing.T) {
	resting := []Status{StatusPending, StatusObserving, StatusUnknown}
	closed := []Status{StatusPass, StatusMismatch, StatusPartial, StatusExpired, StatusRepairRequired}
	for _, s := range resting {
		if s.Terminal() {
			t.Fatalf("%q reported terminal; it must stay due", s)
		}
	}
	for _, s := range closed {
		if !s.Terminal() {
			t.Fatalf("%q reported non-terminal; it must never be polled again", s)
		}
	}
}

func TestExhaustStatus_RepairPolicyDecidesExpiredVsRepairRequired(t *testing.T) {
	cases := []struct {
		policy string
		want   Status
	}{
		{"", StatusExpired},
		{"NONE", StatusExpired},
		{"hcmnext.repair.reissue_provider_call/v1", StatusRepairRequired},
	}
	for _, c := range cases {
		if got := exhaustStatus(c.policy); got != c.want {
			t.Fatalf("exhaustStatus(%q) = %q, want %q", c.policy, got, c.want)
		}
	}
}

func TestValidVerdictStatus_OnlyTheFourComparisonOutcomes(t *testing.T) {
	for _, s := range []Status{StatusPass, StatusMismatch, StatusPartial, StatusUnknown} {
		if !validVerdictStatus(s) {
			t.Fatalf("%q must be an accepted comparer verdict", s)
		}
	}
	for _, s := range []Status{StatusPending, StatusObserving, StatusExpired, StatusRepairRequired, Status("BOGUS")} {
		if validVerdictStatus(s) {
			t.Fatalf("%q must not be an accepted comparer verdict; it is a job-lifecycle-only status", s)
		}
	}
}

func TestMeetsFreshness_OrdersFromMostToLeastReliable(t *testing.T) {
	cases := []struct {
		got, required observe.Freshness
		want          bool
	}{
		{observe.FreshnessFresh, observe.FreshnessFresh, true},
		{observe.FreshnessStale, observe.FreshnessFresh, false},
		{observe.FreshnessFresh, observe.FreshnessStale, true},
		{observe.FreshnessStale, observe.FreshnessStale, true},
		{observe.FreshnessUnavailable, observe.FreshnessUnavailable, true},
		{observe.FreshnessUnknown, observe.FreshnessStale, false},
		{observe.FreshnessPartial, observe.FreshnessUnknown, false},
	}
	for _, c := range cases {
		if got := meetsFreshness(c.got, c.required); got != c.want {
			t.Fatalf("meetsFreshness(%q, %q) = %v, want %v", c.got, c.required, got, c.want)
		}
	}
}

func TestMeetsFreshness_UnrecognisedValueNeverMeetsAnyRequirement(t *testing.T) {
	if meetsFreshness(observe.Freshness("BOGUS"), observe.FreshnessUnavailable) {
		t.Fatal("an unrecognised freshness value satisfied even the most lenient requirement")
	}
}
