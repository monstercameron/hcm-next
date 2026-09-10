// Package designclosure compiles the machine-readable design-closure
// register (CLOSE-001): one row per accepted scope item joining source,
// owner, phase, decision state, artifact, todos, tests, evidence digest
// and expiry, with exact findings for every missing dimension and totals
// that can never silently drop an item. It is kernel-pure: pure snapshot
// compilation plus text emission, no database, network or mutable global
// state. Live repository loading lives in loader.go; identities (accepted
// intents, todos, test names, workflows) are shared by import from the
// owning governors, never re-derived here.
package designclosure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DecisionState is one design-closure disposition for a scope item.
type DecisionState string

// Closure dispositions, in ladder order.
const (
	StateUnselected  DecisionState = "UNSELECTED"
	StateUnbound     DecisionState = "UNBOUND"
	StateDesigned    DecisionState = "DESIGNED"
	StateContracted  DecisionState = "CONTRACTED"
	StateImplemented DecisionState = "IMPLEMENTED"
	StateVerified    DecisionState = "VERIFIED"
	StateDeferred    DecisionState = "DEFERRED"
	StateRejected    DecisionState = "REJECTED"
)

// validDecision reports whether a decision state belongs to the register.
func validDecision(state DecisionState) bool {
	switch state {
	case StateUnselected, StateUnbound, StateDesigned, StateContracted,
		StateImplemented, StateVerified, StateDeferred, StateRejected:
		return true
	}
	return false
}

// Finding codes. MISSING_* mark required RED dimensions with no exact
// value; TICK_WITHOUT_EVIDENCE marks prose-complete ticks that cannot
// satisfy an implementation gate; INCOMPLETE_COVERAGE, UNKNOWN_ITEM and
// DUPLICATE_ITEM guard register totality.
const (
	MissingSource       = "MISSING_SOURCE"
	MissingOwner        = "MISSING_OWNER"
	MissingPhase        = "MISSING_PHASE"
	MissingArtifact     = "MISSING_ARTIFACT"
	MissingTest         = "MISSING_TEST"
	MissingEvidence     = "MISSING_EVIDENCE"
	MissingExpiry       = "MISSING_EXPIRY"
	TickWithoutEvidence = "TICK_WITHOUT_EVIDENCE"
	IncompleteCoverage  = "INCOMPLETE_COVERAGE"
	UnknownItem         = "UNKNOWN_ITEM"
	DuplicateItem       = "DUPLICATE_ITEM"
)

// ItemInput is one accepted scope item: an accepted BusinessIntent
// contract with its exact source, owning family and phase.
type ItemInput struct {
	ID              string
	Source          string
	Owner           string
	Phase           string
	ConformanceOnly bool
}

// TodoInput is one backlog todo bound to scope items by DIRECT reference.
type TodoInput struct {
	ID             string
	Phase          string
	Done           bool
	Retired        bool
	Direct         []string
	TestNames      []string
	EvidenceTests  []string
	EvidenceDigest string
}

// Deferral records an explicit, expiring deferment rationale.
type Deferral struct {
	Rationale string
	Expiry    string
}

// Snapshot is the complete typed input the register compiles. States are
// always computed, never supplied: a forged decision cannot enter because
// there is no field that carries one.
type Snapshot struct {
	Items      []ItemInput
	Todos      []TodoInput
	Workflows  map[string][]string
	OpenGaps   map[string][]string
	Deferrals  map[string]Deferral
	Rejections map[string]string
	Selections map[string]string
	TestExists map[string]bool
}

// Blocker names one unresolved item that keeps a row below VERIFIED, with
// the gate it blocks: todo blockers carry their todo's phase, every other
// blocker carries the item's phase, so an item blocks only its declared
// gate.
type Blocker struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Detail string `json:"detail"`
	Gate   string `json:"gate"`
}

// Row is one closed scope item.
type Row struct {
	Item            string        `json:"item"`
	Source          string        `json:"source"`
	Owner           string        `json:"owner"`
	Phase           string        `json:"phase"`
	ConformanceOnly bool          `json:"conformance_only,omitempty"`
	Decision        DecisionState `json:"decision"`
	Artifact        []string      `json:"artifact"`
	Todos           []string      `json:"todos"`
	Tests           []string      `json:"tests"`
	EvidenceDigest  string        `json:"evidence_digest"`
	Expiry          string        `json:"expiry,omitempty"`
	Blockers        []Blocker     `json:"blockers"`
	Gate            string        `json:"gate"`
}

// Totals reconcile the register: every row is counted under exactly one
// decision, and the digest binds every row, so UNKNOWN, DEFERRED, waived
// or rejected work can never disappear from the totals.
type Totals struct {
	Rows       int                   `json:"rows"`
	ByState    map[DecisionState]int `json:"by_state"`
	Unresolved int                   `json:"unresolved"`
	Digest     string                `json:"digest"`
}

// Register is the compiled design-closure output.
type Register struct {
	Rows   []Row  `json:"rows"`
	Totals Totals `json:"totals"`
}

// Finding is one source-exact closure diagnostic located by item.
type Finding struct {
	Item   string `json:"item"`
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// Report carries a register with the findings its compilation raised.
type Report struct {
	Register Register  `json:"register"`
	Findings []Finding `json:"findings,omitempty"`
}

// MarshalReport renders a register with its findings as canonical JSON.
func MarshalReport(register Register, findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Item != ordered[j].Item {
			return ordered[i].Item < ordered[j].Item
		}
		if ordered[i].Code != ordered[j].Code {
			return ordered[i].Code < ordered[j].Code
		}
		return ordered[i].Detail < ordered[j].Detail
	})
	rendered, err := json.MarshalIndent(Report{Register: register, Findings: ordered}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

// Compile builds the closure register for accepted items from a snapshot.
// Findings never fail compilation: unresolved items are named rows with
// blockers, and totals always reconcile.
func Compile(accepted []string, snap Snapshot) (Register, []Finding) {
	var findings []Finding
	acceptedSet := make(map[string]bool, len(accepted))
	for _, id := range accepted {
		acceptedSet[id] = true
	}
	bound := make(map[string][]TodoInput)
	for _, todo := range snap.Todos {
		for _, direct := range todo.Direct {
			bound[direct] = append(bound[direct], todo)
		}
	}
	seen := make(map[string]bool)
	var rows []Row
	for _, item := range snap.Items {
		if seen[item.ID] {
			findings = append(findings, Finding{Item: item.ID, Code: DuplicateItem, Detail: "scope item declared twice; only the first declaration registers"})
			continue
		}
		seen[item.ID] = true
		row, rowFindings := compileRow(item, bound[item.ID], snap)
		rows = append(rows, row)
		findings = append(findings, rowFindings...)
		if !acceptedSet[item.ID] {
			findings = append(findings, Finding{Item: item.ID, Code: UnknownItem, Detail: "row is not an accepted scope item; it stays counted in totals, never silently dropped"})
		}
	}
	for _, id := range accepted {
		if !seen[id] {
			findings = append(findings, Finding{Item: id, Code: IncompleteCoverage, Detail: "accepted scope item has no closure row"})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Item < rows[j].Item })
	register := Register{Rows: rows}
	register.Totals = summarize(rows)
	return register, findings
}

func compileRow(item ItemInput, todos []TodoInput, snap Snapshot) (Row, []Finding) {
	var findings []Finding
	add := func(code, field, detail string) {
		findings = append(findings, Finding{Item: item.ID, Code: code, Field: field, Detail: detail})
	}
	if strings.TrimSpace(item.Source) == "" {
		add(MissingSource, "source", "scope item names no exact source")
	}
	if strings.TrimSpace(item.Owner) == "" {
		add(MissingOwner, "owner", "scope item names no exact owner")
	}
	if strings.TrimSpace(item.Phase) == "" {
		add(MissingPhase, "phase", "scope item names no exact phase")
	}
	row := Row{
		Item:            item.ID,
		Source:          item.Source,
		Owner:           item.Owner,
		Phase:           item.Phase,
		ConformanceOnly: item.ConformanceOnly,
		Gate:            item.Phase,
	}
	var active []TodoInput
	for _, todo := range todos {
		if !todo.Retired {
			active = append(active, todo)
		}
	}
	workflows := append([]string(nil), snap.Workflows[item.ID]...)
	sort.Strings(workflows)
	row.Artifact = workflows
	gaps := append([]string(nil), snap.OpenGaps[item.ID]...)
	sort.Strings(gaps)

	addBlocker := func(kind, ref, detail, gate string) {
		if gate == "" {
			gate = item.Phase
		}
		row.Blockers = append(row.Blockers, Blocker{Kind: kind, Ref: ref, Detail: detail, Gate: gate})
	}

	switch rationale, rejected := snap.Rejections[item.ID]; {
	case rejected:
		row.Decision = StateRejected
		addBlocker("rejection", rationale, "scope item rejected", "")
	case snap.Deferrals[item.ID].Rationale != "" || snap.Deferrals[item.ID].Expiry != "":
		deferral := snap.Deferrals[item.ID]
		row.Decision = StateDeferred
		row.Expiry = deferral.Expiry
		if strings.TrimSpace(deferral.Expiry) == "" {
			add(MissingExpiry, "expiry", "deferment without an expiry hides work; record when it reopens")
		}
		addBlocker("deferral", deferral.Rationale, "scope item deferred", "")
	case len(active) == 0:
		if len(workflows)+len(gaps) > 0 || snap.Selections[item.ID] != "" {
			row.Decision = StateUnbound
			addBlocker("delivery", "no active DIRECT todos", "scope item referenced but bound to no delivery todo", "")
		} else {
			row.Decision = StateUnselected
			addBlocker("selection", "no selection, delivery, workflow or gap reference", "nothing selected this scope item yet", "")
		}
	default:
		var done, open []TodoInput
		for _, todo := range active {
			if todo.Done {
				done = append(done, todo)
			} else {
				open = append(open, todo)
			}
		}
		var todoIDs []string
		for _, todo := range active {
			todoIDs = append(todoIDs, todo.ID)
		}
		sort.Strings(todoIDs)
		row.Todos = todoIDs
		evidence, dangling, bare := evidenceSets(active, snap.TestExists)
		for _, name := range activeTestNames(active, snap.TestExists) {
			if !containsString(evidence, name) {
				evidence = append(evidence, name)
			}
		}
		sort.Strings(evidence)
		row.Tests = evidence
		if len(evidence) == 0 && len(dangling) == 0 {
			add(MissingTest, "test", "bound work names no existing test")
		}
		if len(dangling) > 0 {
			for _, test := range dangling {
				add(MissingTest, "test", fmt.Sprintf("evidence names %s, which exists in no scanned test source", test))
			}
		}
		if len(bare) > 0 {
			for _, id := range bare {
				add(TickWithoutEvidence, "evidence", fmt.Sprintf("ticked todo %s names no evidence test; prose completion cannot satisfy an implementation gate", id))
			}
		}
		digest := digestEvidence(active)
		row.EvidenceDigest = digest
		for _, todo := range open {
			addBlocker("todo", todo.ID, "bound todo still open", todo.Phase)
		}
		for _, gap := range gaps {
			addBlocker("gap", gap, "open capability gap", "")
		}
		switch {
		case len(bare) > 0:
			// Prose-complete ticks cap the ladder: without an evidence
			// test the row can never prove implementation.
			row.Decision = StateContracted
		case len(open) > 0:
			if len(done) > 0 {
				row.Decision = StateContracted
			} else {
				row.Decision = StateDesigned
			}
		default:
			row.Decision = StateImplemented
		}
		if row.Decision == StateImplemented && len(dangling) == 0 && len(evidence) > 0 && digest != "" {
			row.Decision = StateVerified
		}
		if row.Decision == StateImplemented {
			for _, test := range dangling {
				addBlocker("test", test, "evidence test does not exist", "")
			}
			if digest == "" {
				add(MissingEvidence, "evidence", "committed work with no evidence digest proves nothing")
			}
			if len(evidence) == 0 && len(dangling) == 0 {
				add(MissingTest, "test", "committed work with no existing test proves nothing")
				addBlocker("test", "no existing tests", "no evidence test exists yet", "")
			}
		}
		if (row.Decision == StateDesigned || row.Decision == StateContracted) && digest == "" {
			add(MissingEvidence, "evidence", "bound work with no evidence digest proves nothing yet")
		}
		if row.Decision != StateUnselected && row.Decision != StateUnbound && len(workflows) == 0 {
			add(MissingArtifact, "artifact", "bound scope item with no workflow artifact")
		}
	}
	sort.Slice(row.Blockers, func(i, j int) bool {
		if row.Blockers[i].Kind != row.Blockers[j].Kind {
			return row.Blockers[i].Kind < row.Blockers[j].Kind
		}
		return row.Blockers[i].Ref < row.Blockers[j].Ref
	})
	return row, findings
}

// evidenceSets splits bound todos' evidence tests into existing tests,
// dangling names and ticked todos with no evidence test at all.
func evidenceSets(active []TodoInput, exists map[string]bool) (evidence, dangling, bare []string) {
	seenEvidence := make(map[string]bool)
	seenDangling := make(map[string]bool)
	for _, todo := range active {
		if todo.Done && len(todo.EvidenceTests) == 0 {
			bare = append(bare, todo.ID)
		}
		for _, test := range todo.EvidenceTests {
			if exists[test] {
				if !seenEvidence[test] {
					seenEvidence[test] = true
					evidence = append(evidence, test)
				}
				continue
			}
			if !seenDangling[test] {
				seenDangling[test] = true
				dangling = append(dangling, test)
			}
		}
	}
	sort.Strings(evidence)
	sort.Strings(dangling)
	sort.Strings(bare)
	return evidence, dangling, bare
}

// activeTestNames returns the sorted TEST and TEST MATRIX names bound
// todos declare that exist in a scanned test source.
func activeTestNames(active []TodoInput, exists map[string]bool) []string {
	var names []string
	seen := make(map[string]bool)
	for _, todo := range active {
		for _, name := range todo.TestNames {
			if exists[name] && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// digestEvidence binds the sorted per-todo evidence digests. Empty when no
// bound todo carries evidence.
func digestEvidence(active []TodoInput) string {
	var parts []string
	for _, todo := range active {
		if strings.TrimSpace(todo.EvidenceDigest) != "" {
			parts = append(parts, todo.ID+"\x00"+todo.EvidenceDigest)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func summarize(rows []Row) Totals {
	totals := Totals{ByState: make(map[DecisionState]int)}
	var lines []string
	for _, row := range rows {
		totals.ByState[row.Decision]++
		if row.Decision != StateVerified {
			totals.Unresolved++
		}
		lines = append(lines, strings.Join([]string{
			row.Item, row.Source, row.Owner, row.Phase, string(row.Decision),
			strings.Join(row.Artifact, ","),
			strings.Join(row.Todos, ","),
			strings.Join(row.Tests, ","),
			row.EvidenceDigest, row.Expiry, row.Gate,
		}, "\x00"))
	}
	totals.Rows = len(rows)
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\x01")))
	totals.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return totals
}
