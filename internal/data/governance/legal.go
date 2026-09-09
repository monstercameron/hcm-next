package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func EvaluationResultDigest(r legal.EvaluationResult) string {
	h := sha256.New()
	h.Write([]byte(r.Status.String()))
	for _, o := range r.Obligations {
		h.Write([]byte(o.Type.String()))
		h.Write([]byte(o.ID))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func EvaluationInputDigest(proposal legal.PromotionProposalSnapshot, jurisdiction legal.Jurisdiction) string {
	h := sha256.New()
	h.Write([]byte(jurisdiction.String()))
	h.Write([]byte(proposal.WorkerID))
	h.Write([]byte(proposal.EffectiveDate.String()))
	return hex.EncodeToString(h.Sum(nil))
}

func PersistLegalEvaluation(ctx context.Context, tx dbport.Tx, tenantID, packID uuid.UUID, proposal legal.PromotionProposalSnapshot, result legal.EvaluationResult, evaluatedAt, validUntil time.Time) (RuleEvaluation, error) {
	inputDigest := EvaluationInputDigest(proposal, result.Jurisdiction)
	resultDigest := EvaluationResultDigest(result)
	snap, _ := json.Marshal(proposal)
	statusMap := map[legal.LegalEvaluationStatus]string{
		legal.LegalEvaluationStatusResolvedAllow:        "COMPLIANT",
		legal.LegalEvaluationStatusAllowWithObligations: "COMPLIANT",
		legal.LegalEvaluationStatusRuleCoverageUnknown:  "NOT_APPLICABLE",
	}
	res := statusMap[result.Status]
	if res == "" {
		res = "NEEDS_REVIEW"
	}
	ev := RuleEvaluation{
		TenantID:      tenantID,
		EvaluationID:  uuid.New(),
		PackID:        packID,
		InputDigest:   inputDigest,
		ResultDigest:  resultDigest,
		EvidenceID:    uuid.New(),
		Result:        res,
		ValidUntil:    validUntil.UTC(),
		EvaluatedAt:   evaluatedAt.UTC(),
		InputSnapshot: snap,
	}
	if err := InsertRuleEvaluation(ctx, tx, ev); err != nil {
		return RuleEvaluation{}, err
	}
	for _, o := range result.Obligations {
		sum := sha256.Sum256([]byte(o.Type.String() + o.ID))
		digest := hex.EncodeToString(sum[:])
		ob := Obligation{
			TenantID:      tenantID,
			ObligationID:  uuid.NewSHA1(uuid.Nil, sum[:]),
			EvaluationID:  ev.EvaluationID,
			Kind:          o.Type.String(),
			Status:        "OPEN",
			Payload:       json.RawMessage(`{}`),
			ContentDigest: digest,
			CreatedAt:     evaluatedAt.UTC(),
		}
		_ = InsertObligation(ctx, tx, ob)
	}
	return ev, nil
}

func VerifyEvidenceForAuthorization(decision AuthorizationDecision, evidence EvidenceArtifact, at time.Time) error {
	if err := decision.Verify(at); err != nil {
		return err
	}
	if evidence.CollectedAt.After(at) {
		return ErrExpiredEvidence
	}
	if !at.Before(decision.ValidUntil) {
		return ErrExpiredEvidence
	}
	return nil
}

func VerifyEvidenceForEvaluation(eval RuleEvaluation, evidence EvidenceArtifact, at time.Time) error {
	if err := eval.Verify(at); err != nil {
		return err
	}
	if evidence.CollectedAt.After(at) {
		return ErrExpiredEvidence
	}
	return nil
}
