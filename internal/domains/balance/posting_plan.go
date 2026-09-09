package balance

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PostingPlanRequest contains the complete, already-authorized input to a
// pure balance posting plan. ExpectedHead is a compare-and-set value for the
// later EntryStore.Post calls; planning never reads the store.
type PostingPlanRequest struct {
	Authorized   AuthorizedBalance
	Definition   AccumulatorDefinition
	ExpectedHead int64
	Requested    BalanceEntry
	Rules        []BalanceRule
}

// PostingPlan is an immutable derivation of the entries a caller may post.
// Entries contains only accepted movements, in posting order. Rejected and
// Remainder are the amount left from threshold rules; a floor or cap breach is
// an error and therefore cannot produce a partial plan.
type PostingPlan struct {
	AccountID            string
	DefinitionID         string
	DefinitionVersion    string
	ExpectedHead         int64
	AuthorizedBalance    string
	Requested            BalanceEntry
	Entries              []BalanceEntry
	Opening              values.Decimal
	Accepted             values.Decimal
	Rejected             values.Decimal
	Remainder            values.Decimal
	Ending               values.Decimal
	Rules                []BalanceRule
	Decisions            []RuleDecision
	SourceProposalDigest string
	Digest               string
}

var (
	ErrPostingPlanInvalid  = errors.New("balance: invalid posting plan request")
	ErrPostingPlanRejected = errors.New("balance: posting plan rejected")
	ErrInsufficientBalance = errors.New("balance: insufficient available balance")
)

// PostingPlanRuleError identifies the exact versioned floor or cap that
// refused a proposed movement. It deliberately includes no mutable state or
// persistence handle, so callers can safely present it as a pure explanation.
type PostingPlanRuleError struct {
	RuleID      string
	RuleVersion string
	RuleKind    BalanceRuleKind
	Limit       values.Decimal
	Proposed    values.Decimal
}

func (e *PostingPlanRuleError) Error() string {
	return fmt.Sprintf("%v: rule %s/%s (%s) limit %s rejected proposed balance %s", ErrPostingPlanRejected, e.RuleID, e.RuleVersion, e.RuleKind, e.Limit, e.Proposed)
}

func (e *PostingPlanRuleError) Unwrap() error { return ErrPostingPlanRejected }

// PlanPosting derives a bounded movement without calling EntryStore.Post or
// changing any domain value. Threshold rules reduce the accepted movement and
// report the remainder. Floors and caps are hard boundaries: crossing one
// returns a PostingPlanRuleError naming the rule that rejected the plan.
func PlanPosting(req PostingPlanRequest) (PostingPlan, error) {
	if err := validatePostingPlanRequest(req); err != nil {
		return PostingPlan{}, err
	}
	zero, err := values.NewDecimal("0", req.Authorized.Ending.Scale(), req.Authorized.Ending.Rounding())
	if err != nil {
		return PostingPlan{}, fmt.Errorf("%w: zero: %v", ErrPostingPlanInvalid, err)
	}
	accepted := req.Requested.Amount
	rejected := zero
	decisions := make([]RuleDecision, 0, len(req.Rules))
	for _, rule := range req.Rules {
		if rule.AppliesTo != "" && rule.AppliesTo != req.Requested.Kind {
			continue
		}
		decision := RuleDecision{
			SourceEntry: req.Requested.IdempotencyKey,
			RuleID:      rule.ID,
			RuleVersion: rule.Version,
			RuleKind:    rule.Kind,
			Accepted:    accepted,
			Rejected:    zero,
			Overflow:    zero,
		}
		switch rule.Kind {
		case BalanceRuleThreshold:
			if accepted.Cmp(rule.Limit) > 0 {
				before := accepted
				accepted = rule.Limit
				decision.Rejected, err = before.Sub(accepted)
				if err != nil {
					return PostingPlan{}, fmt.Errorf("%w: threshold %s: %v", ErrPostingPlanInvalid, rule.ID, err)
				}
				rejected, err = rejected.Add(decision.Rejected)
				if err != nil {
					return PostingPlan{}, fmt.Errorf("%w: threshold remainder: %v", ErrPostingPlanInvalid, err)
				}
				decision.ThresholdBreached = true
				decision.Reason = "THRESHOLD_EXCEEDED_REMAINDER"
			} else if accepted.Equal(rule.Limit) {
				decision.ThresholdBreached = true
				decision.Reason = "THRESHOLD_MET"
			} else {
				decision.Reason = "THRESHOLD_NOT_MET"
			}
		case BalanceRuleFloor, BalanceRuleCap:
			candidate, e := postingCandidate(req.Authorized.Ending, req.Requested.Kind, accepted)
			if e != nil {
				return PostingPlan{}, fmt.Errorf("%w: candidate: %v", ErrPostingPlanInvalid, e)
			}
			if rule.Kind == BalanceRuleFloor && candidate.Cmp(rule.Limit) < 0 {
				return PostingPlan{}, &PostingPlanRuleError{RuleID: rule.ID, RuleVersion: rule.Version, RuleKind: rule.Kind, Limit: rule.Limit, Proposed: candidate}
			}
			if rule.Kind == BalanceRuleCap && candidate.Cmp(rule.Limit) > 0 {
				return PostingPlan{}, &PostingPlanRuleError{RuleID: rule.ID, RuleVersion: rule.Version, RuleKind: rule.Kind, Limit: rule.Limit, Proposed: candidate}
			}
			decision.Reason = strings.ToUpper(string(rule.Kind)) + "_NOT_TRIGGERED"
		}
		decision.Accepted = accepted
		decisions = append(decisions, decision)
	}

	if req.Requested.Kind == Debit && accepted.Cmp(req.Authorized.Ending) > 0 {
		return PostingPlan{}, fmt.Errorf("%w: requested %s exceeds authorized ending balance %s", ErrInsufficientBalance, req.Requested.Amount, req.Authorized.Ending)
	}
	ending, err := postingCandidate(req.Authorized.Ending, req.Requested.Kind, accepted)
	if err != nil {
		return PostingPlan{}, fmt.Errorf("%w: ending: %v", ErrPostingPlanInvalid, err)
	}
	entry := req.Requested.copy()
	entry.Amount = accepted
	entry.RecordedAt = values.Instant{}
	entries := []BalanceEntry(nil)
	if accepted.Sign() > 0 {
		entries = []BalanceEntry{entry}
	}
	plan := PostingPlan{
		AccountID:            req.Authorized.AccountID,
		DefinitionID:         req.Definition.ID,
		DefinitionVersion:    req.Definition.Version,
		ExpectedHead:         req.ExpectedHead,
		AuthorizedBalance:    req.Authorized.Digest,
		Requested:            req.Requested.copy(),
		Entries:              entries,
		Opening:              req.Authorized.Ending,
		Accepted:             accepted,
		Rejected:             rejected,
		Remainder:            rejected,
		Ending:               ending,
		Rules:                append([]BalanceRule(nil), req.Rules...),
		Decisions:            append([]RuleDecision(nil), decisions...),
		SourceProposalDigest: req.Requested.Digest(),
	}
	plan.Digest, err = postingPlanDigest(plan)
	if err != nil {
		return PostingPlan{}, fmt.Errorf("%w: digest: %v", ErrPostingPlanInvalid, err)
	}
	return plan, nil
}

// PlanBalancePosting is the descriptive spelling used by callers that want
// to distinguish balance planning from unrelated domain plans.
func PlanBalancePosting(req PostingPlanRequest) (PostingPlan, error) {
	return PlanPosting(req)
}

func validatePostingPlanRequest(req PostingPlanRequest) error {
	if req.ExpectedHead < 0 {
		return fmt.Errorf("%w: expected head cannot be negative", ErrPostingPlanInvalid)
	}
	if err := req.Definition.Validate(); err != nil {
		return fmt.Errorf("%w: definition: %v", ErrPostingPlanInvalid, err)
	}
	if strings.TrimSpace(req.Authorized.AccountID) == "" || req.Authorized.AccountID != req.Requested.AccountID {
		return fmt.Errorf("%w: authorized and requested account identities must match", ErrPostingPlanInvalid)
	}
	if strings.TrimSpace(req.Authorized.Digest) == "" {
		return fmt.Errorf("%w: authorized balance digest is required", ErrPostingPlanInvalid)
	}
	if err := req.Authorized.Ending.Validate(); err != nil {
		return fmt.Errorf("%w: authorized ending: %v", ErrPostingPlanInvalid, err)
	}
	if req.Authorized.Ending.Scale() <= 0 {
		return fmt.Errorf("%w: authorized ending scale must be positive", ErrPostingPlanInvalid)
	}
	if err := req.Requested.Validate(req.Definition); err != nil {
		return fmt.Errorf("%w: requested entry: %v", ErrPostingPlanInvalid, err)
	}
	if req.Requested.Amount.Scale() != req.Authorized.Ending.Scale() {
		return fmt.Errorf("%w: requested amount scale %d does not match authorized scale %d", ErrPostingPlanInvalid, req.Requested.Amount.Scale(), req.Authorized.Ending.Scale())
	}
	if !req.Authorized.Ending.Rounding().Valid() {
		return fmt.Errorf("%w: authorized rounding is unspecified", ErrPostingPlanInvalid)
	}
	ruleReq := RuleApplicationRequest{AccountID: req.Authorized.AccountID, Opening: req.Authorized.Ending, Scale: req.Authorized.Ending.Scale(), Rounding: req.Authorized.Ending.Rounding(), Rules: req.Rules}
	if err := validateRuleApplication(ruleReq); err != nil {
		return fmt.Errorf("%w: rules: %v", ErrPostingPlanInvalid, err)
	}
	return nil
}

func postingCandidate(opening values.Decimal, kind EntryKind, amount values.Decimal) (values.Decimal, error) {
	delta, err := signedContribution(kind, amount)
	if err != nil {
		return values.Decimal{}, err
	}
	return opening.Add(delta)
}

func postingPlanDigest(plan PostingPlan) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.balance.PostingPlan", 1).
		String("account_id", plan.AccountID).
		String("definition_id", plan.DefinitionID).
		String("definition_version", plan.DefinitionVersion).
		Int("expected_head", plan.ExpectedHead).
		String("authorized_balance", plan.AuthorizedBalance).
		String("source_proposal", plan.SourceProposalDigest).
		Value("opening", plan.Opening).
		Value("accepted", plan.Accepted).
		Value("rejected", plan.Rejected).
		Value("remainder", plan.Remainder).
		Value("ending", plan.Ending).
		String("requested", plan.Requested.Digest()).
		Count("rules", len(plan.Rules))
	for _, rule := range plan.Rules {
		w.String("rule.id", rule.ID).String("rule.version", rule.Version).String("rule.kind", string(rule.Kind)).String("rule.applies_to", string(rule.AppliesTo)).Value("rule.limit", rule.Limit)
	}
	w.Count("entries", len(plan.Entries))
	for _, entry := range plan.Entries {
		w.String("entry", entry.Digest())
	}
	w.Count("decisions", len(plan.Decisions))
	for _, decision := range plan.Decisions {
		w.String("decision.source", decision.SourceEntry).String("decision.id", decision.RuleID).String("decision.version", decision.RuleVersion).String("decision.kind", string(decision.RuleKind)).String("decision.reason", decision.Reason).Value("decision.accepted", decision.Accepted).Value("decision.rejected", decision.Rejected).Value("decision.overflow", decision.Overflow).Bool("decision.threshold", decision.ThresholdBreached)
	}
	return w.Digest()
}

// Verify checks the plan's digest without posting it.
func (p PostingPlan) Verify() bool {
	if p.Digest == "" {
		return false
	}
	digest, err := postingPlanDigest(p)
	return err == nil && digest == p.Digest
}
