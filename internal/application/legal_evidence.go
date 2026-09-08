package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/legalevidencestore"
	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// legalEvidenceBackend is the narrow data-port implemented by the durable
// evidence verifier. Keeping the adapter on this port makes request mapping
// explicit and keeps data-layer request types out of the intent application.
type legalEvidenceBackend interface {
	VerifyLegalEvidence(context.Context, legalevidencestore.EvidenceRequest) (legal.EvaluationBinding, error)
}

type legalEvidenceAdapter struct{ backend legalEvidenceBackend }

var _ app.LegalEvidenceVerifier = (*legalEvidenceAdapter)(nil)

func newLegalEvidenceAdapter(backend legalEvidenceBackend) app.LegalEvidenceVerifier {
	if backend == nil {
		return nil
	}
	return &legalEvidenceAdapter{backend: backend}
}

// newLegalEvidenceTenantAuthority binds three independently trusted facts:
// the process's configured tenant, the authenticated principal's tenant and
// the canonical storage UUID currently registered for that exact tenant key.
// The returned UUID is storage routing only; callers must retain the signed
// semantic tenant key when authenticating the evidence envelope.
func newLegalEvidenceTenantAuthority(q dbport.Querier, configuredTenant string) legalevidencestore.TenantAuthority {
	return func(ctx context.Context, tenant string) (uuid.UUID, error) {
		if q == nil || configuredTenant == "" || tenant != configuredTenant {
			return uuid.Nil, fmt.Errorf("application: legal evidence tenant is not configured for this process")
		}
		principal, err := trust.MustFromContext(ctx)
		if err != nil {
			return uuid.Nil, fmt.Errorf("application: legal evidence needs authenticated tenant authority: %w", err)
		}
		if string(principal.Tenant()) != tenant {
			return uuid.Nil, fmt.Errorf("application: legal evidence principal tenant is not authorized")
		}
		var tenantID uuid.UUID
		if err := q.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_key=$1 AND status='ACTIVE'`, tenant).Scan(&tenantID); err != nil {
			return uuid.Nil, fmt.Errorf("application: resolve legal evidence tenant: %w", err)
		}
		if tenantID == uuid.Nil {
			return uuid.Nil, fmt.Errorf("application: resolved legal evidence tenant is nil")
		}
		return tenantID, nil
	}
}

func (a *legalEvidenceAdapter) VerifyLegalEvidence(ctx context.Context, req app.LegalEvidenceRequest) (legal.EvaluationBinding, error) {
	if a == nil || a.backend == nil {
		return legal.EvaluationBinding{}, fmt.Errorf("application: legal evidence verifier is not configured")
	}
	return a.backend.VerifyLegalEvidence(ctx, legalevidencestore.EvidenceRequest{
		Tenant: req.Tenant, IntentID: req.IntentID, ProposalRevisionID: req.ProposalRevisionID,
		MaterialDigest: req.MaterialDigest, LegalContextDigest: req.LegalContextDigest,
	})
}
