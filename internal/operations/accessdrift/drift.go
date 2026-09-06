// Package accessdrift compares Access-owned desired state with provider
// observations. It creates bounded repair plans only; it never writes an
// account, device, entitlement or badge and never reruns a parent employment
// transaction.
package accessdrift

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/observe"
)

const Version = 1

var (
	ErrInvalidRequest   = errors.New("accessdrift: invalid request")
	ErrFreshObservation = errors.New("accessdrift: a fresh complete observation is required")
	ErrRepairParent     = errors.New("accessdrift: repair may not rerun a parent transaction")
	ErrRepairConflict   = errors.New("accessdrift: repair plan conflicts with the supplied report")
)

// ResourceKind keeps logical accounts, entitlements, managed devices and
// physical badges distinct even though comparison mechanics are shared.
type ResourceKind string

const (
	KindAccount     ResourceKind = "ACCOUNT"
	KindEntitlement ResourceKind = "ENTITLEMENT"
	KindDevice      ResourceKind = "DEVICE"
	KindBadge       ResourceKind = "BADGE"
)

func (k ResourceKind) Valid() bool {
	switch k {
	case KindAccount, KindEntitlement, KindDevice, KindBadge:
		return true
	default:
		return false
	}
}

// Resource is one expected or observed access object. State is the provider
// state token, while Value is the normalized account/grant/custody value being
// compared. Neither is interpreted as a workforce fact.
type Resource struct {
	ID              string
	Kind            ResourceKind
	Subject         string
	Value           string
	State           string
	Risk            Risk
	ProviderVersion string
	ObservedAt      time.Time
	Freshness       observe.Freshness
	Complete        bool
}

// Risk is closed because privileged drift has a mandatory repair posture.
type Risk string

const (
	RiskLow        Risk = "LOW"
	RiskHigh       Risk = "HIGH"
	RiskPrivileged Risk = "PRIVILEGED"
)

func (r Risk) Valid() bool { return r == RiskLow || r == RiskHigh || r == RiskPrivileged }

// ReconcileRequest is a point-in-time desired-versus-observed comparison.
type ReconcileRequest struct {
	Tenant   string
	AsOf     time.Time
	Expected []Resource
	Observed []Resource
}

// DriftStatus is the exact per-resource result vocabulary required by
// ACCESS-004.
type DriftStatus string

const (
	StatusMatch   DriftStatus = "MATCH"
	StatusMissing DriftStatus = "MISSING"
	StatusExcess  DriftStatus = "EXCESS"
	StatusPartial DriftStatus = "PARTIAL"
	StatusStale   DriftStatus = "STALE"
	StatusUnknown DriftStatus = "UNKNOWN"
)

func (s DriftStatus) Valid() bool {
	switch s {
	case StatusMatch, StatusMissing, StatusExcess, StatusPartial, StatusStale, StatusUnknown:
		return true
	default:
		return false
	}
}

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityAction   Severity = "ACTION_REQUIRED"
	SeverityCritical Severity = "CRITICAL"
)

// Finding is a redaction-safe comparison result with digested values.
type Finding struct {
	ID              string
	ResourceID      string
	Kind            ResourceKind
	Subject         string
	Status          DriftStatus
	Severity        Severity
	Risk            Risk
	ExpectedDigest  string
	ObservedDigest  string
	ProviderVersion string
	Reason          string
}

// Report is immutable comparison evidence. A report is not a repair plan.
type Report struct {
	Tenant          string
	AsOf            time.Time
	Freshness       observe.Freshness
	Complete        bool
	Findings        []Finding
	CanonicalDigest string
}

// RepairAction is the intended operation; provider execution belongs to the
// integration plane after a separate approval/effect decision.
type RepairAction string

const (
	RepairGrant       RepairAction = "GRANT"
	RepairRevoke      RepairAction = "REVOKE"
	RepairInvestigate RepairAction = "INVESTIGATE"
)

type RepairStep struct {
	FindingID  string
	ResourceID string
	Kind       ResourceKind
	Action     RepairAction
	Risk       Risk
}

// RepairRequest deliberately contains no parent employment transaction ID
// that can be replayed. ParentTransactionID is retained only as a guard for
// adapters that need to prove it was not supplied.
type RepairRequest struct {
	Report              Report
	FindingIDs          []string
	ParentTransactionID string
	Actor               string
	IdempotencyKey      string
	At                  time.Time
	Freshness           observe.Freshness
	Complete            bool
}

type RepairPlan struct {
	PlanID           string
	Tenant           string
	ReportDigest     string
	Steps            []RepairStep
	RequiresApproval bool
	IdempotencyKey   string
	At               time.Time
	CanonicalDigest  string
}

func validateResource(r Resource, observed bool) error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Subject) == "" || !r.Kind.Valid() || strings.TrimSpace(r.State) == "" {
		return fmt.Errorf("%w: resource identity, kind and state are required", ErrInvalidRequest)
	}
	if !r.Risk.Valid() {
		return fmt.Errorf("%w: resource risk is not declared", ErrInvalidRequest)
	}
	if observed {
		if !r.Freshness.Valid() || r.ObservedAt.IsZero() || r.ProviderVersion == "" {
			return fmt.Errorf("%w: observed resource requires freshness, provider version and time", ErrInvalidRequest)
		}
	}
	return nil
}

// Reconcile compares one complete expected graph with one provider
// observation. Any non-fresh or incomplete evidence remains UNKNOWN/STALE;
// it can never be reported as MATCH.
func Reconcile(req ReconcileRequest) (Report, error) {
	if strings.TrimSpace(req.Tenant) == "" || req.AsOf.IsZero() {
		return Report{}, fmt.Errorf("%w: tenant and as_of are required", ErrInvalidRequest)
	}
	expected := make(map[string]Resource, len(req.Expected))
	for _, resource := range req.Expected {
		if err := validateResource(resource, false); err != nil {
			return Report{}, err
		}
		key := resourceKey(resource)
		if _, exists := expected[key]; exists {
			return Report{}, fmt.Errorf("%w: duplicate expected resource", ErrInvalidRequest)
		}
		expected[key] = resource
	}
	observed := make(map[string]Resource, len(req.Observed))
	for _, resource := range req.Observed {
		if err := validateResource(resource, true); err != nil {
			return Report{}, err
		}
		key := resourceKey(resource)
		if _, exists := observed[key]; exists {
			return Report{}, fmt.Errorf("%w: duplicate observed resource", ErrInvalidRequest)
		}
		observed[key] = resource
	}
	keys := make([]string, 0, len(expected)+len(observed))
	seen := make(map[string]struct{})
	for key := range expected {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range observed {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	report := Report{Tenant: req.Tenant, AsOf: req.AsOf.UTC(), Complete: true, Freshness: observe.FreshnessFresh}
	if len(req.Observed) == 0 {
		report.Freshness, report.Complete = observe.FreshnessUnknown, false
	}
	for _, key := range keys {
		want, hasWant := expected[key]
		got, hasGot := observed[key]
		finding := Finding{ID: "drift/" + digestParts(key)[:24], Kind: kindOf(want, got), ResourceID: idOf(want, got), Subject: subjectOf(want, got), Severity: SeverityInfo, Risk: riskOf(want, got)}
		if hasGot {
			if got.Freshness != observe.FreshnessFresh {
				report.Freshness = lessFresh(report.Freshness, got.Freshness)
			}
			if !got.Complete {
				report.Complete = false
			}
			finding.ProviderVersion = got.ProviderVersion
			finding.ObservedDigest = digestResource(got)
		}
		if hasWant {
			finding.ExpectedDigest = digestResource(want)
		}
		switch {
		case !hasGot:
			if report.Freshness != observe.FreshnessFresh || !report.Complete {
				finding.Status, finding.Reason = StatusUnknown, "observation is not fresh and complete"
			} else {
				finding.Status, finding.Reason = StatusMissing, "expected resource was not observed"
			}
		case !hasWant:
			finding.Status, finding.Reason = StatusExcess, "provider observed a resource outside desired state"
		case got.Freshness != observe.FreshnessFresh:
			finding.Status, finding.Reason = StatusStale, "provider observation is not fresh"
		case !got.Complete:
			finding.Status, finding.Reason = StatusPartial, "provider observation is incomplete"
		case got.State != want.State || got.Value != want.Value:
			finding.Status, finding.Reason = StatusPartial, "observed state differs from desired state"
		default:
			finding.Status, finding.Reason = StatusMatch, "fresh observation matches desired state"
		}
		if finding.Status != StatusMatch {
			finding.Severity = SeverityAction
		}
		if finding.Risk == RiskPrivileged && (finding.Status == StatusExcess || (finding.Status == StatusPartial && hasWant && want.State == "REVOKED")) {
			finding.Severity = SeverityCritical
		}
		report.Findings = append(report.Findings, finding)
	}
	report.CanonicalDigest = digestReport(report)
	return report, nil
}

// Detect is a descriptive alias for Reconcile.
func Detect(req ReconcileRequest) (Report, error) { return Reconcile(req) }

func CreateRepairPlan(req RepairRequest) (RepairPlan, error) {
	if strings.TrimSpace(req.Report.Tenant) == "" || req.Report.CanonicalDigest == "" || strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return RepairPlan{}, fmt.Errorf("%w: report, actor, idempotency key and time are required", ErrInvalidRequest)
	}
	if req.ParentTransactionID != "" {
		return RepairPlan{}, ErrRepairParent
	}
	if req.Freshness != observe.FreshnessFresh || !req.Complete || req.Report.Freshness != observe.FreshnessFresh || !req.Report.Complete {
		return RepairPlan{}, ErrFreshObservation
	}
	byID := make(map[string]Finding, len(req.Report.Findings))
	for _, finding := range req.Report.Findings {
		byID[finding.ID] = finding
	}
	ids := append([]string(nil), req.FindingIDs...)
	sort.Strings(ids)
	if len(ids) == 0 {
		return RepairPlan{}, fmt.Errorf("%w: at least one finding is required", ErrInvalidRequest)
	}
	plan := RepairPlan{Tenant: req.Report.Tenant, ReportDigest: req.Report.CanonicalDigest, IdempotencyKey: req.IdempotencyKey, At: req.At.UTC(), PlanID: "repair/" + digestParts(req.Report.CanonicalDigest, req.IdempotencyKey)[:24]}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return RepairPlan{}, fmt.Errorf("%w: duplicate finding", ErrInvalidRequest)
		}
		seen[id] = struct{}{}
		finding, ok := byID[id]
		if !ok || finding.Status == StatusMatch || finding.Status == StatusStale || finding.Status == StatusUnknown {
			return RepairPlan{}, ErrRepairConflict
		}
		action := RepairInvestigate
		if finding.Status == StatusMissing {
			action = RepairGrant
		}
		if finding.Status == StatusExcess || (finding.Status == StatusPartial && finding.ExpectedDigest != "") {
			action = RepairRevoke
		}
		plan.Steps = append(plan.Steps, RepairStep{FindingID: finding.ID, ResourceID: finding.ResourceID, Kind: finding.Kind, Action: action, Risk: finding.Risk})
		if finding.Risk != RiskLow {
			plan.RequiresApproval = true
		}
	}
	plan.CanonicalDigest = digestPlan(plan)
	return plan, nil
}

func resourceKey(r Resource) string { return string(r.Kind) + "\x00" + r.ID + "\x00" + r.Subject }
func idOf(a, b Resource) string {
	if a.ID != "" {
		return a.ID
	}
	return b.ID
}
func subjectOf(a, b Resource) string {
	if a.Subject != "" {
		return a.Subject
	}
	return b.Subject
}
func kindOf(a, b Resource) ResourceKind {
	if a.Kind.Valid() {
		return a.Kind
	}
	return b.Kind
}
func riskOf(a, b Resource) Risk {
	if a.Risk.Valid() {
		return a.Risk
	}
	return b.Risk
}
func lessFresh(a, b observe.Freshness) observe.Freshness {
	if freshnessRank(b) > freshnessRank(a) {
		return b
	}
	return a
}
func freshnessRank(v observe.Freshness) int {
	switch v {
	case observe.FreshnessFresh:
		return 0
	case observe.FreshnessStale:
		return 1
	case observe.FreshnessPartial:
		return 2
	case observe.FreshnessUnknown:
		return 3
	default:
		return 4
	}
}
func digestParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		_, _ = h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func digestResource(r Resource) string {
	return digestParts("resource.v1", string(r.Kind), r.ID, r.Subject, r.Value, r.State, string(r.Risk), r.ProviderVersion, r.ObservedAt.UTC().Format(time.RFC3339Nano), string(r.Freshness), fmt.Sprint(r.Complete))
}
func digestReport(r Report) string {
	parts := []string{"report.v1", r.Tenant, r.AsOf.UTC().Format(time.RFC3339Nano), string(r.Freshness), fmt.Sprint(r.Complete)}
	for _, f := range r.Findings {
		parts = append(parts, f.ID, f.ExpectedDigest, f.ObservedDigest, string(f.Status), string(f.Severity), string(f.Risk), f.ProviderVersion)
	}
	return digestParts(parts...)
}
func digestPlan(p RepairPlan) string {
	parts := []string{"repair-plan.v1", p.Tenant, p.ReportDigest, p.IdempotencyKey, p.At.UTC().Format(time.RFC3339Nano), fmt.Sprint(p.RequiresApproval)}
	for _, s := range p.Steps {
		parts = append(parts, s.FindingID, s.ResourceID, string(s.Kind), string(s.Action), string(s.Risk))
	}
	return digestParts(parts...)
}

// Explain reports only counts and governance state.
func (r Report) Explain() string {
	return fmt.Sprintf("access drift freshness=%s complete=%t findings=%d digest=%s", r.Freshness, r.Complete, len(r.Findings), r.CanonicalDigest)
}
func (p RepairPlan) Explain() string {
	return fmt.Sprintf("access repair steps=%d approval_required=%t digest=%s", len(p.Steps), p.RequiresApproval, p.CanonicalDigest)
}
func Explain(r Report) string { return r.Explain() }
