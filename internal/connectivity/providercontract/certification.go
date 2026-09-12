package providercontract

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// CONN-RT-008 certifies connector maturity as a pure, versioned decision
// over supplied evidence. It never reaches a real connector, provider or
// store: certification consumes evidence as an input, exactly as CICD-006's
// release decision (tools/policy/release/decision.go) consumes its eleven
// evidence classes. This file adds no second digest or health-evaluation
// mechanism of its own beyond that same discipline, applied to ten
// connector-conformance evidence classes instead of release evidence.
//
// The defining property: an expired credential is a rejection, not a pass.
// Every evidence class therefore carries an observation timestamp and an
// expiry, never a bare Passed bool - a bare bool cannot distinguish
// "verified last year" from "verified this morning". Evaluation time is
// always a caller-supplied parameter; nothing in this file calls
// time.Now().

// CertificationModelVersion is the certification decision's schema/model
// version. It is carried on every Certification and every Rejection so a
// caller can tell which model produced a given verdict.
const CertificationModelVersion = 1

// productionAvailabilityPercent is the minimum measured availability an SLO
// evidence class must show, and productionScaleFactor requires a
// connector's tested peak load to be at least its declared per-minute
// quota, before L4 (as opposed to L3) is reachable.
const productionAvailabilityPercent = 99.9

// MaturityLevel is a connector's certified conformance level, L0 (nothing
// certified) through L4 (production-grade at scale). The zero value is L0,
// which every connector reaches by definition and which never implies any
// evidence was checked.
type MaturityLevel int

const (
	MaturityL0 MaturityLevel = iota
	MaturityL1
	MaturityL2
	MaturityL3
	MaturityL4
)

// String renders the level for messages and the Rejection shape.
func (m MaturityLevel) String() string {
	switch m {
	case MaturityL0:
		return "L0"
	case MaturityL1:
		return "L1"
	case MaturityL2:
		return "L2"
	case MaturityL3:
		return "L3"
	case MaturityL4:
		return "L4"
	default:
		return fmt.Sprintf("L?(%d)", int(m))
	}
}

// RejectionField names the offending evidence class (or the connector
// identity itself) in a Rejection, so a caller can branch on exactly which
// input was the problem instead of parsing an error string.
type RejectionField string

const (
	FieldConnectorID      RejectionField = "connector_id"
	FieldSupportedVersion RejectionField = "supported_version"
	FieldSecurity         RejectionField = "security"
	FieldMapping          RejectionField = "mapping"
	FieldIdempotency      RejectionField = "idempotency"
	FieldRate             RejectionField = "rate"
	FieldAmbiguity        RejectionField = "ambiguity"
	FieldReconciliation   RejectionField = "reconciliation"
	FieldRestore          RejectionField = "restore"
	FieldScale            RejectionField = "scale"
	FieldSLO              RejectionField = "slo"
)

// RejectionState names why the offending field was rejected: it was never
// observed, it was observed but has since lapsed, it was observed and is
// current but did not pass, or the evidence belongs to a different
// connector than the one being certified.
type RejectionState string

const (
	StateMissing       RejectionState = "MISSING"
	StateExpired       RejectionState = "EXPIRED"
	StateFailed        RejectionState = "FAILED"
	StateScopeMismatch RejectionState = "SCOPE_MISMATCH"
)

// RejectedCode is the CONN-RT-008 typed-rejection code.
const RejectedCode = "CONN_RT_008_REJECTED"

// ErrCertificationRejected is the sentinel every *Rejection wraps, so a
// caller who only needs the family can use errors.Is(err,
// ErrCertificationRejected) without decoding the specific field and state
// that errors.As(err, &rejection) exposes.
var ErrCertificationRejected = errors.New(RejectedCode)

// Rejection is the typed CONN_RT_008_REJECTED error. It names the offending
// field, its state, the connector and model version, and the level that
// was requested - never a bare error string - so a caller can tell exactly
// which evidence class blocked certification.
type Rejection struct {
	Code           string
	Field          RejectionField
	State          RejectionState
	ModelVersion   int
	ConnectorID    string
	RequestedLevel MaturityLevel
}

// Error renders an audit-safe, deterministic rejection message.
func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: connector=%s field=%s state=%s requested=%s model_version=%d",
		r.Code, r.ConnectorID, r.Field, r.State, r.RequestedLevel, r.ModelVersion)
}

// Unwrap exposes ErrCertificationRejected so errors.Is matches the family
// without needing the specific field or state.
func (r *Rejection) Unwrap() error { return ErrCertificationRejected }

func newRejection(field RejectionField, state RejectionState, connectorID string, requested MaturityLevel) *Rejection {
	return &Rejection{
		Code:           RejectedCode,
		Field:          field,
		State:          state,
		ModelVersion:   CertificationModelVersion,
		ConnectorID:    connectorID,
		RequestedLevel: requested,
	}
}

// Window is the observation/expiry shape every evidence class embeds. A
// zero ObservedAt or ExpiresAt means the class was never checked - never
// "still valid" - mirroring CICD-006's BlockerEvidence.present(), which is
// !CheckedAt.IsZero() for exactly the same reason: "nobody looked" must
// never read as "nothing found".
type Window struct {
	ObservedAt time.Time
	ExpiresAt  time.Time
}

func (w Window) observed() bool { return !w.ObservedAt.IsZero() && !w.ExpiresAt.IsZero() }

// unexpired reports whether the window was observed at all and, if so,
// whether it is still current as of now. now is always a parameter: no
// method in this file calls time.Now().
func (w Window) unexpired(now time.Time) bool { return w.observed() && now.Before(w.ExpiresAt) }

// classEvidence is satisfied by every one of the ten evidence classes.
// unexpired is promoted automatically from the embedded Window; each class
// only has to add its own identifying field to present() and its own
// pass/fail field to passing().
type classEvidence interface {
	present() bool
	unexpired(now time.Time) bool
	passing() bool
}

// SupportedVersionEvidence is the supported-version evidence class: the
// provider API/schema version the connector was observed against is one
// this connector build actually supports.
type SupportedVersionEvidence struct {
	Window
	Version   string
	Supported bool
}

func (e SupportedVersionEvidence) present() bool { return e.Window.observed() && e.Version != "" }
func (e SupportedVersionEvidence) passing() bool { return e.Supported }

// SecurityEvidence is the security evidence class: the credential and
// security review backing the connector. An expired CredentialRef is the
// canonical CONN-RT-008 RED case.
type SecurityEvidence struct {
	Window
	CredentialRef string
	ScanPassed    bool
}

func (e SecurityEvidence) present() bool { return e.Window.observed() && e.CredentialRef != "" }
func (e SecurityEvidence) passing() bool { return e.ScanPassed }

// MappingEvidence is the mapping evidence class: the field/object mapping
// between the provider schema and the internal model has been verified
// against a specific schema digest.
type MappingEvidence struct {
	Window
	SchemaDigest string
	Verified     bool
}

func (e MappingEvidence) present() bool { return e.Window.observed() && e.SchemaDigest != "" }
func (e MappingEvidence) passing() bool { return e.Verified }

// IdempotencyEvidence is the idempotency evidence class: a named fixture
// proving repeat delivery of the same operation has no duplicate effect.
type IdempotencyEvidence struct {
	Window
	FixtureID string
	Passed    bool
}

func (e IdempotencyEvidence) present() bool { return e.Window.observed() && e.FixtureID != "" }
func (e IdempotencyEvidence) passing() bool { return e.Passed }

// RateEvidence is the rate evidence class: the connector's declared
// per-minute quota and whether operation under that quota was verified.
type RateEvidence struct {
	Window
	QuotaPerMinute int
	WithinBudget   bool
}

func (e RateEvidence) present() bool { return e.Window.observed() && e.QuotaPerMinute > 0 }
func (e RateEvidence) passing() bool { return e.WithinBudget }

// AmbiguityEvidence is the ambiguity evidence class: a named fixture
// proving an ambiguous provider response is observed rather than guessed.
type AmbiguityEvidence struct {
	Window
	FixtureID string
	Passed    bool
}

func (e AmbiguityEvidence) present() bool { return e.Window.observed() && e.FixtureID != "" }
func (e AmbiguityEvidence) passing() bool { return e.Passed }

// ReconciliationEvidence is the reconciliation evidence class: a named run
// proving drift between the connector and the provider is detected and
// resolved.
type ReconciliationEvidence struct {
	Window
	RunID    string
	Verified bool
}

func (e ReconciliationEvidence) present() bool { return e.Window.observed() && e.RunID != "" }
func (e ReconciliationEvidence) passing() bool { return e.Verified }

// RestoreEvidence is the restore evidence class: a named run proving the
// connector's state can be restored after loss. A connector without
// restore evidence specifically cannot reach L3 or L4, regardless of how
// complete its other nine classes are.
type RestoreEvidence struct {
	Window
	RunID    string
	Verified bool
}

func (e RestoreEvidence) present() bool { return e.Window.observed() && e.RunID != "" }
func (e RestoreEvidence) passing() bool { return e.Verified }

// ScaleEvidence is the scale evidence class: the peak load the connector
// was actually tested at. L4 additionally requires this to be at least the
// connector's declared per-minute quota (RateEvidence.QuotaPerMinute) -
// scale-tested to at least the rate envelope it claims to support.
type ScaleEvidence struct {
	Window
	PeakLoadTested int
	Passed         bool
}

func (e ScaleEvidence) present() bool { return e.Window.observed() && e.PeakLoadTested > 0 }
func (e ScaleEvidence) passing() bool { return e.Passed }

// SLOEvidence is the SLO evidence class: measured availability. L4
// additionally requires this to meet productionAvailabilityPercent.
type SLOEvidence struct {
	Window
	AvailabilityPercent float64
	WithinTarget        bool
}

func (e SLOEvidence) present() bool { return e.Window.observed() && e.AvailabilityPercent > 0 }
func (e SLOEvidence) passing() bool { return e.WithinTarget }

// ConnectorEvidence bundles the ten independently required evidence classes
// for one connector. ConnectorID is the identity this evidence was
// collected for; Certify refuses to certify a different connector with it
// (tenant/connector scoping).
type ConnectorEvidence struct {
	ConnectorID      string
	SupportedVersion SupportedVersionEvidence
	Security         SecurityEvidence
	Mapping          MappingEvidence
	Idempotency      IdempotencyEvidence
	Rate             RateEvidence
	Ambiguity        AmbiguityEvidence
	Reconciliation   ReconciliationEvidence
	Restore          RestoreEvidence
	Scale            ScaleEvidence
	SLO              SLOEvidence
}

// ClassStatus records, per evidence class and in a fixed field order,
// whether that class was present, unexpired and passing at evaluation time.
// It is never a map, so two evaluations of identical evidence at the same
// instant always produce the identical value.
type ClassStatus struct {
	SupportedVersion bool
	Security         bool
	Mapping          bool
	Idempotency      bool
	Rate             bool
	Ambiguity        bool
	Reconciliation   bool
	Restore          bool
	Scale            bool
	SLO              bool
}

// Certification is the certification decision for one connector as of
// EvaluatedAt. It is populated on every call, whether or not the requested
// level was reached, so a caller always has a complete record of what was
// actually observed.
type Certification struct {
	ConnectorID  string
	ModelVersion int
	Level        MaturityLevel
	EvaluatedAt  time.Time
	Classes      ClassStatus
}

// Explain renders an audit-safe decision summary.
func (c Certification) Explain() string {
	return fmt.Sprintf("connector=%s level=%s model_version=%d evaluated_at=%s",
		c.ConnectorID, c.Level, c.ModelVersion, c.EvaluatedAt.UTC().Format(time.RFC3339))
}

// classCheck is the per-class result of evaluating one evidence class as of
// a given instant.
type classCheck struct {
	field RejectionField
	ok    bool
	state RejectionState
}

// classify turns present()/unexpired()/passing() into the single
// three-state result CONN-RT-008 needs to name an offending field: missing
// (never observed), expired (observed but lapsed) or failed (observed,
// current, did not pass).
func classify(now time.Time, ce classEvidence) (bool, RejectionState) {
	switch {
	case !ce.present():
		return false, StateMissing
	case !ce.unexpired(now):
		return false, StateExpired
	case !ce.passing():
		return false, StateFailed
	default:
		return true, ""
	}
}

// checkClasses evaluates all ten evidence classes in a fixed order that
// exactly matches the L1/L2/L3 tiers below: SupportedVersion and Security
// gate L1, Mapping/Idempotency/Rate additionally gate L2, and the remaining
// five additionally gate L3. Certify relies on this ordering to report the
// earliest-tier failure as the offending field.
func checkClasses(now time.Time, evidence ConnectorEvidence) []classCheck {
	entries := []struct {
		field RejectionField
		ce    classEvidence
	}{
		{FieldSupportedVersion, evidence.SupportedVersion},
		{FieldSecurity, evidence.Security},
		{FieldMapping, evidence.Mapping},
		{FieldIdempotency, evidence.Idempotency},
		{FieldRate, evidence.Rate},
		{FieldAmbiguity, evidence.Ambiguity},
		{FieldReconciliation, evidence.Reconciliation},
		{FieldRestore, evidence.Restore},
		{FieldScale, evidence.Scale},
		{FieldSLO, evidence.SLO},
	}
	checks := make([]classCheck, 0, len(entries))
	for _, entry := range entries {
		ok, state := classify(now, entry.ce)
		checks = append(checks, classCheck{field: entry.field, ok: ok, state: state})
	}
	return checks
}

func okFor(checks []classCheck, field RejectionField) bool {
	for _, c := range checks {
		if c.field == field {
			return c.ok
		}
	}
	return false
}

func classStatusFrom(checks []classCheck) ClassStatus {
	var status ClassStatus
	for _, c := range checks {
		switch c.field {
		case FieldSupportedVersion:
			status.SupportedVersion = c.ok
		case FieldSecurity:
			status.Security = c.ok
		case FieldMapping:
			status.Mapping = c.ok
		case FieldIdempotency:
			status.Idempotency = c.ok
		case FieldRate:
			status.Rate = c.ok
		case FieldAmbiguity:
			status.Ambiguity = c.ok
		case FieldReconciliation:
			status.Reconciliation = c.ok
		case FieldRestore:
			status.Restore = c.ok
		case FieldScale:
			status.Scale = c.ok
		case FieldSLO:
			status.SLO = c.ok
		}
	}
	return status
}

// attainedLevel computes the maturity level evidence supports: L1 needs
// supported-version and security; L2 additionally needs mapping,
// idempotency and rate; L3 additionally needs ambiguity, reconciliation,
// restore, scale and SLO (all ten, matching GREEN exactly); L4 additionally
// needs the connector to be scale-tested to at least its declared quota and
// to measure at least productionAvailabilityPercent.
func attainedLevel(checks []classCheck, evidence ConnectorEvidence) MaturityLevel {
	if !okFor(checks, FieldSupportedVersion) || !okFor(checks, FieldSecurity) {
		return MaturityL0
	}
	if !okFor(checks, FieldMapping) || !okFor(checks, FieldIdempotency) || !okFor(checks, FieldRate) {
		return MaturityL1
	}
	if !okFor(checks, FieldAmbiguity) || !okFor(checks, FieldReconciliation) || !okFor(checks, FieldRestore) ||
		!okFor(checks, FieldScale) || !okFor(checks, FieldSLO) {
		return MaturityL2
	}
	if evidence.Scale.PeakLoadTested >= evidence.Rate.QuotaPerMinute && evidence.SLO.AvailabilityPercent >= productionAvailabilityPercent {
		return MaturityL4
	}
	return MaturityL3
}

// Certify decides whether evidence certifies connectorID at requested. now
// is always a caller-supplied evaluation instant, never time.Now(), so the
// decision is deterministic and reproducible against a pinned clock.
//
// Certify performs no I/O and mutates neither its arguments nor any package
// state: it persists no authoritative rows, business events, outbox
// entries, human work or provider requests, because none of those concepts
// exist on this path. The returned Certification is populated on every
// call, reached or not, so a caller always has a full record of what was
// observed; the returned error is nil only when requested was reached and
// otherwise is always a *Rejection wrapping ErrCertificationRejected.
func Certify(connectorID string, evidence ConnectorEvidence, requested MaturityLevel, now time.Time) (Certification, error) {
	if requested < MaturityL0 || requested > MaturityL4 {
		return Certification{}, fmt.Errorf("providercontract: invalid requested maturity level %d", int(requested))
	}
	connectorID = strings.TrimSpace(connectorID)
	base := Certification{ConnectorID: connectorID, ModelVersion: CertificationModelVersion, EvaluatedAt: now.UTC()}
	if connectorID == "" {
		return base, newRejection(FieldConnectorID, StateMissing, connectorID, requested)
	}
	if strings.TrimSpace(evidence.ConnectorID) != connectorID {
		return base, newRejection(FieldConnectorID, StateScopeMismatch, connectorID, requested)
	}

	checks := checkClasses(now, evidence)
	cert := base
	cert.Level = attainedLevel(checks, evidence)
	cert.Classes = classStatusFrom(checks)
	if cert.Level >= requested {
		return cert, nil
	}
	for _, c := range checks {
		if !c.ok {
			return cert, newRejection(c.field, c.state, connectorID, requested)
		}
	}
	// Every one of the ten classes is individually present, unexpired and
	// passing (cert.Level is at least L3), but L4 was requested and its
	// scale/SLO production bar was not met.
	if evidence.Scale.PeakLoadTested < evidence.Rate.QuotaPerMinute {
		return cert, newRejection(FieldScale, StateFailed, connectorID, requested)
	}
	return cert, newRejection(FieldSLO, StateFailed, connectorID, requested)
}
