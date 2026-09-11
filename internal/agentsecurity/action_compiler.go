package agentsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// IntentDefinition is one discoverable definition the compiler may target.
// Only registered definitions exist: model output can never invent one.
type IntentDefinition struct {
	ID                 string
	Version            string
	RequiresReview     bool
	RequiresSimulation bool
	MaxBulk            int
}

// DefinitionRegistry is the discoverable definition catalog.
type DefinitionRegistry struct {
	mu          sync.Mutex
	definitions map[string]IntentDefinition
}

// NewDefinitionRegistry starts an empty catalog.
func NewDefinitionRegistry() *DefinitionRegistry {
	return &DefinitionRegistry{definitions: make(map[string]IntentDefinition)}
}

// Register publishes one definition. Duplicates and hollow entries refuse.
func (r *DefinitionRegistry) Register(definition IntentDefinition) error {
	if r == nil {
		return refusal(RefusalInvalid, "definitions", "nil registry")
	}
	if strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Version) == "" {
		return refusal(RefusalInvalid, "definition", "definition id and version are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.definitions[definition.ID]; dup {
		return refusal(RefusalInvalid, "definition", "definition is already registered")
	}
	r.definitions[definition.ID] = definition
	return nil
}

// Lookup resolves one registered definition.
func (r *DefinitionRegistry) Lookup(id string) (IntentDefinition, bool) {
	if r == nil {
		return IntentDefinition{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	definition, ok := r.definitions[id]
	return definition, ok
}

// Attribution binds the compiled draft to its exact agent provenance.
type Attribution struct {
	Agent       string
	Model       string
	ModelDigest string
	Delegation  string
	Purpose     string
	Cost        int
}

// ProposedAction is the validated shape the compiler accepts: a cited
// definition, exact arguments, sources, taint, disclosed uncertainty and
// a bounded bulk fan-out.
type ProposedAction struct {
	DefinitionID string
	Arguments    map[string]string
	Sources      []string
	Taint        []string
	Uncertainty  string
	Bulk         int
}

// DraftIntent is the attributed draft the compiler emits. It carries the
// required review and simulation gates unsatisfied: deterministic gateways
// perform all subsequent governance and execution, never the model.
type DraftIntent struct {
	DraftID            string
	DefinitionID       string
	DefinitionVersion  string
	Arguments          map[string]string
	Sources            []string
	Taint              []string
	Uncertainty        string
	Attribution        Attribution
	Tool               string
	ToolVersion        uint32
	RequiresReview     bool
	RequiresSimulation bool
	Receipt            string
}

func draftIntentDigest(definition IntentDefinition, action ProposedAction, attribution Attribution, tool string, version uint32) string {
	keys := make([]string, 0, len(action.Arguments))
	for key := range action.Arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make([]string, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, key+"="+action.Arguments[key])
	}
	bound := struct {
		Definition  string   `json:"definition"`
		Version     string   `json:"version"`
		Arguments   []string `json:"arguments"`
		Sources     []string `json:"sources"`
		Taint       []string `json:"taint"`
		Uncertainty string   `json:"uncertainty"`
		Bulk        int      `json:"bulk"`
		Agent       string   `json:"agent"`
		Model       string   `json:"model"`
		Tool        string   `json:"tool"`
	}{action.DefinitionID, definition.Version, ordered, action.Sources, action.Taint, action.Uncertainty, action.Bulk, attribution.Agent, attribution.Model, tool}
	encoded, _ := json.Marshal(bound)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-draft-intent/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ActionCompiler compiles validated agent output into governed draft
// intents. One compiler serves HCM Concierge and every domain agent: agent
// branding never changes authority semantics. The compiler owns no
// execution port, so compiled output can never execute a write.
type ActionCompiler struct {
	definitions *DefinitionRegistry
	owners      *OwnerRegistry
}

// NewActionCompiler binds the compiler to its definition catalog and its
// draft-ingestion owners.
func NewActionCompiler(definitions *DefinitionRegistry, owners *OwnerRegistry) (*ActionCompiler, error) {
	if definitions == nil || owners == nil {
		return nil, refusal(RefusalInvalid, "compiler", "definition catalog and draft owners are required")
	}
	return &ActionCompiler{definitions: definitions, owners: owners}, nil
}

// CompileAction validates agent output through draft ingestion and emits
// the attributed draft. Invented definitions, capabilities, subjects and
// scopes refuse; hidden uncertainty, unbounded bulk and bypassed review or
// simulation gates refuse; nothing executes.
func (c *ActionCompiler) CompileAction(ctx context.Context, admission Admission, toolName string, output AgentOutput, action ProposedAction, attribution Attribution) (DraftIntent, error) {
	if c == nil {
		return DraftIntent{}, refusal(RefusalInvalid, "compiler", "nil compiler")
	}
	draft, err := c.owners.IngestDraft(ctx, admission, toolName, output)
	if err != nil {
		return DraftIntent{}, err
	}
	_ = draft
	definition, ok := c.definitions.Lookup(action.DefinitionID)
	if !ok {
		return DraftIntent{}, refusal(RefusalCapability, "definition", "definition is not discoverable")
	}
	if strings.TrimSpace(action.Uncertainty) == "" {
		return DraftIntent{}, refusal(RefusalOutput, "uncertainty", "proposed action hides its uncertainty")
	}
	if len(action.Sources) == 0 {
		return DraftIntent{}, refusal(RefusalOutput, "sources", "proposed action cites no evidence")
	}
	if len(action.Taint) == 0 {
		return DraftIntent{}, refusal(RefusalOutput, "taint", "proposed action carries no taint")
	}
	if action.Bulk < 1 || action.Bulk > definition.MaxBulk || definition.MaxBulk < 1 {
		return DraftIntent{}, refusal(RefusalOutput, "bulk", "proposed action submits an unbounded bulk loop")
	}
	if strings.TrimSpace(attribution.Agent) == "" || strings.TrimSpace(attribution.Model) == "" || strings.TrimSpace(attribution.ModelDigest) == "" {
		return DraftIntent{}, refusal(RefusalIdentity, "attribution", "agent, model and model digest are required")
	}
	digest := draftIntentDigest(definition, action, attribution, admission.Tool, admission.Version)
	arguments := make(map[string]string, len(action.Arguments))
	for key, value := range action.Arguments {
		arguments[key] = value
	}
	return DraftIntent{
		DraftID: digest, DefinitionID: definition.ID, DefinitionVersion: definition.Version,
		Arguments:   arguments,
		Sources:     append([]string(nil), action.Sources...),
		Taint:       append([]string(nil), action.Taint...),
		Uncertainty: action.Uncertainty, Attribution: attribution,
		Tool: admission.Tool, ToolVersion: admission.Version,
		RequiresReview: definition.RequiresReview, RequiresSimulation: definition.RequiresSimulation,
		Receipt: digest,
	}, nil
}
