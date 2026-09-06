// Package achrisk owns pure, versioned ACH risk scoring and Nacha 2026
// applicability resolution. It produces safe evidence records and leaves
// payment execution and investigator I/O to ports owned by callers.
package achrisk

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/paymethod"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

var (
	ErrInvalidPolicy      = errors.New("achrisk: invalid risk policy")
	ErrInvalidInput       = errors.New("achrisk: invalid evaluation input")
	ErrInvalidDisposition = errors.New("achrisk: invalid investigator disposition")
	ErrUnknownParticipant = errors.New("achrisk: unknown ACH participant type")
)

type Decision string

const (
	Alert   Decision = "ALERT"
	Hold    Decision = "HOLD"
	Release Decision = "RELEASE"
	Reject  Decision = "REJECT"
)

func (d Decision) valid() bool { return d == Alert || d == Hold || d == Release || d == Reject }

type ParticipantType string

const (
	Originator            ParticipantType = "ORIGINATOR"
	OriginatingDepository ParticipantType = "ODFI"
	ThirdPartyService     ParticipantType = "THIRD_PARTY_SERVICE_PROVIDER"
	ThirdPartySender      ParticipantType = "THIRD_PARTY_SENDER"
	ReceivingDepository   ParticipantType = "RDFI"
)

func (p ParticipantType) valid() bool {
	switch p {
	case Originator, OriginatingDepository, ThirdPartyService, ThirdPartySender, ReceivingDepository:
		return true
	default:
		return false
	}
}

// Signals are normalized policy inputs. Risk scores are 0..100 and monetary
// values are integer minor units; no floating-point arithmetic is involved.
type Signals struct {
	AmountMinor    int64
	VelocityCount  int
	VelocityWindow time.Duration
	DestinationAge time.Duration
	OperatorRisk   int
	DeviceRisk     int
	TimingRisk     int
	PayrollChange  bool
}

type PolicyConfig struct {
	RuleVersion           string
	AmountThresholdMinor  int64
	VelocityThreshold     int
	DestinationAgeMinimum time.Duration
	OperatorRiskThreshold int
	DeviceRiskThreshold   int
	TimingRiskThreshold   int
	AlertThreshold        int
	HoldThreshold         int
	RejectThreshold       int
	AmountWeight          int
	VelocityWeight        int
	DestinationAgeWeight  int
	OperatorWeight        int
	DeviceWeight          int
	TimingWeight          int
	PayrollChangeWeight   int
}

type Policy struct {
	config PolicyConfig
}

func DefaultPolicy() Policy {
	return Policy{config: PolicyConfig{
		RuleVersion:           "2026.1",
		AmountThresholdMinor:  10000000,
		VelocityThreshold:     10,
		DestinationAgeMinimum: 24 * time.Hour,
		OperatorRiskThreshold: 60,
		DeviceRiskThreshold:   60,
		TimingRiskThreshold:   60,
		AlertThreshold:        25,
		HoldThreshold:         50,
		RejectThreshold:       75,
		AmountWeight:          30,
		VelocityWeight:        25,
		DestinationAgeWeight:  20,
		OperatorWeight:        15,
		DeviceWeight:          15,
		TimingWeight:          10,
		PayrollChangeWeight:   20,
	}}
}

func NewPolicy(config PolicyConfig) (Policy, error) {
	config = mergeDefaults(config, DefaultPolicy().config)
	policy := Policy{config: config}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func mergeDefaults(given, defaults PolicyConfig) PolicyConfig {
	if given.RuleVersion == "" {
		given.RuleVersion = defaults.RuleVersion
	}
	if given.AmountThresholdMinor == 0 {
		given.AmountThresholdMinor = defaults.AmountThresholdMinor
	}
	if given.VelocityThreshold == 0 {
		given.VelocityThreshold = defaults.VelocityThreshold
	}
	if given.DestinationAgeMinimum == 0 {
		given.DestinationAgeMinimum = defaults.DestinationAgeMinimum
	}
	if given.OperatorRiskThreshold == 0 {
		given.OperatorRiskThreshold = defaults.OperatorRiskThreshold
	}
	if given.DeviceRiskThreshold == 0 {
		given.DeviceRiskThreshold = defaults.DeviceRiskThreshold
	}
	if given.TimingRiskThreshold == 0 {
		given.TimingRiskThreshold = defaults.TimingRiskThreshold
	}
	if given.AlertThreshold == 0 {
		given.AlertThreshold = defaults.AlertThreshold
	}
	if given.HoldThreshold == 0 {
		given.HoldThreshold = defaults.HoldThreshold
	}
	if given.RejectThreshold == 0 {
		given.RejectThreshold = defaults.RejectThreshold
	}
	if given.AmountWeight == 0 {
		given.AmountWeight = defaults.AmountWeight
	}
	if given.VelocityWeight == 0 {
		given.VelocityWeight = defaults.VelocityWeight
	}
	if given.DestinationAgeWeight == 0 {
		given.DestinationAgeWeight = defaults.DestinationAgeWeight
	}
	if given.OperatorWeight == 0 {
		given.OperatorWeight = defaults.OperatorWeight
	}
	if given.DeviceWeight == 0 {
		given.DeviceWeight = defaults.DeviceWeight
	}
	if given.TimingWeight == 0 {
		given.TimingWeight = defaults.TimingWeight
	}
	if given.PayrollChangeWeight == 0 {
		given.PayrollChangeWeight = defaults.PayrollChangeWeight
	}
	return given
}

func (p Policy) Validate() error {
	c := p.config
	if strings.TrimSpace(c.RuleVersion) == "" || c.AmountThresholdMinor <= 0 || c.VelocityThreshold <= 0 || c.DestinationAgeMinimum < 0 {
		return fmt.Errorf("%w: thresholds and rule version are required", ErrInvalidPolicy)
	}
	for name, value := range map[string]int{"operator_risk_threshold": c.OperatorRiskThreshold, "device_risk_threshold": c.DeviceRiskThreshold, "timing_risk_threshold": c.TimingRiskThreshold} {
		if value < 0 || value > 100 {
			return fmt.Errorf("%w: %s must be between 0 and 100", ErrInvalidPolicy, name)
		}
	}
	if c.AlertThreshold <= 0 || c.AlertThreshold >= c.HoldThreshold || c.HoldThreshold >= c.RejectThreshold {
		return fmt.Errorf("%w: decision thresholds must be strictly increasing", ErrInvalidPolicy)
	}
	for name, value := range map[string]int{"amount_weight": c.AmountWeight, "velocity_weight": c.VelocityWeight, "destination_age_weight": c.DestinationAgeWeight, "operator_weight": c.OperatorWeight, "device_weight": c.DeviceWeight, "timing_weight": c.TimingWeight, "payroll_change_weight": c.PayrollChangeWeight} {
		if value <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrInvalidPolicy, name)
		}
	}
	return nil
}

type EvaluationInput struct {
	TenantID                 string
	Participant              ParticipantType
	AnnualACHVolume2023      int64
	ConsumerOriginator       bool
	Signals                  Signals
	EvaluatedAt              time.Time
	DestinationChangeDigest  string
	PaymentDestinationChange *paymethod.BankDetailChange
}

type NachaDeadline struct {
	Applicable  bool
	Participant ParticipantType
	Date        time.Time
	Phase       string
	Basis       string
}

// ResolveNacha2026Deadline resolves the official two-phase 2026 dates. The
// volume thresholds apply only to the phase-one participant categories.
func ResolveNacha2026Deadline(participant ParticipantType, annualVolume2023 int64, consumerOriginator bool) (NachaDeadline, error) {
	if !participant.valid() {
		return NachaDeadline{}, fmt.Errorf("%w: %q", ErrUnknownParticipant, participant)
	}
	if annualVolume2023 < 0 {
		return NachaDeadline{}, fmt.Errorf("%w: annual volume cannot be negative", ErrInvalidInput)
	}
	if participant == Originator && consumerOriginator {
		return NachaDeadline{Applicable: false, Participant: participant, Basis: "consumer originator is outside the 2026 non-consumer fraud-monitoring scope"}, nil
	}
	phaseOne := participant == OriginatingDepository ||
		(participant == ReceivingDepository && annualVolume2023 >= 10000000) ||
		((participant == Originator || participant == ThirdPartyService || participant == ThirdPartySender) && annualVolume2023 >= 6000000)
	if phaseOne {
		return NachaDeadline{Applicable: true, Participant: participant, Date: time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC), Phase: "PHASE_1", Basis: "ODFI or 2023 volume threshold"}, nil
	}
	return NachaDeadline{Applicable: true, Participant: participant, Date: time.Date(2026, time.June, 19, 0, 0, 0, 0, time.UTC), Phase: "PHASE_2", Basis: "all other applicable participant types"}, nil
}

// ResolveDeadline is the concise alias for the Nacha 2026 resolver.
func ResolveDeadline(participant ParticipantType, annualVolume2023 int64, consumerOriginator bool) (NachaDeadline, error) {
	return ResolveNacha2026Deadline(participant, annualVolume2023, consumerOriginator)
}

type RiskAssessment struct {
	TenantID                string
	Participant             ParticipantType
	RuleVersion             string
	Score                   int
	Decision                Decision
	Threshold               int
	SignalCount             int
	DestinationChangeDigest string
	Deadline                NachaDeadline
	EvaluatedAt             time.Time
	Revision                uint64
	SupersedesDigest        string
	Disposition             *InvestigatorDisposition
	CanonicalDigest         string
}

func (p Policy) Evaluate(input EvaluationInput) (RiskAssessment, error) {
	if err := p.Validate(); err != nil {
		return RiskAssessment{}, err
	}
	if strings.TrimSpace(input.TenantID) == "" || !input.Participant.valid() || input.EvaluatedAt.IsZero() {
		return RiskAssessment{}, fmt.Errorf("%w: tenant, participant and evaluation instant are required", ErrInvalidInput)
	}
	if err := validateSignals(input.Signals); err != nil {
		return RiskAssessment{}, err
	}
	if input.Signals.PayrollChange && input.PaymentDestinationChange == nil && input.DestinationChangeDigest == "" {
		return RiskAssessment{}, fmt.Errorf("%w: payroll-change signal requires the SECARCH-013 change digest", ErrInvalidInput)
	}
	if input.PaymentDestinationChange != nil {
		if err := input.PaymentDestinationChange.Validate(); err != nil {
			return RiskAssessment{}, fmt.Errorf("%w: destination change: %v", ErrInvalidInput, err)
		}
		input.DestinationChangeDigest = input.PaymentDestinationChange.CanonicalDigest
	}
	deadline, err := ResolveNacha2026Deadline(input.Participant, input.AnnualACHVolume2023, input.ConsumerOriginator)
	if err != nil {
		return RiskAssessment{}, err
	}
	score, count := p.score(input.Signals)
	decision, threshold := decide(score, p.config)
	assessment := RiskAssessment{TenantID: input.TenantID, Participant: input.Participant, RuleVersion: p.config.RuleVersion, Score: score, Decision: decision, Threshold: threshold, SignalCount: count, DestinationChangeDigest: input.DestinationChangeDigest, Deadline: deadline, EvaluatedAt: input.EvaluatedAt.UTC(), Revision: 1}
	assessment.CanonicalDigest = assessment.digest()
	return assessment, nil
}

// Score is the descriptive alias for Evaluate used by callers that treat the
// policy as a scoring engine.
func (p Policy) Score(input EvaluationInput) (RiskAssessment, error) { return p.Evaluate(input) }

func (p Policy) score(s Signals) (int, int) {
	c := p.config
	score, count := 0, 0
	if s.AmountMinor > c.AmountThresholdMinor {
		score += c.AmountWeight
		count++
	}
	if s.VelocityCount > c.VelocityThreshold {
		score += c.VelocityWeight
		count++
	}
	if s.DestinationAge < c.DestinationAgeMinimum {
		score += c.DestinationAgeWeight
		count++
	}
	if s.OperatorRisk >= c.OperatorRiskThreshold {
		score += c.OperatorWeight
		count++
	}
	if s.DeviceRisk >= c.DeviceRiskThreshold {
		score += c.DeviceWeight
		count++
	}
	if s.TimingRisk >= c.TimingRiskThreshold {
		score += c.TimingWeight
		count++
	}
	if s.PayrollChange {
		score += c.PayrollChangeWeight
		count++
	}
	return score, count
}

func decide(score int, c PolicyConfig) (Decision, int) {
	switch {
	case score >= c.RejectThreshold:
		return Reject, c.RejectThreshold
	case score >= c.HoldThreshold:
		return Hold, c.HoldThreshold
	case score >= c.AlertThreshold:
		return Alert, c.AlertThreshold
	default:
		return Release, 0
	}
}

func validateSignals(s Signals) error {
	if s.AmountMinor < 0 || s.VelocityCount < 0 || s.VelocityWindow < 0 || s.DestinationAge < 0 {
		return fmt.Errorf("%w: signal counts, amounts and durations cannot be negative", ErrInvalidInput)
	}
	for name, value := range map[string]int{"operator_risk": s.OperatorRisk, "device_risk": s.DeviceRisk, "timing_risk": s.TimingRisk} {
		if value < 0 || value > 100 {
			return fmt.Errorf("%w: %s must be between 0 and 100", ErrInvalidInput, name)
		}
	}
	return nil
}

type Disposition string

const (
	DispositionCleared        Disposition = "CLEARED"
	DispositionConfirmedFraud Disposition = "CONFIRMED_FRAUD"
	DispositionEscalated      Disposition = "ESCALATED"
)

type InvestigatorDisposition struct {
	Investigator    string
	Outcome         Disposition
	RecordedAt      time.Time
	NotesDigest     string
	CanonicalDigest string
}

type DispositionInput struct {
	Investigator string
	Outcome      Disposition
	RecordedAt   time.Time
	NotesDigest  string
}

func (a RiskAssessment) RecordDisposition(input DispositionInput) (RiskAssessment, error) {
	if err := a.Validate(); err != nil {
		return RiskAssessment{}, err
	}
	if strings.TrimSpace(input.Investigator) == "" || input.RecordedAt.IsZero() || !validDigest(input.NotesDigest) || (input.Outcome != DispositionCleared && input.Outcome != DispositionConfirmedFraud && input.Outcome != DispositionEscalated) {
		return RiskAssessment{}, fmt.Errorf("%w: investigator, outcome, instant and notes digest are required", ErrInvalidDisposition)
	}
	d := InvestigatorDisposition{Investigator: input.Investigator, Outcome: input.Outcome, RecordedAt: input.RecordedAt.UTC(), NotesDigest: input.NotesDigest}
	d.CanonicalDigest = d.digest()
	next := a
	next.Revision++
	next.SupersedesDigest = a.CanonicalDigest
	next.Disposition = &d
	next.CanonicalDigest = next.digest()
	return next, nil
}

// RecordInvestigatorDisposition is the explicit audit-facing alias.
func (a RiskAssessment) RecordInvestigatorDisposition(input DispositionInput) (RiskAssessment, error) {
	return a.RecordDisposition(input)
}

func (a RiskAssessment) Validate() error {
	if strings.TrimSpace(a.TenantID) == "" || !a.Participant.valid() || strings.TrimSpace(a.RuleVersion) == "" || !a.Decision.valid() || a.Score < 0 || a.Threshold < 0 || a.Revision == 0 || a.EvaluatedAt.IsZero() || !a.Deadline.Participant.valid() {
		return fmt.Errorf("%w: assessment fields are invalid", ErrInvalidInput)
	}
	if a.Revision > 1 && !validDigest(a.SupersedesDigest) {
		return fmt.Errorf("%w: successor requires predecessor digest", ErrInvalidInput)
	}
	if a.Disposition != nil && !validDisposition(*a.Disposition) {
		return ErrInvalidDisposition
	}
	if a.CanonicalDigest == "" || a.CanonicalDigest != a.digest() {
		return fmt.Errorf("%w: assessment digest mismatch", ErrInvalidInput)
	}
	return nil
}

// Explain is safe for audit output: only policy metadata and aggregate signal
// counts are shown; tenant, operator, device, destination and notes are not.
func (a RiskAssessment) Explain() string {
	return fmt.Sprintf("ACH risk rule=%s decision=%s score=%d threshold=%d signals=%d deadline_phase=%s disposition_recorded=%t", a.RuleVersion, a.Decision, a.Score, a.Threshold, a.SignalCount, a.Deadline.Phase, a.Disposition != nil)
}

func Explain() string {
	return "achrisk: versioned amount/velocity/destination-age/operator/device/timing/payroll-change scoring with immutable disposition evidence and Nacha 2026 participant deadlines"
}

func (a RiskAssessment) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.achrisk.RiskAssessment", schemaVersion).
		String("tenant_id", a.TenantID).String("participant", string(a.Participant)).String("rule_version", a.RuleVersion).
		Int("score", int64(a.Score)).String("decision", string(a.Decision)).Int("threshold", int64(a.Threshold)).
		Int("signal_count", int64(a.SignalCount)).String("destination_change_digest", a.DestinationChangeDigest).
		Bool("deadline_applicable", a.Deadline.Applicable).String("deadline_participant", string(a.Deadline.Participant)).
		Int("deadline_date", a.Deadline.Date.Unix()).String("deadline_phase", a.Deadline.Phase).String("deadline_basis", a.Deadline.Basis).
		Int("evaluated_at", a.EvaluatedAt.UnixNano()).Int("revision", int64(a.Revision)).String("supersedes_digest", a.SupersedesDigest)
	if a.Disposition == nil {
		w.String("disposition_digest", "")
	} else {
		w.String("disposition_digest", a.Disposition.CanonicalDigest)
	}
	return canonicalbytes.Digest(mustBytes(w))
}

func (d InvestigatorDisposition) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.achrisk.InvestigatorDisposition", schemaVersion).
		String("investigator", d.Investigator).String("outcome", string(d.Outcome)).Int("recorded_at", d.RecordedAt.UnixNano()).String("notes_digest", d.NotesDigest)
	return canonicalbytes.Digest(mustBytes(w))
}

func validDisposition(d InvestigatorDisposition) bool {
	return strings.TrimSpace(d.Investigator) != "" && !d.RecordedAt.IsZero() && validDigest(d.NotesDigest) && d.CanonicalDigest == d.digest()
}

func validDigest(value string) bool {
	return len(value) == len("sha256:")+64 && strings.HasPrefix(value, "sha256:")
}

func mustBytes(w *canonicalbytes.Writer) []byte {
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
