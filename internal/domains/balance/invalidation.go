package balance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Availability event kinds: the only intervening changes the index tracks.
const (
	EventDebit      = "debit"
	EventCorrection = "correction"
	EventExpiry     = "expiry"
	EventRollover   = "rollover"
)

// PlanComponent is one future approved plan component bound to its
// availability sources with the amount it was approved against.
type PlanComponent struct {
	PlanID      string
	ComponentID string
	Sources     []string
	Available   values.Decimal
	Paid        values.Decimal
	Unpaid      values.Decimal
	Revision    string
}

// AvailabilityEvent is one intervening change to a source.
type AvailabilityEvent struct {
	Kind         string
	SourceID     string
	OldRevision  string
	NewRevision  string
	OldAvailable values.Decimal
	NewAvailable values.Decimal
}

// ReplanFinding is the typed REPLAN_REQUIRED finding for one affected
// component: old and new amounts travel explicitly, so paid/unpaid
// allocation never changes silently.
type ReplanFinding struct {
	Finding      string
	PlanID       string
	ComponentID  string
	SourceID     string
	OldRevision  string
	NewRevision  string
	OldAvailable string
	NewAvailable string
	Paid         string
	Unpaid       string
	Digest       string
}

func findingDigest(finding ReplanFinding) string {
	parts := []string{"balance-replan-required", finding.PlanID, finding.ComponentID, finding.SourceID, finding.OldRevision, finding.NewRevision, finding.OldAvailable, finding.NewAvailable, finding.Paid, finding.Unpaid}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DependencyIndex maps availability sources to the plan components
// approved against them. Recalculation creates successor findings and
// never edits the approved plan.
type DependencyIndex struct {
	mu         sync.Mutex
	components map[string]PlanComponent
	bySource   map[string]map[string]bool
}

// NewDependencyIndex starts an empty index.
func NewDependencyIndex() *DependencyIndex {
	return &DependencyIndex{components: make(map[string]PlanComponent), bySource: make(map[string]map[string]bool)}
}

// Register binds one approved component to its sources.
func (index *DependencyIndex) Register(component PlanComponent) error {
	if index == nil {
		return fmt.Errorf("balance: nil dependency index")
	}
	if strings.TrimSpace(component.PlanID) == "" || strings.TrimSpace(component.ComponentID) == "" {
		return fmt.Errorf("balance: plan and component identities are required")
	}
	if len(component.Sources) == 0 {
		return fmt.Errorf("balance: component %s names no availability source", component.ComponentID)
	}
	if strings.TrimSpace(component.Revision) == "" {
		return fmt.Errorf("balance: component %s needs its approval revision", component.ComponentID)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	key := component.PlanID + "\x00" + component.ComponentID
	if _, dup := index.components[key]; dup {
		return fmt.Errorf("balance: component %s is already indexed", key)
	}
	index.components[key] = component
	for _, source := range component.Sources {
		if index.bySource[source] == nil {
			index.bySource[source] = make(map[string]bool)
		}
		index.bySource[source][key] = true
	}
	return nil
}

// Invalidate compares the event against indexed components and emits one
// typed REPLAN_REQUIRED finding per affected component. Components whose
// available amount did not move stay valid without findings; unrelated
// plans are never touched.
func (index *DependencyIndex) Invalidate(event AvailabilityEvent) ([]ReplanFinding, error) {
	if index == nil {
		return nil, fmt.Errorf("balance: nil dependency index")
	}
	switch event.Kind {
	case EventDebit, EventCorrection, EventExpiry, EventRollover:
	default:
		return nil, fmt.Errorf("balance: event kind %q is not an availability change", event.Kind)
	}
	if strings.TrimSpace(event.SourceID) == "" || strings.TrimSpace(event.NewRevision) == "" {
		return nil, fmt.Errorf("balance: event needs a source and a new revision")
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	var findings []ReplanFinding
	for key := range index.bySource[event.SourceID] {
		component := index.components[key]
		if event.NewAvailable.Equal(component.Available) && event.NewRevision == component.Revision {
			continue
		}
		finding := ReplanFinding{
			Finding: "REPLAN_REQUIRED", PlanID: component.PlanID, ComponentID: component.ComponentID,
			SourceID: event.SourceID, OldRevision: component.Revision, NewRevision: event.NewRevision,
			OldAvailable: component.Available.String(), NewAvailable: event.NewAvailable.String(),
			Paid: component.Paid.String(), Unpaid: component.Unpaid.String(),
		}
		finding.Digest = findingDigest(finding)
		findings = append(findings, finding)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].PlanID != findings[j].PlanID {
			return findings[i].PlanID < findings[j].PlanID
		}
		return findings[i].ComponentID < findings[j].ComponentID
	})
	return findings, nil
}

// Valid reports whether the component still matches its approval: no
// finding, no invalidation.
func (index *DependencyIndex) Valid(planID, componentID, revision string, available values.Decimal) bool {
	if index == nil {
		return false
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	component, ok := index.components[planID+"\x00"+componentID]
	if !ok {
		return false
	}
	return component.Revision == revision && component.Available.Equal(available)
}
