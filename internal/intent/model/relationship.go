package model

import (
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Cardinality declares how many active target endpoints a source may carry.
type Cardinality string

// Cardinalities.
const (
	CardinalityOneToOne   Cardinality = "ONE_TO_ONE"
	CardinalityOneToMany  Cardinality = "ONE_TO_MANY"
	CardinalityManyToMany Cardinality = "MANY_TO_MANY"
)

// Valid reports whether c is one of the three declared cardinalities.
func (c Cardinality) Valid() bool {
	switch c {
	case CardinalityOneToOne, CardinalityOneToMany, CardinalityManyToMany:
		return true
	default:
		return false
	}
}

// RelationshipDefinition declares one first-class, effective-dated edge type
// between two entities (MODEL-013).
type RelationshipDefinition struct {
	Ref RelationshipRef

	SourceEntity EntityRef
	TargetEntity EntityRef

	Cardinality Cardinality

	// Exclusive marks an edge type where a source may hold at most one active
	// (non-overlapping) target at a time, e.g. a manager relationship.
	Exclusive bool

	// AllowCycles permits a source to reach itself through a chain of this
	// relationship's edges. False for hierarchies such as manager or
	// organization-unit parentage.
	AllowCycles bool

	// TenantScoped requires both endpoints of every fact to share one tenant.
	TenantScoped bool
}

// Validate rejects a relationship definition missing an endpoint kind or
// declaring an unevaluable cardinality.
func (d RelationshipDefinition) Validate() error {
	if err := d.Ref.Validate(); err != nil {
		return err
	}
	if err := d.SourceEntity.Validate(); err != nil {
		return newError("RelationshipDefinition.Validate", "source_entity", ErrInvalidRelationship,
			"%s declares no valid source endpoint kind: %v", d.Ref, err)
	}
	if err := d.TargetEntity.Validate(); err != nil {
		return newError("RelationshipDefinition.Validate", "target_entity", ErrInvalidRelationship,
			"%s declares no valid target endpoint kind: %v", d.Ref, err)
	}
	if !d.Cardinality.Valid() {
		return newError("RelationshipDefinition.Validate", "cardinality", ErrInvalidRelationship,
			"%s has cardinality %q, outside the three declared cardinalities", d.Ref, d.Cardinality)
	}
	return nil
}

// RelationshipFact is one bitemporal edge instance: effective time is when the
// edge holds in the business, recorded time is when the platform learned it,
// and known time is the earliest time the asserting authority had the fact
// (see internal/kernel/values for the shared bitemporal primitives).
type RelationshipFact struct {
	Relationship RelationshipRef

	SourceRef    string
	SourceTenant string
	TargetRef    string
	TargetTenant string

	Effective values.EffectiveInterval
	Recorded  values.RecordedAt
	Known     values.KnownAt

	ProvenanceRef string
}

// Validate rejects an incomplete relationship fact.
func (f RelationshipFact) Validate() error {
	if err := f.Relationship.Validate(); err != nil {
		return err
	}
	if f.SourceRef == "" || f.TargetRef == "" {
		return newError("RelationshipFact.Validate", "endpoints", ErrInvalidRelationship,
			"%s fact is missing a source or target reference", f.Relationship)
	}
	if f.SourceTenant == "" || f.TargetTenant == "" {
		return newError("RelationshipFact.Validate", "tenant", ErrInvalidRelationship,
			"%s fact is missing a source or target tenant", f.Relationship)
	}
	if err := f.Effective.Validate(); err != nil {
		return newError("RelationshipFact.Validate", "effective", ErrInvalidRelationship,
			"%s fact has no valid effective interval: %v", f.Relationship, err)
	}
	if !f.Recorded.Instant().IsSet() {
		return newError("RelationshipFact.Validate", "recorded", ErrInvalidRelationship,
			"%s fact has no recorded time", f.Relationship)
	}
	if !f.Known.Instant().IsSet() {
		return newError("RelationshipFact.Validate", "known", ErrInvalidRelationship,
			"%s fact has no known-at time", f.Relationship)
	}
	if f.ProvenanceRef == "" {
		return newError("RelationshipFact.Validate", "provenance_ref", ErrInvalidRelationship,
			"%s fact names no provenance edge", f.Relationship)
	}
	return nil
}

// ValidateFacts checks one relationship definition's declared constraints
// against a set of facts: every fact must validate and reference this
// definition, exclusive facts for the same source must not overlap in
// effective time, tenant-scoped facts must not cross tenants, and — unless
// cycles are allowed — the fact graph must not close a cycle.
func ValidateFacts(def RelationshipDefinition, facts []RelationshipFact) error {
	if err := def.Validate(); err != nil {
		return err
	}
	for _, f := range facts {
		if err := f.Validate(); err != nil {
			return err
		}
		if f.Relationship != def.Ref {
			return newError("ValidateFacts", "relationship", ErrInvalidRelationship,
				"fact references %s, not %s", f.Relationship, def.Ref)
		}
		if def.TenantScoped && f.SourceTenant != f.TargetTenant {
			return newError("ValidateFacts", "tenant", ErrCrossTenantEdge,
				"%s fact crosses tenants %s -> %s", def.Ref, f.SourceTenant, f.TargetTenant)
		}
	}
	if def.Exclusive {
		if err := checkExclusiveOverlap(def, facts); err != nil {
			return err
		}
	}
	if !def.AllowCycles {
		if cyc, ok := detectCycle(facts); ok {
			return newError("ValidateFacts", "cycle", ErrRelationshipCycle,
				"%s facts close a cycle: %v", def.Ref, cyc)
		}
	}
	return nil
}

func checkExclusiveOverlap(def RelationshipDefinition, facts []RelationshipFact) error {
	bySource := map[string][]RelationshipFact{}
	for _, f := range facts {
		key := f.SourceTenant + "/" + f.SourceRef
		bySource[key] = append(bySource[key], f)
	}
	for _, group := range bySource {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				overlap, err := group[i].Effective.Overlaps(group[j].Effective)
				if err != nil {
					continue
				}
				if overlap {
					return newError("checkExclusiveOverlap", "effective", ErrExclusiveOverlap,
						"%s has two overlapping exclusive edges from %s", def.Ref, group[i].SourceRef)
				}
			}
		}
	}
	return nil
}

// detectCycle reports whether the source->target adjacency implied by facts
// (ignoring effective time — a conservative, time-independent check) contains
// a cycle, and returns one offending path if so.
func detectCycle(facts []RelationshipFact) ([]string, bool) {
	adj := map[string][]string{}
	for _, f := range facts {
		adj[f.SourceRef] = append(adj[f.SourceRef], f.TargetRef)
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var path []string
	var visit func(node string) []string
	visit = func(node string) []string {
		color[node] = gray
		path = append(path, node)
		for _, next := range adj[node] {
			switch color[next] {
			case gray:
				return append(append([]string(nil), path...), next)
			case white:
				if cyc := visit(next); cyc != nil {
					return cyc
				}
			}
		}
		path = path[:len(path)-1]
		color[node] = black
		return nil
	}
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if color[n] == white {
			if cyc := visit(n); cyc != nil {
				return cyc, true
			}
		}
	}
	return nil, false
}

// AsOf returns the facts effective at asOf, sorted by source then target.
func AsOf(facts []RelationshipFact, asOf values.Instant) []RelationshipFact {
	var out []RelationshipFact
	for _, f := range facts {
		if ok, err := f.Effective.ContainsInstant(asOf); err == nil && ok {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceRef != out[j].SourceRef {
			return out[i].SourceRef < out[j].SourceRef
		}
		return out[i].TargetRef < out[j].TargetRef
	})
	return out
}

// KnownAt returns the facts effective at asOf that were also known to their
// asserting authority no later than knownAt: the bitemporal "what would this
// query have returned had we run it back then" replay.
func KnownAt(facts []RelationshipFact, asOf, knownAt values.Instant) []RelationshipFact {
	var out []RelationshipFact
	for _, f := range AsOf(facts, asOf) {
		if !f.Known.Instant().After(knownAt) {
			out = append(out, f)
		}
	}
	return out
}
