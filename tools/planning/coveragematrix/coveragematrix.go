// Package coveragematrix implements the GOV-011 platform capability
// coverage matrix: it maps every compiled-in BOOTSTRAP capability
// (internal/capability) and every catalog intent definition
// (internal/intent/definitions) to the planning/todos.md entries that claim
// them, classifies each item's coverage state, and fails the build on an
// item nobody has claimed or on a claim whose cited evidence test does not
// exist in the repository.
//
// Coverage states mirror planning/specs/platform-capability-coverage-matrix.md's
// vocabulary, scoped to these compiled-in items:
//
//	DEFINED  a todo names this exact item (by intent ref or DisplayName) and
//	         at least one of its Done claimants cites a real, existing test
//	PARTIAL  a todo names this exact item, but no Done claimant yet cites a
//	         real test (work claimed, evidence not closed)
//	IMPLIED  only a same-domain todo (INTENT CONTEXT SETS) mentions this
//	         item's owning domain, with no todo naming the item itself
//	MISSING  no todo claims this item at all, not even by domain
package coveragematrix

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	intentdefs "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
	"gopkg.in/yaml.v3"
)

// Kind names which compiled-in table an Item comes from.
type Kind string

const (
	KindCapability Kind = "capability"
	KindIntent     Kind = "intent"
)

// Coverage state vocabulary (see package doc).
const (
	StateDefined Coverage = "DEFINED"
	StatePartial Coverage = "PARTIAL"
	StateImplied Coverage = "IMPLIED"
	StateMissing Coverage = "MISSING"
)

// Coverage is one of the four coverage states above.
type Coverage string

// Item is one governed unit the coverage matrix tracks.
type Item struct {
	Kind        Kind
	ID          string // capability ID, or intent Ref.TypeID (dotted, no /vN)
	DisplayName string // intent DisplayName; empty for a capability
	OwnerDomain string // owner domain exactly as declared in the Go source
}

// CapabilityItems returns one Item per capability in the compiled-in
// BOOTSTRAP registry (internal/capability.NewBootstrapRegistry), in the
// registry's own deterministic (ID, Version) order.
func CapabilityItems() ([]Item, error) {
	reg, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, fmt.Errorf("coveragematrix: build bootstrap capability registry: %w", err)
	}
	var items []Item
	for _, rec := range reg.List() {
		items = append(items, Item{
			Kind:        KindCapability,
			ID:          rec.Definition.ID,
			OwnerDomain: rec.Definition.OwnerDomain,
		})
	}
	return items, nil
}

// IntentItems returns one Item per catalog intent definition
// (internal/intent/definitions.All), in catalog order.
func IntentItems() []Item {
	var items []Item
	for _, d := range intentdefs.All() {
		items = append(items, Item{
			Kind:        KindIntent,
			ID:          d.Ref.TypeID,
			DisplayName: d.DisplayName,
			OwnerDomain: d.OwnerDomain,
		})
	}
	return items
}

// AllItems returns every capability item followed by every intent item.
func AllItems() ([]Item, error) {
	caps, err := CapabilityItems()
	if err != nil {
		return nil, err
	}
	return append(caps, IntentItems()...), nil
}

// ClaimKind names how a todo referenced an Item.
type ClaimKind string

const (
	// ClaimDirect is an exact reference: the todo's INTENT CONTEXT
	// `INTENTS=` field names this item's ID, or its `DIRECT=` field names
	// an intent's DisplayName that this item shares (a capability whose ID
	// equals a claimed intent's ref is credited with the same claim: the
	// BOOTSTRAP table publishes that intent's capability under the
	// identical name).
	ClaimDirect ClaimKind = "DIRECT"
	// ClaimDomain is a same-domain reference only: the todo's `SETS=` field
	// names a BI.<domain> token matching the item's OwnerDomain, but no
	// `INTENTS=`/`DIRECT=` field names the item itself.
	ClaimDomain ClaimKind = "DOMAIN"
)

// Claim is one todo's reference to one Item.
type Claim struct {
	TodoID string
	Kind   ClaimKind
}

// IntentContext is one todo's parsed `INTENT CONTEXT` field. todoregistry
// does not retain this field (it is not part of the core registry), so this
// package extracts it directly from planning/todos.md, keyed by todo ID.
type IntentContext struct {
	Sets    []string // e.g. ["BI.PEOPLE", "BI.REWARDS"]
	Direct  []string // intent DisplayNames, e.g. ["PromoteWorker"]
	Intents []string // dotted intent refs with the /vN stripped
}

var intentContextFieldRe = regexp.MustCompile("^- \\*\\*INTENT CONTEXT:\\*\\* `(.*)`\\s*\\.?\\s*$")
var todoTitleRe = regexp.MustCompile("^- \\[[ x]\\] `([^`]+)`")

// ParseIntentContexts walks todos.md content and returns each todo's parsed
// INTENT CONTEXT field, keyed by todo ID. A todo with no INTENT CONTEXT
// field (should not occur in a well-formed corpus) is simply absent from
// the map.
func ParseIntentContexts(content string) map[string]IntentContext {
	out := make(map[string]IntentContext)
	currentID := ""
	for _, line := range strings.Split(content, "\n") {
		if m := todoTitleRe.FindStringSubmatch(line); m != nil {
			currentID = m[1]
			continue
		}
		trimmed := strings.TrimSpace(line)
		if currentID == "" {
			continue
		}
		if m := intentContextFieldRe.FindStringSubmatch(trimmed); m != nil {
			out[currentID] = parseIntentContextField(m[1])
		}
	}
	return out
}

// parseIntentContextField parses one INTENT CONTEXT value, e.g.:
//
//	ROLE=DIRECT; SETS=BI.PEOPLE; INTENTS=hcmnext.people.explain_worker_state/v1; FAMILY=...; WHY=...
//
// Unknown fields (ROLE, FAMILY, CHILDREN, WHY, ...) are ignored.
func parseIntentContextField(raw string) IntentContext {
	var ic IntentContext
	for field := range strings.SplitSeq(raw, ";") {
		field = strings.TrimSpace(field)
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "none") {
			continue
		}
		switch key {
		case "SETS":
			ic.Sets = append(ic.Sets, splitCSV(value)...)
		case "DIRECT":
			ic.Direct = append(ic.Direct, splitCSV(value)...)
		case "INTENTS":
			for _, tok := range splitCSV(value) {
				ref, _, _ := strings.Cut(tok, "/v")
				ic.Intents = append(ic.Intents, ref)
			}
		}
	}
	return ic
}

func splitCSV(value string) []string {
	var out []string
	for _, tok := range strings.Split(value, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// ClaimsFor returns every Claim made against item across the todo corpus,
// deduplicated and sorted by (Kind, TodoID) with DIRECT claims first. A
// capability whose ID is textually identical to an intent's ref (every
// P1A capability in bootstrap.go is published under its intent's own ref)
// is matched by that same ID equality, with no separate propagation step
// needed: the todo names the ref once, and both items share it.
func ClaimsFor(item Item, contexts map[string]IntentContext) []Claim {
	seen := make(map[Claim]bool)
	for todoID, ic := range contexts {
		direct := false
		for _, ref := range ic.Intents {
			if ref == item.ID {
				direct = true
			}
		}
		if item.DisplayName != "" {
			for _, name := range ic.Direct {
				if name == item.DisplayName {
					direct = true
				}
			}
		}
		if direct {
			seen[Claim{TodoID: todoID, Kind: ClaimDirect}] = true
			continue
		}
		if item.OwnerDomain == "" {
			continue
		}
		domainToken := "BI." + strings.ToUpper(item.OwnerDomain)
		for _, s := range ic.Sets {
			if s == domainToken {
				seen[Claim{TodoID: todoID, Kind: ClaimDomain}] = true
			}
		}
	}

	claims := make([]Claim, 0, len(seen))
	for c := range seen {
		claims = append(claims, c)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Kind != claims[j].Kind {
			return claims[i].Kind == ClaimDirect
		}
		return claims[i].TodoID < claims[j].TodoID
	})
	return claims
}

// Entry is one Item's computed coverage: its state, every claim against it,
// and every existing test name cited by a Done DIRECT claimant's Evidence
// field.
type Entry struct {
	Item   Item
	State  Coverage
	Claims []Claim
	Tests  []string
}

// EvidenceViolation names one Done todo whose Evidence field, in the
// context of claiming item, names a test that does not exist anywhere in
// the repository.
type EvidenceViolation struct {
	Item   Item
	TodoID string
	Test   string
}

func (v EvidenceViolation) String() string {
	return fmt.Sprintf("%s: todo %s claims %s but cites nonexistent test %s", v.Item.ID, v.TodoID, v.Item.Kind, v.Test)
}

// withSharedDisplayNames returns a copy of items where every capability
// item whose ID is textually identical to an intent item's ID inherits
// that intent's DisplayName. Every P1A capability in bootstrap.go is
// published under its intent's own ref, so a todo naming the intent via
// `DIRECT=<DisplayName>` has, by construction, also named the capability
// BOOTSTRAP publishes under the identical ID - this lets ClaimsFor's
// existing DisplayName match pick that up for the capability item too,
// with no separate propagation logic.
func withSharedDisplayNames(items []Item) []Item {
	displayNameByID := make(map[string]string, len(items))
	for _, it := range items {
		if it.Kind == KindIntent && it.DisplayName != "" {
			displayNameByID[it.ID] = it.DisplayName
		}
	}

	out := make([]Item, len(items))
	for i, it := range items {
		if it.Kind == KindCapability && it.DisplayName == "" {
			if dn, ok := displayNameByID[it.ID]; ok {
				it.DisplayName = dn
			}
		}
		out[i] = it
	}
	return out
}

// Build computes one Entry per item plus every evidence violation found
// among its DIRECT claimants. existingTests is normally
// traceability.ScanTestNames(root)'s result.
func Build(items []Item, todos []todoregistry.Todo, contexts map[string]IntentContext, existingTests map[string]bool) ([]Entry, []EvidenceViolation) {
	items = withSharedDisplayNames(items)

	todoByID := make(map[string]todoregistry.Todo, len(todos))
	for _, t := range todos {
		todoByID[t.ID] = t
	}

	var entries []Entry
	var violations []EvidenceViolation

	for _, item := range items {
		claims := ClaimsFor(item, contexts)
		entry := Entry{Item: item, Claims: claims}

		hasDirect := false
		hasDoneDirectWithRealTest := false
		for _, c := range claims {
			if c.Kind != ClaimDirect {
				continue
			}
			hasDirect = true
			td, ok := todoByID[c.TodoID]
			if !ok || !td.Done {
				continue
			}
			names := traceability.ExtractEvidenceTestNames(td.Evidence)
			for _, n := range names {
				if existingTests[n] {
					hasDoneDirectWithRealTest = true
					entry.Tests = appendUnique(entry.Tests, n)
				} else {
					violations = append(violations, EvidenceViolation{Item: item, TodoID: c.TodoID, Test: n})
				}
			}
		}

		switch {
		case hasDoneDirectWithRealTest:
			entry.State = StateDefined
		case hasDirect:
			entry.State = StatePartial
		case len(claims) > 0:
			entry.State = StateImplied
		default:
			entry.State = StateMissing
		}

		sort.Strings(entry.Tests)
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Item.Kind != entries[j].Item.Kind {
			return entries[i].Item.Kind < entries[j].Item.Kind
		}
		return entries[i].Item.ID < entries[j].Item.ID
	})
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Item.ID != violations[j].Item.ID {
			return violations[i].Item.ID < violations[j].Item.ID
		}
		if violations[i].TodoID != violations[j].TodoID {
			return violations[i].TodoID < violations[j].TodoID
		}
		return violations[i].Test < violations[j].Test
	})

	return entries, violations
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// CoverageViolation names one item whose coverage state fails GOV-011's
// GREEN bar: MISSING (no claim at all) or IMPLIED (only a domain-level
// mention, never named specifically).
type CoverageViolation struct {
	Item  Item
	State Coverage
}

func (v CoverageViolation) String() string {
	return fmt.Sprintf("%s %s (%s): coverage is %s, want DEFINED or PARTIAL", v.Item.Kind, v.Item.ID, v.Item.OwnerDomain, v.State)
}

// CheckCoverage returns a CoverageViolation for every entry whose state is
// MISSING or IMPLIED (GOV-011 RED: "fails when a ... dependency is IMPLIED
// or MISSING"). DEFINED and PARTIAL both pass: PARTIAL is claimed work
// whose evidence has not closed yet, which is an accepted, visible
// boundary, not silence.
func CheckCoverage(entries []Entry) []CoverageViolation {
	var violations []CoverageViolation
	for _, e := range entries {
		if e.State == StateMissing || e.State == StateImplied {
			violations = append(violations, CoverageViolation{Item: e.Item, State: e.State})
		}
	}
	return violations
}

// --- YAML generation -------------------------------------------------------

type yamlEntry struct {
	ID          string   `yaml:"id"`
	Kind        string   `yaml:"kind"`
	DisplayName string   `yaml:"display_name,omitempty"`
	OwnerDomain string   `yaml:"owner_domain"`
	State       string   `yaml:"state"`
	Claims      []string `yaml:"claims"`
	Tests       []string `yaml:"tests"`
}

type yamlFile struct {
	Version int         `yaml:"version"`
	Items   []yamlEntry `yaml:"items"`
}

// ToYAML renders entries deterministically: sorted (already Build's
// contract), LF-terminated, UTF-8 without BOM. claims are rendered as
// "<TodoID>:<Kind>" strings so the file stays a flat, diff-friendly list.
func ToYAML(entries []Entry) ([]byte, error) {
	out := yamlFile{Version: 1}
	for _, e := range entries {
		ye := yamlEntry{
			ID:          e.Item.ID,
			Kind:        string(e.Item.Kind),
			DisplayName: e.Item.DisplayName,
			OwnerDomain: e.Item.OwnerDomain,
			State:       string(e.State),
			Claims:      []string{},
			Tests:       e.Tests,
		}
		if ye.Tests == nil {
			ye.Tests = []string{}
		}
		for _, c := range e.Claims {
			ye.Claims = append(ye.Claims, c.TodoID+":"+string(c.Kind))
		}
		out.Items = append(out.Items, ye)
	}

	// 2-space sequence indent matches this repository's `npx prettier
	// --write definitions/planning` formatting exactly (yaml.v3's default
	// is 4), so the generated file is prettier-idempotent: running
	// prettier over it produces no further diff.
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// ScanIntentContexts reads planning/todos.md at path and returns every
// todo's parsed INTENT CONTEXT field, keyed by todo ID. A thin file-reading
// wrapper around ParseIntentContexts, so callers (tests, the plancheck
// subcommand) share one field grammar.
func ScanIntentContexts(path string) (map[string]IntentContext, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("coveragematrix: reading %s: %w", path, err)
	}
	return ParseIntentContexts(string(content)), nil
}

// KnownGap is an allow-listed coverage gap, recorded with its rationale in
// definitions/planning/known-defects.yaml.
type KnownGap struct {
	ID     string `yaml:"id"`
	Owner  string `yaml:"owner"`
	Expiry string `yaml:"expiry"`
	Reason string `yaml:"reason"`
}

type knownGapsFile struct {
	KnownGaps []KnownGap `yaml:"known_coverage_gaps"`
}

// LoadKnownGaps reads the known-coverage-gaps allow-list. A missing file is
// treated as an empty list.
func LoadKnownGaps(path string) ([]KnownGap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("coveragematrix: reading %s: %w", path, err)
	}
	var f knownGapsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("coveragematrix: parsing %s: %w", path, err)
	}
	return f.KnownGaps, nil
}
