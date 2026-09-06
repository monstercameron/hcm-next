// Package performance owns the planning contracts for workload envelopes and
// unit-cost/capacity forecasts. The records are evidence inputs, not runtime
// admission configuration and never create billing or operational effects.
package performance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	// SchemaVersion is the version of the PERF-ENV-001 and PERF-007 records.
	SchemaVersion = 1

	SmallTier  = "SMALL"
	MediumTier = "MEDIUM"
	LargeTier  = "LARGE"
	PeakTier   = "PEAK"

	BudgetWithin      = "WITHIN_BUDGET"
	BudgetAlertStatus = "BUDGET_ALERT"
)

var (
	ErrInvalidEnvelope = errors.New("performance: invalid workload envelope")
	ErrInvalidForecast = errors.New("performance: invalid forecast")
)

// Limit is one declared resource or correctness budget. A zero value is
// intentionally invalid for required dimensions.
type Limit struct {
	Value int64  `json:"value"`
	Unit  string `json:"unit"`
}

// WorkloadEnvelope is the versioned, customer-independent shape of one
// workload scenario. PLACEHOLDER values belong in fixtures until a human
// supplies the design-partner workload and tier assumptions.
type WorkloadEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	TenantCount   Limit  `json:"tenant_count"`
	Tier          string `json:"tier"`

	ConcurrentUsers    Limit `json:"concurrent_users"`
	ConcurrentCommands Limit `json:"concurrent_commands"`
	CommandsPerMinute  Limit `json:"commands_per_minute"`
	FanoutPerCommand   Limit `json:"fanout_per_command"`
	PayloadBytes       Limit `json:"payload_bytes"`
	ObjectBytes        Limit `json:"object_bytes"`
	IntegrationOps     Limit `json:"integration_ops"`
	AgentRuns          Limit `json:"agent_runs"`
	PeakMultiplier     Limit `json:"peak_multiplier"`

	CPUMillis           Limit `json:"cpu_millis"`
	MemoryBytes         Limit `json:"memory_bytes"`
	DatabaseRows        Limit `json:"database_rows"`
	QueueAgeSeconds     Limit `json:"queue_age_seconds"`
	MaxErrorBasisPoints Limit `json:"max_error_basis_points"`
	MaxDuplicateEffects Limit `json:"max_duplicate_effects"`

	DegradationPolicy string `json:"degradation_policy"`
}

// Diagnostic identifies a rejected field without requiring callers to parse
// error prose.
type Diagnostic struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s %s: %s", d.Code, d.Field, d.Reason)
}

// Version reports the planning contract version.
func Version() int { return SchemaVersion }

// Explain describes the evidence boundary consumed by planning tools.
func Explain() string {
	return "PERF-ENV-001/PERF-007 v1: versioned workload envelopes and deterministic unit-cost/capacity forecasts"
}

// Validate returns all envelope violations in deterministic field order.
func Validate(envelope WorkloadEnvelope) []Diagnostic {
	var diagnostics []Diagnostic
	needText := func(field, value, reason string) {
		if strings.TrimSpace(value) == "" {
			diagnostics = append(diagnostics, Diagnostic{Field: field, Code: "MISSING", Reason: reason})
		}
	}
	needPositive := func(field string, limit Limit) {
		if limit.Value <= 0 || strings.TrimSpace(limit.Unit) == "" {
			diagnostics = append(diagnostics, Diagnostic{Field: field, Code: "MISSING_OR_INVALID", Reason: "a positive value and unit are required"})
		}
	}

	if envelope.SchemaVersion != SchemaVersion {
		diagnostics = append(diagnostics, Diagnostic{Field: "schema_version", Code: "UNSUPPORTED", Reason: "schema version must be 1"})
	}
	needText("id", envelope.ID, "envelope id is required")
	needText("version", envelope.Version, "envelope version is required")
	needPositive("tenant_count", envelope.TenantCount)
	needText("tier", envelope.Tier, "tenant tier is required")
	needPositive("concurrent_users", envelope.ConcurrentUsers)
	needPositive("concurrent_commands", envelope.ConcurrentCommands)
	needPositive("commands_per_minute", envelope.CommandsPerMinute)
	needPositive("fanout_per_command", envelope.FanoutPerCommand)
	needPositive("payload_bytes", envelope.PayloadBytes)
	needPositive("object_bytes", envelope.ObjectBytes)
	needPositive("integration_ops", envelope.IntegrationOps)
	needPositive("agent_runs", envelope.AgentRuns)
	needPositive("peak_multiplier", envelope.PeakMultiplier)
	for field, limit := range map[string]Limit{
		"cpu_millis":             envelope.CPUMillis,
		"memory_bytes":           envelope.MemoryBytes,
		"database_rows":          envelope.DatabaseRows,
		"queue_age_seconds":      envelope.QueueAgeSeconds,
		"max_error_basis_points": envelope.MaxErrorBasisPoints,
		"max_duplicate_effects":  envelope.MaxDuplicateEffects,
	} {
		needPositive(field, limit)
	}
	needText("degradation_policy", envelope.DegradationPolicy, "degradation policy is required")
	if envelope.Tier != "" && envelope.Tier != SmallTier && envelope.Tier != MediumTier && envelope.Tier != LargeTier && envelope.Tier != PeakTier {
		diagnostics = append(diagnostics, Diagnostic{Field: "tier", Code: "INVALID", Reason: "tier must be SMALL, MEDIUM, LARGE or PEAK"})
	}
	if envelope.PeakMultiplier.Value > 1000 {
		diagnostics = append(diagnostics, Diagnostic{Field: "peak_multiplier", Code: "INVALID", Reason: "peak multiplier exceeds the planning bound"})
	}
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Field != diagnostics[j].Field {
			return diagnostics[i].Field < diagnostics[j].Field
		}
		return diagnostics[i].Code < diagnostics[j].Code
	})
	return diagnostics
}

// Check rejects an envelope with a stable first diagnostic.
func Check(envelope WorkloadEnvelope) error {
	if diagnostics := Validate(envelope); len(diagnostics) != 0 {
		return fmt.Errorf("%w: %s", ErrInvalidEnvelope, diagnostics[0])
	}
	return nil
}

// EnvelopeFixtures returns clearly labelled mechanics fixtures. They are not
// customer commitments; all values are placeholders pending human selection.
func EnvelopeFixtures() []WorkloadEnvelope {
	return []WorkloadEnvelope{
		placeholderEnvelope("PLACEHOLDER_SMALL_ENVELOPE", SmallTier, 1, 10, 100, 1000, 4, 256*1024, 10*1024*1024, 100, 10, 2),
		placeholderEnvelope("PLACEHOLDER_MEDIUM_ENVELOPE", MediumTier, 10, 100, 1000, 10000, 8, 1024*1024, 100*1024*1024, 1000, 100, 4),
		placeholderEnvelope("PLACEHOLDER_LARGE_ENVELOPE", LargeTier, 100, 1000, 10000, 100000, 16, 4*1024*1024, 1024*1024*1024, 10000, 1000, 8),
		placeholderEnvelope("PLACEHOLDER_PEAK_ENVELOPE", PeakTier, 100, 2000, 20000, 200000, 32, 8*1024*1024, 2*1024*1024*1024, 20000, 2000, 16),
	}
}

func placeholderEnvelope(id, tier string, tenants, users, commands, perMinute, fanout int64, payload, object, integration, agents, peak int64) WorkloadEnvelope {
	return WorkloadEnvelope{
		SchemaVersion: SchemaVersion, ID: id, Version: "PLACEHOLDER_v1", TenantCount: Limit{tenants, "tenants"}, Tier: tier,
		ConcurrentUsers: Limit{users, "users"}, ConcurrentCommands: Limit{commands, "commands"}, CommandsPerMinute: Limit{perMinute, "commands/minute"},
		FanoutPerCommand: Limit{fanout, "effects/command"}, PayloadBytes: Limit{payload, "bytes"}, ObjectBytes: Limit{object, "bytes"},
		IntegrationOps: Limit{integration, "operations/minute"}, AgentRuns: Limit{agents, "runs/hour"}, PeakMultiplier: Limit{peak, "x"},
		CPUMillis: Limit{1000, "millicores"}, MemoryBytes: Limit{512 * 1024 * 1024, "bytes"}, DatabaseRows: Limit{100000, "rows"},
		QueueAgeSeconds: Limit{60, "seconds"}, MaxErrorBasisPoints: Limit{100, "basis_points"}, MaxDuplicateEffects: Limit{0 + 1, "effects"},
		DegradationPolicy: "PLACEHOLDER_READ_ONLY_THEN_QUEUE",
	}
}

// UsageForecast is the expected workload against an envelope for one named
// horizon. All quantities are explicit so unallocated spend cannot disappear.
type UsageForecast struct {
	Commands        int64 `json:"commands"`
	WorkflowRuns    int64 `json:"workflow_runs"`
	ConnectorOps    int64 `json:"connector_ops"`
	StoredGBMonths  int64 `json:"stored_gb_months"`
	AgentRuns       int64 `json:"agent_runs"`
	PeakCommands    int64 `json:"peak_commands"`
	PeakConcurrency int64 `json:"peak_concurrency"`
}

// UnitRates are planning inputs, not prices or a billing contract.
type UnitRates struct {
	CentsPerCommand       int64 `json:"cents_per_command"`
	CentsPerWorkflowRun   int64 `json:"cents_per_workflow_run"`
	CentsPerConnectorOp   int64 `json:"cents_per_connector_op"`
	CentsPerStoredGBMonth int64 `json:"cents_per_stored_gb_month"`
	CentsPerAgentRun      int64 `json:"cents_per_agent_run"`
}

// Allocation identifies the source and disposition of a spend line. A line
// without an allocation fails closed for PERF-007.
type Allocation struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	AmountCents int64  `json:"amount_cents"`
	Allocated   bool   `json:"allocated"`
	Source      string `json:"source"`
}

// CostEvidenceSummary is the minimal compiled WEDGE-007 handoff consumed by
// PERF-007. WEDGE-007 remains customer-specific; the summary is deliberately
// supplied by the caller rather than invented here.
type CostEvidenceSummary struct {
	CustomerLaborCents int64  `json:"customer_labor_cents"`
	HCMNextCostCents   int64  `json:"hcm_next_cost_cents"`
	SourceRef          string `json:"source_ref"`
}

// CostForecastInput combines WEDGE-007 cost evidence with technical usage.
type CostForecastInput struct {
	SchemaVersion int                 `json:"schema_version"`
	ID            string              `json:"id"`
	Envelope      WorkloadEnvelope    `json:"envelope"`
	Usage         UsageForecast       `json:"usage"`
	Rates         UnitRates           `json:"rates"`
	Allocations   []Allocation        `json:"allocations"`
	WedgeCost     CostEvidenceSummary `json:"wedge_cost"`
	BudgetCents   int64               `json:"budget_cents"`
}

// CapacityForecast is a deterministic capacity and headroom projection.
type CapacityForecast struct {
	EnvelopeID          string  `json:"envelope_id"`
	PeakCommands        int64   `json:"peak_commands"`
	PeakConcurrency     int64   `json:"peak_concurrency"`
	CommandHeadroom     float64 `json:"command_headroom"`
	ConcurrencyHeadroom float64 `json:"concurrency_headroom"`
	WithinEnvelope      bool    `json:"within_envelope"`
}

// UnitCost is one deterministic semantic cost line in the forecast.
type UnitCost struct {
	Name       string `json:"name"`
	Quantity   int64  `json:"quantity"`
	RateCents  int64  `json:"rate_cents"`
	TotalCents int64  `json:"total_cents"`
}

// BudgetAlert records a forecast that crosses its supplied planning budget.
type BudgetAlert struct {
	Code          string `json:"code"`
	BudgetCents   int64  `json:"budget_cents"`
	ObservedCents int64  `json:"observed_cents"`
}

// ForecastReport is the evidence compiler output for PERF-007.
type ForecastReport struct {
	SchemaVersion        int              `json:"schema_version"`
	ID                   string           `json:"id"`
	EnvelopeID           string           `json:"envelope_id"`
	CommandCostCents     int64            `json:"command_cost_cents"`
	WorkflowCostCents    int64            `json:"workflow_cost_cents"`
	ConnectorCostCents   int64            `json:"connector_cost_cents"`
	StorageCostCents     int64            `json:"storage_cost_cents"`
	AgentCostCents       int64            `json:"agent_cost_cents"`
	CustomerLaborCents   int64            `json:"customer_labor_cents"`
	HCMNextCostCents     int64            `json:"hcm_next_cost_cents"`
	TotalCostCents       int64            `json:"total_cost_cents"`
	BudgetRemainingCents int64            `json:"budget_remaining_cents"`
	BudgetStatus         string           `json:"budget_status"`
	UnitCosts            []UnitCost       `json:"unit_costs"`
	BudgetAlerts         []BudgetAlert    `json:"budget_alerts"`
	Capacity             CapacityForecast `json:"capacity"`
	Digest               string           `json:"digest"`
}

// Forecast compiles deterministic unit costs and capacity. It rejects
// unallocated spend and envelope violations before calculating a report.
func Forecast(input CostForecastInput) (ForecastReport, error) {
	if input.SchemaVersion != SchemaVersion || strings.TrimSpace(input.ID) == "" {
		return ForecastReport{}, fmt.Errorf("%w: schema version and id are required", ErrInvalidForecast)
	}
	if err := Check(input.Envelope); err != nil {
		return ForecastReport{}, err
	}
	if input.Usage.Commands < 0 || input.Usage.WorkflowRuns < 0 || input.Usage.ConnectorOps < 0 || input.Usage.StoredGBMonths < 0 || input.Usage.AgentRuns < 0 || input.Usage.PeakCommands < 0 || input.Usage.PeakConcurrency < 0 {
		return ForecastReport{}, fmt.Errorf("%w: usage cannot be negative", ErrInvalidForecast)
	}
	for _, rate := range []int64{input.Rates.CentsPerCommand, input.Rates.CentsPerWorkflowRun, input.Rates.CentsPerConnectorOp, input.Rates.CentsPerStoredGBMonth, input.Rates.CentsPerAgentRun} {
		if rate < 0 {
			return ForecastReport{}, fmt.Errorf("%w: rates cannot be negative", ErrInvalidForecast)
		}
	}
	allocations := append([]Allocation(nil), input.Allocations...)
	sort.SliceStable(allocations, func(i, j int) bool { return allocations[i].ID < allocations[j].ID })
	var allocated int64
	for _, allocation := range allocations {
		if strings.TrimSpace(allocation.ID) == "" || strings.TrimSpace(allocation.Category) == "" || strings.TrimSpace(allocation.Source) == "" || allocation.AmountCents < 0 || !allocation.Allocated {
			return ForecastReport{}, fmt.Errorf("%w: allocation %q is missing a source, amount or disposition", ErrInvalidForecast, allocation.ID)
		}
		allocated += allocation.AmountCents
	}
	if allocated != input.WedgeCost.CustomerLaborCents+input.WedgeCost.HCMNextCostCents {
		return ForecastReport{}, fmt.Errorf("%w: allocated spend does not equal WEDGE-007 cost evidence", ErrInvalidForecast)
	}
	if input.WedgeCost.CustomerLaborCents < 0 || input.WedgeCost.HCMNextCostCents < 0 || strings.TrimSpace(input.WedgeCost.SourceRef) == "" {
		return ForecastReport{}, fmt.Errorf("%w: WEDGE-007 cost evidence requires non-negative totals and a source", ErrInvalidForecast)
	}

	report := ForecastReport{SchemaVersion: SchemaVersion, ID: input.ID, EnvelopeID: input.Envelope.ID}
	report.CommandCostCents = input.Usage.Commands * input.Rates.CentsPerCommand
	report.WorkflowCostCents = input.Usage.WorkflowRuns * input.Rates.CentsPerWorkflowRun
	report.ConnectorCostCents = input.Usage.ConnectorOps * input.Rates.CentsPerConnectorOp
	report.StorageCostCents = input.Usage.StoredGBMonths * input.Rates.CentsPerStoredGBMonth
	report.AgentCostCents = input.Usage.AgentRuns * input.Rates.CentsPerAgentRun
	report.CustomerLaborCents = input.WedgeCost.CustomerLaborCents
	report.HCMNextCostCents = input.WedgeCost.HCMNextCostCents
	report.TotalCostCents = report.CommandCostCents + report.WorkflowCostCents + report.ConnectorCostCents + report.StorageCostCents + report.AgentCostCents + report.CustomerLaborCents + report.HCMNextCostCents
	report.BudgetRemainingCents = input.BudgetCents - report.TotalCostCents
	report.BudgetStatus = BudgetWithin
	if report.TotalCostCents > input.BudgetCents {
		report.BudgetStatus = BudgetAlertStatus
		report.BudgetAlerts = []BudgetAlert{{Code: BudgetAlertStatus, BudgetCents: input.BudgetCents, ObservedCents: report.TotalCostCents}}
	}
	report.UnitCosts = []UnitCost{
		{Name: "command", Quantity: input.Usage.Commands, RateCents: input.Rates.CentsPerCommand, TotalCents: report.CommandCostCents},
		{Name: "workflow_run", Quantity: input.Usage.WorkflowRuns, RateCents: input.Rates.CentsPerWorkflowRun, TotalCents: report.WorkflowCostCents},
		{Name: "connector_op", Quantity: input.Usage.ConnectorOps, RateCents: input.Rates.CentsPerConnectorOp, TotalCents: report.ConnectorCostCents},
		{Name: "stored_gb_month", Quantity: input.Usage.StoredGBMonths, RateCents: input.Rates.CentsPerStoredGBMonth, TotalCents: report.StorageCostCents},
		{Name: "agent_run", Quantity: input.Usage.AgentRuns, RateCents: input.Rates.CentsPerAgentRun, TotalCents: report.AgentCostCents},
	}
	report.Capacity = capacity(input.Envelope, input.Usage)
	if input.BudgetCents < 0 {
		return ForecastReport{}, fmt.Errorf("%w: budget cannot be negative", ErrInvalidForecast)
	}
	digest, err := digest(report)
	if err != nil {
		return ForecastReport{}, err
	}
	report.Digest = digest
	return report, nil
}

func capacity(envelope WorkloadEnvelope, usage UsageForecast) CapacityForecast {
	commandLimit := envelope.CommandsPerMinute.Value * envelope.PeakMultiplier.Value
	concurrencyLimit := envelope.ConcurrentCommands.Value
	commandHeadroom := ratio(commandLimit-usage.PeakCommands, commandLimit)
	concurrencyHeadroom := ratio(concurrencyLimit-usage.PeakConcurrency, concurrencyLimit)
	return CapacityForecast{
		EnvelopeID: envelope.ID, PeakCommands: usage.PeakCommands, PeakConcurrency: usage.PeakConcurrency,
		CommandHeadroom: commandHeadroom, ConcurrencyHeadroom: concurrencyHeadroom,
		WithinEnvelope: usage.PeakCommands <= commandLimit && usage.PeakConcurrency <= concurrencyLimit,
	}
}

func ratio(remaining, limit int64) float64 {
	if limit <= 0 {
		return 0
	}
	return math.Round(float64(remaining)/float64(limit)*1000000) / 1000000
}

func digest(report ForecastReport) (string, error) {
	copyReport := report
	copyReport.Digest = ""
	data, err := json.Marshal(copyReport)
	if err != nil {
		return "", fmt.Errorf("performance: encode forecast: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
