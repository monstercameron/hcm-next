// Package parse turns one reference workflow markdown document into the
// typed model.Document/model.Workflow shape. It never executes anything;
// it only extracts structure that already exists in the document's prose,
// ASCII diagrams, and lists.
//
// Two document shapes are recognized:
//
//   - A standalone workflow document (manager-change.md,
//     promote-into-management.md): one level-1 title, level-2 sections,
//     fenced diagrams, and a "## Required conformance scenarios" numbered
//     list. Parsed at full depth (model.KindDocument).
//
//   - The suite index (reference-suite.md): a level-1 title followed by
//     "#### Reference Workflow N: ..." and "#### Sixth Infrastructure
//     Stress Test: ..." subsections, each describing one workflow at
//     narrative depth with a "This reference workflow must prove:" bullet
//     list instead of a numbered scenario section. Parsed at reduced depth
//     (model.KindSuiteSection) plus the suite's foundational-questions list
//     and legacy-fixture table.
//
// A document is classified as the suite index purely by the presence of a
// "Reference Workflow N:" heading, not by file name, so the classification
// is data-driven rather than path-coupled.
package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

var (
	headingRegex                = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	fenceRegex                  = regexp.MustCompile("^```")
	scenarioLineRegex           = regexp.MustCompile(`^(\d+)\.\s+(.+?)\s*$`)
	bulletLineRegex             = regexp.MustCompile(`^-\s+(.+?)\s*$`)
	capabilityRegex             = regexp.MustCompile(`\b[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*){1,4}\b`)
	actorRegex                  = regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*\([^()\n]*\)`)
	cardinalityRegex            = regexp.MustCompile(`\b(ALL_OF|ANY_OF|ONE_OF|QUORUM)\b`)
	completionKeyRegex          = regexp.MustCompile(`^(RequestState|ExecutionState|BusinessState|ConsistencyState|ObligationState)\s+(\S.*?)\s*$`)
	connectorTokenRegex         = regexp.MustCompile(`^[|vV<>+=.:│┌└├┤─▼▲↑↓^-]+$`)
	revalidationHeadingRegex    = regexp.MustCompile(`(?i)revalidat`)
	slugNonAlnumRegex           = regexp.MustCompile(`[^a-z0-9]+`)
	suiteWorkflowHeadingRegex   = regexp.MustCompile(`^Reference Workflow \d+:`)
	suiteStressTestHeadingRegex = regexp.MustCompile(`^Sixth Infrastructure Stress Test:`)
	mustProveMarkerRegex        = regexp.MustCompile(`(?i)this reference workflow must prove:`)
)

// ParseFile reads path from disk and parses it, tagging the result with
// relPath (the path the caller wants recorded in the model, typically
// relative to the repository root).
func ParseFile(path, relPath string, v *vocab.Vocabulary) (*model.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse: read %s: %w", path, err)
	}
	return Parse(relPath, data, v)
}

// Parse builds a model.Document from raw markdown bytes.
func Parse(relPath string, data []byte, v *vocab.Vocabulary) (*model.Document, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("parse %s: document is empty", relPath)
	}
	lines := strings.Split(text, "\n")

	titleMatch := headingRegex.FindStringSubmatch(lines[0])
	if titleMatch == nil || len(titleMatch[1]) != 1 {
		return nil, fmt.Errorf("parse %s: first line is not a level-1 (# ) title heading", relPath)
	}
	title := titleMatch[2]

	sections := splitSections(lines)
	doc := &model.Document{SourcePath: relPath, Title: title}

	if containsSuiteWorkflowHeading(sections) {
		doc.Workflows = parseSuiteSections(relPath, sections, v)
		doc.FoundationalQuestions = extractFoundationalQuestions(sections)
		doc.LegacyFixtures = extractLegacyFixtures(sections)
		if len(doc.Workflows) == 0 {
			return nil, fmt.Errorf("parse %s: suite heading pattern present but no workflow subsections extracted", relPath)
		}
		return doc, nil
	}

	doc.Workflows = []*model.Workflow{parseStandaloneWorkflow(relPath, title, text, sections, v)}
	return doc, nil
}

// rawSection is the intermediate, pre-model representation of one heading
// and the lines beneath it, up to (not including) the next heading of any
// level.
type rawSection struct {
	Level   int
	Heading string
	Lines   []string
}

func splitSections(lines []string) []rawSection {
	var sections []rawSection
	var cur *rawSection
	for _, line := range lines {
		if m := headingRegex.FindStringSubmatch(line); m != nil {
			if cur != nil {
				sections = append(sections, *cur)
			}
			cur = &rawSection{Level: len(m[1]), Heading: m[2]}
			continue
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, line)
		}
	}
	if cur != nil {
		sections = append(sections, *cur)
	}
	return sections
}

func containsSuiteWorkflowHeading(sections []rawSection) bool {
	for _, s := range sections {
		if suiteWorkflowHeadingRegex.MatchString(s.Heading) {
			return true
		}
	}
	return false
}

func parseStandaloneWorkflow(relPath, title, fullText string, sections []rawSection, v *vocab.Vocabulary) *model.Workflow {
	base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	wf := &model.Workflow{
		ID:         base,
		SourcePath: relPath,
		Kind:       model.KindDocument,
		Title:      title,
		RawText:    fullText,
	}

	var allDiagrams []model.Diagram
	for _, s := range sections {
		wf.Sections = append(wf.Sections, model.Section{Heading: s.Heading, Level: s.Level, Body: strings.Join(s.Lines, "\n")})
		ds := extractDiagrams(s.Heading, s.Lines)
		allDiagrams = append(allDiagrams, ds...)
		if strings.EqualFold(strings.TrimSpace(s.Heading), "Required conformance scenarios") {
			wf.Scenarios = extractScenarios(s.Lines)
		}
	}
	wf.Diagrams = allDiagrams
	wf.Steps = flattenSteps(allDiagrams)
	wf.Capabilities, wf.Actors, wf.Cardinalities = collectFromDiagrams(allDiagrams)
	wf.PrimitiveHits = extractPrimitiveHits(fullText, v)
	wf.Preconditions = extractPreconditions(sections)
	wf.ExpectedOutcomes = extractExpectedOutcomes(allDiagrams)

	return wf
}

func parseSuiteSections(relPath string, sections []rawSection, v *vocab.Vocabulary) []*model.Workflow {
	var workflows []*model.Workflow
	for _, s := range sections {
		if !suiteWorkflowHeadingRegex.MatchString(s.Heading) && !suiteStressTestHeadingRegex.MatchString(s.Heading) {
			continue
		}
		sectionText := s.Heading + "\n" + strings.Join(s.Lines, "\n")
		diagrams := extractDiagrams(s.Heading, s.Lines)

		wf := &model.Workflow{
			ID:         "suite-" + slug(s.Heading),
			SourcePath: relPath,
			Kind:       model.KindSuiteSection,
			Title:      s.Heading,
			RawText:    sectionText,
			Diagrams:   diagrams,
		}
		wf.Steps = flattenSteps(diagrams)
		wf.Capabilities, wf.Actors, wf.Cardinalities = collectFromDiagrams(diagrams)
		wf.PrimitiveHits = extractPrimitiveHits(sectionText, v)
		wf.ExpectedOutcomes = extractExpectedOutcomes(diagrams)
		wf.Scenarios = extractBulletScenariosAfterMarker(s.Lines, mustProveMarkerRegex)

		workflows = append(workflows, wf)
	}
	sort.Slice(workflows, func(i, j int) bool { return workflows[i].ID < workflows[j].ID })
	return workflows
}

func flattenSteps(diagrams []model.Diagram) []model.Step {
	var steps []model.Step
	order := 0
	for _, d := range diagrams {
		for _, s := range d.Steps {
			steps = append(steps, model.Step{Text: s, SectionHeading: d.SectionHeading, Order: order})
			order++
		}
	}
	return steps
}

func extractDiagrams(sectionHeading string, lines []string) []model.Diagram {
	var diagrams []model.Diagram
	inFence := false
	var cur []string
	for _, line := range lines {
		if fenceRegex.MatchString(strings.TrimSpace(line)) {
			if inFence {
				diagrams = append(diagrams, buildDiagram(sectionHeading, cur))
				cur = nil
				inFence = false
			} else {
				inFence = true
			}
			continue
		}
		if inFence {
			cur = append(cur, line)
		}
	}
	// An unterminated fence (malformed document) still yields whatever
	// content it collected rather than being silently dropped.
	if inFence && len(cur) > 0 {
		diagrams = append(diagrams, buildDiagram(sectionHeading, cur))
	}
	return diagrams
}

func buildDiagram(sectionHeading string, lines []string) model.Diagram {
	d := model.Diagram{SectionHeading: sectionHeading, Lines: append([]string(nil), lines...)}
	for _, l := range lines {
		if isContentLine(l) {
			d.Steps = append(d.Steps, strings.TrimSpace(l))
		}
	}
	return d
}

// isContentLine reports whether line, tokenized on whitespace, has at
// least one token that is not composed entirely of ASCII/box-drawing
// connector characters. It is token-based (not character-based) so that a
// legitimate word containing a connector letter, such as "validate", is
// never mistaken for arrow-art.
func isContentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, tok := range strings.Fields(trimmed) {
		if !connectorTokenRegex.MatchString(tok) {
			return true
		}
	}
	return false
}

func collectFromDiagrams(diagrams []model.Diagram) (capabilities, actors, cardinalities []string) {
	capSet := map[string]bool{}
	actorSet := map[string]bool{}
	cardSet := map[string]bool{}
	for _, d := range diagrams {
		for _, line := range d.Lines {
			for _, m := range capabilityRegex.FindAllString(line, -1) {
				capSet[m] = true
			}
			for _, m := range actorRegex.FindAllString(line, -1) {
				actorSet[m] = true
			}
			for _, m := range cardinalityRegex.FindAllString(line, -1) {
				cardSet[m] = true
			}
		}
	}
	return setToSortedSlice(capSet), setToSortedSlice(actorSet), setToSortedSlice(cardSet)
}

func setToSortedSlice(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func extractPrimitiveHits(rawText string, v *vocab.Vocabulary) []model.PrimitiveHit {
	names := append(append([]string{}, v.AllPrimitiveNames()...), v.RetiredNames()...)
	if len(names) == 0 {
		return nil
	}
	pattern := `\b(` + strings.Join(names, "|") + `)\b`
	re := regexp.MustCompile(pattern)

	var hits []model.PrimitiveHit
	for i, line := range strings.Split(rawText, "\n") {
		for _, m := range re.FindAllString(line, -1) {
			hits = append(hits, model.PrimitiveHit{Primitive: m, Context: strings.TrimSpace(line), Line: i + 1})
		}
	}
	return hits
}

func extractScenarios(lines []string) []model.Scenario {
	var out []model.Scenario
	for _, line := range lines {
		m := scenarioLineRegex.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		out = append(out, model.Scenario{Index: idx, Description: m[2]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// extractBulletScenariosAfterMarker returns the "- " bullet items found
// immediately after the first line matching marker, stopping at the first
// blank line once collection has started. If no bullet list follows the
// marker (the sixth stress-test section states its scenarios as one prose
// sentence rather than a list), it returns nil rather than guessing at a
// comma-split.
func extractBulletScenariosAfterMarker(lines []string, marker *regexp.Regexp) []model.Scenario {
	var out []model.Scenario
	collecting := false
	idx := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !collecting {
			if marker.MatchString(t) {
				collecting = true
			}
			continue
		}
		if t == "" {
			if idx > 0 {
				break
			}
			continue
		}
		if m := bulletLineRegex.FindStringSubmatch(t); m != nil {
			idx++
			out = append(out, model.Scenario{Index: idx, Description: m[1]})
			continue
		}
		if idx > 0 {
			break
		}
	}
	return out
}

func extractPreconditions(sections []rawSection) []string {
	var out []string
	for _, s := range sections {
		if !revalidationHeadingRegex.MatchString(s.Heading) {
			continue
		}
		for _, d := range extractDiagrams(s.Heading, s.Lines) {
			out = append(out, d.Steps...)
		}
	}
	return out
}

func extractExpectedOutcomes(diagrams []model.Diagram) []model.KeyValue {
	for _, d := range diagrams {
		var kvs []model.KeyValue
		count := 0
		for _, line := range d.Lines {
			m := completionKeyRegex.FindStringSubmatch(strings.TrimSpace(line))
			if m == nil {
				continue
			}
			kvs = append(kvs, model.KeyValue{Key: m[1], Value: m[2]})
			count++
		}
		if count >= 3 {
			return kvs
		}
	}
	return nil
}

func extractFoundationalQuestions(sections []rawSection) []model.Scenario {
	for _, s := range sections {
		if strings.EqualFold(strings.TrimSpace(s.Heading), "Foundational Questions the Suite Must Close") {
			return extractScenarios(s.Lines)
		}
	}
	return nil
}

func extractLegacyFixtures(sections []rawSection) []model.LegacyFixture {
	for _, s := range sections {
		if !strings.EqualFold(strings.TrimSpace(s.Heading), "Preserved Legacy Conformance Fixtures") {
			continue
		}
		return parseTwoColumnTable(s.Lines)
	}
	return nil
}

func parseTwoColumnTable(lines []string) []model.LegacyFixture {
	var out []model.LegacyFixture
	headerSeen := false
	sepSeen := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			if headerSeen && sepSeen {
				break
			}
			continue
		}
		cells := splitTableRow(t)
		if !headerSeen {
			headerSeen = true
			continue
		}
		if !sepSeen {
			sepSeen = true
			continue
		}
		if len(cells) < 2 {
			continue
		}
		out = append(out, model.LegacyFixture{Name: cells[0], ContractPressure: cells[1]})
	}
	return out
}

func splitTableRow(line string) []string {
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func slug(s string) string {
	s = strings.ToLower(s)
	s = slugNonAlnumRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
