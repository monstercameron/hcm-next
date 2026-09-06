package workflow

import (
	"sort"

	"github.com/monstercameron/hcm-next/internal/capability"
)

// BranchConsistencyVector is one concurrent branch's declared consistency
// surface: which nodes run on it, which logical effects it can produce, and
// which data domains it reads and writes.
//
// It is a *declared* surface, taken from the capability manifests the branch's
// nodes bind, never from what a run happens to do. That is the whole point:
// two branches are proved disjoint at publication, so the runtime never has
// to discover a write conflict by colliding at execution time.
type BranchConsistencyVector struct {
	// ParallelNodeID is the PARALLEL node that opens this branch.
	ParallelNodeID string `json:"parallel_node_id"`
	// EntryNodeID is the branch's first node -- the fan-out edge's target.
	// It is the branch's identity: two edges to one node are one branch.
	EntryNodeID string `json:"entry_node_id"`
	// JoinNodeIDs are the JOIN nodes this branch reaches, sorted. A branch
	// that reaches none is joined by nothing, which is only legal when the
	// branch declares no write.
	JoinNodeIDs []string `json:"join_node_ids,omitempty"`
	// NodeIDs are every node on the branch, sorted, up to but excluding the
	// JOIN that closes it.
	NodeIDs []string `json:"node_ids"`
	// WriteKeys are the logical effect keys the branch can produce, sorted.
	WriteKeys []string `json:"write_keys,omitempty"`
	// WriteDomains and ReadDomains are the declared data domains the branch's
	// capability manifests write and read, sorted.
	WriteDomains []string `json:"write_domains,omitempty"`
	ReadDomains  []string `json:"read_domains,omitempty"`
	// Irreversible reports that the branch carries at least one irreversible
	// external mutation, so cancelling the sibling branches cannot unwind it.
	Irreversible bool `json:"irreversible,omitempty"`
}

// JoinContract is the compiled accounting one JOIN performs: which branches
// it is declared to receive and which of their effects it therefore accounts
// for. A JOIN is where the parallel region's consistency is re-established,
// so an effect produced on a branch that reaches no JOIN is an effect nothing
// accounts for.
type JoinContract struct {
	JoinNodeID string `json:"join_node_id"`
	// ParallelNodeIDs are the PARALLEL nodes whose branches reach this JOIN,
	// sorted.
	ParallelNodeIDs []string `json:"parallel_node_ids,omitempty"`
	// BranchEntryNodeIDs names the branches, by entry node, this JOIN closes.
	BranchEntryNodeIDs []string `json:"branch_entry_node_ids,omitempty"`
	// ArrivingNodeIDs are the JOIN's own incoming edge sources, sorted: the
	// arrivals a runtime counts. It is stated separately from the branch
	// entries because a branch is many nodes long and only its last node
	// arrives.
	ArrivingNodeIDs []string `json:"arriving_node_ids"`
	// AccountedWriteKeys is the union of the joined branches' write keys,
	// sorted: exactly the effects that must be reconciled here.
	AccountedWriteKeys []string `json:"accounted_write_keys,omitempty"`
}

// AtomicRegion is a span of the graph whose side effects must complete, fail
// or compensate together. It opens at a node carrying an external or
// irreversible external mutation and closes at the OBSERVE nodes that confirm
// what that mutation actually did.
//
// Between those two points the instance holds an effect nobody has observed
// yet, so pause, cancellation, migration and rewind are refused there: an
// administrative intervention inside the region would leave a partial effect
// with no record of what happened. This is the compiled form of the spec's
// "Safe point A -> atomic execution region -> Safe point B".
type AtomicRegion struct {
	// EntryNodeID is the mutating node that opens the region. The entry is
	// itself a safe point: the mutation has not run yet when the frontier
	// sits on it.
	EntryNodeID string `json:"entry_node_id"`
	// ExitNodeIDs are the OBSERVE nodes that close the region, sorted.
	ExitNodeIDs []string `json:"exit_node_ids,omitempty"`
	// InteriorNodeIDs are every node strictly inside the region -- everything
	// reachable after the entry up to and including the exits, sorted. These
	// are the positions at which intervention is refused.
	InteriorNodeIDs []string `json:"interior_node_ids,omitempty"`
	// EffectKeys are the logical effects the region holds open, sorted.
	EffectKeys []string `json:"effect_keys,omitempty"`
}

// ConcurrencySummary is WF-COMP-004's compiled analysis: the branch
// consistency vectors, the join contracts, the atomic regions and the derived
// intervention eligibility a runtime reads instead of guessing.
//
// A plan with no PARALLEL node and no unobserved external mutation has
// nothing to state here, and [Compile] leaves the field nil rather than
// emitting an empty object. That keeps the canonical bytes -- and therefore
// the content digest -- of every already-published plan unchanged by this
// analysis: a compiler pass that silently re-digested every activated version
// would invalidate the governance records pinning them.
type ConcurrencySummary struct {
	Branches      []BranchConsistencyVector `json:"branches,omitempty"`
	Joins         []JoinContract            `json:"joins,omitempty"`
	AtomicRegions []AtomicRegion            `json:"atomic_regions,omitempty"`
	// InterventionIneligibleNodes is the union of every atomic region's
	// interior, sorted: the frontier positions at which pause (WF-RUN-008),
	// cancellation (WF-RUN-010) and migration are refused.
	InterventionIneligibleNodes []string `json:"intervention_ineligible_nodes,omitempty"`
}

// InAtomicRegion reports whether nodeID sits strictly inside a compiled
// atomic region, so an intervention with the frontier there would abandon an
// unobserved effect.
//
// A plan that declares no atomic region answers false for every node, which
// is the correct answer for a zero-effect plan rather than a missing one.
func (p *CompiledWorkflow) InAtomicRegion(nodeID string) bool {
	if p == nil || p.Concurrency == nil {
		return false
	}
	for _, id := range p.Concurrency.InterventionIneligibleNodes {
		if id == nodeID {
			return true
		}
	}
	return false
}

// InterventionEligible reports whether an administrative intervention (pause,
// cancellation, migration) may take effect with the frontier sitting on
// nodeID. It is the negation of [CompiledWorkflow.InAtomicRegion], named for
// the question a runtime actually asks.
func (p *CompiledWorkflow) InterventionEligible(nodeID string) bool {
	return !p.InAtomicRegion(nodeID)
}

// analyzeConcurrency proves the parallel-write and safe-point properties
// WF-COMP-004 owns and returns the compiled facts, or nil when the plan has
// none to state.
//
// It reports three refusals:
//
//   - CONFLICTING_WRITE_SET: two branches of one PARALLEL whose declared
//     write sets intersect, by logical effect key or by data domain. Their
//     interleaving is not defined, so the plan does not publish.
//   - UNACCOUNTED_BRANCH_EFFECT: a branch that declares a write and reaches
//     no JOIN. Nothing reconciles that effect with its siblings.
//   - UNSAFE_CHECKPOINT: an author-requested safe point strictly inside an
//     atomic region. Safe points are compiled facts, so the request is
//     refused rather than quietly dropped or quietly honored.
func analyzeConcurrency(
	def *Definition, g *graph, records map[string]capability.Record, c *collector,
) *ConcurrencySummary {
	regions := findAtomicRegions(def, g, records)
	ineligible := interiorUnion(regions)
	checkRequestedSafePoints(def, ineligible, c)

	branches, joins := analyzeParallelBranches(def, g, records, c)

	if len(branches) == 0 && len(joins) == 0 && len(regions) == 0 {
		return nil
	}
	return &ConcurrencySummary{
		Branches:                    branches,
		Joins:                       joins,
		AtomicRegions:               regions,
		InterventionIneligibleNodes: sortedSet(ineligible),
	}
}

// findAtomicRegions walks forward from every external or irreversible
// mutation to the OBSERVE nodes that close it. Descent stops at an OBSERVE
// (the region's exit) and at a terminal, so the interior is exactly "the
// effect is done and nobody has confirmed it yet".
func findAtomicRegions(def *Definition, g *graph, records map[string]capability.Record) []AtomicRegion {
	var out []AtomicRegion
	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		class := effectClassOf(n, records)
		if class != capability.EffectExternalMutation && class != capability.EffectIrreversibleExternalMutation {
			continue
		}
		interior := map[string]bool{}
		exits := map[string]bool{}
		stack := make([]string, 0, len(g.out[id]))
		for _, e := range g.out[id] {
			stack = append(stack, e.To)
		}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if interior[cur] || cur == id {
				continue
			}
			node, ok := g.nodes[cur]
			if !ok {
				continue
			}
			if node.Type == StepEnd {
				// A terminal ends the instance; there is no position there to
				// intervene at. A terminal reached with the effect still
				// unobserved is the spec's high-risk repair case, and the END
				// node's own terminal tuple is what records it.
				continue
			}
			interior[cur] = true
			if node.Type == StepObserve {
				// The observation closes the region: it is the last unsafe
				// position, and everything after it is safe point B.
				exits[cur] = true
				continue
			}
			for _, e := range g.out[cur] {
				stack = append(stack, e.To)
			}
		}
		region := AtomicRegion{
			EntryNodeID:     id,
			ExitNodeIDs:     sortedSet(exits),
			InteriorNodeIDs: sortedSet(interior),
		}
		if key := effectKeyOf(n, records); key != "" {
			region.EffectKeys = []string{key}
		}
		out = append(out, region)
	}
	_ = def
	sort.Slice(out, func(i, j int) bool { return out[i].EntryNodeID < out[j].EntryNodeID })
	return out
}

func interiorUnion(regions []AtomicRegion) map[string]bool {
	out := map[string]bool{}
	for _, r := range regions {
		for _, id := range r.InteriorNodeIDs {
			out[id] = true
		}
	}
	return out
}

// checkRequestedSafePoints refuses an author's safe-point request inside an
// atomic region. A request outside one is honored by [placeSafePoints]; the
// compiler still decides, and this is the diagnostic that says so out loud
// instead of dropping the request in silence.
func checkRequestedSafePoints(def *Definition, ineligible map[string]bool, c *collector) {
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if n.SafePointRequested && ineligible[n.ID] {
			c.add(CodeUnsafeCheckpoint, Location{NodeID: n.ID},
				"a safe point is requested here, but this node is inside an atomic region: "+
					"an intervention at this position would abandon an effect no OBSERVE has confirmed")
		}
	}
}

// analyzeParallelBranches builds one [BranchConsistencyVector] per fan-out
// edge of every PARALLEL node, one [JoinContract] per JOIN, and reports the
// conflicting and unaccounted branch effects.
func analyzeParallelBranches(
	def *Definition, g *graph, records map[string]capability.Record, c *collector,
) ([]BranchConsistencyVector, []JoinContract) {
	var branches []BranchConsistencyVector
	joinBranches := map[string]map[string]bool{}
	joinParallels := map[string]map[string]bool{}
	joinKeys := map[string]map[string]bool{}

	for _, pid := range g.sortedNodeIDs() {
		if g.nodes[pid].Type != StepParallel {
			continue
		}
		entries := branchEntries(g, pid)
		vectors := make([]BranchConsistencyVector, 0, len(entries))
		for _, entry := range entries {
			vectors = append(vectors, branchVector(g, records, pid, entry))
		}
		checkBranchConflicts(pid, vectors, c)
		for _, v := range vectors {
			if len(v.JoinNodeIDs) == 0 && len(v.WriteKeys)+len(v.WriteDomains) > 0 {
				c.add(CodeUnaccountedBranchEffect, Location{NodeID: v.EntryNodeID},
					"branch of PARALLEL %q declares writes %v but reaches no JOIN, so no join contract accounts for them",
					pid, append(append([]string(nil), v.WriteKeys...), v.WriteDomains...))
			}
			for _, jid := range v.JoinNodeIDs {
				addTo(joinBranches, jid, v.EntryNodeID)
				addTo(joinParallels, jid, pid)
				for _, k := range v.WriteKeys {
					addTo(joinKeys, jid, k)
				}
			}
		}
		branches = append(branches, vectors...)
	}

	var joins []JoinContract
	for _, jid := range g.sortedNodeIDs() {
		if g.nodes[jid].Type != StepJoin {
			continue
		}
		arriving := map[string]bool{}
		for _, e := range g.in[jid] {
			arriving[e.From] = true
		}
		joins = append(joins, JoinContract{
			JoinNodeID:         jid,
			ParallelNodeIDs:    sortedSet(joinParallels[jid]),
			BranchEntryNodeIDs: sortedSet(joinBranches[jid]),
			ArrivingNodeIDs:    sortedSet(arriving),
			AccountedWriteKeys: sortedSet(joinKeys[jid]),
		})
	}
	_ = def
	return branches, joins
}

func addTo(m map[string]map[string]bool, key, value string) {
	if m[key] == nil {
		m[key] = map[string]bool{}
	}
	m[key][value] = true
}

// branchEntries lists the distinct targets of a PARALLEL node's fan-out
// route, sorted. Every one of them opens a concurrent branch; the FAILED
// route does not, because it is the PARALLEL's own failure continuation
// rather than a branch.
func branchEntries(g *graph, parallelID string) []string {
	fanOut := ""
	if conf, ok := ConformanceFor(StepParallel); ok {
		fanOut = string(conf.FanOutRoute)
	}
	seen := map[string]bool{}
	for _, e := range g.out[parallelID] {
		if e.RouteKey == fanOut {
			seen[e.To] = true
		}
	}
	return sortedSet(seen)
}

// branchVector walks one branch from its entry to the JOIN nodes that close
// it, accumulating the declared read/write surface of every node on the way.
// A JOIN is a boundary: it belongs to no branch, and descent stops there.
func branchVector(
	g *graph, records map[string]capability.Record, parallelID, entry string,
) BranchConsistencyVector {
	members := map[string]bool{}
	joins := map[string]bool{}
	stack := []string{entry}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node, ok := g.nodes[cur]
		if !ok {
			continue
		}
		if node.Type == StepJoin {
			joins[cur] = true
			continue
		}
		if members[cur] {
			continue
		}
		members[cur] = true
		for _, e := range g.out[cur] {
			stack = append(stack, e.To)
		}
	}

	v := BranchConsistencyVector{
		ParallelNodeID: parallelID,
		EntryNodeID:    entry,
		JoinNodeIDs:    sortedSet(joins),
		NodeIDs:        sortedSet(members),
	}
	writeKeys := map[string]bool{}
	writeDomains := map[string]bool{}
	readDomains := map[string]bool{}
	for _, id := range v.NodeIDs {
		n := g.nodes[id]
		class := effectClassOf(n, records)
		if class == capability.EffectIrreversibleExternalMutation {
			v.Irreversible = true
		}
		if class.IsWrite() {
			if key := effectKeyOf(n, records); key != "" {
				writeKeys[key] = true
			}
		}
		rec, ok := records[id]
		if !ok {
			continue
		}
		if class.IsWrite() {
			for _, d := range rec.Definition.WriteData.DataDomains {
				writeDomains[d] = true
			}
		}
		for _, d := range rec.Definition.ReadData.DataDomains {
			readDomains[d] = true
		}
	}
	v.WriteKeys = sortedSet(writeKeys)
	v.WriteDomains = sortedSet(writeDomains)
	v.ReadDomains = sortedSet(readDomains)
	return v
}

// checkBranchConflicts refuses two branches of one PARALLEL whose declared
// write sets intersect. Effect keys are compared first because they are the
// exact identity a retry deduplicates on; data domains are compared as well,
// so two different capabilities writing the same domain concurrently are
// caught even though their effect keys differ.
func checkBranchConflicts(parallelID string, vectors []BranchConsistencyVector, c *collector) {
	for i := 0; i < len(vectors); i++ {
		for j := i + 1; j < len(vectors); j++ {
			a, b := vectors[i], vectors[j]
			if shared := intersectSorted(a.WriteKeys, b.WriteKeys); len(shared) > 0 {
				c.add(CodeConflictingWriteSet, Location{NodeID: parallelID},
					"branches %q and %q both write effect key(s) %v; concurrent branches declare disjoint write sets",
					a.EntryNodeID, b.EntryNodeID, shared)
				continue
			}
			if shared := intersectSorted(a.WriteDomains, b.WriteDomains); len(shared) > 0 {
				c.add(CodeConflictingWriteSet, Location{NodeID: parallelID},
					"branches %q and %q both write data domain(s) %v; concurrent branches declare disjoint write sets",
					a.EntryNodeID, b.EntryNodeID, shared)
			}
		}
	}
}

func intersectSorted(a, b []string) []string {
	in := make(map[string]bool, len(a))
	for _, s := range a {
		in[s] = true
	}
	var out []string
	for _, s := range b {
		if in[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// sortedSet renders a set as a sorted slice, or nil when the set is
// empty, so an omitempty field stays absent rather than becoming [].
func sortedSet(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
