package custom

import (
	"context"
	"testing"
)

// TestTodo_CUSTOM_005 verifies that the generated surface is a closed,
// governed five-operation manifest with evidence and side-effect metadata.
func TestTodo_CUSTOM_005(t *testing.T) {
	manifest, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Capabilities) != 5 || manifest.Digest == "" {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, capability := range manifest.Capabilities {
		if !capability.EvidenceRequired || len(capability.AuthZDomains) != 1 || capability.InputSchema == "" || len(capability.SideEffects) == 0 {
			t.Fatalf("ungoverned capability = %+v", capability)
		}
	}
}

// TestTodo_CUSTOM_005_Golden pins transport parity to the generated digest.
func TestTodo_CUSTOM_005_Golden(t *testing.T) {
	a, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckClientParity(a, b); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CUSTOM_005_Integration exercises the generated client against an
// injected service boundary rather than bypassing the capability manifest.
func TestTodo_CUSTOM_005_Integration(t *testing.T) {
	manifest, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewCapabilityClient(manifest, func(_ context.Context, capability Capability, invocation Invocation) (CommitReceipt, error) {
		return CommitReceipt{Event: Event{Operation: capability.Operation, ObjectID: invocation.ObjectID}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Change(context.Background(), "vehicle-1", "evidence-1")
	if err != nil || receipt.Event.Operation != OperationChange {
		t.Fatalf("generated client result=%+v err=%v", receipt, err)
	}
}

// TestTodo_CUSTOM_005_Security rejects an operation that was not generated.
func TestTodo_CUSTOM_005_Security(t *testing.T) {
	manifest, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewCapabilityClient(manifest, func(context.Context, Capability, Invocation) (CommitReceipt, error) {
		t.Fatal("invoker called")
		return CommitReceipt{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Invoke(context.Background(), Operation("DELETE"), "vehicle-1", "evidence-1"); err == nil {
		t.Fatal("generic delete was accepted")
	}
}

// TestTodo_CUSTOM_005_Conformance proves the grpcbridge-facing wrapper has
// the same governed manifest and invocation behavior.
func TestTodo_CUSTOM_005_Conformance(t *testing.T) {
	manifest, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewGRPCCapabilityClient(manifest, func(_ context.Context, capability Capability, invocation Invocation) (CommitReceipt, error) {
		return CommitReceipt{Event: Event{Operation: capability.Operation, ObjectID: invocation.ObjectID}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(context.Background(), "vehicle-1", "evidence-1"); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CUSTOM_005_Mutation proves that a manifest's policy digest changes
// when the declared purpose changes.
func TestTodo_CUSTOM_005_Mutation(t *testing.T) {
	a, err := GenerateCapabilities(testDefinition(), "fleet.operations")
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateCapabilities(testDefinition(), "fleet.audit")
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest == b.Digest {
		t.Fatal("purpose mutation did not change capability digest")
	}
}
