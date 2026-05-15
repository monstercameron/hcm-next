package executor

import "fmt"

// Registry stores deterministic block handlers by name and version.
type Registry struct {
	handlers map[string]BlockHandler
}

// NewRegistry creates an empty static block registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]BlockHandler)}
}

// Register adds a block handler to the registry.
func (registry *Registry) Register(block BlockReference, handler BlockHandler) error {
	if block.Name == "" {
		return fmt.Errorf("block name is required")
	}

	if block.Version == "" {
		return fmt.Errorf("block version is required")
	}

	if handler == nil {
		return fmt.Errorf("block handler is required")
	}

	key := registryKey(block)
	if _, exists := registry.handlers[key]; exists {
		return fmt.Errorf("block %s is already registered", key)
	}

	registry.handlers[key] = handler
	return nil
}

// Execute dispatches an execution request to the matching block handler.
func (registry *Registry) Execute(request ExecutionRequest) (BlockResult, *ExecutionError) {
	handler, exists := registry.handlers[registryKey(request.Block)]
	if !exists {
		return BlockResult{}, UnknownBlockError(request.Block)
	}

	return handler(request)
}

func registryKey(block BlockReference) string {
	return block.Name + "@" + block.Version
}
