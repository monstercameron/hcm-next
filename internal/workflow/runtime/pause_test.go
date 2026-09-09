package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// pause.go's per-file suite. wfrun008_test.go drives the whole pause lifecycle
// against a real database; what is pinned here is the half that runs *before*
// any statement does -- request validation and the stable refusal codes -- and
// it is pinned by calling the entry points with a nil Executor.
//
// That nil is the assertion. Every case below would panic if validation ran
// after the first read, so the tests prove not only that a malformed request
// is refused but that it is refused without touching the database: a pause
// request missing its actor never opens a transaction, and a resume that
// cannot name its plan never loads an instance.

var pauseInstant = time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)

func wellFormedPauseRequest(plan *workflow.CompiledWorkflow) runtime.PauseRequest {
	return runtime.PauseRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		ExpectedInstanceVersion: 1,
		Plan:                    plan,
		Reason:                  "INCIDENT_REVIEW",
		RequestedBy:             "principal:operations-duty",
		RequestedAt:             pauseInstant,
	}
}

func TestPause_RequestValidationRefusesBeforeAnyRead(t *testing.T) {
	t.Parallel()
	plan := referencePlan(t)

	cases := []struct {
		name string
		with func(*runtime.PauseRequest)
	}{
		{"nil tenant", func(r *runtime.PauseRequest) { r.TenantID = uuid.Nil }},
		{"nil instance", func(r *runtime.PauseRequest) { r.InstanceID = uuid.Nil }},
		{"no plan", func(r *runtime.PauseRequest) { r.Plan = nil }},
		{"no reason", func(r *runtime.PauseRequest) { r.Reason = "" }},
		{"no requester", func(r *runtime.PauseRequest) { r.RequestedBy = "" }},
		{"no instant", func(r *runtime.PauseRequest) { r.RequestedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := wellFormedPauseRequest(plan)
			tc.with(&req)

			// A nil Executor: reaching a statement would panic, so passing
			// these is the proof that nothing is read before the refusal.
			if _, err := runtime.RequestPause(context.Background(), nil, req); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
				t.Fatalf("RequestPause: code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeInvalidRecord, err)
			}
			if _, err := runtime.ApplyPause(context.Background(), nil, req); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
				t.Fatalf("ApplyPause: code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeInvalidRecord, err)
			}
		})
	}
}

func TestPause_ResumeValidationRefusesBeforeAnyRead(t *testing.T) {
	t.Parallel()
	plan := referencePlan(t)

	wellFormed := runtime.ResumeRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		ExpectedInstanceVersion: 2,
		Plan:                    plan,
		Reason:                  "INCIDENT_CLEARED",
		ResumedBy:               "principal:operations-duty",
		ResumedAt:               pauseInstant,
	}

	cases := []struct {
		name string
		with func(*runtime.ResumeRequest)
	}{
		{"nil tenant", func(r *runtime.ResumeRequest) { r.TenantID = uuid.Nil }},
		{"nil instance", func(r *runtime.ResumeRequest) { r.InstanceID = uuid.Nil }},
		// A resume always transitions, so unlike a pause request it has no
		// version-free replay path: zero is refused rather than defaulted.
		{"unfenced", func(r *runtime.ResumeRequest) { r.ExpectedInstanceVersion = 0 }},
		{"no plan", func(r *runtime.ResumeRequest) { r.Plan = nil }},
		{"no resumer", func(r *runtime.ResumeRequest) { r.ResumedBy = "" }},
		{"no instant", func(r *runtime.ResumeRequest) { r.ResumedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := wellFormed
			tc.with(&req)
			if _, err := runtime.ResumeFromPause(context.Background(), nil, req); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
				t.Fatalf("ResumeFromPause: code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeInvalidRecord, err)
			}
		})
	}

	// A pause request, unlike a resume, may legitimately omit the version: a
	// replayed request against an already-paused instance is answered from the
	// stored row. So a zero version must survive validation and be judged
	// later, against the instance's actual status -- which is why this case is
	// the one that is NOT in the table above.
	unfenced := wellFormedPauseRequest(plan)
	unfenced.ExpectedInstanceVersion = 0
	func() {
		defer func() {
			// Getting past validation means reaching the instance read, which
			// with a nil Executor is a panic. That panic is the pass.
			_ = recover()
		}()
		_, err := runtime.RequestPause(context.Background(), nil, unfenced)
		if runtime.CodeOf(err) == runtime.CodeInvalidRecord {
			t.Errorf("an unfenced pause request was refused by validation (%v); a replayed pause could not then be answered from storage", err)
		}
	}()
}

func TestPause_RefusalCodesAreStable(t *testing.T) {
	t.Parallel()

	// These two strings cross the wire to operators and to the transport's
	// error mapping. Renaming one is a breaking change, so it is spelled here
	// rather than only in the code that emits it.
	if runtime.CodeInstancePaused != "INSTANCE_PAUSED" {
		t.Errorf("CodeInstancePaused = %q, want INSTANCE_PAUSED", runtime.CodeInstancePaused)
	}
	if runtime.CodeNotPaused != "NOT_PAUSED" {
		t.Errorf("CodeNotPaused = %q, want NOT_PAUSED", runtime.CodeNotPaused)
	}
	if runtime.CodeInstancePaused == runtime.CodeNotPaused {
		t.Errorf("the pause and the not-paused refusal share one code")
	}
}
