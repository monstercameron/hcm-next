package abuse

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// High-risk detection is intentionally a small, pure evaluation kernel. It
// consumes minimized observations and produces review evidence; it does not
// resolve references, persist findings, or make an employment decision.

var (
	ErrRiskVersion       = errors.New("abuse: invalid high-risk detector version")
	ErrRiskPolicy        = errors.New("abuse: invalid high-risk detector policy")
	ErrRiskEvent         = errors.New("abuse: invalid high-risk event")
	ErrRiskRawContent    = errors.New("abuse: high-risk event must not carry raw content")
	ErrRiskVersionFamily = errors.New("abuse: detector version family does not match its declared input")
)

// DetectorFamily identifies the kind of detector policy in a published
// high-risk detector version.
type DetectorFamily string

const (
	DetectorFamilyBulkExport    DetectorFamily = "BULK_EXPORT"
	DetectorFamilyPayrollChange DetectorFamily = "PAYROLL_CHANGE"
	DetectorFamilyAccessChange  DetectorFamily = "ACCESS_CHANGE"
)

func (f DetectorFamily) Valid() bool {
	switch f {
	case DetectorFamilyBulkExport, DetectorFamilyPayrollChange, DetectorFamilyAccessChange:
		return true
	default:
		return false
	}
}

// ChangeKind is the closed vocabulary of sensitive payroll and access
// changes. Ordinary merit, payroll and import work is represented explicitly
// so approved context can be evaluated without inspecting raw content.
type ChangeKind string

const (
	ChangeUnspecified       ChangeKind = ""
	ChangeBankDetail        ChangeKind = "BANK_DETAIL_CHANGE"
	ChangePayRate           ChangeKind = "PAY_RATE_CHANGE"
	ChangeNetPayRedirection ChangeKind = "NET_PAY_REDIRECTION"
	ChangeSelfGrant         ChangeKind = "SELF_GRANT"
	ChangeRoleEscalation    ChangeKind = "ROLE_ESCALATION"
	ChangeMeritBatch        ChangeKind = "MERIT_BATCH"
	ChangePayrollBatch      ChangeKind = "PAYROLL_BATCH"
	ChangeImportBatch       ChangeKind = "IMPORT_BATCH"
)

func (k ChangeKind) Valid() bool {
	switch k {
	case ChangeBankDetail, ChangePayRate, ChangeNetPayRedirection,
		ChangeSelfGrant, ChangeRoleEscalation, ChangeMeritBatch,
		ChangePayrollBatch, ChangeImportBatch:
		return true
	default:
		return false
	}
}

func (k ChangeKind) highRisk() bool {
	switch k {
	case ChangeBankDetail, ChangePayRate, ChangeNetPayRedirection,
		ChangeSelfGrant, ChangeRoleEscalation:
		return true
	default:
		return false
	}
}

// FindingSeverity is typed so callers cannot confuse severity with a free
// form label or an irreversible personnel outcome.
type FindingSeverity string

const (
	SeverityLow      FindingSeverity = "LOW"
	SeverityMedium   FindingSeverity = "MEDIUM"
	SeverityHigh     FindingSeverity = "HIGH"
	SeverityCritical FindingSeverity = "CRITICAL"
)

func (s FindingSeverity) Valid() bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

// FindingScope identifies the aggregation key used by a finding.
type FindingScope string

const (
	FindingScopeEvent     FindingScope = "EVENT"
	FindingScopePrincipal FindingScope = "PRINCIPAL"
	FindingScopeTenant    FindingScope = "TENANT"
)

// FindingCategory is the closed vocabulary of typed findings emitted by this
// ticket. The values carry no accusation semantics; they name evidence that
// may require governed review.
type FindingCategory string

const (
	FindingBulkExport       FindingCategory = "BULK_EXPORT"
	FindingBankDetailChange FindingCategory = "BANK_DETAIL_CHANGE"
	FindingPayRateChange    FindingCategory = "PAY_RATE_CHANGE"
	FindingNetPayRedirect   FindingCategory = "NET_PAY_REDIRECTION"
	FindingSelfGrant        FindingCategory = "SELF_GRANT"
	FindingRoleEscalation   FindingCategory = "ROLE_ESCALATION"
	FindingFakeWorker       FindingCategory = "FAKE_WORKER_PATTERN"
	FindingPrivilegeBurst   FindingCategory = "PRIVILEGE_BURST"
)

// RiskEvent is the minimized input accepted by the high-risk detectors. The
// Principal string is a stable opaque id; Actor is an equivalent typed
// reference for callers already using ABUSE-001's reference model. No worker,
// bank, compensation, role, or free-text content is accepted here.
type RiskEvent struct {
	ID         string
	Kind       SignalKind
	Subject    Ref
	Actor      Ref
	Principal  string
	Tenant     string
	ObservedAt time.Time

	// Volume is the number of records in a bulk export. It is ignored for
	// payroll/access events and must never be used to carry raw data.
	Volume int64
	Change ChangeKind

	// ApprovedContext marks a declared, approved batch or incident context.
	// Context remains typed evidence; it is not a free-text explanation.
	ApprovedContext bool
	RawContent      string // boundary-only refusal; never a supported input
}

// Validate enforces the minimized event contract.
func (e RiskEvent) Validate() error {
	if strings.TrimSpace(e.RawContent) != "" {
		return ErrRiskRawContent
	}
	if strings.TrimSpace(e.ID) == "" || !e.Kind.Valid() || strings.TrimSpace(e.Tenant) == "" || e.ObservedAt.IsZero() {
		return ErrRiskEvent
	}
	if strings.TrimSpace(e.Principal) == "" && !e.Actor.Valid() {
		return ErrRiskEvent
	}
	if e.Volume < 0 {
		return ErrRiskEvent
	}
	if e.Change != ChangeUnspecified && !e.Change.Valid() {
		return ErrRiskEvent
	}
	if e.Kind == SignalKindBulkExport {
		if e.Volume <= 0 || e.Change != ChangeUnspecified {
			return ErrRiskEvent
		}
	}
	if e.Kind == SignalKindPrivilegedChange || e.Kind == SignalKindAccessGrant {
		if !e.Change.Valid() || e.Volume != 0 {
			return ErrRiskEvent
		}
	}
	return nil
}

func (e RiskEvent) principalID() string {
	if id := strings.TrimSpace(e.Principal); id != "" {
		return id
	}
	return e.Actor.ID
}

func (e RiskEvent) subjectID() string {
	return e.Subject.ID
}

// BulkExportPolicy declares both volume and velocity limits for each scope.
// Velocity is records per second across the declared Window. A limit is
// exceeded strictly (equal-to-limit remains approved).
type BulkExportPolicy struct {
	Window                  time.Duration
	MaxVolumePerPrincipal   int64
	MaxVolumePerTenant      int64
	MaxVelocityPerPrincipal float64
	MaxVelocityPerTenant    float64
}

// PayrollChangePolicy declares the window used to detect repeated high-risk
// payroll changes affecting multiple workers. Individual bank, pay-rate and
// net-pay redirection events always produce typed findings unless approved.
type PayrollChangePolicy struct {
	Window                         time.Duration
	MaxChangesPerPrincipal         int64
	MaxDistinctWorkersPerPrincipal int64
}

// AccessChangePolicy declares the window used to detect a privilege burst.
// Individual self-grants and role escalations always produce typed findings
// unless approved.
type AccessChangePolicy struct {
	Window                 time.Duration
	MaxChangesPerPrincipal int64
	MaxChangesPerTenant    int64
}

// DetectionRules are the policy inputs for a high-risk detector version.
// Only the rule corresponding to RiskDetectorVersion.Family is used.
type DetectionRules struct {
	BulkExport    BulkExportPolicy
	PayrollChange PayrollChangePolicy
	AccessChange  AccessChangePolicy
}

func (p BulkExportPolicy) validate() error {
	if p.Window <= 0 || p.MaxVolumePerPrincipal <= 0 || p.MaxVolumePerTenant <= 0 || p.MaxVelocityPerPrincipal <= 0 || p.MaxVelocityPerTenant <= 0 {
		return ErrRiskPolicy
	}
	return nil
}

func (p PayrollChangePolicy) validate() error {
	if p.Window <= 0 || p.MaxChangesPerPrincipal <= 0 || p.MaxDistinctWorkersPerPrincipal <= 0 {
		return ErrRiskPolicy
	}
	return nil
}

func (p AccessChangePolicy) validate() error {
	if p.Window <= 0 || p.MaxChangesPerPrincipal <= 0 || p.MaxChangesPerTenant <= 0 {
		return ErrRiskPolicy
	}
	return nil
}

// RiskDetectorVersion binds one ABUSE-001 DetectorVersion to the typed rules
// used by its family. It is published by value, and its Digest is computed
// over the complete immutable version and policy body.
type RiskDetectorVersion struct {
	DetectorVersion
	Family DetectorFamily
	Rules  DetectionRules
	Digest string
}

func (v RiskDetectorVersion) Validate() error {
	if err := v.DetectorVersion.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrRiskVersion, err)
	}
	if !v.Family.Valid() {
		return fmt.Errorf("%w: family", ErrRiskVersion)
	}
	var required SignalKind
	var err error
	switch v.Family {
	case DetectorFamilyBulkExport:
		required, err = SignalKindBulkExport, v.Rules.BulkExport.validate()
	case DetectorFamilyPayrollChange:
		required, err = SignalKindPrivilegedChange, v.Rules.PayrollChange.validate()
	case DetectorFamilyAccessChange:
		required, err = SignalKindAccessGrant, v.Rules.AccessChange.validate()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRiskPolicy, err)
	}
	if v.ConsumesUndeclaredKind(required) {
		return fmt.Errorf("%w: %s", ErrRiskVersionFamily, required)
	}
	return nil
}

// NewRiskDetectorVersion validates and returns a complete, immutable-by-value
// detector release. The caller must provide the ABUSE-001 declared inputs,
// outputs, threshold references and activation instant in base.
func NewRiskDetectorVersion(base DetectorVersion, family DetectorFamily, rules DetectionRules) (RiskDetectorVersion, error) {
	base = cloneDetectorVersion(base)
	v := RiskDetectorVersion{DetectorVersion: base, Family: family, Rules: rules}
	if err := v.Validate(); err != nil {
		return RiskDetectorVersion{}, err
	}
	canon := struct {
		Base   DetectorVersion
		Family DetectorFamily
		Rules  DetectionRules
	}{base, family, rules}
	d, err := digest(canon)
	if err != nil {
		return RiskDetectorVersion{}, err
	}
	v.Digest = d
	return v, nil
}

// NewBulkExportDetectorVersion, NewPayrollChangeDetectorVersion and
// NewAccessChangeDetectorVersion are focused constructors for the three
// detector families. The base DetectorVersion remains the ABUSE-001 source of
// declared inputs, outputs, threshold references and activation metadata.
func NewBulkExportDetectorVersion(base DetectorVersion, policy BulkExportPolicy) (RiskDetectorVersion, error) {
	rules := DetectionRules{BulkExport: policy}
	return NewRiskDetectorVersion(base, DetectorFamilyBulkExport, rules)
}

func NewPayrollChangeDetectorVersion(base DetectorVersion, policy PayrollChangePolicy) (RiskDetectorVersion, error) {
	rules := DetectionRules{PayrollChange: policy}
	return NewRiskDetectorVersion(base, DetectorFamilyPayrollChange, rules)
}

func NewAccessChangeDetectorVersion(base DetectorVersion, policy AccessChangePolicy) (RiskDetectorVersion, error) {
	rules := DetectionRules{AccessChange: policy}
	return NewRiskDetectorVersion(base, DetectorFamilyAccessChange, rules)
}

func cloneDetectorVersion(v DetectorVersion) DetectorVersion {
	v.DeclaredInputs = append([]SignalKind(nil), v.DeclaredInputs...)
	v.DeclaredOutputs = append([]string(nil), v.DeclaredOutputs...)
	v.Thresholds = append([]ThresholdRef(nil), v.Thresholds...)
	return v
}

// VersionDigest returns the publication digest, or the empty string for a
// value not produced by NewRiskDetectorVersion.
func (v RiskDetectorVersion) VersionDigest() string { return v.Digest }

// RiskExplanation states whether an event is an input declared by a specific
// detector release. DeclaredInputs is copied into the explanation so audit
// callers can see the governing input contract alongside the result.
type RiskExplanation struct {
	DetectorID     string
	Semver         string
	DetectorDigest string
	SignalID       string
	SignalKind     SignalKind
	DeclaredInputs []SignalKind
	Applicable     bool
	Reason         string
}

const (
	RiskExplainEventInvalid     = "EVENT_INVALID"
	RiskExplainVersionInvalid   = "DETECTOR_VERSION_INVALID"
	RiskExplainNotDeclaredInput = "SIGNAL_KIND_NOT_DECLARED_INPUT"
	RiskExplainDeclaredInput    = "SIGNAL_KIND_DECLARED_INPUT"
)

// ExplainRisk names the declared input contract without evaluating a risk
// threshold. It is the high-risk counterpart to ABUSE-001 Explain.
func ExplainRisk(e RiskEvent, v RiskDetectorVersion) RiskExplanation {
	exp := RiskExplanation{
		DetectorID:     v.DetectorID,
		Semver:         v.Semver,
		DetectorDigest: v.Digest,
		SignalID:       e.ID,
		SignalKind:     e.Kind,
		DeclaredInputs: append([]SignalKind(nil), v.DeclaredInputs...),
	}
	if err := e.Validate(); err != nil {
		exp.Reason = RiskExplainEventInvalid
		return exp
	}
	if err := v.Validate(); err != nil {
		exp.Reason = RiskExplainVersionInvalid
		return exp
	}
	if v.ConsumesUndeclaredKind(e.Kind) {
		exp.Reason = RiskExplainNotDeclaredInput
		return exp
	}
	exp.Applicable = true
	exp.Reason = RiskExplainDeclaredInput
	return exp
}

// Explain is the method form used by a RiskDetector.
func (v RiskDetectorVersion) Explain(e RiskEvent) RiskExplanation { return ExplainRisk(e, v) }

// RiskEvidence contains only references and bounded aggregates. InputIDs are
// stable event ids, never the observed records themselves.
type RiskEvidence struct {
	Scope       FindingScope
	Principal   string
	Tenant      string
	WindowStart time.Time
	WindowEnd   time.Time
	InputIDs    []string
	EventCount  int64
	Volume      int64
}

// RiskFinding is a typed review signal. Severity is bounded and no field is a
// misconduct fact or an automatic employment action.
type RiskFinding struct {
	DetectorID     string
	DetectorSemver string
	DetectorDigest string
	SignalID       string
	Kind           SignalKind
	Category       FindingCategory
	Severity       FindingSeverity
	Evidence       RiskEvidence
}

// RiskDetection is the deterministic output of a pure detector evaluation.
// Explanations are retained for every input event, including approved and
// undeclared events, so callers can account for why an event was not used.
type RiskDetection struct {
	DetectorID     string
	DetectorSemver string
	DetectorDigest string
	Explanations   []RiskExplanation
	Findings       []RiskFinding
}

func (r RiskDetection) Accepted() bool { return len(r.Findings) == 0 }

// Short aliases follow the Activity/Result/Finding vocabulary used by the
// adjacent pure anomaly engine while retaining the explicit Risk-prefixed
// names for callers that use several abuse detectors together.
type Activity = RiskEvent
type Event = RiskEvent
type Finding = RiskFinding
type Result = RiskDetection

// RiskDetector is an immutable-by-value evaluator for one published release.
type RiskDetector struct{ version RiskDetectorVersion }

// Detector is a short compatibility alias for callers that use the generic
// detector name.
type Detector = RiskDetector

// NewRiskDetector creates a pure evaluator from a validated detector release.
func NewRiskDetector(v RiskDetectorVersion) (RiskDetector, error) {
	// Rebuild the release so a caller cannot mutate a previously returned
	// slice or stale Digest field into the evaluator's private version.
	canonical, err := NewRiskDetectorVersion(v.DetectorVersion, v.Family, v.Rules)
	if err != nil {
		return RiskDetector{}, err
	}
	return RiskDetector{version: canonical}, nil
}

// NewDetector is the concise constructor alias.
func NewDetector(v RiskDetectorVersion) (RiskDetector, error) { return NewRiskDetector(v) }

// Version returns a value copy of the release used by d.
func (d RiskDetector) Version() RiskDetectorVersion {
	v := d.version
	v.DetectorVersion = cloneDetectorVersion(v.DetectorVersion)
	return v
}

// Detect evaluates events in a deterministic order. A detector emits an
// event-level finding for each unapproved high-risk payroll/access change and
// aggregate findings for bulk volume/velocity, fake-worker, or privilege-burst
// windows.
func (d RiskDetector) Detect(events []RiskEvent) (RiskDetection, error) {
	if err := d.version.Validate(); err != nil {
		return RiskDetection{}, err
	}
	ordered := append([]RiskEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ObservedAt.Equal(ordered[j].ObservedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].ObservedAt.Before(ordered[j].ObservedAt)
	})
	out := RiskDetection{DetectorID: d.version.DetectorID, DetectorSemver: d.version.Semver, DetectorDigest: d.version.Digest}
	for _, e := range ordered {
		if err := e.Validate(); err != nil {
			return RiskDetection{}, fmt.Errorf("%w: %s: %v", ErrRiskEvent, e.ID, err)
		}
		out.Explanations = append(out.Explanations, d.version.Explain(e))
	}
	switch d.version.Family {
	case DetectorFamilyBulkExport:
		out.Findings = detectBulk(d.version, ordered)
	case DetectorFamilyPayrollChange:
		out.Findings = detectPayroll(d.version, ordered)
	case DetectorFamilyAccessChange:
		out.Findings = detectAccess(d.version, ordered)
	}
	return out, nil
}

// Detect is the package-level form for callers that do not retain a detector.
func Detect(d RiskDetector, events []RiskEvent) (RiskDetection, error) { return d.Detect(events) }

func applicable(v RiskDetectorVersion, e RiskEvent, kind SignalKind) bool {
	return !e.ApprovedContext && e.Kind == kind && !v.ConsumesUndeclaredKind(kind)
}

type aggregate struct {
	key     string
	ids     []string
	count   int64
	volume  int64
	start   time.Time
	end     time.Time
	workers map[string]struct{}
}

func windowEvents(events []RiskEvent, end time.Time, window time.Duration, keep func(RiskEvent) bool) []RiskEvent {
	start := end.Add(-window)
	var out []RiskEvent
	for _, e := range events {
		if !keep(e) || e.ObservedAt.Before(start) || e.ObservedAt.After(end) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func aggregateFor(events []RiskEvent, end time.Time, window time.Duration, scope FindingScope, key string, keep func(RiskEvent) bool) aggregate {
	a := aggregate{key: key, end: end, start: end.Add(-window), workers: map[string]struct{}{}}
	for _, e := range windowEvents(events, end, window, keep) {
		if (scope == FindingScopePrincipal && e.principalID() != key) || (scope == FindingScopeTenant && e.Tenant != key) {
			continue
		}
		a.ids = append(a.ids, e.ID)
		a.count++
		a.volume += e.Volume
		if worker := e.subjectID(); worker != "" {
			a.workers[worker] = struct{}{}
		}
	}
	return a
}

func evidence(a aggregate, scope FindingScope) RiskEvidence {
	return RiskEvidence{Scope: scope, Principal: func() string {
		if scope == FindingScopePrincipal {
			return a.key
		}
		return ""
	}(), Tenant: func() string {
		if scope == FindingScopeTenant {
			return a.key
		}
		return ""
	}(), WindowStart: a.start, WindowEnd: a.end, InputIDs: append([]string(nil), a.ids...), EventCount: a.count, Volume: a.volume}
}

func finding(v RiskDetectorVersion, eventID string, kind SignalKind, category FindingCategory, severity FindingSeverity, ev RiskEvidence) RiskFinding {
	return RiskFinding{DetectorID: v.DetectorID, DetectorSemver: v.Semver, DetectorDigest: v.Digest, SignalID: eventID, Kind: kind, Category: category, Severity: severity, Evidence: ev}
}

func detectBulk(v RiskDetectorVersion, events []RiskEvent) []RiskFinding {
	p := v.Rules.BulkExport
	var findings []RiskFinding
	seen := map[string]bool{}
	for _, e := range events {
		if !applicable(v, e, SignalKindBulkExport) {
			continue
		}
		for _, scope := range []FindingScope{FindingScopePrincipal, FindingScopeTenant} {
			key := e.Tenant
			maxVolume, maxVelocity := p.MaxVolumePerTenant, p.MaxVelocityPerTenant
			if scope == FindingScopePrincipal {
				key, maxVolume, maxVelocity = e.principalID(), p.MaxVolumePerPrincipal, p.MaxVelocityPerPrincipal
			}
			a := aggregateFor(events, e.ObservedAt, p.Window, scope, key, func(candidate RiskEvent) bool {
				return candidate.Kind == SignalKindBulkExport && !candidate.ApprovedContext
			})
			velocity := float64(a.volume) / p.Window.Seconds()
			if a.volume <= maxVolume && velocity <= maxVelocity {
				continue
			}
			signature := string(scope) + "\x00" + key + "\x00" + e.ObservedAt.UTC().Format(time.RFC3339Nano)
			if seen[signature] {
				continue
			}
			seen[signature] = true
			findings = append(findings, finding(v, e.ID, SignalKindBulkExport, FindingBulkExport, SeverityHigh, evidence(a, scope)))
		}
	}
	return findings
}

func detectPayroll(v RiskDetectorVersion, events []RiskEvent) []RiskFinding {
	p := v.Rules.PayrollChange
	var findings []RiskFinding
	seen := map[string]bool{}
	for _, e := range events {
		if !applicable(v, e, SignalKindPrivilegedChange) || !e.Change.highRisk() {
			continue
		}
		category := FindingBankDetailChange
		switch e.Change {
		case ChangePayRate:
			category = FindingPayRateChange
		case ChangeNetPayRedirection:
			category = FindingNetPayRedirect
		}
		findings = append(findings, finding(v, e.ID, SignalKindPrivilegedChange, category, SeverityHigh, evidence(aggregate{key: e.principalID(), ids: []string{e.ID}, count: 1, start: e.ObservedAt, end: e.ObservedAt, workers: map[string]struct{}{e.subjectID(): {}}}, FindingScopeEvent)))

		a := aggregateFor(events, e.ObservedAt, p.Window, FindingScopePrincipal, e.principalID(), func(candidate RiskEvent) bool {
			return candidate.Kind == SignalKindPrivilegedChange && candidate.Change.highRisk() && !candidate.ApprovedContext
		})
		if a.count > p.MaxChangesPerPrincipal || int64(len(a.workers)) > p.MaxDistinctWorkersPerPrincipal {
			signature := "payroll\x00" + e.principalID() + "\x00" + e.ObservedAt.UTC().Format(time.RFC3339Nano)
			if !seen[signature] {
				seen[signature] = true
				findings = append(findings, finding(v, e.ID, SignalKindPrivilegedChange, FindingFakeWorker, SeverityCritical, evidence(a, FindingScopePrincipal)))
			}
		}
	}
	return findings
}

func detectAccess(v RiskDetectorVersion, events []RiskEvent) []RiskFinding {
	p := v.Rules.AccessChange
	var findings []RiskFinding
	seen := map[string]bool{}
	for _, e := range events {
		if !applicable(v, e, SignalKindAccessGrant) || (e.Change != ChangeSelfGrant && e.Change != ChangeRoleEscalation) {
			continue
		}
		category, severity := FindingSelfGrant, SeverityCritical
		if e.Change == ChangeRoleEscalation {
			category, severity = FindingRoleEscalation, SeverityHigh
		}
		single := aggregate{key: e.principalID(), ids: []string{e.ID}, count: 1, start: e.ObservedAt, end: e.ObservedAt, workers: map[string]struct{}{}}
		findings = append(findings, finding(v, e.ID, SignalKindAccessGrant, category, severity, evidence(single, FindingScopeEvent)))

		principal := aggregateFor(events, e.ObservedAt, p.Window, FindingScopePrincipal, e.principalID(), func(candidate RiskEvent) bool {
			return candidate.Kind == SignalKindAccessGrant && (candidate.Change == ChangeSelfGrant || candidate.Change == ChangeRoleEscalation) && !candidate.ApprovedContext
		})
		tenant := aggregateFor(events, e.ObservedAt, p.Window, FindingScopeTenant, e.Tenant, func(candidate RiskEvent) bool {
			return candidate.Kind == SignalKindAccessGrant && (candidate.Change == ChangeSelfGrant || candidate.Change == ChangeRoleEscalation) && !candidate.ApprovedContext
		})
		if principal.count > p.MaxChangesPerPrincipal {
			signature := "access-principal\x00" + e.principalID() + "\x00" + e.ObservedAt.UTC().Format(time.RFC3339Nano)
			if !seen[signature] {
				seen[signature] = true
				findings = append(findings, finding(v, e.ID, SignalKindAccessGrant, FindingPrivilegeBurst, SeverityCritical, evidence(principal, FindingScopePrincipal)))
			}
		}
		if tenant.count > p.MaxChangesPerTenant {
			signature := "access-tenant\x00" + e.Tenant + "\x00" + e.ObservedAt.UTC().Format(time.RFC3339Nano)
			if !seen[signature] {
				seen[signature] = true
				findings = append(findings, finding(v, e.ID, SignalKindAccessGrant, FindingPrivilegeBurst, SeverityCritical, evidence(tenant, FindingScopeTenant)))
			}
		}
	}
	return findings
}
