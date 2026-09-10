package leave

import (
	"sync/atomic"
	"testing"
	"time"

	wiredigest "github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var leaveStartCounter atomic.Int64

func leaveStartIDs() intent.IDSource {
	return func() (string, error) {
		return "plan:leave-start:" + itoa64(leaveStartCounter.Add(1)), nil
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func leaveStartInput() LeaveStartInput {
	return LeaveStartInput{
		Proposal: intent.ProposalRevision{
			ProposalRevisionID: "rev:leave:w1:1",
			IntentID:           "intent:leave:w1",
			MaterialDigest:     wiredigest.Reference{Digest: "material:leave:w1"},
			Tenant:             values.TenantId("acme"),
		},
		Definition: intent.Definition{
			Ref:          intent.Ref{TypeID: "hcmnext.leave.start", Version: 1},
			AllowedModes: []intent.Mode{intent.ModeSimulate},
		},
		EmploymentStatus: "ACTIVE",
		BalanceHead:      7,
		AvailabilityHead: 3,
		Governance: intent.GovernanceSnapshot{
			SnapshotDigest: "governance-1", AuthZDecision: "PERMIT",
			LegalDecision: "PERMIT", PolicyDecision: "PERMIT", RiskDecision: "ACCEPT",
		},
		Conflict: intent.ConflictSnapshot{
			SnapshotDigest: "conflict-1", FenceToken: "fence-1",
			FootprintRef: "leave_affected_fields_and_effective_interval/v1",
		},
		ExpiresAt: values.NewInstant(time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)),
		IDs:       leaveStartIDs(),
	}
}

func TestTodo_LEAVE_008(t *testing.T) {
	plan, err := CompileLeaveStart(leaveStartInput())
	if err != nil {
		t.Fatalf("CompileLeaveStart: %v", err)
	}
	// One admitted boundary: the four local streams.
	local := 0
	for _, participant := range plan.Participants {
		if participant.Local {
			local++
		}
	}
	if local != 4 {
		t.Fatalf("local participants = %d, want leave/absence/availability/balance", local)
	}
	// Exact balance entries ride the boundary with expected heads.
	if len(plan.Reads) != 2 || len(plan.Appends) != 2 {
		t.Fatalf("reads=%d appends=%d", len(plan.Reads), len(plan.Appends))
	}
	// Payroll, benefits and WFM stay ordered external effects with
	// observation and reconciliation policies.
	if len(plan.Effects) != 3 || len(plan.Observations) != 3 || len(plan.Compensations) != 3 {
		t.Fatalf("effects=%d observations=%d compensations=%d", len(plan.Effects), len(plan.Observations), len(plan.Compensations))
	}
	for _, effect := range plan.Effects {
		if effect.IdempotencyKey == "" {
			t.Fatalf("effect %+v has no idempotency key", effect)
		}
	}
	// Employment stays ACTIVE by precondition; the plan never executes.
	found := false
	for _, precondition := range plan.Preconditions {
		if precondition.Kind == "EMPLOYMENT_ACTIVE" {
			found = true
		}
	}
	if !found {
		t.Fatal("ACTIVE employment precondition missing")
	}
	if plan.Executable() {
		t.Fatal("leave-start plan reports executable")
	}
	if err := plan.AuthorizeExecution(); err == nil {
		t.Fatal("leave-start plan authorized execution")
	}
	// RED: terminated employment, overlaps, hollow heads and local
	// external systems refuse.
	terminated := leaveStartInput()
	terminated.EmploymentStatus = "TERMINATED"
	if _, err := CompileLeaveStart(terminated); err == nil {
		t.Fatal("terminated employment planned")
	}
	overlapped := leaveStartInput()
	overlapped.OverlappingLeaves = []string{"leave:other"}
	if _, err := CompileLeaveStart(overlapped); err == nil {
		t.Fatal("overlapping leave planned")
	}
	headless := leaveStartInput()
	headless.BalanceHead = 0
	if _, err := CompileLeaveStart(headless); err == nil {
		t.Fatal("headless plan compiled")
	}
}

func TestTodo_LEAVE_008_Property(t *testing.T) {
	first, err := CompileLeaveStart(leaveStartInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileLeaveStart(leaveStartInput())
	if err != nil {
		t.Fatal(err)
	}
	// Same semantic content compiles the same digest: IDs differ, content does not.
	if first.Digest != second.Digest {
		t.Fatal("identical leave starts compiled different digests")
	}
	if first.PlanID == second.PlanID {
		t.Fatal("compiled plans reused an identity")
	}
}

func TestTodo_LEAVE_008_Conformance(t *testing.T) {
	plan, err := CompileLeaveStart(leaveStartInput())
	if err != nil {
		t.Fatal(err)
	}
	// Every local stream the contract names is present exactly once.
	seen := map[string]int{}
	for _, participant := range plan.Participants {
		if participant.Local {
			seen[participant.StreamID]++
		}
	}
	for _, stream := range []string{"leave.record.w1", "leave.absence.w1", "leave.availability.w1", "leave.balance.w1"} {
		if seen[stream] != 1 {
			t.Fatalf("stream %s present %d times", stream, seen[stream])
		}
	}
	// Appends target only admitted local streams.
	for _, append := range plan.Appends {
		if seen[append.StreamID] != 1 {
			t.Fatalf("append targets unadmitted stream %q", append.StreamID)
		}
		if append.ExpectedSequence == 0 {
			t.Fatalf("append %+v lacks its expected head", append)
		}
	}
}

func TestTodo_LEAVE_008_Mutation(t *testing.T) {
	base, err := CompileLeaveStart(leaveStartInput())
	if err != nil {
		t.Fatal(err)
	}
	// Head movement recompiles with a new digest.
	moved := leaveStartInput()
	moved.BalanceHead = 8
	recompiled, err := CompileLeaveStart(moved)
	if err != nil {
		t.Fatal(err)
	}
	if recompiled.Digest == base.Digest {
		t.Fatal("head mutation kept the plan digest")
	}
	// Availability loss refuses: the plan cannot promise unknown coverage.
	lost := leaveStartInput()
	lost.AvailabilityHead = 0
	if _, err := CompileLeaveStart(lost); err == nil {
		t.Fatal("headless availability compiled")
	}
}
