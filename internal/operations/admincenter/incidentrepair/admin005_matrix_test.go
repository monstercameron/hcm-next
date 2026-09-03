package incidentrepair

import (
	"errors"
	"testing"
	"time"
)

func fixture(state State) Incident {
	return Incident{ID: "i-1", TenantID: "t-1", State: state, Version: 4, Evidence: []Evidence{{ID: "ev-1", TenantID: "t-1"}}}
}
func auth(actions ...Action) Authorization {
	return Authorization{ActorID: "operator", TenantID: "t-1", AllowedActions: actions, ApproverID: "approver", CanCustomerView: true}
}

func TestTodoADMIN005Registry(t *testing.T) {
	r := Registry()
	if r.ID != "ADMIN-005" || r.Version == "" || !r.RedactionRequired || !r.RequiresSoD {
		t.Fatalf("bad registry: %+v", r)
	}
	want := []Action{ActionInspect, ActionTest, ActionRedrive, ActionReconcile, ActionDiff, ActionSimulate, ActionPromote, ActionRollback, ActionReopen, ActionMerge, ActionSplit}
	if len(r.Actions) != len(want) {
		t.Fatalf("actions=%v want=%v", r.Actions, want)
	}
	for i := range want {
		if r.Actions[i] != want[i] {
			t.Fatalf("actions=%v want=%v", r.Actions, want)
		}
	}
}

// TestTodo_ADMIN_005 is the named primary contract test from the delivery
// registry. It exercises the operator path through the lifecycle mutations.
func TestTodo_ADMIN_005(t *testing.T) {
	for _, tc := range []struct {
		state  State
		action Action
		want   State
	}{
		{StateResolved, ActionReopen, StateReopened},
		{StateDeclared, ActionMerge, StateMerged},
		{StateMonitoring, ActionSplit, StateSplit},
	} {
		r := ActionRequest{Action: tc.action, Incident: fixture(tc.state), Authorization: auth(tc.action), ExpectedVersion: 4, Reason: "verified operator action", IdempotencyKey: "primary-" + string(tc.action)}
		p, err := Plan(r)
		if err != nil {
			t.Fatalf("%s: unexpected planning error: %v", tc.action, err)
		}
		if p.From != tc.state || p.To != tc.want || !p.Mutates || !p.RequiresApproval {
			t.Fatalf("%s: plan=%+v", tc.action, p)
		}
	}
}

// TestTodo_ADMIN_005_Mutation proves that the mutation boundary rejects
// stale, unauthorized, self-approved, and evidence-free repair plans.
func TestTodo_ADMIN_005_Mutation(t *testing.T) {
	base := ActionRequest{Action: ActionPromote, Incident: fixture(StateDeclared), Authorization: auth(ActionPromote), ExpectedVersion: 4, Reason: "repair drift", IdempotencyKey: "mutation-1"}
	cases := []struct {
		name string
		edit func(*ActionRequest)
		want error
	}{
		{"stale plan", func(r *ActionRequest) { r.ExpectedVersion = 3 }, ErrStale},
		{"unauthorized", func(r *ActionRequest) { r.Authorization.AllowedActions = nil }, ErrUnauthorized},
		{"self approval", func(r *ActionRequest) { r.Authorization.ApproverID = r.Authorization.ActorID }, ErrSelfApproval},
		{"missing evidence", func(r *ActionRequest) { r.Incident.Evidence = nil }, ErrEvidenceRequired},
	}
	for _, tc := range cases {
		r := base
		r.Authorization.AllowedActions = append([]Action(nil), base.Authorization.AllowedActions...)
		tc.edit(&r)
		if _, err := Plan(r); !errors.Is(err, tc.want) {
			t.Errorf("%s: error=%v, want %v", tc.name, err, tc.want)
		}
	}
}
func TestTodoADMIN005LifecycleReopenMergeSplit(t *testing.T) {
	for _, tc := range []struct {
		s  State
		a  Action
		to State
	}{{StateResolved, ActionReopen, StateReopened}, {StateDeclared, ActionMerge, StateMerged}, {StateMonitoring, ActionSplit, StateSplit}} {
		p, e := Plan(ActionRequest{Action: tc.a, Incident: fixture(tc.s), Authorization: auth(tc.a), ExpectedVersion: 4, Reason: "governed", IdempotencyKey: "k"})
		if e != nil || p.To != tc.to {
			t.Fatalf("%s: plan=%+v err=%v", tc.a, p, e)
		}
	}
}
func TestTodoADMIN005Safety(t *testing.T) {
	base := ActionRequest{Action: ActionPromote, Incident: fixture(StateDeclared), Authorization: auth(ActionPromote), ExpectedVersion: 4, Reason: "x", IdempotencyKey: "k"}
	cases := []struct {
		name   string
		mutate func(*ActionRequest)
		want   error
	}{{"stale", func(r *ActionRequest) { r.ExpectedVersion = 3 }, ErrStale}, {"unauthorized", func(r *ActionRequest) { r.Authorization.AllowedActions = nil }, ErrUnauthorized}, {"self approval", func(r *ActionRequest) { r.Authorization.ApproverID = "operator" }, ErrSelfApproval}, {"missing evidence", func(r *ActionRequest) { r.Incident.Evidence = nil }, ErrEvidenceRequired}}
	for _, tc := range cases {
		r := base
		tc.mutate(&r)
		if _, e := Plan(r); !errors.Is(e, tc.want) {
			t.Errorf("%s err=%v want %v", tc.name, e, tc.want)
		}
	}
}
func TestTodoADMIN005RedactedTimeline(t *testing.T) {
	in := fixture(StateMonitoring)
	in.Timeline = []TimelineEvent{{ID: "safe", TenantID: "t-1", Compartment: "customer"}, {ID: "secret", TenantID: "t-1", Compartment: "security"}, {ID: "other", TenantID: "t-2", Compartment: "customer"}}
	v := View([]Incident{in}, auth(ActionInspect), time.Now())
	if len(v.Incidents) != 1 || len(v.Incidents[0].Timeline) != 1 || v.Incidents[0].Timeline[0].ID != "safe" {
		t.Fatalf("redaction failed: %+v", v)
	}
}
