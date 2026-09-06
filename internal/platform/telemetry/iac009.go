package telemetry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const iac009Version = 1

// Version reports the IAC-009 portable telemetry and quota contract version.
// The package already owns the OBS telemetry contracts, so this is additive
// rather than a second telemetry package with a competing schema.
func Version() int { return iac009Version }

func Explain() string {
	return "IAC-009 v1: private tenant-scoped OTLP and Prometheus telemetry with bounded attributes and fail-closed quotas"
}

type QuotaConfig struct {
	StoreAvailable  bool
	FailClosed      bool
	EventsPerTenant int64
	BytesPerTenant  int64
	EventsPerCell   int64
}

type InfrastructurePlan struct {
	CellID          string
	Private         bool
	TenantScoped    bool
	OTLPEnabled     bool
	Prometheus      bool
	Retention       time.Duration
	MaxCardinality  int
	HealthMonitored bool
	Quota           QuotaConfig
}

type Signal struct {
	TenantID   string
	CellID     string
	Name       string
	Attributes map[string]string
	Bytes      int64
}

type QuotaUsage struct {
	TenantEvents int64
	TenantBytes  int64
	CellEvents   int64
}

type QuotaDecision struct {
	Allowed bool
	Code    string
	Detail  string
}

type Violation struct {
	Field  string
	Code   string
	Detail string
}

var ErrInvalidInfrastructure = errors.New("telemetry: invalid IAC-009 infrastructure plan")

func ValidateInfrastructure(p InfrastructurePlan) []Violation {
	var out []Violation
	need := func(field, code, detail string, bad bool) {
		if bad {
			out = append(out, Violation{Field: field, Code: code, Detail: detail})
		}
	}
	need("cell_id", "CELL_REQUIRED", "telemetry infrastructure must name a cell", strings.TrimSpace(p.CellID) == "")
	need("private", "PRIVATE_REQUIRED", "telemetry backends must be private", !p.Private)
	need("tenant_scoped", "TENANT_SCOPE_REQUIRED", "telemetry must be tenant scoped", !p.TenantScoped)
	need("otlp", "OTLP_REQUIRED", "OTLP-compatible ingestion must be enabled", !p.OTLPEnabled)
	need("prometheus", "PROMETHEUS_REQUIRED", "Prometheus-compatible metrics must be enabled", !p.Prometheus)
	need("retention", "RETENTION_REQUIRED", "telemetry retention must be positive", p.Retention <= 0)
	need("max_cardinality", "CARDINALITY_REQUIRED", "telemetry cardinality must be positively bounded", p.MaxCardinality <= 0)
	need("health_monitoring", "HEALTH_MONITORING_REQUIRED", "telemetry backend health must be monitored", !p.HealthMonitored)
	need("quota.fail_closed", "QUOTA_FAIL_CLOSED_REQUIRED", "quota failure must deny admission", !p.Quota.FailClosed)
	need("quota.events_per_tenant", "TENANT_QUOTA_REQUIRED", "tenant event quota must be positive", p.Quota.EventsPerTenant <= 0)
	need("quota.bytes_per_tenant", "TENANT_BYTE_QUOTA_REQUIRED", "tenant byte quota must be positive", p.Quota.BytesPerTenant <= 0)
	need("quota.events_per_cell", "CELL_QUOTA_REQUIRED", "cell event quota must be positive", p.Quota.EventsPerCell <= 0)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func CheckInfrastructure(p InfrastructurePlan) error {
	if violations := ValidateInfrastructure(p); len(violations) != 0 {
		v := violations[0]
		return fmt.Errorf("%w: %s %s: %s", ErrInvalidInfrastructure, v.Code, v.Field, v.Detail)
	}
	return nil
}

// AdmitQuota is fail-closed when the quota store is unavailable. It also
// rejects an unbounded or sensitive signal before it can reach a backend.
func AdmitQuota(p InfrastructurePlan, signal Signal, usage QuotaUsage) QuotaDecision {
	if err := CheckInfrastructure(p); err != nil {
		return QuotaDecision{Code: "INVALID_PLAN", Detail: err.Error()}
	}
	if !p.Quota.StoreAvailable {
		return QuotaDecision{Code: "QUOTA_STORE_UNAVAILABLE", Detail: "telemetry admission is denied while quota state is unavailable"}
	}
	if err := ValidateSignal(p, signal); err != nil {
		return QuotaDecision{Code: "SIGNAL_REJECTED", Detail: err.Error()}
	}
	if signal.Bytes < 0 || usage.TenantEvents < 0 || usage.TenantBytes < 0 || usage.CellEvents < 0 {
		return QuotaDecision{Code: "QUOTA_USAGE_INVALID", Detail: "quota usage cannot be negative"}
	}
	if usage.TenantEvents+1 > p.Quota.EventsPerTenant || usage.TenantBytes+signal.Bytes > p.Quota.BytesPerTenant || usage.CellEvents+1 > p.Quota.EventsPerCell {
		return QuotaDecision{Code: "QUOTA_EXCEEDED", Detail: "bounded telemetry quota would be exceeded"}
	}
	return QuotaDecision{Allowed: true, Code: "TELEMETRY_ADMITTED", Detail: "bounded tenant and cell quota admitted"}
}

func ValidateSignal(p InfrastructurePlan, signal Signal) error {
	if strings.TrimSpace(signal.TenantID) == "" || signal.CellID != p.CellID || strings.TrimSpace(signal.Name) == "" {
		return errors.New("telemetry: signal tenant, cell, and name are required")
	}
	if signal.Bytes < 0 {
		return errors.New("telemetry: signal size cannot be negative")
	}
	for key, value := range signal.Attributes {
		lower := strings.ToLower(key)
		if !allowedIAC009Attribute(lower) {
			return fmt.Errorf("telemetry: attribute %q is not bounded and allow-listed", key)
		}
		if len(value) == 0 || len(value) > 128 {
			return fmt.Errorf("telemetry: attribute %q has an invalid bounded value", key)
		}
	}
	return nil
}

func allowedIAC009Attribute(key string) bool {
	switch key {
	case "cell_id", "tenant_class", "outcome", "service", "signal_class":
		return true
	default:
		return false
	}
}
