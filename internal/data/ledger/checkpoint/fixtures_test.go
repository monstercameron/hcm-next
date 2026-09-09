package checkpoint_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	schemaRef  = "hcmnext.intents.v1.BusinessIntent@1"
	streamOne  = "worker:1"
	streamTwo  = "worker:2"
	devKeyID   = "hcmnext:checkpoint:dev"
	devKeyFile = "dev-signing-key.json"
)

var (
	occurredAt  = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Recording instants. They are pinned so a manifest - and therefore its
	// digest and its signature - is reproducible.
	recordedFirst  = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	recordedSecond = time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	checkpointOne  = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	checkpointTwo  = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	keyValidFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

// devKey is the checked-in test keypair. It is loaded rather than generated
// so that a golden digest over a signed manifest is reproducible across
// runs.
type devKey struct {
	KeyID      string `json:"key_id"`
	Algorithm  string `json:"algorithm"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

func loadDevKey(t *testing.T) (devKey, ed25519.PrivateKey) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", devKeyFile))
	if err != nil {
		t.Fatalf("read signing key fixture: %v", err)
	}
	var key devKey
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatalf("parse signing key fixture: %v", err)
	}
	priv, err := hex.DecodeString(key.PrivateKey)
	if err != nil {
		t.Fatalf("decode signing key fixture: %v", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("signing key fixture holds %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	return key, ed25519.PrivateKey(priv)
}

// fixture is a migrated schema holding one tenant with two chained ledger
// streams, plus a signer and key directory built from the test keypair.
type fixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	signer   *checkpoint.Ed25519Signer
	key      devKey
	release  checkpoint.SchemaRelease
	chainer  *hashchain.Appender
	digester *hashchain.Digester
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)

	key, priv := loadDevKey(t)
	signer, err := checkpoint.NewEd25519Signer(key.KeyID, priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build chain registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)

	version, err := migrations.TargetVersion()
	if err != nil {
		t.Fatalf("migration target version: %v", err)
	}
	artifact, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("migration artifact digest: %v", err)
	}

	f := fixture{
		db: db, tenant: uuid.New(), signer: signer, key: key,
		release:  checkpoint.SchemaRelease{Version: version, Digest: artifact},
		chainer:  hashchain.NewAppender(digester),
		digester: digester,
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

// keyDirectory returns a directory that trusts the fixture key over a window
// the caller chooses, so a test can make the same key usable, expired or
// revoked without changing anything else.
func (f fixture) keyDirectory(notAfter, revokedAt time.Time) checkpoint.KeyDirectory {
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID:     f.key.KeyID,
		PublicKey: f.key.PublicKey,
		NotBefore: keyValidFrom,
		NotAfter:  notAfter,
		RevokedAt: revokedAt,
	})
}

// liveKeyDirectory trusts the fixture key indefinitely.
func (f fixture) liveKeyDirectory() checkpoint.KeyDirectory {
	return f.keyDirectory(time.Time{}, time.Time{})
}

func (f fixture) service(t *testing.T, dir checkpoint.KeyDirectory, opts ...checkpoint.ServiceOption) *checkpoint.Service {
	t.Helper()
	svc, err := checkpoint.NewService(f.signer, dir, opts...)
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	return svc
}

// appendLinked appends one event to a stream and links it into the hash
// chain in the same transaction, which is the calling pattern a checkpoint
// depends on: an unlinked head cannot be attested to.
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
		// event, and the checkpoint would silently cover less than it looks.
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
// stream head exists that no checkpoint may attest to.
func (f fixture) appendUnlinked(t *testing.T, tenant uuid.UUID, stream string, expectedHead int64, recordedAt time.Time) datalogger.AppendReceipt {
	t.Helper()
	appender := datalogger.New(datalogger.WithClock(func() time.Time { return recordedAt.UTC() }))
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = appender.Append(context.Background(), tx, datalogger.AppendRequest{
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
	return receipt
}

// seedTwoStreams gives the tenant one linked event on each stream.
func (f fixture) seedTwoStreams(t *testing.T) {
	t.Helper()
	f.appendLinked(t, f.tenant, streamOne, 0, recordedFirst)
	f.appendLinked(t, f.tenant, streamTwo, 0, recordedFirst)
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

// createCheckpoint runs one Create inside its own committed transaction.
func (f fixture) createCheckpoint(t *testing.T, svc *checkpoint.Service, at time.Time) (checkpoint.Manifest, error) {
	t.Helper()
	var manifest checkpoint.Manifest
	err := f.inTxErr(func(tx dbport.Tx) error {
		var createErr error
		manifest, createErr = svc.Create(context.Background(), tx, checkpoint.CreateRequest{
			Tenant: f.tenant, Schema: f.release, At: at,
		})
		return createErr
	})
	return manifest, err
}

func (f fixture) mustCreateCheckpoint(t *testing.T, svc *checkpoint.Service, at time.Time) checkpoint.Manifest {
	t.Helper()
	manifest, err := f.createCheckpoint(t, svc, at)
	if err != nil {
		t.Fatalf("create checkpoint at %s: %v", at, err)
	}
	return manifest
}
