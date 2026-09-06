package incidentstate

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func incidentFixture(t *testing.T) Incident {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	in, err := New("inc-1", "tenant-a", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func advance(t *testing.T, in Incident, to State, now time.Time, extra Command) Incident {
	t.Helper()
	extra.To, extra.Actor, extra.Reason, extra.EvidenceRef = to, "on-call", "verified transition", "evidence-"+strings.ToLower(string(to))
	out, err := Transition(in, extra, now)
	if err != nil {
		t.Fatalf("%s: %v", to, err)
	}
	return out
}

func TestTodo_OPS_004(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	in := incidentFixture(t)
	in = advance(t, in, Triaged, now, Command{})
	in = advance(t, in, Declared, now, Command{Owner: "ops", Affected: AffectedSet{Known: true, Verified: true, Facts: []AffectedFact{{TenantID: "tenant-a", Kind: "capability", Value: "promotion"}}}})
	in = advance(t, in, Mitigating, now, Command{Mitigation: "route to safe read-only path", RepairLink: "repair-1"})
	monitoringUntil := now.Add(time.Hour)
	in = advance(t, in, Monitoring, now, Command{MonitoringUntil: monitoringUntil})
	in = advance(t, in, Resolved, monitoringUntil, Command{})
	in = advance(t, in, Reviewed, monitoringUntil.Add(time.Minute), Command{Review: "review complete"})
	if in.State != Reviewed || in.Version != 7 || len(in.Timeline) != 7 {
		t.Fatalf("incident=%+v timeline=%+v", in, in.Timeline)
	}
}

func TestTodo_OPS_004_Property(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	in := incidentFixture(t)
	_, err := Transition(in, Command{To: Resolved, Actor: "on-call", Reason: "close", EvidenceRef: "e"}, now)
	if !errors.Is(err, ErrTransitionInvalid) && !errors.Is(err, ErrClosureIncomplete) {
		t.Fatalf("bad closure error=%v", err)
	}
	if in.State != Detected || in.Version != 1 || len(in.Timeline) != 1 {
		t.Fatal("invalid transition mutated incident")
	}
}

func TestTodo_OPS_004_Golden(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	in := incidentFixture(t)
	got, err := Transition(in, Command{To: Triaged, Actor: "on-call", Reason: "correlated", EvidenceRef: "signal-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Timeline[1].From != Detected || got.Timeline[1].To != Triaged || got.Timeline[1].EvidenceRef != "signal-1" {
		t.Fatalf("event=%+v", got.Timeline[1])
	}
	if !strings.Contains(Explain(got), "state=TRIAGED") {
		t.Fatal("explain must identify current state")
	}
}
