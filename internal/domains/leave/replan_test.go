package leave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func replanPrior() LeaveEntitlementPlan {
	request := PlanRequest{
		StartDay: 10, EndDay: 17, HoursPerDay: 8, BalanceAvailable: 72,
		Programs: []ProgramResult{
			{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", RuleID: "tenure-12mo", RuleVersion: "v3", Result: ProgramEligible, Obligations: []string{"protect:job"}},
		},
		Timezone: "America/Los_Angeles",
	}
	plan, err := ComposePlan(request)
	if err != nil {
		panic(err)
	}
	return plan
}

func TestTodo_LEAVE_007(t *testing.T) {
	prior := replanPrior()
	if prior.PlannedDebits != 64 {
		t.Fatalf("prior debits = %d, want 64", prior.PlannedDebits)
	}
	// GREEN example: PTO 72→56 creates the successor with additional
	// unpaid hours and explicit pay treatment.
	replan, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, []string{"protect:job"}, "retain-unaffected")
	if err != nil {
		t.Fatalf("ReplanLeave: %v", err)
	}
	if replan.AdditionalUnpaid != 16 || replan.PaidDelta != -16 {
		t.Fatalf("replan=%+v", replan)
	}
	if replan.Route != ReplanReapproval {
		t.Fatalf("route = %q, want reapproval for pay movement", replan.Route)
	}
	if replan.NewDigest == "" || replan.NewDigest == replan.PriorDigest {
		t.Fatal("successor must advance past the old digest")
	}
	if len(replan.RetainedSegments) != 6 || len(replan.RetainedDecisions) != 1 {
		t.Fatalf("retained segments=%v decisions=%v", replan.RetainedSegments, replan.RetainedDecisions)
	}
	if err := replan.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// An intervening debit never denies otherwise valid leave: protection
	// survives, only pay treatment moves.
	for _, segment := range replan.Segments {
		if segment.Kind != SegmentPaid && segment.Kind != SegmentUnpaid {
			t.Fatalf("segment %+v lost protection", segment)
		}
	}
	// RED: hollow policy, hollow funding and unsealed priors refuse.
	if _, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, nil, ""); err == nil {
		t.Fatal("policy-free replan retained decisions")
	}
	if _, err := ReplanLeave(prior, []FundingChange{{SourceID: "", OldAvailable: 72, NewAvailable: 56}}, nil, "retain-unaffected"); err == nil {
		t.Fatal("sourceless funding replanned")
	}
	if _, err := ReplanLeave(LeaveEntitlementPlan{}, nil, nil, "retain-unaffected"); err == nil {
		t.Fatal("unsealed prior replanned")
	}
	// A shortfall beyond convertible paid time refuses instead of
	// silently changing pay.
	if _, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 0, NewRevision: "rev-2"}}, nil, "retain-unaffected"); err == nil {
		t.Fatal("uncoverable shortfall replanned")
	}
}

func TestTodo_LEAVE_007_Property(t *testing.T) {
	prior := replanPrior()
	first, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, nil, "retain-unaffected")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, nil, "retain-unaffected")
	if err != nil || first.NewDigest != second.NewDigest {
		t.Fatal("replan is not deterministic")
	}
	// No drift routes nothing and keeps every segment.
	steady, err := ReplanLeave(prior, nil, []string{"protect:job"}, "retain-unaffected")
	if err != nil {
		t.Fatal(err)
	}
	if steady.Route != ReplanNone || steady.PaidDelta != 0 || steady.AdditionalUnpaid != 0 {
		t.Fatalf("steady=%+v", steady)
	}
	// Paid treatment and entitlement stay separate dimensions.
	if steady.EntitlementDelta != 0 {
		t.Fatalf("entitlement moved without entitlement drift: %+v", steady)
	}
}

func TestTodo_LEAVE_007_Golden(t *testing.T) {
	prior := replanPrior()
	replan, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, []string{"protect:job"}, "retain-unaffected")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"prior=" + replan.PriorDigest,
		"successor=" + replan.NewDigest,
		"paid-delta=-16 additional-unpaid=16 route=reapproval",
		"retained=" + strings.Join(replan.RetainedSegments, ","),
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave007_replan.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_LEAVE_007_Conformance(t *testing.T) {
	prior := replanPrior()
	replan, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, []string{"protect:job"}, "retain-unaffected")
	if err != nil {
		t.Fatal(err)
	}
	// Segments still tile the interval with no overlap.
	day := 10
	for i, segment := range replan.Segments {
		if segment.StartDay != day || segment.EndDay != day {
			t.Fatalf("segment %d breaks tiling: %+v", i, segment)
		}
		day++
	}
	if day != 18 {
		t.Fatal("replan does not cover the interval")
	}
	// Converted segments name no program: protection accounting never
	// double-counts.
	paid, unpaid := 0, 0
	for _, segment := range replan.Segments {
		switch segment.Kind {
		case SegmentPaid:
			paid++
			if len(segment.Programs) == 0 {
				t.Fatalf("paid segment %+v names no program", segment)
			}
		case SegmentUnpaid:
			unpaid++
			if len(segment.Programs) != 0 {
				t.Fatalf("unpaid segment %+v still names programs", segment)
			}
		}
	}
	if paid != 6 || unpaid != 2 {
		t.Fatalf("paid=%d unpaid=%d", paid, unpaid)
	}
}

func TestTodo_LEAVE_007_Mutation(t *testing.T) {
	prior := replanPrior()
	base, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}}, nil, "retain-unaffected")
	if err != nil {
		t.Fatal(err)
	}
	// A deeper cut converts more hours with a new seal.
	deeper, err := ReplanLeave(prior, []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 48, NewRevision: "rev-3"}}, nil, "retain-unaffected")
	if err != nil {
		t.Fatal(err)
	}
	if deeper.AdditionalUnpaid != 24 || deeper.NewDigest == base.NewDigest {
		t.Fatalf("deeper=%+v", deeper)
	}
	// Forged replans never verify.
	forged := base
	forged.PaidDelta = 0
	if err := forged.Verify(); err == nil {
		t.Fatal("forged replan verified")
	}
}
