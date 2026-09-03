package observability

import (
	"errors"
	"reflect"
	"testing"
)

// TestTodo_OBS_010_Property checks that registry results are deterministic and
// that registration does not retain mutable caller-owned maps.
func TestTodo_OBS_010_Property(t *testing.T) {
	d := testDefinition()
	r, err := NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.Map(d.Name, OutcomeFailure, &ErrorFields{Code: "STORE_TIMEOUT", Type: "transient", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Map(d.Name, OutcomeFailure, &ErrorFields{Code: "STORE_TIMEOUT", Type: "transient", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same semantic input produced different events: %#v vs %#v", first, second)
	}
	// Register clones both maps, so changing the source definition cannot
	// change a previously accepted event contract.
	d.Templates[OutcomeFailure] = "mutated"
	d.Severity[OutcomeFailure] = SeverityDebug
	got, err := r.Resolve(d.Name, OutcomeFailure, &ErrorFields{Code: "STORE_TIMEOUT", Type: "transient", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Template != "operation FAILURE" || got.Severity != SeverityError {
		t.Fatalf("caller mutation changed registered definition: %#v", got)
	}
	if !reflect.DeepEqual(r.Operations(), []string{d.Name}) {
		t.Fatalf("operations are not deterministic: %v", r.Operations())
	}
}

// TestTodo_OBS_010_Mutation exercises the definition and resolution gates
// that prevent missing outcomes from silently inheriting SUCCESS semantics.
func TestTodo_OBS_010_Mutation(t *testing.T) {
	base := testDefinition()
	cases := []Definition{
		{Name: "workflow.node.completed", Version: 0, Templates: base.Templates, Severity: base.Severity},
		{Name: "workflow.node.completed", Version: 1, Templates: base.Templates, Severity: base.Severity},
	}
	// Version zero is always rejected; a duplicate event name is rejected even
	// when its version is changed, because changing meaning requires a new
	// versioned event identifier/definition at the owner boundary.
	if _, err := NewRegistry(cases[0]); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("version-zero definition accepted: %v", err)
	}
	r, err := NewRegistry(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Register(cases[1]); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("duplicate event definition accepted: %v", err)
	}
	if _, err := r.Resolve(base.Name, Outcome("NEW_OUTCOME"), nil); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("unknown outcome accepted: %v", err)
	}
	if _, err := r.Resolve(base.Name, OutcomeFailure, &ErrorFields{Code: "X", Type: "Y", Retryable: false}); err != nil {
		t.Fatalf("non-retryable typed failure rejected: %v", err)
	}
}

func TestTodo_OBS_010_ConformanceRejectsMalformedDefinitionsAndErrorTokens(t *testing.T) {
	base := testDefinition()
	badNames := []string{"dynamic", "Workflow.node", "workflow..node", "workflow.node.", "workflow.node with space"}
	for _, name := range badNames {
		base.Name = name
		if _, err := NewRegistry(base); !errors.Is(err, ErrInvalidDefinition) {
			t.Errorf("event name %q accepted: %v", name, err)
		}
	}
	for _, e := range []ErrorFields{
		{Code: "", Type: "transient"},
		{Code: "OK", Type: ""},
		{Code: "raw SQL text", Type: "transient"},
		{Code: "OK", Type: "contains space"},
		{Code: "OK", Type: "transient", Retryable: false},
	} {
		if e.Code == "OK" && e.Type == "transient" && !e.Retryable {
			continue // This is a valid non-retryable error, not malformed input.
		}
		if e.Valid() {
			t.Errorf("unsafe error fields accepted: %#v", e)
		}
	}
}
