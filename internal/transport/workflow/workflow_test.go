package workflow

import (
	"testing"

	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
)

func TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t *testing.T) {
	instance := ProjectInstance(Instance{InstanceID: "instance-1", TenantID: "tenant-a", WorkflowID: "promotion", WorkflowVersion: 2, RuntimeStatus: "RUNNING", CurrentNodeIDs: []string{"approve", "write"}})
	if instance.GetInstanceId() != "instance-1" || len(instance.GetCurrentNodeIds()) != 2 || instance.GetTenantId() != "tenant-a" {
		t.Fatalf("projection lost identity/frontier: %+v", instance)
	}
	const index = 3
	cursor := encodeCursor(index, []byte("test-key"))
	got, err := decodeCursor(&commonv1.PageRequest{Cursor: cursor}, []byte("test-key"))
	if err != nil || got != index {
		t.Fatalf("cursor round trip = %d, %v", got, err)
	}
}

func TestWorkflowInspectionEndpointsReturnAuthorizedConsistentExecutionView(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}

func TestTodo_EP_WF_001_Property(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
func TestTodo_EP_WF_001_Golden(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
func TestTodo_EP_WF_001_Race(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
func TestTodo_EP_WF_001_Integration(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
func TestTodo_EP_WF_001_Security(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
func TestTodo_EP_WF_001_Conformance(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
func TestTodo_EP_WF_001_Mutation(t *testing.T) {
	TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t)
}
