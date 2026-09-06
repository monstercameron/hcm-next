package application

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// testDevKey is long enough to satisfy MinimumHMACKeyBytes. It signs nothing
// outside this test binary.
const testDevKey = "application-composition-root-test-signing-key"

// parseServe parses args against the serve role's declared fields with an
// empty environment, so a case's outcome depends only on what it passed.
func parseServe(t *testing.T, args ...string) *bootstrap.Values {
	t.Helper()
	values, err := bootstrap.ParseConfig(args, func(string) (string, bool) { return "", false }, ServeConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig(%v): %v", args, err)
	}
	return values
}

// TestServeConfigFieldsDeclareEveryConfigurationTheRoleReads is the
// configuration half of "no hidden dependencies": every field the composition
// reads has to be declared, or a deployment can set it and be ignored.
func TestServeConfigFieldsDeclareEveryConfigurationTheRoleReads(t *testing.T) {
	declared := map[string]bootstrap.Field{}
	for _, field := range ServeConfigFields() {
		if _, duplicate := declared[field.Name]; duplicate {
			t.Fatalf("field %q declared twice", field.Name)
		}
		declared[field.Name] = field
	}
	for _, name := range []string{
		FieldGRPCListen, FieldHTTPListen, FieldDatabaseURL, FieldDevHMACKey,
		FieldIssuer, FieldAudience, FieldTenant, FieldCellID, FieldMaxDeadline,
		FieldMigrate, FieldWorkspace, FieldDevBrowserLogin, FieldOTelExporter,
		FieldOTelEndpoint, FieldExecutionAuthority, FieldExecutionAuthorityDigest,
		FieldExecutionAuthorityRole, FieldExecutionApprover,
		FieldWorkflowPlan,
	} {
		if _, ok := declared[name]; !ok {
			t.Errorf("field %q is read by the composition but not declared", name)
		}
	}
	if !declared[FieldDevHMACKey].Secret {
		t.Error("the signing key is not marked Secret; it would reach the config fingerprint and the startup log")
	}
	if declared[FieldDevHMACKey].Default != "" {
		t.Error("the signing key has a default; a listener with a default signing key is one anyone can forge against")
	}
	if declared[FieldExecutionAuthority].Default != "false" {
		t.Errorf("-%s defaults to %q, want false: P1B must be opt-in",
			FieldExecutionAuthority, declared[FieldExecutionAuthority].Default)
	}
	if declared[FieldWorkflowPlan].Default != WorkflowPlanPrototype {
		t.Errorf("-%s defaults to %q, want %q", FieldWorkflowPlan, declared[FieldWorkflowPlan].Default, WorkflowPlanPrototype)
	}
	if declared[FieldOTelExporter].Default != OTelExporterNone {
		t.Errorf("-%s defaults to %q, want %q", FieldOTelExporter, declared[FieldOTelExporter].Default, OTelExporterNone)
	}
}

// TestServeConfigFromValuesResolvesEveryFieldOnce proves the composition
// reads a value, not a flag: what ServeConfigFromValues returns is the whole
// input every constructor downstream sees.
func TestServeConfigFromValuesResolvesEveryFieldOnce(t *testing.T) {
	values := parseServe(t,
		"-grpc-listen=127.0.0.1:1", "-http-listen=127.0.0.1:2",
		"-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
		"-issuer=https://issuer.test", "-audience=aud", "-tenant=acme",
		"-cell-id=cell-9", "-max-deadline=7s", "-migrate=false",
		"-workspace=false", "-dev-browser-login=true",
		"-otel-exporter=otlphttp", "-otel-endpoint=http://collector:4318",
		"-execution-authority=true", "-execution-authority-digest=sha256:abc",
		"-execution-authority-role=promo_op", "-execution-authority-approver=principal:approver",
		"-timer-tzdb-version=2026b", "-timer-calendar-version=2026.2", "-health-addr=127.0.0.1:9",
		"-workflow-plan=execute",
	)
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	want := ServeConfig{
		GRPCListen: "127.0.0.1:1", HTTPListen: "127.0.0.1:2",
		DatabaseURL: "postgres://x", DevHMACKey: testDevKey,
		Issuer: "https://issuer.test", Audience: "aud", Tenant: "acme",
		CellID: "cell-9", MaxDeadline: 7 * time.Second, Migrate: false,
		Workspace: false, DevBrowserLogin: true,
		OTelExporter: OTelExporterOTLPHTTP, OTelEndpoint: "http://collector:4318",
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:abc",
		ExecutionAuthorityRole: "promo_op", ExecutionApprover: "principal:approver",
		WorkflowPlan:     WorkflowPlanExecute,
		TimerTzdbVersion: "2026b", TimerCalendarVersion: "2026.2", HealthAddr: "127.0.0.1:9",
	}
	if cfg != want {
		t.Errorf("ServeConfigFromValues =\n %+v\nwant\n %+v", cfg, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate on a complete configuration: %v", err)
	}
}

// The health address is the one setting bootstrap reads before the config
// parse, so ServeSpec resolves it ahead of time from the same fields; a
// broken flag set resolves to none rather than to a stale value.
func TestHealthAddrOfResolvesTheFlagAheadOfBootstrap(t *testing.T) {
	if got := HealthAddrOf([]string{"-database-url=postgres://x", "-health-addr=127.0.0.1:9"}); got != "127.0.0.1:9" {
		t.Fatalf("HealthAddrOf = %q, want 127.0.0.1:9", got)
	}
	if got := HealthAddrOf([]string{"-database-url=postgres://x"}); got != "" {
		t.Fatalf("HealthAddrOf with no flag = %q, want empty", got)
	}
	if got := HealthAddrOf([]string{"-no-such-flag=1"}); got != "" {
		t.Fatalf("HealthAddrOf on a broken flag set = %q, want empty", got)
	}
}

// The timer dataset is one decision named twice: both releases default
// together, both may be cleared together, and naming only one is refused
// because a timer promise pins both.
func TestServeConfigTimerDatasetIsAllOrNothing(t *testing.T) {
	defaults, err := ServeConfigFromValues(parseServe(t, "-database-url=postgres://x", "-dev-hmac-key="+testDevKey))
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if got := defaults.TimerDataset(); got.TzdbVersion != DefaultTimerTzdbVersion || got.CalendarVersion != DefaultTimerCalendarVersion {
		t.Fatalf("default timer dataset = %+v", got)
	}
	if err := defaults.Validate(); err != nil {
		t.Fatalf("Validate with the default dataset: %v", err)
	}

	none := defaults
	none.TimerTzdbVersion, none.TimerCalendarVersion = "", ""
	if err := none.Validate(); err != nil {
		t.Fatalf("Validate with no timer dataset: %v", err)
	}
	if got := none.TimerDataset(); got.Validate() == nil {
		t.Fatalf("an empty pair reported itself as a usable dataset: %+v", got)
	}

	half := defaults
	half.TimerCalendarVersion = ""
	if err := half.Validate(); err == nil {
		t.Fatal("Validate accepted a tzdb release with no calendar release")
	}
}

// TestServeConfigFromValuesRejectsNoValues guards the one call shape a
// composition root must not silently accept.
func TestServeConfigFromValuesRejectsNoValues(t *testing.T) {
	if _, err := ServeConfigFromValues(nil); err == nil {
		t.Fatal("ServeConfigFromValues(nil) returned no error")
	}
	if err := ValidateServeValues(nil); err == nil {
		t.Fatal("ValidateServeValues(nil) returned no error")
	}
}

// TestServeConfigValidateRejectsAConfigurationAListenerMustNotStartOn walks
// every refusal the serve role owns.
func TestServeConfigValidateRejectsAConfigurationAListenerMustNotStartOn(t *testing.T) {
	base := ServeConfig{
		DatabaseURL: "postgres://x", DevHMACKey: testDevKey,
		OTelExporter: OTelExporterNone,
	}
	cases := []struct {
		name    string
		mutate  func(*ServeConfig)
		wantSub string
	}{
		{"no database url", func(c *ServeConfig) { c.DatabaseURL = "" }, EnvDatabaseURL},
		{"short signing key", func(c *ServeConfig) { c.DevHMACKey = "too-short" }, "at least 32 bytes"},
		{"unknown exporter", func(c *ServeConfig) { c.OTelExporter = "jaeger" }, "must be one of"},
		{"otlp without endpoint", func(c *ServeConfig) { c.OTelExporter = OTelExporterOTLPHTTP }, FieldOTelEndpoint},
		{"authority without digest", func(c *ServeConfig) {
			c.ExecutionAuthority = true
			c.ExecutionAuthorityRole = "r"
			c.ExecutionApprover = "a"
		}, FieldExecutionAuthorityDigest},
		{"authority without role", func(c *ServeConfig) {
			c.ExecutionAuthority = true
			c.ExecutionAuthorityDigest = "d"
			c.ExecutionApprover = "a"
		}, FieldExecutionAuthorityRole},
		{"authority without approver", func(c *ServeConfig) {
			c.ExecutionAuthority = true
			c.ExecutionAuthorityDigest = "d"
			c.ExecutionAuthorityRole = "r"
		}, FieldExecutionApprover},
		{"unknown workflow plan", func(c *ServeConfig) { c.WorkflowPlan = "other" }, FieldWorkflowPlan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate error = %q, want it to name %q", err, tc.wantSub)
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Errorf("Validate on the minimum valid configuration: %v", err)
	}
}

// TestValidateServeValuesReportsTheMissingDatabaseURLFirst pins the reporting
// order the previous cmd/hcmnext implementation had: a listener with no
// database URL is told that before anything else that is also wrong.
func TestValidateServeValuesReportsTheMissingDatabaseURLFirst(t *testing.T) {
	values := parseServe(t, "-dev-hmac-key=short", "-otel-exporter=nonsense")
	err := ValidateServeValues(values)
	if err == nil {
		t.Fatal("ValidateServeValues accepted a configuration with no database URL")
	}
	if !strings.Contains(err.Error(), EnvDatabaseURL) {
		t.Errorf("first reported failure = %q, want the missing %s", err, EnvDatabaseURL)
	}

	values = parseServe(t, "-database-url=postgres://x", "-dev-hmac-key=short")
	if err := ValidateServeValues(values); err == nil || !strings.Contains(err.Error(), FieldDevHMACKey) {
		t.Errorf("short key failure = %v, want it to name -%s", err, FieldDevHMACKey)
	}

	values = parseServe(t, "-database-url=postgres://x", "-dev-hmac-key="+testDevKey)
	if err := ValidateServeValues(values); err != nil {
		t.Errorf("ValidateServeValues on the defaults plus a URL and a key: %v", err)
	}
}

// TestServeConfigDefaultsComposeTheP1ACell proves the defaults alone describe
// the shipped P1A cell: workspace on, migrations on, telemetry off and the
// P1B execution authority off.
func TestServeConfigDefaultsComposeTheP1ACell(t *testing.T) {
	cfg, err := ServeConfigFromValues(parseServe(t,
		"-database-url=postgres://x", "-dev-hmac-key="+testDevKey))
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if !cfg.Migrate || !cfg.Workspace {
		t.Errorf("defaults = migrate %t workspace %t, want both on", cfg.Migrate, cfg.Workspace)
	}
	if cfg.DevBrowserLogin || cfg.ExecutionAuthority {
		t.Errorf("defaults = dev-browser-login %t execution-authority %t, want both off",
			cfg.DevBrowserLogin, cfg.ExecutionAuthority)
	}
	if cfg.OTelExporter != OTelExporterNone {
		t.Errorf("default exporter = %q, want %q", cfg.OTelExporter, OTelExporterNone)
	}
	if cfg.WorkflowPlan != WorkflowPlanPrototype {
		t.Errorf("default workflow plan = %q, want %q", cfg.WorkflowPlan, WorkflowPlanPrototype)
	}
	if cfg.Issuer != DefaultIssuer || cfg.Audience != DefaultAudience {
		t.Errorf("default issuer/audience = %q/%q, want %q/%q",
			cfg.Issuer, cfg.Audience, DefaultIssuer, DefaultAudience)
	}
	if cfg.MaxDeadline != 30*time.Second {
		t.Errorf("default max deadline = %s, want 30s", cfg.MaxDeadline)
	}
}
