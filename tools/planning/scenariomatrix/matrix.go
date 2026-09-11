// Package scenariomatrix generates adversarial scenario matrices from
// workflow designs (WF-DISC-010): every template in the reviewed table
// below yields exactly one scenario or one typed justification, so each
// declared read, write, effect, wait, human decision, compartment,
// invalidator and completion dimension produces exact fixtures with
// prohibited and expected states. Shared mechanics live in the template
// table once; every oracle names domain-specific values explicitly. It
// is kernel-pure and safe for concurrent use: generation shares no
// mutable state.
package scenariomatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

// Generation finding codes.
const (
	MissingExpansion = "MISSING_EXPANSION"
	GraphMismatch    = "GRAPH_MISMATCH"
	ProfileMismatch  = "PROFILE_MISMATCH"
)

// Scenario classes.
const (
	ClassPositive    = "POSITIVE"
	ClassNegative    = "NEGATIVE"
	ClassTemporal    = "TEMPORAL"
	ClassConcurrency = "CONCURRENCY"
	ClassFailure     = "FAILURE"
	ClassSecurity    = "SECURITY"
	ClassRepair      = "REPAIR"
)

// Default justifications for graph-derived guards without a record
// dimension of their own.
const (
	NoEffectsDeclared = "NO_EFFECTS_DECLARED"
	NoEnginesDeclared = "NO_ENGINES_DECLARED"
)

// Scenario is one generated adversarial scenario with its explicit oracle.
type Scenario struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Class      string `json:"class"`
	Dimension  string `json:"dimension"`
	Fixture    string `json:"fixture"`
	Expected   string `json:"expected"`
	Prohibited string `json:"prohibited"`
	Oracle     string `json:"oracle"`
}

// NotApplicable is one typed justification for a skipped template.
type NotApplicable struct {
	Class     string `json:"class"`
	Dimension string `json:"dimension"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}

// Matrix is one definition's complete scenario matrix.
type Matrix struct {
	Definition    string          `json:"definition"`
	Intent        string          `json:"intent"`
	Scenarios     []Scenario      `json:"scenarios"`
	NotApplicable []NotApplicable `json:"not_applicable,omitempty"`
	Digest        string          `json:"digest"`
}

// Finding is one exact generation diagnostic.
type Finding struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// MarshalMatrix renders a matrix with its findings as canonical JSON.
func MarshalMatrix(matrix *Matrix, findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Code != ordered[j].Code {
			return ordered[i].Code < ordered[j].Code
		}
		return ordered[i].Detail < ordered[j].Detail
	})
	rendered, err := json.MarshalIndent(struct {
		Matrix   *Matrix   `json:"matrix"`
		Findings []Finding `json:"findings,omitempty"`
	}{Matrix: matrix, Findings: ordered}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

type context struct {
	record  workflowdesign.DesignRecord
	profile workflowexpansion.Profile
	engines []string
	writes  []string
	effects []string
}

type template struct {
	name      string
	class     string
	dimension string
	guard     func(ctx *context) (bool, string)
	build     func(ctx *context) (fixture, expected, prohibited, oracle string)
}

func applicable(dimension workflowdesign.Dimension) (bool, string) {
	if dimension.Value == workflowdesign.NotApplicable || strings.TrimSpace(dimension.Value) == "" && len(dimension.Items) == 0 {
		if dimension.Value == workflowdesign.NotApplicable && strings.TrimSpace(dimension.Reason) != "" {
			return false, dimension.Reason
		}
		return false, "UNDECLARED"
	}
	return true, ""
}

func dimGuard(dimension string, get func(workflowdesign.DesignRecord) workflowdesign.Dimension) func(*context) (bool, string) {
	return func(ctx *context) (bool, string) {
		ok, reason := applicable(get(ctx.record))
		if !ok && reason == "UNDECLARED" {
			return false, reason
		}
		return ok, reason
	}
}

func eitherGuard(first, second func(*context) (bool, string)) func(*context) (bool, string) {
	return func(ctx *context) (bool, string) {
		if ok, _ := first(ctx); ok {
			return true, ""
		}
		if ok, _ := second(ctx); ok {
			return true, ""
		}
		_, reason := first(ctx)
		return false, reason
	}
}

func effectsGuard(ctx *context) (bool, string) {
	if len(ctx.effects) > 0 {
		return true, ""
	}
	return false, NoEffectsDeclared
}

func enginesGuard(ctx *context) (bool, string) {
	if len(ctx.engines) > 0 {
		return true, ""
	}
	return false, NoEnginesDeclared
}

func alwaysGuard(*context) (bool, string) { return true, "" }

func csv(values []string) string { return strings.Join(values, ",") }

func baseFixture(ctx *context) string {
	return "definition=" + ctx.record.Definition + " boundary=" + ctx.record.InputBoundary + " engines=" + csv(ctx.engines)
}

// templates is the reviewed scenario table: shared mechanics defined
// once, domain-specific oracles built per record. TemplateCount pins its
// size so a dropped template fails conformance instead of shrinking a
// matrix silently.
var templates = []template{
	{name: "happy-path-execution", class: ClassPositive, dimension: "completion", guard: alwaysGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx), "terminal " + ctx.record.Completion + " observed with digest",
			"partial or unordered completion",
			"execute " + ctx.record.Intent + " through " + ctx.record.InputBoundary + " to " + ctx.record.Completion
	}},
	{name: "exact-write-posted", class: ClassPositive, dimension: "writes", guard: dimGuard("writes", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Writes }), build: func(ctx *context) (string, string, string, string) {
		writes := csv(ctx.writes)
		return baseFixture(ctx) + " writes=" + writes, writes + " posted exactly once",
			"duplicate " + writes + " rows",
			"write " + writes + " posted exactly once with " + ctx.record.Completion
	}},
	{name: "exact-read-returned", class: ClassPositive, dimension: "reads", guard: enginesGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx), "engines " + csv(ctx.engines) + " return typed reads",
			"untyped or partial reads presented as complete",
			"engines " + csv(ctx.engines) + " return typed reads under " + ctx.record.SnapshotPolicy
	}},
	{name: "human-decision-recorded", class: ClassPositive, dimension: "human", guard: dimGuard("human", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.HumanWork }), build: func(ctx *context) (string, string, string, string) {
		human := dimText(ctx.record.HumanWork)
		return baseFixture(ctx) + " decider=" + human, "decision by " + human + " recorded with rationale",
			"unrecorded decision",
			"decision by " + human + " recorded with rationale before " + ctx.record.Completion
	}},
	{name: "completion-observed", class: ClassPositive, dimension: "completion", guard: alwaysGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx), ctx.record.Completion + " observed with digest",
			"unobserved completion",
			"terminal " + ctx.record.Completion + " observed with digest per " + ctx.profile.Closure
	}},
	{name: "invalid-input-rejected", class: ClassNegative, dimension: "reads", guard: alwaysGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " input=malformed", "typed rejection naming the boundary",
			"silent acceptance past " + ctx.record.InputBoundary,
			"malformed input rejected at " + ctx.record.InputBoundary + " with a typed error"
	}},
	{name: "duplicate-signal-coalesced", class: ClassNegative, dimension: "effects", guard: eitherGuard(dimGuard("writes", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Writes }), effectsGuard), build: func(ctx *context) (string, string, string, string) {
		target := csv(ctx.writes)
		if target == "" {
			target = csv(ctx.effects)
		}
		return baseFixture(ctx) + " signal=" + target + " twice", "duplicate " + target + " coalesced to one application",
			"double application of " + target,
			"duplicate signal for " + target + " coalesced to one application"
	}},
	{name: "provider-ambiguity-refused", class: ClassNegative, dimension: "reads", guard: enginesGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " provider=ambiguous", "ambiguous provider refused with a typed error",
			"ambiguous provider silently selected",
			"engines " + csv(ctx.engines) + " refuse an ambiguous provider with a typed error"
	}},
	{name: "stale-approval-revalidated", class: ClassTemporal, dimension: "waits", guard: eitherGuard(dimGuard("waits", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Waits }), dimGuard("human", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.HumanWork })), build: func(ctx *context) (string, string, string, string) {
		waits := dimText(ctx.record.Waits)
		return baseFixture(ctx) + " approval=stale", "stale approval revalidated after " + waits,
			"stale approval honored",
			"stale approval revalidated after " + waits + " before " + ctx.record.Completion
	}},
	{name: "cancellation-during-wait", class: ClassTemporal, dimension: "waits", guard: dimGuard("waits", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Waits }), build: func(ctx *context) (string, string, string, string) {
		waits := dimText(ctx.record.Waits)
		return baseFixture(ctx) + " cancel=during-" + waits, "cancellation aborts " + waits + " without effects",
			"orphaned wait past cancellation",
			"cancellation during " + waits + " aborts without effects"
	}},
	{name: "conflicting-future-change", class: ClassConcurrency, dimension: "effects", guard: eitherGuard(dimGuard("writes", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Writes }), effectsGuard), build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " future=conflicting", "conflicting future change detected before commit",
			"silent overwrite by a future change",
			"conflicting future change detected before commit of " + ctx.record.Completion
	}},
	{name: "concurrent-effect-idempotent", class: ClassConcurrency, dimension: "effects", guard: effectsGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " concurrent=" + csv(ctx.effects), "concurrent " + csv(ctx.effects) + " applies once",
			"concurrent double application",
			"concurrent " + csv(ctx.effects) + " applies exactly once"
	}},
	{name: "partial-snapshot-detected", class: ClassFailure, dimension: "reads", guard: alwaysGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " snapshot=partial", "partial " + ctx.record.SnapshotPolicy + " detected and refused",
			"partial snapshot honored",
			"partial " + ctx.record.SnapshotPolicy + " detected and refused before execution"
	}},
	{name: "worker-crash-recoverable", class: ClassFailure, dimension: "effects", guard: eitherGuard(dimGuard("writes", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Writes }), effectsGuard), build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " crash=mid-execution", "crash replays from the last observed edge",
			"unrecoverable crash past " + ctx.record.Completion,
			"worker crash replays from the last observed edge to " + ctx.record.Completion
	}},
	{name: "partial-external-success", class: ClassFailure, dimension: "effects", guard: effectsGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " external=partial", "partial success reconciled, never half-reported",
			"half-reported external success",
			"partial external success over " + csv(ctx.effects) + " reconciled, never half-reported"
	}},
	{name: "invalidator-supersedes", class: ClassFailure, dimension: "invalidators", guard: dimGuard("invalidators", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Invalidators }), build: func(ctx *context) (string, string, string, string) {
		invalidator := dimText(ctx.record.Invalidators)
		return baseFixture(ctx) + " invalidation=" + invalidator, invalidator + " supersedes stale work",
			"stale work surviving " + invalidator,
			invalidator + " supersedes stale work before " + ctx.record.Completion
	}},
	{name: "quarantined-ingress-corrected", class: ClassFailure, dimension: "invalidators", guard: dimGuard("invalidators", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Invalidators }), build: func(ctx *context) (string, string, string, string) {
		invalidator := dimText(ctx.record.Invalidators)
		return baseFixture(ctx) + " ingress=quarantined", "quarantined ingress corrected via " + invalidator,
			"quarantined ingress executed",
			"quarantined ingress corrected via " + invalidator + " with evidence"
	}},
	{name: "authority-drift-refused", class: ClassSecurity, dimension: "reads", guard: eitherGuard(dimGuard("human", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.HumanWork }), enginesGuard), build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " authority=drifted", "drifted authority refused before effects",
			"drifted authority honored",
			"drifted authority refused before effects by engines " + csv(ctx.engines)
	}},
	{name: "confidential-field-redacted", class: ClassSecurity, dimension: "reads", guard: eitherGuard(enginesGuard, dimGuard("human", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.HumanWork })), build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " field=confidential", "confidential fields redacted in reads and decisions",
			"confidential field leak into evidence",
			"confidential fields redacted in reads by " + csv(ctx.engines) + " and in decisions"
	}},
	{name: "cross-compartment-isolation", class: ClassSecurity, dimension: "reads", guard: alwaysGuard, build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " compartment=sibling", "profile " + ctx.record.DomainProfile + " isolated from sibling compartments",
			"cross-compartment read",
			"profile " + ctx.record.DomainProfile + " for " + ctx.record.Definition + " isolated from sibling compartments"
	}},
	{name: "correction-supersedes", class: ClassRepair, dimension: "invalidators", guard: dimGuard("correction", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Correction }), build: func(ctx *context) (string, string, string, string) {
		correction := dimText(ctx.record.Correction)
		return baseFixture(ctx) + " fault=correctable", correction + " supersedes the faulty revision",
			"faulty revision surviving correction",
			correction + " supersedes the faulty revision with evidence"
	}},
	{name: "failed-repair-escalates", class: ClassRepair, dimension: "invalidators", guard: eitherGuard(dimGuard("invalidators", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Invalidators }), dimGuard("correction", func(r workflowdesign.DesignRecord) workflowdesign.Dimension { return r.Correction })), build: func(ctx *context) (string, string, string, string) {
		return baseFixture(ctx) + " repair=failed", "failed repair escalates with evidence, never silent",
			"silent failed repair",
			"failed repair escalates with evidence for " + ctx.record.Intent
	}},
}

// TemplateCount pins the reviewed table size.
const TemplateCount = 22

func dimText(dimension workflowdesign.Dimension) string {
	if strings.TrimSpace(dimension.Value) != "" {
		return dimension.Value
	}
	return strings.Join(dimension.Items, ",")
}

// Generate compiles one record, graph and profile into a complete
// matrix. It shares no mutable state and is safe for concurrent use.
func Generate(record workflowdesign.DesignRecord, graph *workflowexpansion.Graph, profile workflowexpansion.Profile) (*Matrix, []Finding) {
	var findings []Finding
	add := func(code, field, detail string) {
		findings = append(findings, Finding{Code: code, Field: field, Detail: detail})
	}
	if graph == nil {
		add(MissingExpansion, "graph", "generation requires the expanded graph")
		return nil, findings
	}
	if graph.Intent != record.Intent {
		add(GraphMismatch, "graph", "expansion intent does not match the generated record")
		return nil, findings
	}
	if profile.Code != record.DomainProfile {
		add(ProfileMismatch, "domain_profile", fmt.Sprintf("profile %s does not cover record domain %s", profile.Code, record.DomainProfile))
		return nil, findings
	}
	ctx := &context{record: record, profile: profile, engines: engineNames(record), writes: writeNames(record)}
	for _, effect := range graph.Effects {
		ctx.effects = append(ctx.effects, effect.Description)
	}
	matrix := &Matrix{Definition: record.Definition, Intent: record.Intent}
	for i, tmpl := range templates {
		ok, reason := tmpl.guard(ctx)
		if !ok {
			matrix.NotApplicable = append(matrix.NotApplicable, NotApplicable{Class: tmpl.class, Dimension: tmpl.dimension, Name: tmpl.name, Reason: reason})
			continue
		}
		fixture, expected, prohibited, oracle := tmpl.build(ctx)
		matrix.Scenarios = append(matrix.Scenarios, Scenario{
			ID:         fmt.Sprintf("%s/%s/%02d", record.Definition, strings.ToLower(tmpl.class), i),
			Name:       tmpl.name,
			Class:      tmpl.class,
			Dimension:  tmpl.dimension,
			Fixture:    fixture,
			Expected:   expected,
			Prohibited: prohibited,
			Oracle:     oracle,
		})
	}
	matrix.Digest = digestMatrix(matrix)
	return matrix, findings
}

func engineNames(record workflowdesign.DesignRecord) []string {
	if record.Engines.Value == workflowdesign.NotApplicable {
		return nil
	}
	if strings.TrimSpace(record.Engines.Value) != "" {
		return []string{record.Engines.Value}
	}
	return append([]string(nil), record.Engines.Items...)
}

func writeNames(record workflowdesign.DesignRecord) []string {
	if record.Writes.Value == workflowdesign.NotApplicable {
		return nil
	}
	if strings.TrimSpace(record.Writes.Value) != "" {
		return []string{record.Writes.Value}
	}
	return append([]string(nil), record.Writes.Items...)
}

func digestMatrix(matrix *Matrix) string {
	var lines []string
	for _, scenario := range matrix.Scenarios {
		lines = append(lines, strings.Join([]string{scenario.ID, scenario.Name, scenario.Class, scenario.Dimension, scenario.Fixture, scenario.Expected, scenario.Prohibited, scenario.Oracle}, "\x00"))
	}
	for _, na := range matrix.NotApplicable {
		lines = append(lines, strings.Join([]string{na.Class, na.Dimension, na.Name, na.Reason}, "\x00"))
	}
	lines = append(lines, matrix.Definition, matrix.Intent)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
