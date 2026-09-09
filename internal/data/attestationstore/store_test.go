package attestationstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/attestation"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 45); err != nil {
		t.Fatalf("apply migrations through 00045: %v", err)
	}
	return db
}

func tenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantValue(id uuid.UUID) values.TenantId { return values.TenantId(id.String()) }

func inTenant(t *testing.T, conn *pgxadapter.Conn, id uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func statement(tenant string, id string, version uint64) attestation.AttestationStatement {
	person := values.EntityRef{Tenant: values.TenantId(tenant), Kind: "person", Id: "550e8400-e29b-41d4-a716-446655440000"}
	return attestation.AttestationStatement{
		ID: id, Version: version, Kind: attestation.StatementKindConsent, SubjectRef: person,
		Attester:        attestation.AttesterPrincipal{PrincipalRef: person, IdentityAssuranceRef: attestation.IdentityAssuranceRef{ID: "assurance-1", Kind: "CREDENTIAL"}},
		TextDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EvidenceRefs:    []attestation.EvidenceRef{{ID: "evidence-1", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		ValidityWindow:  attestation.ValidityWindow{StartsAt: values.NewInstant(time.Unix(100, 0)), ExpiresAt: values.NewInstant(time.Unix(200, 0))},
		JurisdictionRef: values.EntityRef{Tenant: values.TenantId(tenant), Kind: "jurisdiction", Id: "550e8400-e29b-41d4-a716-446655440001"},
	}
}

func binding(tenant string, version uint64) attestation.Binding {
	return attestation.Binding{
		StatementID: "statement-1", StatementVersion: 1,
		ContextDigest:    attestation.ContextDigest{SubjectAsOf: "sha256:subject", Jurisdiction: "US", Locale: "en-US", RenderedTextHash: "sha256:text"},
		EvidenceBindings: []attestation.EvidenceBinding{{EvidenceID: "evidence-1", EvidenceHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		BindingVersion:   version, BoundAt: values.NewInstant(time.Unix(150, 0)),
		Signer: values.EntityRef{Tenant: values.TenantId(tenant), Kind: "person", Id: "550e8400-e29b-41d4-a716-446655440002"},
	}
}

func TestTodo_PERSIST_ATTESTATION_001(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	id := tenant(t, db, "persist-attestation-primary")
	store := New(conn)
	ctx := context.Background()
	tenantKey := tenantValue(id)
	stmt := statement(tenantKey.String(), "statement-1", 1)

	if err := store.PutStatement(ctx, tenantKey, stmt); err != nil {
		t.Fatal(err)
	}
	if CodeOf(store.PutStatement(ctx, tenantKey, stmt)) != CodeDuplicateRevision {
		t.Fatalf("duplicate code = %q", CodeOf(store.PutStatement(ctx, tenantKey, stmt)))
	}
	loaded, err := store.GetStatement(ctx, tenantKey, stmt.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != stmt.ID || loaded.Version != stmt.Version || loaded.Digest() != stmt.Digest() {
		t.Fatalf("statement was not rehydrated: got %+v", loaded)
	}
	if err := store.AppendBinding(ctx, tenantKey, binding(tenantKey.String(), 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendBinding(ctx, tenantKey, binding(tenantKey.String(), 2)); err != nil {
		t.Fatal(err)
	}
	bindings, err := store.ListBindings(ctx, tenantKey, "statement-1")
	if err != nil || len(bindings) != 2 {
		t.Fatalf("bindings = %d, err=%v", len(bindings), err)
	}
	requirement := humanwork.ApprovalRequirement{
		RequirementID: "approval-1", Revision: 1, Stage: 1,
		ExpressionDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Deadline:         humanwork.Deadline{DecideBy: values.NewInstant(time.Unix(100, 0)), Expiry: values.NewInstant(time.Unix(200, 0))},
	}
	if err := store.PutApprovalRequirement(ctx, tenantKey, requirement); err != nil {
		t.Fatal(err)
	}
	row, err := store.LoadApprovalRequirement(ctx, tenantKey, requirement.RequirementID, 1)
	if err != nil || row.ExpressionDigest != requirement.ExpressionDigest {
		t.Fatalf("approval requirement row = %+v, err=%v", row, err)
	}
	resolution := humanwork.Resolution{
		RequirementID: "approval-1", RequirementRevision: 1, Outcome: humanwork.OutcomeNoAuthorizedApprover,
		ResolvedAt: values.NewInstant(time.Unix(201, 0)), EffectiveAt: values.NewInstant(time.Unix(150, 0)),
		ExpressionDigest: requirement.ExpressionDigest, RequirementDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
	if err := store.AppendResolution(ctx, tenantKey, resolution, 1); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_ATTESTATION_001_Fault(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	id := tenant(t, db, "persist-attestation-fault")
	store := New(conn)
	ctx := context.Background()
	key := tenantValue(id)
	if err := store.PutStatement(ctx, key, statement(key.String(), "statement-1", 1)); err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.PutStatement(ctx, key, statement(key.String(), "statement-1", 1))); got != CodeDuplicateRevision {
		t.Fatalf("duplicate revision code = %q", got)
	}
	if got := CodeOf(store.PutStatement(ctx, key, statement(key.String(), "statement-1", 3), 0)); got != CodeVersionConflict {
		t.Fatalf("stale/gapped CAS code = %q", got)
	}
	if err := store.AppendBinding(ctx, key, binding(key.String(), 1)); err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.AppendBinding(ctx, key, binding(key.String(), 1))); got != CodeDuplicateEvent {
		t.Fatalf("duplicate binding code = %q", got)
	}
}

func TestTodo_PERSIST_ATTESTATION_001_Integration(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	id := tenant(t, db, "persist-attestation-integration")
	store := New(conn)
	key := tenantValue(id)
	ctx := context.Background()
	if err := store.PutStatement(ctx, key, statement(key.String(), "statement-1", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutStatement(ctx, key, statement(key.String(), "statement-1", 2), 1); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListStatementVersions(ctx, key, "statement-1")
	if err != nil || len(versions) != 2 || versions[1].Version != 2 {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
}

func TestTodo_PERSIST_ATTESTATION_001_Security(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	a := tenant(t, db, "persist-attestation-a")
	b := tenant(t, db, "persist-attestation-b")
	store := New(conn)
	if err := store.PutStatement(context.Background(), tenantValue(a), statement(tenantValue(a).String(), "statement-a", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutStatement(context.Background(), tenantValue(b), statement(tenantValue(b).String(), "statement-b", 1)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, a); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM attestation_statement WHERE tenant_id=$1`, b).Scan(&count); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	if count != 0 {
		t.Fatalf("tenant A saw %d rows from tenant B", count)
	}
}

func TestTodo_PERSIST_ATTESTATION_001_Recovery(t *testing.T) {
	db := newDB(t)
	id := tenant(t, db, "persist-attestation-recovery")
	key := tenantValue(id)
	first := appConn(t, db)
	if err := New(first).PutStatement(context.Background(), key, statement(key.String(), "statement-1", 1)); err != nil {
		t.Fatal(err)
	}
	second := appConn(t, db)
	loaded, err := New(second).GetStatement(context.Background(), key, "statement-1", 1)
	if err != nil || loaded.ID != "statement-1" {
		t.Fatalf("fresh connection load = %+v, %v", loaded, err)
	}
}

func TestTodo_PERSIST_ATTESTATION_001_Mutation(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	id := tenant(t, db, "persist-attestation-mutation")
	key := tenantValue(id)
	store := New(conn)
	if err := store.PutStatement(context.Background(), key, statement(key.String(), "statement-1", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendBinding(context.Background(), key, binding(key.String(), 1)); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE attestation_binding SET signer=$1 WHERE tenant_id=$2`, uuid.New(), id); err == nil {
		t.Fatal("append-only binding accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM attestation_binding WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("append-only binding accepted DELETE")
	}
	if err := db.ExecErr(`UPDATE attestation_statement SET kind='POSITIONAL' WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("immutable statement accepted UPDATE")
	}
	if got := CodeOf(store.PutStatement(context.Background(), key, statement(key.String(), "statement-1", 1))); got != CodeDuplicateRevision {
		t.Fatalf("duplicate after mutation code = %q", got)
	}
}

func TestAttestationStore_AliasesValidationAndNotFoundBranches(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	id := tenant(t, db, "persist-attestation-negative")
	key := tenantValue(id)
	store := New(conn)
	ctx := context.Background()
	stmt := statement(key.String(), "statement-negative", 1)
	if err := store.SaveStatement(ctx, key, stmt); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStatement(ctx, key, statement(key.String(), stmt.ID, 2), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetStatement(ctx, key, "missing", 1); CodeOf(err) != CodeNotFound || !errors.Is(err, attestation.ErrStatementNotFound) {
		t.Fatalf("missing statement = %v", err)
	}
	if _, err := store.ListStatementVersions(ctx, key, "missing"); CodeOf(err) != CodeNotFound || !errors.Is(err, attestation.ErrStatementNotFound) {
		t.Fatalf("missing statement versions = %v", err)
	}
	if err := store.PutStatement(ctx, key, stmt, 1, 2); CodeOf(err) != CodeInvalid {
		t.Fatalf("too many statement CAS args = %v", err)
	}
	badSubject := stmt
	badSubject.ID = "bad-subject"
	badSubject.SubjectRef.Id = "not-a-uuid"
	if err := store.PutStatement(ctx, key, badSubject); CodeOf(err) != CodeInvalid {
		t.Fatalf("invalid statement subject = %v", err)
	}
	b := binding(key.String(), 1)
	if err := store.SaveBinding(ctx, key, b); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListBindings(ctx, key, "missing"); CodeOf(err) != CodeNotFound || !errors.Is(err, attestation.ErrStatementNotFound) {
		t.Fatalf("missing bindings = %v", err)
	}
	req := humanwork.ApprovalRequirement{RequirementID: "req-negative", Revision: 1, Stage: 1, ExpressionDigest: "sha256:" + strings.Repeat("c", 64), Deadline: humanwork.Deadline{DecideBy: values.NewInstant(time.Unix(100, 0)), Expiry: values.NewInstant(time.Unix(200, 0))}}
	if err := store.PutApprovalRequirement(ctx, key, req); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadApprovalRequirement(ctx, key, "missing", 1); CodeOf(err) != CodeNotFound || !errors.Is(err, attestation.ErrStatementNotFound) {
		t.Fatalf("missing approval requirement = %v", err)
	}
	if err := store.PutApprovalRequirement(ctx, key, req, 1, 2); CodeOf(err) != CodeInvalid {
		t.Fatalf("too many approval CAS args = %v", err)
	}
	if err := store.AppendResolution(ctx, key, humanwork.Resolution{}, 0); CodeOf(err) != CodeInvalid {
		t.Fatalf("invalid resolution = %v", err)
	}
	if _, err := store.GetStatement(ctx, values.TenantId("not-a-uuid"), stmt.ID, 1); CodeOf(err) != CodeInvalid {
		t.Fatalf("invalid tenant = %v", err)
	}
}
