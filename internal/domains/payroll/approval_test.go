package payroll

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/sod"
)

func approvalRequestFixture(t *testing.T) ApprovalRequest {
	t.Helper()
	calculated, err := validPayrollRun(t).Calculate("sha256:calculation")
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	context, err := NewApprovalContext(calculated, "sha256:totals", "sha256:exceptions", "sha256:rules")
	if err != nil {
		t.Fatalf("NewApprovalContext: %v", err)
	}
	return ApprovalRequest{
		Context:    context,
		Requester:  sod.Actor{Subject: "principal:requester"},
		Calculator: sod.Actor{Subject: "principal:calculator"},
		Releaser:   sod.Actor{Subject: "principal:releaser"},
		Approvers: []sod.Actor{
			{Subject: "principal:approver-a"},
			{Subject: "principal:approver-b"},
			{Subject: "principal:approver-c"},
		},
		Policy: NewApprovalPolicy("policy.payroll.separation/2026.1", 2),
	}
}

func cloneApprovalRequest(in ApprovalRequest) ApprovalRequest {
	out := in
	out.Approvers = append([]sod.Actor(nil), in.Approvers...)
	for i := range out.Approvers {
		out.Approvers[i].DelegationChain = append([]string(nil), in.Approvers[i].DelegationChain...)
	}
	out.Requester.DelegationChain = append([]string(nil), in.Requester.DelegationChain...)
	out.Calculator.DelegationChain = append([]string(nil), in.Calculator.DelegationChain...)
	out.Releaser.DelegationChain = append([]string(nil), in.Releaser.DelegationChain...)
	return out
}

// TestTodo_PAYRUN_006 proves exact-result approval and lock binding with
// requester, calculator, releaser, and quorum separation.
func TestTodo_PAYRUN_006(t *testing.T) {
	request := approvalRequestFixture(t)
	approval, err := ApprovePayroll(request)
	if err != nil {
		t.Fatalf("ApprovePayroll: %v", err)
	}
	locked, err := LockPayroll(approval, request)
	if err != nil {
		t.Fatalf("LockPayroll: %v", err)
	}
	if locked.RunID != request.Context.Run.RunID || locked.RunRevision != request.Context.Run.Revision {
		t.Fatalf("lock lost run identity: %+v", locked)
	}
	if locked.BasisDigest != approval.BasisDigest || locked.ApprovalDigest != approval.ApprovalDigest {
		t.Fatal("lock did not bind the approval and exact basis")
	}
	if len(approval.ApproverSubjects) != 3 || approval.Quorum != 2 {
		t.Fatalf("unexpected quorum evidence: %+v", approval)
	}
	if _, err := approval.Explain(); err != nil {
		t.Fatalf("Explain: %v", err)
	}
}

// TestTodo_PAYRUN_006_Property proves each material payroll digest is part of
// the approval binding and cannot be replaced at lock time.
func TestTodo_PAYRUN_006_Property(t *testing.T) {
	base := approvalRequestFixture(t)
	approval, err := ApprovePayroll(base)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*ApprovalRequest)
	}{
		{"totals", func(r *ApprovalRequest) { r.Context.TotalsDigest = "sha256:totals-changed" }},
		{"exceptions", func(r *ApprovalRequest) { r.Context.ExceptionsDigest = "sha256:exceptions-changed" }},
		{"rules", func(r *ApprovalRequest) { r.Context.RulesDigest = "sha256:rules-changed" }},
		{"population", func(r *ApprovalRequest) {
			r.Context.PopulationDigest = "sha256:population-changed"
			r.Context.Run.Population.Digest = "sha256:population-changed"
			r.Context.Run.CanonicalDigest = r.Context.Run.computedDigest()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneApprovalRequest(base)
			tc.mutate(&changed)
			if _, err := LockPayroll(approval, changed); !errors.Is(err, ErrApprovalStale) {
				t.Fatalf("LockPayroll error = %v, want ErrApprovalStale", err)
			}
		})
	}
}

// TestTodo_PAYRUN_006_Golden proves repeated construction emits byte-identical
// approval evidence for the same server-held request.
func TestTodo_PAYRUN_006_Golden(t *testing.T) {
	first, err := ApprovePayroll(approvalRequestFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ApprovePayroll(approvalRequestFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.ApprovalDigest != second.ApprovalDigest || !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("identical approval inputs did not produce identical canonical evidence")
	}
	if first.ApprovalID != "payroll-approval/"+first.RequestDigest {
		t.Fatalf("approval id is not request-bound: %s", first.ApprovalID)
	}
}

// TestTodo_PAYRUN_006_Race proves independent approval calculations are safe
// and deterministic when concurrent callers share one request value.
func TestTodo_PAYRUN_006_Race(t *testing.T) {
	request := approvalRequestFixture(t)
	const workers = 24
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			approval, err := ApprovePayroll(request)
			if err != nil {
				errs <- err
				return
			}
			results <- approval.ApprovalDigest
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var want string
	for got := range results {
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("concurrent digest = %s, want %s", got, want)
		}
	}
}

// TestTodo_PAYRUN_006_Integration proves the public approve/lock seam can be
// composed without persistence or an implicit effect.
func TestTodo_PAYRUN_006_Integration(t *testing.T) {
	request := approvalRequestFixture(t)
	approval, err := Approve(request)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := Lock(approval, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := locked.Validate(); err != nil {
		t.Fatalf("locked payroll invalid: %v", err)
	}
	if request.Context.Run.State != Calculated {
		t.Fatal("pure lock changed the source run")
	}
}

// TestTodo_PAYRUN_006_Fault proves malformed roles, state, and insufficient
// quorum fail before any approval or lock value is returned.
func TestTodo_PAYRUN_006_Fault(t *testing.T) {
	request := approvalRequestFixture(t)
	request.Requester.Subject = request.Calculator.Subject
	if _, err := ApprovePayroll(request); err == nil {
		t.Fatal("requester/calculator collision was accepted")
	}
	request = approvalRequestFixture(t)
	request.Policy.Quorum = 4
	if _, err := ApprovePayroll(request); !errors.Is(err, sod.ErrUnsatisfiable) {
		t.Fatalf("quorum error = %v, want sod.ErrUnsatisfiable", err)
	}
	request = approvalRequestFixture(t)
	request.Context.Run.State = Draft
	request.Context.Run.CanonicalDigest = ""
	if _, err := ApprovePayroll(request); err == nil {
		t.Fatal("draft payroll was accepted for approval")
	}
}

// TestTodo_PAYRUN_006_Mutation proves approval and lock self-digests detect
// tampering rather than silently accepting edited evidence.
func TestTodo_PAYRUN_006_Mutation(t *testing.T) {
	request := approvalRequestFixture(t)
	approval, err := ApprovePayroll(request)
	if err != nil {
		t.Fatal(err)
	}
	approval.BasisDigest = "sha256:tampered"
	if _, err := LockPayroll(approval, request); !errors.Is(err, ErrApprovalInvalid) {
		t.Fatalf("tampered approval error = %v, want ErrApprovalInvalid", err)
	}

	approval, err = ApprovePayroll(request)
	if err != nil {
		t.Fatal(err)
	}
	approval.ApproverSubjects[0] = "principal:tampered"
	if _, err := LockPayroll(approval, request); !errors.Is(err, ErrApprovalInvalid) {
		t.Fatalf("tampered approver error = %v, want ErrApprovalInvalid", err)
	}
}
