package reliability

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	StatusHealthy  = "HEALTHY"
	StatusAtRisk   = "AT_RISK"
	StatusBreached = "BREACHED"
	StatusUnknown  = "UNKNOWN"

	DefaultManifestPath = "definitions/operations/pilot-reliability.yaml"
)

// Manifest is an immutable, versioned set of pilot reliability definitions.
type Manifest struct {
	Version     int            `yaml:"version"`
	Module      string         `yaml:"module"`
	EffectiveAt string         `yaml:"effective_at"`
	Owner       string         `yaml:"owner"`
	SLIs        []SLI          `yaml:"slis"`
	SLOs        []SLO          `yaml:"slos"`
	Actions     []BudgetAction `yaml:"error_budget_actions"`
}

// SLI fixes the query and measurement semantics; it does not define a
// customer promise. Availability is expressed as good/valid events.
type SLI struct {
	ID                 string  `yaml:"id"`
	Version            string  `yaml:"version"`
	Capability         string  `yaml:"capability"`
	Query              string  `yaml:"query"`
	Denominator        string  `yaml:"denominator"`
	Window             string  `yaml:"window"`
	StalenessBound     string  `yaml:"staleness_bound"`
	Owner              string  `yaml:"owner"`
	AvailabilityTarget float64 `yaml:"availability_target"`
	LatencyTargetMs    int64   `yaml:"latency_target_ms"`
}

// SLO is the versioned objective and its consequence policy.
type SLO struct {
	ID              string  `yaml:"id"`
	Version         string  `yaml:"version"`
	SLI             string  `yaml:"sli"`
	Target          float64 `yaml:"target"`
	LatencyTargetMs int64   `yaml:"latency_target_ms"`
	Window          string  `yaml:"window"`
	Owner           string  `yaml:"owner"`
	BreachAction    string  `yaml:"breach_action"`
	AtRiskAction    string  `yaml:"at_risk_action"`
	Contractual     bool    `yaml:"contractual"`
}

// BudgetAction names the bounded response at a burn threshold.
type BudgetAction struct {
	ID        string  `yaml:"id"`
	Version   string  `yaml:"version"`
	Threshold float64 `yaml:"threshold"` // fraction of budget consumed
	Action    string  `yaml:"action"`
	Owner     string  `yaml:"owner"`
}

// Measurement is a telemetry aggregate supplied by an observation system.
type Measurement struct {
	AsOf         time.Time
	WindowStart  time.Time
	WindowEnd    time.Time
	ObservedAt   time.Time
	Good         int64
	Valid        int64
	Total        int64
	LatencyP95Ms int64
}

type Diagnostic struct{ Entry, Field, State, Version, Reason string }

func (d Diagnostic) String() string {
	return fmt.Sprintf("entry=%s field=%s state=%s version=%s: %s", d.Entry, d.Field, d.State, d.Version, d.Reason)
}

type Readiness struct {
	Status      string
	Diagnostics []Diagnostic
}

func (r Readiness) Ready() bool { return len(r.Diagnostics) == 0 }

type Result struct {
	Capability, SLI, SLO, Status, Reason, BreachAction string
	Availability, ErrorBudgetRemaining                 float64
	LatencyP95Ms                                       int64
}

func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reliability: reading %s: %w", path, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("reliability: parsing %s: %w", path, err)
	}
	return &m, nil
}

// Validate is deterministic: diagnostics are sorted by entry, field, state,
// and version, making release reports stable across process runs.
func Validate(m *Manifest, now time.Time) Readiness {
	if m == nil {
		return Readiness{Status: StatusUnknown, Diagnostics: []Diagnostic{{Field: "manifest", State: "MISSING", Reason: "manifest is required"}}}
	}
	var ds []Diagnostic
	add := func(e, f, s, v, r string) { ds = append(ds, Diagnostic{e, f, s, v, r}) }
	if m.Version != 1 {
		add("manifest", "version", "UNSUPPORTED", fmt.Sprint(m.Version), "manifest version must be 1")
	}
	for f, v := range map[string]string{"module": m.Module, "effective_at": m.EffectiveAt, "owner": m.Owner} {
		if strings.TrimSpace(v) == "" {
			add("manifest", f, "MISSING", "", f+" is required")
		}
	}
	if len(m.SLIs) == 0 {
		add("manifest", "slis", "MISSING", "", "at least one SLI is required")
	}
	if len(m.SLOs) == 0 {
		add("manifest", "slos", "MISSING", "", "at least one SLO is required")
	}
	slis := map[string]SLI{}
	for _, s := range m.SLIs {
		if _, ok := slis[s.ID]; ok {
			add(s.ID, "id", "DUPLICATE", s.Version, "SLI id is duplicated")
		}
		slis[s.ID] = s
		validateSLI(s, add, now)
	}
	for _, s := range m.SLOs {
		if _, ok := slis[s.SLI]; !ok {
			add(s.ID, "sli", "UNKNOWN", s.Version, "SLO references an unknown SLI")
		}
		validateSLO(s, add)
	}
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		for _, p := range [][2]string{{a.Entry, b.Entry}, {a.Field, b.Field}, {a.State, b.State}, {a.Version, b.Version}, {a.Reason, b.Reason}} {
			if p[0] != p[1] {
				return p[0] < p[1]
			}
		}
		return false
	})
	if len(ds) > 0 {
		return Readiness{Status: StatusUnknown, Diagnostics: ds}
	}
	return Readiness{Status: StatusHealthy}
}

func validateSLI(s SLI, add func(string, string, string, string, string), now time.Time) {
	for f, v := range map[string]string{"version": s.Version, "capability": s.Capability, "query": s.Query, "denominator": s.Denominator, "window": s.Window, "staleness_bound": s.StalenessBound, "owner": s.Owner} {
		if strings.TrimSpace(v) == "" {
			add(s.ID, f, "MISSING", s.Version, f+" is required")
		}
	}
	if s.AvailabilityTarget <= 0 || s.AvailabilityTarget > 1 {
		add(s.ID, "availability_target", "INVALID", s.Version, "availability target must be > 0 and <= 1")
	}
	if s.LatencyTargetMs <= 0 {
		add(s.ID, "latency_target_ms", "INVALID", s.Version, "latency target must be positive")
	}
	if s.StalenessBound != "" {
		if d, e := time.ParseDuration(s.StalenessBound); e != nil || d <= 0 {
			add(s.ID, "staleness_bound", "INVALID", s.Version, "staleness bound must be a positive duration")
		}
	}
	if s.Window != "" {
		if d, e := time.ParseDuration(s.Window); e != nil || d <= 0 {
			add(s.ID, "window", "INVALID", s.Version, "window must be a positive duration")
		}
	}
	_ = now
}
func validateSLO(s SLO, add func(string, string, string, string, string)) {
	for f, v := range map[string]string{"version": s.Version, "sli": s.SLI, "window": s.Window, "owner": s.Owner, "breach_action": s.BreachAction, "at_risk_action": s.AtRiskAction} {
		if strings.TrimSpace(v) == "" {
			add(s.ID, f, "MISSING", s.Version, f+" is required")
		}
	}
	if s.Target <= 0 || s.Target > 1 {
		add(s.ID, "target", "INVALID", s.Version, "SLO target must be > 0 and <= 1")
	}
	if s.LatencyTargetMs <= 0 {
		add(s.ID, "latency_target_ms", "INVALID", s.Version, "latency target must be positive")
	}
	if s.Window != "" {
		if d, e := time.ParseDuration(s.Window); e != nil || d <= 0 {
			add(s.ID, "window", "INVALID", s.Version, "window must be a positive duration")
		}
	}
}

// Evaluate never treats missing, empty, invalid, or stale telemetry as good.
func Evaluate(m Manifest, measurements map[string]Measurement, now time.Time) []Result {
	slis := map[string]SLI{}
	for _, s := range m.SLIs {
		slis[s.ID] = s
	}
	out := make([]Result, 0, len(m.SLOs))
	for _, slo := range m.SLOs {
		sli := slis[slo.SLI]
		r := Result{Capability: sli.Capability, SLI: sli.ID, SLO: slo.ID, BreachAction: slo.BreachAction, Status: StatusUnknown}
		x, ok := measurements[sli.ID]
		if !ok || x.Total <= 0 || x.Valid <= 0 || x.Good < 0 || x.Good > x.Valid || x.Valid > x.Total {
			r.Reason = "measurement denominator or validity is unavailable"
			out = append(out, r)
			continue
		}
		stale := false
		if d, e := time.ParseDuration(sli.StalenessBound); e != nil || now.Sub(x.ObservedAt) > d {
			stale = true
		}
		if stale {
			r.Reason = "telemetry exceeds declared staleness bound"
			out = append(out, r)
			continue
		}
		r.Availability = float64(x.Good) / float64(x.Valid)
		r.LatencyP95Ms = x.LatencyP95Ms
		r.ErrorBudgetRemaining = (r.Availability - slo.Target) / (1 - slo.Target)
		if r.Availability < slo.Target || x.LatencyP95Ms > slo.LatencyTargetMs {
			r.Status = StatusBreached
			r.Reason = "availability or latency target breached"
		} else if r.Availability < slo.Target+(1-slo.Target)*0.5 {
			r.Status = StatusAtRisk
			r.Reason = "more than half of error budget is consumed"
		} else {
			r.Status = StatusHealthy
			r.Reason = "measurement satisfies objective"
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SLO < out[j].SLO })
	return out
}
