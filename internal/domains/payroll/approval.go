package payroll

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/trust/sod"
)

const payrollApprovalSchema = "hcmnext.domains.payroll.PayrollApproval"

// ApprovalPolicy is the effective-dated separation-of-duties policy for a
// payroll approval. The payroll boundary always enforces the requester,
// calculator, releaser, delegation, and one-principal exclusions, even when a
// caller supplies a less restrictive sod.Constraint value.
type ApprovalPolicy struct {
	Quorum      int
	Constraints sod.Constraints
}

// NewApprovalPolicy creates the minimum payroll-grade policy. The required
// separation constraints are enabled by the approval boundary itself.
func NewApprovalPolicy(ruleID string, quorum int) ApprovalPolicy {
	return ApprovalPolicy{
		Quorum: quorum,
		Constraints: sod.Constraints{
			RuleID: ruleID,
		},
	}
}

func (p ApprovalPolicy) normalized() (ApprovalPolicy, error) {
	if p.Quorum < 1 {
		return ApprovalPolicy{}, fmt.Errorf("payroll: quorum must be at least one")
	}
	p.Constraints.RequesterMayNotApprove = true
	p.Constraints.RequesterDelegateMayNotApprove = true
	p.Constraints.ExecutorMayNotApprove = true
	p.Constraints.RequesterMayNotExecute = true
	p.Constraints.OneApprovalPerPrincipal = true
	if err := p.Constraints.Validate(); err != nil {
		return ApprovalPolicy{}, err
	}
	return p, nil
}

func (p ApprovalPolicy) write(w *canonicalbytes.Writer, prefix string) {
	w.Int(prefix+".quorum", int64(p.Quorum))
	w.String(prefix+".rule_id", p.Constraints.RuleID)
	w.Bool(prefix+".requester_may_not_approve", p.Constraints.RequesterMayNotApprove)
	w.Bool(prefix+".requester_delegate_may_not_approve", p.Constraints.RequesterDelegateMayNotApprove)
	w.Bool(prefix+".executor_may_not_approve", p.Constraints.ExecutorMayNotApprove)
	w.Bool(prefix+".requester_may_not_execute", p.Constraints.RequesterMayNotExecute)
	w.Bool(prefix+".one_approval_per_principal", p.Constraints.OneApprovalPerPrincipal)
}

// Digest returns the canonical policy digest after payroll's mandatory
// separation constraints have been applied.
func (p ApprovalPolicy) Digest() (string, error) {
	normalized, err := p.normalized()
	if err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.ApprovalPolicy", 1)
	normalized.write(w, "policy")
	return w.Digest()
}

// ApprovalContext is the complete material payroll result bound by approval.
// Each digest is supplied by the owning calculation, exception, or rules
// subsystem; the approval package never interprets those payloads as facts.
type ApprovalContext struct {
	Run              PayrollRun
	TotalsDigest     string
	ExceptionsDigest string
	RulesDigest      string
	PopulationDigest string
}

// NewApprovalContext binds the material payroll outputs to a calculated run.
func NewApprovalContext(run PayrollRun, totalsDigest, exceptionsDigest, rulesDigest string) (ApprovalContext, error) {
	context := ApprovalContext{
		Run:              run,
		TotalsDigest:     totalsDigest,
		ExceptionsDigest: exceptionsDigest,
		RulesDigest:      rulesDigest,
		PopulationDigest: run.Population.Digest,
	}
	if err := context.Validate(); err != nil {
		return ApprovalContext{}, err
	}
	return context, nil
}

// Validate ensures that approval is only possible for an exact calculated
// run and that population data cannot be substituted independently.
func (c ApprovalContext) Validate() error {
	if err := c.Run.Validate(); err != nil {
		return fmt.Errorf("payroll: approval run: %w", err)
	}
	if c.Run.State != PayrollRunStateCalculated {
		return fmt.Errorf("payroll: approval requires CALCULATED run, got %s", c.Run.State)
	}
	if strings.TrimSpace(c.Run.CalculationDigest) == "" {
		return fmt.Errorf("payroll: approval calculation digest is required")
	}
	for name, value := range map[string]string{
		"totals_digest": c.TotalsDigest, "exceptions_digest": c.ExceptionsDigest,
		"rules_digest": c.RulesDigest, "population_digest": c.PopulationDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("payroll: approval %s is required", name)
		}
	}
	if c.PopulationDigest != c.Run.Population.Digest {
		return fmt.Errorf("payroll: approval population digest does not match the run")
	}
	return nil
}

func (c ApprovalContext) body() (*canonicalbytes.Writer, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	runDigest, err := c.Run.Digest()
	if err != nil {
		return nil, err
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.ApprovalContext", 1)
	w.String("run_digest", runDigest).
		String("run_id", c.Run.RunID).
		Int("run_revision", int64(c.Run.Revision)).
		String("totals_digest", c.TotalsDigest).
		String("exceptions_digest", c.ExceptionsDigest).
		String("rules_digest", c.RulesDigest).
		String("population_digest", c.PopulationDigest)
	return w, nil
}

// Canonical returns the exact bytes used to bind a payroll result.
func (c ApprovalContext) Canonical() []byte {
	w, err := c.body()
	if err != nil {
		return nil
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the material payroll-result digest an approval must bind.
func (c ApprovalContext) Digest() (string, error) {
	w, err := c.body()
	if err != nil {
		return "", err
	}
	return w.Digest()
}

// ApprovalRequest contains the server-resolved actors and candidates for one
// payroll approval. Approvers are candidate principals for the required
// quorum; the request contains no client-supplied digest.
type ApprovalRequest struct {
	Context    ApprovalContext
	Requester  sod.Actor
	Calculator sod.Actor
	Releaser   sod.Actor
	Approvers  []sod.Actor
	Policy     ApprovalPolicy
}

// PayrollApprovalRequest is a descriptive alias for callers at the payroll
// boundary.
type PayrollApprovalRequest = ApprovalRequest

func writeActor(w *canonicalbytes.Writer, prefix string, actor sod.Actor) {
	w.String(prefix+".subject", actor.Subject).
		Count(prefix+".delegation", len(actor.DelegationChain))
	for _, subject := range actor.DelegationChain {
		w.String(prefix+".delegate", subject)
	}
}

func (r ApprovalRequest) normalized() (ApprovalRequest, sod.Result, error) {
	if err := r.Context.Validate(); err != nil {
		return ApprovalRequest{}, sod.Result{}, err
	}
	policy, err := r.Policy.normalized()
	if err != nil {
		return ApprovalRequest{}, sod.Result{}, err
	}
	if strings.TrimSpace(r.Requester.Subject) == "" {
		return ApprovalRequest{}, sod.Result{}, fmt.Errorf("payroll: requester is required")
	}
	if strings.TrimSpace(r.Calculator.Subject) == "" {
		return ApprovalRequest{}, sod.Result{}, fmt.Errorf("payroll: calculator is required")
	}
	if strings.TrimSpace(r.Releaser.Subject) == "" {
		return ApprovalRequest{}, sod.Result{}, fmt.Errorf("payroll: releaser is required")
	}
	if r.Requester.Subject == r.Calculator.Subject || r.Requester.Subject == r.Releaser.Subject || r.Calculator.Subject == r.Releaser.Subject {
		return ApprovalRequest{}, sod.Result{}, fmt.Errorf("payroll: requester, calculator, and releaser must be distinct")
	}
	for _, approver := range r.Approvers {
		if approver.Subject == r.Releaser.Subject {
			return ApprovalRequest{}, sod.Result{}, fmt.Errorf("payroll: releaser cannot approve the payroll")
		}
	}
	result, err := sod.Evaluate(sod.DecisionContext{
		Requester: r.Requester,
		Approvers: r.Approvers,
		Executor:  r.Calculator,
	}, policy.Constraints, policy.Quorum)
	if err != nil {
		return ApprovalRequest{}, result, err
	}
	r.Policy = policy
	return r, result, nil
}

func (r ApprovalRequest) requestDigest(result sod.Result) (string, error) {
	normalized, _, err := r.normalized()
	if err != nil {
		return "", err
	}
	basisDigest, err := normalized.Context.Digest()
	if err != nil {
		return "", err
	}
	policyDigest, err := normalized.Policy.Digest()
	if err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.PayrollApprovalRequest", 1)
	w.String("basis_digest", basisDigest).
		String("policy_digest", policyDigest)
	writeActor(w, "requester", normalized.Requester)
	writeActor(w, "calculator", normalized.Calculator)
	writeActor(w, "releaser", normalized.Releaser)
	w.Count("approver", len(normalized.Approvers))
	for _, approver := range normalized.Approvers {
		writeActor(w, "approver", approver)
	}
	w.String("sod.rule_id", result.RuleID).Bool("sod.satisfied", result.Satisfied)
	w.SortedStrings("sod.eligible", result.Eligible)
	w.Count("sod.excluded", len(result.Excluded))
	for _, excluded := range result.Excluded {
		w.String("sod.excluded.subject", excluded.Subject).
			String("sod.excluded.rule_id", excluded.RuleID).
			String("sod.excluded.reason", excluded.Reason)
	}
	return w.Digest()
}

// PayrollApproval is an immutable-shaped, exact-run approval result. Its
// ApprovalDigest detects mutation before a lock is accepted.
type PayrollApproval struct {
	ApprovalID       string
	BasisDigest      string
	RequestDigest    string
	RunID            string
	RunRevision      uint64
	RequesterID      string
	CalculatorID     string
	ReleaserID       string
	ApproverSubjects []string
	Quorum           int
	PolicyRuleID     string
	SOD              sod.Result
	ApprovalDigest   string
}

// Approval is the short spelling for PayrollApproval.
type Approval = PayrollApproval

func (a PayrollApproval) body() *canonicalbytes.Writer {
	w := canonicalbytes.New(payrollApprovalSchema, 1)
	w.String("approval_id", a.ApprovalID).
		String("basis_digest", a.BasisDigest).
		String("request_digest", a.RequestDigest).
		String("run_id", a.RunID).
		Int("run_revision", int64(a.RunRevision)).
		String("requester_id", a.RequesterID).
		String("calculator_id", a.CalculatorID).
		String("releaser_id", a.ReleaserID).
		Int("quorum", int64(a.Quorum)).
		String("policy_rule_id", a.PolicyRuleID).
		String("sod.rule_id", a.SOD.RuleID).
		Bool("sod.satisfied", a.SOD.Satisfied).
		Count("approver", len(a.ApproverSubjects))
	for _, subject := range a.ApproverSubjects {
		w.String("approver", subject)
	}
	w.SortedStrings("sod.eligible", a.SOD.Eligible).
		Count("sod.excluded", len(a.SOD.Excluded))
	for _, excluded := range a.SOD.Excluded {
		w.String("sod.excluded.subject", excluded.Subject).
			String("sod.excluded.rule_id", excluded.RuleID).
			String("sod.excluded.reason", excluded.Reason)
	}
	return w
}

func (a PayrollApproval) computedDigest() string {
	digest, err := a.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the approval's self-digest and quorum evidence.
func (a PayrollApproval) Validate() error {
	for name, value := range map[string]string{
		"approval_id": a.ApprovalID, "basis_digest": a.BasisDigest,
		"request_digest": a.RequestDigest, "run_id": a.RunID,
		"requester_id": a.RequesterID, "calculator_id": a.CalculatorID,
		"releaser_id": a.ReleaserID, "policy_rule_id": a.PolicyRuleID,
		"approval_digest": a.ApprovalDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("payroll: approval %s is required", name)
		}
	}
	if a.RunRevision == 0 || a.Quorum < 1 || !a.SOD.Satisfied || len(a.ApproverSubjects) < a.Quorum {
		return fmt.Errorf("payroll: approval quorum evidence is incomplete")
	}
	if a.ApprovalID != "payroll-approval/"+a.RequestDigest {
		return fmt.Errorf("payroll: approval id is not request-bound")
	}
	if !slices.Equal(a.ApproverSubjects, a.SOD.Eligible) {
		return fmt.Errorf("payroll: approval approvers do not match SOD eligibility")
	}
	seen := make(map[string]struct{}, len(a.ApproverSubjects))
	for _, subject := range a.ApproverSubjects {
		if strings.TrimSpace(subject) == "" {
			return fmt.Errorf("payroll: approval has an empty approver")
		}
		if _, ok := seen[subject]; ok {
			return fmt.Errorf("payroll: approval repeats approver %q", subject)
		}
		seen[subject] = struct{}{}
	}
	if got := a.computedDigest(); got != a.ApprovalDigest {
		return fmt.Errorf("payroll: approval digest mismatch")
	}
	return nil
}

// Canonical returns the approval evidence bytes, or nil when invalid.
func (a PayrollApproval) Canonical() []byte {
	if err := a.Validate(); err != nil {
		return nil
	}
	raw, err := a.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the self-digest of an approval.
func (a PayrollApproval) Digest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a.ApprovalDigest, nil
}

// ApprovePayroll evaluates the payroll separation policy and binds the
// resulting quorum to the exact run, totals, exceptions, rules, and
// population digests. It has no persistence or side effects.
func ApprovePayroll(request ApprovalRequest) (PayrollApproval, error) {
	normalized, result, err := request.normalized()
	if err != nil {
		return PayrollApproval{}, err
	}
	basisDigest, err := normalized.Context.Digest()
	if err != nil {
		return PayrollApproval{}, err
	}
	requestDigest, err := normalized.requestDigest(result)
	if err != nil {
		return PayrollApproval{}, err
	}
	approval := PayrollApproval{
		ApprovalID:       "payroll-approval/" + requestDigest,
		BasisDigest:      basisDigest,
		RequestDigest:    requestDigest,
		RunID:            normalized.Context.Run.RunID,
		RunRevision:      normalized.Context.Run.Revision,
		RequesterID:      normalized.Requester.Subject,
		CalculatorID:     normalized.Calculator.Subject,
		ReleaserID:       normalized.Releaser.Subject,
		ApproverSubjects: append([]string(nil), result.Eligible...),
		Quorum:           normalized.Policy.Quorum,
		PolicyRuleID:     normalized.Policy.Constraints.RuleID,
		SOD:              cloneSODResult(result),
	}
	approval.ApprovalDigest = approval.computedDigest()
	if err := approval.Validate(); err != nil {
		return PayrollApproval{}, err
	}
	return approval, nil
}

// Approve is the concise spelling for ApprovePayroll.
func Approve(request ApprovalRequest) (PayrollApproval, error) {
	return ApprovePayroll(request)
}

func cloneSODResult(result sod.Result) sod.Result {
	return sod.Result{
		RuleID:    result.RuleID,
		Eligible:  append([]string(nil), result.Eligible...),
		Excluded:  append([]sod.Exclusion(nil), result.Excluded...),
		Satisfied: result.Satisfied,
	}
}

func equalSODResult(left, right sod.Result) bool {
	if left.RuleID != right.RuleID || left.Satisfied != right.Satisfied || !slices.Equal(left.Eligible, right.Eligible) || len(left.Excluded) != len(right.Excluded) {
		return false
	}
	for i := range left.Excluded {
		if left.Excluded[i] != right.Excluded[i] {
			return false
		}
	}
	return true
}

var (
	// ErrApprovalStale means current payroll material differs from what was approved.
	ErrApprovalStale = errors.New("payroll: approval is stale")
	// ErrApprovalInvalid means approval evidence was malformed or tampered with.
	ErrApprovalInvalid = errors.New("payroll: invalid approval")
	// ErrPayrollAlreadyLocked means this value cannot represent a second lock.
	ErrPayrollAlreadyLocked = errors.New("payroll: payroll is already locked")
)

// ApprovalError carries a stable code and offending field for a refused
// approval or lock attempt.
type ApprovalError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *ApprovalError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

func (e *ApprovalError) Unwrap() error { return e.Cause }

func approvalRefusal(field, reason string, cause error) error {
	return &ApprovalError{Code: "PAYRUN_006_REJECTED", Field: field, Reason: reason, Cause: cause}
}

// PayrollLock is the exact approval gate accepted for the calculated run.
type PayrollLock struct {
	RunID          string
	RunRevision    uint64
	BasisDigest    string
	ApprovalDigest string
	RequesterID    string
	CalculatorID   string
	ReleaserID     string
	Quorum         int
	LockDigest     string
}

// LockedPayroll is the descriptive spelling for PayrollLock.
type LockedPayroll = PayrollLock

func (l PayrollLock) body() *canonicalbytes.Writer {
	return canonicalbytes.New("hcmnext.domains.payroll.PayrollLock", 1).
		String("run_id", l.RunID).
		Int("run_revision", int64(l.RunRevision)).
		String("basis_digest", l.BasisDigest).
		String("approval_digest", l.ApprovalDigest).
		String("requester_id", l.RequesterID).
		String("calculator_id", l.CalculatorID).
		String("releaser_id", l.ReleaserID).
		Int("quorum", int64(l.Quorum))
}

// Validate checks the lock's self-digest.
func (l PayrollLock) Validate() error {
	if strings.TrimSpace(l.RunID) == "" || l.RunRevision == 0 || strings.TrimSpace(l.BasisDigest) == "" || strings.TrimSpace(l.ApprovalDigest) == "" || strings.TrimSpace(l.RequesterID) == "" || strings.TrimSpace(l.CalculatorID) == "" || strings.TrimSpace(l.ReleaserID) == "" || l.Quorum < 1 || strings.TrimSpace(l.LockDigest) == "" {
		return fmt.Errorf("payroll: invalid payroll lock")
	}
	if l.RequesterID == l.CalculatorID || l.RequesterID == l.ReleaserID || l.CalculatorID == l.ReleaserID {
		return fmt.Errorf("payroll: payroll lock roles must be distinct")
	}
	if got, err := l.body().Digest(); err != nil || got != l.LockDigest {
		return fmt.Errorf("payroll: payroll lock digest mismatch")
	}
	return nil
}

// Digest returns the lock digest.
func (l PayrollLock) Digest() (string, error) {
	if err := l.Validate(); err != nil {
		return "", err
	}
	return l.LockDigest, nil
}

// LockPayroll revalidates the approval against current server-held payroll
// evidence. Any changed total, exception, rule, population, role, candidate,
// or policy produces PAYRUN_006_REJECTED and no lock value.
func LockPayroll(approval PayrollApproval, current ApprovalRequest) (PayrollLock, error) {
	if err := approval.Validate(); err != nil {
		return PayrollLock{}, approvalRefusal("approval", err.Error(), errors.Join(ErrApprovalInvalid, err))
	}
	normalized, result, err := current.normalized()
	if err != nil {
		return PayrollLock{}, approvalRefusal("current_request", err.Error(), err)
	}
	basisDigest, err := normalized.Context.Digest()
	if err != nil {
		return PayrollLock{}, approvalRefusal("current_request", err.Error(), err)
	}
	if basisDigest != approval.BasisDigest {
		return PayrollLock{}, approvalRefusal("basis_digest", "material payroll evidence changed", ErrApprovalStale)
	}
	requestDigest, err := normalized.requestDigest(result)
	if err != nil {
		return PayrollLock{}, approvalRefusal("request_digest", err.Error(), err)
	}
	if requestDigest != approval.RequestDigest || approval.RunID != normalized.Context.Run.RunID || approval.RunRevision != normalized.Context.Run.Revision || approval.RequesterID != normalized.Requester.Subject || approval.CalculatorID != normalized.Calculator.Subject || approval.ReleaserID != normalized.Releaser.Subject || approval.Quorum != normalized.Policy.Quorum || approval.PolicyRuleID != normalized.Policy.Constraints.RuleID || !slices.Equal(approval.ApproverSubjects, result.Eligible) || !equalSODResult(approval.SOD, result) {
		return PayrollLock{}, approvalRefusal("approval_binding", "approval does not bind the current request", ErrApprovalStale)
	}
	lock := PayrollLock{
		RunID:          approval.RunID,
		RunRevision:    approval.RunRevision,
		BasisDigest:    approval.BasisDigest,
		ApprovalDigest: approval.ApprovalDigest,
		RequesterID:    approval.RequesterID,
		CalculatorID:   approval.CalculatorID,
		ReleaserID:     approval.ReleaserID,
		Quorum:         approval.Quorum,
	}
	lock.LockDigest, err = lock.body().Digest()
	if err != nil {
		return PayrollLock{}, err
	}
	return lock, nil
}

// Lock is the concise spelling for LockPayroll.
func Lock(approval PayrollApproval, current ApprovalRequest) (PayrollLock, error) {
	return LockPayroll(approval, current)
}

// PayrollApprovalExplanation is a redaction-safe explanation of an approval.
type PayrollApprovalExplanation struct {
	RunID         string
	RunRevision   uint64
	BasisDigest   string
	Quorum        int
	ApproverCount int
	Satisfied     bool
	RuleID        string
}

// Explain reports the approval binding without recomputing or mutating it.
func (a PayrollApproval) Explain() (PayrollApprovalExplanation, error) {
	if err := a.Validate(); err != nil {
		return PayrollApprovalExplanation{}, err
	}
	return PayrollApprovalExplanation{
		RunID: a.RunID, RunRevision: a.RunRevision, BasisDigest: a.BasisDigest,
		Quorum: a.Quorum, ApproverCount: len(a.ApproverSubjects),
		Satisfied: a.SOD.Satisfied, RuleID: a.PolicyRuleID,
	}, nil
}
