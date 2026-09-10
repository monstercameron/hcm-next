// Package workflowexpansion compiles an archetype recipe plus a domain
// profile plus an intent delta into a complete high-level responsibility
// graph (WF-DISC-007): every recipe phase expands in order, deltas add,
// justify or replace responsibilities without ever dropping a mandatory
// governance, revalidation or reconciliation phase, external effects stay
// ordered on effect nodes, and repair branches always reach closure. The
// compiler fails closed: any fault produces exact findings and no partial
// graph. It is kernel-pure and separate from production workflow
// compilation, so exploratory designs cannot be published accidentally.
package workflowexpansion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

// Expansion finding codes.
const (
	MandatoryOmission    = "MANDATORY_OMISSION"
	MandatoryReplacement = "MANDATORY_REPLACEMENT"
	BareOmission         = "BARE_OMISSION"
	BareReplacement      = "BARE_REPLACEMENT"
	DuplicateOmit        = "DUPLICATE_OMIT"
	DanglingAdd          = "DANGLING_ADD"
	DanglingOmit         = "DANGLING_OMIT"
	DanglingReplace      = "DANGLING_REPLACE"
	EmptyPhase           = "EMPTY_PHASE"
	EmptyRecipe          = "EMPTY_RECIPE"
	ArchetypeMismatch    = "ARCHETYPE_MISMATCH"
	ProfileMismatch      = "PROFILE_MISMATCH"
	UnknownRecipe        = "UNKNOWN_RECIPE"
	UnknownDomainProfile = "UNKNOWN_DOMAIN_PROFILE"
	MissingRecord        = "MISSING_RECORD"
	UnorderableEffect    = "UNORDERABLE_EFFECT"
	ProhibitedAdd        = "PROHIBITED_ADD"
	UnreachableNode      = "UNREACHABLE_NODE"
	MisroutedRepair      = "MISROUTED_REPAIR"
)

// Node kinds.
const (
	KindGovernance  = "GOVERNANCE"
	KindObservation = "OBSERVATION"
	KindEffect      = "EFFECT"
	KindExecution   = "EXECUTION"
	KindCompletion  = "COMPLETION"
)

// NodeKind is one responsibility kind.
type NodeKind = string

// Responsibility origins.
const (
	OriginInherited = "INHERITED"
	OriginAdded     = "ADDED"
	OriginReplaced  = "REPLACED"
)

// Origin is one responsibility origin.
type Origin = string

// Edge kinds.
const (
	EdgeSequence = "SEQUENCE"
	EdgeRepair   = "REPAIR"
)

// EdgeKind is one graph edge kind.
type EdgeKind = string

var (
	governanceRe  = regexp.MustCompile(`(?i)\b(approv|authoriz|govern|consent|sign|certif|attest|permission|admit)`)
	observationRe = regexp.MustCompile(`(?i)\b(observ|reconcil|monitor|compar|review|inspect|detect|simulat|assess|evaluat|analy|measur|read|calculat|query|resolv|replay|redrive|quarantine)`)
	effectRe      = regexp.MustCompile(`(?i)\b(effect|mutat|creat|writ|publish|emit|dispatch|submit|provis|releas|migrat|execut|allocat|reserv|distribut|send|stor|persist|append|revok|suspend|delet|notif|charg|pay)`)
	readOnlyRe    = regexp.MustCompile(`(?i)\b(read|calculat|query|validat|deterministic|trace|label|return)`)
	// mandatoryRe marks responsibilities a delta can never drop or
	// replace: governance, revalidation, evidence, observation,
	// reconciliation and closure.
	mandatoryRe = regexp.MustCompile(`(?i)\b(revalidat|reconcil|approv|observ|repair|clos|govern|authoriz|evidence)`)
	// prohibitedAddRe marks added responsibilities that weaken
	// governance: skipping, bypassing or disabling approval,
	// authorization, audit, evidence or trace.
	prohibitedAddRe = regexp.MustCompile(`(?i)\b(skip|bypass|without (approval|authoriz|audit|evidence)|no (audit|approval|trace)|disable|ignore)`)
	reconcileRe     = regexp.MustCompile(`(?i)reconcil`)
)

// Recipe is one archetype's ordered phase sequence.
type Recipe struct {
	Archetype string
	Phases    []string
}

// Profile is one domain profile's default data, engines and closure.
type Profile struct {
	Code    string
	Reads   string
	Writes  string
	Closure string
}

// AddedPhase inserts a responsibility after a recipe sequence number.
type AddedPhase struct {
	AfterSeq int
	Phase    string
}

// OmittedPhase justifiably drops a recipe phase by sequence number.
type OmittedPhase struct {
	Seq    int
	Reason string
}

// ReplacedPhase substitutes a recipe phase by sequence number.
type ReplacedPhase struct {
	Seq    int
	With   string
	Reason string
}

// Delta is one intent's additions, omissions and replacements.
type Delta struct {
	Add     []AddedPhase
	Omit    []OmittedPhase
	Replace []ReplacedPhase
}

// Input is one complete expansion request.
type Input struct {
	Recipe  Recipe
	Profile Profile
	Record  workflowdesign.DesignRecord
	Delta   Delta
}

// Node is one ordered, typed responsibility.
type Node struct {
	Seq            int      `json:"seq"`
	Kind           NodeKind `json:"kind"`
	Responsibility string   `json:"responsibility"`
	Origin         Origin   `json:"origin"`
}

// Edge is one ordered graph edge.
type Edge struct {
	From int      `json:"from"`
	To   int      `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// Effect is one external effect ordered on an effect node.
type Effect struct {
	Order       int    `json:"order"`
	Node        int    `json:"node"`
	Description string `json:"description"`
}

// CompletionPolicy binds the terminal node to the record's completion.
type CompletionPolicy struct {
	Policy      string `json:"policy"`
	TerminalSeq int    `json:"terminal_seq"`
}

// Graph is one expanded high-level responsibility graph.
type Graph struct {
	Intent         string           `json:"intent"`
	Definition     string           `json:"definition,omitempty"`
	Archetype      string           `json:"archetype"`
	Profile        string           `json:"profile"`
	Disposition    string           `json:"disposition"`
	Nodes          []Node           `json:"nodes"`
	Edges          []Edge           `json:"edges"`
	Effects        []Effect         `json:"effects"`
	Invalidators   []string         `json:"invalidators"`
	Omitted        []OmittedPhase   `json:"omitted,omitempty"`
	Completion     CompletionPolicy `json:"completion"`
	Engines        []string         `json:"engines"`
	HumanWork      string           `json:"human_work"`
	ProfileReads   string           `json:"profile_reads"`
	ProfileClosure string           `json:"profile_closure"`
	Digest         string           `json:"digest"`
}

// Finding is one exact expansion diagnostic.
type Finding struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// MarshalGraph renders a graph with its findings as canonical JSON.
func MarshalGraph(graph *Graph, findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Code != ordered[j].Code {
			return ordered[i].Code < ordered[j].Code
		}
		return ordered[i].Detail < ordered[j].Detail
	})
	rendered, err := json.MarshalIndent(struct {
		Graph    *Graph    `json:"graph"`
		Findings []Finding `json:"findings,omitempty"`
	}{Graph: graph, Findings: ordered}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

// Expand compiles a recipe, profile, record and delta into a graph. Any
// fault returns exact findings with no partial graph.
func Expand(in Input) (*Graph, []Finding) {
	var findings []Finding
	add := func(code, field, detail string) {
		findings = append(findings, Finding{Code: code, Field: field, Detail: detail})
	}
	if len(in.Recipe.Phases) == 0 {
		add(EmptyRecipe, "recipe", "archetype "+in.Recipe.Archetype+" carries no phases to expand")
		return nil, findings
	}
	for i, phase := range in.Recipe.Phases {
		if strings.TrimSpace(phase) == "" {
			add(EmptyRecipe, "recipe", fmt.Sprintf("recipe phase %d is blank", i))
			return nil, findings
		}
	}
	if in.Recipe.Archetype != in.Record.Archetype {
		add(ArchetypeMismatch, "archetype", fmt.Sprintf("recipe %s does not expand record archetype %s", in.Recipe.Archetype, in.Record.Archetype))
		return nil, findings
	}
	if in.Profile.Code != in.Record.DomainProfile {
		add(ProfileMismatch, "domain_profile", fmt.Sprintf("profile %s does not cover record domain %s", in.Profile.Code, in.Record.DomainProfile))
		return nil, findings
	}
	omitted := make(map[int]OmittedPhase)
	for _, omit := range in.Delta.Omit {
		if omit.Seq < 0 || omit.Seq >= len(in.Recipe.Phases) {
			add(DanglingOmit, "delta.omit", fmt.Sprintf("omit seq %d references no recipe phase", omit.Seq))
			return nil, findings
		}
		if _, dup := omitted[omit.Seq]; dup {
			add(DuplicateOmit, "delta.omit", fmt.Sprintf("omit seq %d declared twice", omit.Seq))
			return nil, findings
		}
		phase := in.Recipe.Phases[omit.Seq]
		if mandatoryRe.MatchString(phase) {
			add(MandatoryOmission, "delta.omit", fmt.Sprintf("omit seq %d (%q) drops a mandatory responsibility", omit.Seq, phase))
			return nil, findings
		}
		if strings.TrimSpace(omit.Reason) == "" {
			add(BareOmission, "delta.omit", fmt.Sprintf("omit seq %d (%q) states no reason", omit.Seq, phase))
			return nil, findings
		}
		omitted[omit.Seq] = omit
	}
	replaced := make(map[int]ReplacedPhase)
	for _, replace := range in.Delta.Replace {
		if replace.Seq < 0 || replace.Seq >= len(in.Recipe.Phases) {
			add(DanglingReplace, "delta.replace", fmt.Sprintf("replace seq %d references no recipe phase", replace.Seq))
			return nil, findings
		}
		phase := in.Recipe.Phases[replace.Seq]
		if mandatoryRe.MatchString(phase) {
			add(MandatoryReplacement, "delta.replace", fmt.Sprintf("replace seq %d (%q) rewrites a mandatory responsibility", replace.Seq, phase))
			return nil, findings
		}
		if strings.TrimSpace(replace.With) == "" {
			add(EmptyPhase, "delta.replace", fmt.Sprintf("replace seq %d carries no replacement text", replace.Seq))
			return nil, findings
		}
		if strings.TrimSpace(replace.Reason) == "" {
			add(BareReplacement, "delta.replace", fmt.Sprintf("replace seq %d states no reason", replace.Seq))
			return nil, findings
		}
		replaced[replace.Seq] = replace
	}
	for _, addPhase := range in.Delta.Add {
		if addPhase.AfterSeq < 0 || addPhase.AfterSeq >= len(in.Recipe.Phases) {
			add(DanglingAdd, "delta.add", fmt.Sprintf("add after seq %d references no recipe phase", addPhase.AfterSeq))
			return nil, findings
		}
		if _, gone := omitted[addPhase.AfterSeq]; gone {
			add(DanglingAdd, "delta.add", fmt.Sprintf("add after seq %d anchors an omitted phase", addPhase.AfterSeq))
			return nil, findings
		}
		if strings.TrimSpace(addPhase.Phase) == "" {
			add(EmptyPhase, "delta.add", fmt.Sprintf("add after seq %d carries no responsibility text", addPhase.AfterSeq))
			return nil, findings
		}
		if prohibitedAddRe.MatchString(addPhase.Phase) {
			add(ProhibitedAdd, "delta.add", fmt.Sprintf("add after seq %d weakens governance: %q", addPhase.AfterSeq, addPhase.Phase))
			return nil, findings
		}
	}
	graph := &Graph{
		Intent:         in.Record.Intent,
		Definition:     in.Record.Definition,
		Archetype:      in.Recipe.Archetype,
		Profile:        in.Profile.Code,
		Disposition:    in.Record.Disposition,
		ProfileReads:   in.Profile.Reads,
		ProfileClosure: in.Profile.Closure,
		Completion:     CompletionPolicy{Policy: in.Record.Completion},
		HumanWork:      dimensionText(in.Record.HumanWork),
		Engines:        dimensionList(in.Record.Engines),
		Invalidators:   dimensionList(in.Record.Invalidators),
	}
	addsBySeq := make(map[int][]string)
	for _, addPhase := range in.Delta.Add {
		addsBySeq[addPhase.AfterSeq] = append(addsBySeq[addPhase.AfterSeq], addPhase.Phase)
	}
	for seq, phase := range in.Recipe.Phases {
		if omit, gone := omitted[seq]; gone {
			graph.Omitted = append(graph.Omitted, OmittedPhase{Seq: seq, Reason: omit.Reason})
			continue
		}
		responsibility, origin := phase, OriginInherited
		if replace, ok := replaced[seq]; ok {
			responsibility, origin = replace.With, OriginReplaced
		}
		graph.Nodes = append(graph.Nodes, Node{Seq: len(graph.Nodes), Kind: classify(responsibility), Responsibility: responsibility, Origin: origin})
		for _, added := range addsBySeq[seq] {
			graph.Nodes = append(graph.Nodes, Node{Seq: len(graph.Nodes), Kind: classify(added), Responsibility: added, Origin: OriginAdded})
		}
	}
	if len(graph.Nodes) > 0 {
		graph.Nodes[len(graph.Nodes)-1].Kind = KindCompletion
	}
	graph.Completion.TerminalSeq = len(graph.Nodes) - 1
	for i := 1; i < len(graph.Nodes); i++ {
		graph.Edges = append(graph.Edges, Edge{From: graph.Nodes[i-1].Seq, To: graph.Nodes[i].Seq, Kind: EdgeSequence})
	}
	var effectNodes []int
	for _, node := range graph.Nodes {
		if node.Kind == KindEffect {
			effectNodes = append(effectNodes, node.Seq)
		}
	}
	descriptions := dimensionList(in.Record.Writes)
	if len(descriptions) > 0 && len(effectNodes) == 0 {
		add(UnorderableEffect, "writes", "record declares external effects but the expanded graph holds no effect node; DIRECT dispositions must stay effect-free")
		return nil, findings
	}
	for i, description := range descriptions {
		graph.Effects = append(graph.Effects, Effect{Order: i, Node: effectNodes[i%len(effectNodes)], Description: description})
	}
	for _, node := range graph.Nodes {
		if node.Kind == KindEffect || (node.Kind == KindObservation && reconcileRe.MatchString(node.Responsibility)) {
			graph.Edges = append(graph.Edges, Edge{From: node.Seq, To: graph.Completion.TerminalSeq, Kind: EdgeRepair})
		}
	}
	graph.Digest = digestGraph(graph)
	if validate := graph.Validate(); len(validate) > 0 {
		return nil, append(findings, validate...)
	}
	return graph, findings
}

// classify kinds one responsibility by reviewed content patterns. The
// last node is forced to COMPLETION by the compiler, never by content.
func classify(responsibility string) NodeKind {
	lowered := strings.ToLower(responsibility)
	switch {
	case governanceRe.MatchString(lowered):
		return KindGovernance
	case observationRe.MatchString(lowered):
		return KindObservation
	case effectRe.MatchString(lowered) && !readOnlyRe.MatchString(lowered):
		return KindEffect
	default:
		return KindExecution
	}
}

func dimensionText(dimension workflowdesign.Dimension) string {
	if dimension.Value != "" {
		if dimension.Reason != "" {
			return dimension.Value + " (" + dimension.Reason + ")"
		}
		return dimension.Value
	}
	return strings.Join(dimension.Items, ", ")
}

func dimensionList(dimension workflowdesign.Dimension) []string {
	if dimension.Value == workflowdesign.NotApplicable || strings.TrimSpace(dimension.Value) == "" {
		out := append([]string(nil), dimension.Items...)
		sort.Strings(out)
		return out
	}
	return []string{dimension.Value}
}

// Validate checks graph reachability: every node reachable from the
// start, completion reachable from every node, and every repair edge
// routed to the completion node.
func (g *Graph) Validate() []Finding {
	var findings []Finding
	if len(g.Nodes) == 0 {
		return []Finding{{Code: EmptyRecipe, Detail: "graph holds no nodes"}}
	}
	forward := make(map[int][]int)
	for _, edge := range g.Edges {
		forward[edge.From] = append(forward[edge.From], edge.To)
	}
	reached := map[int]bool{g.Nodes[0].Seq: true}
	queue := []int{g.Nodes[0].Seq}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range forward[current] {
			if !reached[next] {
				reached[next] = true
				queue = append(queue, next)
			}
		}
	}
	terminal := g.Completion.TerminalSeq
	for _, node := range g.Nodes {
		if !reached[node.Seq] {
			findings = append(findings, Finding{Code: UnreachableNode, Detail: fmt.Sprintf("node %d (%q) unreachable from the start", node.Seq, node.Responsibility)})
		}
	}
	for _, edge := range g.Edges {
		if edge.Kind == EdgeRepair && edge.To != terminal {
			findings = append(findings, Finding{Code: MisroutedRepair, Detail: fmt.Sprintf("repair edge %d->%d misses the completion node %d", edge.From, edge.To, terminal)})
		}
	}
	reverse := make(map[int][]int)
	for _, edge := range g.Edges {
		reverse[edge.To] = append(reverse[edge.To], edge.From)
	}
	canFinish := map[int]bool{terminal: true}
	queue = []int{terminal}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, prev := range reverse[current] {
			if !canFinish[prev] {
				canFinish[prev] = true
				queue = append(queue, prev)
			}
		}
	}
	for _, node := range g.Nodes {
		if !canFinish[node.Seq] {
			findings = append(findings, Finding{Code: UnreachableNode, Detail: fmt.Sprintf("node %d (%q) cannot reach completion", node.Seq, node.Responsibility)})
		}
	}
	return findings
}

func digestGraph(graph *Graph) string {
	var lines []string
	for _, node := range graph.Nodes {
		lines = append(lines, strings.Join([]string{node.Kind, node.Responsibility, node.Origin}, "\x00"))
	}
	for _, edge := range graph.Edges {
		lines = append(lines, fmt.Sprintf("%d\x00%d\x00%s", edge.From, edge.To, edge.Kind))
	}
	for _, effect := range graph.Effects {
		lines = append(lines, fmt.Sprintf("%d\x00%d\x00%s", effect.Order, effect.Node, effect.Description))
	}
	lines = append(lines, strings.Join(graph.Invalidators, ","))
	for _, omitted := range graph.Omitted {
		lines = append(lines, fmt.Sprintf("%d\x00%s", omitted.Seq, omitted.Reason))
	}
	lines = append(lines, graph.Completion.Policy, strings.Join(graph.Engines, ","), graph.HumanWork, graph.Profile, graph.Archetype, graph.Disposition)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
