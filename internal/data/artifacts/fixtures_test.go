package artifacts_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	mediaType      = "application/octet-stream"
	retentionClass = "artifact-standard"
	creatorRef     = "authority:workday"
	evidenceRef    = "ev:test:seed"
)

// fixedNow is the trusted clock reading tests hand to [artifacts.Retrieve]
// wherever the exact instant is not itself under test.
var fixedNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// fixture is a migrated schema (the core migration tree plus its companion
// artifact-store schema, migrations/00010_artifacts.sql) with one registered
// tenant.
type fixture struct {
	db     *pgtest.DB
	schema string
	tenant uuid.UUID
}

// newFixture stands up one tenant. schema is the companion artifact-store
// schema [artifacts.Schema] computes from the migrated core schema.
func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	return fixture{
		db:     db,
		schema: artifacts.Schema(db.Schema),
		tenant: insertTenant(t, db, "acme"),
	}
}

// insertTenant registers one active tenant as the admin role (the pgtest
// connection, a PostgreSQL superuser never itself subject to row level
// security).
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// appRoleConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, exactly the way
// internal/data/tenancy_test's fixtures do: the pgtest URL authenticates as a
// superuser, which may always SET ROLE to any role at all.
func appRoleConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// putRequest returns a well-formed PutRequest for content under this
// fixture's tenant.
func (f fixture) putRequest(content []byte) artifacts.PutRequest {
	return artifacts.PutRequest{
		Tenant:              f.tenant,
		Content:             content,
		MediaType:           mediaType,
		Classification:      model.ClassPII,
		RetentionClass:      retentionClass,
		CreatorPrincipalRef: creatorRef,
		EvidenceID:          evidenceRef,
	}
}

// inTx runs fn in a transaction on the fixture's own connection, committing
// on success and rolling back on failure, and returns fn's error.
func (f fixture) inTx(fn func(dbport.Tx) error) error {
	return inTxErr(f.db.Conn, fn)
}

func inTxErr(conn *pgxadapter.Conn, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	return tx.Commit(ctx)
}

// put runs one Put in its own transaction.
func (f fixture) put(req artifacts.PutRequest) (artifacts.Record, bool, error) {
	var rec artifacts.Record
	var created bool
	err := f.inTx(func(tx dbport.Tx) error {
		var putErr error
		rec, created, putErr = artifacts.Put(context.Background(), tx, f.schema, req)
		return putErr
	})
	return rec, created, err
}

// mustPut fails the test when Put is refused.
func (f fixture) mustPut(t *testing.T, req artifacts.PutRequest) artifacts.Record {
	t.Helper()
	rec, _, err := f.put(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return rec
}

// addReference runs one AddReference in its own transaction.
func (f fixture) addReference(contentID string, owner artifacts.OwnerRef) error {
	return f.inTx(func(tx dbport.Tx) error {
		return artifacts.AddReference(context.Background(), tx, f.schema, f.tenant, contentID, owner)
	})
}

// mustAddReference fails the test when AddReference is refused.
func (f fixture) mustAddReference(t *testing.T, contentID string, owner artifacts.OwnerRef) {
	t.Helper()
	if err := f.addReference(contentID, owner); err != nil {
		t.Fatalf("add reference %s to %s: %v", owner.ID, contentID, err)
	}
}

// removeReference runs one RemoveReference in its own transaction.
func (f fixture) removeReference(contentID string, owner artifacts.OwnerRef) error {
	return f.inTx(func(tx dbport.Tx) error {
		return artifacts.RemoveReference(context.Background(), tx, f.schema, f.tenant, contentID, owner)
	})
}

// retrieve runs one Retrieve in its own transaction. Per [artifacts.Retrieve]'s
// documented contract, a denial is not a transaction failure: the refusal row
// it already wrote is only durable if this still commits, so this helper
// commits on ErrRetrievalDenied exactly as any real caller must, and rolls
// back only on a genuinely unexpected error.
func (f fixture) retrieve(contentID string, auth artifacts.RetrievalAuthorization, now time.Time) ([]byte, artifacts.Record, error) {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		return nil, artifacts.Record{}, fmt.Errorf("begin: %w", err)
	}

	content, rec, retrieveErr := artifacts.Retrieve(ctx, tx, f.schema, f.tenant, contentID, auth, now)

	var denied artifacts.ErrRetrievalDenied
	if retrieveErr == nil || errors.As(retrieveErr, &denied) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, artifacts.Record{}, fmt.Errorf("commit: %w", commitErr)
		}
		return content, rec, retrieveErr
	}

	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		return nil, artifacts.Record{}, errors.Join(retrieveErr, fmt.Errorf("rollback: %w", rollbackErr))
	}
	return nil, artifacts.Record{}, retrieveErr
}

// allowAll is a RetrievalAuthorization that grants every classification and
// every subject, for tests whose focus is something other than DATA-016
// enforcement itself.
func allowAll(requestedBy string) artifacts.RetrievalAuthorization {
	return artifacts.RetrievalAuthorization{
		Purpose: "test.read",
		AllowedClassifications: []model.ClassificationLabel{
			model.ClassPublic, model.ClassInternal, model.ClassPII, model.ClassCompensation,
			model.ClassBank, model.ClassMedical, model.ClassImmigration, model.ClassCase,
			model.ClassSpecialCategory,
		},
		Scope:       artifacts.SubjectScope{AllSubjects: true},
		RequestedBy: requestedBy,
	}
}

// artifactsReferenceCount runs ReferenceCount in its own transaction.
func artifactsReferenceCount(f fixture, contentID string) (int64, error) {
	var count int64
	err := f.inTx(func(tx dbport.Tx) error {
		var countErr error
		count, countErr = artifacts.ReferenceCount(context.Background(), tx, f.schema, f.tenant, contentID)
		return countErr
	})
	return count, err
}

// latestRefusalEvidenceIDs returns the most recent n evidence ids recorded
// against contentID, most recent first.
func latestRefusalEvidenceIDs(t *testing.T, f fixture, contentID string, n int) []string {
	t.Helper()
	table := pgx.Identifier{f.schema, "artifact_retrieval_refusal"}.Sanitize()
	rows, err := f.db.Conn.Query(context.Background(),
		fmt.Sprintf(`SELECT evidence_id FROM %s WHERE tenant_id = $1 AND content_id = $2
			ORDER BY refused_at DESC, refusal_id DESC LIMIT $3`, table),
		f.tenant, contentID, n)
	if err != nil {
		t.Fatalf("list refusal evidence for %s: %v", contentID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan refusal evidence for %s: %v", contentID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list refusal evidence for %s: %v", contentID, err)
	}
	return ids
}

// rlsVisibleCrossTenant queries the artifact table directly as the
// least-privilege hcmnext_app role, scoped (via tenancy.WithTenant) to
// foreignTenant rather than to the tenant that actually owns contentID. It
// proves migrations/00010_artifacts.sql's row level security policy, not
// just this package's own WHERE clauses, keeps one tenant's rows out of
// another's reach on the companion artifact-store schema.
func rlsVisibleCrossTenant(t *testing.T, f fixture, foreignTenant uuid.UUID, contentID string) bool {
	t.Helper()
	ctx := context.Background()
	conn := appRoleConn(t, f.db)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin as %s: %v", tenancy.AppRole, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, foreignTenant); err != nil {
		t.Fatalf("scope to foreign tenant: %v", err)
	}

	table := pgx.Identifier{f.schema, "artifact"}.Sanitize()
	var count int
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE content_id = $1`, table), contentID).Scan(&count); err != nil {
		t.Fatalf("query %s as %s scoped to a foreign tenant: %v", table, tenancy.AppRole, err)
	}
	return count > 0
}

func refusalCount(t *testing.T, f fixture, contentID string) int {
	t.Helper()
	table := pgx.Identifier{f.schema, "artifact_retrieval_refusal"}.Sanitize()
	var count int
	if err := f.db.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1 AND content_id = $2`, table),
		f.tenant, contentID).Scan(&count); err != nil {
		t.Fatalf("count refusals for %s: %v", contentID, err)
	}
	return count
}
