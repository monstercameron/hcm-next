package artifacts

import (
	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
)

// The child completion modes this package reasons about, re-exported from the
// durable store so a caller does not have to name internal/data/runtimestate
// to say how a child reports back.
const (
	childModeDetach = runtimestate.ChildDetach
	// leaseResourceWorkflowInstance is the only lease resource kind a
	// workflow-instance migration may present a fence for.
	leaseResourceWorkflowInstance = runtimestate.LeaseWorkflowInstance
)

// subscriptionNamespace is the fixed UUIDv5 namespace an open subscription's
// identity is derived under. It is this package's own namespace because
// nothing else derives one yet: internal/data/runtimestate's Subscribe takes
// whatever id its caller minted, and a migration that minted a fresh random
// id every time it re-keyed would create a second subscription on every
// replay instead of colliding with the one it already wrote.
var subscriptionNamespace = uuid.MustParse("2b8f4c17-6d3e-5a92-b41c-8e7d05f39a26")

// SubscriptionID is the derived identity of the subscription that one node of
// one instance opens for one signal.
//
// The tuple is exactly workflow_signal_subscription's own uniqueness
// constraint -- (tenant, instance, node, signal name) -- and deliberately
// excludes the correlation key: the schema allows one subscription per node
// and signal, so folding the correlation key into the identity would mint an
// id that collides on the constraint rather than on the primary key, turning
// a re-key into a storage error instead of a clean deduplication.
func SubscriptionID(tenantID, instanceID uuid.UUID, nodeID, signalName string) uuid.UUID {
	return uuid.NewSHA1(subscriptionNamespace,
		[]byte(tenantID.String()+"\x00"+instanceID.String()+"\x00"+nodeID+"\x00"+signalName))
}
