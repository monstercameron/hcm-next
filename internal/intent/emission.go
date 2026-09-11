package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// ChildCompletionBehavior declares how a parent treats a child at
// completion: wait for it, detach with an obligation, fail on its failure,
// or repair on its failure.
type ChildCompletionBehavior string

// Declared parent behaviors.
const (
	WaitForChild         ChildCompletionBehavior = "WAIT_FOR_CHILD"
	DetachWithObligation ChildCompletionBehavior = "DETACH_WITH_OBLIGATION"
	FailOnChildFailure   ChildCompletionBehavior = "FAIL_ON_CHILD_FAILURE"
	RepairOnFailure      ChildCompletionBehavior = "REPAIR_ON_FAILURE"
)

// Valid reports whether b is a declared behavior.
func (b ChildCompletionBehavior) Valid() bool {
	switch b {
	case WaitForChild, DetachWithObligation, FailOnChildFailure, RepairOnFailure:
		return true
	}
	return false
}

// EmissionRequest asks the emitter to name one child or follow-up intent.
// The emission key binds parent revision, source node/event, child
// definition/version and ordinal; quotas bound depth, fan-out and cost;
// the manifest decision is the one explicit authorization that may invoke
// a child intent.
type EmissionRequest struct {
	ParentID         string
	ParentRevision   string
	ParentDefinition string
	SourceNode       string
	SourceEvent      string
	ChildDefinition  string
	ChildVersion     string
	Ordinal          int
	Kind             RelationKind
	ParentScope      []string
	ChildScope       []string
	ParentPurpose    string
	ChildPurpose     string
	Ancestors        []string
	Depth            int
	MaxDepth         int
	MaxFanout        int
	Cost             int64
	MaxCost          int64
	ManifestDecision string
	Behavior         ChildCompletionBehavior
}

// ChildGovernance is the independent governance one emitted child carries:
// its own attenuated scope, narrowed purpose and policy reference.
type ChildGovernance struct {
	Scope     []string
	Purpose   string
	PolicyRef string
}

// EmittedChild is one deterministically named child intent.
type EmittedChild struct {
	Key        string
	ChildID    string
	Definition string
	Version    string
	Ordinal    int
	Kind       RelationKind
	Governance ChildGovernance
	Behavior   ChildCompletionBehavior
	Duplicate  bool
}

type storedEmission struct {
	request EmissionRequest
	child   EmittedChild
}

// Emitter names child and follow-up intents deterministically. It is safe
// for concurrent use: concurrent deliveries of one key name one child.
type Emitter struct {
	mu       sync.Mutex
	byKey    map[string]storedEmission
	outcomes map[string]string
}

// NewEmitter returns an empty emitter.
func NewEmitter() *Emitter {
	return &Emitter{byKey: make(map[string]storedEmission), outcomes: make(map[string]string)}
}

func emissionKey(req EmissionRequest) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.ParentRevision, req.SourceNode, req.SourceEvent,
		req.ChildDefinition, req.ChildVersion,
		fmt.Sprintf("ordinal=%d", req.Ordinal), req.Kind.String(),
	}, "\x00")))
	return "emit:" + hex.EncodeToString(sum[:])
}

// narrows reports whether child equals parent or extends it by dot-suffix:
// purposes narrow downwards, never broaden upwards or sideways.
func narrows(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+".")
}

// Emit names one child intent or returns the original child for a
// duplicate delivery. A redelivery that reuses a key for a different
// request fails as a conflict.
func (e *Emitter) Emit(req EmissionRequest) (EmittedChild, error) {
	if strings.TrimSpace(req.ParentID) == "" || strings.TrimSpace(req.ParentRevision) == "" ||
		strings.TrimSpace(req.SourceNode) == "" || strings.TrimSpace(req.ChildDefinition) == "" ||
		strings.TrimSpace(req.ChildVersion) == "" {
		return EmittedChild{}, newError("Emit", "emission", ErrInvalidEmission, "parent, revision, source node, child definition and version are required")
	}
	if req.Kind != RelationChild && req.Kind != RelationFollowUp {
		return EmittedChild{}, newError("Emit", "kind", ErrInvalidEmission, "kind %s cannot name a child", req.Kind)
	}
	if req.ChildDefinition == req.ParentDefinition {
		return EmittedChild{}, newError("Emit", "child_definition", ErrRecursiveEmission, "child repeats its parent definition")
	}
	for _, ancestor := range req.Ancestors {
		if req.ChildDefinition == ancestor {
			return EmittedChild{}, newError("Emit", "child_definition", ErrRecursiveEmission, "child recurses into ancestor %q", ancestor)
		}
	}
	if req.MaxFanout <= 0 || req.Ordinal < 0 || req.Ordinal >= req.MaxFanout {
		return EmittedChild{}, newError("Emit", "ordinal", ErrFanoutOverflow, "ordinal %d with fan-out bound %d", req.Ordinal, req.MaxFanout)
	}
	if req.MaxDepth <= 0 || req.Depth <= 0 || req.Depth > req.MaxDepth {
		return EmittedChild{}, newError("Emit", "depth", ErrDepthOverflow, "depth %d with bound %d", req.Depth, req.MaxDepth)
	}
	if req.MaxCost <= 0 || req.Cost < 0 || req.Cost > req.MaxCost {
		return EmittedChild{}, newError("Emit", "cost", ErrBudgetOverflow, "cost %d with bound %d", req.Cost, req.MaxCost)
	}
	approved := make(map[string]bool, len(req.ParentScope))
	for _, scope := range req.ParentScope {
		approved[scope] = true
	}
	for _, scope := range req.ChildScope {
		if !approved[scope] {
			return EmittedChild{}, newError("Emit", "child_scope", ErrScopeExpansion, "scope %q is outside parent approval", scope)
		}
	}
	if !narrows(req.ParentPurpose, req.ChildPurpose) {
		return EmittedChild{}, newError("Emit", "child_purpose", ErrPurposeBroadening, "purpose %q does not narrow %q", req.ChildPurpose, req.ParentPurpose)
	}
	if strings.TrimSpace(req.ManifestDecision) == "" {
		return EmittedChild{}, newError("Emit", "manifest_decision", ErrMissingManifestDecision, "a child needs its explicit manifest decision")
	}
	if !req.Behavior.Valid() {
		return EmittedChild{}, newError("Emit", "behavior", ErrInvalidCompletionBehavior, "behavior %q is not declared", req.Behavior)
	}
	scope := append([]string(nil), req.ChildScope...)
	sort.Strings(scope)
	key := emissionKey(req)
	child := EmittedChild{
		Key:        key,
		ChildID:    "child:" + key[len("emit:"):len("emit:")+16],
		Definition: req.ChildDefinition,
		Version:    req.ChildVersion,
		Ordinal:    req.Ordinal,
		Kind:       req.Kind,
		Governance: ChildGovernance{
			Scope:     scope,
			Purpose:   req.ChildPurpose,
			PolicyRef: "governance/" + req.ChildDefinition + "@" + req.ChildVersion,
		},
		Behavior: req.Behavior,
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if prior, ok := e.byKey[key]; ok {
		if !reflect.DeepEqual(prior.request, req) {
			return EmittedChild{}, newError("Emit", "emission", ErrEmissionConflict, "key %q already names a different request", key)
		}
		prior.child.Duplicate = true
		return prior.child, nil
	}
	e.byKey[key] = storedEmission{request: req, child: child}
	return child, nil
}

// RecordOutcome records one emitted child's outcome for completion.
func (e *Emitter) RecordOutcome(key, outcome string) error {
	if strings.TrimSpace(outcome) == "" {
		return newError("RecordOutcome", "outcome", ErrInvalidEmission, "outcome is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.byKey[key]; !ok {
		return newError("RecordOutcome", "key", ErrUnknownEmission, "key %q was never emitted", key)
	}
	e.outcomes[key] = outcome
	return nil
}

// Complete closes one parent: every mandatory child key must carry a
// recorded outcome. A parent that ignores a mandatory child result fails.
func (e *Emitter) Complete(parentID string, mandatory []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, key := range mandatory {
		outcome, ok := e.outcomes[key]
		if !ok || strings.TrimSpace(outcome) == "" {
			return newError("Complete", "mandatory", ErrMissingChildOutcome, "parent %q ignores child %q", parentID, key)
		}
	}
	return nil
}
