package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/hcm-next/internal/connectivity/observe"
	"github.com/monstercameron/hcm-next/internal/connectivity/observe/adapters/postgres"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// The tests in this package deliberately do not call t.Parallel.
//
// migrations/00008_tenant_isolation.sql creates the cluster-wide role
// hcmnext_app inside a DO block guarded against duplicate_object. Two schemas
// migrating at the same moment race on pg_authid's unique index instead, which
// raises unique_violation and is not caught by that guard. Until that
// migration's owner makes role creation concurrency-safe, running these tests
// in parallel fails during migration rather than during anything this package
// is responsible for.

const (
	testTenant  = "5e3f1c2b-0000-4000-8000-000000000001"
	otherTenant = "5e3f1c2b-0000-4000-8000-000000000002"
)

var (
	publishedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	draftedAt   = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
)

// newStore returns a store over an isolated schema with a tenant row, because
// every authoritative row in this tree is tenant scoped by construction.
func newStore(t *testing.T, tenants ...string) (*postgres.Store, *pgtest.DB) {
	t.Helper()
	db := pgtest.New(t)
	if len(tenants) == 0 {
		tenants = []string{testTenant}
	}
	for i, tenant := range tenants {
		db.Exec(t, `
            INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
            VALUES ($1, $2, 'cell-a', 'HarborCare', 'ACTIVE', now())`,
			tenant, "harborcare-"+string(rune('a'+i)))
	}
	return postgres.New(db.Conn), db
}

func activeConnection(t *testing.T, tenant string) *connectivity.ConnectorConnection {
	t.Helper()
	registry := connectivity.NewRegistry()
	pub, err := registry.Publish(fakeincumbent.DefaultDefinition(), connectivity.PublicationMeta{
		PublishedBy: "user:platform@hcmnext", PublishedAt: publishedAt,
	})
	if err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	credential, err := connectivity.ParseCredentialRef("secretref://harborcare/workday/client")
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	conn, err := connectivity.NewConnection(pub, connectivity.ConnectionSpec{
		ConnectionID:     fakeincumbent.DefaultDescriptor().ConnectionID,
		TenantID:         tenant,
		OrgID:            "org-harborcare-us",
		SystemID:         "sys-workday-prod",
		Environment:      connectivity.EnvironmentProduction,
		Residency:        "us-east",
		ConnectorID:      pub.Definition.ConnectorID,
		ConnectorVersion: pub.Definition.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    credential,
		Scopes:           []string{"worker.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"api.workday.example"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		Bounds:       pub.Definition.Bounds,
		CreatedAt:    draftedAt,
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}
	for i, step := range []connectivity.LifecycleState{
		connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive,
	} {
		err := conn.Transition(step, connectivity.TransitionEvidence{
			Reason:      "enable",
			ActorRef:    "user:ops@harborcare",
			EvidenceRef: "evd:enable",
			OccurredAt:  draftedAt.Add(time.Duration(i+1) * time.Minute),
		})
		if err != nil {
			t.Fatalf("transition to %s: %v", step, err)
		}
	}
	return conn
}

func fixedClock() func() time.Time {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var tick int
	return func() time.Time {
		t := base.Add(time.Duration(tick) * time.Minute)
		tick++
		return t
	}
}

func newRunner(t *testing.T, tenant string, store *postgres.Store) (*observe.Runner, *fakeincumbent.Incumbent) {
	t.Helper()
	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	return &observe.Runner{
		Connector:       incumbent,
		Connection:      activeConnection(t, tenant),
		Observations:    store,
		Checkpoints:     store,
		FreshnessBudget: 100 * 365 * 24 * time.Hour,
		Now:             fixedClock(),
	}, incumbent
}

func recordFirstPage(t *testing.T, tenant string) observe.Observation {
	t.Helper()
	ctx := context.Background()
	inc, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	snapshot, err := inc.Snapshot(ctx, connectivity.ObjectWorker)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	start := connectivity.StartCursor(snapshot)
	page, err := inc.Read(ctx, connectivity.ReadRequest{
		Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull,
		Cursor: start, Limit: inc.Bounds().MaxPageSize,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	obs, err := observe.Record(page, observe.RecordOptions{
		TenantID:        tenant,
		Descriptor:      inc.Descriptor(),
		PageSequence:    1,
		StartCursor:     start,
		FreshnessBudget: 100 * 365 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return obs
}

// TestTodo_INTG_009_Integration is the INTG-009 integration test: observation
// evidence survives a real PostgreSQL round trip with its attribution intact,
// is idempotent to re-append, and cannot be rewritten - the last of which is
// enforced by the database's own append-only trigger, not by this adapter.
func TestTodo_INTG_009_Integration(t *testing.T) {
	ctx := context.Background()
	store, db := newStore(t)

	obs := recordFirstPage(t, testTenant)
	appended, err := store.Append(ctx, obs)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if appended.Existing {
		t.Fatal("the first append reported the observation as pre-existing")
	}

	got, err := store.Get(ctx, testTenant, obs.ObservationID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("round-tripped observation does not verify: %v", err)
	}
	for name, pair := range map[string][2]string{
		"connection":     {got.ConnectionID, obs.ConnectionID},
		"connector":      {got.ConnectorID, obs.ConnectorID},
		"version":        {got.ConnectorVersion, obs.ConnectorVersion},
		"source":         {got.SourceRef, obs.SourceRef},
		"authority":      {got.AuthorityRef, obs.AuthorityRef},
		"schema version": {got.SchemaVersion, obs.SchemaVersion},
		"snapshot":       {got.SnapshotID, obs.SnapshotID},
		"start cursor":   {got.StartCursor, obs.StartCursor},
		"digest":         {got.ContentDigest, obs.ContentDigest},
		"classification": {string(got.Classification), string(obs.Classification)},
		"freshness":      {string(got.Freshness), string(obs.Freshness)},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("round-tripped %s is %q, want %q", name, pair[0], pair[1])
		}
	}
	if !got.RetrievedAt.Equal(obs.RetrievedAt) || !got.Watermark.Equal(obs.Watermark) {
		t.Fatalf("times did not survive: retrieved %v/%v watermark %v/%v",
			got.RetrievedAt, obs.RetrievedAt, got.Watermark, obs.Watermark)
	}

	// Re-appending identical evidence is idempotent.
	again, err := store.Append(ctx, obs)
	if err != nil {
		t.Fatalf("re-append: %v", err)
	}
	if !again.Existing {
		t.Fatal("re-appending identical evidence was treated as new")
	}

	// Different content under the same identity is refused.
	forged := obs
	forged.RecordCount++
	if _, err := store.Append(ctx, forged); !errors.Is(err, observe.ErrImmutable) {
		t.Fatalf("forged evidence returned %v, want ErrImmutable", err)
	}

	// The database refuses a rewrite even when the adapter is bypassed.
	if err := db.ExecErr(`UPDATE external_observation SET record_count = 99`); err == nil {
		t.Fatal("a direct UPDATE of stored evidence succeeded")
	}
	if err := db.ExecErr(`DELETE FROM external_observation`); err == nil {
		t.Fatal("a direct DELETE of stored evidence succeeded")
	}

	// A raw provider payload is a reference, not the evidence.
	var rawRef *string
	if err := db.QueryRow(ctx,
		`SELECT raw_artifact_ref FROM external_observation WHERE observation_id = $1`,
		obs.ObservationID).Scan(&rawRef); err != nil {
		t.Fatalf("read raw artifact ref: %v", err)
	}
	if rawRef != nil {
		t.Fatalf("a raw payload reference was stored without being asked for: %v", *rawRef)
	}

	// An observation cannot be filed as a domain fact.
	if err := db.ExecErr(`
        INSERT INTO external_observation (
            tenant_id, observation_id, connection_id, connector_id, connector_version,
            source_ref, authority_ref, object_kind, schema_version, snapshot_id,
            page_sequence, start_cursor, next_cursor, record_count, complete,
            classification, freshness, retrieved_at, watermark_at,
            content_digest, digest_algorithm, canonical_length, payload)
        VALUES ($1, $2, 'c', 'c', '1', 's', 'a', 'WORKER', 'v', 'snap', 1, 'cur', 'cur', 0, false,
                'DOMAIN_FACT', 'FRESH', now(), now(),
                repeat('a', 64), 'sha256', 1, '\x00'::bytea)`,
		testTenant, uuid.New()); err == nil {
		t.Fatal("an observation was stored as a DOMAIN_FACT")
	}
}

// TestTodo_INTG_008_Integration is the INTG-008 integration test: a bounded
// traversal against PostgreSQL-backed evidence and checkpoints resumes exactly
// where it stopped, and a stale worker's commit is refused by the database in
// one statement rather than by a read-then-write in this process.
func TestTodo_INTG_008_Integration(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	runner, incumbent := newRunner(t, testTenant, store)
	object := connectivity.ObjectPosition
	total := incumbent.RecordCount(object)

	req := observe.RunRequest{
		RunID:    "run-positions",
		TenantID: testTenant,
		Object:   object,
		Mode:     connectivity.ReadFull,
		MaxPages: 1,
	}

	var pages, records int
	var lastFence uint64
	for range 16 {
		result, err := runner.Run(ctx, req)
		if err != nil {
			t.Fatalf("bounded run: %v", err)
		}
		if result.Status == observe.RunAlreadyComplete {
			break
		}
		if result.Checkpoint.Fence <= lastFence {
			t.Fatalf("fence did not advance: %d then %d", lastFence, result.Checkpoint.Fence)
		}
		lastFence = result.Checkpoint.Fence
		pages += result.Pages
		records += result.Records
		if result.Duplicate != 0 {
			t.Fatalf("a resumed run re-observed %d pages", result.Duplicate)
		}
		if result.Status == observe.RunCompleted {
			break
		}
	}
	if records != total {
		t.Fatalf("resumed traversal observed %d records, source holds %d", records, total)
	}

	stored, err := store.List(ctx, observe.Query{TenantID: testTenant, Object: object})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stored) != pages {
		t.Fatalf("stored %d observations across %d pages", len(stored), pages)
	}
	replayed, err := observe.Replay(ctx, store, observe.Query{TenantID: testTenant, Object: object})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	counted := 0
	for i, obs := range replayed {
		if obs.PageSequence != uint64(i+1) {
			t.Fatalf("replayed page %d has sequence %d", i, obs.PageSequence)
		}
		counted += obs.RecordCount
	}
	if counted != total {
		t.Fatalf("replayed evidence covers %d records, source holds %d", counted, total)
	}

	// A stale worker cannot rewind the traversal.
	key := observe.CheckpointKey{
		TenantID: testTenant, ConnectionID: runner.Connection.ID(), Object: object,
	}
	current, found, err := store.Load(ctx, key)
	if err != nil || !found {
		t.Fatalf("load checkpoint: %v (found=%t)", err, found)
	}
	stale := current
	stale.Fence--
	stale.Complete = false
	stale.RunID = "stale-worker"
	if err := store.Commit(ctx, stale); !errors.Is(err, observe.ErrFenced) {
		t.Fatalf("a stale commit returned %v, want ErrFenced", err)
	}
	after, _, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	if after.Fence != current.Fence || after.RunID != current.RunID || !after.Complete {
		t.Fatalf("the stale commit changed the checkpoint: %+v", after)
	}

	// A re-run over a completed traversal reads nothing further.
	result, err := runner.Run(ctx, observe.RunRequest{
		RunID: "run-positions", TenantID: testTenant, Object: object, Mode: connectivity.ReadFull,
	})
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if result.Status != observe.RunAlreadyComplete || result.Pages != 0 {
		t.Fatalf("re-run status %s read %d pages", result.Status, result.Pages)
	}
}

// TestTodo_INTG_009_Fault_Integration covers the storage-layer failures the
// port promises to classify: a non-UUID tenant, an unknown observation, and a
// cross-tenant read that must find nothing rather than someone else's evidence.
func TestTodo_INTG_009_Fault_Integration(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t, testTenant, otherTenant)

	obs := recordFirstPage(t, testTenant)
	if _, err := store.Append(ctx, obs); err != nil {
		t.Fatalf("append: %v", err)
	}

	if _, err := store.Get(ctx, otherTenant, obs.ObservationID); !errors.Is(err, observe.ErrNotFound) {
		t.Fatalf("a cross-tenant read returned %v, want ErrNotFound", err)
	}
	listed, err := store.List(ctx, observe.Query{TenantID: otherTenant})
	if err != nil {
		t.Fatalf("cross-tenant list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("a cross-tenant list returned %d observations", len(listed))
	}

	if _, err := store.Get(ctx, testTenant, uuid.New()); !errors.Is(err, observe.ErrNotFound) {
		t.Fatalf("an unknown observation returned %v, want ErrNotFound", err)
	}

	bad := obs
	bad.TenantID = "not-a-uuid"
	if _, err := store.Append(ctx, bad); !errors.Is(err, postgres.ErrTenant) {
		t.Fatalf("a non-uuid tenant returned %v, want ErrTenant", err)
	}

	// An observation whose payload no longer reproduces its digest is refused
	// before it reaches the database.
	corrupt := obs
	corrupt.Payload = append([]byte(nil), obs.Payload...)
	corrupt.Payload[0] ^= 0xff
	if _, err := store.Append(ctx, corrupt); !errors.Is(err, observe.ErrDigestMismatch) {
		t.Fatalf("corrupt evidence returned %v, want ErrDigestMismatch", err)
	}
}
