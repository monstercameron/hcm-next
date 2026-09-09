// Package corpus reconciles the specification, workflow, model, and todo
// registries into one deterministic coverage graph (GOV-024).
package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/obligations"
	"gopkg.in/yaml.v3"
)

const schemaVersion = 1

// Source names the four first-class corpus sources.
type Source string

const (
	SourceSpec     Source = "spec"
	SourceWorkflow Source = "workflow"
	SourceModel    Source = "model"
	SourceTodo     Source = "todo"
)

// Obligation is the graph projection of one normative sentence.
type Obligation struct {
	ID       string   `json:"id"`
	Document string   `json:"document"`
	Line     int      `json:"line"`
	Text     string   `json:"text,omitempty"`
	TodoIDs  []string `json:"todo_ids,omitempty"`
}

// SpecDocument retains enough source text to make model references explicit
// and auditable. It is never emitted in a report, because the report needs
// identities and counts rather than protected planning prose.
type SpecDocument struct {
	Path    string
	Content string
}

// Workflow is the normalized portion of a catalogue workflow used by the
// graph. Todo IDs must be named explicitly; prose citations do not count.
type Workflow struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Intents       []string `json:"intents"`
	TodoIDs       []string `json:"todo_ids"`
	SpecRefs      []string `json:"spec_refs,omitempty"`
	ModelText     []string `json:"-"`
	IntentStatus  []string `json:"-"`
	CatalogStatus string   `json:"status,omitempty"`
}

// ModelEntity is one entity from definitions/generation/model-sources.yaml.
type ModelEntity struct {
	Ref    string `json:"ref"`
	Name   string `json:"name,omitempty"`
	Key    string `json:"key,omitempty"`
	Status string `json:"status,omitempty"`
}

// Todo is the graph projection of one generated todo-registry row.
type Todo struct {
	ID           string            `json:"id"`
	Title        string            `json:"title,omitempty"`
	Test         string            `json:"test"`
	TestMatrix   map[string]string `json:"test_matrix,omitempty"`
	Refs         string            `json:"refs,omitempty"`
	SpecRefs     []string          `json:"spec_refs,omitempty"`
	WorkflowRefs []string          `json:"workflow_refs,omitempty"`
	Red          string            `json:"red,omitempty"`
	Green        string            `json:"green,omitempty"`
	Retired      bool              `json:"retired,omitempty"`
	Done         bool              `json:"done,omitempty"`
}

// Corpus is an in-memory, detached snapshot of all four inputs.
type Corpus struct {
	Obligations       []Obligation
	Specs             []SpecDocument
	Workflows         []Workflow
	WorkflowDocuments []string
	ModelEntities     []ModelEntity
	Todos             []Todo
}

// AllowlistEntry permits one exact, reviewed orphan for the current corpus
// snapshot. A wildcard is intentionally not supported: new orphans must be
// visible to CI rather than silently inheriting an old exception.
type AllowlistEntry struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Reason     string `json:"reason"`
	ReviewDate string `json:"review_date"`
}

// Options controls reconciliation. TestNames is optional for callers that
// only want source-pair coverage; repository reconciliation supplies it to
// enforce the executable-oracle edge in GOV-024.
type Options struct {
	Allowlist    []AllowlistEntry
	TestNames    map[string]bool
	CheckOracles bool
}

// PairCount is one directed source-pair edge count. Pairs are sorted by
// source name in Matrix and retain both directions only once.
type PairCount struct {
	Left  Source `json:"left"`
	Right Source `json:"right"`
	Count int    `json:"count"`
}

// CoverageMatrix is the deterministic pair-count projection of the graph.
type CoverageMatrix struct {
	SchemaVersion int         `json:"schema_version"`
	Sources       []Source    `json:"sources"`
	Pairs         []PairCount `json:"pairs"`
	Digest        string      `json:"digest"`
}

// Orphan is a missing edge or executable oracle. Detail carries the complete
// path a reviewer needs to repair, rather than a generic "uncovered" label.
type Orphan struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
	Detail string `json:"detail"`
}

func (o Orphan) Key() string { return o.Kind + "|" + o.ID }

// AllowlistedOrphan preserves the exact orphan and its accountable owner.
type AllowlistedOrphan struct {
	Orphan
	Owner      string `json:"owner"`
	Reason     string `json:"reason"`
	ReviewDate string `json:"review_date"`
}

// Report is the one graph result consumed by human reports and the command.
type Report struct {
	SchemaVersion int                 `json:"schema_version"`
	SourceCounts  map[Source]int      `json:"source_counts"`
	Matrix        CoverageMatrix      `json:"coverage_matrix"`
	Orphans       []Orphan            `json:"orphans"`
	Allowlisted   []AllowlistedOrphan `json:"allowlisted_orphans"`
	NewOrphans    []Orphan            `json:"new_orphans"`
}

// Violations returns only unallowlisted orphan paths.
func (r Report) Violations() []Orphan { return append([]Orphan(nil), r.NewOrphans...) }

// MatrixDigest returns the digest of the coverage matrix without its digest
// field, making it safe to compare with a golden value.
func (r Report) MatrixDigest() string { return r.Matrix.Digest }

// JSON returns deterministic indented report JSON.
func (r Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal corpus report: %w", err)
	}
	return append(b, '\n'), nil
}

// Reconcile builds a deterministic graph. Passing no options is useful for
// fixture tests; passing Options{TestNames: ...} additionally checks that each
// active todo's declared TEST is an executable oracle.
func Reconcile(c Corpus, options ...Options) Report {
	var opt Options
	if len(options) > 0 {
		opt = options[0]
	}
	prepareCorpus(&c)
	report := Report{
		SchemaVersion: schemaVersion,
		SourceCounts: map[Source]int{
			SourceSpec: len(c.Obligations), SourceWorkflow: len(c.Workflows),
			SourceModel: len(c.ModelEntities), SourceTodo: len(c.Todos),
		},
	}
	counts := make(map[[2]Source]int)
	add := func(left, right Source) { counts[[2]Source{left, right}]++ }

	todoByID := make(map[string]Todo, len(c.Todos))
	for _, todo := range c.Todos {
		todoByID[todo.ID] = todo
		if len(todo.SpecRefs) > 0 || len(todo.WorkflowRefs) > 0 || citedAuthority(todo.Refs) {
			for range todo.SpecRefs {
				add(SourceSpec, SourceTodo)
			}
			for range todo.WorkflowRefs {
				add(SourceWorkflow, SourceTodo)
			}
			if len(todo.SpecRefs) == 0 && len(todo.WorkflowRefs) == 0 {
				for _, ref := range authorityRefs(todo.Refs) {
					if strings.HasPrefix(ref, "planning/specs/") {
						add(SourceSpec, SourceTodo)
					}
					if strings.HasPrefix(ref, "planning/workflows/") || strings.HasPrefix(ref, "planning/reference-workflows/") {
						add(SourceWorkflow, SourceTodo)
					}
				}
			}
		}
	}

	specTexts := specTextByPath(c)
	modelRefs := make(map[string]map[Source]bool, len(c.ModelEntities))
	for _, entity := range c.ModelEntities {
		modelRefs[entity.Ref] = map[Source]bool{}
		for _, text := range specTexts {
			if entityMentioned(entity, text) {
				modelRefs[entity.Ref][SourceSpec] = true
				add(SourceSpec, SourceModel)
			}
		}
		for _, todo := range c.Todos {
			if entityMentioned(entity, todoText(todo)) {
				modelRefs[entity.Ref][SourceTodo] = true
				add(SourceTodo, SourceModel)
			}
		}
	}

	for _, workflow := range c.Workflows {
		for _, ref := range workflow.SpecRefs {
			if ref != "" {
				add(SourceSpec, SourceWorkflow)
			}
		}
		for _, todoID := range workflow.TodoIDs {
			if todoID != "" {
				add(SourceWorkflow, SourceTodo)
			}
		}
		for _, entity := range c.ModelEntities {
			if entityMentioned(entity, strings.Join(workflow.ModelText, "\n")) {
				add(SourceWorkflow, SourceModel)
			}
		}
	}

	for _, obligation := range c.Obligations {
		for _, todoID := range obligation.TodoIDs {
			if todoID != "" {
				add(SourceSpec, SourceTodo)
			}
		}
		if len(obligation.TodoIDs) == 0 {
			report.Orphans = append(report.Orphans, Orphan{Kind: "spec_obligation", ID: obligation.ID, Source: obligation.Document, Detail: fmt.Sprintf("%s:%d obligation has no requirement-level todo claim", obligation.Document, obligation.Line)})
			continue
		}
		for _, todoID := range obligation.TodoIDs {
			if todo, ok := todoByID[todoID]; !ok {
				report.Orphans = append(report.Orphans, Orphan{Kind: "spec_obligation", ID: obligation.ID, Source: obligation.Document, Detail: fmt.Sprintf("%s:%d claims missing todo %s", obligation.Document, obligation.Line, todoID)})
			} else if todo.Retired {
				report.Orphans = append(report.Orphans, Orphan{Kind: "spec_obligation", ID: obligation.ID, Source: obligation.Document, Detail: fmt.Sprintf("%s:%d reaches retired todo %s", obligation.Document, obligation.Line, todoID)})
			}
		}
	}

	for _, workflow := range c.Workflows {
		if len(workflow.Intents) == 0 {
			report.Orphans = append(report.Orphans, Orphan{Kind: "workflow_intents", ID: workflow.ID, Source: workflow.ID, Detail: "workflow has no explicitly named intent binding"})
		}
		if len(workflow.TodoIDs) == 0 {
			report.Orphans = append(report.Orphans, Orphan{Kind: "workflow_todos", ID: workflow.ID, Source: workflow.ID, Detail: "workflow has no explicitly named proving todo"})
		}
		for _, todoID := range workflow.TodoIDs {
			if _, ok := todoByID[todoID]; !ok {
				report.Orphans = append(report.Orphans, Orphan{Kind: "workflow_todos", ID: workflow.ID, Source: workflow.ID, Detail: fmt.Sprintf("workflow names missing todo %s", todoID)})
			}
		}
	}

	for _, entity := range c.ModelEntities {
		refs := modelRefs[entity.Ref]
		if !refs[SourceSpec] && !refs[SourceTodo] {
			report.Orphans = append(report.Orphans, Orphan{Kind: "model_entity", ID: entity.Ref, Source: entity.Ref, Detail: "model entity is not referenced by a spec or todo"})
		}
	}

	for _, todo := range c.Todos {
		if todo.Retired {
			continue
		}
		if !todoHasAuthority(todo, c) {
			report.Orphans = append(report.Orphans, Orphan{Kind: "todo_authority", ID: todo.ID, Source: todo.ID, Detail: "todo refs cite neither a current spec nor a current workflow"})
		}
		if opt.CheckOracles && strings.TrimSpace(todo.Test) == "" {
			report.Orphans = append(report.Orphans, Orphan{Kind: "todo_oracle", ID: todo.ID, Source: todo.ID, Detail: "todo has no executable TEST oracle"})
		} else if opt.CheckOracles && !opt.TestNames[todo.Test] {
			report.Orphans = append(report.Orphans, Orphan{Kind: "todo_oracle", ID: todo.ID, Source: todo.ID, Detail: fmt.Sprintf("todo TEST %q is not an executable test", todo.Test)})
		}
	}

	for _, pair := range allPairs() {
		report.Matrix.Pairs = append(report.Matrix.Pairs, PairCount{Left: pair[0], Right: pair[1], Count: counts[pair]})
	}
	report.Matrix.SchemaVersion = schemaVersion
	report.Matrix.Sources = []Source{SourceSpec, SourceWorkflow, SourceModel, SourceTodo}
	report.Matrix.Digest = matrixDigest(report.Matrix)
	sortOrphans(report.Orphans)
	report.Allowlisted, report.NewOrphans = classifyOrphans(report.Orphans, opt.Allowlist)
	return report
}

// ReconcileRepository loads the live four-source corpus. A missing workflow
// catalogue is accepted because another lane may be writing it concurrently.
func ReconcileRepository(root string, options ...Options) (Report, error) {
	c, err := LoadRepository(root)
	if err != nil {
		return Report{}, err
	}
	opt := Options{CheckOracles: true}
	if len(options) > 0 {
		opt = options[0]
		opt.CheckOracles = true
	}
	if opt.TestNames == nil {
		opt.TestNames, err = scanTestNames(root)
		if err != nil {
			return Report{}, err
		}
	}
	return Reconcile(c, opt), nil
}

// LoadRepository reads the current source snapshot without mutating it.
func LoadRepository(root string) (Corpus, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Corpus{}, fmt.Errorf("resolve repository root: %w", err)
	}
	registry, err := obligations.Scan(root)
	if err != nil {
		return Corpus{}, err
	}
	c := Corpus{}
	for _, item := range registry.Obligations {
		c.Obligations = append(c.Obligations, Obligation{ID: item.ID, Document: item.Document, Line: item.Line, TodoIDs: append([]string(nil), item.TodoIDsCitedNearby...)})
	}
	c.Specs, err = loadSpecDocuments(root)
	if err != nil {
		return Corpus{}, err
	}
	c.Todos, err = loadTodos(filepath.Join(root, "definitions", "planning", "todo-registry.json"))
	if err != nil {
		return Corpus{}, err
	}
	c.ModelEntities, err = loadModelEntities(filepath.Join(root, "definitions", "generation", "model-sources.yaml"))
	if err != nil {
		return Corpus{}, err
	}
	workflowPath := filepath.Join(root, "planning", "workflows", "catalog.yaml")
	if _, statErr := os.Stat(workflowPath); statErr == nil {
		c.Workflows, err = loadWorkflows(workflowPath)
		if err != nil {
			return Corpus{}, err
		}
		c.WorkflowDocuments, err = loadWorkflowDocuments(root)
		if err != nil {
			return Corpus{}, err
		}
	} else if !os.IsNotExist(statErr) {
		return Corpus{}, fmt.Errorf("stat workflow catalogue: %w", statErr)
	}
	return c, nil
}

// LoadAllowlist reads exact owner-backed exceptions. A missing file is an
// empty allowlist, which keeps new orphans failing closed.
func LoadAllowlist(path string) ([]AllowlistEntry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read corpus allowlist: %w", err)
	}
	var out []AllowlistEntry
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse corpus allowlist: %w", err)
	}
	for i, item := range out {
		if strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Owner) == "" {
			return nil, fmt.Errorf("allowlist[%d]: kind, id, and owner are required", i)
		}
	}
	return out, nil
}

// BuildAllowlist creates a reviewed baseline for a named corpus snapshot.
// Owners are assigned by orphan kind and every entry remains exact; this is
// intended for deliberate baseline updates, never for normal CI execution.
func BuildAllowlist(orphans []Orphan, reviewDate string) []AllowlistEntry {
	owners := map[string]string{
		"model_entity":     "model-governance",
		"spec_obligation":  "spec-governance",
		"todo_authority":   "backlog-governance",
		"todo_oracle":      "test-governance",
		"workflow_intents": "workflow-governance",
		"workflow_todos":   "workflow-governance",
	}
	reasons := map[string]string{
		"model_entity":     "present in the generated model registry but not yet connected to a current spec or todo",
		"spec_obligation":  "normative requirement is retained for the current planning baseline while todo closure is pending",
		"todo_authority":   "backlog row requires current authority citation repair",
		"todo_oracle":      "backlog row requires executable oracle closure",
		"workflow_intents": "catalogue workflow requires explicit intent binding repair",
		"workflow_todos":   "catalogue workflow requires explicit proving-todo repair",
	}
	seen := make(map[string]bool, len(orphans))
	entries := make([]AllowlistEntry, 0, len(orphans))
	for _, orphan := range orphans {
		if seen[orphan.Key()] {
			continue
		}
		seen[orphan.Key()] = true
		owner := owners[orphan.Kind]
		if owner == "" {
			owner = "corpus-governance"
		}
		entries = append(entries, AllowlistEntry{Kind: orphan.Kind, ID: orphan.ID, Owner: owner, Reason: reasons[orphan.Kind], ReviewDate: reviewDate})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
	return entries
}

// WriteAllowlist writes a deterministic owner-backed baseline file.
func WriteAllowlist(path string, entries []AllowlistEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal corpus allowlist: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create corpus allowlist directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write corpus allowlist: %w", err)
	}
	return nil
}

func prepareCorpus(c *Corpus) {
	for i := range c.Obligations {
		c.Obligations[i].TodoIDs = sortedUnique(c.Obligations[i].TodoIDs)
	}
	for i := range c.Workflows {
		c.Workflows[i].Intents = sortedUnique(c.Workflows[i].Intents)
		c.Workflows[i].TodoIDs = sortedUnique(c.Workflows[i].TodoIDs)
		c.Workflows[i].SpecRefs = sortedUnique(c.Workflows[i].SpecRefs)
		c.Workflows[i].ModelText = append([]string(nil), c.Workflows[i].ModelText...)
	}
	sort.Slice(c.Obligations, func(i, j int) bool { return c.Obligations[i].ID < c.Obligations[j].ID })
	sort.Slice(c.Workflows, func(i, j int) bool { return c.Workflows[i].ID < c.Workflows[j].ID })
	sort.Slice(c.ModelEntities, func(i, j int) bool { return c.ModelEntities[i].Ref < c.ModelEntities[j].Ref })
	sort.Slice(c.Todos, func(i, j int) bool { return c.Todos[i].ID < c.Todos[j].ID })
}

func allPairs() [][2]Source {
	return [][2]Source{{SourceSpec, SourceWorkflow}, {SourceSpec, SourceModel}, {SourceSpec, SourceTodo}, {SourceWorkflow, SourceModel}, {SourceWorkflow, SourceTodo}, {SourceModel, SourceTodo}}
}

func matrixDigest(matrix CoverageMatrix) string {
	copyOf := matrix
	copyOf.Digest = ""
	b, _ := json.Marshal(copyOf)
	return canonicalbytes.Digest(b)
}

func classifyOrphans(orphans []Orphan, allowlist []AllowlistEntry) ([]AllowlistedOrphan, []Orphan) {
	allow := make(map[string]AllowlistEntry, len(allowlist))
	for _, item := range allowlist {
		allow[item.Kind+"|"+item.ID] = item
	}
	var accepted []AllowlistedOrphan
	var fresh []Orphan
	for _, orphan := range orphans {
		if item, ok := allow[orphan.Key()]; ok {
			accepted = append(accepted, AllowlistedOrphan{Orphan: orphan, Owner: item.Owner, Reason: item.Reason, ReviewDate: item.ReviewDate})
		} else {
			fresh = append(fresh, orphan)
		}
	}
	return accepted, fresh
}

func sortOrphans(orphans []Orphan) {
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Kind != orphans[j].Kind {
			return orphans[i].Kind < orphans[j].Kind
		}
		if orphans[i].ID != orphans[j].ID {
			return orphans[i].ID < orphans[j].ID
		}
		return orphans[i].Detail < orphans[j].Detail
	})
}

func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if strings.TrimSpace(value) != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func specTextByPath(c Corpus) map[string]string {
	out := make(map[string]string, len(c.Specs))
	for _, spec := range c.Specs {
		out[slash(spec.Path)] = spec.Content
	}
	for _, obligation := range c.Obligations {
		if _, ok := out[slash(obligation.Document)]; !ok {
			out[slash(obligation.Document)] = obligation.Text
		}
	}
	return out
}

func entityMentioned(entity ModelEntity, text string) bool {
	text = strings.ToLower(text)
	for _, candidate := range []string{entity.Ref, entity.Name, entity.Key} {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" || (candidate == entity.Key && len(candidate) < 4) {
			continue
		}
		if strings.Contains(text, candidate) {
			return true
		}
	}
	return false
}

func todoText(todo Todo) string {
	return strings.Join([]string{todo.ID, todo.Title, todo.Test, todo.Refs, todo.Red, todo.Green, strings.Join(todo.SpecRefs, " "), strings.Join(todo.WorkflowRefs, " ")}, "\n")
}

func todoHasAuthority(todo Todo, c Corpus) bool {
	for _, ref := range todo.SpecRefs {
		for _, spec := range c.Specs {
			if sameRef(ref, spec.Path) {
				return true
			}
		}
	}
	for _, ref := range todo.WorkflowRefs {
		for _, workflow := range c.Workflows {
			if ref == workflow.ID {
				return true
			}
		}
		for _, path := range c.WorkflowDocuments {
			if sameRef(ref, path) {
				return true
			}
		}
	}
	for _, ref := range authorityRefs(todo.Refs) {
		for _, spec := range c.Specs {
			if sameRef(ref, spec.Path) {
				return true
			}
		}
		for _, workflow := range c.Workflows {
			if ref == workflow.ID {
				return true
			}
		}
		for _, path := range c.WorkflowDocuments {
			if sameRef(ref, path) {
				return true
			}
		}
	}
	return false
}

func citedAuthority(refs string) bool { return len(authorityRefs(refs)) != 0 }

var mdLinkRE = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)
var pathRE = regexp.MustCompile(`(?:planning/)?(?:specs|workflows|reference-workflows)/[^\s,)#]+|planning/plan\.md`)

func authorityRefs(refs string) []string {
	seen := map[string]bool{}
	for _, match := range mdLinkRE.FindAllStringSubmatch(refs, -1) {
		seen[normalizeRef(match[1])] = true
	}
	for _, match := range pathRE.FindAllString(refs, -1) {
		seen[normalizeRef(match)] = true
	}
	var out []string
	for ref := range seen {
		if ref != "" {
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeRef(ref string) string {
	ref = strings.TrimSpace(strings.SplitN(ref, "#", 2)[0])
	ref = strings.TrimPrefix(ref, "./")
	if strings.HasPrefix(ref, "specs/") || strings.HasPrefix(ref, "workflows/") || strings.HasPrefix(ref, "reference-workflows/") {
		ref = "planning/" + ref
	}
	return slash(ref)
}

func sameRef(a, b string) bool { return normalizeRef(a) == normalizeRef(b) }

func loadTodos(path string) ([]Todo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read todo registry: %w", err)
	}
	var todos []Todo
	if err := json.Unmarshal(b, &todos); err != nil {
		return nil, fmt.Errorf("parse todo registry: %w", err)
	}
	return todos, nil
}

type modelManifest struct {
	Entities []ModelEntity `yaml:"entities"`
}

func loadModelEntities(path string) ([]ModelEntity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model registry: %w", err)
	}
	var manifest modelManifest
	if err := yaml.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("parse model registry: %w", err)
	}
	for i := range manifest.Entities {
		if manifest.Entities[i].Name == "" {
			manifest.Entities[i].Name = strings.TrimSuffix(strings.TrimSuffix(manifest.Entities[i].Ref, "/v1"), "/v2")
		}
	}
	return manifest.Entities, nil
}

func loadSpecDocuments(root string) ([]SpecDocument, error) {
	paths := []string{filepath.Join(root, "planning", "plan.md")}
	specRoot := filepath.Join(root, "planning", "specs")
	err := filepath.WalkDir(specRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk spec documents: %w", err)
	}
	sort.Strings(paths)
	var out []SpecDocument
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read spec document: %w", err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		out = append(out, SpecDocument{Path: slash(rel), Content: string(b)})
	}
	return out, nil
}

func loadWorkflowDocuments(root string) ([]string, error) {
	workflowRoot := filepath.Join(root, "planning", "workflows")
	var out []string
	err := filepath.WalkDir(workflowRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && (strings.EqualFold(filepath.Ext(path), ".md") || strings.EqualFold(filepath.Ext(path), ".yaml") || strings.EqualFold(filepath.Ext(path), ".yml")) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, slash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk workflow documents: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

type workflowFile struct {
	Workflows []map[string]interface{} `yaml:"workflows"`
}

func loadWorkflows(path string) ([]Workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow catalogue: %w", err)
	}
	var file workflowFile
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("parse workflow catalogue: %w", err)
	}
	var out []Workflow
	for _, raw := range file.Workflows {
		workflow := Workflow{ID: scalar(raw["id"]), Name: scalar(raw["name"]), CatalogStatus: scalar(raw["status"])}
		if intents, ok := raw["intents"].([]interface{}); ok {
			for _, item := range intents {
				if m, ok := item.(map[string]interface{}); ok {
					ref := scalar(m["intent_type_id"])
					if ref != "" {
						workflow.Intents = append(workflow.Intents, ref)
					}
				}
			}
		}
		workflow.TodoIDs = namedIDs(raw, map[string]bool{"todos": true, "todo_ids": true, "todo_references": true, "proof_todos": true})
		workflow.TodoIDs = append(workflow.TodoIDs, idsInNamedStrings(raw, "evidence_citations")...)
		workflow.SpecRefs = refsInNamedStrings(raw, "evidence_citations")
		collectScalarStrings(raw, &workflow.ModelText)
		workflow.Intents = sortedUnique(workflow.Intents)
		workflow.TodoIDs = sortedUnique(workflow.TodoIDs)
		workflow.SpecRefs = sortedUnique(workflow.SpecRefs)
		out = append(out, workflow)
	}
	return out, nil
}

func scalar(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func collectScalarStrings(value interface{}, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case []interface{}:
		for _, item := range typed {
			collectScalarStrings(item, out)
		}
	case map[string]interface{}:
		for _, item := range typed {
			collectScalarStrings(item, out)
		}
	}
}

func namedIDs(value interface{}, names map[string]bool) []string {
	var out []string
	var walk func(interface{}, string)
	walk = func(current interface{}, key string) {
		if names[key] {
			collectIDs(current, &out)
			return
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			for child, item := range typed {
				walk(item, child)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item, key)
			}
		}
	}
	walk(value, "")
	return sortedUnique(out)
}

func collectIDs(value interface{}, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, todoIDs(typed)...)
	case []interface{}:
		for _, item := range typed {
			collectIDs(item, out)
		}
	case map[string]interface{}:
		for key, item := range typed {
			if key == "id" || key == "todo_id" {
				collectIDs(item, out)
			} else {
				collectIDs(item, out)
			}
		}
	}
}

var todoIDRE = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+\b`)

func todoIDs(text string) []string { return todoIDRE.FindAllString(text, -1) }

func idsInNamedStrings(value interface{}, name string) []string {
	var result []string
	var walk func(interface{}, string)
	walk = func(current interface{}, key string) {
		if key == name {
			collectIDs(current, &result)
			return
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			for child, item := range typed {
				walk(item, child)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item, key)
			}
		}
	}
	walk(value, "")
	return sortedUnique(result)
}

func refsInNamedStrings(value interface{}, name string) []string {
	var result []string
	var walk func(interface{}, string)
	walk = func(current interface{}, key string) {
		if key == name {
			var texts []string
			collectScalarStrings(current, &texts)
			for _, text := range texts {
				for _, ref := range authorityRefs(text) {
					if strings.HasPrefix(ref, "planning/specs/") || strings.HasSuffix(ref, "/plan.md") {
						result = append(result, ref)
					}
				}
			}
			return
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			for child, item := range typed {
				walk(item, child)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item, key)
			}
		}
	}
	walk(value, "")
	return sortedUnique(result)
}

func scanTestNames(root string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, sourceRoot := range []string{"tools", "internal", "cmd", "gen"} {
		dir := filepath.Join(root, sourceRoot)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				if info.Name() == "vendor" || info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(info.Name(), "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(b), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "func" && (strings.HasPrefix(fields[1], "Test") || strings.HasPrefix(fields[1], "Fuzz") || strings.HasPrefix(fields[1], "Benchmark")) {
					out[strings.SplitN(fields[1], "(", 2)[0]] = true
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan executable test names below %s: %w", dir, err)
		}
	}
	return out, nil
}

func slash(path string) string { return filepath.ToSlash(filepath.Clean(path)) }
