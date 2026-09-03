package workflow

import "sort"

// degradedRoutes are the outcome route keys that carry a degraded, unknown or
// refused result. A terminal one of them reaches directly may not claim a
// completed, consistent business outcome.
var degradedRoutes = map[string]bool{
	string(OutcomeUnknown):   true,
	string(OutcomeAmbiguous): true,
	string(OutcomePartial):   true,
	string(OutcomeFail):      true,
	string(OutcomeFailed):    true,
	string(OutcomeRejected):  true,
}

// graph is the analyzed shape of one definition (WF-COMP-002).
type graph struct {
	nodes map[string]*Node
	out   map[string][]Edge
	in    map[string][]Edge

	// reachable is forward reachability from the start node.
	reachable map[string]bool
	// terminating is reverse reachability from the END nodes.
	terminating map[string]bool
	// dominators[n] holds every node that runs before n on every path,
	// including n itself. A mapping may only read a strict dominator's output.
	dominators map[string]map[string]bool
	// order is a deterministic traversal order over the acyclic skeleton.
	order []string
	// depth is the longest distance from the start node over the acyclic
	// skeleton (declared back edges removed).
	depth map[string]uint32
	// backEdges are the edges that close a cycle.
	backEdges []Edge
	// degradedEntry marks nodes entered directly by a degraded outcome route.
	degradedEntry map[string]bool
	// sound reports whether every edge endpoint resolved, so later passes may
	// trust the topology.
	sound bool
}

// analyzeGraph proves reachability, explicit routing, termination, bounded
// cycles and bounded fan-out/depth. It reports every problem it finds rather
// than the first, and always returns a usable (possibly partial) graph so the
// mapping and step passes can still say something useful.
func analyzeGraph(def *Definition, c *collector) *graph {
	g := &graph{
		nodes:         def.nodeIndex(),
		out:           map[string][]Edge{},
		in:            map[string][]Edge{},
		reachable:     map[string]bool{},
		terminating:   map[string]bool{},
		dominators:    map[string]map[string]bool{},
		depth:         map[string]uint32{},
		degradedEntry: map[string]bool{},
		sound:         true,
	}

	for _, e := range def.Edges {
		loc := Location{NodeID: e.From, EdgeFrom: e.From, EdgeTo: e.To, RouteKey: e.RouteKey}
		if _, ok := g.nodes[e.From]; !ok {
			g.sound = false
			c.add(CodeUnresolvedRef, loc, "edge source %q is not a declared node", e.From)
			continue
		}
		if _, ok := g.nodes[e.To]; !ok {
			g.sound = false
			c.add(CodeUnresolvedRef, loc, "edge target %q is not a declared node", e.To)
			continue
		}
		g.out[e.From] = append(g.out[e.From], e)
		g.in[e.To] = append(g.in[e.To], e)
		if degradedRoutes[e.RouteKey] {
			g.degradedEntry[e.To] = true
		}
	}

	start, hasStart := g.nodes[def.StartNodeID]
	if !hasStart {
		g.sound = false
		c.add(CodeUnresolvedRef, Location{Ref: def.StartNodeID},
			"start_node_id %q is not a declared node", def.StartNodeID)
	}

	g.checkRoutes(def, c)
	if hasStart {
		g.computeReachability(start.ID)
		g.checkLimits(def, c)
		g.checkCycles(def, c)
		g.computeDominators(start.ID)
		g.computeDepth(def, c)
	}
	g.computeTermination()

	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		if hasStart && !g.reachable[id] {
			c.add(CodeUnreachableNode, Location{NodeID: id},
				"no path from start node %q reaches this node", def.StartNodeID)
		}
		if n.Type == StepEnd {
			continue
		}
		if hasStart && g.reachable[id] && !g.terminating[id] {
			c.add(CodeInvalidTerminalPath, Location{NodeID: id},
				"no path from this node reaches an END node")
		}
	}

	return g
}

func (g *graph) sortedNodeIDs() []string {
	out := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// expectedRoutes returns the exact outcome route keys a node must route, in
// deterministic order.
func expectedRoutes(n *Node) []string {
	conf, ok := ConformanceFor(n.Type)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(key string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, o := range conf.Outcomes {
		add(string(o))
	}
	if conf.AuthorRoutes && n.Decision != nil {
		for _, r := range n.Decision.Routes {
			add(r.Key)
		}
	}
	sort.Strings(out)
	return out
}

func (g *graph) checkRoutes(def *Definition, c *collector) {
	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		conf, known := ConformanceFor(n.Type)
		if !known {
			continue
		}
		edges := g.out[id]

		if conf.Terminal {
			for _, e := range edges {
				c.add(CodeInvalidTerminalPath,
					Location{NodeID: id, EdgeFrom: e.From, EdgeTo: e.To, RouteKey: e.RouteKey},
					"%s is terminal and cannot have an outgoing edge", n.Type)
			}
			continue
		}

		want := map[string]bool{}
		for _, key := range expectedRoutes(n) {
			want[key] = true
		}
		got := map[string]bool{}
		for _, e := range edges {
			loc := Location{NodeID: id, EdgeFrom: e.From, EdgeTo: e.To, RouteKey: e.RouteKey}
			if e.RouteKey == "" {
				c.add(CodeImplicitFirstEdge, loc,
					"every outgoing edge declares its outcome route key; there is no implicit first edge")
				continue
			}
			if got[e.RouteKey] {
				c.add(CodeDuplicateRoute, loc, "route %q is already routed from this node", e.RouteKey)
				continue
			}
			got[e.RouteKey] = true
			if !want[e.RouteKey] {
				c.add(CodeUnknownRoute, loc, "%s cannot produce outcome %q", n.Type, e.RouteKey)
			}
		}
		for _, key := range expectedRoutes(n) {
			if !got[key] {
				c.add(CodeMissingRoute, Location{NodeID: id, RouteKey: key},
					"%s outcome %q has no explicit route", n.Type, key)
			}
		}

		if n.FailureRoute != "" {
			if _, ok := g.nodes[n.FailureRoute]; !ok {
				c.add(CodeUnresolvedRef, Location{NodeID: id, Ref: n.FailureRoute},
					"failure_route %q is not a declared node", n.FailureRoute)
			}
		}
		if n.Observe != nil && n.Observe.RetryExhaustionRoute != "" {
			if _, ok := g.nodes[n.Observe.RetryExhaustionRoute]; !ok {
				c.add(CodeUnresolvedRef, Location{NodeID: id, Ref: n.Observe.RetryExhaustionRoute},
					"retry_exhaustion_route %q is not a declared node", n.Observe.RetryExhaustionRoute)
			}
		}
		_ = def
	}
}

func (g *graph) computeReachability(start string) {
	stack := []string{start}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if g.reachable[id] {
			continue
		}
		g.reachable[id] = true
		for _, e := range g.out[id] {
			stack = append(stack, e.To)
		}
	}
}

func (g *graph) computeTermination() {
	stack := make([]string, 0, len(g.nodes))
	for id, n := range g.nodes {
		if n.Type == StepEnd {
			stack = append(stack, id)
		}
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if g.terminating[id] {
			continue
		}
		g.terminating[id] = true
		for _, e := range g.in[id] {
			stack = append(stack, e.From)
		}
	}
}

// checkCycles finds every back edge and requires a declared, guarded, bounded
// cycle for it. A loop the compiler discovers but the author never declared is
// rejected: the runtime does not learn about loops at execution time.
func (g *graph) checkCycles(def *Definition, c *collector) {
	state := map[string]int{} // 0 unvisited, 1 on stack, 2 done
	var walk func(id string)
	walk = func(id string) {
		state[id] = 1
		for _, e := range g.out[id] {
			switch state[e.To] {
			case 0:
				walk(e.To)
			case 1:
				g.backEdges = append(g.backEdges, e)
			}
		}
		state[id] = 2
	}
	if g.reachable[def.StartNodeID] {
		walk(def.StartNodeID)
	}
	sort.SliceStable(g.backEdges, func(i, j int) bool {
		if g.backEdges[i].From != g.backEdges[j].From {
			return g.backEdges[i].From < g.backEdges[j].From
		}
		return g.backEdges[i].To < g.backEdges[j].To
	})

	for _, e := range g.backEdges {
		loc := Location{NodeID: e.From, EdgeFrom: e.From, EdgeTo: e.To, RouteKey: e.RouteKey}
		decl, found := declaredCycle(def, e.To)
		if !found {
			c.add(CodeUndeclaredCycle, loc,
				"edge closes a cycle back to %q with no declared cycle bound", e.To)
			continue
		}
		if decl.MaxIterations == 0 {
			c.add(CodeUndeclaredCycle, loc,
				"declared cycle at %q has no iteration bound", e.To)
		}
		guard, ok := g.nodes[decl.GuardNodeID]
		if !ok {
			c.add(CodeUndeclaredCycle, loc,
				"declared cycle guard %q is not a declared node", decl.GuardNodeID)
			continue
		}
		if guard.Type != StepDecision {
			c.add(CodeUndeclaredCycle, Location{NodeID: decl.GuardNodeID},
				"a cycle guard is a DECISION that can leave the loop; %q is %s",
				decl.GuardNodeID, guard.Type)
			continue
		}
		if !g.reaches(e.To, decl.GuardNodeID) || !g.reaches(decl.GuardNodeID, e.From) {
			c.add(CodeUndeclaredCycle, loc,
				"cycle guard %q does not lie on the path from %q back to %q",
				decl.GuardNodeID, e.To, e.From)
		}
	}
}

func declaredCycle(def *Definition, entry string) (CycleDeclaration, bool) {
	for _, d := range def.Limits.DeclaredCycles {
		if d.EntryNodeID == entry {
			return d, true
		}
	}
	return CycleDeclaration{}, false
}

// reaches reports whether to is reachable from from.
func (g *graph) reaches(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.out[id] {
			if e.To == to {
				return true
			}
			if !seen[e.To] {
				seen[e.To] = true
				stack = append(stack, e.To)
			}
		}
	}
	return false
}

// computeDominators runs the classic iterative dominator fixpoint over the
// reachable subgraph.
func (g *graph) computeDominators(start string) {
	ids := make([]string, 0, len(g.nodes))
	for _, id := range g.sortedNodeIDs() {
		if g.reachable[id] {
			ids = append(ids, id)
		}
	}
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	for _, id := range ids {
		if id == start {
			g.dominators[id] = map[string]bool{start: true}
			continue
		}
		set := make(map[string]bool, len(all))
		for k := range all {
			set[k] = true
		}
		g.dominators[id] = set
	}

	for changed := true; changed; {
		changed = false
		for _, id := range ids {
			if id == start {
				continue
			}
			var next map[string]bool
			for _, e := range g.in[id] {
				if !g.reachable[e.From] {
					continue
				}
				pred := g.dominators[e.From]
				if next == nil {
					next = make(map[string]bool, len(pred))
					for k := range pred {
						next[k] = true
					}
					continue
				}
				for k := range next {
					if !pred[k] {
						delete(next, k)
					}
				}
			}
			if next == nil {
				next = map[string]bool{}
			}
			next[id] = true
			if !sameSet(next, g.dominators[id]) {
				g.dominators[id] = next
				changed = true
			}
		}
	}
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// dominates reports whether ancestor runs before node on every path, and is
// not the node itself.
func (g *graph) dominates(ancestor, node string) bool {
	if ancestor == node {
		return false
	}
	doms, ok := g.dominators[node]
	return ok && doms[ancestor]
}

// computeDepth walks the acyclic skeleton (declared back edges removed) in
// topological order and records the longest distance from the start node.
func (g *graph) computeDepth(def *Definition, c *collector) {
	back := map[Edge]bool{}
	for _, e := range g.backEdges {
		back[e] = true
	}
	indegree := map[string]int{}
	for _, id := range g.sortedNodeIDs() {
		if !g.reachable[id] {
			continue
		}
		indegree[id] = 0
	}
	for _, id := range g.sortedNodeIDs() {
		if !g.reachable[id] {
			continue
		}
		for _, e := range g.out[id] {
			if back[e] || !g.reachable[e.To] {
				continue
			}
			indegree[e.To]++
		}
	}
	queue := make([]string, 0, len(indegree))
	for _, id := range g.sortedNodeIDs() {
		if _, ok := indegree[id]; ok && indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		g.order = append(g.order, id)
		for _, e := range g.out[id] {
			if back[e] || !g.reachable[e.To] {
				continue
			}
			if g.depth[id]+1 > g.depth[e.To] {
				g.depth[e.To] = g.depth[id] + 1
			}
			indegree[e.To]--
			if indegree[e.To] == 0 {
				queue = append(queue, e.To)
			}
		}
	}
	if def.Limits.MaxDepth > 0 {
		for _, id := range g.order {
			if g.depth[id] > def.Limits.MaxDepth {
				c.add(CodeUnboundedFanout, Location{NodeID: id},
					"node depth %d exceeds declared max_depth %d", g.depth[id], def.Limits.MaxDepth)
			}
		}
	}
}

func (g *graph) checkLimits(def *Definition, c *collector) {
	root := Location{Ref: def.WorkflowID}
	if def.Limits.MaxFanOut == 0 {
		c.add(CodeUnboundedFanout, root, "limits.max_fan_out is required and must be greater than zero")
	}
	if def.Limits.MaxDepth == 0 {
		c.add(CodeUnboundedFanout, root, "limits.max_depth is required and must be greater than zero")
	}
	if def.Limits.MaxNodes == 0 {
		c.add(CodeUnboundedFanout, root, "limits.max_nodes is required and must be greater than zero")
	} else if uint32(len(def.Nodes)) > def.Limits.MaxNodes {
		c.add(CodeUnboundedFanout, root,
			"definition declares %d nodes, above the declared max_nodes %d", len(def.Nodes), def.Limits.MaxNodes)
	}
	if def.Limits.MaxFanOut == 0 {
		return
	}
	for _, id := range g.sortedNodeIDs() {
		if uint32(len(g.out[id])) > def.Limits.MaxFanOut {
			c.add(CodeUnboundedFanout, Location{NodeID: id},
				"node fans out to %d edges, above the declared max_fan_out %d",
				len(g.out[id]), def.Limits.MaxFanOut)
		}
	}
}
