package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// CapabilityDescriptorResolver is the read side of process-role wiring: it
// resolves one exact immutable capability descriptor version and never
// selects "latest". Commands reach the capability registry only through
// this application seam (composition-root rule), never by importing
// internal/capability themselves.
// CapabilityKey and CapabilityRecord are the descriptor identity and record
// commands see through this seam.
type (
	CapabilityKey        = capability.Key
	CapabilityRecord     = capability.Record
	CapabilityDefinition = capability.Definition
)

type CapabilityDescriptorResolver interface {
	ResolveCapability(context.Context, capability.Key) (capability.Record, bool)
}

type registryCapabilityResolver struct{ registry *capability.Registry }

func (r registryCapabilityResolver) ResolveCapability(_ context.Context, key capability.Key) (capability.Record, bool) {
	if r.registry == nil {
		return capability.Record{}, false
	}
	return r.registry.Lookup(key)
}

// NewRegistryCapabilityResolver adapts a compiled capability registry to the
// resolver seam. A nil registry resolves nothing.
func NewRegistryCapabilityResolver(registry *capability.Registry) CapabilityDescriptorResolver {
	return registryCapabilityResolver{registry: registry}
}
