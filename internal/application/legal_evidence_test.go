package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/legalevidencestore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

type legalTenantQuerier struct{ tenantID uuid.UUID }

func (q legalTenantQuerier) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (q legalTenantQuerier) QueryRow(_ context.Context, _ string, args ...any) dbport.Row {
	return legalTenantRow{tenantID: q.tenantID, tenantKey: args[0]}
}

type legalTenantRow struct {
	tenantID  uuid.UUID
	tenantKey any
}

func (r legalTenantRow) Scan(dst ...any) error {
	if r.tenantKey != "tenant-a" {
		return errors.New("tenant not found")
	}
	*(dst[0].(*uuid.UUID)) = r.tenantID
	return nil
}

func legalPrincipal(t *testing.T, tenant string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: "legal-verifier", SubjectKind: trust.SubjectKindService,
		AuthenticationMethod: trust.AuthenticationMethodMutualTLS, Assurance: trust.AssuranceHigh,
		SessionRef: "legal-session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), CredentialDigest: "legal-credential",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

type recordingLegalEvidenceBackend struct {
	req legalevidencestore.EvidenceRequest
	err error
}

func (b *recordingLegalEvidenceBackend) VerifyLegalEvidence(_ context.Context, req legalevidencestore.EvidenceRequest) (legal.EvaluationBinding, error) {
	b.req = req
	if b.err != nil {
		return legal.EvaluationBinding{}, b.err
	}
	return legal.EvaluationBinding{IntentID: req.IntentID}, nil
}

func TestTodo_LEGAL_014_AdapterTranslatesExactRequest(t *testing.T) {
	backend := &recordingLegalEvidenceBackend{}
	adapter := newLegalEvidenceAdapter(backend)
	req := app.LegalEvidenceRequest{Tenant: "tenant", IntentID: "intent", ProposalRevisionID: "proposal", MaterialDigest: "material", LegalContextDigest: "context"}
	got, err := adapter.VerifyLegalEvidence(context.Background(), req)
	if err != nil {
		t.Fatalf("VerifyLegalEvidence: %v", err)
	}
	if backend.req.Tenant != req.Tenant || backend.req.IntentID != req.IntentID || backend.req.ProposalRevisionID != req.ProposalRevisionID || backend.req.MaterialDigest != req.MaterialDigest || backend.req.LegalContextDigest != req.LegalContextDigest {
		t.Fatalf("translated request = %+v, want exact fields from %+v", backend.req, req)
	}
	if got.IntentID != req.IntentID {
		t.Fatalf("binding = %+v, want backend result", got)
	}
}

func TestTodo_LEGAL_014_AdapterPreservesBackendErrorAndNilFails(t *testing.T) {
	sentinel := errors.New("backend failure")
	adapter := newLegalEvidenceAdapter(&recordingLegalEvidenceBackend{err: sentinel})
	if _, err := adapter.VerifyLegalEvidence(context.Background(), app.LegalEvidenceRequest{}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	var nilAdapter *legalEvidenceAdapter
	if _, err := nilAdapter.VerifyLegalEvidence(context.Background(), app.LegalEvidenceRequest{}); err == nil {
		t.Fatal("nil adapter unexpectedly succeeded")
	}
}

func TestTodo_LEGAL_014_TrustedKeyConfigurationRejectsMalformedAndAllowsEmpty(t *testing.T) {
	if keys, err := parseLegalEvidenceIssuerKeys(""); err != nil || keys != nil {
		t.Fatalf("empty key config = (%v, %v), want nil,nil", keys, err)
	}
	if _, err := parseLegalEvidenceIssuerKeys("not-base64"); err == nil {
		t.Fatal("malformed key configuration unexpectedly accepted")
	}
	if _, err := parseLegalEvidenceIssuerKeys(base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize-1))); err == nil {
		t.Fatal("wrong-sized Ed25519 key configuration unexpectedly accepted")
	}
	encoded := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	keys, err := parseLegalEvidenceIssuerKeys(encoded)
	if err != nil || len(keys) != 1 || len(keys[0]) != ed25519.PublicKeySize {
		t.Fatalf("valid Ed25519 key configuration = (%v, %v), want one key", keys, err)
	}
}

func TestTodo_LEGAL_014_Security_TenantAuthorityResolvesCanonicalStorageTenant(t *testing.T) {
	want := uuid.New()
	authority := newLegalEvidenceTenantAuthority(legalTenantQuerier{tenantID: want}, "tenant-a")
	if _, err := authority(context.Background(), "tenant-a"); !errors.Is(err, trust.ErrNoPrincipal) {
		t.Fatalf("authority without principal = %v, want ErrNoPrincipal", err)
	}
	foreign := trust.WithPrincipal(context.Background(), legalPrincipal(t, "tenant-b"))
	if _, err := authority(foreign, "tenant-a"); err == nil {
		t.Fatal("foreign-tenant principal unexpectedly authorized")
	}
	ctx := trust.WithPrincipal(context.Background(), legalPrincipal(t, "tenant-a"))
	got, err := authority(ctx, "tenant-a")
	if err != nil || got != want {
		t.Fatalf("authority = (%s, %v), want (%s, nil)", got, err, want)
	}
	if _, err := authority(ctx, want.String()); err == nil {
		t.Fatal("storage UUID accepted in place of the signed semantic tenant")
	}
}

func TestTodo_LEGAL_014_Integration_DurableAdapterVerifiesPublicSignedEvidence(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	tenantID := uuid.New()
	const tenant = "tenant-legal-integration"
	if _, err := pool.Exec(context.Background(), `INSERT INTO tenant
		(tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-legal','Legal integration','ACTIVE',now())`, tenantID, tenant); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	principal := legalPrincipal(t, tenant)
	ctx := trust.WithPrincipal(context.Background(), principal)
	signer, receipt := legalReceiptFixture(t)
	obligations := make([]legal.BoundObligation, len(receipt.ObligationsApplied))
	for i, obligation := range receipt.ObligationsApplied {
		obligations[i] = legal.BoundObligation{Type: obligation.Type, ID: obligation.ID, BodyDigest: obligation.BodyDigest}
	}
	const intentID = "intent-legal-integration"
	const proposalID = "proposal-legal-integration"
	const materialDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const receiptRef = "legal-receipt:integration"
	binding, err := legal.SignEvaluationBinding(legal.EvaluationBinding{
		Tenant: tenant, IntentID: intentID, ProposalRevisionID: proposalID, MaterialDigest: materialDigest,
		ReceiptRef: receiptRef, ReceiptDigest: receipt.Digest, LegalContextDigest: receipt.LegalContextDigest,
		AppliedObligations: obligations,
	}, signer)
	if err != nil {
		t.Fatalf("sign binding: %v", err)
	}
	authority := newLegalEvidenceTenantAuthority(pool, tenant)
	backend := legalevidencestore.NewVerifier(legalevidencestore.New(pool), authority, signer.PublicKey())
	if err := backend.AppendEvaluationReceipt(ctx, legalevidencestore.EvaluationReceiptEntry{
		TenantID: tenantID.String(), Tenant: tenant, ReceiptRef: receiptRef, Receipt: receipt,
	}); err != nil {
		t.Fatalf("append receipt: %v", err)
	}
	if err := backend.AppendEvaluationBinding(ctx, legalevidencestore.EvaluationBindingEntry{TenantID: tenantID.String(), Binding: binding}); err != nil {
		t.Fatalf("append binding: %v", err)
	}
	adapter := newLegalEvidenceAdapter(backend)
	req := app.LegalEvidenceRequest{Tenant: tenant, IntentID: intentID, ProposalRevisionID: proposalID, MaterialDigest: materialDigest, LegalContextDigest: receipt.LegalContextDigest}
	got, err := adapter.VerifyLegalEvidence(ctx, req)
	if err != nil || got.Digest != binding.Digest {
		t.Fatalf("VerifyLegalEvidence = (%+v, %v), want binding %s", got, err, binding.Digest)
	}

	missing := req
	missing.ProposalRevisionID = "proposal-missing"
	if _, err := adapter.VerifyLegalEvidence(ctx, missing); !errors.Is(err, legalevidencestore.ErrNotFound) {
		t.Fatalf("missing proposal evidence = %v, want ErrNotFound", err)
	}
	foreign := req
	foreign.Tenant = "tenant-foreign"
	if _, err := adapter.VerifyLegalEvidence(ctx, foreign); err == nil {
		t.Fatal("cross-tenant evidence unexpectedly verified")
	}
	tampered := binding
	tampered.MaterialDigest = "material-tampered"
	if err := backend.AppendEvaluationBinding(ctx, legalevidencestore.EvaluationBindingEntry{TenantID: tenantID.String(), Binding: tampered}); !errors.Is(err, legalevidencestore.ErrInvalid) {
		t.Fatalf("tampered binding append = %v, want ErrInvalid", err)
	}
}

func legalReceiptFixture(t *testing.T) (*legal.Signer, legal.LegalEvaluationReceipt) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = 0x5a
	}
	signer, err := legal.NewSigner(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	registry := legal.NewRegistry()
	pack, err := legal.CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := registry.Register(pack); err != nil {
		t.Fatalf("Register: %v", err)
	}
	effective, err := values.NewLocalDate(2026, time.March, 1)
	if err != nil {
		t.Fatalf("effective date: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(time.Unix(1_770_000_000, 0)))
	if err != nil {
		t.Fatalf("known at: %v", err)
	}
	jurisdiction := legal.Jurisdiction{Country: "US", State: "CA"}
	legalContext, err := legal.Resolve(legal.LegalContextInput{
		LegalEntityID: "legal-entity-integration", WorkLocation: jurisdiction,
		EmploymentJurisdiction: jurisdiction, EffectiveDate: effective, KnownAt: knownAt,
	}, registry, signer, values.NewInstant(time.Unix(1_770_100_000, 0)))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	receipt, err := legal.EvaluateReceipt(legalContext, legal.PromotionProposalSnapshot{}, registry, signer, values.NewInstant(time.Unix(1_770_200_000, 0)))
	if err != nil {
		t.Fatalf("EvaluateReceipt: %v", err)
	}
	return signer, receipt
}
