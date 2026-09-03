// Package organization contains the canonical, read-only organization graph
// contract.  It deliberately has no persistence dependency: adapters may
// build a Snapshot from the canonical stream and all consumers use the same
// validation and traversal rules.
package organization

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrTenantBoundary  = errors.New("organization: tenant boundary violation")
	ErrOrphan          = errors.New("organization: orphan node")
	ErrCycle           = errors.New("organization: cycle")
	ErrDuplicateParent = errors.New("organization: duplicate exclusive parent")
	ErrUnauthorized    = errors.New("organization: unauthorized scope")
	ErrInvalidGraph    = errors.New("organization: invalid graph")
)

// EdgeType identifies an independent relationship stream.  Parent hierarchy
// is exclusive; matrix/project relationships are intentionally not parents.
type EdgeType string

const (
	Hierarchy            EdgeType = "HIERARCHY"
	CorporateOwnership   EdgeType = "CORPORATE_OWNERSHIP"
	CostCenter           EdgeType = "COST_CENTER"
	LegalEntityStructure EdgeType = "LEGAL_ENTITY_STRUCTURE"
	Geographic           EdgeType = "GEOGRAPHIC"
	ProjectMatrix        EdgeType = "PROJECT_MATRIX"
)

func (t EdgeType) exclusiveParent() bool { return t == Hierarchy }

type OrganizationUnit struct {
	ID, Tenant, Name, Type, LegalEntity, SourceRevision string
	EffectiveFrom                                       time.Time
	EffectiveTo                                         *time.Time
}

func (n OrganizationUnit) active(at time.Time) bool {
	return !at.Before(n.EffectiveFrom) && (n.EffectiveTo == nil || at.Before(*n.EffectiveTo))
}

type RelationshipEdge struct {
	ID, Tenant, Source, Target, SourceRevision string
	Type                                       EdgeType
	EffectiveFrom                              time.Time
	EffectiveTo                                *time.Time
}

func (e RelationshipEdge) active(at time.Time) bool {
	return !at.Before(e.EffectiveFrom) && (e.EffectiveTo == nil || at.Before(*e.EffectiveTo))
}

// Snapshot is a verified graph projection at one canonical watermark.
type Snapshot struct {
	Tenant, Watermark, ResolverPolicyVersion string
	Units                                    []OrganizationUnit
	Edges                                    []RelationshipEdge
}

// Graph and Edge are concise names used by read adapters.
type Graph = Snapshot
type Edge = RelationshipEdge

// Authorizer is called for each requested node. Returning false fails closed.
type Authorizer func(unit OrganizationUnit) bool

type ReadRequest struct {
	Tenant, Root string
	AsOf         time.Time
	EdgeTypes    []EdgeType
	Authorize    Authorizer
}
type ReadResult struct {
	Units                            []OrganizationUnit
	Edges                            []RelationshipEdge
	Watermark, ResolverPolicyVersion string
}

// Validate checks tenant isolation, orphaning, exclusive-parent cardinality,
// and effective-time cycles. It is safe to call before exposing a snapshot.
func (s Snapshot) Validate(at time.Time) error {
	units := map[string]OrganizationUnit{}
	for _, n := range s.Units {
		if n.ID == "" || n.Tenant == "" || n.Tenant != s.Tenant {
			return fmt.Errorf("%w: unit %q", ErrTenantBoundary, n.ID)
		}
		if !n.active(at) {
			continue
		}
		if _, ok := units[n.ID]; ok {
			return fmt.Errorf("%w: unit %q", ErrInvalidGraph, n.ID)
		}
		units[n.ID] = n
	}
	parents := map[string]string{}
	for _, e := range s.Edges {
		if !e.active(at) {
			continue
		}
		if e.ID == "" || e.Type == "" {
			return fmt.Errorf("%w: edge identity/type", ErrInvalidGraph)
		}
		if e.Tenant != s.Tenant {
			return fmt.Errorf("%w: edge %q", ErrTenantBoundary, e.ID)
		}
		if _, ok := units[e.Source]; !ok {
			return fmt.Errorf("%w: %s", ErrOrphan, e.Source)
		}
		if _, ok := units[e.Target]; !ok {
			return fmt.Errorf("%w: %s", ErrOrphan, e.Target)
		}
		if e.Type.exclusiveParent() {
			if old, ok := parents[e.Target]; ok && old != e.Source {
				return fmt.Errorf("%w: %s", ErrDuplicateParent, e.Target)
			}
			parents[e.Target] = e.Source
		}
	}
	for child := range parents {
		seen := map[string]bool{child: true}
		for p, ok := parents[child]; ok; p, ok = parents[p] {
			if seen[p] {
				return fmt.Errorf("%w: %s", ErrCycle, p)
			}
			seen[p] = true
		}
	}
	return nil
}

// Read returns an authorized scope closure (the root and all reachable units)
// with deterministic ordering and the snapshot's watermark attached.
func Read(s Snapshot, q ReadRequest) (ReadResult, error) {
	if q.Tenant == "" || q.Tenant != s.Tenant {
		return ReadResult{}, ErrTenantBoundary
	}
	if q.AsOf.IsZero() {
		return ReadResult{}, fmt.Errorf("%w: as-of is required", ErrInvalidGraph)
	}
	if err := s.Validate(q.AsOf); err != nil {
		return ReadResult{}, err
	}
	units := map[string]OrganizationUnit{}
	for _, n := range s.Units {
		if n.Tenant == q.Tenant && n.active(q.AsOf) {
			units[n.ID] = n
		}
	}
	if _, ok := units[q.Root]; !ok {
		return ReadResult{}, fmt.Errorf("%w: root %s", ErrOrphan, q.Root)
	}
	if q.Authorize != nil && !q.Authorize(units[q.Root]) {
		return ReadResult{}, ErrUnauthorized
	}
	allowed := map[EdgeType]bool{}
	for _, typ := range q.EdgeTypes {
		allowed[typ] = true
	}
	closure := map[string]bool{q.Root: true}
	changed := true
	for changed {
		changed = false
		for _, e := range s.Edges {
			if !e.active(q.AsOf) || e.Tenant != q.Tenant || (len(allowed) > 0 && !allowed[e.Type]) {
				continue
			}
			if closure[e.Source] && !closure[e.Target] {
				closure[e.Target] = true
				changed = true
			}
			if closure[e.Target] && !closure[e.Source] {
				closure[e.Source] = true
				changed = true
			}
		}
	}
	result := ReadResult{Watermark: s.Watermark, ResolverPolicyVersion: s.ResolverPolicyVersion}
	for id := range closure {
		n := units[id]
		if q.Authorize != nil && !q.Authorize(n) {
			continue
		}
		result.Units = append(result.Units, n)
	}
	visible := map[string]bool{}
	for _, n := range result.Units {
		visible[n.ID] = true
	}
	for _, e := range s.Edges {
		if !e.active(q.AsOf) || !visible[e.Source] || !visible[e.Target] || (len(allowed) > 0 && !allowed[e.Type]) {
			continue
		}
		result.Edges = append(result.Edges, e)
	}
	sort.Slice(result.Units, func(i, j int) bool { return result.Units[i].ID < result.Units[j].ID })
	sort.Slice(result.Edges, func(i, j int) bool { return result.Edges[i].ID < result.Edges[j].ID })
	return result, nil
}

// Read is also available as a method for repository adapters.
func (s Snapshot) Read(q ReadRequest) (ReadResult, error) { return Read(s, q) }

// Ancestry returns the authorized root-to-parent scope for a unit.
func Ancestry(s Snapshot, q ReadRequest) (ReadResult, error) {
	q.EdgeTypes = []EdgeType{Hierarchy}
	return Read(s, q)
}

// Descendency returns the authorized parent-to-descendant scope for a unit.
// The read contract returns the same verified closure so callers cannot
// accidentally combine independently-watermarked traversals.
func Descendency(s Snapshot, q ReadRequest) (ReadResult, error) {
	q.EdgeTypes = []EdgeType{Hierarchy}
	return Read(s, q)
}
