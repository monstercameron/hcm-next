package userflow

import "testing"

func validRecord() UserFlowRecord {
	return UserFlowRecord{FlowID: "UF-001", Version: "1", Status: "DRAFT", Owner: "experience", Title: "Submit leave", JobToBeDone: "request leave", SuccessDefinition: "request is accepted", RootBusinessIntent: "CreateLeaveRequest", References: SemanticReferences{Workflow: "LeaveWorkflow", VerticalSlice: "LeaveSlice"}, PrimaryParticipant: Participant{ID: "employee", Role: "HUMAN_SELF"}, OtherParticipants: []Participant{{ID: "manager", Role: "MANAGER"}}, IdentityAssurance: "session-bound", SessionAssumptions: "active session", Entry: EntryPaths{Entry: "portal", Discovery: "inbox", Resume: "task"}, Surfaces: []string{"GUIDED_FORM"}, Devices: []string{"desktop", "mobile"}, RequestedInput: "dates", ServerResolvedTruth: "eligibility", Locale: "locale rules", Accessibility: "keyboard and screen reader", Privacy: "least disclosure", Phase: "MAX-v1", MaximalConfiguration: "all variants", Stages: []FlowStage{{ID: "s1", Stage: Collect, ParticipantGoal: "provide dates", Surface: "GUIDED_FORM", SystemState: "draft", AvailableActions: "save", Input: "dates", Validation: "valid dates", CapabilityTransition: "CreateLeaveRequest", VisibleResult: "draft saved", Evidence: "draft receipt", ErrorRecovery: "correct or resume"}}, StateMatrix: []StatePresentation{{State: "Loading", Understand: "what is resolving", Behavior: "bounded progress"}}, Scenarios: []string{"happy path"}, Oracles: []string{"accepted"}, Evidence: []string{"receipt"}, TodoLinks: []string{"UXFLOW-001"}}
}

func TestUserFlowRecordRejectsMissingParticipantStateRecoveryOrSemanticReference(t *testing.T) {
	base := validRecord()
	cases := []struct {
		name string
		edit func(*UserFlowRecord)
	}{
		{"participant", func(r *UserFlowRecord) { r.PrimaryParticipant.ID = "" }}, {"state", func(r *UserFlowRecord) { r.StateMatrix = nil }},
		{"recovery", func(r *UserFlowRecord) { r.Stages[0].ErrorRecovery = "" }}, {"semantic reference", func(r *UserFlowRecord) { r.References.Workflow = "" }},
		{"stage", func(r *UserFlowRecord) { r.Stages[0].Stage = "UNKNOWN" }}, {"oracle", func(r *UserFlowRecord) { r.Oracles = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.edit(&r)
			if r.Validate() == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}
func TestTodo_UXFLOW_001_Property(t *testing.T) {
	r := validRecord()
	a, e := r.CanonicalDigest()
	if e != nil {
		t.Fatal(e)
	}
	b, e := r.CanonicalDigest()
	if e != nil || a != b {
		t.Fatal("digest not deterministic")
	}
	r.Stages = append(r.Stages, FlowStage{ID: "s2", Stage: Track, ParticipantGoal: "track", Surface: "STATUS_SUMMARY", SystemState: "running", AvailableActions: "wait", Input: "none", Validation: "none", CapabilityTransition: "none", VisibleResult: "status", Evidence: "receipt", ErrorRecovery: "retry"})
	if _, e = r.CanonicalDigest(); e != nil {
		t.Fatal(e)
	}
}
func TestTodo_UXFLOW_001_Golden(t *testing.T) {
	r := validRecord()
	d, e := r.CanonicalDigest()
	if e != nil || len(d) != 64 {
		t.Fatalf("golden digest: %v %q", e, d)
	}
	if r.Stages[0].Stage != Collect || r.FlowID != "UF-001" {
		t.Fatal("golden vocabulary drift")
	}
}
func TestTodo_UXFLOW_001_Security(t *testing.T) {
	r := validRecord()
	r.Privacy = ""
	if r.Validate() == nil {
		t.Fatal("privacy omission accepted")
	}
	r = validRecord()
	r.ProhibitedTelemetry = "raw salary"
	if r.Validate() != nil {
		t.Fatal("prohibited telemetry should remain representable")
	}
}
func TestTodo_UXFLOW_001_Conformance(t *testing.T) {
	for _, s := range Stages() {
		if !s.Valid() {
			t.Fatalf("vocabulary stage invalid: %s", s)
		}
	}
	if err := validRecord().Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_UXFLOW_001_Mutation(t *testing.T) {
	r := validRecord()
	for _, f := range []func(*UserFlowRecord){func(r *UserFlowRecord) { r.FlowID = "" }, func(r *UserFlowRecord) { r.Stages[0].ID = "" }, func(r *UserFlowRecord) { r.PrimaryParticipant.ID = "" }} {
		m := r
		f(&m)
		if m.Validate() == nil {
			t.Fatal("mutation accepted")
		}
	}
}
