package leave

import (
	"sync"
	"testing"
)

func planRequest() PlanRequest {
	return PlanRequest{
		StartDay: 10, EndDay: 14, HoursPerDay: 8, BalanceAvailable: 80,
		HolidayDays: []int{12},
		Programs: []ProgramResult{
			{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", RuleID: "tenure-12mo", RuleVersion: "v3", Result: ProgramEligible, Obligations: []string{"protect:job"}},
			{ProgramID: "state-paid", Authority: "state", Release: "ca-2026.1", RuleID: "hours-1250", RuleVersion: "v1", Result: ProgramEligible, Obligations: []string{"pay:benefit"}},
		},
		Timezone: "America/Los_Angeles",
	}
}

func TestTodo_LEAVE_005(t *testing.T) {
	plan, err := ComposePlan(planRequest())
	if err != nil {
		t.Fatalf("ComposePlan: %v", err)
	}
	// Five days tile exactly with no overlap.
	if len(plan.Segments) != 5 {
		t.Fatalf("segments = %d, want 5", len(plan.Segments))
	}
	for i, segment := range plan.Segments {
		if segment.StartDay != 10+i || segment.EndDay != 10+i {
			t.Fatalf("segment %d = %+v", i, segment)
		}
	}
	// Holiday is an explicit unpaid segment; the rest are paid with both
	// programs identified and protection never double-counted.
	kinds := map[int]string{}
	for _, segment := range plan.Segments {
		kinds[segment.StartDay] = segment.Kind
	}
	if kinds[12] != SegmentUnpaid {
		t.Fatalf("holiday kind = %q", kinds[12])
	}
	for _, day := range []int{10, 11, 13, 14} {
		if kinds[day] != SegmentPaid {
			t.Fatalf("day %d kind = %q", day, kinds[day])
		}
	}
	if plan.ScheduledHours != 40 || plan.PlannedDebits != 32 {
		t.Fatalf("hours=%d debits=%d", plan.ScheduledHours, plan.PlannedDebits)
	}
	if len(plan.Obligations) != 2 || len(plan.Effects) != 3 {
		t.Fatalf("obligations=%v effects=%v", plan.Obligations, plan.Effects)
	}
	if err := plan.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Unknown program results stay unknown, never unpaid.
	blind := planRequest()
	blind.Programs = append(blind.Programs, ProgramResult{ProgramID: "company-parental", Authority: "company", Release: "h", RuleID: "r", RuleVersion: "v", Result: ProgramUnknown})
	unknown, err := ComposePlan(blind)
	if err != nil {
		t.Fatal(err)
	}
	for _, segment := range unknown.Segments {
		if segment.StartDay == 12 {
			continue
		}
		if segment.Kind != SegmentUnknown {
			t.Fatalf("unknown program planned as %q", segment.Kind)
		}
	}
	// RED: debits beyond balance and hollow requests refuse.
	broke := planRequest()
	broke.BalanceAvailable = 8
	if _, err := ComposePlan(broke); err == nil {
		t.Fatal("over-balance plan composed")
	}
	inverted := planRequest()
	inverted.EndDay = 9
	if _, err := ComposePlan(inverted); err == nil {
		t.Fatal("inverted interval composed")
	}
	rogue := planRequest()
	rogue.Programs[0].Result = "EVAPORATED"
	if _, err := ComposePlan(rogue); err == nil {
		t.Fatal("off-vocabulary program result planned")
	}
}

func TestTodo_LEAVE_005_Property(t *testing.T) {
	first, err := ComposePlan(planRequest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposePlan(planRequest())
	if err != nil || first.Digest != second.Digest {
		t.Fatal("plan is not deterministic")
	}
	// Segments always tile the requested interval exactly.
	day := planRequest().StartDay
	for i, segment := range first.Segments {
		if segment.StartDay != day || segment.EndDay != day {
			t.Fatalf("segment %d breaks tiling: %+v", i, segment)
		}
		day++
	}
	if day != planRequest().EndDay+1 {
		t.Fatal("segments do not cover the interval")
	}
	// Timezone binds the plan: rezoning is explicit.
	rezoned := planRequest()
	rezoned.Timezone = "America/New_York"
	moved, err := ComposePlan(rezoned)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Digest == first.Digest {
		t.Fatal("timezone change kept the plan digest")
	}
}

func TestTodo_LEAVE_005_Race(t *testing.T) {
	registry := NewPlanRegistry()
	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plan, err := ComposePlan(planRequest())
			if err != nil {
				errs[i] = err
				return
			}
			if err := plan.Verify(); err != nil {
				errs[i] = err
				return
			}
			errs[i] = registry.Record("req-1", plan)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
	}
}

func TestTodo_LEAVE_005_Conformance(t *testing.T) {
	plan, err := ComposePlan(planRequest())
	if err != nil {
		t.Fatal(err)
	}
	// Concurrent programs are identified on every paid segment.
	for _, segment := range plan.Segments {
		if segment.Kind == SegmentPaid && len(segment.Programs) != 2 {
			t.Fatalf("paid segment %+v hides a concurrent program", segment)
		}
	}
	// Effect plans cover payroll, benefits and WFM without posting.
	systems := map[string]bool{}
	for _, effect := range plan.Effects {
		systems[effect.System] = true
	}
	for _, system := range []string{"payroll", "benefits", "wfm"} {
		if !systems[system] {
			t.Fatalf("effect plan misses %s", system)
		}
	}
}

func TestTodo_LEAVE_005_Mutation(t *testing.T) {
	base, err := ComposePlan(planRequest())
	if err != nil {
		t.Fatal(err)
	}
	// Holiday shift reshapes the partition with a new seal.
	moved := planRequest()
	moved.HolidayDays = []int{13}
	reshaped, err := ComposePlan(moved)
	if err != nil {
		t.Fatal(err)
	}
	if reshaped.Digest == base.Digest {
		t.Fatal("holiday mutation kept the plan digest")
	}
	for _, segment := range reshaped.Segments {
		if segment.StartDay == 13 && segment.Kind != SegmentUnpaid {
			t.Fatalf("moved holiday planned as %q", segment.Kind)
		}
		if segment.StartDay == 12 && segment.Kind != SegmentPaid {
			t.Fatalf("former holiday planned as %q", segment.Kind)
		}
	}
	// Program loss flips paid segments to unpaid.
	lost := planRequest()
	lost.Programs = nil
	bare, err := ComposePlan(lost)
	if err != nil {
		t.Fatal(err)
	}
	for _, segment := range bare.Segments {
		if segment.StartDay != 12 && segment.Kind != SegmentUnpaid {
			t.Fatalf("unprotected day planned as %q", segment.Kind)
		}
	}
	// Broken tiling never verifies.
	broken := base
	broken.Segments[1].StartDay = broken.Segments[0].StartDay
	if err := broken.Verify(); err == nil {
		t.Fatal("overlapping plan verified")
	}
}
