package evidence_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	schemaRef = "hcmnext.intents.v1.BusinessIntent@1"
	streamOne = "worker:1"
	streamTwo = "worker:2"
	testKeyID = "hcmnext:evidence:test"
)

var (
	occurredAt  = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Recording instants. They are pinned so a package - and therefore its
	// part digests and its manifest digest - is reproducible.
	recordedFirst  = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	recordedSecond = time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	recordedThird  = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	windowFrom    = recordedFirst
	windowTo      = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	checkpointOne = windowTo
	checkpointTwo = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	keyValidFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

// testSigningSeed is a fixed Ed25519 seed. A seed rather than a checked-in
// key file, because Ed25519 signing is deterministic (RFC 8032): the same
// seed and the same manifest always produce the same signature, which is
// what a golden digest over a signed package needs, and no private material
// then has to live in the repository as data.
var testSigningSeed = [ed25519.SeedSize]byte{
	0x4c, 0x45, 0x44, 0x47, 0x45, 0x52, 0x2d, 0x30,
	0x31, 0x32, 0x2f, 0x65, 0x76, 0x69, 0x64, 0x65,
	0x6e, 0x63, 0x65, 0x2f, 0x74, 0x65, 0x73, 0x74,
	0x2d, 0x73, 0x69, 0x67, 0x6e, 0x69, 0x6e, 0x67,
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

// fixture is a migrated schema holding one tenant with chained ledger
// streams, plus a signer and key directory built from the fixed seed.
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

func (f fixture) seedTenant(t *testing.T, tenant uuid.UUID) {
	t.Helper()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "tenant-"+tenant.String()[:8])
	f.db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, schemaRef)
	for _, stream := range []string{streamOne, streamTwo} {
		f.inTx(t, func(tx dbport.Tx) error {
			return datalogger.EnsureStream(context.Background(), tx, tenant, stream, "WORKER", stream)
		})
	}
}

// keyDirectory trusts the fixture key over a window the caller chooses, so a
// test can make the same key usable, expired or revoked without changing
// anything else about the package.
func (f fixture) keyDirectory(notAfter, revokedAt time.Time) evidence.KeyDirectory {
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID:     testKeyID,
		PublicKey: f.signer.PublicKey(),
		NotBefore: keyValidFrom,
		NotAfter:  notAfter,
		RevokedAt: revokedAt,
	})
}

func (f fixture) liveKeyDirectory() evidence.KeyDirectory {
	return f.keyDirectory(time.Time{}, time.Time{})
}

// appendLinked appends one event and links it into the hash chain in the
// same transaction, which is the calling pattern an export depends on: an
// unchained head cannot be exported as evidence.
func (f fixture) appendLinked(t *testing.T, tenant uuid.UUID, stream string, expectedHead int64, recordedAt time.Time) datalogger.AppendReceipt {
	t.Helper()
	appender := datalogger.New(datalogger.WithClock(func() time.Time { return recordedAt.UTC() }))
	req := datalogger.AppendRequest{
		Tenant: tenant, StreamKey: stream, ExpectedHead: expectedHead,
		AssertionClass: datalogger.TransactionFact,
		SourceRef:      "hcmnext:test", SchemaRef: schemaRef,
		Payload:     []byte(fmt.Sprintf("%s-%d", stream, expectedHead+1)),
		OccurredAt:  occurredAt,
		EffectiveAt: effectiveAt,
		// A fresh correlation and idempotency key per append: reusing one
		// would make the second append an idempotent replay rather than a new
		// event, and the package would silently cover less than it looks.
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	}

	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = appender.Append(context.Background(), tx, req)
		if appendErr != nil {
			return appendErr
		}
		_, appendErr = f.chainer.Append(context.Background(), tx, receipt)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append and link %s@%d: %v", stream, expectedHead+1, err)
	}
	return receipt
}

// appendUnlinked appends an event without recording its chain link, so a
// stream head exists that no package may claim to have proved.
func (f fixture) appendUnlinked(t *testing.T, tenant uuid.UUID, stream string, expectedHead int64, recordedAt time.Time) {
	t.Helper()
	appender := datalogger.New(datalogger.WithClock(func() time.Time { return recordedAt.UTC() }))
	err := f.inTxErr(func(tx dbport.Tx) error {
		_, appendErr := appender.Append(context.Background(), tx, datalogger.AppendRequest{
			Tenant: tenant, StreamKey: stream, ExpectedHead: expectedHead,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      "hcmnext:test", SchemaRef: schemaRef,
			Payload:    []byte("unlinked"),
			OccurredAt: occurredAt, EffectiveAt: effectiveAt,
			CorrelationID:  uuid.New(),
			IdempotencyKey: uuid.NewString(),
		})
		return appendErr
	})
	if err != nil {
		t.Fatalf("append unlinked %s@%d: %v", stream, expectedHead+1, err)
	}
}

// withTenant returns the same fixture pointed at another seeded tenant, so
// one migrated schema can carry several independent scenarios.
func (f fixture) withTenant(t *testing.T) fixture {
	t.Helper()
	other := f
	other.tenant = uuid.New()
	other.seedTenant(t, other.tenant)
	return other
}

// seedCoveredWindow gives the tenant two events on stream one and one on
// stream two, all recorded inside [windowFrom, windowTo), and takes a signed
// checkpoint at the window's end. That is the smallest fixture that is
// actually evidence: more than one stream, more than one sequence, and a
// signature reaching all of it.
func (f fixture) seedCoveredWindow(t *testing.T) checkpoint.Manifest {
	t.Helper()
	f.appendLinked(t, f.tenant, streamOne, 0, recordedFirst)
	f.appendLinked(t, f.tenant, streamOne, 1, recordedSecond)
	f.appendLinked(t, f.tenant, streamTwo, 0, recordedFirst)
	return f.mustCheckpoint(t, checkpointOne)
}

func (f fixture) service(t *testing.T, dir evidence.KeyDirectory) *checkpoint.Service {
	t.Helper()
	svc, err := checkpoint.NewService(f.signer, dir)
	if err != nil {
		t.Fatalf("build checkpoint service: %v", err)
	}
	return svc
}

func (f fixture) mustCheckpoint(t *testing.T, at time.Time) checkpoint.Manifest {
	t.Helper()
	svc := f.service(t, f.liveKeyDirectory())
	var manifest checkpoint.Manifest
	err := f.inTxErr(func(tx dbport.Tx) error {
		var createErr error
		manifest, createErr = svc.Create(context.Background(), tx, checkpoint.CreateRequest{
			Tenant: f.tenant, Schema: f.release, At: at,
		})
		return createErr
	})
	if err != nil {
		t.Fatalf("create checkpoint at %s: %v", at, err)
	}
	return manifest
}

// request is the export request over the seeded window.
func (f fixture) request() evidence.Request {
	return evidence.Request{Tenant: f.tenant, From: windowFrom, To: windowTo, Schema: f.release}
}

func (f fixture) mustExport(t *testing.T, req evidence.Request) evidence.Package {
	t.Helper()
	pkg, err := evidence.NewExporter().Export(context.Background(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return pkg
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

// mustVerify verifies a package's bytes and fails with every finding, so a
// regression reports what actually broke rather than "not ok".
func mustVerify(t *testing.T, files map[string][]byte, dir evidence.KeyDirectory, opts ...evidence.VerifyOption) evidence.Report {
	t.Helper()
	report := evidence.Verify(files, dir, opts...)
	if !report.OK() {
		for _, finding := range report.Findings {
			t.Errorf("unexpected finding: %s", finding)
		}
		t.FailNow()
	}
	return report
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

// flipOneByte alters exactly one byte of a part, inside the JSON body rather
// than at its framing, so what fails is the digest and not the parser.
func flipOneByte(raw []byte) []byte {
	for i := len(raw) - 2; i > 0; i-- {
		if raw[i] >= '0' && raw[i] <= '8' {
			raw[i]++
			return raw
		}
	}
	raw[len(raw)/2]++
	return raw
}

// rewriteJSON decodes a part into a generic map, lets fn edit it, and
// re-encodes. It is how a test forges a part that is still well-formed JSON,
// which is the interesting adversary: a corrupt file is caught by the
// parser, a plausible one has to be caught by a digest.
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

// kinds lists every finding kind a report carries, for a test that wants to
// say what was reported rather than only that something was.
func kinds(r evidence.Report) []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, string(f.Kind))
	}
	return out
}
