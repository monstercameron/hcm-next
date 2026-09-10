package rolloutplan

// Health evaluation and deterministic expansion (ROLLOUT-004): one
// activated stage reports exact telemetry, the evaluator stores metrics,
// thresholds and evidence in a digested decision, and only an EXPAND
// decision advances the ledger to the next planned stage. Unknown
// telemetry, SLO burn, correctness/reconciliation/security regressions and
// short windows block expansion with exact codes. Kernel-pure: no clock,
// no network, no mutable globals.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Health verdicts.
const (
	VerdictExpand = "EXPAND"
	VerdictHold   = "HOLD"
	VerdictStop   = "STOP"
)

// Health evaluation codes.
const (
	UnknownTelemetry   = "UNKNOWN_TELEMETRY"
	TelemetryMismatch  = "TELEMETRY_MISMATCH"
	InsufficientWindow = "INSUFFICIENT_WINDOW"
	HealthBlocked      = "HEALTH_BLOCKED"
	NoFurtherStage     = "NO_FURTHER_STAGE"
	TamperedDecision   = "TAMPERED_DECISION"
)

// HealthError is one exact health failure carrying its code.
type HealthError struct {
	Code   string
	Detail string
}

func (e *HealthError) Error() string { return e.Code + ": " + e.Detail }

// HasHealthCode reports whether err carries the health code.
func HasHealthCode(err error, code string) bool {
	var healthErr *HealthError
	if !errors.As(err, &healthErr) {
		return false
	}
	return healthErr.Code == code
}

// Telemetry is one exact stage health report. BurnPerMille carries the SLO
// burn rate in thousandths so the evaluator never touches floats.
type Telemetry struct {
	PlanDigest             string
	Stage                  string
	Source                 string
	ObservedSeconds        int64
	Requests               int64
	Errors                 int64
	CorrectnessFailures    int64
	ReconciliationFailures int64
	SecurityFindings       int64
	BurnPerMille           int64
	Complete               bool
}

// Thresholds fence one evaluation: window budgets hold expansion, burn
// holds service, and correctness/reconciliation/security regressions stop
// the rollout.
type Thresholds struct {
	MaxErrors              int64
	MaxCorrectnessFailures int64
	MaxReconciliationFails int64
	MaxSecurityFindings    int64
	MaxBurnPerMille        int64
}

// HealthDecision stores one evaluation: the metrics, the thresholds and
// the evidence digests behind the verdict.
type HealthDecision struct {
	Stage                  string
	Verdict                string
	ObservedSeconds        int64
	Requests               int64
	Errors                 int64
	CorrectnessFailures    int64
	ReconciliationFailures int64
	SecurityFindings       int64
	BurnPerMille           int64
	PlanDigest             string
	TelemetryDigest        string
	ThresholdDigest        string
	Digest                 string
}

func telemetryDigest(tel Telemetry) string {
	return cohortDigest("telemetry", tel.PlanDigest, tel.Stage, tel.Source,
		strconv.FormatInt(tel.ObservedSeconds, 10), strconv.FormatInt(tel.Requests, 10),
		strconv.FormatInt(tel.Errors, 10), strconv.FormatInt(tel.CorrectnessFailures, 10),
		strconv.FormatInt(tel.ReconciliationFailures, 10), strconv.FormatInt(tel.SecurityFindings, 10),
		strconv.FormatInt(tel.BurnPerMille, 10))
}

func thresholdDigest(th Thresholds) string {
	return cohortDigest("thresholds",
		strconv.FormatInt(th.MaxErrors, 10), strconv.FormatInt(th.MaxCorrectnessFailures, 10),
		strconv.FormatInt(th.MaxReconciliationFails, 10), strconv.FormatInt(th.MaxSecurityFindings, 10),
		strconv.FormatInt(th.MaxBurnPerMille, 10))
}

// decisionDigest binds the verdict to the stored metrics and the evidence
// digests: editing any metric or swapping the evidence breaks the seal.
func decisionDigest(decision HealthDecision) string {
	return cohortDigest("health-decision", decision.PlanDigest, decision.Stage, decision.Verdict,
		strconv.FormatInt(decision.ObservedSeconds, 10), strconv.FormatInt(decision.Requests, 10),
		strconv.FormatInt(decision.Errors, 10), strconv.FormatInt(decision.CorrectnessFailures, 10),
		strconv.FormatInt(decision.ReconciliationFailures, 10), strconv.FormatInt(decision.SecurityFindings, 10),
		strconv.FormatInt(decision.BurnPerMille, 10),
		decision.TelemetryDigest, decision.ThresholdDigest)
}

// EvaluateHealth decides one stage from exact telemetry. Telemetry gaps
// and short windows fail with codes; regressions stop; budget and burn
// overruns hold; clean windows expand.
func EvaluateHealth(plan Plan, stage string, tel Telemetry, th Thresholds) (HealthDecision, error) {
	if findings := Validate(plan); len(findings) != 0 {
		return HealthDecision{}, &HealthError{Code: TelemetryMismatch, Detail: "plan is not releasable: " + findings[0].String()}
	}
	compiled, err := Compile(plan)
	if err != nil {
		return HealthDecision{}, &HealthError{Code: TelemetryMismatch, Detail: "plan does not compile"}
	}
	if !tel.Complete || strings.TrimSpace(tel.Source) == "" {
		return HealthDecision{}, &HealthError{Code: UnknownTelemetry, Detail: "telemetry is incomplete or sourceless"}
	}
	for _, count := range []int64{tel.ObservedSeconds, tel.Requests, tel.Errors, tel.CorrectnessFailures, tel.ReconciliationFailures, tel.SecurityFindings, tel.BurnPerMille} {
		if count < 0 {
			return HealthDecision{}, &HealthError{Code: UnknownTelemetry, Detail: "telemetry counts are negative"}
		}
	}
	if tel.Errors > tel.Requests {
		return HealthDecision{}, &HealthError{Code: UnknownTelemetry, Detail: "telemetry reports more errors than requests"}
	}
	if tel.Stage != stage || tel.PlanDigest != compiled.Digest {
		return HealthDecision{}, &HealthError{Code: TelemetryMismatch, Detail: fmt.Sprintf(
			"telemetry binds %s@%.12s, evaluation needs %s@%.12s",
			tel.Stage, tel.PlanDigest, stage, compiled.Digest)}
	}
	var window int64 = -1
	for _, s := range plan.Stages {
		if s.Name == stage {
			window = s.HealthWindow.DurationSeconds
		}
	}
	if window < 0 {
		return HealthDecision{}, &HealthError{Code: TelemetryMismatch, Detail: fmt.Sprintf("stage %q is not in the plan", stage)}
	}
	if tel.ObservedSeconds < window {
		return HealthDecision{}, &HealthError{Code: InsufficientWindow, Detail: fmt.Sprintf(
			"observed %ds of %ds health window", tel.ObservedSeconds, window)}
	}
	verdict := VerdictExpand
	switch {
	case tel.CorrectnessFailures > th.MaxCorrectnessFailures,
		tel.ReconciliationFailures > th.MaxReconciliationFails,
		tel.SecurityFindings > th.MaxSecurityFindings:
		verdict = VerdictStop
	case tel.Errors > th.MaxErrors, tel.BurnPerMille > th.MaxBurnPerMille:
		verdict = VerdictHold
	}
	digestThreshold := thresholdDigest(th)
	decision := HealthDecision{
		Stage: stage, Verdict: verdict,
		ObservedSeconds: tel.ObservedSeconds, Requests: tel.Requests, Errors: tel.Errors,
		CorrectnessFailures: tel.CorrectnessFailures, ReconciliationFailures: tel.ReconciliationFailures,
		SecurityFindings: tel.SecurityFindings, BurnPerMille: tel.BurnPerMille,
		PlanDigest: compiled.Digest, TelemetryDigest: telemetryDigest(tel),
		ThresholdDigest: digestThreshold,
	}
	decision.Digest = decisionDigest(decision)
	return decision, nil
}

// Verify recomputes the decision digest from the stored metrics and
// evidence: anything edited after the evaluation fails.
func (d HealthDecision) Verify() error {
	if decisionDigest(d) != d.Digest {
		return &HealthError{Code: TamperedDecision, Detail: "decision digest does not match stored metrics and evidence"}
	}
	return nil
}

// ExpansionRequest advances one healthy stage to the next planned stage at
// a fresh epoch. The decision, cohort and artifact all bind explicitly.
type ExpansionRequest struct {
	Plan         Plan
	Cohorts      CohortSet
	Decision     HealthDecision
	Artifact     Artifact
	CohortDigest string
	Epoch        uint64
}

// Expand verifies an EXPAND decision and activates the next planned stage.
// Anything else fails before the ledger moves.
func (l *ActivationLedger) Expand(req ExpansionRequest) (ActivationReceipt, error) {
	if err := req.Decision.Verify(); err != nil {
		return ActivationReceipt{}, err
	}
	if req.Decision.Verdict != VerdictExpand {
		return ActivationReceipt{}, &HealthError{Code: HealthBlocked, Detail: fmt.Sprintf(
			"stage %q verdict is %s, not EXPAND", req.Decision.Stage, req.Decision.Verdict)}
	}
	next := ""
	for i, stage := range req.Plan.Stages {
		if stage.Name == req.Decision.Stage && i+1 < len(req.Plan.Stages) {
			next = req.Plan.Stages[i+1].Name
		}
	}
	if next == "" {
		return ActivationReceipt{}, &HealthError{Code: NoFurtherStage, Detail: fmt.Sprintf(
			"stage %q is last or unknown", req.Decision.Stage)}
	}
	return l.Activate(ActivationRequest{
		Plan: req.Plan, Cohorts: req.Cohorts, Stage: next, Artifact: req.Artifact,
		PlanDigest: req.Decision.PlanDigest, CohortDigest: req.CohortDigest, Epoch: req.Epoch,
	})
}
