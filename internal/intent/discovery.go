package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Child outcomes: the independent truth each child retains.
const (
	ChildSucceeded = "succeeded"
	ChildFailed    = "failed"
	ChildUnknown   = "unknown"
)

// ChildTruth is the independent lifecycle, authority, idempotency and
// evidence truth of one composed child. Composition derives aggregates
// from these without ever overwriting them.
type ChildTruth struct {
	ChildKey       string
	Outcome        string
	Scope          []string
	Classification string
	IdempotencyKey string
	Evidence       string
	Irreversible   bool
	Cancelled      bool
}

// TriggerFiring is one trigger-driven intent firing keyed for idempotency.
type TriggerFiring struct {
	FiringKey      string
	TargetIntentID string
	IdempotencyKey string
}

// Aggregate is the derived parent status with zero information loss: every
// child outcome survives inside it.
type Aggregate struct {
	Composition string
	Status      string
	Children    []ChildTruth
	RepairOnly  []string
	Digest      string
}

func aggregateDigest(composition string, children []ChildTruth) string {
	parts := []string{"intent-composition-truth", composition}
	ordered := append([]ChildTruth(nil), children...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ChildKey < ordered[j].ChildKey })
	for _, child := range ordered {
		scope := append([]string(nil), child.Scope...)
		sort.Strings(scope)
		parts = append(parts, strings.Join([]string{child.ChildKey, child.Outcome, strings.Join(scope, ","), child.Classification, child.IdempotencyKey, child.Evidence, fmt.Sprint(child.Irreversible, child.Cancelled)}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func subset(child, parent []string) bool {
	allowed := make(map[string]bool, len(parent))
	for _, scope := range parent {
		allowed[scope] = true
	}
	for _, scope := range child {
		if !allowed[scope] {
			return false
		}
	}
	return true
}

// DeriveTruth binds the typed relationship policy to per-child truth and
// derives the aggregate. No child outcome, authority, classification,
// idempotency key or evidence is collapsed: the aggregate carries them all.
func DeriveTruth(composition string, parentScope []string, parentClassification string, children []ChildTruth, firings []TriggerFiring) (Aggregate, error) {
	if strings.TrimSpace(composition) == "" {
		return Aggregate{}, fmt.Errorf("intent: composition truth requires a composition identity")
	}
	aggregate := Aggregate{Composition: composition}
	seenChild := make(map[string]bool, len(children))
	seenFire := make(map[string]bool, len(firings))
	for _, child := range children {
		if strings.TrimSpace(child.ChildKey) == "" || seenChild[child.ChildKey] {
			return Aggregate{}, fmt.Errorf("intent: child keys must be unique and non-empty")
		}
		seenChild[child.ChildKey] = true
		switch child.Outcome {
		case ChildSucceeded, ChildFailed, ChildUnknown:
		default:
			return Aggregate{}, fmt.Errorf("intent: child %s outcome %q is not independent truth", child.ChildKey, child.Outcome)
		}
		if !subset(child.Scope, parentScope) {
			return Aggregate{}, fmt.Errorf("intent: child %s inherits broader authority than its parent", child.ChildKey)
		}
		if child.Classification != parentClassification {
			return Aggregate{}, fmt.Errorf("intent: child %s inherits a different classification than its parent", child.ChildKey)
		}
		if strings.TrimSpace(child.IdempotencyKey) == "" {
			return Aggregate{}, fmt.Errorf("intent: child %s has no idempotency key", child.ChildKey)
		}
		aggregate.Children = append(aggregate.Children, child)
	}
	for _, firing := range firings {
		if strings.TrimSpace(firing.FiringKey) == "" || seenFire[firing.FiringKey] {
			return Aggregate{}, fmt.Errorf("intent: trigger firings must be unique and non-empty")
		}
		seenFire[firing.FiringKey] = true
		for _, other := range firings {
			if other.FiringKey != firing.FiringKey && other.IdempotencyKey == firing.IdempotencyKey {
				return Aggregate{}, fmt.Errorf("intent: trigger retry duplicates target intent %s", firing.TargetIntentID)
			}
		}
	}
	status := ChildSucceeded
	for _, child := range aggregate.Children {
		switch child.Outcome {
		case ChildFailed:
			status = ChildFailed
		case ChildUnknown:
			if status == ChildSucceeded {
				status = ChildUnknown
			}
		}
	}
	aggregate.Status = status
	for _, child := range aggregate.Children {
		if child.Outcome != ChildSucceeded {
			aggregate.RepairOnly = append(aggregate.RepairOnly, child.ChildKey)
		}
	}
	sort.Strings(aggregate.RepairOnly)
	aggregate.Digest = aggregateDigest(composition, aggregate.Children)
	return aggregate, nil
}

// Cancel marks one child cancelled. Crossing an irreversible boundary is
// an explicit breach, never a silent success.
func Cancel(children []ChildTruth, childKey string) ([]ChildTruth, error) {
	out := append([]ChildTruth(nil), children...)
	for i, child := range out {
		if child.ChildKey != childKey {
			continue
		}
		if child.Irreversible {
			return nil, fmt.Errorf("intent: cancellation breaches the irreversible boundary of child %s", childKey)
		}
		out[i].Cancelled = true
		return out, nil
	}
	return nil, fmt.Errorf("intent: unknown child %s", childKey)
}

// TruthRegistry guards composed truth for concurrent derivation.
type TruthRegistry struct {
	mu         sync.Mutex
	aggregates map[string]Aggregate
}

// NewTruthRegistry starts an empty registry.
func NewTruthRegistry() *TruthRegistry {
	return &TruthRegistry{aggregates: make(map[string]Aggregate)}
}

// Record derives and stores one aggregate. Re-recording an identical
// digest is idempotent; a conflicting digest for one composition refuses.
func (r *TruthRegistry) Record(composition string, parentScope []string, parentClassification string, children []ChildTruth, firings []TriggerFiring) (Aggregate, error) {
	aggregate, err := DeriveTruth(composition, parentScope, parentClassification, children, firings)
	if err != nil {
		return Aggregate{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.aggregates[composition]; ok {
		if prior.Digest != aggregate.Digest {
			return Aggregate{}, fmt.Errorf("intent: composition %s truth conflicts with the recorded aggregate", composition)
		}
		return prior, nil
	}
	r.aggregates[composition] = aggregate
	return aggregate, nil
}

// Lookup returns the recorded aggregate.
func (r *TruthRegistry) Lookup(composition string) (Aggregate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	aggregate, ok := r.aggregates[composition]
	return aggregate, ok
}
