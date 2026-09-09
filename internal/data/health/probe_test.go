package health_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/health"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

// testRegistry is a minimal disposition registry naming exactly the three
// tables this package evaluates on all dimensions -- the same shape
// DefaultDispositionRegistry publishes, but declared locally so a test does
// not depend on that fixture's own field values changing underneath it.
func testRegistry() health.DispositionRegistry {
	return health.DispositionRegistry{
		Version: 1,
		Tables: []health.StoreDisposition{
			{Table: "ledger_event", DataRole: "LEDGER", TenantScopingColumn: "tenant_id", RetentionClass: "PERMANENT"},
			{
				Table: "projection_checkpoint", DataRole: "PROJECTION", TenantScopingColumn: "tenant_id",
				RetentionClass: "REBUILDABLE", RebuildSource: "ledger_event",
			},
			{
				Table: "outbox", DataRole: "OUTBOX", TenantScopingColumn: "tenant_id",
				RetentionClass: "REBUILDABLE", RebuildSource: "ledger_event",
			},
		},
	}
}

func storesByTable(snap health.Snapshot) map[string]health.StoreHealth {
	out := make(map[string]health.StoreHealth, len(snap.Stores))
	for _, s := range snap.Stores {
		out[s.Table] = s
	}
	return out
}

// ackAll drains every claimable outbox message for tenant to DELIVERED.
func ackAll(t *testing.T, conn outbox.Beginner, tenant uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	consumer := outbox.NewConsumer(conn)
	batch, err := consumer.Poll(ctx, tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	for _, msg := range batch {
		if err := consumer.Ack(ctx, tenant, msg.OutboxID); err != nil {
			t.Fatalf("ack %s: %v", msg.OutboxID, err)
		}
	}
}

// TestTodo_DATA_020 proves the DATA-020 GREEN clause end to end: given a
// tenant whose ledger, projection checkpoint and outbox are all current,
// health exposes the authoritative source head, the applied head, a zero
// lag, the rebuild source, the checkpoint's own status and the accepted
// staleness bounds -- and the overall verdict is HEALTHY.
func TestTodo_DATA_020(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

	f.commit(t, "d20-1", 0, at)
	f.commit(t, "d20-2", 1, at.Add(time.Minute))
	ackAll(t, f.db.Conn, f.tenant)

	now := at.Add(2 * time.Minute)
	policy := health.DefaultPolicy()
	probe := health.NewProbe(f.db.Conn, testRegistry(), policy, health.WithClock(health.NewFakeClock(now)))

	snap := probe.Run(ctx, f.tenant)
	if snap.State != health.StateHealthy {
		t.Fatalf("overall state = %s, want HEALTHY; reasons: %v; stores: %+v", snap.State, snap.Reasons, snap.Stores)
	}
	if snap.Digest == "" {
		t.Fatal("snapshot carries no digest")
	}
	if !snap.GeneratedAt.Equal(now) {
		t.Fatalf("generated at = %s, want %s", snap.GeneratedAt, now)
	}

	stores := storesByTable(snap)
	ledgerStore, projStore, outboxStore := stores["ledger_event"], stores["projection_checkpoint"], stores["outbox"]

	if ledgerStore.Saturation == nil || ledgerStore.Saturation.RowCount != 2 {
		t.Fatalf("ledger row count = %+v, want 2", ledgerStore.Saturation)
	}
	if !ledgerStore.Freshness.HasEvidence {
		t.Fatal("ledger freshness reports no evidence after two commits")
	}

	// "authoritative source head, applied head, lag ... rebuild status and
	// accepted staleness" (DATA-020 GREEN).
	if projStore.Freshness.LagSequences != 0 {
		t.Fatalf("projection lag = %d, want 0 (applied head matches source head)", projStore.Freshness.LagSequences)
	}
	if projStore.Freshness.LagThreshold != policy.MaxProjectionLagSequences {
		t.Fatalf("projection lag threshold = %d, want the accepted staleness %d", projStore.Freshness.LagThreshold, policy.MaxProjectionLagSequences)
	}
	if projStore.Recovery.Status != "CURRENT" {
		t.Fatalf("projection recovery status = %q, want CURRENT", projStore.Recovery.Status)
	}
	if !projStore.Recovery.ChainConsistent {
		t.Fatal("projection checkpoint chain reported inconsistent for a freshly-applied event")
	}
	if projStore.RebuildSource != "ledger_event" {
		t.Fatalf("projection rebuild source = %q, want ledger_event", projStore.RebuildSource)
	}

	if outboxStore.Recovery.AbandonedCount != 0 {
		t.Fatalf("outbox abandoned count = %d, want 0", outboxStore.Recovery.AbandonedCount)
	}
	if outboxStore.Freshness.HasEvidence {
		t.Fatal("outbox freshness reports unacknowledged evidence after draining")
	}
	if outboxStore.Freshness.AgeThresholdSeconds != policy.MaxOutboxUnackedAge.Seconds() {
		t.Fatalf("outbox age threshold = %v, want the accepted staleness %v", outboxStore.Freshness.AgeThresholdSeconds, policy.MaxOutboxUnackedAge.Seconds())
	}
}

// TestTodo_DATA_020_Golden pins the exact, reproducible values a fixed
// clock and a fixed set of evidence produce: the same evidence at the same
// instant always yields the same digest, and moving the clock or changing
// the evidence changes it.
func TestTodo_DATA_020_Golden(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f.commit(t, "golden-1", 0, at)

	fixedClock := health.NewFakeClock(at.Add(time.Hour))
	probe := health.NewProbe(f.db.Conn, testRegistry(), health.DefaultPolicy(), health.WithClock(fixedClock))

	snap1 := probe.Run(ctx, f.tenant)
	snap2 := probe.Run(ctx, f.tenant)
	if snap1.Digest == "" {
		t.Fatal("empty digest")
	}
	if snap1.Digest != snap2.Digest {
		t.Fatalf("two evaluations of identical evidence at the identical instant produced different digests:\n%s\n%s", snap1.Digest, snap2.Digest)
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(snap1.Digest) {
		t.Fatalf("digest %q does not look like sha256:<hex>", snap1.Digest)
	}

	// Moving the clock changes GeneratedAt, which must move the digest.
	fixedClock.Advance(time.Second)
	snap3 := probe.Run(ctx, f.tenant)
	if snap3.Digest == snap1.Digest {
		t.Fatal("advancing the clock did not change the digest")
	}

	// A stale projection (created by appending without applying) must also
	// move the digest at a fixed instant, since it changes the evidence.
	fixedClock.Set(at.Add(time.Hour))
	f.appendOnly(t, "golden-2", 1, at.Add(2*time.Minute))
	snap4 := probe.Run(ctx, f.tenant)
	if snap4.Digest == snap1.Digest {
		t.Fatal("a changed projection lag did not change the digest")
	}
	if snap4.State == health.StateHealthy {
		t.Fatalf("expected the lagging projection to move state off HEALTHY, got reasons: %v", snap4.Reasons)
	}
}

// TestTodo_DATA_020_Recovery proves recovery evidence reflects real
// reconciliation: a projection left behind by an append that skipped
// projection.Apply reports DEGRADED, and after
// internal/data/projection.Reconciler catches it up, the same Probe reports
// HEALTHY again.
func TestTodo_DATA_020_Recovery(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	f.commit(t, "rec-1", 0, at)
	f.appendOnly(t, "rec-2", 1, at.Add(time.Minute))

	clock := health.NewFakeClock(at.Add(2 * time.Minute))
	probe := health.NewProbe(f.db.Conn, testRegistry(), health.DefaultPolicy(), health.WithClock(clock))

	before := probe.Run(ctx, f.tenant)
	if before.State == health.StateHealthy {
		t.Fatalf("expected a lagging projection to degrade the plane before reconciliation, got HEALTHY")
	}
	beforeProj := storesByTable(before)["projection_checkpoint"]
	if beforeProj.Freshness.LagSequences != 1 {
		t.Fatalf("lag before reconciliation = %d, want 1", beforeProj.Freshness.LagSequences)
	}

	reconciler := projection.NewReconciler(f.db.Conn, ledger.NewReader())
	applied, err := reconciler.ReconcileOne(ctx, projection.StreamProjection{Tenant: f.tenant, ProjectionName: hProjection, StreamKey: hStreamKey})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if applied != 1 {
		t.Fatalf("reconciler applied %d events, want 1", applied)
	}

	after := probe.Run(ctx, f.tenant)
	afterProj := storesByTable(after)["projection_checkpoint"]
	if afterProj.Freshness.LagSequences != 0 {
		t.Fatalf("lag after reconciliation = %d, want 0", afterProj.Freshness.LagSequences)
	}
	if afterProj.Recovery.Status != "CURRENT" {
		t.Fatalf("recovery status after reconciliation = %q, want CURRENT", afterProj.Recovery.Status)
	}
	if after.State != health.StateHealthy {
		t.Fatalf("overall state after reconciliation = %s, want HEALTHY; reasons: %v", after.State, after.Reasons)
	}
}

// TestTodo_STORE_003 proves the two RED clauses by name: "a stale projection"
// and "an unacked outbox age past threshold" each degrade the plane -- never
// silently HEALTHY, and never indistinguishable from truth unavailability
// (the reason names exactly which store and by how much).
func TestTodo_STORE_003(t *testing.T) {
	t.Parallel()

	t.Run("a stale projection degrades the plane", func(t *testing.T) {
		t.Parallel()
		f := newHealthFixture(t)
		ctx := context.Background()
		at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

		f.commit(t, "store003-proj-1", 0, at)
		f.appendOnly(t, "store003-proj-2", 1, at.Add(time.Minute))

		probe := health.NewProbe(f.db.Conn, testRegistry(), health.DefaultPolicy(), health.WithClock(health.NewFakeClock(at.Add(2*time.Minute))))
		snap := probe.Run(ctx, f.tenant)

		proj := storesByTable(snap)["projection_checkpoint"]
		if proj.State != health.StateDegraded {
			t.Fatalf("projection_checkpoint state = %s, want DEGRADED (lag 1 versus a zero-lag policy)", proj.State)
		}
		if proj.Freshness.LagSequences != 1 {
			t.Fatalf("lag = %d, want 1", proj.Freshness.LagSequences)
		}
		if snap.State != health.StateDegraded {
			t.Fatalf("overall state = %s, want DEGRADED: a lagging projection must never read as HEALTHY", snap.State)
		}
	})

	t.Run("an unacked outbox age past threshold degrades the plane", func(t *testing.T) {
		t.Parallel()
		f := newHealthFixture(t)
		ctx := context.Background()
		at := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

		f.commit(t, "store003-outbox-1", 0, at) // left PENDING deliberately
		// The outbox row's own created_at column is stamped by the database's
		// now() default at insert time (migrations/00006), not by the
		// business timestamp `at` above -- so the Probe's clock must be set
		// relative to real wall-clock time, not to `at`, for an age
		// comparison against that column to mean anything.
		insertedAt := time.Now().UTC()

		policy := health.DefaultPolicy()
		policy.MaxOutboxUnackedAge = time.Minute
		probe := health.NewProbe(f.db.Conn, testRegistry(), policy, health.WithClock(health.NewFakeClock(insertedAt.Add(5*time.Minute))))
		snap := probe.Run(ctx, f.tenant)

		ob := storesByTable(snap)["outbox"]
		if ob.State != health.StateDegraded {
			t.Fatalf("outbox state = %s, want DEGRADED (5m old, 1m limit)", ob.State)
		}
		if ob.Freshness.AgeSeconds < (5 * time.Minute).Seconds() {
			t.Fatalf("outbox age = %vs, want at least 300s", ob.Freshness.AgeSeconds)
		}
		if snap.State != health.StateDegraded {
			t.Fatalf("overall state = %s, want DEGRADED: an old unacked outbox row must never read as HEALTHY", snap.State)
		}
	})
}

// TestTodo_STORE_003_Golden pins Snapshot's exact shape for a fixed,
// fully-healthy fixture and a fixed clock: the schema/registry versions,
// the sorted store order, and the digest's own format and stability.
func TestTodo_STORE_003_Golden(t *testing.T) {
	t.Parallel()
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	f := newHealthFixtureWithTenant(t, tenant)
	ctx := context.Background()
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	f.commit(t, "golden-store003", 0, at)
	ackAll(t, f.db.Conn, f.tenant)

	fixed := at.Add(time.Hour)
	probe := health.NewProbe(f.db.Conn, testRegistry(), health.DefaultPolicy(), health.WithClock(health.NewFakeClock(fixed)))

	snap := probe.Run(ctx, f.tenant)
	if snap.SchemaVersion != health.SnapshotSchemaVersion {
		t.Fatalf("schema version = %d, want %d", snap.SchemaVersion, health.SnapshotSchemaVersion)
	}
	if snap.RegistryVersion != 1 {
		t.Fatalf("registry version = %d, want 1", snap.RegistryVersion)
	}
	if len(snap.Stores) != 3 {
		t.Fatalf("store count = %d, want 3", len(snap.Stores))
	}
	wantOrder := []string{"ledger_event", "outbox", "projection_checkpoint"}
	for i, name := range wantOrder {
		if snap.Stores[i].Table != name {
			t.Fatalf("stores[%d] = %q, want %q (alphabetical order)", i, snap.Stores[i].Table, name)
		}
	}
	if snap.State != health.StateHealthy {
		t.Fatalf("state = %s, want HEALTHY; reasons: %v", snap.State, snap.Reasons)
	}

	again := probe.Run(ctx, f.tenant)
	if again.Digest != snap.Digest {
		t.Fatalf("re-running Run over identical evidence at the identical instant changed the digest:\n%s\n%s", snap.Digest, again.Digest)
	}
}

// TestTodo_STORE_003_Integration exercises more of Probe's surface at once
// than the primary test: two tenants sharing one schema stay isolated from
// each other's degraded state, and a CONTROL/REGISTRY table Probe has no
// store-specific check for still gets a genuine, live saturation verdict
// (row count versus a declared budget) through probeGeneric.
func TestTodo_STORE_003_Integration(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// Tenant A: healthy.
	f.commit(t, "int-a-1", 0, at)
	ackAll(t, f.db.Conn, f.tenant)

	// Tenant B: a second tenant registered directly against the same
	// schema, with its own stream/projection, left lagging.
	tenantB := uuid.New()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Beta', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantB, "beta-"+tenantB.String())
	f.db.Exec(t, `
		INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1, 'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenantB, hSchemaRef)

	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.EnsureStream(ctx, tx, tenantB, hStreamKey, "WORKER", hStreamKey); err != nil {
		t.Fatalf("ensure stream B: %v", err)
	}
	if err := projection.EnsureProjection(ctx, tx, tenantB, hProjection, hStreamKey); err != nil {
		t.Fatalf("ensure projection B: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	appenderB := ledger.New()
	tx, err = f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := appenderB.Append(ctx, tx, ledger.AppendRequest{
		Tenant: tenantB, StreamKey: hStreamKey, ExpectedHead: 0,
		AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:worker", SchemaRef: hSchemaRef,
		Payload: []byte("event:int-b-1"), OccurredAt: at, EffectiveAt: at,
		CorrelationID: uuid.New(), IdempotencyKey: "int-b-1",
	}); err != nil {
		t.Fatalf("append B: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	clock := health.NewFakeClock(at.Add(time.Minute))

	policy := health.DefaultPolicy()
	probeA := health.NewProbe(f.db.Conn, testRegistry(), policy, health.WithClock(clock))
	snapA := probeA.Run(ctx, f.tenant)
	if snapA.State != health.StateHealthy {
		t.Fatalf("tenant A state = %s, want HEALTHY (unaffected by tenant B's lag); reasons: %v", snapA.State, snapA.Reasons)
	}

	probeB := health.NewProbe(f.db.Conn, testRegistry(), policy, health.WithClock(clock))
	snapB := probeB.Run(ctx, tenantB)
	if snapB.State != health.StateDegraded {
		t.Fatalf("tenant B state = %s, want DEGRADED (its own projection is lagging)", snapB.State)
	}

	// Generic saturation: register payload_schema (no store-specific check)
	// with a budget of zero, which the fixture's own one row already
	// exceeds.
	genericRegistry := testRegistry()
	genericRegistry.Tables = append(genericRegistry.Tables, health.StoreDisposition{
		Table: "payload_schema", DataRole: "REGISTRY", TenantScopingColumn: "tenant_id", RetentionClass: "OPERATIONAL",
	})
	budgetPolicy := health.DefaultPolicy()
	budgetPolicy.RowCountBudgets = map[string]int64{"payload_schema": 0}
	probeGeneric := health.NewProbe(f.db.Conn, genericRegistry, budgetPolicy, health.WithClock(clock))
	snapGeneric := probeGeneric.Run(ctx, f.tenant)

	schemaStore := storesByTable(snapGeneric)["payload_schema"]
	if schemaStore.Freshness != nil {
		t.Fatal("a saturation-only store must not report Freshness")
	}
	if schemaStore.Recovery != nil {
		t.Fatal("a saturation-only store must not report Recovery")
	}
	if schemaStore.Saturation == nil || schemaStore.Saturation.State != health.StateDegraded {
		t.Fatalf("payload_schema saturation = %+v, want DEGRADED (1 row over a budget of 0)", schemaStore.Saturation)
	}
	if schemaStore.State != health.StateDegraded {
		t.Fatalf("payload_schema state = %s, want DEGRADED", schemaStore.State)
	}
}

// TestTodo_STORE_003_Fault proves a probe error yields UNKNOWN, not HEALTHY
// (DATA-020/STORE-003 GREEN), and that it never gets silently absorbed:
// every other store may be genuinely healthy, but the overall Snapshot
// still reports UNKNOWN rather than pretending the failed store was fine.
func TestTodo_STORE_003_Fault(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	f.commit(t, "fault-1", 0, at)
	ackAll(t, f.db.Conn, f.tenant)

	registry := testRegistry()
	registry.Tables = append(registry.Tables, health.StoreDisposition{
		Table: "no_such_table_xyz", DataRole: "REGISTRY", TenantScopingColumn: "tenant_id", RetentionClass: "OPERATIONAL",
	})

	probe := health.NewProbe(f.db.Conn, registry, health.DefaultPolicy(), health.WithClock(health.NewFakeClock(at.Add(time.Minute))))
	snap := probe.Run(ctx, f.tenant)

	broken := storesByTable(snap)["no_such_table_xyz"]
	if broken.State != health.StateUnknown {
		t.Fatalf("broken store state = %s, want UNKNOWN", broken.State)
	}
	if len(broken.Reasons) == 0 {
		t.Fatal("expected a reason naming the probe error")
	}

	if snap.State != health.StateUnknown {
		t.Fatalf("overall state = %s, want UNKNOWN even though every other store was healthy; reasons: %v", snap.State, snap.Reasons)
	}
	if snap.State == health.StateHealthy {
		t.Fatal("a probe error must never yield an overall HEALTHY state")
	}

	// The genuinely healthy stores must still report their own real state,
	// not get swept into UNKNOWN by the broken one.
	ledgerStore := storesByTable(snap)["ledger_event"]
	if ledgerStore.State != health.StateHealthy {
		t.Fatalf("ledger_event state = %s, want HEALTHY (unaffected by the unrelated broken table)", ledgerStore.State)
	}
}

// TestTodo_STORE_003_Recovery proves an outbox recovers from DEGRADED back
// to HEALTHY once its unacked row is actually acknowledged -- recovery
// evidence tracks real state, not a one-way ratchet.
func TestTodo_STORE_003_Recovery(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	receipt := f.commit(t, "rec003-1", 0, at)
	// See TestTodo_STORE_003's outbox subtest: created_at is stamped by the
	// database's own now() at insert time, so the clock must be anchored to
	// real wall-clock time, not to the business timestamp `at`.
	insertedAt := time.Now().UTC()

	policy := health.DefaultPolicy()
	policy.MaxOutboxUnackedAge = time.Minute
	clock := health.NewFakeClock(insertedAt.Add(5 * time.Minute))
	probe := health.NewProbe(f.db.Conn, testRegistry(), policy, health.WithClock(clock))

	before := probe.Run(ctx, f.tenant)
	if before.State != health.StateDegraded {
		t.Fatalf("state before ack = %s, want DEGRADED", before.State)
	}

	consumer := outbox.NewConsumer(f.db.Conn)
	batch, err := consumer.Poll(ctx, f.tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(batch) != 1 || batch[0].OutboxID != receipt.Outbox.OutboxID {
		t.Fatalf("polled %d messages, want exactly the one committed row", len(batch))
	}
	if err := consumer.Ack(ctx, f.tenant, batch[0].OutboxID); err != nil {
		t.Fatalf("ack: %v", err)
	}

	after := probe.Run(ctx, f.tenant)
	if after.State != health.StateHealthy {
		t.Fatalf("state after ack = %s, want HEALTHY; reasons: %v", after.State, after.Reasons)
	}
	outboxStore := storesByTable(after)["outbox"]
	if outboxStore.Freshness.HasEvidence {
		t.Fatal("outbox freshness still reports unacknowledged evidence after ack")
	}
}

// BenchmarkTodo_STORE_003 benchmarks the deterministic, pure part of a
// Probe evaluation -- Snapshot digesting -- over a representative number of
// stores. pgtest.New/NewEmpty require a *testing.T (see
// internal/data/pgtest/pgtest.go), so a *testing.B cannot drive a real
// PostgreSQL fixture through this package's frozen test harness; digesting
// is the CPU-bound step Run performs on every evaluation regardless of how
// many rows a store's query touched, so it is what this benchmark targets.
func BenchmarkTodo_STORE_003(b *testing.B) {
	stores := make([]health.StoreHealth, 0, 20)
	for i := 0; i < 20; i++ {
		lag := int64(i % 3)
		state := health.StateHealthy
		if lag > 0 {
			state = health.StateDegraded
		}
		stores = append(stores, health.StoreHealth{
			Table: "table_" + string(rune('a'+i)), DataRole: "PROJECTION", RetentionClass: "REBUILDABLE",
			RebuildSource: "ledger_event",
			Freshness: &health.FreshnessEvidence{
				HasEvidence: true, NewestAt: time.Now(), AgeSeconds: 12.5,
				LagSequences: lag, LagThreshold: 0, State: state,
			},
			Saturation: &health.SaturationEvidence{RowCount: int64(i * 100), RowBudget: 10000, State: health.StateHealthy},
			Recovery:   &health.RecoveryEvidence{HasEvidence: true, Status: "CURRENT", ChainConsistent: true, State: state},
			State:      state,
		})
	}
	snap := health.Snapshot{
		SchemaVersion: health.SnapshotSchemaVersion, Tenant: uuid.New(), GeneratedAt: time.Now(),
		RegistryVersion: 1, Stores: stores, State: health.StateDegraded,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := health.ComputeDigest(snap); err != nil {
			b.Fatalf("ComputeDigest: %v", err)
		}
	}
}
