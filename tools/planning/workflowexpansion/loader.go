// Live loading for the design expansion compiler: archetype recipes
// parsed from the reviewed archetype document, domain profiles parsed
// from the registry's profile table, accepted definitions shared by
// import with tools/planning/intentcoverage/GOV-026, and design records
// validated by tools/planning/workflowdesign/WF-DISC-005. Both parsers
// fail closed on shape drift so a dropped phase line or a ragged table
// row can never silently shrink an expansion.
package workflowexpansion

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

var (
	recipeHeaderRe = regexp.MustCompile(`^## ([A-Z][0-9]+)\b`)
	codeFenceRe    = regexp.MustCompile("^```")
)

// Snapshot is the complete live input the expansion conformance compiles.
type Snapshot struct {
	Accepted []string
	Records  []workflowdesign.DesignRecord
	Recipes  map[string][]string
	Profiles map[string]Profile
}

// ParseRecipes parses archetype phase recipes from an archetype document:
// every `## <ID>` section carries exactly one text block whose `->`
// separated lines are the ordered phases.
func ParseRecipes(markdown string) (map[string][]string, error) {
	recipes := make(map[string][]string)
	var current string
	var block []string
	inBlock := false
	flush := func(line int) error {
		if current == "" {
			return nil
		}
		if len(block) == 0 {
			return fmt.Errorf("archetype %s has no phase block before line %d", current, line)
		}
		var phases []string
		for _, chunk := range strings.Split(strings.Join(block, "\n"), "->") {
			if trimmed := strings.TrimSpace(chunk); trimmed != "" {
				phases = append(phases, trimmed)
			}
		}
		if len(phases) == 0 {
			return fmt.Errorf("archetype %s phase block holds no phases before line %d", current, line)
		}
		recipes[current] = phases
		block = nil
		return nil
	}
	for lineNumber, line := range strings.Split(markdown, "\n") {
		lineNumber++
		if match := recipeHeaderRe.FindStringSubmatch(line); match != nil {
			if err := flush(lineNumber); err != nil {
				return nil, err
			}
			current = match[1]
			inBlock = false
			continue
		}
		if current == "" {
			continue
		}
		if codeFenceRe.MatchString(line) {
			if inBlock {
				inBlock = false
				continue
			}
			inBlock = true
			continue
		}
		if inBlock {
			block = append(block, line)
		}
	}
	if err := flush(len(strings.Split(markdown, "\n")) + 1); err != nil {
		return nil, err
	}
	if len(recipes) == 0 {
		return nil, fmt.Errorf("no archetype recipes found")
	}
	return recipes, nil
}

// ParseProfiles parses the domain profile table below the
// `## Domain profiles` heading: code plus reads, writes and closure
// evidence columns.
func ParseProfiles(markdown string) (map[string]Profile, error) {
	profiles := make(map[string]Profile)
	lines := strings.Split(markdown, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Domain profiles" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("no Domain profiles section found")
	}
	for _, line := range lines[start+1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitRow(trimmed)
		if len(cells) < 4 {
			return nil, fmt.Errorf("ragged profile row: %q", trimmed)
		}
		code := strings.Trim(strings.TrimSpace(cells[0]), "`")
		if code == "" || strings.HasPrefix(code, "-") || code == "Profile" {
			continue
		}
		reads, writes, closure := strings.TrimSpace(cells[1]), strings.TrimSpace(cells[2]), strings.TrimSpace(cells[3])
		if reads == "" || writes == "" || closure == "" {
			return nil, fmt.Errorf("profile %s has an empty column", code)
		}
		if _, dup := profiles[code]; dup {
			return nil, fmt.Errorf("duplicate profile %s", code)
		}
		profiles[code] = Profile{Code: code, Reads: reads, Writes: writes, Closure: closure}
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no domain profiles found")
	}
	return profiles, nil
}

func splitRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	parts := strings.Split(trimmed, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// LoadSnapshot reads the live archetypes, profiles, accepted definitions
// and validated design records below root. It fails closed on shape
// drift or contract violations.
func LoadSnapshot(root string) (Snapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository root: %w", err)
	}
	var snap Snapshot
	archetypes, err := os.ReadFile(filepath.Join(root, "planning", "workflows", "_shared", "workflow-archetypes.md"))
	if err != nil {
		return Snapshot{}, err
	}
	if snap.Recipes, err = ParseRecipes(string(archetypes)); err != nil {
		return Snapshot{}, fmt.Errorf("parse archetype recipes: %w", err)
	}
	registry, err := os.ReadFile(filepath.Join(root, "planning", "workflows", "business-intent-workflow-registry.md"))
	if err != nil {
		return Snapshot{}, err
	}
	if snap.Profiles, err = ParseProfiles(string(registry)); err != nil {
		return Snapshot{}, fmt.Errorf("parse domain profiles: %w", err)
	}
	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		return Snapshot{}, err
	}
	if err := intentmanifests.ValidateIntentManifestYAML(descriptors); err != nil {
		return Snapshot{}, fmt.Errorf("accepted intent catalog failed validation: %w", err)
	}
	for _, d := range descriptors {
		snap.Accepted = append(snap.Accepted, fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version))
	}
	records, err := workflowdesign.LoadRecords(filepath.Join(root, "tools", "planning", "workflowdesign", "testdata", "seed", "records.yaml"))
	if err != nil {
		return Snapshot{}, err
	}
	if report := workflowdesign.ValidateRecords(records); !report.OK() {
		return Snapshot{}, fmt.Errorf("design records violate the contract: %+v", report.Findings)
	}
	snap.Records = records
	return snap, nil
}

// ExpandSnapshot expands one accepted definition's record with a delta.
// Unknown recipe or profile references fail with exact findings.
func ExpandSnapshot(snap Snapshot, definition string, delta Delta) (*Graph, []Finding) {
	var record *workflowdesign.DesignRecord
	for i := range snap.Records {
		if snap.Records[i].Definition == definition {
			record = &snap.Records[i]
			break
		}
	}
	if record == nil {
		return nil, []Finding{{Code: MissingRecord, Detail: "definition " + definition + " has no design record"}}
	}
	phases, ok := snap.Recipes[record.Archetype]
	if !ok {
		return nil, []Finding{{Code: UnknownRecipe, Detail: "record archetype " + record.Archetype + " names no parsed recipe"}}
	}
	profile, ok := snap.Profiles[record.DomainProfile]
	if !ok {
		return nil, []Finding{{Code: UnknownDomainProfile, Detail: "record domain " + record.DomainProfile + " names no parsed profile"}}
	}
	return Expand(Input{Recipe: Recipe{Archetype: record.Archetype, Phases: phases}, Profile: profile, Record: *record, Delta: delta})
}
