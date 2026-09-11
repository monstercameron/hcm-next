// Package directcapability proves direct-capability dispositions stay
// pure, governed and action-promoting (WF-DISC-008): a DIRECT design
// must hold no durable mechanism and no state mutation, a DIRECT
// execution must pin purpose, fields, population, rules, watermarks and
// DLP policy, return typed completeness with uncertainty and trace,
// record evidence, and emit proposed actions only as new unexecuted
// intents. It shares the capability gateway and governance contracts
// with workflow steps by import; only durability mechanics differ. It is
// kernel-pure: pure snapshot proof plus text emission, no database,
// network or mutable global state.
package directcapability

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

// Proof finding codes.
const (
	DurableMechanism          = "DURABLE_MECHANISM"
	StateMutation             = "STATE_MUTATION"
	WrongDisposition          = "WRONG_DISPOSITION"
	MissingExpansion          = "MISSING_EXPANSION"
	GraphMismatch             = "GRAPH_MISMATCH"
	GraphInvalid              = "GRAPH_INVALID"
	MissingPurpose            = "MISSING_PURPOSE"
	MissingFieldScope         = "MISSING_FIELD_SCOPE"
	MissingPopulationScope    = "MISSING_POPULATION_SCOPE"
	MissingRulePin            = "MISSING_RULE_PIN"
	MissingWatermark          = "MISSING_WATERMARK"
	MissingDlpPolicy          = "MISSING_DLP_POLICY"
	MissingCompleteness       = "MISSING_COMPLETENESS"
	MissingUncertainty        = "MISSING_UNCERTAINTY"
	MissingTrace              = "MISSING_TRACE"
	MissingEvidence           = "MISSING_EVIDENCE"
	ContradictoryCompleteness = "CONTRADICTORY_COMPLETENESS"
	ExecutedProposal          = "EXECUTED_PROPOSAL"
	UngovernedAction          = "UNGOVERNED_ACTION"
)

// Completeness states for a direct result.
const (
	CompletenessComplete = "COMPLETE"
	CompletenessPartial  = "PARTIAL"
	CompletenessUnknown  = "UNKNOWN"
)

// durableRe marks graph responsibilities that invent a durable workflow:
// waits, timers, deadlines, queues, human mechanisms and persisted rows.
var durableRe = regexp.MustCompile(`(?i)\b(wait|timer|deadline|queue|lease|claim|human|sleep|retry|row|table|ledger|outbox)\b`)

// Authorization is one direct execution's pinned governance context.
type Authorization struct {
	Purpose    string
	Fields     []string
	Population []string
	Rules      string
	Watermark  string
	DlpPolicy  string
}

// DirectResult is one direct execution's typed outcome.
type DirectResult struct {
	Completeness string
	Uncertainty  string
	Trace        string
	Evidence     string
	Exclusions   []string
}

// ActionProposal is one proposed follow-up action.
type ActionProposal struct {
	IntentRef string
	Governed  bool
	Executed  bool
	Summary   string
}

// Execution is one direct-capability execution to prove.
type Execution struct {
	Record  workflowdesign.DesignRecord
	Graph   *workflowexpansion.Graph
	Auth    Authorization
	Result  DirectResult
	Actions []ActionProposal
}

// Finding is one exact proof diagnostic.
type Finding struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// MarshalFindings renders findings as canonical JSON.
func MarshalFindings(findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Code != ordered[j].Code {
			return ordered[i].Code < ordered[j].Code
		}
		return ordered[i].Detail < ordered[j].Detail
	})
	rendered, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

// ProveDesign proves a DIRECT design holds no durable mechanism and no
// state mutation, from its record dimensions and its expanded graph.
func ProveDesign(record workflowdesign.DesignRecord, graph *workflowexpansion.Graph) []Finding {
	var findings []Finding
	add := func(code, field, detail string) {
		findings = append(findings, Finding{Code: code, Field: field, Detail: detail})
	}
	if record.Disposition != workflowdesign.DispositionDirect {
		add(WrongDisposition, "disposition", "direct proof requires a DIRECT disposition")
		return findings
	}
	if graph == nil {
		add(MissingExpansion, "graph", "direct proof requires the expanded graph")
		return findings
	}
	if graph.Intent != record.Intent {
		add(GraphMismatch, "graph", "expansion intent does not match the proven record")
		return findings
	}
	if applicable(record.Waits) {
		add(DurableMechanism, "waits", "DIRECT record declares waits; durable waiting needs a workflow")
	}
	if applicable(record.HumanWork) {
		add(DurableMechanism, "human_work", "DIRECT record declares human work; durable claims need a workflow")
	}
	if applicable(record.Writes) {
		add(StateMutation, "writes", "DIRECT record declares writes; state mutation needs a separately governed intent")
	}
	if len(graph.Effects) > 0 {
		add(StateMutation, "effects", "DIRECT expansion holds ordered effects; pure reads and calculations emit none")
	}
	for _, node := range graph.Nodes {
		if durableRe.MatchString(node.Responsibility) {
			add(DurableMechanism, "graph", "node invents durable machinery: "+node.Responsibility)
		}
	}
	if validate := graph.Validate(); len(validate) > 0 {
		add(GraphInvalid, "graph", "expansion fails graph validation")
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
	return findings
}

// ProveExecution proves a full DIRECT execution: design purity plus
// pinned authorization, honest typed results, recorded evidence and
// action promotion as new unexecuted intents.
func ProveExecution(exec Execution) []Finding {
	findings := ProveDesign(exec.Record, exec.Graph)
	add := func(code, field, detail string) {
		findings = append(findings, Finding{Code: code, Field: field, Detail: detail})
	}
	auth := exec.Auth
	if strings.TrimSpace(auth.Purpose) == "" {
		add(MissingPurpose, "auth.purpose", "direct execution states no authorized purpose")
	}
	if len(nonBlank(auth.Fields)) == 0 {
		add(MissingFieldScope, "auth.fields", "direct execution authorizes no fields")
	}
	if len(nonBlank(auth.Population)) == 0 {
		add(MissingPopulationScope, "auth.population", "direct execution authorizes no population")
	}
	if strings.TrimSpace(auth.Rules) == "" {
		add(MissingRulePin, "auth.rules", "direct execution pins no rules")
	}
	if strings.TrimSpace(auth.Watermark) == "" {
		add(MissingWatermark, "auth.watermark", "direct execution pins no watermark")
	}
	if strings.TrimSpace(auth.DlpPolicy) == "" {
		add(MissingDlpPolicy, "auth.dlp_policy", "direct execution applies no DLP policy")
	}
	result := exec.Result
	if strings.TrimSpace(result.Completeness) == "" {
		add(MissingCompleteness, "result.completeness", "direct execution returns no typed completeness")
	}
	if strings.TrimSpace(result.Uncertainty) == "" {
		add(MissingUncertainty, "result.uncertainty", "direct execution returns no uncertainty")
	}
	if strings.TrimSpace(result.Trace) == "" {
		add(MissingTrace, "result.trace", "direct execution returns no trace")
	}
	if strings.TrimSpace(result.Evidence) == "" {
		add(MissingEvidence, "result.evidence", "direct execution records no evidence")
	}
	if result.Completeness == CompletenessComplete && len(nonBlank(result.Exclusions)) > 0 {
		add(ContradictoryCompleteness, "result.completeness", "COMPLETE result with exclusions reports incomplete data as complete")
	}
	for _, action := range exec.Actions {
		field := "actions"
		if strings.TrimSpace(action.IntentRef) == "" || !action.Governed {
			add(UngovernedAction, field, "proposed action without a separately governed intent mutates outside governance")
			continue
		}
		if action.Executed {
			add(ExecutedProposal, field, "proposed action "+action.IntentRef+" already executed; proposals stay unexecuted intents")
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
	return findings
}

// applicable reports whether an execution dimension declares a real
// mechanism rather than reasoned NOT_APPLICABLE.
func applicable(dimension workflowdesign.Dimension) bool {
	if dimension.Value == workflowdesign.NotApplicable {
		return false
	}
	if strings.TrimSpace(dimension.Value) != "" {
		return true
	}
	return len(nonBlank(dimension.Items)) > 0
}

func nonBlank(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
