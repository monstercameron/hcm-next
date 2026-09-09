package todogovernance

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// phaseRank orders the phases that participate in real delivery sequencing.
// A todo in an earlier phase may not depend on a todo in a strictly later
// phase, because that dependency could not yet have been satisfied when the
// earlier-phase todo executes. CONFORMANCE, DESIGN, RETIRED and OUT are
// deliberately excluded: they are not part of the linear delivery sequence
// (RETIRED resolves as satisfied per the backlog rules; CONFORMANCE/DESIGN
// are fixture-or-contract-only and carry no execution order; OUT is out of
// scope entirely), so an edge touching one of them is never a phase
// inversion under this rule.
var phaseRank = map[string]int{
	"P0":      0,
	"GATE_A":  1,
	"GATE_B":  2,
	"GATE_C":  3,
	"PHASE_2": 4,
	"PHASE_3": 5,
	"PHASE_4": 6,
	"PHASE_5": 7,
}

const (
	CodeUnresolvedDependency = "UNRESOLVED_DEPENDENCY"
	CodeProseDependency      = "PROSE_DEPENDENCY"
	CodeDependencyCycle      = "DEPENDENCY_CYCLE"
	CodePhaseInversion       = "PHASE_INVERSION"
)

// nonIDConnector matches the punctuation/word tokens that are allowed to sit
// between backtick-wrapped dependency IDs without counting as prose: list
// separators (",", ";"), range connectors ("-", "–", "—", "to"),
// and whitespace.
func isConnectorWord(w string) bool {
	switch strings.ToLower(w) {
	case "", "-", "–", "—", "to", "none":
		return true
	default:
		return false
	}
}

// ValidateDependencies runs the GOV-016 checks against the given todos
// (typically loaded from the compiled registry) plus the raw per-todo
// Depends text (from the markdown, needed for prose detection since the
// registry only ever contains successfully extracted IDs). It reports every
// unresolved dependency edge, every dependency field containing prose
// outside its backtick-wrapped ID tokens, every dependency cycle, and every
// direct edge that points from an earlier declared phase to a later one.
func ValidateDependencies(todos []todoregistry.Todo, raw map[string]string) []Finding {
	var findings []Finding

	byID := make(map[string]todoregistry.Todo, len(todos))
	for _, t := range todos {
		byID[t.ID] = t
	}

	// Unresolved dependencies: RETIRED todos remain in the registry with
	// their own ID, so a dependency on one resolves as satisfied per the
	// backlog rules without any special-casing here.
	for _, t := range todos {
		for _, dep := range t.Depends {
			if _, ok := byID[dep]; !ok {
				findings = append(findings, Finding{
					TodoID: t.ID,
					Rule:   "GOV-016",
					Code:   CodeUnresolvedDependency,
					Line:   t.Line,
					Detail: dep,
					Reason: fmt.Sprintf("depends on %q, which is not a known todo ID", dep),
				})
			}
		}
	}

	// Prose dependencies: strip every backtick-wrapped token from the raw
	// field text; anything left over that is not a list/range connector or
	// "none" is prose.
	for id, raw := range raw {
		if leftover := stripDependencyTokens(raw); leftover != "" {
			t := byID[id]
			findings = append(findings, Finding{
				TodoID: t.ID,
				Rule:   "GOV-016",
				Code:   CodeProseDependency,
				Line:   t.Line,
				Reason: fmt.Sprintf("Depends field contains prose outside its backtick-wrapped IDs: %q", leftover),
			})
		}
	}

	// Phase inversion: a direct edge whose target's declared phase ranks
	// strictly later than the source's. Only edges between two ranked
	// (orderable) phases are considered.
	for _, t := range todos {
		fromRank, fromOK := phaseRank[t.Phase]
		if !fromOK {
			continue
		}
		for _, dep := range t.Depends {
			d, ok := byID[dep]
			if !ok {
				continue // already reported as unresolved
			}
			toRank, toOK := phaseRank[d.Phase]
			if !toOK {
				continue
			}
			if toRank > fromRank {
				findings = append(findings, Finding{
					TodoID: t.ID,
					Rule:   "GOV-016",
					Code:   CodePhaseInversion,
					Line:   t.Line,
					Detail: dep,
					Reason: fmt.Sprintf("%s todo depends on %s todo %s (later phase)", t.Phase, d.Phase, dep),
				})
			}
		}
	}

	findings = append(findings, findCycles(byID)...)

	sortFindings(findings)
	return findings
}

// stripDependencyTokens removes every backtick-wrapped token from raw,
// along with any run of connector punctuation/words between or around them,
// and returns whatever text remains (trimmed). A non-empty result means the
// field contained prose.
func stripDependencyTokens(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "none") || raw == "" {
		return ""
	}

	var b strings.Builder
	i := 0
	for i < len(raw) {
		if raw[i] == '`' {
			end := strings.IndexByte(raw[i+1:], '`')
			if end == -1 {
				// Unterminated backtick: treat the rest as prose.
				b.WriteString(raw[i:])
				break
			}
			i = i + 1 + end + 1
			continue
		}
		b.WriteByte(raw[i])
		i++
	}

	leftover := b.String()
	// Now leftover is the original text with every `...` token removed.
	// Split on remaining punctuation/whitespace and keep anything that is
	// not a recognized connector.
	fields := strings.FieldsFunc(leftover, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '.'
	})
	var prose []string
	for _, f := range fields {
		if !isConnectorWord(f) {
			prose = append(prose, f)
		}
	}
	return strings.Join(prose, " ")
}

// findCycles reports one finding per distinct dependency cycle, keyed at
// its lexicographically smallest rotation so re-running the check is
// deterministic regardless of traversal order.
func findCycles(byID map[string]todoregistry.Todo) []Finding {
	adjacency := make(map[string][]string, len(byID))
	ids := make([]string, 0, len(byID))
	for id, t := range byID {
		ids = append(ids, id)
		for _, dep := range t.Depends {
			if _, ok := byID[dep]; ok {
				adjacency[id] = append(adjacency[id], dep)
			}
		}
	}
	sort.Strings(ids)
	for id := range adjacency {
		sort.Strings(adjacency[id])
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]uint8, len(ids))
	pos := make(map[string]int, len(ids))
	var stack []string
	seen := make(map[string]bool)
	var findings []Finding

	var visit func(string)
	visit = func(n string) {
		color[n] = gray
		pos[n] = len(stack)
		stack = append(stack, n)
		for _, dep := range adjacency[n] {
			switch color[dep] {
			case white:
				visit(dep)
			case gray:
				cycle := append([]string(nil), stack[pos[dep]:]...)
				cycle = append(cycle, dep)
				key := canonicalCycle(cycle)
				if !seen[key] {
					seen[key] = true
					head := byID[cycle[0]]
					findings = append(findings, Finding{
						TodoID: head.ID,
						Rule:   "GOV-016",
						Code:   CodeDependencyCycle,
						Line:   head.Line,
						Detail: key,
						Reason: fmt.Sprintf("dependency cycle: %s", strings.Join(cycle, " -> ")),
					})
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(pos, n)
		color[n] = black
	}

	for _, id := range ids {
		if color[id] == white {
			visit(id)
		}
	}
	return findings
}

// canonicalCycle rotates cycle (a closed walk, first==last) to start at its
// lexicographically smallest node so the same cycle discovered from
// different starting points produces the same key.
func canonicalCycle(cycle []string) string {
	if len(cycle) <= 1 {
		return strings.Join(cycle, " -> ")
	}
	nodes := cycle[:len(cycle)-1]
	best := ""
	for i := range nodes {
		rotated := append(append([]string{}, nodes[i:]...), nodes[:i]...)
		rotated = append(rotated, rotated[0])
		candidate := strings.Join(rotated, " -> ")
		if best == "" || candidate < best {
			best = candidate
		}
	}
	return best
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].TodoID != findings[j].TodoID {
			return findings[i].TodoID < findings[j].TodoID
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
}
