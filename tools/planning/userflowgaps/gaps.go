// Package userflowgaps compiles unresolved user-flow obligations into atomic,
// deterministic TDD backlog candidates.  The package deliberately has no
// knowledge of screens or routes: identity is the semantic owner plus its
// contract, while flow and stage references are reverse coverage edges.
package userflowgaps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Finding is one unresolved obligation extracted from a flow stage.
type Finding struct {
	FlowID, Stage, Configuration string
	Owner, Contract, Kind        string
	Description                  string
	Phase, Intelligence          string
	Dependencies, References     []string
	Oracle                       Oracle
}

// Oracle records exact observable assertions required to close a gap.
type Oracle struct {
	Browser, API, Security, Fault, Conformance, Mutation string
	VisibleState, EnabledActions, SemanticResult         string
	PersistedEffects, ProhibitedDisclosure               string
}

// Todo is an atomic candidate. Key is stable across runs and independent of
// flow wording; Coverage contains every consuming flow/stage/configuration.
type Todo struct {
	Key, ID, Owner, Contract, Kind string
	Description                    string
	Phase, Intelligence            string
	Dependencies, References       []string
	Oracle                         Oracle
	Coverage                       []CoverageEdge
}

// CoverageEdge is the reverse edge from a shared todo to a consuming flow.
type CoverageEdge struct{ FlowID, Stage, Configuration string }

// Input is the compiler input. Existing todos are treated as authoritative
// identities and receive newly discovered reverse edges.
type Input struct {
	Findings []Finding
	Existing []Todo
}

// Result is deterministic and suitable for digesting or serialising.
type Result struct {
	Todos         []Todo
	Reverse       map[string][]CoverageEdge
	Digest        string
	NewIdentities []string
}

// Candidates is a compatibility alias for the generated atomic todos.
func (r Result) Candidates() []Todo { return r.Todos }

// Compiler is a reusable facade for callers that run multiple passes.
type Compiler struct{ Existing []Todo }

func (c Compiler) Compile(findings []Finding) Result {
	return CompileFindings(Input{Findings: findings, Existing: c.Existing})
}

// CompileFindings compiles findings to a fixed point. Existing entries are
// retained and enriched, so feeding Result.Todos into a second invocation
// produces no NewIdentities and the same Digest.
func CompileFindings(in Input) Result {
	byKey := make(map[string]Todo, len(in.Existing))
	for _, t := range in.Existing {
		normalizeTodo(&t)
		byKey[t.Key] = t
	}
	newIDs := []string{}
	for _, f := range in.Findings {
		key := Identity(f.Owner, f.Contract)
		t, ok := byKey[key]
		if !ok {
			t = Todo{Key: key, ID: key, Owner: clean(f.Owner), Contract: clean(f.Contract), Kind: clean(f.Kind), Description: clean(f.Description), Phase: clean(f.Phase), Intelligence: clean(f.Intelligence), Oracle: f.Oracle}
			newIDs = append(newIDs, key)
		}
		mergeFinding(&t, f)
		byKey[key] = t
	}
	all := make([]Todo, 0, len(byKey))
	for _, t := range byKey {
		normalizeTodo(&t)
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	reverse := make(map[string][]CoverageEdge, len(all))
	for _, t := range all {
		reverse[t.Key] = append([]CoverageEdge(nil), t.Coverage...)
	}
	for k := range reverse {
		sortEdges(reverse[k])
	}
	return Result{Todos: all, Reverse: reverse, Digest: digest(all), NewIdentities: uniqueSorted(newIDs)}
}

// Compile is the concise entry point used by callers with no pre-existing
// backlog. Use CompileFindings to seed an existing registry.
func Compile(input any) Result {
	switch v := input.(type) {
	case []Finding:
		return CompileFindings(Input{Findings: v})
	case Input:
		return CompileFindings(v)
	case nil:
		return CompileFindings(Input{})
	default:
		return CompileFindings(Input{})
	}
}

// Identity returns the canonical owner+contract identity.
func Identity(owner, contract string) string { return clean(owner) + "::" + clean(contract) }

func mergeFinding(t *Todo, f Finding) {
	if t.Kind == "" {
		t.Kind = clean(f.Kind)
	}
	if t.Description == "" {
		t.Description = clean(f.Description)
	}
	if t.Phase == "" {
		t.Phase = clean(f.Phase)
	}
	if t.Intelligence == "" {
		t.Intelligence = clean(f.Intelligence)
	}
	t.Dependencies = append(t.Dependencies, f.Dependencies...)
	t.References = append(t.References, f.References...)
	if t.Oracle == (Oracle{}) {
		t.Oracle = f.Oracle
	}
	e := CoverageEdge{clean(f.FlowID), clean(f.Stage), clean(f.Configuration)}
	for _, x := range t.Coverage {
		if x == e {
			return
		}
	}
	t.Coverage = append(t.Coverage, e)
}
func normalizeTodo(t *Todo) {
	t.Key = Identity(t.Owner, t.Contract)
	if t.ID == "" {
		t.ID = t.Key
	}
	t.Dependencies = uniqueSorted(t.Dependencies)
	t.References = uniqueSorted(t.References)
	sortEdges(t.Coverage)
}
func sortEdges(x []CoverageEdge) {
	sort.Slice(x, func(i, j int) bool {
		a, b := x[i], x[j]
		if a.FlowID != b.FlowID {
			return a.FlowID < b.FlowID
		}
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		return a.Configuration < b.Configuration
	})
}
func clean(s string) string { return strings.TrimSpace(s) }
func uniqueSorted(x []string) []string {
	m := map[string]bool{}
	for _, v := range x {
		if clean(v) != "" {
			m[clean(v)] = true
		}
	}
	r := make([]string, 0, len(m))
	for v := range m {
		r = append(r, v)
	}
	sort.Strings(r)
	return r
}
func digest(x []Todo) string {
	b, _ := json.Marshal(x)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
