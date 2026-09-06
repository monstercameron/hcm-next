package securityevidence_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/legalhold"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry/securityevidence"
	"github.com/monstercameron/hcm-next/internal/trust/breakglass"
	"github.com/monstercameron/hcm-next/internal/trust/dlp"
	"github.com/monstercameron/hcm-next/internal/trust/jit"
)

var evidenceBaseTime = time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)

func testBase(t *testing.T, correlation string) telemetry.Envelope {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	ctx := telemetry.WithCorrelationID(nilContext{}, correlation)
	base, err := telemetry.BuildEnvelope(ctx, telemetry.Resource{
		SchemaVersion: 1, ServiceName: "hcmnext", ServiceVersion: "test",
		ServiceInstanceID: "instance-a", Environment: "test", CellID: "cell-a",
		Region: "us-east", ProcessRole: telemetry.ProcessRoleAPI,
		BuildDigest: "build-a", TenantClass: telemetry.TenantClassStandard,
	}, telemetry.OutcomeDenied, 1, map[string]string{"cell_id": "cell-a"}, allow)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// nilContext is a minimal context.Context used to keep test setup explicit.
// telemetry.WithCorrelationID only needs a context value carrier.
type nilContext struct{}

func (nilContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (nilContext) Done() <-chan struct{}       { return nil }
func (nilContext) Err() error                  { return nil }
func (nilContext) Value(any) any               { return nil }

func securityEnvelope(t *testing.T, correlation string, hold securityevidence.HoldState, holdRef string) securityevidence.SignalEnvelope {
	t.Helper()
	envelope, err := securityevidence.NewEnvelope(testBase(t, correlation), securityevidence.TagSecurity, securityevidence.RetentionSecurityDenial, hold, holdRef)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func signal(t *testing.T, kind securityevidence.SequenceKind, offset time.Duration) securityevidence.Signal {
	t.Helper()
	s, err := securityevidence.NewSignal(kind, evidenceBaseTime.Add(offset), securityEnvelope(t, "correlation-"+kindString(kind)+offset.String(), securityevidence.HoldNone, ""))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func kindString(kind securityevidence.SequenceKind) string {
	return strings.ReplaceAll(string(kind), "_", "-")
}

func mustSignalWithTTL(t *testing.T, kind securityevidence.SequenceKind, used, ceiling, offset time.Duration) securityevidence.Signal {
	t.Helper()
	s, err := securityevidence.NewSignalWithTTL(kind, evidenceBaseTime.Add(offset), securityEnvelope(t, "jit-correlation-"+offset.String(), securityevidence.HoldNone, ""), used, ceiling)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTodo_SECARCH_008(t *testing.T) {
	rules, err := securityevidence.DefaultAlertRuleRegistry()
	if err != nil {
		t.Fatal(err)
	}
	port, err := securityevidence.NewMemoryPort(
		signal(t, securityevidence.SequenceRepeatedDLPRefusal, time.Minute),
		signal(t, securityevidence.SequenceRepeatedDLPRefusal, 2*time.Minute),
		signal(t, securityevidence.SequenceRepeatedDLPRefusal, 3*time.Minute),
		signal(t, securityevidence.SequenceBreakGlassUse, 4*time.Minute),
		mustSignalWithTTL(t, securityevidence.SequenceJITNearTTLCeiling, 50*time.Minute, time.Hour, 4*time.Minute),
		signal(t, securityevidence.SequenceCrossTenantDenialBurst, 5*time.Minute),
		signal(t, securityevidence.SequenceCrossTenantDenialBurst, 6*time.Minute),
		signal(t, securityevidence.SequenceCrossTenantDenialBurst, 7*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	detector, err := securityevidence.NewSequenceDetector(port, rules)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := detector.Detect(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 4 {
		t.Fatalf("alerts = %d, want four routed high-risk alerts", len(alerts))
	}
	if alerts[0].Revision != 1 || alerts[1].Revision != 2 || alerts[2].Revision != 3 || alerts[3].Revision != 4 {
		t.Fatalf("alert revisions = %+v, want sequential immutable revisions", alerts)
	}
	if alerts[0].RuleVersion != 1 || alerts[0].Route == "" {
		t.Fatalf("alert routing/version missing: %+v", alerts[0])
	}

	ledger := securityevidence.NewLedger()
	for _, item := range []securityevidence.Signal{
		signal(t, securityevidence.SequenceRepeatedDLPRefusal, 8*time.Minute),
		signal(t, securityevidence.SequenceBreakGlassUse, 9*time.Minute),
	} {
		if _, err := ledger.Emit(item); err != nil {
			t.Fatal(err)
		}
	}
	export := ledger.Export()
	if err := securityevidence.VerifyExport(export); err != nil {
		t.Fatalf("VerifyExport() = %v", err)
	}
}

func TestTodo_SECARCH_008_Golden(t *testing.T) {
	rules := securityevidence.DefaultAlertRules()
	if len(rules) != 4 {
		t.Fatalf("default rule count = %d, want four", len(rules))
	}
	want := map[securityevidence.SequenceKind]struct {
		id        string
		threshold int
		route     securityevidence.AlertRoute
	}{
		securityevidence.SequenceRepeatedDLPRefusal:     {"security.dlp-refusal-burst", 3, securityevidence.RouteSecurityOnCall},
		securityevidence.SequenceBreakGlassUse:          {"security.break-glass-use", 1, securityevidence.RouteIncidentReview},
		securityevidence.SequenceJITNearTTLCeiling:      {"security.jit-near-ttl-ceiling", 1, securityevidence.RouteSecurityOnCall},
		securityevidence.SequenceCrossTenantDenialBurst: {"security.cross-tenant-denial-burst", 3, securityevidence.RouteTenantSecurity},
	}
	for _, rule := range rules {
		expected, ok := want[rule.Sequence]
		if !ok || rule.ID != expected.id || rule.Version != 1 || rule.Threshold != expected.threshold || rule.Route != expected.route {
			t.Fatalf("rule = %+v, want published golden mapping", rule)
		}
	}
}

func TestTodo_SECARCH_008_Integration(t *testing.T) {
	// Wire the named lifecycle owners through their real exported constructors;
	// only the detector's signal source is the in-memory I/O port.
	detectorDefinition, err := dlp.NewDetector("secret-detector", dlp.ClassSpecialCategory, dlp.SeverityHigh, "sensitive")
	if err != nil {
		t.Fatal(err)
	}
	detector, err := dlp.NewInspector(detectorDefinition)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := detector.Inspect([]byte("sensitive"))
	if err != nil || len(inspection.Findings) == 0 {
		t.Fatalf("DLP inspection = %+v, %v", inspection, err)
	}

	now := evidenceBaseTime
	glass, err := breakglass.Open("glass-1", breakglass.Request{User: "operator", IncidentRef: "incident-1", Justification: "containment", Capabilities: []string{"read"}, TTL: time.Minute}, breakglass.Approval{Approver: "reviewer", At: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := glass.Use("read", "review", now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}

	grant, err := jit.New("jit-1", jit.Request{Principal: "operator", Tenant: values.TenantId("tenant-a"), Role: jit.RoleIncidentResponder, TicketRef: "incident-1", Justification: "containment", Capabilities: []string{"read"}, Purpose: "incident", TTL: time.Hour}, jit.Approval{Approver: "reviewer", At: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := grant.Use("review", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	holds := legalhold.NewStore()
	if err := holds.Create(legalhold.Hold{ID: "hold-1", Tenant: "tenant-a", Compartment: "incident", Scope: legalhold.Scope{Tenant: "tenant-a", Compartment: "incident"}, Reason: "review", Authority: "governance", CreatedAt: values.NewInstant(now)}); err != nil {
		t.Fatal(err)
	}
	decision, err := holds.EvaluateDisposition(legalhold.Record{Tenant: "tenant-a", Compartment: "incident", Ref: "record-1"}, "hold-evidence-1", values.NewInstant(now))
	if !errors.Is(err, legalhold.ErrHoldBlocked) || decision.Code != "HOLD_BLOCKED" {
		t.Fatalf("legal hold decision = %+v, %v", decision, err)
	}
	holded := securityEnvelope(t, "held-correlation", securityevidence.HoldActive, "hold-1")
	event, err := securityevidence.NewSignal(securityevidence.SequenceBreakGlassUse, now, holded)
	if err != nil {
		t.Fatal(err)
	}
	port, err := securityevidence.NewMemoryPort(event)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := securityevidence.DefaultAlertRuleRegistry()
	if err != nil {
		t.Fatal(err)
	}
	detectorForIntegration, err := securityevidence.NewSequenceDetector(port, rules)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := detectorForIntegration.Detect(securityevidence.Window{Start: now.Add(-time.Second), End: now.Add(time.Second)})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("detected alerts = %+v, %v", alerts, err)
	}
	ledger := securityevidence.NewLedger()
	if _, err := ledger.Emit(event); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Export().Verify(); err != nil {
		t.Fatal(err)
	}
	_ = inspection
}

func TestTodo_SECARCH_008_Security(t *testing.T) {
	_, err := securityevidence.NewEnvelope(testBase(t, "security-missing-retention"), securityevidence.TagSecurity, securityevidence.RetentionNone, securityevidence.HoldUnspecified, "")
	if err == nil {
		t.Fatal("security envelope without retention/hold was accepted")
	}
	var rejection *securityevidence.RejectionError
	if !errors.As(err, &rejection) || rejection.Field != "retention_class" {
		t.Fatalf("rejection = %v, want typed retention_class refusal", err)
	}

	valid := securityEnvelope(t, "safe-correlation", securityevidence.HoldNone, "")
	record, err := func() (securityevidence.SignalRecord, error) {
		item, err := securityevidence.NewSignal(securityevidence.SequenceBreakGlassUse, evidenceBaseTime, valid)
		if err != nil {
			return securityevidence.SignalRecord{}, err
		}
		ledger := securityevidence.NewLedger()
		return ledger.Emit(item)
	}()
	if err != nil {
		t.Fatal(err)
	}
	explanation := record.Explain()
	if explanation.RecordDigest == "" || strings.Contains(strings.Join(explanation.FieldNames, " "), "correlation") {
		t.Fatalf("Explain exposed unsafe fields: %+v", explanation)
	}
}

func TestTodo_SECARCH_008_Mutation(t *testing.T) {
	item := signal(t, securityevidence.SequenceBreakGlassUse, time.Minute)
	ledger := securityevidence.NewLedger()
	if _, err := ledger.Emit(item); err != nil {
		t.Fatal(err)
	}
	export := ledger.Export()
	export.Records[0].Signal.Kind = securityevidence.SequenceCrossTenantDenialBurst
	if err := securityevidence.VerifyExport(export); err == nil || !errors.Is(err, securityevidence.ErrTamperedExport) {
		t.Fatalf("mutated export verification = %v, want ErrTamperedExport", err)
	}
}
