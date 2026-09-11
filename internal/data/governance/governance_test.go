package governance_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root")
		}
		dir = parent
	}
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newPrincipal(tenant uuid.UUID) governance.Principal {
	return governance.Principal{
		TenantID:        tenant,
		PrincipalID:     uuid.New(),
		Kind:            "USER",
		Subject:         "user:" + uuid.NewString(),
		Assurance:       "AAL2",
		AuthnMethod:     "PASSWORD",
		RevocationEpoch: 1,
		Lifecycle:       "ACTIVE",
		CreatedAt:       fixedInstant,
		UpdatedAt:       fixedInstant,
		Metadata:        json.RawMessage(`{}`),
	}
}

func newAuthoritySource(tenant uuid.UUID) governance.AuthoritySource {
	return governance.AuthoritySource{
		TenantID:          tenant,
		AuthoritySourceID: uuid.New(),
		Kind:              "IDP",
		DisplayName:       "idp",
		URI:               "idp:" + uuid.NewString(),
		Version:           1,
		ValidFrom:         fixedInstant,
		ContentDigest:     digestOf("authority-" + uuid.NewString()),
		CreatedAt:         fixedInstant,
	}
}

func newPolicySnapshot(tenant uuid.UUID) governance.AuthorizationPolicySnapshot {
	return governance.AuthorizationPolicySnapshot{
		TenantID:      tenant,
		SnapshotID:    uuid.New(),
		PolicyKey:     "policy:" + uuid.NewString(),
		Version:       1,
		ContentDigest: digestOf("policy-body-" + uuid.NewString()),
		Body:          json.RawMessage(`{"rules":[]}`),
		ValidFrom:     fixedInstant,
		CreatedAt:     fixedInstant,
	}
}

func newDecision(tenant, principalID, snapshotID uuid.UUID) governance.AuthorizationDecision {
	return governance.AuthorizationDecision{
		TenantID:          tenant,
		DecisionID:        uuid.New(),
		PrincipalID:       principalID,
		PolicySnapshotID:  snapshotID,
		InputDigest:       digestOf("input-" + uuid.NewString()),
		ResultDigest:      digestOf("result-" + uuid.NewString()),
		EvidenceID:        uuid.New(),
		Result:            "ALLOW",
		ValidUntil:        fixedInstant.Add(time.Hour),
		EvaluatedAt:       fixedInstant,
		PolicySnapshotIDs: []uuid.UUID{snapshotID},
		Metadata:          json.RawMessage(`{}`),
	}
}

func newJurisdiction() governance.Jurisdiction {
	return governance.Jurisdiction{
		JurisdictionID: uuid.New(),
		Country:        "US",
		Version:        1,
		ValidFrom:      fixedInstant,
		CreatedAt:      fixedInstant,
	}
}

func newRulePack(tenant, jurisdictionID uuid.UUID) governance.LegalRulePack {
	return governance.LegalRulePack{
		TenantID:       tenant,
		PackID:         uuid.New(),
		JurisdictionID: jurisdictionID,
		PackKey:        "pack:" + uuid.NewString(),
		Version:        1,
		ValidFrom:      fixedInstant,
		ContentDigest:  digestOf("pack-" + uuid.NewString()),
		Body:           json.RawMessage(`{}`),
		CreatedAt:      fixedInstant,
	}
}

func newRuleEvaluation(tenant, packID uuid.UUID) governance.RuleEvaluation {
	return governance.RuleEvaluation{
		TenantID:      tenant,
		EvaluationID:  uuid.New(),
		PackID:        packID,
		InputDigest:   digestOf("eval-input-" + uuid.NewString()),
		ResultDigest:  digestOf("eval-result-" + uuid.NewString()),
		EvidenceID:    uuid.New(),
		Result:        "COMPLIANT",
		ValidUntil:    fixedInstant.Add(time.Hour),
		EvaluatedAt:   fixedInstant,
		InputSnapshot: json.RawMessage(`{}`),
	}
}

func newObligation(tenant, evalID uuid.UUID) governance.Obligation {
	return governance.Obligation{
		TenantID:      tenant,
		ObligationID:  uuid.New(),
		EvaluationID:  evalID,
		Kind:          "REVIEW",
		Status:        "OPEN",
		Payload:       json.RawMessage(`{}`),
		ContentDigest: digestOf("ob-" + uuid.NewString()),
		CreatedAt:     fixedInstant,
	}
}

func newEvidenceArtifact(tenant uuid.UUID) governance.EvidenceArtifact {
	return governance.EvidenceArtifact{
		TenantID:       tenant,
		ArtifactID:     uuid.New(),
		Kind:           "ATTESTATION",
		ContentDigest:  digestOf("artifact-" + uuid.NewString()),
		ObjectStoreRef: "s3://bucket/" + uuid.NewString(),
		SizeBytes:      1024,
		Retention:      "PERMANENT",
		CollectedAt:    fixedInstant,
		CreatedAt:      fixedInstant,
		Metadata:       json.RawMessage(`{}`),
	}
}

func setupFullChain(t *testing.T, db *pgtest.DB, tenant uuid.UUID) (governance.Principal, governance.AuthoritySource, governance.AuthorizationPolicySnapshot, governance.AuthorizationDecision, governance.Jurisdiction, governance.LegalRulePack, governance.RuleEvaluation, governance.Obligation, governance.EvidenceArtifact, governance.EvidenceManifest, governance.GovernanceDecisionBundle) {
	t.Helper()
	ctx := context.Background()
	conn := appConn(t, db)
	p := newPrincipal(tenant)
	_ = ctx
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, p) })
	a := newAuthoritySource(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthoritySource(ctx, tx, a) })
	binding := governance.AuthorityBinding{TenantID: tenant, BindingID: uuid.New(), PrincipalID: p.PrincipalID, AuthoritySourceID: a.AuthoritySourceID, Scope: json.RawMessage(`{}`), ValidFrom: fixedInstant, CreatedAt: fixedInstant}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorityBinding(ctx, tx, binding) })
	sess := governance.AuthenticationSession{TenantID: tenant, SessionID: uuid.New(), PrincipalID: p.PrincipalID, IssuedAt: fixedInstant, ExpiresAt: fixedInstant.Add(time.Hour), AuthnMethod: "PASSWORD", Assurance: "AAL2", SessionDigest: digestOf("sess-" + uuid.NewString()), Metadata: json.RawMessage(`{}`)}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthenticationSession(ctx, tx, sess) })
	snap := newPolicySnapshot(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationPolicySnapshot(ctx, tx, snap) })
	dec := newDecision(tenant, p.PrincipalID, snap.SnapshotID)
	dec.SessionID = &sess.SessionID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, dec) })
	man := governance.DataAccessManifest{TenantID: tenant, ManifestID: uuid.New(), DecisionID: dec.DecisionID, PrincipalID: p.PrincipalID, ResourceRef: "resource:" + uuid.NewString(), AccessKind: "READ", Fields: json.RawMessage(`{}`), ValidFrom: fixedInstant, CreatedAt: fixedInstant}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertDataAccessManifest(ctx, tx, man) })
	j := newJurisdiction()
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertJurisdiction(ctx, tx, j) })
	pack := newRulePack(tenant, j.JurisdictionID)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertLegalRulePack(ctx, tx, pack) })
	ev := newRuleEvaluation(tenant, pack.PackID)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertRuleEvaluation(ctx, tx, ev) })
	ob := newObligation(tenant, ev.EvaluationID)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertObligation(ctx, tx, ob) })
	obBind := governance.ObligationBinding{TenantID: tenant, BindingID: uuid.New(), ObligationID: ob.ObligationID, PrincipalID: p.PrincipalID, BoundAt: fixedInstant, Status: "BOUND", Metadata: json.RawMessage(`{}`)}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertObligationBinding(ctx, tx, obBind) })
	art := newEvidenceArtifact(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertEvidenceArtifact(ctx, tx, art) })
	eman := governance.EvidenceManifest{TenantID: tenant, ManifestID: uuid.New(), ArtifactIDs: []uuid.UUID{art.ArtifactID}, RootDigest: digestOf("root-" + uuid.NewString()), CollectedAt: fixedInstant, CreatedAt: fixedInstant, Metadata: json.RawMessage(`{}`)}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertEvidenceManifest(ctx, tx, eman) })
	bundle := governance.GovernanceDecisionBundle{TenantID: tenant, BundleID: uuid.New(), AuthzDecisionIDs: []uuid.UUID{dec.DecisionID}, LegalEvaluationIDs: []uuid.UUID{ev.EvaluationID}, ObligationIDs: []uuid.UUID{ob.ObligationID}, EvidenceManifestID: &eman.ManifestID, CanonicalDigest: digestOf("bundle-" + uuid.NewString()), ValidUntil: fixedInstant.Add(time.Hour), CreatedAt: fixedInstant, Metadata: json.RawMessage(`{}`)}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertGovernanceDecisionBundle(ctx, tx, bundle) })
	return p, a, snap, dec, j, pack, ev, ob, art, eman, bundle
}

func TestTodo_DB_013(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	var inv governance.SchemaInventory
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	inv, err = governance.Inspect(ctx, tx)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !inv.Exact() {
		t.Fatalf("inventory not exact: missing=%v unexpected=%v present=%v", inv.Missing, inv.Unexpected, inv.Present)
	}
	expected := append([]string(nil), governance.GovernanceTables...)
	sort.Strings(expected)
	sort.Strings(inv.Present)
	if len(inv.Present) != len(expected) {
		t.Fatalf("present %v want %v", inv.Present, expected)
	}
	for i := range expected {
		if inv.Present[i] != expected[i] {
			t.Fatalf("present[%d]=%s want %s", i, inv.Present[i], expected[i])
		}
	}
	t.Run("tenant scoping declared correctly", func(t *testing.T) {
		tenantScoped := map[string]bool{
			"principal": true, "authority_source": true, "authority_binding": true, "authentication_session": true, "delegation_grant": true, "authorization_policy_snapshot": true, "authorization_decision": true, "data_access_manifest": true, "legal_rule_pack": true, "rule_evaluation": true, "obligation": true, "obligation_binding": true, "evidence_artifact": true, "evidence_manifest": true, "governance_decision_bundle": true,
		}
		for _, tbl := range governance.GovernanceTables {
			var col string
			err := db.Conn.QueryRow(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='tenant_id'`, tbl).Scan(&col)
			isScoped := err == nil
			want := tenantScoped[tbl]
			if isScoped != want {
				t.Errorf("table %s tenant_id column present=%v want=%v", tbl, isScoped, want)
			}
		}
		var col string
		err := db.Conn.QueryRow(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='jurisdiction' AND column_name='tenant_id'`).Scan(&col)
		if err == nil {
			t.Fatalf("jurisdiction should not have tenant_id column, but found %s", col)
		}
	})
	t.Run("no lease timer tables exist", func(t *testing.T) {
		for _, g := range governance.GatedTables {
			if governance.Contains(inv.Present, g) {
				t.Fatalf("gated table %s unexpectedly present", g)
			}
		}
	})
	t.Run("RLS enabled on tenant scoped tables", func(t *testing.T) {
		for _, tbl := range governance.GovernanceTables {
			if tbl == "jurisdiction" {
				continue
			}
			var relrowsecurity, relforcerowsecurity bool
			if err := db.Conn.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname=$1`, tbl).Scan(&relrowsecurity, &relforcerowsecurity); err != nil {
				t.Fatalf("pg_class for %s: %v", tbl, err)
			}
			if !relrowsecurity || !relforcerowsecurity {
				t.Fatalf("table %s RLS not enabled/forcerowsecurity", tbl)
			}
		}
	})
}

func TestTodo_DB_013_Golden(t *testing.T) {
	t.Parallel()
	expected := []string{
		"authority_binding",
		"authority_source",
		"authentication_session",
		"authorization_decision",
		"authorization_policy_snapshot",
		"data_access_manifest",
		"delegation_grant",
		"evidence_artifact",
		"evidence_manifest",
		"governance_decision_bundle",
		"jurisdiction",
		"legal_rule_pack",
		"obligation",
		"obligation_binding",
		"principal",
		"rule_evaluation",
	}
	sort.Strings(expected)
	golden := append([]string(nil), governance.GovernanceTables...)
	sort.Strings(golden)
	if len(golden) != len(expected) {
		t.Fatalf("governance table count %d want %d", len(golden), len(expected))
	}
	for i := range expected {
		if golden[i] != expected[i] {
			t.Fatalf("golden[%d]=%s want %s", i, golden[i], expected[i])
		}
	}
	root := repoRoot(t)
	reg, err := storagedisposition.Load(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if err := storagedisposition.Validate(reg); err != nil {
		t.Fatalf("registry validate: %v", err)
	}
	for _, tbl := range expected {
		entry, ok := reg.Lookup(tbl)
		if !ok {
			t.Fatalf("table %s not in storage disposition", tbl)
		}
		if entry.Migration != "00021_governance_authz_legal_evidence.sql" {
			t.Fatalf("table %s migration=%s want 00021", tbl, entry.Migration)
		}
	}
	for _, tbl := range governance.GatedTables {
		if _, ok := reg.Lookup(tbl); ok {
			t.Fatalf("gated table %s unexpectedly in registry", tbl)
		}
	}
	dispositionGolden := map[string]string{
		"principal": "OPERATIONAL", "authority_source": "OPERATIONAL", "authority_binding": "OPERATIONAL", "authentication_session": "OPERATIONAL", "delegation_grant": "OPERATIONAL", "authorization_policy_snapshot": "PERMANENT", "authorization_decision": "OPERATIONAL", "data_access_manifest": "OPERATIONAL", "jurisdiction": "PERMANENT", "legal_rule_pack": "PERMANENT", "rule_evaluation": "PERMANENT", "obligation": "OPERATIONAL", "obligation_binding": "OPERATIONAL", "evidence_artifact": "PERMANENT", "evidence_manifest": "PERMANENT", "governance_decision_bundle": "PERMANENT",
	}
	for tbl, want := range dispositionGolden {
		e, ok := reg.Lookup(tbl)
		if !ok {
			t.Fatalf("missing %s", tbl)
		}
		if e.RetentionClass != want {
			t.Fatalf("table %s retention=%s want %s", tbl, e.RetentionClass, want)
		}
	}
}

func TestTodo_DB_013_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "db013-integration")
	conn := appConn(t, db)
	p, a, snap, dec, j, pack, ev, ob, art, eman, bundle := setupFullChain(t, db, tenant)
	t.Run("digests preserved exactly", func(t *testing.T) {
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			gotP, err := governance.LoadPrincipal(ctx, tx, tenant, p.PrincipalID)
			if err != nil {
				return err
			}
			if gotP.Kind != p.Kind || gotP.Subject != p.Subject {
				return fmt.Errorf("principal mismatch")
			}
			gotA, err := governance.LoadAuthoritySource(ctx, tx, tenant, a.AuthoritySourceID)
			if err != nil {
				return err
			}
			if gotA.ContentDigest != a.ContentDigest {
				return fmt.Errorf("authority digest mismatch %s vs %s", gotA.ContentDigest, a.ContentDigest)
			}
			if !gotA.ValidFrom.Equal(a.ValidFrom) {
				return fmt.Errorf("validFrom mismatch")
			}
			gotSnap, err := governance.LoadAuthorizationPolicySnapshot(ctx, tx, tenant, snap.SnapshotID)
			if err != nil {
				return err
			}
			if gotSnap.ContentDigest != snap.ContentDigest || gotSnap.Version != snap.Version {
				return fmt.Errorf("snapshot digest/version mismatch")
			}
			gotDec, err := governance.LoadAuthorizationDecision(ctx, tx, tenant, dec.DecisionID)
			if err != nil {
				return err
			}
			if gotDec.InputDigest != dec.InputDigest || gotDec.ResultDigest != dec.ResultDigest {
				return fmt.Errorf("decision digest mismatch")
			}
			if gotDec.EvidenceID != dec.EvidenceID {
				return fmt.Errorf("evidence_id mismatch")
			}
			gotJ, err := governance.LoadJurisdiction(ctx, tx, j.JurisdictionID)
			if err != nil {
				return err
			}
			if gotJ.Country != j.Country || gotJ.Version != j.Version {
				return fmt.Errorf("jurisdiction mismatch")
			}
			gotPack, err := governance.LoadLegalRulePack(ctx, tx, tenant, pack.PackID)
			if err != nil {
				return err
			}
			if gotPack.ContentDigest != pack.ContentDigest {
				return fmt.Errorf("pack digest mismatch")
			}
			gotEv, err := governance.LoadRuleEvaluation(ctx, tx, tenant, ev.EvaluationID)
			if err != nil {
				return err
			}
			if gotEv.InputDigest != ev.InputDigest || gotEv.ResultDigest != ev.ResultDigest {
				return fmt.Errorf("eval digest mismatch")
			}
			gotOb, err := governance.LoadObligation(ctx, tx, tenant, ob.ObligationID)
			if err != nil {
				return err
			}
			if gotOb.ContentDigest != ob.ContentDigest {
				return fmt.Errorf("obligation digest mismatch")
			}
			gotArt, err := governance.LoadEvidenceArtifact(ctx, tx, tenant, art.ArtifactID)
			if err != nil {
				return err
			}
			if gotArt.ContentDigest != art.ContentDigest || gotArt.ObjectStoreRef != art.ObjectStoreRef {
				return fmt.Errorf("artifact mismatch")
			}
			gotEman, err := governance.LoadEvidenceManifest(ctx, tx, tenant, eman.ManifestID)
			if err != nil {
				return err
			}
			if gotEman.RootDigest != eman.RootDigest {
				return fmt.Errorf("manifest root mismatch")
			}
			gotBundle, err := governance.LoadGovernanceDecisionBundle(ctx, tx, tenant, bundle.BundleID)
			if err != nil {
				return err
			}
			if gotBundle.CanonicalDigest != bundle.CanonicalDigest {
				return fmt.Errorf("bundle digest mismatch")
			}
			return nil
		})
	})
	t.Run("validity intervals preserved", func(t *testing.T) {
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			gotA, _ := governance.LoadAuthoritySource(ctx, tx, tenant, a.AuthoritySourceID)
			if !gotA.ValidFrom.Equal(a.ValidFrom) {
				t.Fatalf("validFrom not preserved")
			}
			gotPack, _ := governance.LoadLegalRulePack(ctx, tx, tenant, pack.PackID)
			if !gotPack.ValidFrom.Equal(pack.ValidFrom) {
				t.Fatalf("pack validFrom not preserved")
			}
			gotDec, _ := governance.LoadAuthorizationDecision(ctx, tx, tenant, dec.DecisionID)
			if !gotDec.ValidUntil.Equal(dec.ValidUntil) || !gotDec.EvaluatedAt.Equal(dec.EvaluatedAt) {
				t.Fatalf("decision interval not preserved")
			}
			return nil
		})
	})
	t.Run("cross tenant delegation rejected", func(t *testing.T) {
		other := insertTenant(t, db, "db013-integration-other")
		p2 := newPrincipal(other)
		otherConn := appConn(t, db)
		inTenantTx(t, otherConn, other, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, p2) })
		delegator := p.PrincipalID
		delegate := p2.PrincipalID
		grant := governance.DelegationGrant{TenantID: tenant, GrantID: uuid.New(), DelegatorPrincipalID: delegator, DelegatePrincipalID: delegate, DelegatorTenant: tenant, DelegateTenant: other, Scope: json.RawMessage(`{}`), ValidFrom: fixedInstant, CreatedAt: fixedInstant}
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertDelegationGrant(ctx, tx, grant) })
		if err == nil {
			t.Fatal("cross-tenant delegation not rejected")
		}
		if !strings.Contains(err.Error(), "cross-tenant") && !strings.Contains(err.Error(), "same_tenant") {
			t.Fatalf("unexpected error for cross-tenant delegation: %v", err)
		}
	})
	t.Run("expired evidence fails verification", func(t *testing.T) {
		expiredDec := newDecision(tenant, p.PrincipalID, snap.SnapshotID)
		expiredDec.ValidUntil = fixedInstant.Add(-time.Hour)
		expiredDec.EvaluatedAt = fixedInstant.Add(-2 * time.Hour)
		if err := expiredDec.Verify(fixedInstant); err == nil {
			t.Fatal("expired decision verify should fail")
		}
		if err := governance.VerifyEvidenceForAuthorization(dec, art, dec.ValidUntil.Add(time.Hour)); err == nil {
			t.Fatal("evidence collected after valid_until should fail")
		}
		if err := expiredDec.Verify(fixedInstant); err == nil {
			t.Fatal("expired decision should not verify")
		}
		evalExpired := newRuleEvaluation(tenant, pack.PackID)
		evalExpired.ValidUntil = fixedInstant.Add(-time.Hour)
		evalExpired.EvaluatedAt = fixedInstant.Add(-2 * time.Hour)
		if err := evalExpired.Verify(fixedInstant); err == nil {
			t.Fatal("expired evaluation verify should fail")
		}
	})
	t.Run("missing context fails", func(t *testing.T) {
		missingDigestSnap := newPolicySnapshot(tenant)
		missingDigestSnap.ContentDigest = ""
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return governance.InsertAuthorizationPolicySnapshot(ctx, tx, missingDigestSnap)
		}); err == nil {
			t.Fatal("missing digest should be rejected")
		}
		unversioned := newAuthoritySource(tenant)
		unversioned.Version = 0
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthoritySource(ctx, tx, unversioned) }); err == nil {
			t.Fatal("unversioned authority should be rejected")
		}
		nilTenantPrincipal := newPrincipal(uuid.Nil)
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, nilTenantPrincipal) }); err == nil {
			t.Fatal("nil tenant should be rejected")
		}
		missingDec := newDecision(tenant, p.PrincipalID, snap.SnapshotID)
		missingDec.InputDigest = ""
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, missingDec) }); err == nil {
			t.Fatal("missing input digest should be rejected")
		}
	})
}

func TestTodo_DB_013_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "db013-mutation")
	conn := appConn(t, db)
	p := newPrincipal(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, p) })
	a := newAuthoritySource(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthoritySource(ctx, tx, a) })
	snap := newPolicySnapshot(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationPolicySnapshot(ctx, tx, snap) })
	dec := newDecision(tenant, p.PrincipalID, snap.SnapshotID)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, dec) })
	art := newEvidenceArtifact(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertEvidenceArtifact(ctx, tx, art) })

	t.Run("immutable evidence artifact cannot be updated via raw SQL via app role", func(t *testing.T) {
		app := appConn(t, db)
		err := inTenantTxErr(app, tenant, func(tx dbport.Tx) error {
			_, e := tx.Exec(ctx, `UPDATE evidence_artifact SET content_digest=$1 WHERE tenant_id=$2 AND artifact_id=$3`, digestOf("mutated-app"), tenant, art.ArtifactID)
			return e
		})
		if err == nil {
			t.Fatal("app role update of evidence_artifact should be refused")
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			got, e := governance.LoadEvidenceArtifact(ctx, tx, tenant, art.ArtifactID)
			if e != nil {
				return e
			}
			if got.ContentDigest != art.ContentDigest {
				t.Fatalf("artifact digest mutated to %s", got.ContentDigest)
			}
			return nil
		})
	})

	t.Run("immutable decision cannot be updated", func(t *testing.T) {
		app := appConn(t, db)
		err := inTenantTxErr(app, tenant, func(tx dbport.Tx) error {
			_, e := tx.Exec(ctx, `UPDATE authorization_decision SET result='DENY' WHERE tenant_id=$1 AND decision_id=$2`, tenant, dec.DecisionID)
			return e
		})
		if err == nil {
			t.Fatal("app role update of authorization_decision should be refused")
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			got, e := governance.LoadAuthorizationDecision(ctx, tx, tenant, dec.DecisionID)
			if e != nil {
				return e
			}
			if got.Result != dec.Result {
				t.Fatalf("decision result mutated")
			}
			return nil
		})
	})

	t.Run("duplicate evidence_id rejected", func(t *testing.T) {
		dup := newDecision(tenant, p.PrincipalID, snap.SnapshotID)
		dup.EvidenceID = dec.EvidenceID
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, dup) })
		if err == nil {
			t.Fatal("duplicate evidence_id not rejected")
		}
		evPack := newRulePack(tenant, newJurisdiction().JurisdictionID)
		j2 := newJurisdiction()
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertJurisdiction(ctx, tx, j2) })
		evPack.JurisdictionID = j2.JurisdictionID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertLegalRulePack(ctx, tx, evPack) })
		ev1 := newRuleEvaluation(tenant, evPack.PackID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertRuleEvaluation(ctx, tx, ev1) })
		ev2 := newRuleEvaluation(tenant, evPack.PackID)
		ev2.EvidenceID = ev1.EvidenceID
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertRuleEvaluation(ctx, tx, ev2) }); err == nil {
			t.Fatal("duplicate rule evaluation evidence_id not rejected")
		}
	})

	t.Run("cross-tenant FK via raw SQL rejected", func(t *testing.T) {
		other := insertTenant(t, db, "db013-mutation-other")
		otherP := newPrincipal(other)
		otherConn := appConn(t, db)
		inTenantTx(t, otherConn, other, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, otherP) })
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO authority_binding (tenant_id, binding_id, principal_id, authority_source_id, scope, valid_from, valid_to, created_at) VALUES ($1,$2,$3,$4,'{}',$5,null,$5)`, tenant, uuid.New(), otherP.PrincipalID, a.AuthoritySourceID, fixedInstant)
			return e
		})
		if err == nil {
			t.Fatal("cross-tenant authority_binding FK not rejected")
		}
	})

	t.Run("invalid intervals rejected", func(t *testing.T) {
		bad := newAuthoritySource(tenant)
		to := fixedInstant.Add(-time.Hour)
		bad.ValidTo = &to
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthoritySource(ctx, tx, bad) }); err == nil {
			t.Fatal("invalid interval not rejected")
		}
		badSess := governance.AuthenticationSession{TenantID: tenant, SessionID: uuid.New(), PrincipalID: p.PrincipalID, IssuedAt: fixedInstant, ExpiresAt: fixedInstant.Add(-time.Hour), AuthnMethod: "PASSWORD", Assurance: "AAL1", SessionDigest: digestOf("bad-sess"), Metadata: json.RawMessage(`{}`)}
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthenticationSession(ctx, tx, badSess) }); err == nil {
			t.Fatal("invalid session interval not rejected")
		}
	})

	t.Run("delete revoked via app role", func(t *testing.T) {
		app := appConn(t, db)
		if err := inTenantTxErr(app, tenant, func(tx dbport.Tx) error {
			_, e := tx.Exec(ctx, `DELETE FROM principal WHERE tenant_id=$1 AND principal_id=$2`, tenant, p.PrincipalID)
			return e
		}); err == nil {
			t.Fatal("app role delete of principal should be refused")
		}
		if err := inTenantTxErr(app, tenant, func(tx dbport.Tx) error {
			_, e := tx.Exec(ctx, `DELETE FROM authorization_policy_snapshot WHERE tenant_id=$1 AND snapshot_id=$2`, tenant, snap.SnapshotID)
			return e
		}); err == nil {
			t.Fatal("app role delete of policy snapshot should be refused")
		}
	})

	t.Run("unversioned rule pack rejected", func(t *testing.T) {
		j3 := newJurisdiction()
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { return governance.InsertJurisdiction(ctx, tx, j3) })
		badPack := newRulePack(tenant, j3.JurisdictionID)
		badPack.Version = 0
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { return governance.InsertLegalRulePack(ctx, tx, badPack) }); err == nil {
			t.Fatal("unversioned rule pack not rejected")
		}
	})
}

func TestTodo_DB_013_Property(t *testing.T) {
	t.Parallel()
	t.Run("digest preservation is deterministic", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			payload := "payload-" + uuid.NewString()
			d1 := governance.ComputeDigest([]byte(payload))
			d2 := governance.ComputeDigest([]byte(payload))
			if d1 != d2 {
				t.Fatalf("digest not deterministic %s vs %s", d1, d2)
			}
			if len(d1) != 64 {
				t.Fatalf("digest length %d want 64", len(d1))
			}
		}
	})
	t.Run("validity interval half-open enforcement", func(t *testing.T) {
		cases := []struct {
			from time.Time
			to   *time.Time
			ok   bool
		}{
			{fixedInstant, nil, true},
			{fixedInstant, timePtr(fixedInstant.Add(time.Hour)), true},
			{fixedInstant, timePtr(fixedInstant), false},
			{fixedInstant, timePtr(fixedInstant.Add(-time.Hour)), false},
		}
		for _, tc := range cases {
			a := governance.AuthoritySource{TenantID: uuid.New(), AuthoritySourceID: uuid.New(), Kind: "IDP", DisplayName: "x", URI: "uri:x", Version: 1, ValidFrom: tc.from, ValidTo: tc.to, ContentDigest: digestOf("x"), CreatedAt: fixedInstant}
			err := a.Validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok for %v -> %v, got %v", tc.from, tc.to, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error for %v -> %v", tc.from, tc.to)
			}
		}
	})
	t.Run("fuzz digest input does not affect validation beyond presence", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			d := digestOf(uuid.NewString() + string(rune(i)))
			p := newPrincipal(uuid.New())
			p.CredentialDigest = &d
			if err := p.Validate(); err != nil {
				t.Fatalf("principal with random digest should validate: %v", err)
			}
			empty := ""
			p2 := newPrincipal(uuid.New())
			p2.CredentialDigest = &empty
			_ = p2.Validate()
		}
	})
	t.Run("evidence expiry is monotonic", func(t *testing.T) {
		tenant := uuid.New()
		snap := uuid.New()
		dec := governance.AuthorizationDecision{TenantID: tenant, DecisionID: uuid.New(), PrincipalID: uuid.New(), PolicySnapshotID: snap, InputDigest: digestOf("in"), ResultDigest: digestOf("out"), EvidenceID: uuid.New(), Result: "ALLOW", ValidUntil: fixedInstant.Add(time.Hour), EvaluatedAt: fixedInstant, PolicySnapshotIDs: []uuid.UUID{snap}, Metadata: json.RawMessage(`{}`)}
		if err := dec.Verify(fixedInstant); err != nil {
			t.Fatalf("should verify before expiry: %v", err)
		}
		if err := dec.Verify(fixedInstant.Add(2 * time.Hour)); err == nil {
			t.Fatal("should not verify after expiry")
		}
		if err := dec.Verify(dec.ValidUntil); err == nil {
			t.Fatal("verify at exact valid_until should fail")
		}
	})
	t.Run("content digest required", func(t *testing.T) {
		ev := governance.EvidenceArtifact{TenantID: uuid.New(), ArtifactID: uuid.New(), Kind: "ATTESTATION", ContentDigest: "", ObjectStoreRef: "s3://x", SizeBytes: 10, Retention: "PERMANENT", CollectedAt: fixedInstant, CreatedAt: fixedInstant, Metadata: json.RawMessage(`{}`)}
		if err := ev.Validate(); err == nil {
			t.Fatal("empty digest should fail")
		}
		ev.ContentDigest = digestOf("ok")
		if err := ev.Validate(); err != nil {
			t.Fatalf("valid digest should pass: %v", err)
		}
	})
}

func timePtr(t time.Time) *time.Time { return &t }

func TestTodo_DB_013_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "db013-race")
	basePrincipal := newPrincipal(tenant)
	baseConn := appConn(t, db)
	inTenantTx(t, baseConn, tenant, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, basePrincipal) })
	baseSnap := newPolicySnapshot(tenant)
	inTenantTx(t, baseConn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationPolicySnapshot(ctx, tx, baseSnap) })

	t.Run("concurrent principal inserts same tenant", func(t *testing.T) {
		const workers = 8
		conns := make([]*pgxadapter.Conn, workers)
		for i := range conns {
			conns[i] = appConn(t, db)
		}
		var wg sync.WaitGroup
		errs := make([]error, workers)
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				p := newPrincipal(tenant)
				errs[idx] = inTenantTxErr(conns[idx], tenant, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, p) })
			}(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("worker %d: %v", i, err)
			}
		}
		var count int
		inTenantTx(t, baseConn, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM principal WHERE tenant_id=$1`, tenant).Scan(&count)
		})
		if count != workers+1 {
			t.Fatalf("principal count %d want %d", count, workers+1)
		}
	})

	t.Run("concurrent decisions with unique evidence_id all succeed", func(t *testing.T) {
		const workers = 8
		conns := make([]*pgxadapter.Conn, workers)
		for i := range conns {
			conns[i] = appConn(t, db)
		}
		var wg sync.WaitGroup
		var success atomic.Int32
		errs := make([]error, workers)
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				d := newDecision(tenant, basePrincipal.PrincipalID, baseSnap.SnapshotID)
				e := inTenantTxErr(conns[idx], tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, d) })
				errs[idx] = e
				if e == nil {
					success.Add(1)
				}
			}(i)
		}
		wg.Wait()
		if int(success.Load()) != workers {
			t.Fatalf("concurrent decisions success %d want %d errs=%v", success.Load(), workers, errs)
		}
	})

	t.Run("concurrent duplicate evidence_id only one wins", func(t *testing.T) {
		sharedEvidence := uuid.New()
		d1 := newDecision(tenant, basePrincipal.PrincipalID, baseSnap.SnapshotID)
		d1.EvidenceID = sharedEvidence
		inTenantTx(t, baseConn, tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, d1) })
		const workers = 6
		conns := make([]*pgxadapter.Conn, workers)
		for i := range conns {
			conns[i] = appConn(t, db)
		}
		var wg sync.WaitGroup
		errs := make([]error, workers)
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				d := newDecision(tenant, basePrincipal.PrincipalID, baseSnap.SnapshotID)
				d.EvidenceID = sharedEvidence
				errs[idx] = inTenantTxErr(conns[idx], tenant, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, d) })
			}(i)
		}
		wg.Wait()
		for _, err := range errs {
			if err == nil {
				t.Fatal("duplicate evidence_id concurrent insert should have failed")
			}
		}
	})
}

func TestTodo_DB_013_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "db013-sec-a")
	tenantB := insertTenant(t, db, "db013-sec-b")
	connA := appConn(t, db)
	pA := newPrincipal(tenantA)
	inTenantTx(t, connA, tenantA, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, pA) })
	pB := newPrincipal(tenantB)
	connB := appConn(t, db)
	inTenantTx(t, connB, tenantB, func(tx dbport.Tx) error { return governance.InsertPrincipal(ctx, tx, pB) })
	snapA := newPolicySnapshot(tenantA)
	inTenantTx(t, connA, tenantA, func(tx dbport.Tx) error { return governance.InsertAuthorizationPolicySnapshot(ctx, tx, snapA) })
	decA := newDecision(tenantA, pA.PrincipalID, snapA.SnapshotID)
	inTenantTx(t, connA, tenantA, func(tx dbport.Tx) error { return governance.InsertAuthorizationDecision(ctx, tx, decA) })
	artA := newEvidenceArtifact(tenantA)
	inTenantTx(t, connA, tenantA, func(tx dbport.Tx) error { return governance.InsertEvidenceArtifact(ctx, tx, artA) })

	t.Run("missing tenant context sees nothing", func(t *testing.T) {
		app := appConn(t, db)
		var count int
		if err := app.QueryRow(ctx, `SELECT count(*) FROM principal`).Scan(&count); err != nil {
			t.Fatalf("count without tenant: %v", err)
		}
		if count != 0 {
			t.Fatalf("unscoped app saw %d principals, want 0", count)
		}
		if err := app.QueryRow(ctx, `SELECT count(*) FROM authorization_decision`).Scan(&count); err != nil {
			t.Fatalf("count decisions without tenant: %v", err)
		}
		if count != 0 {
			t.Fatalf("unscoped app saw %d decisions, want 0", count)
		}
	})

	t.Run("cross-tenant read returns nothing", func(t *testing.T) {
		app := appConn(t, db)
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantA); err != nil {
			t.Fatalf("withTenant: %v", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM principal WHERE tenant_id=$1`, tenantB).Scan(&count); err != nil {
			t.Fatalf("cross-tenant count: %v", err)
		}
		if count != 0 {
			t.Fatal("tenant A saw tenant B rows")
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM principal WHERE principal_id=$1`, pB.PrincipalID).Scan(&count); err != nil {
			t.Fatalf("query by id: %v", err)
		}
		if count != 0 {
			t.Fatal("tenant A resolved tenant B principal by id")
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM evidence_artifact WHERE artifact_id=$1`, artA.ArtifactID).Scan(&n); err != nil {
			t.Fatalf("artifact query: %v", err)
		}
		if n != 1 {
			t.Fatalf("tenant A should see its own artifact, got %d", n)
		}
		_ = tx.Rollback(ctx)
		tx2, _ := app.Begin(ctx)
		defer func() { _ = tx2.Rollback(ctx) }()
		_ = tenancy.WithTenant(ctx, tx2, tenantB)
		if err := tx2.QueryRow(ctx, `SELECT count(*) FROM principal WHERE principal_id=$1`, pA.PrincipalID).Scan(&count); err != nil {
			t.Fatalf("tenant B query A: %v", err)
		}
		if count != 0 {
			t.Fatal("tenant B saw tenant A principal")
		}
	})

	t.Run("RLS enforced for app role", func(t *testing.T) {
		var rolsuper, rolbypassrls bool
		if err := db.Conn.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname=$1`, tenancy.AppRole).Scan(&rolsuper, &rolbypassrls); err != nil {
			t.Fatalf("pg_roles: %v", err)
		}
		if rolsuper || rolbypassrls {
			t.Fatal("app role has superuser or bypassrls")
		}
		app := appConn(t, db)
		if _, err := app.Exec(ctx, `ALTER TABLE principal DISABLE ROW LEVEL SECURITY`); err == nil {
			t.Fatal("app role disabled RLS")
		}
	})

	t.Run("forged cross-tenant write is rejected", func(t *testing.T) {
		app := appConn(t, db)
		tx, _ := app.Begin(ctx)
		defer func() { _ = tx.Rollback(ctx) }()
		_ = tenancy.WithTenant(ctx, tx, tenantA)
		_, err := tx.Exec(ctx, `INSERT INTO principal (tenant_id, principal_id, kind, subject, assurance, authn_method, revocation_epoch, lifecycle, created_at, updated_at, metadata) VALUES ($1,$2,'USER','user:evil','AAL1','PASSWORD',1,'ACTIVE',now(),now(),'{}')`, tenantB, uuid.New())
		if err == nil {
			t.Fatal("forged cross-tenant principal insert not rejected")
		}
	})

	t.Run("unregistered tenant reads empty", func(t *testing.T) {
		unknown := uuid.New()
		app := appConn(t, db)
		tx, _ := app.Begin(ctx)
		defer func() { _ = tx.Rollback(ctx) }()
		_ = tenancy.WithTenant(ctx, tx, unknown)
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM principal`).Scan(&count); err != nil {
			t.Fatalf("unknown tenant count: %v", err)
		}
		if count != 0 {
			t.Fatalf("unknown tenant saw %d rows", count)
		}
	})
}
