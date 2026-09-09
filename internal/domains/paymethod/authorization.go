package paymethod

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

const authorizationSchemaVersion = 1

var (
	ErrInvalidAuthorization = errors.New("paymethod: invalid change authorization")
	ErrProposalStale        = errors.New("paymethod: payment change proposal is not current")
	ErrVerificationStale    = errors.New("paymethod: destination verification is stale")
	ErrRiskDecisionStale    = errors.New("paymethod: risk decision is stale or incomplete")
)

// AuthorizationStatus is the fail-closed activation disposition for a
// direct-deposit change. Only Authorized permits activation.
type AuthorizationStatus string

const (
	AuthorizationAuthorized     AuthorizationStatus = "AUTHORIZED"
	AuthorizationStepUpRequired AuthorizationStatus = "STEP_UP_REQUIRED"
	AuthorizationReviewRequired AuthorizationStatus = "REVIEW_REQUIRED"
	AuthorizationReplanRequired AuthorizationStatus = "REPLAN_REQUIRED"
	AuthorizationBlocked        AuthorizationStatus = "BLOCKED"

	// Short aliases keep the wire vocabulary convenient for callers.
	Authorized     = AuthorizationAuthorized
	StepUpRequired = AuthorizationStepUpRequired
	ReviewRequired = AuthorizationReviewRequired
	ReplanRequired = AuthorizationReplanRequired
	Blocked        = AuthorizationBlocked
)

const (
	ReasonAuthorizationReady        = "authorization_ready"
	ReasonStepUpMissing             = "step_up_required"
	ReasonStepUpRejected            = "step_up_rejected"
	ReasonRiskReview                = "risk_review_required"
	ReasonFraudBlocked              = "fraud_blocked"
	ReasonProposalNotCurrent        = "proposal_not_current"
	ReasonDestinationNotVerified    = "destination_not_verified"
	ReasonVerificationNotFresh      = "verification_not_fresh"
	ReasonCoolingNotComplete        = "cooling_off_not_complete"
	ReasonProtectedPayrollWindow    = "protected_payroll_window"
	ReasonSelfApproval              = "self_approval"
	ReasonDestinationBindingInvalid = "destination_binding_invalid"
)

// RiskDecision is the redaction-safe result of a fraud/risk engine. The
// paymethod package intentionally does not import achrisk (achrisk imports
// this package); the higher layer maps its assessment into this port.
// AssessmentDigest and ProposalDigest bind the decision to exact evidence.
type RiskDecision struct {
	Decision         string
	RuleVersion      string
	AssessmentDigest string
	ProposalDigest   string
	EvaluatedAt      time.Time
	ValidUntil       time.Time
}

const (
	RiskRelease = "RELEASE"
	RiskAlert   = "ALERT"
	RiskHold    = "HOLD"
	RiskReject  = "REJECT"
)

// PayrollWindow identifies a payroll interval in which destination changes
// cannot be activated. It carries a reference only; payroll data is absent.
type PayrollWindow struct {
	Protected bool
	Reference string
}

// ProposalRevision is the current server-resolved proposal identity. A
// caller cannot choose an account by supplying a different destination after
// this revision has been approved: all destination and change digests must
// match this exact revision.
type ProposalRevision struct {
	ID       string
	Digest   string
	Revision uint64
}

// StepUpPresenter is implemented by the landed trust/stepup Gate and by
// transport adapters. Present consumes a proof before any effect executes.
type StepUpPresenter interface {
	Present(context.Context, stepup.Proof, stepup.Operation, *trust.Principal, stepup.Requirement) (stepup.Outcome, error)
}

// StepUpGate is a descriptive compatibility alias for the step-up port.
type StepUpGate = StepUpPresenter

// ChangeAuthorizationRequest is the complete, server-bound input to the
// direct-deposit activation gate. It contains no account or routing number.
type ChangeAuthorizationRequest struct {
	Context context.Context

	CurrentDestination  Destination
	ProposedDestination Destination
	Proposal            DestinationChange
	CurrentProposal     ProposalRevision
	BankDetailChange    BankDetailChange

	Principal         *trust.Principal
	StepUpProof       stepup.Proof
	StepUpPresenter   StepUpPresenter
	StepUpRequirement stepup.Requirement

	RiskDecision           RiskDecision
	VerificationFreshUntil time.Time
	PayrollWindow          PayrollWindow
	At                     time.Time
}

// AuthorizationDecision is the immutable result of the activation gate.
// Activated is deliberately separate from Status so every unsafe outcome is
// visibly non-activating even when a caller ignores the status token.
type AuthorizationDecision struct {
	Status            AuthorizationStatus
	Activated         bool
	Reason            string
	ProposalDigest    string
	DestinationDigest string
	RiskDigest        string
	StepUpProofDigest string
	CanonicalDigest   string
}

// AuthorizeChange evaluates a direct-deposit change without performing the
// activation itself. It returns a typed policy outcome for unsafe, otherwise
// well-formed inputs; malformed records return an error and never authorize.
func AuthorizeChange(req ChangeAuthorizationRequest) (AuthorizationDecision, error) {
	req.StepUpRequirement = defaultStepUpRequirement(req.StepUpRequirement)
	decision := AuthorizationDecision{
		ProposalDigest:    req.Proposal.CanonicalDigest,
		DestinationDigest: req.ProposedDestination.CanonicalDigest,
		RiskDigest:        req.RiskDecision.AssessmentDigest,
	}
	deny := func(status AuthorizationStatus, reason string) (AuthorizationDecision, error) {
		decision.Status = status
		decision.Activated = false
		decision.Reason = reason
		var err error
		decision.CanonicalDigest, err = decision.digest()
		if err != nil {
			return AuthorizationDecision{}, fmt.Errorf("%w: decision digest: %v", ErrInvalidAuthorization, err)
		}
		return decision, nil
	}

	if err := validateAuthorizationInput(req); err != nil {
		return AuthorizationDecision{}, err
	}
	if req.Proposal.RequestedBy == req.Proposal.Approver || req.BankDetailChange.RequestedBy == req.BankDetailChange.Approver {
		return deny(AuthorizationBlocked, ReasonSelfApproval)
	}
	if req.CurrentProposal.Digest != req.Proposal.CanonicalDigest {
		return deny(AuthorizationReplanRequired, ReasonProposalNotCurrent)
	}
	if req.Proposal.PreviousDigest != req.CurrentDestination.CanonicalDigest || req.Proposal.ProposedDigest != req.ProposedDestination.CanonicalDigest ||
		req.BankDetailChange.BeforeDigest != req.CurrentDestination.CanonicalDigest || req.BankDetailChange.AfterDigest != req.ProposedDestination.CanonicalDigest {
		return deny(AuthorizationReplanRequired, ReasonDestinationBindingInvalid)
	}
	if req.ProposedDestination.verification() != VerificationVerified {
		return deny(AuthorizationReplanRequired, ReasonDestinationNotVerified)
	}
	if !req.At.Before(req.VerificationFreshUntil) {
		return deny(AuthorizationReplanRequired, ReasonVerificationNotFresh)
	}
	if req.PayrollWindow.Protected {
		return deny(AuthorizationBlocked, ReasonProtectedPayrollWindow)
	}
	if req.BankDetailChange.Status != ChangeConfirmed || req.At.Before(req.BankDetailChange.AvailableAt) {
		return deny(AuthorizationReviewRequired, ReasonCoolingNotComplete)
	}

	switch strings.ToUpper(req.RiskDecision.Decision) {
	case RiskReject:
		return deny(AuthorizationBlocked, ReasonFraudBlocked)
	case RiskAlert, RiskHold:
		return deny(AuthorizationReviewRequired, ReasonRiskReview)
	case RiskRelease:
		// Continue to step-up.
	default:
		return AuthorizationDecision{}, fmt.Errorf("%w: risk decision %q is not declared", ErrInvalidAuthorization, req.RiskDecision.Decision)
	}

	if req.StepUpPresenter == nil {
		return deny(AuthorizationStepUpRequired, ReasonStepUpMissing)
	}
	op := StepUpOperation(req)
	proof := req.StepUpProof
	if proof.Action != op.Action || proof.ProposalID != op.ProposalID || proof.Tenant != op.Tenant ||
		proof.Subject != req.Principal.Subject() || proof.SessionRef != req.Principal.SessionRef() ||
		!equalStrings(proof.Scopes, op.Scopes) || !proof.Assurance.AtLeast(req.StepUpRequirement.MinAssurance) {
		return deny(AuthorizationStepUpRequired, ReasonStepUpRejected)
	}
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := req.StepUpPresenter.Present(ctx, proof, op, req.Principal, req.StepUpRequirement); err != nil {
		return deny(AuthorizationStepUpRequired, ReasonStepUpRejected)
	}
	decision.Status = AuthorizationAuthorized
	decision.Activated = true
	decision.Reason = ReasonAuthorizationReady
	decision.StepUpProofDigest = proof.Digest()
	digest, err := decision.digest()
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("%w: decision digest: %v", ErrInvalidAuthorization, err)
	}
	decision.CanonicalDigest = digest
	return decision, nil
}

// AuthorizeDirectDepositChange is the explicit command-shaped alias.
func AuthorizeDirectDepositChange(req ChangeAuthorizationRequest) (AuthorizationDecision, error) {
	return AuthorizeChange(req)
}

// EvaluateChange is the pure evaluator spelling used by policy callers.
func EvaluateChange(req ChangeAuthorizationRequest) (AuthorizationDecision, error) {
	return AuthorizeChange(req)
}

// StepUpOperation returns the exact operation a proof must bind to this
// proposal. Callers issuing a proof should use this helper rather than
// reconstructing the operation from request fields.
func StepUpOperation(req ChangeAuthorizationRequest) stepup.Operation {
	tenant := values.TenantId(req.BankDetailChange.TenantID)
	return stepup.Operation{
		Action:     stepup.ActionApprove,
		ProposalID: req.Proposal.CanonicalDigest,
		Scopes:     []string{"payment_destination:" + req.Proposal.DestinationID},
		Tenant:     tenant,
		Purpose:    stepup.PurposeHCMOperations,
		Capability: stepup.ActionApprove,
		Risk:       stepup.RiskCritical,
	}
}

// Explain is a package-level engine-shaped symbol. It contains no actor,
// proposal, destination, account, or proof identifiers.
func ExplainAuthorization() string {
	return "paymethod authorization: exact verified destination and current proposal binding, critical step-up, versioned fraud decision, SOD, and cooling/payroll-window controls"
}

func validateAuthorizationInput(req ChangeAuthorizationRequest) error {
	if err := req.CurrentDestination.Validate(); err != nil {
		return fmt.Errorf("%w: current destination: %v", ErrInvalidAuthorization, err)
	}
	if err := req.ProposedDestination.Validate(); err != nil {
		return fmt.Errorf("%w: proposed destination: %v", ErrInvalidAuthorization, err)
	}
	if req.CurrentDestination.id() != req.ProposedDestination.id() || req.CurrentDestination.WorkerRef != req.ProposedDestination.WorkerRef ||
		req.CurrentDestination.Currency != req.ProposedDestination.Currency || req.CurrentDestination.CountryCode != req.ProposedDestination.CountryCode {
		return fmt.Errorf("%w: destinations do not share worker, identity, currency, and country", ErrInvalidAuthorization)
	}
	if err := req.Proposal.Validate(); err != nil {
		return fmt.Errorf("%w: proposal: %v", ErrInvalidAuthorization, err)
	}
	if req.Proposal.CanonicalDigest == "" || !validProtectedDigest(req.Proposal.CanonicalDigest) {
		return fmt.Errorf("%w: proposal digest is required", ErrInvalidAuthorization)
	}
	if req.CurrentProposal.ID == "" || req.CurrentProposal.Digest == "" || req.CurrentProposal.Revision == 0 || !validProtectedDigest(req.CurrentProposal.Digest) {
		return fmt.Errorf("%w: current proposal revision is required", ErrInvalidAuthorization)
	}
	if err := req.BankDetailChange.Validate(); err != nil {
		return fmt.Errorf("%w: bank-detail change: %v", ErrInvalidAuthorization, err)
	}
	if req.Principal == nil {
		return fmt.Errorf("%w: principal is required", ErrInvalidAuthorization)
	}
	if req.Principal.Tenant().String() != req.BankDetailChange.TenantID || req.Principal.Subject() != req.Proposal.RequestedBy {
		return fmt.Errorf("%w: principal is not the server-resolved requester", ErrInvalidAuthorization)
	}
	if req.At.IsZero() || req.VerificationFreshUntil.IsZero() || !req.At.Before(req.VerificationFreshUntil) {
		return fmt.Errorf("%w: evaluation and verification freshness instants are required", ErrInvalidAuthorization)
	}
	if strings.TrimSpace(req.RiskDecision.RuleVersion) == "" || !validProtectedDigest(req.RiskDecision.AssessmentDigest) ||
		!validProtectedDigest(req.RiskDecision.ProposalDigest) || req.RiskDecision.EvaluatedAt.IsZero() || req.RiskDecision.ValidUntil.IsZero() ||
		!req.RiskDecision.EvaluatedAt.Before(req.RiskDecision.ValidUntil) || req.At.Before(req.RiskDecision.EvaluatedAt) || !req.At.Before(req.RiskDecision.ValidUntil) ||
		req.RiskDecision.ProposalDigest != req.Proposal.CanonicalDigest {
		return ErrRiskDecisionStale
	}
	if req.StepUpRequirement.MinAssurance == trust.AssuranceUnspecified || req.StepUpRequirement.Recency <= 0 {
		return fmt.Errorf("%w: step-up requirement is incomplete", ErrInvalidAuthorization)
	}
	return nil
}

func defaultStepUpRequirement(req stepup.Requirement) stepup.Requirement {
	if req.MinAssurance == trust.AssuranceUnspecified {
		req.MinAssurance = trust.AssuranceHigh
	}
	if req.Recency <= 0 {
		req.Recency = 2 * time.Minute
	}
	return req
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, value := range a {
		seen[value]++
	}
	for _, value := range b {
		if seen[value] == 0 {
			return false
		}
		seen[value]--
	}
	return true
}

func (d AuthorizationDecision) digest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.paymethod.AuthorizationDecision", authorizationSchemaVersion).
		String("status", string(d.Status)).Bool("activated", d.Activated).String("reason", d.Reason).
		String("proposal_digest", d.ProposalDigest).String("destination_digest", d.DestinationDigest).
		String("risk_digest", d.RiskDigest).String("step_up_proof_digest", d.StepUpProofDigest)
	return w.Digest()
}

// Explain returns a bounded, redaction-safe decision summary.
func (d AuthorizationDecision) Explain() string {
	return fmt.Sprintf("paymethod authorization status=%s activated=%t reason=%s", d.Status, d.Activated, d.Reason)
}
