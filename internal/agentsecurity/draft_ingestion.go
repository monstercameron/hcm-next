package agentsecurity

import (
	"context"
	"sync"
)

// DraftOwners binds the three concrete owner ports for one registered draft
// tool: the authoritative reference store, the AuthZ field owner and the
// governed-evidence claim owner. Ingestion resolves owners by tool name so
// production callers never hand-wire authority per call.
type DraftOwners struct {
	References OutputReferenceOwner
	Fields     OutputFieldAuthorizer
	Claims     OutputClaimOwner
}

// OwnerRegistry is the concrete schema/capability/AuthZ owner registration
// for draft ingestion. It binds each registered draft tool of its gateway
// to its owning ports exactly once.
type OwnerRegistry struct {
	mu      sync.Mutex
	gateway *ToolGateway
	owners  map[string]DraftOwners
}

// NewOwnerRegistry binds a registry to one gateway. A nil gateway is
// refused: owners are meaningless without the validator that consumes them.
func NewOwnerRegistry(gateway *ToolGateway) (*OwnerRegistry, error) {
	if gateway == nil {
		return nil, refusal(RefusalInvalid, "gateway", "owner registration requires a gateway")
	}
	return &OwnerRegistry{gateway: gateway, owners: make(map[string]DraftOwners)}, nil
}

// Register binds owners to one registered draft tool. Unknown tools,
// non-draft classes, nil owners and duplicate registrations are refused so
// a misconfigured boundary can never ingest.
func (r *OwnerRegistry) Register(toolName string, owners DraftOwners) error {
	if r == nil || r.gateway == nil {
		return refusal(RefusalInvalid, "registry", "nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, ok := r.gateway.tools[toolName]
	if !ok {
		return refusal(RefusalCapability, "tool", "tool is not registered")
	}
	if tool.Class != ToolDraft {
		return refusal(RefusalOutput, "tool", "only a registered draft tool can own draft ingestion")
	}
	if owners.References == nil || owners.Fields == nil || owners.Claims == nil {
		return refusal(RefusalOutput, "owners", "reference, field and claim owners are all required")
	}
	if _, dup := r.owners[toolName]; dup {
		return refusal(RefusalInvalid, "tool", "draft owners are already registered")
	}
	r.owners[toolName] = owners
	return nil
}

// IngestDraft is the production draft-ingestion caller: it resolves the
// registered owners for the admitted tool and validates the agent output
// into an effect-free draft. Output for an unregistered tool never reaches
// the validator.
func (r *OwnerRegistry) IngestDraft(ctx context.Context, admission Admission, toolName string, output AgentOutput) (DraftOutput, error) {
	if r == nil || r.gateway == nil {
		return DraftOutput{}, refusal(RefusalInvalid, "registry", "nil registry")
	}
	r.mu.Lock()
	owners, ok := r.owners[toolName]
	r.mu.Unlock()
	if !ok {
		return DraftOutput{}, refusal(RefusalOutput, "tool", "no draft owners are registered for this tool")
	}
	return r.gateway.ValidateDraftOutput(ctx, admission, toolName, output, owners.References, owners.Fields, owners.Claims)
}
