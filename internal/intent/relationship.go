package intent

import (
	"slices"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// RelationKind is how one intent relates to another.
//
// There are nine, and they are never conflated, because each one answers a
// different question and each one implies different completion and propagation
// behaviour. A CHILD is part of its parent's work; a DEPENDENCY must land
// before this one can; a FOLLOW_UP happens afterwards and stands on its own; a
// CORRECTION restates business truth; a COMPENSATION undoes an effect;
// a REPAIR fixes a consistency failure without changing what was intended;
// a SUPERSESSION replaces an intent that never completed; an ALTERNATIVE is a
// competing proposal for the same decision; and TRIGGERED_BY records that a
// completed intent caused this one without owning it.
//
// Collapsing any pair loses the answer. "Compensation" folded into
// "correction" makes an undo look like a restatement of truth; "child" folded
// into "dependency" makes a composite's obligations invisible at completion.
type RelationKind uint8

// RelationKind values.
const (
	RelationUnspecified RelationKind = iota
	RelationChild
	RelationDependency
	RelationFollowUp
	RelationCorrection
	RelationCompensation
	RelationRepair
	RelationSupersedes
	RelationAlternative
	RelationTriggeredBy
)

var relationKindNames = map[RelationKind]string{
	RelationUnspecified:  "UNSPECIFIED",
	RelationChild:        "CHILD",
	RelationDependency:   "DEPENDENCY",
	RelationFollowUp:     "FOLLOW_UP",
	RelationCorrection:   "CORRECTION",
	RelationCompensation: "COMPENSATION",
	RelationRepair:       "REPAIR",
	RelationSupersedes:   "SUPERSEDES",
	RelationAlternative:  "ALTERNATIVE",
	RelationTriggeredBy:  "TRIGGERED_BY",
}

func (k RelationKind) String() string { return enumName(relationKindNames, k, "RelationKind") }

// Valid reports whether k is a declared relation kind other than UNSPECIFIED.
func (k RelationKind) Valid() bool {
	_, ok := relationKindNames[k]
	return ok && k != RelationUnspecified
}

// RelationKinds returns the nine relation kinds in declaration order.
func RelationKinds() []RelationKind {
	return []RelationKind{
		RelationChild, RelationDependency, RelationFollowUp, RelationCorrection,
		RelationCompensation, RelationRepair, RelationSupersedes,
		RelationAlternative, RelationTriggeredBy,
	}
}

// ParseRelationKind resolves a canonical relation-kind name.
func ParseRelationKind(name string) (RelationKind, error) {
	for k, n := range relationKindNames {
		if n == name && k.Valid() {
			return k, nil
		}
	}
	return RelationUnspecified, newError("ParseRelationKind", "relationship.kind", ErrInvalidRelationship,
		"%q is not one of the nine intent relation kinds", name)
}

// ProhibitsCycles reports whether a cycle in this relation kind is a defect.
//
// Every kind that expresses "this came from that", "this waits for that" or
// "this replaces that" is a chronology, and a chronology cannot loop.
// ALTERNATIVE is the exception: two competing proposals are each other's
// alternative, and refusing that would force an arbitrary winner into a
// relationship that has none.
func (k RelationKind) ProhibitsCycles() bool { return k != RelationAlternative }

// BindsProposal reports whether the relationship must pin an exact proposal
// revision on its parent, as opposed to the parent intent alone.
//
// A child emitted from a proposal, an alternative to a proposal, and a
// correction, compensation or supersession of one all refer to specific
// proposed content: recording them against the intent alone would leave it
// ambiguous which revision they answer once a second revision exists.
func (k RelationKind) BindsProposal() bool {
	switch k {
	case RelationChild, RelationCorrection, RelationCompensation,
		RelationSupersedes, RelationAlternative:
		return true
	default:
		return false
	}
}

// IntentVersionRef pins one endpoint of a relationship to exact versions.
type IntentVersionRef struct {
	IntentID        string
	InstanceVersion uint64

	// ProposalRevisionID and ProposalRevision pin the exact proposal content
	// the relationship refers to. They are required on the parent endpoint of
	// a kind whose BindsProposal is true.
	ProposalRevisionID string
	ProposalRevision   uint64
}

// Validate rejects an endpoint that names no intent or no instance version.
func (r IntentVersionRef) Validate(field string) error {
	if err := requireCanonicalText(field+".intent_id", r.IntentID); err != nil {
		return newError("Validate", field+".intent_id", ErrInvalidRelationship, "%v", err)
	}
	if r.InstanceVersion == 0 {
		return newError("Validate", field+".instance_version", ErrInvalidRelationship,
			"endpoint %q pins no instance version", r.IntentID)
	}
	if (r.ProposalRevisionID == "") != (r.ProposalRevision == 0) {
		return newError("Validate", field+".proposal_revision", ErrInvalidRelationship,
			"endpoint %q pins half a proposal revision", r.IntentID)
	}
	return nil
}

// Cardinality is how many relationships of one kind a parent may hold.
type Cardinality uint8

// Cardinality values.
const (
	CardinalityUnspecified Cardinality = iota
	CardinalityExactlyOne
	CardinalityAtMostOne
	CardinalityOneOrMore
	CardinalityZeroOrMore
)

var cardinalityNames = map[Cardinality]string{
	CardinalityUnspecified: "UNSPECIFIED",
	CardinalityExactlyOne:  "EXACTLY_ONE",
	CardinalityAtMostOne:   "AT_MOST_ONE",
	CardinalityOneOrMore:   "ONE_OR_MORE",
	CardinalityZeroOrMore:  "ZERO_OR_MORE",
}

func (c Cardinality) String() string { return enumName(cardinalityNames, c, "Cardinality") }

// Valid reports whether c is a declared cardinality other than UNSPECIFIED.
func (c Cardinality) Valid() bool {
	_, ok := cardinalityNames[c]
	return ok && c != CardinalityUnspecified
}

// maxOccurrences returns the upper bound a cardinality allows, and whether
// there is one.
func (c Cardinality) maxOccurrences() (int, bool) {
	switch c {
	case CardinalityExactlyOne, CardinalityAtMostOne:
		return 1, true
	default:
		return 0, false
	}
}

// Propagation is the lifecycle signal a parent sends its related intent. It is
// deliberately a signal vocabulary and not an ownership one: there is no
// CASCADE_DELETE, because a relationship is semantic chronology and never
// foreign-key ownership. A related intent that must stop is cancelled through
// its own governed cancellation path, leaving its own record.
type Propagation uint8

// Propagation values.
const (
	PropagationUnspecified Propagation = iota
	// PropagationNone means the parent's lifecycle says nothing about this
	// relationship's other end.
	PropagationNone
	// PropagationRequestCancellation asks the related intent to run its own
	// governed cancellation. It is a request, not a deletion.
	PropagationRequestCancellation
	// PropagationHold suspends the related intent while the parent is blocked.
	PropagationHold
	// PropagationBlockClosure stops the parent closing while the related
	// intent is unresolved.
	PropagationBlockClosure
)

var propagationNames = map[Propagation]string{
	PropagationUnspecified:         "UNSPECIFIED",
	PropagationNone:                "NONE",
	PropagationRequestCancellation: "REQUEST_CANCELLATION",
	PropagationHold:                "HOLD",
	PropagationBlockClosure:        "BLOCK_CLOSURE",
}

func (p Propagation) String() string { return enumName(propagationNames, p, "Propagation") }

// Valid reports whether p is a declared propagation other than UNSPECIFIED.
func (p Propagation) Valid() bool {
	_, ok := propagationNames[p]
	return ok && p != PropagationUnspecified
}

// Completion is how the related intent's result bears on the parent's.
type Completion uint8

// Completion values.
const (
	CompletionUnspecified Completion = iota
	// CompletionMandatory means the parent may not report a result until this
	// relationship's other end has one.
	CompletionMandatory
	// CompletionOptional means the parent reports its own result and records
	// the other end's separately.
	CompletionOptional
	// CompletionDetached means the other end runs on its own governance and
	// the parent never waits.
	CompletionDetached
)

var completionNames = map[Completion]string{
	CompletionUnspecified: "UNSPECIFIED",
	CompletionMandatory:   "MANDATORY",
	CompletionOptional:    "OPTIONAL",
	CompletionDetached:    "DETACHED",
}

func (c Completion) String() string { return enumName(completionNames, c, "Completion") }

// Valid reports whether c is a declared completion policy other than
// UNSPECIFIED.
func (c Completion) Valid() bool {
	_, ok := completionNames[c]
	return ok && c != CompletionUnspecified
}

// RelationshipRevision is one immutable revision of one intent-to-intent
// relationship.
//
// Parentage is immutable: RelationshipID, Kind, Parent, Child and Ordinal are
// fixed at revision 1 and may never change. A later revision may only refine
// the policy fields - cardinality, propagation and completion - which is what
// lets a governance change adjust how a composite waits without rewriting the
// history of what was related to what.
type RelationshipRevision struct {
	RelationshipID string
	Revision       uint64

	Tenant values.TenantId

	Kind    RelationKind
	Parent  IntentVersionRef
	Child   IntentVersionRef
	Ordinal uint32

	// CauseRef names why this relationship exists: the emitting node, the
	// detected inconsistency, the approval decision. It is a reference, not
	// prose.
	CauseRef string

	// Purpose is the purpose of processing the relationship inherits. A
	// relationship with no purpose cannot be disclosed or retained correctly.
	Purpose string

	Cardinality Cardinality
	Propagation Propagation
	Completion  Completion

	// Material says the other end carries material work of its own. A material
	// relationship must be disclosed: a composite that hides one is reporting
	// a result it does not have.
	Material bool

	// Disclosed says the relationship is visible wherever the parent is
	// visible.
	Disclosed bool

	RecordedAt values.Instant
}

// Validate checks everything one revision can assert about itself.
func (r RelationshipRevision) Validate() error {
	if err := requireCanonicalText("relationship.relationship_id", r.RelationshipID); err != nil {
		return newError("Validate", "relationship.relationship_id", ErrInvalidRelationship, "%v", err)
	}
	if r.Revision == 0 {
		return newError("Validate", "relationship.revision", ErrInvalidRelationship,
			"relationship %q is numbered from zero; revisions start at 1", r.RelationshipID)
	}
	if err := r.Tenant.Validate(); err != nil {
		return newError("Validate", "relationship.tenant_id", ErrInvalidRelationship, "%v", err)
	}
	if !r.Kind.Valid() {
		return newError("Validate", "relationship.kind", ErrInvalidRelationship,
			"relationship %q names no relation kind", r.RelationshipID)
	}
	if err := r.Parent.Validate("relationship.parent"); err != nil {
		return err
	}
	if err := r.Child.Validate("relationship.child"); err != nil {
		return err
	}
	if r.Parent.IntentID == r.Child.IntentID {
		return newError("Validate", "relationship.child", ErrRelationshipCycle,
			"intent %q is its own %s", r.Parent.IntentID, r.Kind)
	}
	if r.Kind.BindsProposal() && r.Parent.ProposalRevisionID == "" {
		return newError("Validate", "relationship.parent.proposal_revision_id", ErrInvalidRelationship,
			"a %s relationship pins no parent proposal revision", r.Kind)
	}
	for _, req := range []struct{ field, value string }{
		{"relationship.cause_ref", r.CauseRef},
		{"relationship.purpose", r.Purpose},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return newError("Validate", req.field, ErrInvalidRelationship, "%v", err)
		}
	}
	if !r.Cardinality.Valid() {
		return newError("Validate", "relationship.cardinality", ErrInvalidRelationship,
			"relationship %q declares no cardinality", r.RelationshipID)
	}
	if !r.Propagation.Valid() {
		return newError("Validate", "relationship.propagation", ErrInvalidRelationship,
			"relationship %q declares no lifecycle propagation policy", r.RelationshipID)
	}
	if !r.Completion.Valid() {
		return newError("Validate", "relationship.completion", ErrInvalidRelationship,
			"relationship %q declares no completion policy", r.RelationshipID)
	}
	if !r.RecordedAt.IsSet() {
		return newError("Validate", "relationship.recorded_at", ErrInvalidRelationship,
			"relationship %q records no time", r.RelationshipID)
	}
	if r.Material {
		if !r.Disclosed {
			return newError("Validate", "relationship.disclosed", ErrHiddenChildIntent,
				"relationship %q carries material work and is not disclosed", r.RelationshipID)
		}
		if r.Completion == CompletionDetached {
			return newError("Validate", "relationship.completion", ErrHiddenChildIntent,
				"relationship %q carries material work and is detached from its parent's completion",
				r.RelationshipID)
		}
	}
	return nil
}

// identity is the immutable part of a relationship: the part a later revision
// may not change.
type relationshipIdentity struct {
	kind    RelationKind
	parent  string
	child   string
	ordinal uint32
}

func (r RelationshipRevision) identity() relationshipIdentity {
	return relationshipIdentity{
		kind: r.Kind, parent: r.Parent.IntentID, child: r.Child.IntentID, ordinal: r.Ordinal,
	}
}

// RelationshipGraph is the compiled, checked relationship graph for one tenant.
// It holds the current revision of every relationship plus the full revision
// history, and it is built only by [NewRelationshipGraph], which is where every
// ambiguity rule lives.
type RelationshipGraph struct {
	tenant  values.TenantId
	current map[string]RelationshipRevision
	history map[string][]RelationshipRevision
	order   []string
}

// NewRelationshipGraph compiles a relationship revision log into a checked
// graph.
//
// It refuses, in this order: a revision that fails its own validation; a
// revision from another tenant; a history that is not numbered 1..N; a revision
// that re-parents, re-kinds or re-orders what an earlier revision fixed; two
// different kinds for one ordered pair; a duplicate ordinal under one parent
// and kind; a cardinality the sibling set exceeds; and a cycle in a kind that
// prohibits one.
func NewRelationshipGraph(tenant values.TenantId, revisions []RelationshipRevision) (*RelationshipGraph, error) {
	if err := tenant.Validate(); err != nil {
		return nil, newError("NewRelationshipGraph", "tenant_id", ErrInvalidRelationship, "%v", err)
	}
	history := map[string][]RelationshipRevision{}
	var order []string
	for _, rev := range revisions {
		if err := rev.Validate(); err != nil {
			return nil, err
		}
		if rev.Tenant != tenant {
			return nil, newError("NewRelationshipGraph", "relationship.tenant_id", ErrInvalidRelationship,
				"relationship %q belongs to tenant %s, not %s", rev.RelationshipID, rev.Tenant, tenant)
		}
		if _, seen := history[rev.RelationshipID]; !seen {
			order = append(order, rev.RelationshipID)
		}
		history[rev.RelationshipID] = append(history[rev.RelationshipID], rev)
	}

	current := make(map[string]RelationshipRevision, len(history))
	for id, revs := range history {
		sort.Slice(revs, func(i, j int) bool { return revs[i].Revision < revs[j].Revision })
		identity := revs[0].identity()
		for i, rev := range revs {
			if rev.Revision != uint64(i+1) {
				return nil, newError("NewRelationshipGraph", "relationship.revision", ErrImmutableRelationship,
					"relationship %q jumps to revision %d at position %d; revisions are 1..N",
					id, rev.Revision, i+1)
			}
			if rev.identity() != identity {
				return nil, newError("NewRelationshipGraph", "relationship.parent", ErrImmutableRelationship,
					"relationship %q revision %d changes its parentage, kind or ordinal",
					id, rev.Revision)
			}
		}
		history[id] = revs
		current[id] = revs[len(revs)-1]
	}

	g := &RelationshipGraph{tenant: tenant, current: current, history: history, order: order}
	if err := g.checkAmbiguity(); err != nil {
		return nil, err
	}
	if err := g.checkCycles(); err != nil {
		return nil, err
	}
	return g, nil
}

// checkAmbiguity enforces one kind per ordered pair, unique ordinals under one
// parent and kind, and the declared cardinality of each sibling set.
func (g *RelationshipGraph) checkAmbiguity() error {
	type pair struct{ parent, child string }
	type slot struct {
		parent  string
		kind    RelationKind
		ordinal uint32
	}
	kinds := map[pair]RelationshipRevision{}
	slots := map[slot]string{}
	counts := map[struct {
		parent string
		kind   RelationKind
	}]int{}

	for _, id := range g.order {
		rev := g.current[id]
		p := pair{rev.Parent.IntentID, rev.Child.IntentID}
		if prior, ok := kinds[p]; ok && prior.Kind != rev.Kind {
			return newError("NewRelationshipGraph", "relationship.kind", ErrAmbiguousRelationship,
				"%s -> %s is recorded as both %s and %s",
				p.parent, p.child, prior.Kind, rev.Kind)
		}
		kinds[p] = rev

		s := slot{rev.Parent.IntentID, rev.Kind, rev.Ordinal}
		if other, ok := slots[s]; ok && other != rev.Child.IntentID {
			return newError("NewRelationshipGraph", "relationship.ordinal", ErrAmbiguousRelationship,
				"parent %s has two %s relationships at ordinal %d: %s and %s",
				s.parent, s.kind, s.ordinal, other, rev.Child.IntentID)
		}
		slots[s] = rev.Child.IntentID

		key := struct {
			parent string
			kind   RelationKind
		}{rev.Parent.IntentID, rev.Kind}
		counts[key]++
		if max_, bounded := rev.Cardinality.maxOccurrences(); bounded && counts[key] > max_ {
			return newError("NewRelationshipGraph", "relationship.cardinality", ErrAmbiguousRelationship,
				"parent %s declares %s for %s and holds %d",
				key.parent, rev.Cardinality, key.kind, counts[key])
		}
	}
	return nil
}

// checkCycles walks each cycle-prohibiting kind's edges separately. Kinds are
// checked independently because a CHILD edge and a FOLLOW_UP edge between the
// same two intents are two different chronologies, and only a loop within one
// of them is a defect.
func (g *RelationshipGraph) checkCycles() error {
	edges := map[RelationKind]map[string][]string{}
	for _, id := range g.order {
		rev := g.current[id]
		if !rev.Kind.ProhibitsCycles() {
			continue
		}
		if edges[rev.Kind] == nil {
			edges[rev.Kind] = map[string][]string{}
		}
		edges[rev.Kind][rev.Parent.IntentID] = append(
			edges[rev.Kind][rev.Parent.IntentID], rev.Child.IntentID)
	}
	for _, kind := range RelationKinds() {
		adjacency, ok := edges[kind]
		if !ok {
			continue
		}
		roots := make([]string, 0, len(adjacency))
		for from := range adjacency {
			roots = append(roots, from)
		}
		slices.Sort(roots)

		const (
			unvisited = 0
			onStack   = 1
			done      = 2
		)
		state := map[string]int{}
		var walk func(node string) error
		walk = func(node string) error {
			state[node] = onStack
			for _, next := range adjacency[node] {
				switch state[next] {
				case onStack:
					return newError("NewRelationshipGraph", "relationship.child", ErrRelationshipCycle,
						"%s relationships form a cycle through %s and %s", kind, node, next)
				case unvisited:
					if err := walk(next); err != nil {
						return err
					}
				}
			}
			state[node] = done
			return nil
		}
		for _, root := range roots {
			if state[root] == unvisited {
				if err := walk(root); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Len returns the number of distinct relationships.
func (g *RelationshipGraph) Len() int { return len(g.current) }

// Current returns the current revision of one relationship.
func (g *RelationshipGraph) Current(relationshipID string) (RelationshipRevision, bool) {
	rev, ok := g.current[relationshipID]
	return rev, ok
}

// History returns every recorded revision of one relationship, oldest first.
func (g *RelationshipGraph) History(relationshipID string) []RelationshipRevision {
	return slices.Clone(g.history[relationshipID])
}

// Related returns the current revisions whose parent is intentID, in the order
// they were recorded. Passing a kind narrows the answer; RelationUnspecified
// returns every kind.
func (g *RelationshipGraph) Related(intentID string, kind RelationKind) []RelationshipRevision {
	var out []RelationshipRevision
	for _, id := range g.order {
		rev := g.current[id]
		if rev.Parent.IntentID != intentID {
			continue
		}
		if kind != RelationUnspecified && rev.Kind != kind {
			continue
		}
		out = append(out, rev)
	}
	return out
}

// BlocksCompletionOf returns the relationships whose other end must have a
// result before intentID may report one. It is the answer a composite asks
// before closing, and it is derived from the declared completion policy rather
// than from what a workflow node happens to have waited on.
func (g *RelationshipGraph) BlocksCompletionOf(intentID string) []RelationshipRevision {
	var out []RelationshipRevision
	for _, rev := range g.Related(intentID, RelationUnspecified) {
		if rev.Completion == CompletionMandatory || rev.Propagation == PropagationBlockClosure {
			out = append(out, rev)
		}
	}
	return out
}
