package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

func ResultDigestForDecision(d authz.Decision) string {
	h := sha256.New()
	for _, id := range d.MatchedRules {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	for _, o := range d.Obligations {
		h.Write([]byte(o))
		h.Write([]byte{0})
	}
	h.Write([]byte(d.Tenant.Effect.String()))
	h.Write([]byte{0})
	h.Write([]byte(d.Scope.Effect.String()))
	return hex.EncodeToString(h.Sum(nil))
}

func DecisionToAuthorization(decision authz.Decision, tenantID, principalID, policySnapshotID uuid.UUID, validUntil time.Time) (AuthorizationDecision, error) {
	if err := decision.Validate(); err != nil {
		return AuthorizationDecision{}, err
	}
	if decision.InputsDigest == "" {
		return AuthorizationDecision{}, fmt.Errorf("%w: inputs digest", ErrMissingDigest)
	}
	if len(decision.PolicyVersions) == 0 {
		return AuthorizationDecision{}, ErrEmptyPolicySnapshot
	}
	resultDigest := ResultDigestForDecision(decision)
	evidenceUUID, err := uuid.Parse(decision.EvidenceID)
	if err != nil {
		sum := sha256.Sum256([]byte(decision.EvidenceID))
		evidenceUUID = uuid.NewSHA1(uuid.Nil, sum[:])
	}
	policyIDs := make([]uuid.UUID, 0, len(decision.PolicyVersions))
	for _, v := range decision.PolicyVersions {
		sum := sha256.Sum256([]byte(v))
		policyIDs = append(policyIDs, uuid.NewSHA1(uuid.Nil, sum[:]))
	}
	meta, _ := json.Marshal(map[string]any{
		"purpose":   decision.Purpose,
		"assurance": decision.Assurance.String(),
	})
	eff := "ALLOW"
	if !decision.SubjectDisclosable {
		eff = "DENY"
	} else if len(decision.Fields) > 0 {
		for _, r := range decision.Fields {
			if r.Effect == authz.EffectDenied {
				eff = "DENY"
				break
			}
		}
	}
	_ = eff
	result := "ALLOW"
	if !decision.SubjectDisclosable {
		result = "DENY"
	}
	ad := AuthorizationDecision{
		TenantID:          tenantID,
		DecisionID:        uuid.New(),
		PrincipalID:       principalID,
		PolicySnapshotID:  policySnapshotID,
		InputDigest:       decision.InputsDigest,
		ResultDigest:      resultDigest,
		EvidenceID:        evidenceUUID,
		Result:            result,
		ValidUntil:        validUntil.UTC(),
		EvaluatedAt:       decision.EvaluatedAt.Time().UTC(),
		PolicySnapshotIDs: policyIDs,
		Metadata:          meta,
	}
	if err := ad.Validate(); err != nil {
		return AuthorizationDecision{}, err
	}
	return ad, nil
}

func PersistAuthzDecision(ctx context.Context, tx dbport.Tx, tenantID, principalID, policySnapshotID uuid.UUID, decision authz.Decision, validUntil time.Time) (AuthorizationDecision, error) {
	ad, err := DecisionToAuthorization(decision, tenantID, principalID, policySnapshotID, validUntil)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	if err := InsertAuthorizationDecision(ctx, tx, ad); err != nil {
		return AuthorizationDecision{}, err
	}
	return ad, nil
}

func LoadDecisionAsAuthz(ctx context.Context, q dbport.Querier, tenantID, decisionID uuid.UUID) (AuthorizationDecision, error) {
	return LoadAuthorizationDecision(ctx, q, tenantID, decisionID)
}
