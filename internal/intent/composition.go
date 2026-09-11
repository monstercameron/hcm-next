package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ConsistencyBoundary bounds how one composition coordinates.
type ConsistencyBoundary string

// Declared consistency boundaries.
const (
	BoundarySingleSystem ConsistencyBoundary = "SINGLE_SYSTEM_TRANSACTIONAL"
	BoundarySaga         ConsistencyBoundary = "SAGA"
	BoundaryBestEffort   ConsistencyBoundary = "BEST_EFFORT"
)

// Valid reports whether b is declared.
func (b ConsistencyBoundary) Valid() bool {
	switch b {
	case BoundarySingleSystem, BoundarySaga, BoundaryBestEffort:
		return true
	}
	return false
}

// CompositionWait declares whether the parent waits for its children.
type CompositionWait string

// Wait policies.
const (
	WaitForChildren          CompositionWait = "WAIT_FOR_CHILDREN"
	WaitDetachWithObligation CompositionWait = "DETACH_WITH_OBLIGATION"
)

// Valid reports whether w is declared.
func (w CompositionWait) Valid() bool {
	return w == WaitForChildren || w == WaitDetachWithObligation
}

// CompositionFailure declares parent behavior on child failure.
type CompositionFailure string

// Failure policies.
const (
	FailParent  CompositionFailure = "FAIL_PARENT"
	RepairChild CompositionFailure = "REPAIR_CHILD"
)

// Valid reports whether f is declared.
func (f CompositionFailure) Valid() bool {
	return f == FailParent || f == RepairChild
}

// CompositionCorrection declares how a composed child is corrected.
type CompositionCorrection string

// Correction policies.
const (
	CorrectInPlace CompositionCorrection = "CORRECT_IN_PLACE"
	SupersedeChild CompositionCorrection = "SUPERSEDE_CHILD"
)

// Valid reports whether c is declared.
func (c CompositionCorrection) Valid() bool {
	return c == CorrectInPlace || c == SupersedeChild
}

// CompositionCancellation declares how cancellation reaches children.
type CompositionCancellation string

// Cancellation policies.
const (
	PropagateCancellation CompositionCancellation = "PROPAGATE_CANCELLATION"
	DetachChildren        CompositionCancellation = "DETACH_CHILDREN"
)

// Valid reports whether c is declared.
func (c CompositionCancellation) Valid() bool {
	return c == PropagateCancellation || c == DetachChildren
}

// ChildTemplate is one typed child in a composition plan.
type ChildTemplate struct {
	Definition Ref
	Ordinal    uint32
	Owner      string
	System     string
	Tenant     string
	Org        string
	Purpose    string
	Delegation []string
	Cost       int64
	Material   bool
}

// DependencyEdge orders two declared children: Before lands first.
type DependencyEdge struct {
	Before string
	After  string
}

// CompositionPlan is one immutable intent composition: a parent
// definition and proposal, typed child templates with ordinals, a
// dependency DAG, atomic groups, authority attenuation, consistency
// boundaries, completion policies and resource limits.
type CompositionPlan struct {
	ParentDefinition Ref
	ParentProposal   string
	ParentTenant     string
	ParentOrg        string
	ParentPurpose    string
	ParentDelegation []string
	Children         []ChildTemplate
	Edges            []DependencyEdge
	AtomicGroups     [][]string
	Boundary         ConsistencyBoundary
	Wait             CompositionWait
	Failure          CompositionFailure
	Correction       CompositionCorrection
	Cancellation     CompositionCancellation
	MaxCost          int64
	MaxChildren      int
}

// CompiledComposition is one validated plan plus its deterministic digest.
type CompiledComposition struct {
	Plan   CompositionPlan
	Digest string
}

// IntentBundle is packaging and inspection evidence for one compiled plan:
// its digest, the ordered child identities and evidence refs. It carries
// no execution path and is never an alternate execution gateway.
type IntentBundle struct {
	PlanDigest string
	Children   []string
	Evidence   []string
	Digest     string
}

func childKey(def Ref) string {
	return def.TypeID + "/v" + fmt.Sprintf("%d", def.Version)
}

// CompileComposition validates one plan and binds its deterministic
// digest. Duplicate compilation of one plan always yields one identity.
func CompileComposition(plan CompositionPlan) (CompiledComposition, error) {
	if strings.TrimSpace(plan.ParentDefinition.TypeID) == "" || plan.ParentDefinition.Version == 0 ||
		strings.TrimSpace(plan.ParentProposal) == "" || strings.TrimSpace(plan.ParentTenant) == "" ||
		strings.TrimSpace(plan.ParentOrg) == "" || strings.TrimSpace(plan.ParentPurpose) == "" {
		return CompiledComposition{}, newError("CompileComposition", "parent", ErrInvalidComposition,
			"parent definition, proposal, tenant, org and purpose are required")
	}
	if len(plan.Children) == 0 {
		return CompiledComposition{}, newError("CompileComposition", "children", ErrInvalidComposition,
			"at least one child template is required")
	}
	if !plan.Boundary.Valid() {
		return CompiledComposition{}, newError("CompileComposition", "boundary", ErrInvalidComposition,
			"consistency boundary %q is not declared", plan.Boundary)
	}
	if !plan.Wait.Valid() || !plan.Failure.Valid() || !plan.Correction.Valid() || !plan.Cancellation.Valid() {
		return CompiledComposition{}, newError("CompileComposition", "policy", ErrMissingCompositionPolicy,
			"wait, failure, correction and cancellation policies are all required")
	}
	byRef := make(map[string]ChildTemplate, len(plan.Children))
	ordinals := make(map[uint32]bool, len(plan.Children))
	granted := make(map[string]bool, len(plan.ParentDelegation))
	for _, token := range plan.ParentDelegation {
		granted[token] = true
	}
	var totalCost int64
	for i, child := range plan.Children {
		field := fmt.Sprintf("children[%d]", i)
		if strings.TrimSpace(child.Definition.TypeID) == "" || child.Definition.Version == 0 {
			return CompiledComposition{}, newError("CompileComposition", field, ErrInvalidComposition,
				"child definition and version are required")
		}
		key := child.Definition.TypeID
		if _, dup := byRef[key]; dup {
			return CompiledComposition{}, newError("CompileComposition", field, ErrDuplicateChild,
				"child %q is declared twice", key)
		}
		if ordinals[child.Ordinal] {
			return CompiledComposition{}, newError("CompileComposition", field, ErrUnorderedChildren,
				"ordinal %d is used twice", child.Ordinal)
		}
		ordinals[child.Ordinal] = true
		if strings.TrimSpace(child.Owner) == "" || strings.TrimSpace(child.System) == "" {
			return CompiledComposition{}, newError("CompileComposition", field, ErrInvalidComposition,
				"child %q needs an owner and a system", key)
		}
		if child.Tenant != plan.ParentTenant {
			return CompiledComposition{}, newError("CompileComposition", field, ErrAuthorityBroadening,
				"child %q leaves tenant %q", key, plan.ParentTenant)
		}
		if !narrows(plan.ParentOrg, child.Org) || !narrows(plan.ParentPurpose, child.Purpose) {
			return CompiledComposition{}, newError("CompileComposition", field, ErrAuthorityBroadening,
				"child %q broadens org or purpose", key)
		}
		for _, token := range child.Delegation {
			if !granted[token] {
				return CompiledComposition{}, newError("CompileComposition", field, ErrAuthorityBroadening,
					"child %q claims undelegated %q", key, token)
			}
		}
		byRef[key] = child
		totalCost += child.Cost
	}
	for i, edge := range plan.Edges {
		field := fmt.Sprintf("edges[%d]", i)
		if _, ok := byRef[edge.Before]; !ok {
			return CompiledComposition{}, newError("CompileComposition", field, ErrHiddenChildIntent,
				"edge starts at undeclared child %q", edge.Before)
		}
		if _, ok := byRef[edge.After]; !ok {
			return CompiledComposition{}, newError("CompileComposition", field, ErrHiddenChildIntent,
				"edge ends at undeclared child %q", edge.After)
		}
	}
	if err := checkAcyclic(byRef, plan.Edges); err != nil {
		return CompiledComposition{}, err
	}
	systems := make(map[string]string, len(byRef))
	for key, child := range byRef {
		systems[key] = child.System
	}
	for i, group := range plan.AtomicGroups {
		field := fmt.Sprintf("atomic_groups[%d]", i)
		seen := make(map[string]bool, len(group))
		for _, ref := range group {
			if _, ok := byRef[ref]; !ok {
				return CompiledComposition{}, newError("CompileComposition", field, ErrHiddenChildIntent,
					"atomic group names undeclared child %q", ref)
			}
			seen[ref] = true
		}
		first := ""
		for ref := range seen {
			if first == "" {
				first = systems[ref]
			} else if systems[ref] != first {
				return CompiledComposition{}, newError("CompileComposition", field, ErrCrossSystemAtomicity,
					"atomic group spans %q and %q", first, systems[ref])
			}
		}
	}
	if plan.MaxCost <= 0 || totalCost > plan.MaxCost {
		return CompiledComposition{}, newError("CompileComposition", "max_cost", ErrBudgetExceeded,
			"cost %d against limit %d", totalCost, plan.MaxCost)
	}
	if plan.MaxChildren <= 0 || len(plan.Children) > plan.MaxChildren {
		return CompiledComposition{}, newError("CompileComposition", "max_children", ErrBudgetExceeded,
			"%d children against limit %d", len(plan.Children), plan.MaxChildren)
	}
	return CompiledComposition{Plan: plan, Digest: compositionDigest(plan)}, nil
}

// checkAcyclic rejects dependency graphs that are not DAGs.
func checkAcyclic(children map[string]ChildTemplate, edges []DependencyEdge) error {
	indegree := make(map[string]int, len(children))
	next := make(map[string][]string, len(children))
	for key := range children {
		indegree[key] = 0
	}
	for _, edge := range edges {
		next[edge.Before] = append(next[edge.Before], edge.After)
		indegree[edge.After]++
	}
	var queue []string
	for key, degree := range indegree {
		if degree == 0 {
			queue = append(queue, key)
		}
	}
	sort.Strings(queue)
	visited := 0
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		following := append([]string(nil), next[current]...)
		sort.Strings(following)
		for _, after := range following {
			indegree[after]--
			if indegree[after] == 0 {
				queue = append(queue, after)
			}
		}
		sort.Strings(queue)
	}
	if visited != len(children) {
		return newError("CompileComposition", "edges", ErrCompositionCycle,
			"dependency graph is not acyclic")
	}
	return nil
}

// compositionDigest binds one deterministic identity over the whole plan.
func compositionDigest(plan CompositionPlan) string {
	parts := []string{"composition", childKey(plan.ParentDefinition), plan.ParentProposal,
		plan.ParentTenant, plan.ParentOrg, plan.ParentPurpose,
		string(plan.Boundary), string(plan.Wait), string(plan.Failure),
		string(plan.Correction), string(plan.Cancellation),
		fmt.Sprintf("max_cost=%d", plan.MaxCost), fmt.Sprintf("max_children=%d", plan.MaxChildren)}
	keys := make([]string, 0, len(plan.Children))
	byKey := make(map[string]ChildTemplate, len(plan.Children))
	for _, child := range plan.Children {
		key := childKey(child.Definition)
		keys = append(keys, key)
		byKey[key] = child
	}
	sort.Strings(keys)
	for _, key := range keys {
		child := byKey[key]
		delegation := append([]string(nil), child.Delegation...)
		sort.Strings(delegation)
		parts = append(parts, strings.Join([]string{key, fmt.Sprintf("ordinal=%d", child.Ordinal),
			child.Owner, child.System, child.Tenant, child.Org, child.Purpose,
			strings.Join(delegation, ","), fmt.Sprintf("cost=%d", child.Cost)}, "\x01"))
	}
	edges := make([]string, 0, len(plan.Edges))
	for _, edge := range plan.Edges {
		edges = append(edges, edge.Before+"\x01"+edge.After)
	}
	sort.Strings(edges)
	parts = append(parts, edges...)
	for _, group := range plan.AtomicGroups {
		ordered := append([]string(nil), group...)
		sort.Strings(ordered)
		parts = append(parts, "atomic:"+strings.Join(ordered, ","))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BundleComposition packages one compiled plan as inspection evidence.
func BundleComposition(compiled CompiledComposition, evidence []string) IntentBundle {
	keys := make([]string, 0, len(compiled.Plan.Children))
	for _, child := range compiled.Plan.Children {
		keys = append(keys, childKey(child.Definition))
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(compiled.Digest + "\x00" + strings.Join(keys, "\x00")))
	return IntentBundle{
		PlanDigest: compiled.Digest,
		Children:   keys,
		Evidence:   append([]string(nil), evidence...),
		Digest:     "sha256:" + hex.EncodeToString(sum[:]),
	}
}

// VerifyBundle proves one bundle still matches its plan: any post-approval
// change fails instead of silently rebinding.
func VerifyBundle(bundle IntentBundle, plan CompositionPlan) error {
	compiled, err := CompileComposition(plan)
	if err != nil {
		return err
	}
	if compiled.Digest != bundle.PlanDigest {
		return newError("VerifyBundle", "plan_digest", ErrBundleMismatch,
			"plan digest %q does not match bundle %q", compiled.Digest, bundle.PlanDigest)
	}
	return nil
}
