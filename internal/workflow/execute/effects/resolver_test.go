package effects

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/workflow/prototype"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

type inertTx struct{}

func (inertTx) Exec(context.Context, string, ...any) (int64, error)        { return 0, nil }
func (inertTx) Query(context.Context, string, ...any) (dbport.Rows, error) { return nil, nil }
func (inertTx) QueryRow(context.Context, string, ...any) dbport.Row        { return nil }
func (inertTx) Commit(context.Context) error                               { return nil }
func (inertTx) Rollback(context.Context) error                             { return nil }

func TestPolicyResolver_TransactionalContractPreservesExactStaticPin(t *testing.T) {
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	pin := version.Pin{CompiledPlanDigest: plan.Digest()}
	r := PolicyResolver{Entries: []PolicyEntry{
		{WorkflowID: "not-selected", Plan: plan, Match: func(runtime.StartRequest) bool { return false }},
		{WorkflowID: plan.WorkflowID, Pin: pin, Plan: plan, Match: func(req runtime.StartRequest) bool { return req.CorrelationID == "selected" }},
	}}
	req := runtime.StartRequest{CorrelationID: "selected"}
	selection, err := r.ResolveWorkflowInTx(context.Background(), inertTx{}, req)
	if err != nil {
		t.Fatal(err)
	}
	if selection.WorkflowID != plan.WorkflowID || selection.Pin != pin || selection.Plan != plan {
		t.Fatalf("selection = %+v", selection)
	}
	if _, err := r.ResolveWorkflowInTx(context.Background(), nil, req); err == nil {
		t.Fatal("static resolver accepted no transaction boundary")
	}
	if _, err := r.ResolveWorkflow(context.Background(), runtime.StartRequest{CorrelationID: "unmatched"}); err == nil {
		t.Fatal("unmatched start defaulted to a policy entry")
	}
	for _, invalid := range []PolicyResolver{{Entries: []PolicyEntry{{Plan: plan}}}, {Entries: []PolicyEntry{{WorkflowID: "wf"}}}} {
		if _, err := invalid.ResolveWorkflow(context.Background(), runtime.StartRequest{}); err == nil {
			t.Fatal("invalid immutable policy entry was accepted")
		}
	}
}
