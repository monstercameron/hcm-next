package securityevidence_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legalhold"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
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

func TestSecurityEvidence_EnvelopeValidationAndCopies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*telemetry.Envelope)
		want   error
		field  string
	}{
		{"bad base schema", func(e *telemetry.Envelope) { e.SchemaVersion = 0 }, securityevidence.ErrInvalidEnvelope, "base.schema_version"},
		{"missing correlation", func(e *telemetry.Envelope) { e.CorrelationID = " " }, securityevidence.ErrInvalidEnvelope, "base.correlation_id"},
		{"unknown outcome", func(e *telemetry.Envelope) { e.Outcome = telemetry.Outcome("BOGUS") }, securityevidence.ErrInvalidEnvelope, "base.outcome"},
		{"missing policy", func(e *telemetry.Envelope) { e.PolicyVersion = 0 }, securityevidence.ErrInvalidEnvelope, "base.policy_version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			secret := "secret-" + tc.name
			base := testBase(t, secret)
			tc.mutate(&base)
			_, err := securityevidence.NewEnvelope(base, securityevidence.TagOperational, securityevidence.RetentionNone, securityevidence.HoldUnspecified, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			var rejection *securityevidence.RejectionError
			if !errors.As(err, &rejection) || rejection.Field != tc.field || rejection.Code == "" || rejection.Version != securityevidence.SchemaVersion {
				t.Fatalf("rejection = %#v, want field %q and schema version", rejection, tc.field)
			}
			if strings.Contains(rejection.Error(), secret) {
				t.Fatalf("rejection leaked correlation id: %v", rejection)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		tag   securityevidence.SignalTag
		ret   securityevidence.RetentionClass
		hold  securityevidence.HoldState
		ref   string
		want  error
		field string
	}{
		{"unknown tag", securityevidence.SignalTag("other"), securityevidence.RetentionNone, securityevidence.HoldUnspecified, "", securityevidence.ErrInvalidEnvelope, "tag"},
		{"unknown retention", securityevidence.TagOperational, securityevidence.RetentionClass("other"), securityevidence.HoldUnspecified, "", securityevidence.ErrInvalidEnvelope, "retention_class"},
		{"security missing retention", securityevidence.TagSecurity, securityevidence.RetentionNone, securityevidence.HoldNone, "", securityevidence.ErrMissingSecurityMD, "retention_class"},
		{"security missing hold", securityevidence.TagSecurity, securityevidence.RetentionSecurityDenial, securityevidence.HoldUnspecified, "", securityevidence.ErrMissingSecurityMD, "hold_state"},
		{"unknown hold", securityevidence.TagOperational, securityevidence.RetentionNone, securityevidence.HoldState("other"), "", securityevidence.ErrInvalidEnvelope, "hold_state"},
		{"ref on none", securityevidence.TagOperational, securityevidence.RetentionNone, securityevidence.HoldNone, "unexpected", securityevidence.ErrInvalidEnvelope, "hold_ref"},
		{"active ref missing", securityevidence.TagOperational, securityevidence.RetentionNone, securityevidence.HoldActive, " ", securityevidence.ErrMissingSecurityMD, "hold_ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := securityevidence.NewEnvelope(testBase(t, tc.name), tc.tag, tc.ret, tc.hold, tc.ref)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			var rejection *securityevidence.RejectionError
			if !errors.As(err, &rejection) || rejection.Field != tc.field {
				t.Fatalf("rejection = %#v, want field %q", rejection, tc.field)
			}
		})
	}

	base := testBase(t, "copy-check")
	base.Attributes["cell_id"] = "before"
	env, err := securityevidence.BuildEnvelope(base, securityevidence.TagSecurity, securityevidence.RetentionSecurityDenial, securityevidence.HoldActive, "hold-1")
	if err != nil {
		t.Fatal(err)
	}
	base.Attributes["cell_id"] = "after"
	if got := env.Base.Attributes["cell_id"]; got != "before" {
		t.Fatalf("envelope copy changed after source mutation: %q", got)
	}
	if env.Canonical() == "" || env.Digest() == "" {
		t.Fatal("valid envelope has empty canonical form or digest")
	}
}

func TestSecurityEvidence_SignalRulesAndRegistryRefuseInvalidState(t *testing.T) {
	valid := securityEnvelope(t, "signal-valid", securityevidence.HoldNone, "")
	for _, tc := range []struct {
		name string
		make func() (securityevidence.Signal, error)
		want error
	}{
		{"unknown sequence", func() (securityevidence.Signal, error) {
			return securityevidence.NewSignal(securityevidence.SequenceKind("x"), evidenceBaseTime, valid)
		}, securityevidence.ErrUnknownSequence},
		{"zero time", func() (securityevidence.Signal, error) {
			return securityevidence.NewSignal(securityevidence.SequenceBreakGlassUse, time.Time{}, valid)
		}, securityevidence.ErrInvalidSignal},
		{"operational envelope", func() (securityevidence.Signal, error) {
			e, err := securityevidence.NewEnvelope(testBase(t, "operational"), securityevidence.TagOperational, securityevidence.RetentionNone, securityevidence.HoldUnspecified, "")
			if err != nil {
				return securityevidence.Signal{}, err
			}
			return securityevidence.NewSignal(securityevidence.SequenceBreakGlassUse, evidenceBaseTime, e)
		}, securityevidence.ErrInvalidSignal},
		{"jit missing used", func() (securityevidence.Signal, error) {
			return securityevidence.NewSignalWithTTL(securityevidence.SequenceJITNearTTLCeiling, evidenceBaseTime, valid, 0, time.Hour)
		}, securityevidence.ErrInvalidSignal},
		{"jit missing ceiling", func() (securityevidence.Signal, error) {
			return securityevidence.NewSignalWithTTL(securityevidence.SequenceJITNearTTLCeiling, evidenceBaseTime, valid, time.Minute, 0)
		}, securityevidence.ErrInvalidSignal},
		{"jit exceeds ceiling", func() (securityevidence.Signal, error) {
			return securityevidence.NewSignalWithTTL(securityevidence.SequenceJITNearTTLCeiling, evidenceBaseTime, valid, 2*time.Hour, time.Hour)
		}, securityevidence.ErrInvalidSignal},
		{"non jit ttl", func() (securityevidence.Signal, error) {
			return securityevidence.NewSignalWithTTL(securityevidence.SequenceBreakGlassUse, evidenceBaseTime, valid, time.Minute, time.Hour)
		}, securityevidence.ErrInvalidSignal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.make()
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			var rejection *securityevidence.RejectionError
			if !errors.As(err, &rejection) || rejection.Field == "" || rejection.State == "" {
				t.Fatalf("error = %#v, want typed field/state rejection", rejection)
			}
		})
	}

	s, err := securityevidence.NewSignalWithTTL(securityevidence.SequenceJITNearTTLCeiling, evidenceBaseTime, valid, 50*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !s.At.Equal(evidenceBaseTime) || s.Canonical() == "" || s.Digest() == "" {
		t.Fatalf("valid signal lost fields: %+v", s)
	}
	if _, err := securityevidence.NewAlertRuleRegistry(); !errors.Is(err, securityevidence.ErrInvalidRule) {
		t.Fatalf("empty registry error = %v", err)
	}
	baseRule := securityevidence.DefaultAlertRules()[0]
	for _, tc := range []struct {
		name   string
		mutate func(*securityevidence.AlertRule)
	}{
		{"missing id", func(r *securityevidence.AlertRule) { r.ID = "" }},
		{"missing version", func(r *securityevidence.AlertRule) { r.Version = 0 }},
		{"unknown sequence", func(r *securityevidence.AlertRule) { r.Sequence = "x" }},
		{"missing name", func(r *securityevidence.AlertRule) { r.AlertName = "" }},
		{"unknown route", func(r *securityevidence.AlertRule) { r.Route = "x" }},
		{"zero threshold", func(r *securityevidence.AlertRule) { r.Threshold = 0 }},
		{"jit zero percent", func(r *securityevidence.AlertRule) {
			r.Sequence = securityevidence.SequenceJITNearTTLCeiling
			r.NearPercent = 0
		}},
		{"jit percent too high", func(r *securityevidence.AlertRule) {
			r.Sequence = securityevidence.SequenceJITNearTTLCeiling
			r.NearPercent = 101
		}},
		{"non jit percent", func(r *securityevidence.AlertRule) { r.NearPercent = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := baseRule
			tc.mutate(&rule)
			if _, err := securityevidence.NewAlertRuleRegistry(rule); !errors.Is(err, securityevidence.ErrInvalidRule) {
				t.Fatalf("error = %v, want ErrInvalidRule", err)
			}
		})
	}
	if _, err := securityevidence.NewAlertRuleRegistry(baseRule, baseRule); !errors.Is(err, securityevidence.ErrInvalidRule) {
		t.Fatalf("duplicate rule error = %v", err)
	}
	rules, err := securityevidence.DefaultAlertRuleRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := rules.Lookup(securityevidence.SequenceBreakGlassUse); !ok || got.Route != securityevidence.RouteIncidentReview {
		t.Fatalf("Lookup returned %+v, %t", got, ok)
	}
	if _, ok := rules.Lookup("missing"); ok {
		t.Fatal("unknown sequence unexpectedly resolved")
	}
	if got := (*securityevidence.AlertRuleRegistry)(nil).Rules(); got != nil {
		t.Fatalf("nil registry Rules = %#v, want nil", got)
	}
	gotRules := rules.Rules()
	gotRules[0].ID = "mutated"
	if rules.Rules()[0].ID == "mutated" {
		t.Fatal("Rules returned mutable registry storage")
	}
}

func TestSecurityEvidence_MemoryPortAndDetectorBoundaries(t *testing.T) {
	first := signal(t, securityevidence.SequenceBreakGlassUse, 2*time.Minute)
	second := signal(t, securityevidence.SequenceBreakGlassUse, time.Minute)
	port, err := securityevidence.NewMemoryPort(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := port.Add(second); err != nil {
		t.Fatal(err)
	}
	if err := (*securityevidence.MemoryPort)(nil).Add(first); !errors.Is(err, securityevidence.ErrInvalidPort) {
		t.Fatalf("nil Add error = %v", err)
	}
	if _, err := (*securityevidence.MemoryPort)(nil).Signals(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(time.Hour)}); !errors.Is(err, securityevidence.ErrInvalidPort) {
		t.Fatalf("nil Signals error = %v", err)
	}
	for _, w := range []securityevidence.Window{{End: evidenceBaseTime}, {Start: evidenceBaseTime}, {Start: evidenceBaseTime, End: evidenceBaseTime}, {Start: evidenceBaseTime.Add(time.Hour), End: evidenceBaseTime}} {
		if _, err := port.Signals(w); !errors.Is(err, securityevidence.ErrInvalidWindow) {
			t.Errorf("window %+v error = %v, want ErrInvalidWindow", w, err)
		}
	}
	got, err := port.Signals(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(3 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].At.Before(got[1].At) {
		t.Fatalf("Signals did not sort by time: %+v", got)
	}
	got[0].Envelope.Base.Attributes["cell_id"] = "changed"
	again, err := port.Signals(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(3 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Envelope.Base.Attributes["cell_id"] == "changed" {
		t.Fatal("Signals returned a shallow copy")
	}
	if _, err := securityevidence.NewMemoryPort(securityevidence.Signal{}); !errors.Is(err, securityevidence.ErrUnknownSequence) {
		t.Fatalf("invalid initial signal error = %v", err)
	}

	rules, err := securityevidence.DefaultAlertRuleRegistry()
	if err != nil {
		t.Fatal(err)
	}
	detector, err := securityevidence.NewSequenceDetector(port, rules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := securityevidence.NewSequenceDetector(nil, rules); !errors.Is(err, securityevidence.ErrInvalidPort) {
		t.Fatalf("nil detector port error = %v", err)
	}
	if _, err := securityevidence.NewSequenceDetector(port, nil); !errors.Is(err, securityevidence.ErrInvalidRule) {
		t.Fatalf("nil detector registry error = %v", err)
	}
	if _, err := (*securityevidence.SequenceDetector)(nil).Detect(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(time.Minute)}); !errors.Is(err, securityevidence.ErrInvalidPort) {
		t.Fatalf("nil detector Detect error = %v", err)
	}
	if alerts, err := detector.Detect(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(30 * time.Second)}); err != nil || len(alerts) != 0 {
		t.Fatalf("below-threshold detection = %+v, %v", alerts, err)
	}

	jitBelow := mustSignalWithTTL(t, securityevidence.SequenceJITNearTTLCeiling, 79*time.Minute, 100*time.Minute, 3*time.Minute)
	jitAt := mustSignalWithTTL(t, securityevidence.SequenceJITNearTTLCeiling, 80*time.Minute, 100*time.Minute, 4*time.Minute)
	portJIT, err := securityevidence.NewMemoryPort(jitBelow, jitAt)
	if err != nil {
		t.Fatal(err)
	}
	detectorJIT, err := securityevidence.NewSequenceDetector(portJIT, rules)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := detectorJIT.Detect(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(5 * time.Minute)})
	if err != nil || len(alerts) != 1 || alerts[0].ObservedCount != 1 || alerts[0].Sequence != securityevidence.SequenceJITNearTTLCeiling {
		t.Fatalf("JIT threshold detection = %+v, %v", alerts, err)
	}
	if alerts[0].Digest == "" || securityevidence.DigestOfAlert(alerts[0]) != alerts[0].Digest {
		t.Fatalf("alert digest mismatch: %+v", alerts[0])
	}

	badPort := &testSignalPort{signals: []securityevidence.Signal{{Kind: securityevidence.SequenceBreakGlassUse}}}
	badDetector, err := securityevidence.NewSequenceDetector(badPort, rules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badDetector.Detect(securityevidence.Window{Start: evidenceBaseTime, End: evidenceBaseTime.Add(time.Minute)}); !errors.Is(err, securityevidence.ErrInvalidSignal) {
		t.Fatalf("invalid port signal error = %v", err)
	}
}

type testSignalPort struct{ signals []securityevidence.Signal }

func (p *testSignalPort) Signals(securityevidence.Window) ([]securityevidence.Signal, error) {
	return p.signals, nil
}

func TestSecurityEvidence_LedgerExportVerificationAndExplanation(t *testing.T) {
	item := signal(t, securityevidence.SequenceBreakGlassUse, time.Minute)
	if _, err := (*securityevidence.Ledger)(nil).Emit(item); !errors.Is(err, securityevidence.ErrInvalidLedger) {
		t.Fatalf("nil Emit error = %v", err)
	}
	if got := (*securityevidence.Ledger)(nil).Records(); got != nil {
		t.Fatalf("nil Records = %#v", got)
	}
	ledger := securityevidence.NewLedger()
	if _, err := ledger.Emit(securityevidence.Signal{}); !errors.Is(err, securityevidence.ErrUnknownSequence) {
		t.Fatalf("invalid Emit error = %v", err)
	}
	first, err := ledger.Emit(item)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.Emit(signal(t, securityevidence.SequenceCrossTenantDenialBurst, 2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.PreviousDigest != "" || second.Revision != 2 || second.PreviousDigest != first.Digest || first.Digest == "" || securityevidence.DigestOfRecord(first) != first.Digest {
		t.Fatalf("ledger chain = %+v, %+v", first, second)
	}
	records := ledger.Records()
	records[0].Signal.Envelope.Base.Attributes["cell_id"] = "mutated"
	if ledger.Records()[0].Signal.Envelope.Base.Attributes["cell_id"] == "mutated" {
		t.Fatal("Records returned shallow copies")
	}
	export := ledger.Export()
	for name, mutate := range map[string]func(*securityevidence.OfflineExport){
		"schema": func(e *securityevidence.OfflineExport) { e.SchemaVersion++ },
		"signal": func(e *securityevidence.OfflineExport) {
			e.Records[0].Signal.Kind = securityevidence.SequenceCrossTenantDenialBurst
		},
		"revision":            func(e *securityevidence.OfflineExport) { e.Records[0].Revision = 2 },
		"previous":            func(e *securityevidence.OfflineExport) { e.Records[1].PreviousDigest = "forged" },
		"record digest":       func(e *securityevidence.OfflineExport) { e.Records[0].Digest = "forged" },
		"head":                func(e *securityevidence.OfflineExport) { e.HeadDigest = "forged" },
		"export digest":       func(e *securityevidence.OfflineExport) { e.ExportDigest = "forged" },
		"empty record digest": func(e *securityevidence.OfflineExport) { e.Records[0].Digest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := export
			copy.Records = append([]securityevidence.SignalRecord(nil), export.Records...)
			mutate(&copy)
			if err := securityevidence.Verify(copy); err == nil || !errors.Is(err, securityevidence.ErrTamperedExport) && !errors.Is(err, securityevidence.ErrRevisionMismatch) {
				t.Fatalf("Verify(%s) = %v, want tamper/revision refusal", name, err)
			}
		})
	}
	if err := securityevidence.VerifyExport(export); err != nil {
		t.Fatal(err)
	}
	if err := export.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := securityevidence.Verify(securityevidence.OfflineExport{SchemaVersion: securityevidence.ExportSchemaVersion}); err == nil || !errors.Is(err, securityevidence.ErrTamperedExport) {
		t.Fatalf("empty export error = %v", err)
	}
	explanation := securityevidence.Explain(first)
	if explanation.SchemaVersion != securityevidence.SchemaVersion || explanation.Revision != first.Revision || explanation.RecordDigest != first.Digest || explanation.SignalKind != first.Signal.Kind || len(explanation.FieldNames) != 5 {
		t.Fatalf("explanation = %+v", explanation)
	}
	if strings.Contains(strings.Join(explanation.FieldNames, " "), "correlation") {
		t.Fatal("explanation exposed sensitive field")
	}
}
