package application

// This file is the ARCH-GO-020 configuration contract for the application
// composition root. It is deliberately separate from the wiring in serve.go:
// a composition root that reads flags while it constructs adapters has no
// point at which the configuration is "validated", and every later component
// then has to re-decide what an empty string meant.
//
// The rule this file encodes is that a role is composed from a *value*. The
// command parses arguments into bootstrap.Values, ValidateServeValues rejects
// a configuration a listener must not start on, ServeConfigFromValues turns
// what survived into a ServeConfig, and every constructor downstream reads
// that struct. Nothing downstream reads a flag, an environment variable or a
// package-level default.

import (
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
	kernelvalues "github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// EnvDatabaseURL names the server the serve role connects to, matching
// cmd/migrate's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

// EnvDevHMACKey carries the development signing key, so it need not appear in
// a process listing.
const EnvDevHMACKey = "HCMNEXT_DEV_HMAC_KEY"

// EnvHealthAddr is the loopback host:port the serve role's health and
// readiness endpoint listens on when -health-addr is not passed.
const EnvHealthAddr = "HCMNEXT_HEALTH_ADDR"

// Configuration field names. They are constants because ServeConfigFields
// declares them and ServeConfigFromValues reads them back: a typo between the
// two is a startup failure rather than a silently defaulted value.
const (
	FieldGRPCListen      = "grpc-listen"
	FieldHTTPListen      = "http-listen"
	FieldDatabaseURL     = "database-url"
	FieldDevHMACKey      = "dev-hmac-key"
	FieldIssuer          = "issuer"
	FieldAudience        = "audience"
	FieldTenant          = "tenant"
	FieldCellID          = "cell-id"
	FieldMaxDeadline     = "max-deadline"
	FieldMigrate         = "migrate"
	FieldWorkspace       = "workspace"
	FieldDevBrowserLogin = "dev-browser-login"
	FieldOTelExporter    = "otel-exporter"
	FieldOTelEndpoint    = "otel-endpoint"

	// FieldExecutionAuthority is the P1B execution authority gate family.
	// Every field below defaults to off/empty; a cell composed with no
	// -execution-authority behaves byte-for-byte like the P1A cell of today
	// (internal/intent/app.ExecutionAuthority is nil, and ExecuteIntent
	// refuses exactly as every other governed write in this release does).
	FieldExecutionAuthority       = "execution-authority"
	FieldExecutionAuthorityDigest = "execution-authority-digest"
	FieldExecutionAuthorityRole   = "execution-authority-role"
	FieldExecutionApprover        = "execution-authority-approver"
	FieldTimerTzdbVersion         = "timer-tzdb-version"
	FieldTimerCalendarVersion     = "timer-calendar-version"
	FieldScheduler                = "scheduler"
	FieldHealthAddr               = "health-addr"
	FieldWorkflowPlan             = "workflow-plan"
)

const (
	WorkflowPlanPrototype = "prototype"
	WorkflowPlanExecute   = "execute"
)

// OTelExporterNone, OTelExporterStdout and OTelExporterOTLPHTTP are the
// allowed values of -otel-exporter. OTelExporterNone is the default: a
// listener started with no telemetry flag at all publishes no spans or
// metrics, rather than exporting to stdout by surprise.
const (
	OTelExporterNone     = "none"
	OTelExporterStdout   = "stdout"
	OTelExporterOTLPHTTP = "otlphttp"
)

// DefaultIssuer and DefaultAudience are the serve role's own
// -issuer/-audience defaults. cmd/hcmnext's token command shares these same
// constants for its own defaults, so a credential minted with no flags beyond
// -dev-hmac-key/-tenant/-subject verifies against a serve process started
// with no flags beyond its own -dev-hmac-key: the two commands cannot drift
// apart by one of them changing a literal the other did not.
const (
	DefaultIssuer   = "https://issuer.local.hcm-next.invalid"
	DefaultAudience = "hcm-next-api"
)

// DefaultTimerTzdbVersion and DefaultTimerCalendarVersion are the dataset
// releases a serve process composes its durable timers against when the
// deployment names none. They match the releases the workflow WAIT fixtures
// pin (test/workflow), so a plan compiled against the reference dataset
// parks and resumes on this process without a flag; a deployment on a newer
// tzdb or calendar release sets both flags explicitly.
const (
	DefaultTimerTzdbVersion     = "2026a"
	DefaultTimerCalendarVersion = "2026.1"
)

// MinimumHMACKeyBytes is the shortest development signing key this listener
// will start with.
const MinimumHMACKeyBytes = 32

// ShutdownGrace bounds the whole ordered shutdown sequence.
const ShutdownGrace = 20 * time.Second

// TelemetryShutdownGrace bounds only flushing the process-local OTel
// provider. It remains inside the whole service shutdown deadline above, and
// failed export is intentionally reported without changing the process's
// business shutdown outcome.
const TelemetryShutdownGrace = 5 * time.Second

// ServeConfig is the validated configuration one serve role is composed
// from. It is a plain value with no behaviour and no defaults applied lazily
// at read time: ServeConfigFromValues resolves every field once, and the
// composition reads only this struct afterwards.
type ServeConfig struct {
	GRPCListen  string
	HTTPListen  string
	DatabaseURL string
	// DevHMACKey is the development signing key. It is carried, never
	// logged: bootstrap.Field marks it Secret so the config fingerprint and
	// the startup log attributes redact it.
	DevHMACKey      string
	Issuer          string
	Audience        string
	Tenant          string
	CellID          string
	MaxDeadline     time.Duration
	Migrate         bool
	Workspace       bool
	DevBrowserLogin bool
	OTelExporter    string
	OTelEndpoint    string

	ExecutionAuthority       bool
	ExecutionAuthorityDigest string
	ExecutionAuthorityRole   string
	ExecutionApprover        string
	// TimerTzdbVersion and TimerCalendarVersion are the dataset releases the
	// execution driver's durable timers resolve wake instants against
	// (WF-RUN-004). Both set composes the timer ports; both empty composes
	// none; one of the two set is refused by Validate.
	TimerTzdbVersion     string
	TimerCalendarVersion string
	Scheduler            bool
	WorkflowPlan         string
	// HealthAddr is the loopback host:port the bootstrap health and
	// readiness endpoint listens on (STARTING, READY, DRAINING as
	// {"state":...}); empty serves none. It is a separate listener from the
	// HTTP edge on purpose: a probe must answer while the edge is draining.
	HealthAddr string
}

// ServeConfigFields declares every flag/env-backed configuration value the
// serve role accepts. It is the single declaration: the command does not
// add, rename or re-default one.
func ServeConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{Name: FieldGRPCListen, Usage: "address the canonical gRPC surface listens on", Default: "127.0.0.1:8443"},
		{Name: FieldHTTPListen, Usage: "address the HTTP edge listens on", Default: "127.0.0.1:8080"},
		{Name: FieldDatabaseURL, Env: EnvDatabaseURL, Usage: "PostgreSQL connection URL"},
		{Name: FieldDevHMACKey, Env: EnvDevHMACKey, Usage: "development HMAC signing key, at least 32 bytes", Secret: true},
		{Name: FieldIssuer, Usage: "the only credential issuer this listener accepts", Default: DefaultIssuer},
		{Name: FieldAudience, Usage: "the audience this listener answers to", Default: DefaultAudience},
		{Name: FieldTenant, Usage: "tenant slug to register on start; empty registers none"},
		{Name: FieldCellID, Usage: "cell identifier a registered tenant is bound to", Default: "cell-local"},
		{Name: FieldMaxDeadline, Usage: "server-imposed cap on every request deadline", Default: "30s", Kind: bootstrap.KindDuration},
		{Name: FieldMigrate, Usage: "apply pending migrations before the listeners start", Default: "true", Kind: bootstrap.KindBool},
		{Name: FieldWorkspace, Usage: "serve the human-facing Promotion workspace on the HTTP edge", Default: "true", Kind: bootstrap.KindBool},
		{Name: FieldDevBrowserLogin, Usage: "dev-only: serve a pasted-token sign-in form for the workspace at " + workspace.PathLogin, Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldOTelExporter, Usage: "OTel exporter: none, stdout, or otlphttp", Default: OTelExporterNone},
		{Name: FieldOTelEndpoint, Usage: "OTLP/HTTP collector endpoint; required when -" + FieldOTelExporter + "=" + OTelExporterOTLPHTTP},
		{Name: FieldExecutionAuthority, Usage: "P1B gate: compose this cell with the caller-driven promotion execution driver, so ExecuteIntent can run instead of refusing (planning/next-steps.md \"P1B exists only after a signed Gate A PROCEED\")", Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldExecutionAuthorityDigest, Usage: "the signed P1B authority amendment digest this cell asserts; carried through as evidence, never verified by this process"},
		{Name: FieldExecutionAuthorityRole, Usage: "the principal role ExecuteIntent additionally requires under -" + FieldExecutionAuthority, Default: "promotion_operator"},
		{Name: FieldExecutionApprover, Usage: "the principal the composed promotion approval workflow routes its one approval WorkItem to", Default: "principal:promotion-approver"},
		{Name: FieldTimerTzdbVersion, Usage: "tzdb release the execution driver's durable timers resolve wake instants against; with -" + FieldTimerCalendarVersion + " it composes the WAIT-node timer ports, empty composes none", Default: DefaultTimerTzdbVersion},
		{Name: FieldTimerCalendarVersion, Usage: "business-calendar release the execution driver's durable timers resolve wake instants against", Default: DefaultTimerCalendarVersion},
		{Name: FieldScheduler, Usage: "run the in-process workflow timer/ready-work dispatcher", Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldHealthAddr, Env: EnvHealthAddr, Usage: "loopback host:port (127.0.0.1, localhost or ::1) to serve the health/readiness endpoint on; empty disables it"},
		{Name: FieldWorkflowPlan, Usage: "promotion workflow plan: prototype or execute", Default: WorkflowPlanPrototype},
	}
}

// ServeConfigFromValues resolves parsed configuration into the value the
// composition reads. It returns only the typed-parse failures
// bootstrap.Values reports; semantic rejection is ServeConfig.Validate's job,
// so the two cannot disagree about what "valid" means.
func ServeConfigFromValues(values *bootstrap.Values) (ServeConfig, error) {
	if values == nil {
		return ServeConfig{}, fmt.Errorf("application: serve configuration needs parsed values")
	}
	cfg := ServeConfig{
		GRPCListen:               values.String(FieldGRPCListen),
		HTTPListen:               values.String(FieldHTTPListen),
		DatabaseURL:              values.String(FieldDatabaseURL),
		DevHMACKey:               values.String(FieldDevHMACKey),
		Issuer:                   values.String(FieldIssuer),
		Audience:                 values.String(FieldAudience),
		Tenant:                   values.String(FieldTenant),
		CellID:                   values.String(FieldCellID),
		OTelExporter:             values.String(FieldOTelExporter),
		OTelEndpoint:             values.String(FieldOTelEndpoint),
		ExecutionAuthorityDigest: values.String(FieldExecutionAuthorityDigest),
		ExecutionAuthorityRole:   values.String(FieldExecutionAuthorityRole),
		ExecutionApprover:        values.String(FieldExecutionApprover),
		TimerTzdbVersion:         values.String(FieldTimerTzdbVersion),
		TimerCalendarVersion:     values.String(FieldTimerCalendarVersion),
		HealthAddr:               values.String(FieldHealthAddr),
		WorkflowPlan:             values.String(FieldWorkflowPlan),
	}
	var err error
	if cfg.MaxDeadline, err = values.Duration(FieldMaxDeadline); err != nil {
		return ServeConfig{}, err
	}
	if cfg.Migrate, err = values.Bool(FieldMigrate); err != nil {
		return ServeConfig{}, err
	}
	if cfg.Workspace, err = values.Bool(FieldWorkspace); err != nil {
		return ServeConfig{}, err
	}
	if cfg.DevBrowserLogin, err = values.Bool(FieldDevBrowserLogin); err != nil {
		return ServeConfig{}, err
	}
	if cfg.ExecutionAuthority, err = values.Bool(FieldExecutionAuthority); err != nil {
		return ServeConfig{}, err
	}
	if cfg.Scheduler, err = values.Bool(FieldScheduler); err != nil {
		return ServeConfig{}, err
	}
	return cfg, nil
}

// Validate rejects a configuration a listener must not start on. It is the
// semantic half of the contract: everything here is a statement about the
// deployment, not about whether a string parsed.
func (c ServeConfig) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable",
			EnvDatabaseURL, FieldDatabaseURL)
	}
	if len(c.DevHMACKey) < MinimumHMACKeyBytes {
		return fmt.Errorf("-%s must be at least %d bytes; a listener that cannot authenticate must not start",
			FieldDevHMACKey, MinimumHMACKeyBytes)
	}
	switch c.OTelExporter {
	case OTelExporterNone, OTelExporterStdout:
	case OTelExporterOTLPHTTP:
		if c.OTelEndpoint == "" {
			return fmt.Errorf("-%s is required when -%s=%s", FieldOTelEndpoint, FieldOTelExporter, OTelExporterOTLPHTTP)
		}
	default:
		return fmt.Errorf("-%s must be one of %s, %s, %s; got %q",
			FieldOTelExporter, OTelExporterNone, OTelExporterStdout, OTelExporterOTLPHTTP, c.OTelExporter)
	}
	if c.ExecutionAuthority {
		if c.ExecutionAuthorityDigest == "" {
			return fmt.Errorf("-%s is required when -%s=true", FieldExecutionAuthorityDigest, FieldExecutionAuthority)
		}
		if c.ExecutionAuthorityRole == "" {
			return fmt.Errorf("-%s is required when -%s=true", FieldExecutionAuthorityRole, FieldExecutionAuthority)
		}
		if c.ExecutionApprover == "" {
			return fmt.Errorf("-%s is required when -%s=true", FieldExecutionApprover, FieldExecutionAuthority)
		}
	}
	if c.Scheduler {
		if c.Tenant == "" {
			return fmt.Errorf("-%s requires -%s", FieldScheduler, FieldTenant)
		}
		if !c.ExecutionAuthority {
			return fmt.Errorf("-%s requires -%s=true", FieldScheduler, FieldExecutionAuthority)
		}
	}
	if c.WorkflowPlan != "" && c.WorkflowPlan != WorkflowPlanPrototype && c.WorkflowPlan != WorkflowPlanExecute {
		return fmt.Errorf("-%s must be %q or %q; got %q", FieldWorkflowPlan, WorkflowPlanPrototype, WorkflowPlanExecute, c.WorkflowPlan)
	}
	if (c.TimerTzdbVersion == "") != (c.TimerCalendarVersion == "") {
		return fmt.Errorf("-%s and -%s are set together or not at all; a timer promise names both releases",
			FieldTimerTzdbVersion, FieldTimerCalendarVersion)
	}
	return nil
}

// TimerDataset is the dataset pair the execution driver's timers are
// composed with; the zero value when neither flag is set.
func (c ServeConfig) TimerDataset() kernelvalues.DatasetVersions {
	return kernelvalues.DatasetVersions{TzdbVersion: c.TimerTzdbVersion, CalendarVersion: c.TimerCalendarVersion}
}

// ValidateServeValues is bootstrap.Spec.Validate for the serve role. The two
// string checks run before the typed parse so the reported failure is the
// same one a reader of the previous cmd/hcmnext implementation would expect:
// a listener with no database URL is told that first, whatever else is also
// wrong.
func ValidateServeValues(values *bootstrap.Values) error {
	if values == nil {
		return fmt.Errorf("application: serve configuration needs parsed values")
	}
	if values.String(FieldDatabaseURL) == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable",
			EnvDatabaseURL, FieldDatabaseURL)
	}
	if len(values.String(FieldDevHMACKey)) < MinimumHMACKeyBytes {
		return fmt.Errorf("-%s must be at least %d bytes; a listener that cannot authenticate must not start",
			FieldDevHMACKey, MinimumHMACKeyBytes)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		return err
	}
	return cfg.Validate()
}
