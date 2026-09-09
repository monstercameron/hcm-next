package issuerregistry_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

func governmentIssuer(t *testing.T) issuerregistry.Issuer {
	t.Helper()
	i := validIssuer(t)
	i.GovernmentTenant = true
	table := stepup.DefaultCredentialAssuranceTable()
	i.AssuranceContract = issuerregistry.AssuranceContract{
		TableVersion: table.Version,
		IAL:          stepup.IAL2,
		AAL:          stepup.AAL3,
		FAL:          stepup.FAL2,
	}
	return i
}

// TestTodo_AUTHN_010 is the primary government identity-assurance test.
func TestTodo_AUTHN_010(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	issuer := governmentIssuer(t)
	store := issuerregistry.NewMemoryStore()
	published, err := issuerregistry.Publish(store, issuer)
	if err != nil {
		t.Fatalf("Publish government issuer: %v", err)
	}
	if published.AssuranceContract.TableVersion != table.Version {
		t.Fatalf("published contract = %+v, want table version %d", published.AssuranceContract, table.Version)
	}
	resolved, err := published.ResolveAssurance(table)
	if err != nil || resolved.AAL != stepup.AAL3 {
		t.Fatalf("ResolveAssurance = %+v, err=%v", resolved, err)
	}

	evidence := issuerregistry.NewMemoryAccessEvidenceStore()
	allowed, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{
		Issuer: issuer, Table: table, Presented: resolved, SensitiveWork: true, At: baseTime,
	}, evidence)
	if err != nil || !allowed.Allowed || allowed.Revoked {
		t.Fatalf("allowed access decision = %+v, err=%v", allowed, err)
	}
	downgrade, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{
		Issuer: issuer, Table: table,
		Presented:     stepup.AssuranceTier{IAL: stepup.IAL1, AAL: stepup.AAL2, FAL: stepup.FAL1},
		SensitiveWork: true, At: baseTime.Add(time.Minute),
	}, evidence)
	if err != nil || downgrade.Allowed || !downgrade.Revoked || !downgrade.ReauthenticationNeeded {
		t.Fatalf("downgrade decision = %+v, err=%v, want revoke and reauthentication", downgrade, err)
	}
	deprovisioned, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{
		Issuer: issuer, Table: table, Presented: resolved, Deprovisioned: true, At: baseTime.Add(2 * time.Minute),
	}, evidence)
	if err != nil || deprovisioned.Allowed || !deprovisioned.Revoked || deprovisioned.Reason != issuerregistry.ReasonExternalDeprovisioned {
		t.Fatalf("deprovision decision = %+v, err=%v", deprovisioned, err)
	}
	if len(evidence.Decisions()) != 3 {
		t.Fatalf("durable access evidence count = %d, want 3", len(evidence.Decisions()))
	}
}

// TestTodo_AUTHN_010_Golden pins immutable contract resolution and digest
// stability for the same issuer revision and assurance inputs.
func TestTodo_AUTHN_010_Golden(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	issuer := governmentIssuer(t)
	a, err := issuer.ResolveAssurance(table)
	if err != nil {
		t.Fatalf("ResolveAssurance: %v", err)
	}
	if a != issuer.AssuranceContractTier() {
		t.Fatalf("resolved tier = %+v, contract tier = %+v", a, issuer.AssuranceContractTier())
	}
	sink := issuerregistry.NewMemoryAccessEvidenceStore()
	first, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{Issuer: issuer, Table: table, Presented: a, At: baseTime}, sink)
	if err != nil {
		t.Fatalf("EvaluateAccess: %v", err)
	}
	second, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{Issuer: issuer, Table: table, Presented: a, At: baseTime}, sink)
	if err != nil || first.Digest != second.Digest || first.EvidenceID != second.EvidenceID {
		t.Fatalf("same access inputs changed evidence: first=%+v second=%+v err=%v", first, second, err)
	}
}

// TestTodo_AUTHN_010_Security verifies the two fail-closed external identity
// transitions and ensures the explanation remains identifier-free.
func TestTodo_AUTHN_010_Security(t *testing.T) {
	table := stepup.DefaultCredentialAssuranceTable()
	missing := validIssuer(t)
	missing.GovernmentTenant = true
	if _, err := issuerregistry.Publish(issuerregistry.NewMemoryStore(), missing); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("government issuer without assurance contract error = %v, want ErrInvalidIssuer", err)
	}
	badTable, err := stepup.NewCredentialAssuranceTable(2, table.Mappings)
	if err != nil {
		t.Fatalf("NewCredentialAssuranceTable: %v", err)
	}
	if _, err := governmentIssuer(t).ResolveAssurance(badTable); !errors.Is(err, issuerregistry.ErrAssuranceContract) {
		t.Fatalf("wrong table version error = %v, want ErrAssuranceContract", err)
	}
	sink := issuerregistry.NewMemoryAccessEvidenceStore()
	d, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{Issuer: governmentIssuer(t), Table: table, Presented: stepup.AssuranceTier{IAL: stepup.IAL1, AAL: stepup.AAL1, FAL: stepup.FAL1}, SensitiveWork: true, At: baseTime}, sink)
	if err != nil || d.Allowed || !d.Revoked || !strings.Contains(d.Explain(), issuerregistry.ReasonSensitiveReauthentication) {
		t.Fatalf("downgraded access = %+v, err=%v", d, err)
	}
	for _, secret := range []string{"https://login.acme.invalid/", "account-123", "subject-123"} {
		if strings.Contains(d.Explain(), secret) {
			t.Fatalf("access explanation leaks %q: %s", secret, d.Explain())
		}
	}
}

// TestTodo_AUTHN_010_Integration wires the immutable issuer registry,
// versioned assurance table, and durable evidence adapter through exports.
func TestTodo_AUTHN_010_Integration(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := governmentIssuer(t)
	if _, err := issuerregistry.Publish(store, issuer); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := issuerregistry.Activate(store, issuer.Ref(), issuerregistry.Evidence{ActedBy: "approver", At: baseTime.Add(time.Minute)}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	active, err := issuerregistry.Lookup(store, issuer.Tenant, issuer.IssuerURL)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	sink := issuerregistry.NewMemoryAccessEvidenceStore()
	decision, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{Issuer: active, Table: stepup.DefaultCredentialAssuranceTable(), Presented: stepup.AssuranceTier{IAL: stepup.IAL2, AAL: stepup.AAL3, FAL: stepup.FAL2}, At: baseTime}, sink)
	if err != nil || !decision.Allowed {
		t.Fatalf("integrated access decision = %+v, err=%v", decision, err)
	}
	if len(sink.Decisions()) != 1 {
		t.Fatalf("integrated evidence count = %d, want 1", len(sink.Decisions()))
	}

	// The revision itself is durable state, so the same contract is also
	// round-tripped through the real RLS-protected issuer store.
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "government-corp")
	pgIssuer := governmentIssuer(t)
	pgIssuer.Tenant = values.TenantId(tenantID.String())
	pgStore := issuerregistry.NewPGStore(appConn(t, db))
	pgPublished, err := issuerregistry.Publish(pgStore, pgIssuer)
	if err != nil {
		t.Fatalf("PG Publish: %v", err)
	}
	pgGot, found, err := pgStore.GetIssuer(pgPublished.Ref())
	if err != nil || !found {
		t.Fatalf("PG GetIssuer = found=%t err=%v", found, err)
	}
	if pgGot.GovernmentTenant != pgIssuer.GovernmentTenant || pgGot.AssuranceContract != pgIssuer.AssuranceContract {
		t.Fatalf("PG assurance contract = %+v government=%t, want %+v government=%t", pgGot.AssuranceContract, pgGot.GovernmentTenant, pgIssuer.AssuranceContract, pgIssuer.GovernmentTenant)
	}
}

// TestTodo_AUTHN_010_Mutation verifies that deprovision and assurance
// downgrade produce distinct, immutable evidence outcomes.
func TestTodo_AUTHN_010_Mutation(t *testing.T) {
	issuer := governmentIssuer(t)
	table := stepup.DefaultCredentialAssuranceTable()
	sink := issuerregistry.NewMemoryAccessEvidenceStore()
	good, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{Issuer: issuer, Table: table, Presented: issuer.AssuranceContractTier(), At: baseTime}, sink)
	if err != nil || !good.Allowed {
		t.Fatalf("good decision = %+v, err=%v", good, err)
	}
	changed, err := issuerregistry.EvaluateAccess(issuerregistry.AccessRequest{Issuer: issuer, Table: table, Presented: issuer.AssuranceContractTier(), Deprovisioned: true, At: baseTime}, sink)
	if err != nil || changed.Allowed || changed.Digest == good.Digest || changed.Reason != issuerregistry.ReasonExternalDeprovisioned {
		t.Fatalf("mutated decision = %+v, err=%v", changed, err)
	}
}
