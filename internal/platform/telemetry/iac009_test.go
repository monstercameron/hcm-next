package telemetry

import (
	"strings"
	"testing"
	"time"
)

func validIAC009Plan() InfrastructurePlan {
	return InfrastructurePlan{
		CellID: "cell-a", Private: true, TenantScoped: true, OTLPEnabled: true, Prometheus: true, Retention: 30 * 24 * time.Hour,
		MaxCardinality: 1000, HealthMonitored: true, Quota: QuotaConfig{StoreAvailable: true, FailClosed: true, EventsPerTenant: 100, BytesPerTenant: 1 << 20, EventsPerCell: 1000},
	}
}

func validIAC009Signal() Signal {
	return Signal{TenantID: "tenant-a", CellID: "cell-a", Name: "operation.completed", Attributes: map[string]string{"cell_id": "cell-a", "tenant_class": "design-partner", "outcome": "success"}, Bytes: 100}
}

func TestTodo_IAC_009(t *testing.T) {
	plan := validIAC009Plan()
	decision := AdmitQuota(plan, validIAC009Signal(), QuotaUsage{})
	if !decision.Allowed || decision.Code != "TELEMETRY_ADMITTED" {
		t.Fatalf("telemetry admission = %+v", decision)
	}
	if Version() != 1 || !strings.Contains(Explain(), "fail-closed") {
		t.Fatalf("contract metadata missing: version=%d explain=%q", Version(), Explain())
	}
}

func TestTodo_IAC_009_Integration(t *testing.T) {
	plan := validIAC009Plan()
	signal := validIAC009Signal()
	if err := ValidateSignal(plan, signal); err != nil {
		t.Fatal(err)
	}
	if decision := AdmitQuota(plan, signal, QuotaUsage{TenantEvents: 99, TenantBytes: 1 << 20, CellEvents: 999}); decision.Allowed {
		t.Fatalf("quota boundary was admitted: %+v", decision)
	}
}

func TestTodo_IAC_009_Fault(t *testing.T) {
	plan := validIAC009Plan()
	plan.Quota.StoreAvailable = false
	decision := AdmitQuota(plan, validIAC009Signal(), QuotaUsage{})
	if decision.Allowed || decision.Code != "QUOTA_STORE_UNAVAILABLE" {
		t.Fatalf("quota failure defaulted to allow: %+v", decision)
	}
}

func TestTodo_IAC_009_Security(t *testing.T) {
	plan := validIAC009Plan()
	signal := validIAC009Signal()
	signal.Attributes["worker_id"] = "worker-secret"
	if err := ValidateSignal(plan, signal); err == nil {
		t.Fatal("unbounded worker identity attribute was accepted")
	}
	signal = validIAC009Signal()
	signal.Attributes["password"] = "secret"
	if err := ValidateSignal(plan, signal); err == nil {
		t.Fatal("sensitive attribute was accepted")
	}
}
