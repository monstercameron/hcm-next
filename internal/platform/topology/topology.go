// Package topology models the provider-neutral first production-cell decision.
// It is deliberately a pure artifact: deployment adapters consume its
// digest-backed evidence but this package performs no provisioning.
package topology

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const schemaVersion = 1

// Version reports the topology decision schema version.
func Version() int { return schemaVersion }

// Explain describes the topology contract without selecting a real provider.
func Explain() string {
	return "TOPOLOGY-001 v1: digest-bound production cell, paths, budgets, failure modes, and recovery probes"
}

// Selection records the human-owned deployment choices. Placeholder is
// intentional for a decision that still needs a human selection.
type Selection struct {
	ProviderRef string `json:"provider_ref"`
	Region      string `json:"region"`
	OwnerRef    string `json:"owner_ref"`
	Placeholder bool   `json:"placeholder"`
}

// Process is one named process, queue, or timer in the cell.
type Process struct {
	ID               string   `json:"id"`
	Role             string   `json:"role"`
	DataDependencies []string `json:"data_dependencies"`
}

// Path is an exact network, data, or trust boundary.
type Path struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Plane    string `json:"plane"`
	Trust    string `json:"trust"`
	DataPath string `json:"data_path"`
}

// FailureMode states the expected degradation and repair behavior for one
// material component.
type FailureMode struct {
	ID          string `json:"id"`
	Component   string `json:"component"`
	Degradation string `json:"degradation"`
	Recovery    string `json:"recovery"`
}

// ResourceBudget is the capacity envelope for the pilot cell.
type ResourceBudget struct {
	MaxTenants        int `json:"max_tenants"`
	MaxConcurrentJobs int `json:"max_concurrent_jobs"`
	CPUUnits          int `json:"cpu_units"`
	MemoryMiB         int `json:"memory_mib"`
}

// RecoveryPlan names the backup, restore, drain, and replacement controls.
type RecoveryPlan struct {
	BackupRef        string `json:"backup_ref"`
	RestoreProcedure string `json:"restore_procedure"`
	DrainProcedure   string `json:"drain_procedure"`
	ReplacementPath  string `json:"replacement_path"`
}

// Probe is a sandbox verification probe. Kind must be HEALTH, LOAD, DRAIN, or
// RESTORE.
type Probe struct {
	Kind        string `json:"kind"`
	Expectation string `json:"expectation"`
	BudgetSecs  int    `json:"budget_secs"`
}

// Manifest is the frozen topology inventory.
type Manifest struct {
	SchemaVersion int            `json:"schema_version"`
	DecisionID    string         `json:"decision_id"`
	CellID        string         `json:"cell_id"`
	Environment   string         `json:"environment"`
	Selection     Selection      `json:"selection"`
	Zones         []string       `json:"zones"`
	Processes     []Process      `json:"processes"`
	Paths         []Path         `json:"paths"`
	FailureModes  []FailureMode  `json:"failure_modes"`
	Budget        ResourceBudget `json:"budget"`
	Recovery      RecoveryPlan   `json:"recovery"`
	Probes        []Probe        `json:"probes"`
}

// DeploymentManifest is the adapter-facing inventory that must agree with
// Manifest on all material topology facts.
type DeploymentManifest struct {
	SchemaVersion int            `json:"schema_version"`
	Inventory     []string       `json:"inventory"`
	Paths         []Path         `json:"paths"`
	Zones         []string       `json:"zones"`
	FailureModes  []FailureMode  `json:"failure_modes"`
	Budget        ResourceBudget `json:"budget"`
	Recovery      RecoveryPlan   `json:"recovery"`
}

// Decision contains the selected topology and the generated deploy inventory.
type Decision struct {
	Topology Manifest           `json:"topology"`
	Deploy   DeploymentManifest `json:"deploy"`
}

// Violation identifies one deterministic contract defect.
type Violation struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Evidence is the digest-backed result of comparing the frozen topology with
// the deployable inventory. HumanInputs is explicit when a placeholder remains.
type Evidence struct {
	DecisionID      string   `json:"decision_id"`
	TopologyDigest  string   `json:"topology_digest"`
	DeployDigest    string   `json:"deploy_digest"`
	InventoryDigest string   `json:"inventory_digest"`
	Ready           bool     `json:"ready"`
	Status          string   `json:"status"`
	Blockers        []string `json:"blockers"`
	HumanInputs     []string `json:"human_inputs"`
}

type materialInventory struct {
	Inventory    []string       `json:"inventory"`
	Paths        []Path         `json:"paths"`
	Zones        []string       `json:"zones"`
	FailureModes []FailureMode  `json:"failure_modes"`
	Budget       ResourceBudget `json:"budget"`
	Recovery     RecoveryPlan   `json:"recovery"`
}

// Validate returns structural defects. Explicit placeholders are accepted as
// a captured but not-yet-selected human decision; Compile marks the evidence
// pending until those values are supplied.
func Validate(m Manifest) []Violation {
	var out []Violation
	add := func(field, code, detail string) {
		out = append(out, Violation{Field: field, Code: code, Detail: detail})
	}
	if m.SchemaVersion != schemaVersion {
		add("schema_version", "UNSUPPORTED_SCHEMA", "schema version must be 1")
	}
	if strings.TrimSpace(m.DecisionID) == "" {
		add("decision_id", "MISSING_ID", "decision id is required")
	}
	if strings.TrimSpace(m.CellID) == "" {
		add("cell_id", "MISSING_CELL", "cell id is required")
	}
	if strings.TrimSpace(m.Environment) == "" {
		add("environment", "MISSING_ENVIRONMENT", "environment is required")
	}
	for field, value := range map[string]string{"provider_ref": m.Selection.ProviderRef, "region": m.Selection.Region, "owner_ref": m.Selection.OwnerRef} {
		if strings.TrimSpace(value) == "" {
			add("selection."+field, "MISSING_SELECTION", "selection value is required")
		}
		if strings.ContainsAny(value, "*?") {
			add("selection."+field, "ABSTRACT_SELECTION", "selection cannot contain wildcard values")
		}
		if m.Selection.Placeholder && !strings.HasPrefix(strings.ToLower(value), "placeholder:") {
			add("selection."+field, "PLACEHOLDER_NOT_LABELLED", "placeholder selections must use the placeholder: prefix")
		}
	}
	if len(m.Zones) < 2 {
		add("zones", "INSUFFICIENT_FAILURE_DOMAINS", "at least two zones are required")
	}
	if duplicateStrings(m.Zones) {
		add("zones", "DUPLICATE_FAILURE_DOMAIN", "zones must be unique")
	}
	if len(m.Processes) == 0 {
		add("processes", "MISSING_PROCESS", "at least one process is required")
	}
	seen := map[string]bool{}
	for i, p := range m.Processes {
		if strings.TrimSpace(p.ID) == "" {
			add(fmt.Sprintf("processes[%d].id", i), "MISSING_PROCESS_ID", "process id is required")
		}
		if seen[p.ID] {
			add("processes["+p.ID+"]", "DUPLICATE_PROCESS", "process ids must be unique")
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Role) == "" {
			add(fmt.Sprintf("processes[%d].role", i), "MISSING_PROCESS_ROLE", "process role is required")
		}
	}
	if len(m.Paths) == 0 {
		add("paths", "MISSING_BOUNDARY", "at least one exact boundary path is required")
	}
	for i, p := range m.Paths {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.From) == "" || strings.TrimSpace(p.To) == "" || strings.TrimSpace(p.Plane) == "" || strings.TrimSpace(p.Trust) == "" || strings.TrimSpace(p.DataPath) == "" {
			add(fmt.Sprintf("paths[%d]", i), "INCOMPLETE_BOUNDARY", "path requires id, endpoints, plane, trust, and data path")
		}
		if strings.ContainsAny(p.From+p.To+p.Plane, "*?") {
			add(fmt.Sprintf("paths[%d]", i), "WILDCARD_BOUNDARY", "boundary endpoints cannot be wildcard")
		}
	}
	if len(m.FailureModes) == 0 {
		add("failure_modes", "MISSING_FAILURE_MODE", "failure behavior is required")
	}
	for i, f := range m.FailureModes {
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Component) == "" || strings.TrimSpace(f.Degradation) == "" || strings.TrimSpace(f.Recovery) == "" {
			add(fmt.Sprintf("failure_modes[%d]", i), "INCOMPLETE_FAILURE_MODE", "failure mode requires component, degradation, and recovery")
		}
	}
	if m.Budget.MaxTenants <= 0 || m.Budget.MaxConcurrentJobs <= 0 || m.Budget.CPUUnits <= 0 || m.Budget.MemoryMiB <= 0 {
		add("budget", "MISSING_CAPACITY_BUDGET", "all positive capacity limits are required")
	}
	if strings.TrimSpace(m.Recovery.BackupRef) == "" || strings.TrimSpace(m.Recovery.RestoreProcedure) == "" || strings.TrimSpace(m.Recovery.DrainProcedure) == "" || strings.TrimSpace(m.Recovery.ReplacementPath) == "" {
		add("recovery", "INCOMPLETE_RECOVERY", "backup, restore, drain, and replacement controls are required")
	}
	probeKinds := map[string]bool{}
	for i, p := range m.Probes {
		kind := strings.ToUpper(strings.TrimSpace(p.Kind))
		if !map[string]bool{"HEALTH": true, "LOAD": true, "DRAIN": true, "RESTORE": true}[kind] || strings.TrimSpace(p.Expectation) == "" || p.BudgetSecs <= 0 {
			add(fmt.Sprintf("probes[%d]", i), "INVALID_PROBE", "probe requires a known kind, expectation, and positive budget")
		}
		probeKinds[kind] = true
	}
	for _, kind := range []string{"HEALTH", "LOAD", "DRAIN", "RESTORE"} {
		if !probeKinds[kind] {
			add("probes", "MISSING_PROBE", "missing "+kind+" sandbox probe")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Check returns the first validation defect.
func Check(m Manifest) error {
	if v := Validate(m); len(v) > 0 {
		return fmt.Errorf("topology: %s %s: %s", v[0].Code, v[0].Field, v[0].Detail)
	}
	return nil
}

func normalizeManifest(m Manifest) Manifest {
	m.Zones = append([]string(nil), m.Zones...)
	sort.Strings(m.Zones)
	m.Processes = append([]Process(nil), m.Processes...)
	sort.Slice(m.Processes, func(i, j int) bool { return m.Processes[i].ID < m.Processes[j].ID })
	m.Paths = append([]Path(nil), m.Paths...)
	sort.Slice(m.Paths, func(i, j int) bool { return m.Paths[i].ID < m.Paths[j].ID })
	m.FailureModes = append([]FailureMode(nil), m.FailureModes...)
	sort.Slice(m.FailureModes, func(i, j int) bool { return m.FailureModes[i].ID < m.FailureModes[j].ID })
	m.Probes = append([]Probe(nil), m.Probes...)
	sort.Slice(m.Probes, func(i, j int) bool { return m.Probes[i].Kind < m.Probes[j].Kind })
	for i := range m.Processes {
		m.Processes[i].DataDependencies = append([]string(nil), m.Processes[i].DataDependencies...)
		sort.Strings(m.Processes[i].DataDependencies)
	}
	return m
}

func canonical(value any) ([]byte, error) {
	data, err := json.Marshal(normalize(value))
	if err != nil {
		return nil, err
	}
	return data, nil
}
func normalize(value any) any {
	if m, ok := value.(Manifest); ok {
		return normalizeManifest(m)
	}
	if d, ok := value.(DeploymentManifest); ok {
		d.Zones = append([]string(nil), d.Zones...)
		sort.Strings(d.Zones)
		d.Paths = append([]Path(nil), d.Paths...)
		sort.Slice(d.Paths, func(i, j int) bool { return d.Paths[i].ID < d.Paths[j].ID })
		d.FailureModes = append([]FailureMode(nil), d.FailureModes...)
		sort.Slice(d.FailureModes, func(i, j int) bool { return d.FailureModes[i].ID < d.FailureModes[j].ID })
		d.Inventory = append([]string(nil), d.Inventory...)
		sort.Strings(d.Inventory)
		return d
	}
	if inventory, ok := value.(materialInventory); ok {
		inventory.Inventory = append([]string(nil), inventory.Inventory...)
		sort.Strings(inventory.Inventory)
		inventory.Zones = append([]string(nil), inventory.Zones...)
		sort.Strings(inventory.Zones)
		inventory.Paths = append([]Path(nil), inventory.Paths...)
		sort.Slice(inventory.Paths, func(i, j int) bool { return inventory.Paths[i].ID < inventory.Paths[j].ID })
		inventory.FailureModes = append([]FailureMode(nil), inventory.FailureModes...)
		sort.Slice(inventory.FailureModes, func(i, j int) bool { return inventory.FailureModes[i].ID < inventory.FailureModes[j].ID })
		return inventory
	}
	return value
}
func digest(value any) (string, error) {
	data, err := canonical(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Digest returns the canonical identity of a valid topology manifest.
func Digest(m Manifest) (string, error) {
	if err := Check(m); err != nil {
		return "", err
	}
	return digest(m)
}

// Compile compares the frozen decision and deploy inventory and emits the
// evidence needed by deployment admission.
func Compile(d Decision) (Evidence, error) {
	if err := Check(d.Topology); err != nil {
		return Evidence{}, err
	}
	topologyDigest, err := Digest(d.Topology)
	if err != nil {
		return Evidence{}, err
	}
	deployDigest, err := digest(d.Deploy)
	if err != nil {
		return Evidence{}, err
	}
	if d.Deploy.SchemaVersion != schemaVersion {
		return Evidence{}, fmt.Errorf("topology: deploy schema version must be 1")
	}
	material := materialInventory{Inventory: topologyInventory(d.Topology), Paths: d.Topology.Paths, Zones: d.Topology.Zones, FailureModes: d.Topology.FailureModes, Budget: d.Topology.Budget, Recovery: d.Topology.Recovery}
	inv, err := digest(material)
	if err != nil {
		return Evidence{}, err
	}
	expected := materialInventory{Inventory: d.Deploy.Inventory, Paths: d.Deploy.Paths, Zones: d.Deploy.Zones, FailureModes: d.Deploy.FailureModes, Budget: d.Deploy.Budget, Recovery: d.Deploy.Recovery}
	expectedInv, err := digest(expected)
	if err != nil {
		return Evidence{}, err
	}
	e := Evidence{DecisionID: d.Topology.DecisionID, TopologyDigest: topologyDigest, DeployDigest: deployDigest, InventoryDigest: inv, Status: "READY", Ready: inv == expectedInv}
	if inv != expectedInv {
		e.Status = "TOPOLOGY_DEPLOY_MISMATCH"
		e.Blockers = []string{"topology and deploy boundary inventory differ"}
	}
	if inputs := placeholderInputs(d.Topology); len(inputs) > 0 {
		e.Ready = false
		e.Status = "HUMAN_SELECTION_REQUIRED"
		e.HumanInputs = inputs
		e.Blockers = append(e.Blockers, "one or more topology values are explicit placeholders")
	}
	return e, nil
}

func duplicateStrings(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func topologyInventory(m Manifest) []string {
	values := make([]string, 0, len(m.Processes))
	for _, process := range m.Processes {
		values = append(values, process.ID)
		values = append(values, process.DataDependencies...)
	}
	return values
}

func placeholderInputs(m Manifest) []string {
	var inputs []string
	if m.Selection.Placeholder || strings.HasPrefix(strings.ToLower(m.Selection.ProviderRef), "placeholder:") {
		inputs = append(inputs, "selection.provider_ref")
	}
	if m.Selection.Placeholder || strings.HasPrefix(strings.ToLower(m.Selection.Region), "placeholder:") {
		inputs = append(inputs, "selection.region")
	}
	if m.Selection.Placeholder || strings.HasPrefix(strings.ToLower(m.Selection.OwnerRef), "placeholder:") {
		inputs = append(inputs, "selection.owner_ref")
	}
	for field, value := range map[string]string{"decision_id": m.DecisionID, "cell_id": m.CellID, "recovery.backup_ref": m.Recovery.BackupRef, "recovery.restore_procedure": m.Recovery.RestoreProcedure, "recovery.drain_procedure": m.Recovery.DrainProcedure, "recovery.replacement_path": m.Recovery.ReplacementPath} {
		if strings.HasPrefix(strings.ToLower(value), "placeholder:") && !containsString(inputs, field) {
			inputs = append(inputs, field)
		}
	}
	sort.Strings(inputs)
	return inputs
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
