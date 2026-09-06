package operations

import (
	"strings"
	"testing"
	"time"
)

var ops008Now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func TestTodo_OPS_008(t *testing.T) {
	result := Rehearse(PlaceholderRunbooks(), PlaceholderRehearsals(), ops008Now)
	if !result.Ready() {
		t.Fatalf("OPS-008 result = %s, want READY: %v", result.Status, result.Diagnostics)
	}
	if result.Runbooks != 1 || result.Cases != 1 || !result.Effects.Empty() {
		t.Fatalf("unexpected rehearsal result: %+v", result)
	}
}

func TestTodo_OPS_008_Conformance(t *testing.T) {
	runbooks := PlaceholderRunbooks()
	runbooks[0].Containment = ""
	result := Rehearse(runbooks, PlaceholderRehearsals(), ops008Now)
	if result.Status != StatusRehearsalRejected || !hasOps008Diagnostic(result.Diagnostics, "containment", "MISSING") {
		t.Fatalf("missing containment was admitted: %+v", result)
	}
	runbooks = PlaceholderRunbooks()
	runbooks[0].RollbackOrDegrade = ""
	result = Rehearse(runbooks, PlaceholderRehearsals(), ops008Now)
	if result.Status != StatusRehearsalRejected || !hasOps008Diagnostic(result.Diagnostics, "rollback_or_degrade", "MISSING") {
		t.Fatalf("missing rollback/degrade was admitted: %+v", result)
	}
}

func TestTodo_OPS_008_Fault(t *testing.T) {
	cases := PlaceholderRehearsals()
	cases[0].AcknowledgedAt = cases[0].TriggeredAt.Add(6 * time.Minute)
	result := Rehearse(PlaceholderRunbooks(), cases, ops008Now)
	if result.Status != StatusRehearsalRejected || !hasOps008Diagnostic(result.Diagnostics, "acknowledgement", "TARGET_BREACHED") {
		t.Fatalf("late acknowledgement was admitted: %+v", result)
	}
	if !result.Effects.Empty() {
		t.Fatalf("rejected rehearsal had effects: %+v", result.Effects)
	}

	runbooks := PlaceholderRunbooks()
	runbooks[0].ExpiresAt = ops008Now
	result = Rehearse(runbooks, PlaceholderRehearsals(), ops008Now)
	if result.Status != StatusRehearsalRejected || !hasOps008Diagnostic(result.Diagnostics, "expires_at", "EXPIRED") {
		t.Fatalf("expired runbook was admitted: %+v", result)
	}
}

func TestOperations008ContractNames(t *testing.T) {
	if Version() != OPS008SchemaVersion || !strings.Contains(Explain(), "OPS-008") {
		t.Fatalf("contract description = %q", Explain())
	}
}

func hasOps008Diagnostic(diagnostics []RehearsalDiagnostic, field, state string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Field == field && diagnostic.State == state {
			return true
		}
	}
	return false
}
