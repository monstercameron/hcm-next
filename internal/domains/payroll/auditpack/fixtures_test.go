package auditpack_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/payroll/auditpack"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const testKeyID = "hcmnext:payroll:auditpack:test"

// testSigningSeed is a fixed Ed25519 seed, so a signed checkpoint - and
// therefore any digest computed over it - is reproducible across runs.
var testSigningSeed = [ed25519.SeedSize]byte{
	0x41, 0x55, 0x44, 0x49, 0x54, 0x50, 0x41, 0x43,
	0x4b, 0x2d, 0x30, 0x32, 0x34, 0x2f, 0x74, 0x65,
	0x73, 0x74, 0x2d, 0x73, 0x69, 0x67, 0x6e, 0x69,
	0x6e, 0x67, 0x2d, 0x73, 0x65, 0x65, 0x64, 0x21,
}

func testSigner(t testing.TB) *checkpoint.Ed25519Signer {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(testSigningSeed[:])
	signer, err := checkpoint.NewEd25519Signer(testKeyID, priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}
	return signer
}

var keyValidFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fixture is a migrated schema holding one tenant and the fixed signing
// material an auditor package's checkpoint epoch is signed with.
type fixture struct {
	db      *pgtest.DB
	tenant  uuid.UUID
	signer  *checkpoint.Ed25519Signer
	release evidence.SchemaRelease
	chainer *hashchain.Appender
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)

	signer := testSigner(t)
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build chain registry: %v", err)
	}
	version, err := migrations.TargetVersion()
	if err != nil {
		t.Fatalf("migration target version: %v", err)
	}
	artifact, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("migration artifact digest: %v", err)
	}

	f := fixture{
		db: db, tenant: uuid.New(), signer: signer,
		release: evidence.SchemaRelease{Version: version, Digest: artifact},
		chainer: hashchain.NewAppender(hashchain.NewDigester(registry)),
	}
	f.seedTenant(t, f.tenant)
	return f
}

// seedTenant registers a tenant, the two payload schemas this package's
// events are recorded under (the ledger_event table's own foreign key
// requires a payload_schema row before an event under that schema can be
// appended at all), and no stream yet - streams are registered per run by
// ensureStream, since a fixture may exercise more than one run.
func (f fixture) seedTenant(t *testing.T, tenant uuid.UUID) {
	t.Helper()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "tenant-"+tenant.String()[:8])
	for _, ref := range []string{auditpack.LineSchemaRef, auditpack.BindingSchemaRef} {
		f.db.Exec(t, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')`,
			tenant, ref)
	}
}

func (f fixture) ensureStream(t *testing.T, tenant uuid.UUID, runID string) {
	t.Helper()
	f.inTx(t, func(tx dbport.Tx) error {
		return datalogger.EnsureStream(context.Background(), tx, tenant, auditpack.StreamKey(runID), "PAYROLL_RUN", runID)
	})
}

func (f fixture) withTenant(t *testing.T) fixture {
	t.Helper()
	other := f
	other.tenant = uuid.New()
	other.seedTenant(t, other.tenant)
	return other
}

func (f fixture) keyDirectory(notAfter, revokedAt time.Time) checkpoint.KeyDirectory {
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: testKeyID, PublicKey: f.signer.PublicKey(),
		NotBefore: keyValidFrom, NotAfter: notAfter, RevokedAt: revokedAt,
	})
}

func (f fixture) liveKeyDirectory() checkpoint.KeyDirectory {
	return f.keyDirectory(time.Time{}, time.Time{})
}

func (f fixture) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	if err := f.inTxErr(fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f fixture) inTxErr(fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
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

// appendLine appends and hash-chains one contributing line at recordedAt,
// using a fresh appender clocked to exactly that instant so the recorded
// time is pinned rather than "whenever the test happened to run".
func (f fixture) appendLine(t *testing.T, tenant uuid.UUID, runID string, kind auditpack.TotalKind, amountText string, expectedHead int64, recordedAt time.Time) datalogger.AppendReceipt {
	t.Helper()
	amount, err := values.NewDecimal(amountText, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("decimal %s: %v", amountText, err)
	}
	appender := datalogger.New(datalogger.WithClock(func() time.Time { return recordedAt.UTC() }))
	var receipt datalogger.AppendReceipt
	err = f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = auditpack.AppendLine(context.Background(), tx, appender, f.chainer, auditpack.LineRequest{
			Tenant: tenant, RunID: runID, Kind: kind, Amount: amount, Currency: "USD",
			ExpectedHead: expectedHead, OccurredAt: recordedAt, EffectiveAt: recordedAt,
			CorrelationID: uuid.New(), IdempotencyKey: uuid.NewString(),
		})
		return appendErr
	})
	if err != nil {
		t.Fatalf("append line %s@%d: %v", auditpack.StreamKey(runID), expectedHead+1, err)
	}
	return receipt
}

// bind resolves and binds the run's reconciliation at recordedAt.
func (f fixture) bind(t *testing.T, tenant uuid.UUID, runID string, expectedHead int64, recordedAt time.Time, totals auditpack.RunTotals, decision auditpack.Decision) (datalogger.AppendReceipt, error) {
	t.Helper()
	appender := datalogger.New(datalogger.WithClock(func() time.Time { return recordedAt.UTC() }))
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var bindErr error
		receipt, bindErr = auditpack.Bind(context.Background(), tx, appender, f.chainer, auditpack.BindRequest{
			Tenant: tenant, RunID: runID, ExpectedHead: expectedHead,
			OccurredAt: recordedAt, EffectiveAt: recordedAt, CorrelationID: uuid.New(),
		}, totals, decision)
		return bindErr
	})
	return receipt, err
}

func (f fixture) service(t *testing.T, dir checkpoint.KeyDirectory) *checkpoint.Service {
	t.Helper()
	svc, err := checkpoint.NewService(f.signer, dir)
	if err != nil {
		t.Fatalf("build checkpoint service: %v", err)
	}
	return svc
}

func (f fixture) mustCheckpoint(t *testing.T, tenant uuid.UUID, at time.Time) checkpoint.Manifest {
	t.Helper()
	svc := f.service(t, f.liveKeyDirectory())
	var manifest checkpoint.Manifest
	err := f.inTxErr(func(tx dbport.Tx) error {
		var createErr error
		manifest, createErr = svc.Create(context.Background(), tx, checkpoint.CreateRequest{
			Tenant: tenant, Schema: f.release, At: at,
		})
		return createErr
	})
	if err != nil {
		t.Fatalf("create checkpoint at %s: %v", at, err)
	}
	return manifest
}

// goodRun seeds one reconciling run: register 1000.00 = bank file 800.00 +
// tax liability 200.00, and tax liability 200.00 = filing acknowledgment
// 200.00. from is the run's earliest recorded instant, to is the checkpoint
// instant (the export window's exclusive end).
func (f fixture) goodRun(t *testing.T, tenant uuid.UUID, runID string, from time.Time) (to time.Time) {
	t.Helper()
	f.ensureStream(t, tenant, runID)
	f.appendLine(t, tenant, runID, auditpack.KindRegister, "1000.00", 0, from)
	f.appendLine(t, tenant, runID, auditpack.KindBankFile, "800.00", 1, from.Add(time.Minute))
	f.appendLine(t, tenant, runID, auditpack.KindTaxLiability, "200.00", 2, from.Add(2*time.Minute))
	to = from.Add(3 * time.Minute)
	f.appendLine(t, tenant, runID, auditpack.KindFilingAcknowledgment, "200.00", 3, from.Add(2*time.Minute+30*time.Second))
	return to
}

func (f fixture) exportRequest(tenant uuid.UUID, runID string, from, to time.Time) auditpack.ExportRequest {
	return auditpack.ExportRequest{Tenant: tenant, RunID: runID, From: from, To: to, Schema: f.release}
}

// ---- RLS harness (mirrors internal/transaction/idempotency's fixtures) ----

// appConn opens a connection that assumes the least-privilege application
// role, which is the only way a test observes row level security: the
// pgtest URL authenticates as a superuser, and a superuser bypasses every
// policy.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
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

// ---- shared assertions ----

func kinds(r evidence.Report) []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, string(f.Kind))
	}
	return out
}

func auditKinds(r auditpack.Report) []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, string(f.Kind))
	}
	return out
}

// mutate returns a copy of the package bytes with one path replaced by fn's
// result. Verification must be given exactly what a recipient would hold, so
// tampering is done on the bytes and never on the in-memory value.
func mutate(t *testing.T, files map[string][]byte, path string, fn func([]byte) []byte) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(files))
	found := false
	for p, raw := range files {
		if p == path {
			found = true
			out[p] = fn(append([]byte(nil), raw...))
			continue
		}
		out[p] = append([]byte(nil), raw...)
	}
	if !found {
		t.Fatalf("package has no part at %s", path)
	}
	return out
}

// rewriteJSON decodes a part into a generic map, lets fn edit it, and
// re-encodes.
func rewriteJSON(t *testing.T, raw []byte, fn func(map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode part for rewriting: %v", err)
	}
	fn(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-encode rewritten part: %v", err)
	}
	return append(out, '\n')
}
