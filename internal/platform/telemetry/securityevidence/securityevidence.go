// Package securityevidence joins security telemetry retention, legal-hold
// state, high-risk sequence alerting, and offline tamper verification.
//
// The package is intentionally kernel-pure. Callers provide timestamps and a
// window source; this package never reads a clock, starts a goroutine, opens a
// database, or sends an alert. Its records contain digests and bounded
// operational fields, not payloads or secret material.
package securityevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

const (
	SchemaVersion       = 1
	ExportSchemaVersion = 1
)

// SignalTag identifies whether an envelope is ordinary operational telemetry
// or security evidence. SECURITY envelopes must carry explicit retention and
// hold metadata, including an explicit NONE hold state.
type SignalTag string

const (
	TagOperational SignalTag = "OPERATIONAL"
	TagSecurity    SignalTag = "SECURITY"
)

func (t SignalTag) valid() bool { return t == TagOperational || t == TagSecurity }

// HoldState is explicit so an omitted hold state cannot be confused with a
// deliberate statement that no legal hold currently applies.
type HoldState string

const (
	HoldUnspecified HoldState = ""
	HoldNone        HoldState = "NONE"
	HoldActive      HoldState = "ACTIVE"
	HoldReleased    HoldState = "RELEASED"
)

func (h HoldState) valid() bool {
	return h == HoldNone || h == HoldActive || h == HoldReleased
}

// RetentionClass is the published telemetry retention vocabulary. The alias
// keeps security evidence on the same policy classes as OBS-004.
type RetentionClass = telemetry.RetentionClass

const (
	RetentionNone                     = telemetry.RetentionNone
	RetentionSecurityDenial           = telemetry.RetentionSecurityDenial
	RetentionFinancialMutation        = telemetry.RetentionFinancialMutation
	RetentionIrreversibleEffect       = telemetry.RetentionIrreversibleEffect
	RetentionAmbiguousResult          = telemetry.RetentionAmbiguousResult
	RetentionCorrectnessFailure       = telemetry.RetentionCorrectnessFailure
	RetentionTelemetryPipelineFailure = telemetry.RetentionTelemetryPipelineFailure
)

var (
	ErrInvalidEnvelope   = errors.New("securityevidence: invalid signal envelope")
	ErrInvalidSignal     = errors.New("securityevidence: invalid security signal")
	ErrInvalidRule       = errors.New("securityevidence: invalid alert rule")
	ErrInvalidWindow     = errors.New("securityevidence: invalid detection window")
	ErrInvalidPort       = errors.New("securityevidence: signal port is required")
	ErrInvalidLedger     = errors.New("securityevidence: invalid evidence ledger")
	ErrTamperedExport    = errors.New("securityevidence: offline export is tampered")
	ErrRevisionMismatch  = errors.New("securityevidence: immutable revision is not ordered")
	ErrUnknownSequence   = errors.New("securityevidence: sequence is not published")
	ErrMissingSecurityMD = errors.New("securityevidence: security metadata is required")
)

// RejectionError is the typed refusal returned for malformed evidence. Field
// always names the rejected field; the message never includes a raw payload.
type RejectionError struct {
	Code    string
	Field   string
	State   string
	Version int
	err     error
}

func reject(field, state string, version int, cause error) error {
	return &RejectionError{Code: "SECARCH_008_REJECTED", Field: field, State: state, Version: version, err: cause}
}

func (e *RejectionError) Error() string {
	return fmt.Sprintf("%s: field %q state %q (version %d): %v", e.Code, e.Field, e.State, e.Version, e.err)
}

func (e *RejectionError) Unwrap() error { return e.err }

// SignalEnvelope is a security-aware wrapper around OBS-001's typed
// telemetry envelope. The base envelope is copied, including its attributes,
// at construction so later caller mutation cannot alter this revision.
type SignalEnvelope struct {
	Base           telemetry.Envelope
	Tag            SignalTag
	RetentionClass RetentionClass
	HoldState      HoldState
	HoldRef        string
}

// NewEnvelope constructs a security-aware envelope. For SECURITY, retention
// and hold state are both mandatory; an active or released hold also needs an
// opaque hold reference. The reference is preserved for correlation but is
// never returned by Explain.
func NewEnvelope(base telemetry.Envelope, tag SignalTag, retention RetentionClass, hold HoldState, holdRef string) (SignalEnvelope, error) {
	if !tag.valid() {
		return SignalEnvelope{}, reject("tag", "unknown", SchemaVersion, ErrInvalidEnvelope)
	}
	if err := validateBase(base); err != nil {
		return SignalEnvelope{}, err
	}
	if !validRetention(retention) && retention != RetentionNone {
		return SignalEnvelope{}, reject("retention_class", "unknown", SchemaVersion, ErrInvalidEnvelope)
	}
	if tag == TagSecurity {
		if retention == RetentionNone {
			return SignalEnvelope{}, reject("retention_class", "missing", SchemaVersion, ErrMissingSecurityMD)
		}
		if !hold.valid() {
			return SignalEnvelope{}, reject("hold_state", "missing_or_unknown", SchemaVersion, ErrMissingSecurityMD)
		}
	}
	if hold != HoldUnspecified && !hold.valid() {
		return SignalEnvelope{}, reject("hold_state", "unknown", SchemaVersion, ErrInvalidEnvelope)
	}
	if hold == HoldNone && holdRef != "" {
		return SignalEnvelope{}, reject("hold_ref", "unexpected_for_none", SchemaVersion, ErrInvalidEnvelope)
	}
	if (hold == HoldActive || hold == HoldReleased) && strings.TrimSpace(holdRef) == "" {
		return SignalEnvelope{}, reject("hold_ref", "missing", SchemaVersion, ErrMissingSecurityMD)
	}
	return SignalEnvelope{
		Base:           cloneEnvelope(base),
		Tag:            tag,
		RetentionClass: retention,
		HoldState:      hold,
		HoldRef:        holdRef,
	}, nil
}

// BuildEnvelope is an explicit constructor alias for adapters that use the
// same verb as telemetry.BuildEnvelope.
func BuildEnvelope(base telemetry.Envelope, tag SignalTag, retention RetentionClass, hold HoldState, holdRef string) (SignalEnvelope, error) {
	return NewEnvelope(base, tag, retention, hold, holdRef)
}

func validRetention(r RetentionClass) bool {
	switch r {
	case RetentionSecurityDenial, RetentionFinancialMutation, RetentionIrreversibleEffect,
		RetentionAmbiguousResult, RetentionCorrectnessFailure, RetentionTelemetryPipelineFailure:
		return true
	default:
		return false
	}
}

func validateBase(base telemetry.Envelope) error {
	if base.SchemaVersion != telemetry.EnvelopeSchemaVersion {
		return reject("base.schema_version", "missing", SchemaVersion, ErrInvalidEnvelope)
	}
	if err := base.Resource.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(base.CorrelationID) == "" {
		return reject("base.correlation_id", "missing", SchemaVersion, ErrInvalidEnvelope)
	}
	if !base.Outcome.Valid() {
		return reject("base.outcome", "unknown", SchemaVersion, ErrInvalidEnvelope)
	}
	if base.PolicyVersion <= 0 {
		return reject("base.policy_version", "missing", SchemaVersion, ErrInvalidEnvelope)
	}
	return nil
}

func cloneEnvelope(in telemetry.Envelope) telemetry.Envelope {
	out := in
	out.Resource = in.Resource
	out.Attributes = make(map[string]string, len(in.Attributes))
	for key, value := range in.Attributes {
		out.Attributes[key] = value
	}
	return out
}

// Canonical returns the framed, deterministic input used to digest an
// envelope. It includes only typed fields and already-allow-listed
// attributes; it never appends an unbounded diagnostic message.
func (e SignalEnvelope) Canonical() string {
	var b strings.Builder
	frame := func(name, value string) { fmt.Fprintf(&b, "%d:%s=%d:%s;", len(name), name, len(value), value) }
	frame("schema", fmt.Sprintf("%d", e.Base.SchemaVersion))
	frame("service", e.Base.Resource.ServiceName)
	frame("service_version", e.Base.Resource.ServiceVersion)
	frame("instance", e.Base.Resource.ServiceInstanceID)
	frame("environment", e.Base.Resource.Environment)
	frame("cell", e.Base.Resource.CellID)
	frame("region", e.Base.Resource.Region)
	frame("role", string(e.Base.Resource.ProcessRole))
	frame("build", e.Base.Resource.BuildDigest)
	frame("tenant_class", string(e.Base.Resource.TenantClass))
	frame("correlation", e.Base.CorrelationID)
	frame("request", e.Base.RequestID)
	frame("evidence", e.Base.EvidenceRef)
	frame("principal", e.Base.PrincipalRef)
	frame("outcome", string(e.Base.Outcome))
	frame("policy", fmt.Sprintf("%d", e.Base.PolicyVersion))
	frame("tag", string(e.Tag))
	frame("retention", string(e.RetentionClass))
	frame("hold", string(e.HoldState))
	frame("hold_ref", e.HoldRef)
	keys := make([]string, 0, len(e.Base.Attributes))
	for key := range e.Base.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		frame("attribute."+key, e.Base.Attributes[key])
	}
	return b.String()
}

// Digest returns the SHA-256 digest of the canonical envelope revision.
func (e SignalEnvelope) Digest() string { return digestString(e.Canonical()) }

// SequenceKind names the four high-risk sequences owned by SECARCH-008.
type SequenceKind string

const (
	SequenceRepeatedDLPRefusal     SequenceKind = "repeated_dlp_refusal"
	SequenceBreakGlassUse          SequenceKind = "break_glass_use"
	SequenceJITNearTTLCeiling      SequenceKind = "jit_near_ttl_ceiling"
	SequenceCrossTenantDenialBurst SequenceKind = "cross_tenant_denial_burst"
)

func (k SequenceKind) valid() bool {
	switch k {
	case SequenceRepeatedDLPRefusal, SequenceBreakGlassUse, SequenceJITNearTTLCeiling, SequenceCrossTenantDenialBurst:
		return true
	default:
		return false
	}
}

// Signal is one caller-timestamped event fed to the sequence detector. A JIT
// signal carries durations rather than a grant identifier; the detector
// decides "near" from the rule's percentage threshold.
type Signal struct {
	Kind       SequenceKind
	At         time.Time
	Envelope   SignalEnvelope
	TTLUsed    time.Duration
	TTLCeiling time.Duration
}

func (s Signal) validate() error {
	if !s.Kind.valid() {
		return reject("signal.kind", "unknown", SchemaVersion, ErrUnknownSequence)
	}
	if s.At.IsZero() {
		return reject("signal.at", "missing", SchemaVersion, ErrInvalidSignal)
	}
	if s.Envelope.Tag != TagSecurity {
		return reject("signal.envelope.tag", "not_security", SchemaVersion, ErrInvalidSignal)
	}
	if err := validateEnvelopeMetadata(s.Envelope); err != nil {
		return err
	}
	if s.Kind == SequenceJITNearTTLCeiling {
		if s.TTLUsed <= 0 {
			return reject("signal.ttl_used", "missing", SchemaVersion, ErrInvalidSignal)
		}
		if s.TTLCeiling <= 0 {
			return reject("signal.ttl_ceiling", "missing", SchemaVersion, ErrInvalidSignal)
		}
		if s.TTLUsed > s.TTLCeiling {
			return reject("signal.ttl_used", "exceeds_ceiling", SchemaVersion, ErrInvalidSignal)
		}
	} else if s.TTLUsed != 0 || s.TTLCeiling != 0 {
		return reject("signal.ttl_used", "unexpected", SchemaVersion, ErrInvalidSignal)
	}
	return nil
}

func validateEnvelopeMetadata(e SignalEnvelope) error {
	if err := validateBase(e.Base); err != nil {
		return err
	}
	if e.Tag != TagSecurity {
		return reject("envelope.tag", "not_security", SchemaVersion, ErrMissingSecurityMD)
	}
	if !validRetention(e.RetentionClass) {
		return reject("envelope.retention_class", "missing_or_unknown", SchemaVersion, ErrMissingSecurityMD)
	}
	if !e.HoldState.valid() {
		return reject("envelope.hold_state", "missing_or_unknown", SchemaVersion, ErrMissingSecurityMD)
	}
	if (e.HoldState == HoldActive || e.HoldState == HoldReleased) && strings.TrimSpace(e.HoldRef) == "" {
		return reject("envelope.hold_ref", "missing", SchemaVersion, ErrMissingSecurityMD)
	}
	return nil
}

// NewSignal constructs one high-risk signal and defensively copies the
// envelope. It is the preferred entry point for adapters of DLP,
// break-glass, JIT, and cross-tenant authorization evidence.
func NewSignal(kind SequenceKind, at time.Time, envelope SignalEnvelope) (Signal, error) {
	return NewSignalWithTTL(kind, at, envelope, 0, 0)
}

// NewSignalWithTTL constructs the JIT form with caller-supplied elapsed and
// ceiling durations. It still performs no clock read.
func NewSignalWithTTL(kind SequenceKind, at time.Time, envelope SignalEnvelope, used, ceiling time.Duration) (Signal, error) {
	s := Signal{Kind: kind, At: at.UTC(), Envelope: cloneSignalEnvelope(envelope), TTLUsed: used, TTLCeiling: ceiling}
	if err := s.validate(); err != nil {
		return Signal{}, err
	}
	return s, nil
}

func cloneSignalEnvelope(in SignalEnvelope) SignalEnvelope {
	in.Base = cloneEnvelope(in.Base)
	return in
}

// Canonical returns the deterministic representation of a signal revision.
func (s Signal) Canonical() string {
	return fmt.Sprintf("kind=%s;at=%s;ttl_used=%d;ttl_ceiling=%d;envelope=%s", s.Kind, s.At.UTC().Format(time.RFC3339Nano), s.TTLUsed, s.TTLCeiling, s.Envelope.Canonical())
}

// Digest returns the SHA-256 digest of a signal revision.
func (s Signal) Digest() string { return digestString(s.Canonical()) }

// AlertRoute is the destination category selected by a versioned rule.
type AlertRoute string

const (
	RouteSecurityOnCall AlertRoute = "security_on_call"
	RouteIncidentReview AlertRoute = "incident_review"
	RouteTenantSecurity AlertRoute = "tenant_security"
)

func (r AlertRoute) valid() bool {
	return r == RouteSecurityOnCall || r == RouteIncidentReview || r == RouteTenantSecurity
}

// AlertRule maps one named high-risk sequence to a routed alert. Threshold
// counts matching events; NearPercent applies only to JIT signals.
type AlertRule struct {
	ID          string
	Version     int
	Sequence    SequenceKind
	AlertName   string
	Route       AlertRoute
	Threshold   int
	NearPercent int
}

func (r AlertRule) validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return reject("alert_rule.id", "missing", SchemaVersion, ErrInvalidRule)
	}
	if r.Version <= 0 {
		return reject("alert_rule.version", "missing", SchemaVersion, ErrInvalidRule)
	}
	if !r.Sequence.valid() {
		return reject("alert_rule.sequence", "unknown", SchemaVersion, ErrInvalidRule)
	}
	if strings.TrimSpace(r.AlertName) == "" {
		return reject("alert_rule.alert_name", "missing", SchemaVersion, ErrInvalidRule)
	}
	if !r.Route.valid() {
		return reject("alert_rule.route", "unknown", SchemaVersion, ErrInvalidRule)
	}
	if r.Threshold <= 0 {
		return reject("alert_rule.threshold", "not_positive", SchemaVersion, ErrInvalidRule)
	}
	if r.Sequence == SequenceJITNearTTLCeiling && (r.NearPercent <= 0 || r.NearPercent > 100) {
		return reject("alert_rule.near_percent", "outside_1_to_100", SchemaVersion, ErrInvalidRule)
	}
	if r.Sequence != SequenceJITNearTTLCeiling && r.NearPercent != 0 {
		return reject("alert_rule.near_percent", "unexpected", SchemaVersion, ErrInvalidRule)
	}
	return nil
}

// DefaultAlertRules returns the immutable policy set for the four named
// sequences. A copy is returned so callers cannot mutate registry state.
func DefaultAlertRules() []AlertRule {
	return []AlertRule{
		{ID: "security.dlp-refusal-burst", Version: 1, Sequence: SequenceRepeatedDLPRefusal, AlertName: "Repeated DLP refusal", Route: RouteSecurityOnCall, Threshold: 3},
		{ID: "security.break-glass-use", Version: 1, Sequence: SequenceBreakGlassUse, AlertName: "Break-glass use", Route: RouteIncidentReview, Threshold: 1},
		{ID: "security.jit-near-ttl-ceiling", Version: 1, Sequence: SequenceJITNearTTLCeiling, AlertName: "JIT grant near TTL ceiling", Route: RouteSecurityOnCall, Threshold: 1, NearPercent: 80},
		{ID: "security.cross-tenant-denial-burst", Version: 1, Sequence: SequenceCrossTenantDenialBurst, AlertName: "Cross-tenant denial burst", Route: RouteTenantSecurity, Threshold: 3},
	}
}

// AlertRuleRegistry is an immutable, versioned registry of high-risk rules.
type AlertRuleRegistry struct{ rules map[SequenceKind]AlertRule }

// NewAlertRuleRegistry validates and freezes rules. There is exactly one rule
// per sequence so routing cannot be ambiguous.
func NewAlertRuleRegistry(rules ...AlertRule) (*AlertRuleRegistry, error) {
	if len(rules) == 0 {
		return nil, reject("alert_rules", "missing", SchemaVersion, ErrInvalidRule)
	}
	compiled := make(map[SequenceKind]AlertRule, len(rules))
	for _, rule := range rules {
		if err := rule.validate(); err != nil {
			return nil, err
		}
		if _, exists := compiled[rule.Sequence]; exists {
			return nil, reject("alert_rule.sequence", "duplicate", SchemaVersion, ErrInvalidRule)
		}
		compiled[rule.Sequence] = rule
	}
	return &AlertRuleRegistry{rules: compiled}, nil
}

// DefaultAlertRuleRegistry builds the published high-risk registry.
func DefaultAlertRuleRegistry() (*AlertRuleRegistry, error) {
	return NewAlertRuleRegistry(DefaultAlertRules()...)
}

// Lookup resolves a rule by named sequence.
func (r *AlertRuleRegistry) Lookup(sequence SequenceKind) (AlertRule, bool) {
	if r == nil {
		return AlertRule{}, false
	}
	rule, ok := r.rules[sequence]
	return rule, ok
}

// Rules returns a stable ID-sorted copy of the registry.
func (r *AlertRuleRegistry) Rules() []AlertRule {
	if r == nil {
		return nil
	}
	out := make([]AlertRule, 0, len(r.rules))
	for _, rule := range r.rules {
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Window is the explicit interval supplied to a detector. End is exclusive.
type Window struct{ Start, End time.Time }

func (w Window) validate() error {
	if w.Start.IsZero() {
		return reject("window.start", "missing", SchemaVersion, ErrInvalidWindow)
	}
	if w.End.IsZero() {
		return reject("window.end", "missing", SchemaVersion, ErrInvalidWindow)
	}
	if !w.Start.Before(w.End) {
		return reject("window", "not_increasing", SchemaVersion, ErrInvalidWindow)
	}
	return nil
}

// SignalPort is the read-only I/O seam for a source of already-emitted
// security signals. Implementations own persistence or transport; detection
// only asks for the caller-selected window.
type SignalPort interface {
	Signals(Window) ([]Signal, error)
}

// MemoryPort is a deterministic port for tests and offline composition.
type MemoryPort struct {
	mu      sync.RWMutex
	signals []Signal
}

// NewMemoryPort creates a port with defensively copied signals.
func NewMemoryPort(signals ...Signal) (*MemoryPort, error) {
	port := &MemoryPort{}
	for _, signal := range signals {
		if err := signal.validate(); err != nil {
			return nil, err
		}
		port.signals = append(port.signals, cloneSignal(signal))
	}
	return port, nil
}

// Add appends an already-emitted signal to the test/offline port.
func (p *MemoryPort) Add(signal Signal) error {
	if p == nil {
		return ErrInvalidPort
	}
	if err := signal.validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signals = append(p.signals, cloneSignal(signal))
	return nil
}

// Signals implements SignalPort and returns signals in the requested window.
func (p *MemoryPort) Signals(w Window) ([]Signal, error) {
	if p == nil {
		return nil, ErrInvalidPort
	}
	if err := w.validate(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Signal, 0)
	for _, signal := range p.signals {
		if !signal.At.Before(w.Start) && signal.At.Before(w.End) {
			out = append(out, cloneSignal(signal))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].Digest() < out[j].Digest()
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}

// RoutedAlert is an immutable alert decision produced from one window. It
// includes the rule version and an evidence digest, not raw event IDs.
type RoutedAlert struct {
	Revision       uint64
	PreviousDigest string
	RuleID         string
	RuleVersion    int
	Sequence       SequenceKind
	AlertName      string
	Route          AlertRoute
	WindowStart    time.Time
	WindowEnd      time.Time
	ObservedCount  int
	EvidenceDigest string
	Digest         string
}

func (a RoutedAlert) canonical() string {
	return fmt.Sprintf("revision=%d;previous=%s;rule=%s;rule_version=%d;sequence=%s;alert=%s;route=%s;start=%s;end=%s;count=%d;evidence=%s", a.Revision, a.PreviousDigest, a.RuleID, a.RuleVersion, a.Sequence, a.AlertName, a.Route, a.WindowStart.UTC().Format(time.RFC3339Nano), a.WindowEnd.UTC().Format(time.RFC3339Nano), a.ObservedCount, a.EvidenceDigest)
}

// DigestOfAlert computes the digest of an alert without trusting its stored
// Digest field.
func DigestOfAlert(a RoutedAlert) string { return digestString(a.canonical()) }

// SequenceDetector evaluates rules from a port without goroutines or clock
// reads. The caller owns when detection happens and supplies the window.
type SequenceDetector struct {
	port     SignalPort
	registry *AlertRuleRegistry
}

// NewSequenceDetector composes a read-only signal port with the immutable
// alert registry.
func NewSequenceDetector(port SignalPort, registry *AlertRuleRegistry) (*SequenceDetector, error) {
	if port == nil {
		return nil, ErrInvalidPort
	}
	if registry == nil {
		return nil, reject("alert_registry", "missing", SchemaVersion, ErrInvalidRule)
	}
	return &SequenceDetector{port: port, registry: registry}, nil
}

// Detect returns one routed alert per rule whose sequence threshold is met.
// JIT events count only when elapsed TTL meets the rule's NearPercent.
func (d *SequenceDetector) Detect(w Window) ([]RoutedAlert, error) {
	if d == nil || d.port == nil || d.registry == nil {
		return nil, ErrInvalidPort
	}
	if err := w.validate(); err != nil {
		return nil, err
	}
	signals, err := d.port.Signals(w)
	if err != nil {
		return nil, err
	}
	for index, signal := range signals {
		if err := signal.validate(); err != nil {
			return nil, reject(fmt.Sprintf("port.signals[%d]", index), "invalid", SchemaVersion, ErrInvalidSignal)
		}
	}
	alerts := make([]RoutedAlert, 0)
	var previousDigest string
	var revision uint64
	for _, rule := range d.registry.Rules() {
		matching := make([]Signal, 0)
		for _, signal := range signals {
			if signal.Kind != rule.Sequence {
				continue
			}
			if rule.Sequence == SequenceJITNearTTLCeiling && !nearCeiling(signal, rule.NearPercent) {
				continue
			}
			matching = append(matching, signal)
		}
		if len(matching) < rule.Threshold {
			continue
		}
		evidenceDigest := digestSignals(matching)
		revision++
		alert := RoutedAlert{
			Revision: revision, PreviousDigest: previousDigest,
			RuleID: rule.ID, RuleVersion: rule.Version, Sequence: rule.Sequence,
			AlertName: rule.AlertName, Route: rule.Route, WindowStart: w.Start.UTC(), WindowEnd: w.End.UTC(),
			ObservedCount: len(matching), EvidenceDigest: evidenceDigest,
		}
		alert.Digest = DigestOfAlert(alert)
		previousDigest = alert.Digest
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

func nearCeiling(signal Signal, percent int) bool {
	// Compute ceil(ceiling*percent/100) without multiplying a duration by
	// 100; time.Duration is an int64 and fuzzed inputs may approach its limit.
	quotient, remainder := signal.TTLCeiling/100, signal.TTLCeiling%100
	threshold := quotient*time.Duration(percent) + (remainder*time.Duration(percent)+99)/100
	return signal.TTLUsed >= threshold
}

func digestSignals(signals []Signal) string {
	digests := make([]string, 0, len(signals))
	for _, signal := range signals {
		digests = append(digests, signal.Digest())
	}
	sort.Strings(digests)
	return digestString(strings.Join(digests, "\n"))
}

// SignalRecord is an immutable, sequentially digested emission revision.
type SignalRecord struct {
	Revision       uint64
	PreviousDigest string
	Signal         Signal
	Digest         string
}

func (r SignalRecord) canonical() string {
	return fmt.Sprintf("revision=%d;previous=%s;signal=%s", r.Revision, r.PreviousDigest, r.Signal.Canonical())
}

// DigestOfRecord computes a record digest independently of the stored field.
func DigestOfRecord(r SignalRecord) string { return digestString(r.canonical()) }

func cloneSignal(in Signal) Signal {
	in.Envelope = cloneSignalEnvelope(in.Envelope)
	return in
}

// Ledger is an in-memory append-only evidence stream. A production adapter
// can implement the same record and export contract behind an I/O boundary.
type Ledger struct {
	mu      sync.RWMutex
	records []SignalRecord
}

func NewLedger() *Ledger { return &Ledger{} }

// Emit appends exactly one digested signal revision.
func (l *Ledger) Emit(signal Signal) (SignalRecord, error) {
	if l == nil {
		return SignalRecord{}, ErrInvalidLedger
	}
	if err := signal.validate(); err != nil {
		return SignalRecord{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := ""
	if len(l.records) > 0 {
		previous = l.records[len(l.records)-1].Digest
	}
	record := SignalRecord{Revision: uint64(len(l.records) + 1), PreviousDigest: previous, Signal: cloneSignal(signal)}
	record.Digest = DigestOfRecord(record)
	l.records = append(l.records, record)
	return record, nil
}

// Records returns a deep copy of the emission revisions.
func (l *Ledger) Records() []SignalRecord {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]SignalRecord, len(l.records))
	for i, record := range l.records {
		out[i] = cloneRecord(record)
	}
	return out
}

func cloneRecord(in SignalRecord) SignalRecord {
	in.Signal = cloneSignal(in.Signal)
	return in
}

// OfflineExport is the portable, hash-chained representation of emitted
// security signals. ExportDigest binds the complete ordered chain.
type OfflineExport struct {
	SchemaVersion int
	Records       []SignalRecord
	HeadDigest    string
	ExportDigest  string
}

func (l *Ledger) Export() OfflineExport {
	export := OfflineExport{SchemaVersion: ExportSchemaVersion, Records: l.Records()}
	if len(export.Records) > 0 {
		export.HeadDigest = export.Records[len(export.Records)-1].Digest
	}
	export.ExportDigest = digestString(exportCanonical(export))
	return export
}

func exportCanonical(export OfflineExport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "schema=%d;head=%s;records=%d;", export.SchemaVersion, export.HeadDigest, len(export.Records))
	for _, record := range export.Records {
		fmt.Fprintf(&b, "record=%s;stored=%s;", record.canonical(), record.Digest)
	}
	return b.String()
}

// Verify checks every revision, every record digest, the previous-digest
// links, the export head, and the export digest. A mutation of a copied
// record is therefore refused offline before evidence is relied upon.
func Verify(export OfflineExport) error {
	if export.SchemaVersion != ExportSchemaVersion {
		return reject("export.schema_version", "unknown", ExportSchemaVersion, ErrTamperedExport)
	}
	previous := ""
	for index, record := range export.Records {
		if err := record.Signal.validate(); err != nil {
			return reject(fmt.Sprintf("export.records[%d].signal", index), "invalid", ExportSchemaVersion, ErrTamperedExport)
		}
		if record.Revision != uint64(index+1) {
			return reject(fmt.Sprintf("export.records[%d].revision", index), "not_ordered", ExportSchemaVersion, ErrRevisionMismatch)
		}
		if record.PreviousDigest != previous {
			return reject(fmt.Sprintf("export.records[%d].previous_digest", index), "chain_break", ExportSchemaVersion, ErrTamperedExport)
		}
		if record.Digest == "" || record.Digest != DigestOfRecord(record) {
			return reject(fmt.Sprintf("export.records[%d].digest", index), "mutated", ExportSchemaVersion, ErrTamperedExport)
		}
		previous = record.Digest
	}
	if export.HeadDigest != previous {
		return reject("export.head_digest", "mismatch", ExportSchemaVersion, ErrTamperedExport)
	}
	if export.ExportDigest == "" || export.ExportDigest != digestString(exportCanonical(export)) {
		return reject("export.export_digest", "mutated", ExportSchemaVersion, ErrTamperedExport)
	}
	return nil
}

// VerifyExport is the descriptive alias used by offline tooling.
func VerifyExport(export OfflineExport) error { return Verify(export) }

// Verify checks this export value using the same offline path.
func (e OfflineExport) Verify() error { return Verify(e) }

// Explanation is an audit-safe view. It exposes no correlation, principal,
// hold, service-instance, tenant, account, secret, or payload identifier.
type Explanation struct {
	SchemaVersion int
	Revision      uint64
	RecordDigest  string
	SignalKind    SequenceKind
	Retention     RetentionClass
	HoldState     HoldState
	FieldNames    []string
}

// Explain returns a bounded audit-safe explanation of a signal record.
func (r SignalRecord) Explain() Explanation {
	return Explanation{
		SchemaVersion: SchemaVersion,
		Revision:      r.Revision,
		RecordDigest:  r.Digest,
		SignalKind:    r.Signal.Kind,
		Retention:     r.Signal.Envelope.RetentionClass,
		HoldState:     r.Signal.Envelope.HoldState,
		FieldNames: []string{
			"revision", "record_digest", "signal_kind", "retention_class", "hold_state",
		},
	}
}

// Explain is the top-level form for callers that prefer a function.
func Explain(r SignalRecord) Explanation { return r.Explain() }

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
