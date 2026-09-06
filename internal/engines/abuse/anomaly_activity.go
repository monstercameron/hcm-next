package abuse

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

var (
	ErrAnomalyVersion = errors.New("abuse: invalid activity anomaly detector version")
	ErrAnomalyInput   = errors.New("abuse: invalid activity anomaly input")
	ErrAnomalyRaw     = errors.New("abuse: activity anomaly input must not carry raw content")
)

// ActivityAnomalyPolicy is the declared, immutable policy input for the
// ABUSE-002 evaluator. Baselines and subject scopes are opaque governance
// facts; the evaluator never resolves them to worker or record content.
type ActivityAnomalyPolicy struct {
	DetectorID     string
	Semver         string
	DeclaredInputs []SignalKind

	DeclaredHours     [2]int
	DeclaredLocations []string
	KnownPrincipals   []string

	PrivilegedWindow        time.Duration
	MaxPrivilegedOperations int64

	SensitiveWindow                time.Duration
	SensitiveVolumeBaseline        map[string]int64
	DefaultSensitiveVolumeBaseline int64
	SensitiveVolumeMultiplier      int64
	DeclaredSubjectScopes          map[string][]string
}

func (p ActivityAnomalyPolicy) Validate() error {
	if strings.TrimSpace(p.DetectorID) == "" || !semverPattern.MatchString(p.Semver) || len(p.DeclaredInputs) == 0 {
		return ErrAnomalyVersion
	}
	for _, kind := range p.DeclaredInputs {
		if !kind.Valid() {
			return ErrAnomalyVersion
		}
	}
	if p.DeclaredHours[0] < 0 || p.DeclaredHours[1] > 24 || p.DeclaredHours[0] >= p.DeclaredHours[1] || len(p.DeclaredLocations) == 0 {
		return ErrAnomalyVersion
	}
	for _, location := range p.DeclaredLocations {
		if strings.TrimSpace(location) == "" {
			return ErrAnomalyVersion
		}
	}
	if p.PrivilegedWindow <= 0 || p.MaxPrivilegedOperations <= 0 || p.SensitiveWindow <= 0 || p.SensitiveVolumeMultiplier <= 0 || p.DefaultSensitiveVolumeBaseline <= 0 {
		return ErrAnomalyVersion
	}
	for principal, baseline := range p.SensitiveVolumeBaseline {
		if strings.TrimSpace(principal) == "" || baseline <= 0 {
			return ErrAnomalyVersion
		}
	}
	for principal, subjects := range p.DeclaredSubjectScopes {
		if strings.TrimSpace(principal) == "" || len(subjects) == 0 {
			return ErrAnomalyVersion
		}
		for _, subject := range subjects {
			if strings.TrimSpace(subject) == "" {
				return ErrAnomalyVersion
			}
		}
	}
	return nil
}

// ActivityAnomalyDetectorVersion is a publication receipt for the complete
// ABUSE-002 policy. Its digest changes when any declared input, boundary,
// baseline or subject scope changes.
type ActivityAnomalyDetectorVersion struct {
	ActivityAnomalyPolicy
	Digest string
}

func NewActivityAnomalyDetectorVersion(policy ActivityAnomalyPolicy) (ActivityAnomalyDetectorVersion, error) {
	policy = cloneAnomalyPolicy(policy)
	if err := policy.Validate(); err != nil {
		return ActivityAnomalyDetectorVersion{}, err
	}
	d, err := digest(policy)
	if err != nil {
		return ActivityAnomalyDetectorVersion{}, err
	}
	return ActivityAnomalyDetectorVersion{ActivityAnomalyPolicy: policy, Digest: d}, nil
}

func cloneAnomalyPolicy(p ActivityAnomalyPolicy) ActivityAnomalyPolicy {
	p.DeclaredInputs = append([]SignalKind(nil), p.DeclaredInputs...)
	p.DeclaredLocations = append([]string(nil), p.DeclaredLocations...)
	p.KnownPrincipals = append([]string(nil), p.KnownPrincipals...)
	p.SensitiveVolumeBaseline = cloneInt64Map(p.SensitiveVolumeBaseline)
	p.DeclaredSubjectScopes = cloneStringMap(p.DeclaredSubjectScopes)
	return p
}

func cloneInt64Map(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

// ActivityAnomalyInput is a minimized observation. It contains references,
// times and bounded counts only; raw records and content are prohibited.
type ActivityAnomalyInput struct {
	ID         string
	Principal  string
	Tenant     string
	Subject    string
	Kind       SignalKind
	ObservedAt time.Time
	Hour       int
	Location   string
	Volume     int64

	ApprovedContext bool
	RawContent      string
}

func (a ActivityAnomalyInput) Validate() error {
	if strings.TrimSpace(a.RawContent) != "" {
		return ErrAnomalyRaw
	}
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Principal) == "" || strings.TrimSpace(a.Tenant) == "" || strings.TrimSpace(a.Subject) == "" || !a.Kind.Valid() || a.ObservedAt.IsZero() || a.Hour < 0 || a.Hour > 23 || a.Volume < 0 {
		return ErrAnomalyInput
	}
	if a.Kind == SignalKindSensitiveRead && a.Volume <= 0 {
		return ErrAnomalyInput
	}
	if a.Kind != SignalKindSensitiveRead && a.Kind != SignalKindPrivilegedChange && a.Kind != SignalKindAccessGrant {
		return ErrAnomalyInput
	}
	return nil
}

type ActivityAnomalyCategory string

const (
	AnomalyPrivilegedOutsideHours ActivityAnomalyCategory = "PRIVILEGED_OUTSIDE_DECLARED_HOURS"
	AnomalyPrivilegedNewPrincipal ActivityAnomalyCategory = "PRIVILEGED_NEW_PRINCIPAL"
	AnomalyPrivilegedBurst        ActivityAnomalyCategory = "PRIVILEGED_BURST"
	AnomalySensitiveVolume        ActivityAnomalyCategory = "SENSITIVE_VOLUME_BASELINE_DEVIATION"
	AnomalySensitiveOutOfScope    ActivityAnomalyCategory = "SENSITIVE_SUBJECT_OUTSIDE_SCOPE"
)

type ActivityAnomalyEvidence struct {
	InputIDs    []string
	WindowStart time.Time
	WindowEnd   time.Time
	EventCount  int64
	Volume      int64
}

// ActivityAnomalyFinding is a typed review signal. It does not identify an
// accused person, carry raw content, or perform an employment action.
type ActivityAnomalyFinding struct {
	DetectorID     string
	DetectorSemver string
	DetectorDigest string
	ActivityID     string
	Principal      string
	Tenant         string
	Subject        string
	Category       ActivityAnomalyCategory
	Severity       FindingSeverity
	OffendingField string
	State          string
	Evidence       ActivityAnomalyEvidence
}

type ActivityAnomalyDetection struct {
	DetectorID     string
	DetectorSemver string
	DetectorDigest string
	Explanations   []ActivityAnomalyExplanation
	Findings       []ActivityAnomalyFinding
}

func (d ActivityAnomalyDetection) Accepted() bool { return len(d.Findings) == 0 }

// ActivityAnomalyExplanation names the declared inputs and boundaries used by
// an evaluation. It contains no raw content and no resolved subject data.
type ActivityAnomalyExplanation struct {
	DetectorID        string
	Semver            string
	DetectorDigest    string
	ActivityID        string
	DeclaredInputs    []SignalKind
	DeclaredHours     [2]int
	DeclaredLocations []string
	PrivilegedWindow  time.Duration
	SensitiveWindow   time.Duration
	Applicable        bool
	Reason            string
}

const (
	AnomalyExplainInputInvalid   = "INPUT_INVALID"
	AnomalyExplainVersionInvalid = "DETECTOR_VERSION_INVALID"
	AnomalyExplainUndeclaredKind = "SIGNAL_KIND_NOT_DECLARED_INPUT"
	AnomalyExplainDeclaredKind   = "SIGNAL_KIND_DECLARED_INPUT"
)

func ExplainActivityAnomaly(a ActivityAnomalyInput, v ActivityAnomalyDetectorVersion) ActivityAnomalyExplanation {
	exp := ActivityAnomalyExplanation{
		DetectorID: v.DetectorID, Semver: v.Semver, DetectorDigest: v.Digest,
		ActivityID: a.ID, DeclaredInputs: append([]SignalKind(nil), v.DeclaredInputs...),
		DeclaredHours: v.DeclaredHours, DeclaredLocations: append([]string(nil), v.DeclaredLocations...),
		PrivilegedWindow: v.PrivilegedWindow, SensitiveWindow: v.SensitiveWindow,
	}
	if err := a.Validate(); err != nil {
		exp.Reason = AnomalyExplainInputInvalid
		return exp
	}
	if err := v.Validate(); err != nil {
		exp.Reason = AnomalyExplainVersionInvalid
		return exp
	}
	for _, kind := range v.DeclaredInputs {
		if kind == a.Kind {
			exp.Applicable = true
			exp.Reason = AnomalyExplainDeclaredKind
			return exp
		}
	}
	exp.Reason = AnomalyExplainUndeclaredKind
	return exp
}

// ActivityAnomalyDetector is an immutable-by-value pure evaluator.
type ActivityAnomalyDetector struct {
	version ActivityAnomalyDetectorVersion
}

func NewActivityAnomalyDetector(v ActivityAnomalyDetectorVersion) (ActivityAnomalyDetector, error) {
	canonical, err := NewActivityAnomalyDetectorVersion(v.ActivityAnomalyPolicy)
	if err != nil {
		return ActivityAnomalyDetector{}, err
	}
	return ActivityAnomalyDetector{version: canonical}, nil
}

func (d ActivityAnomalyDetector) Version() ActivityAnomalyDetectorVersion {
	v := d.version
	v.ActivityAnomalyPolicy = cloneAnomalyPolicy(v.ActivityAnomalyPolicy)
	return v
}

func (d ActivityAnomalyDetector) Explain(a ActivityAnomalyInput) ActivityAnomalyExplanation {
	return ExplainActivityAnomaly(a, d.version)
}

func (d ActivityAnomalyDetector) Detect(inputs []ActivityAnomalyInput) (ActivityAnomalyDetection, error) {
	if err := d.version.Validate(); err != nil {
		return ActivityAnomalyDetection{}, err
	}
	ordered := append([]ActivityAnomalyInput(nil), inputs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ObservedAt.Equal(ordered[j].ObservedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].ObservedAt.Before(ordered[j].ObservedAt)
	})
	out := ActivityAnomalyDetection{DetectorID: d.version.DetectorID, DetectorSemver: d.version.Semver, DetectorDigest: d.version.Digest}
	for _, input := range ordered {
		if err := input.Validate(); err != nil {
			return ActivityAnomalyDetection{}, fmt.Errorf("%w: %s: %w", ErrAnomalyInput, input.ID, err)
		}
		out.Explanations = append(out.Explanations, d.Explain(input))
	}
	for _, input := range ordered {
		if input.ApprovedContext || !declaredAnomalyInput(d.version, input.Kind) {
			continue
		}
		switch input.Kind {
		case SignalKindPrivilegedChange, SignalKindAccessGrant:
			out.Findings = append(out.Findings, d.privilegedFindings(input, ordered)...)
		case SignalKindSensitiveRead:
			out.Findings = append(out.Findings, d.sensitiveFindings(input, ordered)...)
		}
	}
	return out, nil
}

func declaredAnomalyInput(v ActivityAnomalyDetectorVersion, kind SignalKind) bool {
	for _, declared := range v.DeclaredInputs {
		if declared == kind {
			return true
		}
	}
	return false
}

func (d ActivityAnomalyDetector) privilegedFindings(input ActivityAnomalyInput, all []ActivityAnomalyInput) []ActivityAnomalyFinding {
	var findings []ActivityAnomalyFinding
	if input.Hour < d.version.DeclaredHours[0] || input.Hour >= d.version.DeclaredHours[1] {
		findings = append(findings, d.finding(input, AnomalyPrivilegedOutsideHours, SeverityHigh, "declared_hours", singleEvidence(input)))
	}
	if !containsString(d.version.KnownPrincipals, input.Principal) {
		findings = append(findings, d.finding(input, AnomalyPrivilegedNewPrincipal, SeverityHigh, "known_principals", singleEvidence(input)))
	}
	if !containsString(d.version.DeclaredLocations, input.Location) {
		findings = append(findings, d.finding(input, AnomalyPrivilegedOutsideHours, SeverityHigh, "declared_location", singleEvidence(input)))
	}
	window := windowAnomalyInputs(all, input, d.version.PrivilegedWindow, func(candidate ActivityAnomalyInput) bool {
		return !candidate.ApprovedContext && (candidate.Kind == SignalKindPrivilegedChange || candidate.Kind == SignalKindAccessGrant) && candidate.Principal == input.Principal
	})
	if int64(len(window)) > d.version.MaxPrivilegedOperations {
		findings = append(findings, d.finding(input, AnomalyPrivilegedBurst, SeverityCritical, "rolling_privileged_operations", aggregateEvidence(window, input, d.version.PrivilegedWindow)))
	}
	return findings
}

func (d ActivityAnomalyDetector) sensitiveFindings(input ActivityAnomalyInput, all []ActivityAnomalyInput) []ActivityAnomalyFinding {
	var findings []ActivityAnomalyFinding
	window := windowAnomalyInputs(all, input, d.version.SensitiveWindow, func(candidate ActivityAnomalyInput) bool {
		return !candidate.ApprovedContext && candidate.Kind == SignalKindSensitiveRead && candidate.Principal == input.Principal
	})
	baseline := d.version.DefaultSensitiveVolumeBaseline
	if specific, ok := d.version.SensitiveVolumeBaseline[input.Principal]; ok {
		baseline = specific
	}
	threshold := baseline
	if baseline <= math.MaxInt64/d.version.SensitiveVolumeMultiplier {
		threshold = baseline * d.version.SensitiveVolumeMultiplier
	}
	var volume int64
	for _, event := range window {
		volume += event.Volume
	}
	if volume > threshold {
		findings = append(findings, d.finding(input, AnomalySensitiveVolume, SeverityHigh, "rolling_sensitive_volume", aggregateEvidence(window, input, d.version.SensitiveWindow)))
	}
	if !containsString(d.version.DeclaredSubjectScopes[input.Principal], input.Subject) {
		findings = append(findings, d.finding(input, AnomalySensitiveOutOfScope, SeverityHigh, "declared_subject_scope", singleEvidence(input)))
	}
	return findings
}

func windowAnomalyInputs(all []ActivityAnomalyInput, end ActivityAnomalyInput, window time.Duration, keep func(ActivityAnomalyInput) bool) []ActivityAnomalyInput {
	start := end.ObservedAt.Add(-window)
	var out []ActivityAnomalyInput
	for _, input := range all {
		if keep(input) && !input.ObservedAt.Before(start) && !input.ObservedAt.After(end.ObservedAt) {
			out = append(out, input)
		}
	}
	return out
}

func singleEvidence(input ActivityAnomalyInput) ActivityAnomalyEvidence {
	return ActivityAnomalyEvidence{InputIDs: []string{input.ID}, WindowStart: input.ObservedAt, WindowEnd: input.ObservedAt, EventCount: 1, Volume: input.Volume}
}

func aggregateEvidence(inputs []ActivityAnomalyInput, end ActivityAnomalyInput, window time.Duration) ActivityAnomalyEvidence {
	evidence := ActivityAnomalyEvidence{WindowStart: end.ObservedAt.Add(-window), WindowEnd: end.ObservedAt, EventCount: int64(len(inputs))}
	for _, input := range inputs {
		evidence.InputIDs = append(evidence.InputIDs, input.ID)
		evidence.Volume += input.Volume
	}
	return evidence
}

func (d ActivityAnomalyDetector) finding(input ActivityAnomalyInput, category ActivityAnomalyCategory, severity FindingSeverity, field string, evidence ActivityAnomalyEvidence) ActivityAnomalyFinding {
	return ActivityAnomalyFinding{DetectorID: d.version.DetectorID, DetectorSemver: d.version.Semver, DetectorDigest: d.version.Digest, ActivityID: input.ID, Principal: input.Principal, Tenant: input.Tenant, Subject: input.Subject, Category: category, Severity: severity, OffendingField: field, State: "REVIEW_REQUIRED", Evidence: evidence}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
