package recover_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// The instants every fixture stamps. Nothing in internal/workflow/recover
// reads a wall clock, so a test that wants a time has to name it -- and the
// difference between these two is the whole of "the worker died": deadAt is
// past the lease worker A took at bootAt.
var (
	bootAt   = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	deadAt   = bootAt.Add(5 * time.Minute)
	leaseTTL = 30 * time.Second
)

// workerA is the workload that dies; workerB is the one that recovers. Both
// are scheme-qualified with a replica reference, which is what WF-RUN-002's
// REFACTOR clause requires of a lease holder.
var (
	workerA = lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:cell-local-1"}
	workerB = lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:cell-local-2"}
)

// retention is a policy that outlives the window in which a dead worker could
// still come back. A record that expired sooner would let a late duplicate
// land as a brand new request.
var retention = idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: time.Hour}

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	// The ledger refuses an event whose payload schema is not registered, so
	// the business effect's own schema is registered here rather than the
	// effect being written against a schema nobody declared.
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.test.WorkflowNodeEffect', 1,
			'hcmnext.test.WorkflowNodeEffect', 'PROTOBUF', 'LEDGER_EVENT')`,
		id, effectSchemaRef)
	return id
}

// effectSchemaRef names the registered payload schema the business effect's
// ledger events are written under.
const effectSchemaRef = "hcmnext.test.WorkflowNodeEffect/v1"

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, which is the only way a test observes the
// row level security the runtime, lease, idempotency and ledger migrations
// declare.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTxErr(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	return inTxErr(conn, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// referencePlan compiles the promotion reference workflow, the one real
// compiled plan this repository has. Using it rather than a hand-built stub
// means the workflow id, version, plan digest, start node and step types are
// the values a real instance would carry.
func referencePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionExceedsThresholdPay)
	if err != nil {
		t.Fatalf("compile the promotion reference: %v", err)
	}
	return setup.Plan
}

// deadWorker is one node execution left exactly as a worker that died in the
// middle of it would have left it: the attempt is RUNNING, and the lease it
// was running under has lapsed by deadAt.
type deadWorker struct {
	tenant     uuid.UUID
	conn       *pgxadapter.Conn
	plan       *workflow.CompiledWorkflow
	instanceID uuid.UUID
	nodeID     string
	// version is the instance version after the setup writes.
	version int64
	// fence is the fence worker A still carries. It is stale from the moment
	// worker B takes the lease over, and worker A has no way to know.
	fence lease.Fence
}

// newDeadWorker creates an instance, records attempt 1 of its start node as
// RUNNING under a lease worker A took at bootAt, and never renews it.
func newDeadWorker(t *testing.T, db *pgtest.DB, key string) deadWorker {
	t.Helper()
	ctx := context.Background()
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	plan := referencePlan(t)

	inst, err := runtime.NewInstance(tenant, uuid.New(), "cell-local", plan,
		workflow.ModeSimulate, "sha256:input-snapshot", "corr-"+key, bootAt)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	nodeID := plan.StartNodeID
	node, ok := plan.Node(nodeID)
	if !ok {
		t.Fatalf("the compiled plan does not declare its own start node %q", nodeID)
	}

	out := deadWorker{tenant: tenant, conn: conn, plan: plan, instanceID: inst.InstanceID, nodeID: nodeID}
	store := runtime.Store{}
	var manager lease.Manager

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		ready := runtime.NewNodeExecution(tenant, inst.InstanceID, nodeID, 1, node.Type, runtime.NodeReady)
		ready.RecordedAt = bootAt
		_, version, err := store.RecordNodeExecution(ctx, tx, ready, stored.InstanceVersion)
		if err != nil {
			return err
		}
		grant, err := manager.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenant,
			Resource: lease.Resource{Kind: lease.ResourceNodeExecution, ID: inst.InstanceID.String() + "/" + nodeID},
			Holder:   workerA, Now: bootAt, TTL: leaseTTL,
		})
		if err != nil {
			return err
		}
		out.fence = grant.Fence
		_, version, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
			TenantID: tenant, InstanceID: inst.InstanceID, NodeID: nodeID, Attempt: 1,
			ExpectedInstanceVersion: version, Status: runtime.NodeRunning,
		})
		if err != nil {
			return err
		}
		out.version = version
		return nil
	})
	return out
}

// request is the recovery request for this dead node execution.
func (d deadWorker) request() wfrecover.Request {
	return wfrecover.Request{
		TenantID: d.tenant, InstanceID: d.instanceID, NodeID: d.nodeID,
		Plan: d.plan, CorrelationID: "corr-recovery",
	}
}

// recovererOn builds a recoverer for this dead node execution over conn,
// with the supplied effect, failpoint and (optionally overridden) verifier.
type recovererConfig struct {
	conn     *pgxadapter.Conn
	effect   wfrecover.Effect
	sink     runtime.ContinuationSink
	fail     wfrecover.Failpoint
	verifier runtime.FenceVerifier
	holder   lease.Identity
	now      time.Time
}

func (d deadWorker) recoverer(t *testing.T, cfg recovererConfig) wfrecover.Recoverer {
	t.Helper()
	if cfg.conn == nil {
		cfg.conn = d.conn
	}
	if cfg.sink == nil {
		cfg.sink = runtime.ContinuationStore{}
	}
	if cfg.verifier == nil {
		cfg.verifier = lease.Fenced{}
	}
	if cfg.holder == (lease.Identity{}) {
		cfg.holder = workerB
	}
	if cfg.now.IsZero() {
		cfg.now = deadAt
	}
	r, err := wfrecover.New(wfrecover.Options{
		DB:          cfg.conn,
		Clock:       wfrecover.FixedClock(cfg.now),
		Holder:      cfg.holder,
		LeaseTTL:    time.Minute,
		Idempotency: idempotency.PostgresStore{},
		Retention:   retention,
		Effects:     cfg.effect,
		Sink:        cfg.sink,
		Verifier:    cfg.verifier,
		Failpoints:  cfg.fail,
		TraceID:     "trace-wfrun003",
	})
	if err != nil {
		t.Fatalf("build the recoverer: %v", err)
	}
	return r
}

// --- the business effect ---------------------------------------------------

// effectStreamPrefix keys one ledger stream per guarded effect, so "how many
// business effects happened" is answerable by counting ledger_event rows
// rather than by trusting a counter the test itself keeps.
const effectStreamPrefix = "wfrun003:"

// effectPayload is what the effect writes into its ledger event. It is the
// durable record Replay reads back: the outcome is reconstructed from the
// committed row, never from anything the recovering process remembered.
type effectPayload struct {
	Outcome      string `json:"outcome"`
	OutputDigest string `json:"output_digest"`
}

// ledgerEffect is the business effect the recovery drives: it counts its own
// dispatches (the "effect sink" WF-RUN-003's FAULT clause asks to be counted)
// and appends exactly one ledger event per dispatch.
type ledgerEffect struct {
	outcome workflow.Outcome
	digest  string

	mu         sync.Mutex
	dispatches int
	replays    int
}

func newLedgerEffect() *ledgerEffect {
	return &ledgerEffect{outcome: workflow.OutcomeSucceeded, digest: "sha256:snapshot"}
}

// Dispatches is how many times the effect was actually performed.
func (e *ledgerEffect) Dispatches() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dispatches
}

// Replays is how many times a committed result was reconstructed instead.
func (e *ledgerEffect) Replays() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.replays
}

func (e *ledgerEffect) Perform(ctx context.Context, tx dbport.Tx, req wfrecover.EffectRequest) (wfrecover.EffectResult, error) {
	e.mu.Lock()
	e.dispatches++
	e.mu.Unlock()

	streamKey := effectStreamPrefix + req.Scope.Key
	if err := ledger.EnsureStream(ctx, tx, req.TenantID, streamKey,
		"WORKFLOW_INSTANCE", req.InstanceID.String()); err != nil {
		return wfrecover.EffectResult{}, err
	}
	payload, err := json.Marshal(effectPayload{Outcome: string(e.outcome), OutputDigest: e.digest})
	if err != nil {
		return wfrecover.EffectResult{}, err
	}
	receipt, err := ledger.Append(ctx, tx, ledger.AppendRequest{
		Tenant: req.TenantID, StreamKey: streamKey, ExpectedHead: 0,
		AssertionClass: ledger.TransactionFact,
		SourceRef:      req.Fence.HolderID,
		SchemaRef:      effectSchemaRef,
		Payload:        payload,
		OccurredAt:     req.RecordedAt, EffectiveAt: req.RecordedAt,
		CorrelationID:  req.InstanceID,
		IdempotencyKey: req.Scope.Key,
	})
	if err != nil {
		return wfrecover.EffectResult{}, err
	}
	return wfrecover.EffectResult{
		Identity: idempotency.ResultIdentity{
			ResultRef:      "node-effect:" + req.NodeID,
			EventRef:       streamKey + "#" + strconv.FormatInt(receipt.Sequence, 10),
			EffectIdentity: "effect:" + req.Scope.Key,
		},
		Outcome: frontier.NodeOutcome{NodeID: req.NodeID, Outcome: e.outcome, OutputDigest: e.digest},
	}, nil
}

func (e *ledgerEffect) Replay(
	ctx context.Context, ex wfrecover.Executor, req wfrecover.EffectRequest, stored idempotency.ResultIdentity,
) (frontier.NodeOutcome, error) {
	e.mu.Lock()
	e.replays++
	e.mu.Unlock()

	streamKey, seqText, ok := strings.Cut(stored.EventRef, "#")
	if !ok {
		return frontier.NodeOutcome{}, fmt.Errorf("stored event ref %q names no sequence", stored.EventRef)
	}
	seq, err := strconv.ParseInt(seqText, 10, 64)
	if err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("stored event ref %q: %w", stored.EventRef, err)
	}
	var raw []byte
	if err := ex.QueryRow(ctx,
		`SELECT payload FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		req.TenantID, streamKey, seq).Scan(&raw); err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("read the committed effect %s: %w", stored.EventRef, err)
	}
	var body effectPayload
	if err := json.Unmarshal(raw, &body); err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("decode the committed effect %s: %w", stored.EventRef, err)
	}
	return frontier.NodeOutcome{
		NodeID: req.NodeID, Outcome: workflow.Outcome(body.Outcome), OutputDigest: body.OutputDigest,
	}, nil
}

var _ wfrecover.Effect = (*ledgerEffect)(nil)

// --- assertions over durable rows -----------------------------------------

// ledgerRows counts the business effects that actually committed for one
// tenant. It is deliberately a count of rows rather than of calls: a counter
// the test keeps can be bumped by an effect whose transaction rolled back.
func ledgerRows(t *testing.T, db *pgtest.DB, tenant uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key LIKE $2`,
		tenant, effectStreamPrefix+"%").Scan(&n); err != nil {
		t.Fatalf("count ledger events: %v", err)
	}
	return n
}

// nodeAttempts reads every recorded attempt of one node, oldest first.
func nodeAttempts(t *testing.T, conn *pgxadapter.Conn, d deadWorker) []runtime.NodeExecution {
	t.Helper()
	var out []runtime.NodeExecution
	inTenantTx(t, conn, d.tenant, func(tx dbport.Tx) error {
		rows, err := runtime.Store{}.LoadNodeExecutions(context.Background(), tx, d.tenant, d.instanceID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.NodeID == d.nodeID {
				out = append(out, row)
			}
		}
		return nil
	})
	return out
}

// loadInstance reads the instance row.
func loadInstance(t *testing.T, conn *pgxadapter.Conn, d deadWorker) runtime.Instance {
	t.Helper()
	var inst runtime.Instance
	inTenantTx(t, conn, d.tenant, func(tx dbport.Tx) error {
		var err error
		inst, err = runtime.Store{}.LoadInstance(context.Background(), tx, d.tenant, d.instanceID)
		return err
	})
	return inst
}

// idempotencyStatus reads the stored record for one recovery request.
func idempotencyStatus(t *testing.T, conn *pgxadapter.Conn, d deadWorker) (idempotency.Record, bool) {
	t.Helper()
	var (
		rec   idempotency.Record
		found bool
	)
	inTenantTx(t, conn, d.tenant, func(tx dbport.Tx) error {
		var err error
		rec, found, err = idempotency.PostgresStore{}.Lookup(context.Background(), tx, d.request().Scope())
		return err
	})
	return rec, found
}

// acceptAnyFence is the MUTATION case's removed guard: a verifier that never
// refuses. It is the exact mutation WF-RUN-003 names -- "removing the fence
// check" -- expressed as a collaborator swap, so every line of the recovery
// itself runs unchanged.
type acceptAnyFence struct{}

func (acceptAnyFence) VerifyFence(context.Context, runtime.Executor, uuid.UUID, runtime.Fence) error {
	return nil
}

var _ runtime.FenceVerifier = acceptAnyFence{}

// recordingTx wraps a real transaction and records the SQL that went through
// it. It is how an ordering claim -- "the fence was refused before the
// idempotency table was even read" -- is asserted rather than asserted about.
type recordingTx struct {
	tx dbport.Tx

	mu   sync.Mutex
	sqls []string
}

func (r *recordingTx) note(sqlText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sqls = append(r.sqls, sqlText)
}

// statements returns every statement issued through this transaction so far.
func (r *recordingTx) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sqls...)
}

// touched reports whether any statement named the given table.
func (r *recordingTx) touched(table string) bool {
	for _, s := range r.statements() {
		if strings.Contains(s, table) {
			return true
		}
	}
	return false
}

// wrote reports whether any statement was a write.
func (r *recordingTx) wrote() bool {
	for _, s := range r.statements() {
		upper := strings.ToUpper(s)
		if strings.Contains(upper, "INSERT ") || strings.Contains(upper, "UPDATE ") ||
			strings.Contains(upper, "DELETE ") {
			return true
		}
	}
	return false
}

func (r *recordingTx) Exec(ctx context.Context, sqlText string, args ...any) (int64, error) {
	r.note(sqlText)
	return r.tx.Exec(ctx, sqlText, args...)
}

func (r *recordingTx) Query(ctx context.Context, sqlText string, args ...any) (dbport.Rows, error) {
	r.note(sqlText)
	return r.tx.Query(ctx, sqlText, args...)
}

func (r *recordingTx) QueryRow(ctx context.Context, sqlText string, args ...any) dbport.Row {
	r.note(sqlText)
	return r.tx.QueryRow(ctx, sqlText, args...)
}

func (r *recordingTx) Commit(ctx context.Context) error   { return r.tx.Commit(ctx) }
func (r *recordingTx) Rollback(ctx context.Context) error { return r.tx.Rollback(ctx) }

var _ dbport.Tx = (*recordingTx)(nil)

// sha256Hex renders a canonical digest of a marker string, in the lower-case
// hex form the idempotency_record's content_digest domain requires.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
