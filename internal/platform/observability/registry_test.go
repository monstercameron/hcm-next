package observability

import (
	"errors"
	"testing"
)

func testDefinition() Definition {
	t := make(map[Outcome]string, 7)
	s := make(map[Outcome]Severity, 7)
	for _, o := range outcomes() {
		t[o] = "operation " + string(o)
		s[o] = DefaultSeverity(o)
	}
	return Definition{Name: "workflow.node.completed", Version: 2, Templates: t, Severity: s}
}

func TestLogSemanticRegistryMapsEachOperationOutcomeToExactEventSeverityAndErrorFields(t *testing.T) {
	r, err := NewRegistry(testDefinition())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		o   Outcome
		sev Severity
	}{
		{OutcomeSuccess, SeverityInfo}, {OutcomeFailure, SeverityError}, {OutcomePartial, SeverityWarn},
		{OutcomeUnknown, SeverityWarn}, {OutcomeDenied, SeverityWarn}, {OutcomeCancelled, SeverityInfo}, {OutcomeDegraded, SeverityWarn},
	}
	for _, tc := range cases {
		var ef *ErrorFields
		if tc.o == OutcomeFailure {
			ef = &ErrorFields{Code: "STORE_TIMEOUT", Type: "transient", Retryable: true}
		}
		e, err := r.Resolve("workflow.node.completed", tc.o, ef)
		if err != nil {
			t.Fatalf("%s: %v", tc.o, err)
		}
		if e.Name != "workflow.node.completed" || e.Version != 2 || e.Outcome != tc.o || e.Severity != tc.sev {
			t.Errorf("%s: %#v", tc.o, e)
		}
		if tc.o != OutcomeFailure && e.Error != nil {
			t.Errorf("%s unexpectedly has error", tc.o)
		}
		if tc.o == OutcomeFailure && (e.Error == nil || e.Error.Code != "STORE_TIMEOUT" || !e.Error.Retryable) {
			t.Errorf("failure error fields: %#v", e.Error)
		}
	}
}

func TestTodo_OBS_010_Conformance(t *testing.T) {
	if DefaultSeverity(OutcomeDenied) == SeverityError {
		t.Fatal("denial must not be represented as system error")
	}
	if DefaultSeverity(OutcomeUnknown) == SeverityInfo || DefaultSeverity(OutcomePartial) == SeverityInfo {
		t.Fatal("uncertain outcomes must not collapse to success")
	}
	if _, err := NewRegistry(Definition{Name: "dynamic", Version: 1}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("invalid event accepted: %v", err)
	}
}

func TestTodo_OBS_010_Security(t *testing.T) {
	r, _ := NewRegistry(testDefinition())
	if _, err := r.Resolve("workflow.node.completed", OutcomeSuccess, &ErrorFields{Code: "raw SQL text", Type: "x"}); !errors.Is(err, ErrInvalidError) {
		t.Fatalf("unsafe code accepted: %v", err)
	}
	if _, err := r.Resolve("workflow.node.completed", OutcomeFailure, nil); !errors.Is(err, ErrInvalidError) {
		t.Fatalf("failure without typed fields accepted: %v", err)
	}
}

func TestTodo_OBS_010_Golden(t *testing.T) {
	r, _ := NewRegistry(testDefinition())
	e, err := r.Resolve("workflow.node.completed", OutcomeFailure, &ErrorFields{Code: "STORE_TIMEOUT", Type: "transient", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	want := Event{Name: "workflow.node.completed", Version: 2, Severity: SeverityError, Outcome: OutcomeFailure, Template: "operation FAILURE", Error: &ErrorFields{Code: "STORE_TIMEOUT", Type: "transient", Retryable: true}}
	if e.Name != want.Name || e.Version != want.Version || e.Severity != want.Severity || e.Outcome != want.Outcome || e.Template != want.Template || *e.Error != *want.Error {
		t.Errorf("event changed: %#v", e)
	}
}
