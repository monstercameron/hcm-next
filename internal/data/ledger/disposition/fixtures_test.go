package disposition_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const (
	schemaRef    = "hcmnext.intents.v1.BusinessIntent@1"
	authorityRef = "authority:workday"
	streamKey    = "worker:ledger011"
)

var (
	occurredAt  = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	fixedAt     = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
)

// appConn returns a connection that has assumed the least-privilege
// application role, exactly as internal/data/recordsmeta's own tests do, so
// these tests exercise the real grants migration 00285 declares rather than
// running as the schema owner.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// registerLedgerSchema and registerAuthority satisfy ledger_event's own FKs
// (payload_schema, authority_assignment) for every event these tests append.
func registerLedgerSchema(t *testing.T, db *pgtest.DB, tenant uuid.UUID) {
	t.Helper()
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,'hcmnext.intents.v1.BusinessIntent',1,'hcmnext.intents.v1.BusinessIntent','PROTOBUF','LEDGER_EVENT')`,
		tenant, schemaRef)
	db.Exec(t, `INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from) VALUES ($1,$2,'EXTERNAL_SYSTEM','workforce.compensation',timestamptz '2026-01-01T00:00:00Z')`,
		tenant, authorityRef)
}

// registerRecordsCopySchema satisfies outbox_schema: recordsmeta.PropagateHold's
// outbox row cites recordsmeta.RecordsCopySchemaRef, and outbox's FK to
// payload_schema requires that reference to exist before any hold can be
// propagated.
func registerRecordsCopySchema(t *testing.T, db *pgtest.DB, tenant uuid.UUID) {
	t.Helper()
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,$2,1,$2,'PROTOBUF','EVIDENCE_MANIFEST')`,
		tenant, recordsmeta.RecordsCopySchemaRef)
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

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// newChainAppender builds the hash-chain writer every append in these tests
// extends, mirroring the composition internal/data/ledger/hashchain's own
// doc comments describe for a real writer.
func newChainAppender(t *testing.T) *hashchain.Appender {
	t.Helper()
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("hashchain registry: %v", err)
	}
	return hashchain.NewAppender(hashchain.NewDigester(registry))
}

func newChainDigester(t *testing.T) *hashchain.Digester {
	t.Helper()
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("hashchain registry: %v", err)
	}
	return hashchain.NewDigester(registry)
}

func ensureStream(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, key string) {
	t.Helper()
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return ledger.EnsureStream(context.Background(), tx, tenant, key, "WORKER", "worker:1")
	})
}

// appendRequest returns a well-formed append request for expectedHead,
// carrying payload as its inline bytes (nil means "use artifactRef
// instead").
func appendRequest(tenant uuid.UUID, key string, expectedHead int64, payload []byte, artifactRef string) ledger.AppendRequest {
	return ledger.AppendRequest{
		Tenant: tenant, StreamKey: key, ExpectedHead: expectedHead,
		AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:workflow", SchemaRef: schemaRef,
		Payload: payload, ArtifactRef: artifactRef,
		OccurredAt: occurredAt, EffectiveAt: effectiveAt,
		CorrelationID: uuid.New(), IdempotencyKey: uuid.NewString(),
	}
}

// appendWithChain appends req and records its hash-chain link in the same
// transaction -- the composition every real writer performs -- and returns
// the receipt.
func appendWithChain(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, chain *hashchain.Appender, req ledger.AppendRequest) ledger.AppendReceipt {
	t.Helper()
	var receipt ledger.AppendReceipt
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = ledger.Append(context.Background(), tx, req)
		if appendErr != nil {
			return appendErr
		}
		_, appendErr = chain.Append(context.Background(), tx, receipt)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append with chain at expected head %d: %v", req.ExpectedHead, err)
	}
	return receipt
}

// newDeclaration returns a well-formed record declaration, eligible for
// disposition, that RECORDS-HOLD-001's own tests use the same shape of.
func newDeclaration(tenant uuid.UUID) recordsmeta.RecordDeclaration {
	cutoff := fixedAt
	eligible := fixedAt.Add(24 * time.Hour)
	return recordsmeta.RecordDeclaration{
		TenantID: tenant, DeclarationID: uuid.New(),
		SubjectEntityRef: "worker:" + uuid.NewString(), ArtifactRef: "artifact://doc/" + uuid.NewString(),
		RecordSeries: "HR-100", RecordClass: "PERSONNEL",
		OwnerRef: "principal:hr", CustodianRef: "principal:records",
		CutoffTrigger: "TERMINATION", CutoffAt: &cutoff,
		RetentionScheduleKey: "schedule:hr-100", RetentionScheduleVersion: 3,
		DispositionEligibleAt: &eligible, Status: "ELIGIBLE", CreatedAt: fixedAt,
	}
}

// newHold returns a well-formed hold with its ScopeDigest already computed
// by recordsmeta.CanonicalScopeDigest -- legal_hold.scope_digest is a
// content_digest domain column (exactly 64 lowercase hex characters), so an
// arbitrary placeholder string would fail the column's own CHECK constraint.
func newHold(t *testing.T, tenant uuid.UUID) recordsmeta.LegalHold {
	t.Helper()
	h := recordsmeta.LegalHold{
		TenantID: tenant, HoldID: uuid.New(),
		MatterRef: "matter:" + uuid.NewString(), AuthorityRef: "principal:legal",
		HoldVersion: 1, Reason: "pending litigation",
		ScopePredicate: []byte(`{"record_series":"HR-100"}`),
		PlacedBy:       "principal:legal", PlacedAt: fixedAt, Status: "ACTIVE",
	}
	digest, err := recordsmeta.CanonicalScopeDigest(h.ScopePredicate)
	if err != nil {
		t.Fatalf("canonical scope digest: %v", err)
	}
	h.ScopeDigest = digest
	return h
}

// readView runs disposition.ReadView inside a tenant-scoped transaction on
// the application-role connection, exactly the context any real caller
// would read through: a bare connection with no app.tenant_id set would
// have every tenant-scoped row hidden by row level security, which would
// misreport as ErrEventNotFound rather than exercising ReadView itself.
func readView(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, key string, sequence int64) disposition.View {
	t.Helper()
	var v disposition.View
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		var err error
		v, err = disposition.ReadView(context.Background(), tx, tenant, key, sequence)
		return err
	})
	if err != nil {
		t.Fatalf("ReadView %s@%d: %v", key, sequence, err)
	}
	return v
}

func loadHold(t *testing.T, conn *pgxadapter.Conn, tenant, holdID uuid.UUID) recordsmeta.LegalHold {
	t.Helper()
	var h recordsmeta.LegalHold
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		var err error
		h, err = recordsmeta.LoadLegalHold(context.Background(), tx, tenant, holdID)
		return err
	})
	if err != nil {
		t.Fatalf("load hold %s: %v", holdID, err)
	}
	return h
}

func listIntersections(t *testing.T, conn *pgxadapter.Conn, tenant, holdID uuid.UUID) []recordsmeta.HoldIntersection {
	t.Helper()
	var out []recordsmeta.HoldIntersection
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = recordsmeta.ListHoldIntersections(context.Background(), tx, tenant, holdID)
		return err
	})
	if err != nil {
		t.Fatalf("list intersections for hold %s: %v", holdID, err)
	}
	return out
}

// verifyChain runs the real internal/data/ledger/hashchain verifier inside a
// tenant-scoped transaction and returns the reproduced head.
func verifyChain(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, key string, digester *hashchain.Digester) hashchain.Head {
	t.Helper()
	var head hashchain.Head
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		var err error
		head, err = digester.Verify(context.Background(), tx, tenant, key)
		return err
	})
	if err != nil {
		t.Fatalf("verify chain for %s: %v", key, err)
	}
	return head
}

// rawDispositionRow is a direct read of ledger_payload_disposition, used by
// tests to inspect fields (like the symbolic key reference and the reason/
// actor evidence) that disposition.View deliberately does not expose in
// full. Every field is a plain, comparable value (never a pointer) so two
// rawDispositionRow values read at different times can be compared with ==
// without a false "changed" report caused by two separately allocated
// pointers to equal contents.
type rawDispositionRow struct {
	Classification    string
	Mechanism         string
	State             string
	Reason            string
	Actor             string
	HasKeyRef         bool
	KeyRef            string
	HasKeyDestroyedAt bool
	KeyDestroyedAt    time.Time
	Digest            string
}

func readRawDisposition(t *testing.T, db *pgtest.DB, tenant uuid.UUID, key string, sequence int64) rawDispositionRow {
	t.Helper()
	var (
		r              rawDispositionRow
		keyRef         *string
		keyDestroyedAt *time.Time
	)
	err := db.QueryRow(context.Background(), `
		SELECT classification, mechanism, state, reason, actor, key_ref, key_destroyed_at, digest
		FROM ledger_payload_disposition WHERE tenant_id=$1 AND stream_key=$2 AND sequence=$3`,
		tenant, key, sequence).Scan(&r.Classification, &r.Mechanism, &r.State, &r.Reason, &r.Actor, &keyRef, &keyDestroyedAt, &r.Digest)
	if err != nil {
		t.Fatalf("read raw disposition %s@%d: %v", key, sequence, err)
	}
	if keyRef != nil {
		r.HasKeyRef, r.KeyRef = true, *keyRef
	}
	if keyDestroyedAt != nil {
		r.HasKeyDestroyedAt, r.KeyDestroyedAt = true, *keyDestroyedAt
	}
	return r
}

func dispositionRowExists(t *testing.T, db *pgtest.DB, tenant uuid.UUID, key string, sequence int64) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM ledger_payload_disposition WHERE tenant_id=$1 AND stream_key=$2 AND sequence=$3)`,
		tenant, key, sequence).Scan(&exists); err != nil {
		t.Fatalf("check disposition row %s@%d: %v", key, sequence, err)
	}
	return exists
}

// rawEventRow is a byte-for-byte snapshot of every column
// ledger_event carries, used to prove Erase leaves the row untouched.
type rawEventRow struct {
	Tenant            uuid.UUID
	StreamKey         string
	Sequence          int64
	EventID           uuid.UUID
	AssertionClass    string
	Authority         *string
	SourceRef         string
	SchemaRef         string
	Payload           []byte
	ArtifactRef       *string
	CanonicalLength   int
	Digest            string
	DigestAlgorithm   string
	OccurredAt        time.Time
	EffectiveAt       time.Time
	RecordedAt        time.Time
	CorrelationID     uuid.UUID
	CausationID       *uuid.UUID
	IdempotencyKey    string
	CorrectsStreamKey *string
	CorrectsSequence  *int64
}

func readRawEvent(t *testing.T, db *pgtest.DB, tenant uuid.UUID, key string, sequence int64) rawEventRow {
	t.Helper()
	var r rawEventRow
	err := db.QueryRow(context.Background(), `
		SELECT tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
		       source_ref, schema_ref, payload, artifact_ref, canonical_length, digest,
		       digest_algorithm, occurred_at, effective_at, recorded_at, correlation_id,
		       causation_id, idempotency_key, corrects_stream_key, corrects_sequence
		FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2 AND sequence=$3`,
		tenant, key, sequence).Scan(
		&r.Tenant, &r.StreamKey, &r.Sequence, &r.EventID, &r.AssertionClass, &r.Authority,
		&r.SourceRef, &r.SchemaRef, &r.Payload, &r.ArtifactRef, &r.CanonicalLength, &r.Digest,
		&r.DigestAlgorithm, &r.OccurredAt, &r.EffectiveAt, &r.RecordedAt, &r.CorrelationID,
		&r.CausationID, &r.IdempotencyKey, &r.CorrectsStreamKey, &r.CorrectsSequence)
	if err != nil {
		t.Fatalf("read raw ledger_event %s@%d: %v", key, sequence, err)
	}
	return r
}
