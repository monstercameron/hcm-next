package runtime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-001's RED clause names two failures: a process restart that loses
// frontier, input, variables, version, context or attempt, and a stale or
// illegal transition that is nevertheless allowed. The tests below are that
// matrix.
//
// None of them exercises a scheduler, a lease, a fence token or a timer, and
// there is nothing here that could: WF-RUN-000 gates every one of those behind
// the P1B re-evaluation. Concurrency is carried entirely by the optimistic
// instance version, which is what makes the Race case below a test of a
// compare-and-set rather than of a lock.

// TestTodo_WF_RUN_001 is the PRIMARY case: everything a restart needs survives
// a restart, and a stale writer is refused.
//
// "Survives a restart" is tested by reading the state back on a connection
// opened after the writes committed, rather than from the handle that wrote
// them: an in-memory cache would satisfy a same-connection read and would lose
// everything the todo names.
func TestTodo_WF_RUN_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun001-primary")
	plan := referencePlan(t)
	store := runtime.Store{}
	conn := appConn(t, db)

	inst := newInstance(t, tenant, plan)
	inst.BusinessSubjectRefs = []string{"worker:jane", "position:POS-MGR-204"}
	inst.EffectiveContextRef = "ctx:legal.us-ca/2026.1"

	var created runtime.Instance
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = store.CreateInstance(ctx, tx, inst)
		return err
	})
	if created.InstanceVersion != 1 {
		t.Fatalf("a new instance is at version %d, want 1", created.InstanceVersion)
	}
	if created.RuntimeStatus != runtime.InstanceCreated {
		t.Fatalf("a new instance is %s, want CREATED", created.RuntimeStatus)
	}

	// Record the first node attempt. The instance version moves with it,
	// because a node write and the instance-version check commit together.
	first := runtime.NewNodeExecution(tenant, inst.InstanceID, plan.StartNodeID, 1,
		workflow.StepCapability, runtime.NodeRunning)
	first.InputSnapshotRef = "artifact:input/1"
	first.StartedAt = timePtr(fixedInstant)
	first.Refs = runtime.GovernanceRefs{
		AuthorizationDecisionID: "authz:1",
		PolicyRef:               "policy.promotion/1.0.0",
		RetryPolicyRef:          "policy.retry.observation.bounded/v1",
	}

	var afterNode int64
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		stored, version, err := store.RecordNodeExecution(ctx, tx, first, created.InstanceVersion)
		if err != nil {
			return err
		}
		afterNode = version
		if stored.NodeExecutionID != runtime.NodeExecutionID(tenant, inst.InstanceID, plan.StartNodeID, 1) {
			t.Errorf("stored node execution id %s is not the derived identity", stored.NodeExecutionID)
		}
		return nil
	})
	if afterNode != 2 {
		t.Fatalf("instance version after one node write is %d, want 2", afterNode)
	}

	// Move the instance to RUNNING, advancing the frontier and the variable
	// revision head at the same time.
	var running runtime.Instance
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		running, err = store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
			TenantID:             tenant,
			InstanceID:           inst.InstanceID,
			ExpectedVersion:      afterNode,
			Status:               runtime.InstanceRunning,
			CurrentNodeIDs:       []string{plan.StartNodeID, "second_node"},
			VariableRevisionHead: 7,
			EffectiveContextRef:  "ctx:legal.us-ca/2026.1",
			LastCheckpointRef:    "checkpoint:1",
			StartedAt:            timePtr(fixedInstant),
		})
		return err
	})
	if running.InstanceVersion != afterNode+1 {
		t.Fatalf("instance version after a state record is %d, want %d", running.InstanceVersion, afterNode+1)
	}

	// The restart. A brand new connection, a brand new session, nothing in
	// memory from the writes above.
	restarted := appConn(t, db)
	var (
		reloaded runtime.Instance
		nodes    []runtime.NodeExecution
	)
	inTenantTx(t, restarted, tenant, func(tx dbport.Tx) error {
		var err error
		reloaded, err = store.LoadInstance(ctx, tx, tenant, inst.InstanceID)
		if err != nil {
			return err
		}
		nodes, err = store.LoadNodeExecutions(ctx, tx, tenant, inst.InstanceID)
		return err
	})

	// Frontier.
	if got := reloaded.Frontier(); len(got) != 2 || got[0] != plan.StartNodeID || got[1] != "second_node" {
		t.Errorf("frontier after restart = %v, want [%s second_node]", got, plan.StartNodeID)
	}
	// Input.
	if reloaded.InputRef != inst.InputRef {
		t.Errorf("input ref after restart = %q, want %q", reloaded.InputRef, inst.InputRef)
	}
	// Variables.
	if reloaded.VariableRevisionHead != 7 {
		t.Errorf("variable revision head after restart = %d, want 7", reloaded.VariableRevisionHead)
	}
	// Version.
	if reloaded.InstanceVersion != running.InstanceVersion {
		t.Errorf("instance version after restart = %d, want %d", reloaded.InstanceVersion, running.InstanceVersion)
	}
	if reloaded.WorkflowVersion != plan.Version || reloaded.CompiledPlanHash != plan.Digest() {
		t.Errorf("pinned plan after restart = %s@%d/%s, want %s@%d/%s",
			reloaded.WorkflowID, reloaded.WorkflowVersion, reloaded.CompiledPlanHash,
			plan.WorkflowID, plan.Version, plan.Digest())
	}
	// Context.
	if reloaded.EffectiveContextRef != "ctx:legal.us-ca/2026.1" {
		t.Errorf("effective context ref after restart = %q", reloaded.EffectiveContextRef)
	}
	if reloaded.LastCheckpointRef != "checkpoint:1" {
		t.Errorf("last checkpoint ref after restart = %q", reloaded.LastCheckpointRef)
	}
	if len(reloaded.BusinessSubjectRefs) != 2 {
		t.Errorf("business subject refs after restart = %v", reloaded.BusinessSubjectRefs)
	}
	// Attempt, and the typed output/evidence references that hang off it.
	if len(nodes) != 1 {
		t.Fatalf("loaded %d node executions after restart, want 1", len(nodes))
	}
	if nodes[0].Attempt != 1 || nodes[0].Status != runtime.NodeRunning {
		t.Errorf("node execution after restart = attempt %d / %s, want attempt 1 / RUNNING",
			nodes[0].Attempt, nodes[0].Status)
	}
	if nodes[0].InputSnapshotRef != "artifact:input/1" || nodes[0].Refs.PolicyRef != "policy.promotion/1.0.0" {
		t.Errorf("node references after restart = %+v", nodes[0].Refs)
	}

	// A stale writer -- one holding a version the store has moved past --
	// changes nothing and is told exactly why.
	err := inTenantTxErr(restarted, tenant, func(tx dbport.Tx) error {
		_, txErr := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
			TenantID:        tenant,
			InstanceID:      inst.InstanceID,
			ExpectedVersion: 1, // the version the instance was created at
			Status:          runtime.InstanceWaiting,
			CurrentNodeIDs:  []string{"somewhere_else"},
		})
		return txErr
	})
	if err == nil {
		t.Fatal("a writer holding a stale instance version was allowed to write")
	}
	if code := runtime.CodeOf(err); code != runtime.CodeStaleInstance {
		t.Fatalf("stale writer refusal code = %q, want %q (%v)", code, runtime.CodeStaleInstance, err)
	}
	if !errors.Is(err, runtime.ErrRuntime) {
		t.Errorf("refusal does not unwrap to ErrRuntime: %v", err)
	}

	var unchanged runtime.Instance
	inTenantTx(t, restarted, tenant, func(tx dbport.Tx) error {
		var loadErr error
		unchanged, loadErr = store.LoadInstance(ctx, tx, tenant, inst.InstanceID)
		return loadErr
	})
	if unchanged.InstanceVersion != reloaded.InstanceVersion {
		t.Errorf("a refused stale write moved the version from %d to %d",
			reloaded.InstanceVersion, unchanged.InstanceVersion)
	}
	if unchanged.RuntimeStatus != runtime.InstanceRunning {
		t.Errorf("a refused stale write changed the status to %s", unchanged.RuntimeStatus)
	}
}

// TestTodo_WF_RUN_001_Race drives two genuinely concurrent writers, on
// separate connections and separate transactions, from the same instance
// version.
//
// Exactly one may win. That is the whole of this package's concurrency
// control: there is no lease to hold and no fence token to compare, so if the
// compare-and-set did not decide the winner, nothing would.
func TestTodo_WF_RUN_001_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun001-race")
	plan := referencePlan(t)
	store := runtime.Store{}

	inst := newInstance(t, tenant, plan)
	setup := appConn(t, db)
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		_, err := store.CreateInstance(ctx, tx, inst)
		return err
	})

	const writers = 6

	type result struct {
		version int64
		err     error
	}
	results := make([]result, writers)
	// Each writer gets its own connection: a pgx connection is not safe for
	// concurrent use, and sharing one would serialize the very race under test.
	conns := make([]*pgxadapter.Conn, writers)
	for i := range writers {
		conns[i] = appConn(t, db)
	}

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
	)
	start.Add(1)
	for i := range writers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			node := runtime.NewNodeExecution(tenant, inst.InstanceID, plan.StartNodeID, i+1,
				workflow.StepCapability, runtime.NodeRunning)
			err := inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
				_, version, recErr := store.RecordNodeExecution(ctx, tx, node, 1)
				if recErr != nil {
					return recErr
				}
				results[i] = result{version: version}
				return nil
			})
			if err != nil {
				results[i] = result{err: err}
			}
		}()
	}
	start.Done()
	done.Wait()

	winners := 0
	for i, r := range results {
		switch {
		case r.err == nil:
			winners++
			if r.version != 2 {
				t.Errorf("writer %d won with version %d, want 2", i, r.version)
			}
		case runtime.CodeOf(r.err) == runtime.CodeStaleInstance:
			// The expected loss.
		default:
			t.Errorf("writer %d failed for an unexpected reason: %v", i, r.err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d writers committed from the same instance version; exactly one may", winners, writers)
	}

	// Exactly one node execution exists: a loser that had already inserted its
	// row before losing the version race would show up here.
	reader := appConn(t, db)
	var nodes []runtime.NodeExecution
	inTenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		var err error
		nodes, err = store.LoadNodeExecutions(ctx, tx, tenant, inst.InstanceID)
		return err
	})
	if len(nodes) != 1 {
		t.Fatalf("%d node executions persisted after the race, want 1", len(nodes))
	}
}

// TestTodo_WF_RUN_001_Fault proves the second half of the RED clause: an
// illegal transition is refused, and a refused write inside a transaction
// leaves nothing behind.
func TestTodo_WF_RUN_001_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun001-fault")
	plan := referencePlan(t)
	store := runtime.Store{}
	conn := appConn(t, db)

	inst := newInstance(t, tenant, plan)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.CreateInstance(ctx, tx, inst)
		return err
	})

	t.Run("an instance may not skip from CREATED to COMPLETED", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
				TenantID:        tenant,
				InstanceID:      inst.InstanceID,
				ExpectedVersion: 1,
				Status:          runtime.InstanceCompleted,
				CompletedAt:     timePtr(fixedInstant),
			})
			return txErr
		})
		if code := runtime.CodeOf(err); code != runtime.CodeIllegalTransition {
			t.Fatalf("refusal code = %q, want %q (%v)", code, runtime.CodeIllegalTransition, err)
		}
	})

	t.Run("a terminal instance has no way back", func(t *testing.T) {
		if !runtime.InstanceCompleted.Terminal() {
			t.Fatal("COMPLETED is not terminal")
		}
		if runtime.LegalInstanceTransition(runtime.InstanceCompleted, runtime.InstanceRunning) {
			t.Fatal("a COMPLETED instance may transition back to RUNNING")
		}
		if runtime.LegalInstanceTransition(runtime.InstanceCancelled, runtime.InstanceRunning) {
			t.Fatal("a CANCELLED instance may transition back to RUNNING")
		}
	})

	t.Run("a node may not jump from READY to SUCCEEDED", func(t *testing.T) {
		node := runtime.NewNodeExecution(tenant, inst.InstanceID, plan.StartNodeID, 1,
			workflow.StepCapability, runtime.NodeReady)
		var version int64
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, v, err := store.RecordNodeExecution(ctx, tx, node, 1)
			version = v
			return err
		})

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, txErr := store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
				TenantID:                tenant,
				InstanceID:              inst.InstanceID,
				NodeID:                  plan.StartNodeID,
				Attempt:                 1,
				ExpectedInstanceVersion: version,
				Status:                  runtime.NodeSucceeded,
				CompletedAt:             timePtr(fixedInstant),
			})
			return txErr
		})
		if code := runtime.CodeOf(err); code != runtime.CodeIllegalTransition {
			t.Fatalf("refusal code = %q, want %q (%v)", code, runtime.CodeIllegalTransition, err)
		}

		// The refusal left the stored attempt exactly as it was, and did not
		// consume an instance version.
		var (
			stored  runtime.NodeExecution
			current runtime.Instance
		)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			stored, loadErr = store.LoadNodeExecution(ctx, tx, tenant, inst.InstanceID, plan.StartNodeID, 1)
			if loadErr != nil {
				return loadErr
			}
			current, loadErr = store.LoadInstance(ctx, tx, tenant, inst.InstanceID)
			return loadErr
		})
		if stored.Status != runtime.NodeReady {
			t.Errorf("the refused transition moved the node to %s", stored.Status)
		}
		if current.InstanceVersion != version {
			t.Errorf("the refused transition moved the instance version from %d to %d",
				version, current.InstanceVersion)
		}

		// The legal route to SUCCEEDED is through RUNNING, and it works.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, v, txErr := store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
				TenantID:                tenant,
				InstanceID:              inst.InstanceID,
				NodeID:                  plan.StartNodeID,
				Attempt:                 1,
				ExpectedInstanceVersion: version,
				Status:                  runtime.NodeRunning,
			})
			version = v
			return txErr
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			done, _, txErr := store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
				TenantID:                tenant,
				InstanceID:              inst.InstanceID,
				NodeID:                  plan.StartNodeID,
				Attempt:                 1,
				ExpectedInstanceVersion: version,
				Status:                  runtime.NodeSucceeded,
				OutputArtifactRef:       "artifact:output/1",
				CompletedAt:             timePtr(fixedInstant),
			})
			if txErr != nil {
				return txErr
			}
			if done.OutputArtifactRef != "artifact:output/1" {
				t.Errorf("output artifact ref = %q", done.OutputArtifactRef)
			}
			return nil
		})
	})

	t.Run("a rolled-back transaction persists neither the node nor the version bump", func(t *testing.T) {
		before := loadInstance(t, conn, tenant, inst.InstanceID)

		node := runtime.NewNodeExecution(tenant, inst.InstanceID, "rolled_back_node", 1,
			workflow.StepDecision, runtime.NodeReady)
		wantErr := errors.New("caller aborted after recording")
		err := inTxErr(conn, func(tx dbport.Tx) error {
			if txErr := tenancy.WithTenant(ctx, tx, tenant); txErr != nil {
				return txErr
			}
			if _, _, txErr := store.RecordNodeExecution(ctx, tx, node, before.InstanceVersion); txErr != nil {
				return txErr
			}
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("transaction error = %v, want the caller's own", err)
		}

		after := loadInstance(t, conn, tenant, inst.InstanceID)
		if after.InstanceVersion != before.InstanceVersion {
			t.Errorf("a rolled-back transaction still bumped the version from %d to %d",
				before.InstanceVersion, after.InstanceVersion)
		}
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, loadErr := store.LoadNodeExecution(ctx, tx, tenant, inst.InstanceID, "rolled_back_node", 1)
			return loadErr
		}); runtime.CodeOf(err) != runtime.CodeNodeExecutionNotFound {
			t.Errorf("a rolled-back node execution is still readable: %v", err)
		}
	})

	t.Run("an unknown instance is not found rather than silently created", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.LoadInstance(ctx, tx, tenant, uuid.New())
			return txErr
		})
		if code := runtime.CodeOf(err); code != runtime.CodeInstanceNotFound {
			t.Fatalf("refusal code = %q, want %q (%v)", code, runtime.CodeInstanceNotFound, err)
		}
	})
}

// TestTodo_WF_RUN_001_Security proves the tenant boundary is physical, not a
// convention: under the least-privilege role, one tenant's transaction cannot
// read, write or even learn about another tenant's instance, and a transaction
// that forgot to declare a tenant sees nothing at all.
func TestTodo_WF_RUN_001_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alice := insertTenant(t, db, "wfrun001-security-a")
	bob := insertTenant(t, db, "wfrun001-security-b")
	plan := referencePlan(t)
	store := runtime.Store{}
	conn := appConn(t, db)

	aliceInstance := newInstance(t, alice, plan)
	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		_, err := store.CreateInstance(ctx, tx, aliceInstance)
		return err
	})

	t.Run("another tenant cannot read the instance", func(t *testing.T) {
		err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
			_, txErr := store.LoadInstance(ctx, tx, alice, aliceInstance.InstanceID)
			return txErr
		})
		// Not found, not denied: the refusal must not confirm the instance
		// exists.
		if code := runtime.CodeOf(err); code != runtime.CodeInstanceNotFound {
			t.Fatalf("cross-tenant read code = %q, want %q (%v)", code, runtime.CodeInstanceNotFound, err)
		}
	})

	t.Run("a transaction with no tenant scope sees nothing", func(t *testing.T) {
		err := inTxErr(conn, func(tx dbport.Tx) error {
			_, txErr := store.LoadInstance(ctx, tx, alice, aliceInstance.InstanceID)
			return txErr
		})
		if code := runtime.CodeOf(err); code != runtime.CodeInstanceNotFound {
			t.Fatalf("unscoped read code = %q, want %q (%v)", code, runtime.CodeInstanceNotFound, err)
		}
	})

	t.Run("a tenant cannot write a row belonging to another", func(t *testing.T) {
		forged := newInstance(t, alice, plan)
		err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
			_, txErr := store.CreateInstance(ctx, tx, forged)
			return txErr
		})
		if err == nil {
			t.Fatal("a tenant-scoped transaction inserted another tenant's instance")
		}
	})

	t.Run("another tenant cannot see the node executions either", func(t *testing.T) {
		node := runtime.NewNodeExecution(alice, aliceInstance.InstanceID, plan.StartNodeID, 1,
			workflow.StepCapability, runtime.NodeRunning)
		inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
			_, _, err := store.RecordNodeExecution(ctx, tx, node, 1)
			return err
		})

		var seen []runtime.NodeExecution
		inTenantTx(t, conn, bob, func(tx dbport.Tx) error {
			var err error
			seen, err = store.LoadNodeExecutions(ctx, tx, alice, aliceInstance.InstanceID)
			return err
		})
		if len(seen) != 0 {
			t.Fatalf("another tenant read %d node executions", len(seen))
		}
	})
}

// TestTodo_WF_RUN_001_Mutation kills the mutants that would make "legal state,
// stable identity" unfalsifiable: an identity that is not derived, a record
// missing what a restart needs, and a completion instant on a status that
// never completed.
func TestTodo_WF_RUN_001_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun001-mutation")
	plan := referencePlan(t)
	store := runtime.Store{}
	conn := appConn(t, db)

	inst := newInstance(t, tenant, plan)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.CreateInstance(ctx, tx, inst)
		return err
	})

	t.Run("the node execution identity is derived, not allocated", func(t *testing.T) {
		a := runtime.NodeExecutionID(tenant, inst.InstanceID, "node_x", 3)
		b := runtime.NodeExecutionID(tenant, inst.InstanceID, "node_x", 3)
		if a != b {
			t.Fatalf("the same attempt derived two identities: %s and %s", a, b)
		}
		if a == runtime.NodeExecutionID(tenant, inst.InstanceID, "node_x", 4) {
			t.Fatal("two attempts of one node share an identity")
		}
		if a == runtime.NodeExecutionID(tenant, uuid.New(), "node_x", 3) {
			t.Fatal("the same node of two instances shares an identity")
		}
		if a == runtime.NodeExecutionID(uuid.New(), inst.InstanceID, "node_x", 3) {
			t.Fatal("the same node of two tenants shares an identity")
		}
	})

	t.Run("a hand-set identity is refused", func(t *testing.T) {
		node := runtime.NewNodeExecution(tenant, inst.InstanceID, plan.StartNodeID, 1,
			workflow.StepCapability, runtime.NodeReady)
		node.NodeExecutionID = uuid.New()
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, txErr := store.RecordNodeExecution(ctx, tx, node, 1)
			return txErr
		})
		if code := runtime.CodeOf(err); code != runtime.CodeInvalidRecord {
			t.Fatalf("refusal code = %q, want %q (%v)", code, runtime.CodeInvalidRecord, err)
		}
	})

	t.Run("recording the same attempt twice collides rather than forking", func(t *testing.T) {
		node := runtime.NewNodeExecution(tenant, inst.InstanceID, "twice", 1,
			workflow.StepDecision, runtime.NodeReady)
		// Read the version in its own transaction first: a pgx connection has
		// no nested transactions, so a read inside the write below would join
		// it rather than stand apart from it.
		held := currentVersion(t, conn, tenant, inst.InstanceID)
		var version int64
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, v, err := store.RecordNodeExecution(ctx, tx, node, held)
			version = v
			return err
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, txErr := store.RecordNodeExecution(ctx, tx, node, version)
			return txErr
		})
		if err == nil {
			t.Fatal("the same attempt was recorded twice under two rows")
		}
		if code := runtime.CodeOf(err); code != runtime.CodeStorageFailed {
			t.Fatalf("refusal code = %q, want %q (%v)", code, runtime.CodeStorageFailed, err)
		}
	})

	t.Run("a record missing what a restart needs is refused", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(i *runtime.Instance)
		}{
			{"no compiled plan hash", func(i *runtime.Instance) { i.CompiledPlanHash = "" }},
			{"no input ref", func(i *runtime.Instance) { i.InputRef = "" }},
			{"no frontier", func(i *runtime.Instance) { i.CurrentNodeIDs = nil }},
			{"no correlation id", func(i *runtime.Instance) { i.CorrelationID = "" }},
			{"undeclared status", func(i *runtime.Instance) { i.RuntimeStatus = "NEARLY_DONE" }},
			{"undeclared mode", func(i *runtime.Instance) { i.ExecutionMode = "GUESSING" }},
			{"completed before it ended", func(i *runtime.Instance) { i.CompletedAt = timePtr(fixedInstant) }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				broken := newInstance(t, tenant, plan)
				broken.InstanceID = uuid.New()
				tc.mutate(&broken)
				err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
					_, txErr := store.CreateInstance(ctx, tx, broken)
					return txErr
				})
				if code := runtime.CodeOf(err); code != runtime.CodeInvalidRecord {
					t.Fatalf("refusal code = %q, want %q (%v)", code, runtime.CodeInvalidRecord, err)
				}
			})
		}
	})

	t.Run("the database refuses a completion instant on a live status", func(t *testing.T) {
		// The Go validator above is one guard; the CHECK constraint in
		// migration 00016 is the other, and a mutant that deleted the Go
		// check must still be caught here.
		err := db.ExecErr(`
			INSERT INTO workflow_instance (
				tenant_id, instance_id, cell_id, workflow_id, workflow_version,
				compiled_plan_hash, execution_mode, runtime_status, input_ref,
				current_node_ids, correlation_id, completed_at)
			VALUES ($1, $2, 'cell-local', 'wf.test', 1,
				'`+zeroDigest+`', 'SIMULATE', 'RUNNING', 'sha256:x',
				ARRAY['n1'], 'corr', now())`,
			tenant, uuid.New())
		if err == nil {
			t.Fatal("the database accepted a RUNNING instance carrying a completion instant")
		}
	})

	t.Run("the declared status sets match the migration", func(t *testing.T) {
		// Two copies of a state machine drift. The Go transition tables in
		// state.go and the CHECK constraints in migration 00016 are exactly
		// that pair, so this asserts they still name the same statuses: a
		// status added to one and not the other would otherwise fail at write
		// time, in production, with a constraint error nobody could read.
		instanceDef := constraintDef(t, db, "workflow_instance_runtime_status_allowed")
		for _, s := range runtime.InstanceStatuses() {
			if !strings.Contains(instanceDef, "'"+string(s)+"'") {
				t.Errorf("instance status %q is declared in Go but not in the migration CHECK", s)
			}
		}
		if got := strings.Count(instanceDef, "'"); got != 2*len(runtime.InstanceStatuses()) {
			t.Errorf("the migration CHECK names %d literals, Go declares %d instance statuses",
				got/2, len(runtime.InstanceStatuses()))
		}

		nodeDef := constraintDef(t, db, "workflow_node_execution_status_allowed")
		for _, s := range runtime.NodeStatuses() {
			if !strings.Contains(nodeDef, "'"+string(s)+"'") {
				t.Errorf("node status %q is declared in Go but not in the migration CHECK", s)
			}
		}
		if got := strings.Count(nodeDef, "'"); got != 2*len(runtime.NodeStatuses()) {
			t.Errorf("the migration CHECK names %d literals, Go declares %d node statuses",
				got/2, len(runtime.NodeStatuses()))
		}
	})
}

// zeroDigest is a syntactically valid content_digest for a raw SQL fixture
// that is testing something other than the digest itself.
const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func loadInstance(t *testing.T, conn *pgxadapter.Conn, tenant, instanceID uuid.UUID) runtime.Instance {
	t.Helper()
	var out runtime.Instance
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = runtime.Store{}.LoadInstance(context.Background(), tx, tenant, instanceID)
		return err
	})
	return out
}

func currentVersion(t *testing.T, conn *pgxadapter.Conn, tenant, instanceID uuid.UUID) int64 {
	t.Helper()
	return loadInstance(t, conn, tenant, instanceID).InstanceVersion
}

// constraintDef returns one CHECK constraint's definition from this test's own
// schema. Filtering on the namespace matters: every pgtest test owns a private
// schema on one shared server, so an unfiltered pg_constraint read would count
// every concurrent test's copy of the same constraint.
func constraintDef(t *testing.T, db *pgtest.DB, name string) string {
	t.Helper()
	var def string
	err := db.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE c.conname = $1 AND n.nspname = current_schema()`, name).Scan(&def)
	if err != nil {
		t.Fatalf("read constraint %s: %v", name, err)
	}
	return def
}
