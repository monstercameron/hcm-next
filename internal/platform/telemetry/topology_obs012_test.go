package telemetry

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalSpanTopologyMatchesCapabilityWorkflowTransactionAndRepairSemantics(t *testing.T) {
	defs := CanonicalSpanTopology()
	if len(defs) != 19 {
		t.Fatalf("canonical topology has %d definitions, want 19", len(defs))
	}
	seen := map[SpanName]bool{}
	for _, d := range defs {
		if seen[d.Name] || d.Version != SpanTopologyVersion {
			t.Fatalf("duplicate or wrong version: %#v", d)
		}
		seen[d.Name] = true
		if strings.Contains(string(d.Name), "{") || strings.Contains(string(d.Name), "worker") {
			t.Errorf("dynamic span name %q", d.Name)
		}
		if _, ok := SpanDefinitionFor(d.Name); !ok {
			t.Errorf("definition %q is not findable", d.Name)
		}
	}
	for _, name := range []SpanName{SpanCapabilityInvoke, SpanWorkflowNode, SpanTransactionCommit, SpanRepair} {
		d, _ := SpanDefinitionFor(name)
		if !d.HasAttempt {
			t.Errorf("%s must distinguish retry attempts", name)
		}
		if !hasAttribute(d, "logical_operation_id") || !hasAttribute(d, "attempt_id") {
			t.Errorf("%s lacks operation/attempt identity", name)
		}
	}
	if d, _ := SpanDefinitionFor(SpanQueueDeliver); !d.DurableBoundary {
		t.Error("queue delivery must be a durable boundary")
	}
}

func TestTodo_OBS_012_Golden(t *testing.T) {
	if got := StatusFor(OutcomeSuccess); got != SpanStatusOK {
		t.Errorf("success status %s", got)
	}
	if got := StatusFor(OutcomeDenied); got != SpanStatusOK {
		t.Errorf("denied status %s", got)
	}
	if got := StatusFor(OutcomeCancelled); got != SpanStatusOK {
		t.Errorf("cancelled status %s", got)
	}
	for _, o := range []Outcome{OutcomeFailure, OutcomePartial, OutcomeUnknown, OutcomeDegraded} {
		if got := StatusFor(o); got != SpanStatusError {
			t.Errorf("%s status %s", o, got)
		}
	}
}

func TestTodo_OBS_012_Conformance(t *testing.T) {
	if err := ValidateSpanAttributes(SpanWorkflowNode, map[string]string{"node_id": "node-1", "attempt_id": "attempt-1"}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ValidateSpanAttributes(SpanWorkflowNode, map[string]string{"worker": "secret"}), ErrDynamicSpanAttribute) {
		t.Fatal("unregistered attribute accepted")
	}
	if !errors.Is(ValidateSpanAttributes(SpanWorkflowNode, map[string]string{"node_id": strings.Repeat("x", maxSpanAttributeValue+1)}), ErrSpanAttributeValue) {
		t.Fatal("unbounded attribute accepted")
	}
}

func TestTodo_OBS_012_Property(t *testing.T) {
	defs := CanonicalSpanTopology()
	defs[0].Attributes[0].Key = "mutated"
	d, _ := SpanDefinitionFor(defs[0].Name)
	if d.Attributes[0].Key == "mutated" {
		t.Fatal("topology leaked mutable attribute storage")
	}
}

func hasAttribute(d SpanDefinition, key string) bool {
	for _, a := range d.Attributes {
		if a.Key == key {
			return true
		}
	}
	return false
}
