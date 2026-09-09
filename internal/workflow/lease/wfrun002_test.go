package lease_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// WF-RUN-002's matrix, against a real PostgreSQL through internal/data/pgtest.
//
// The ticket's RED clause is one sentence -- "expired/stale worker heartbeat
// or fence completes a node, writes state or dispatches an effect" -- and it
// is really two claims: that a superseded holder is refused, and that the
// refusal costs nothing. Every case below therefore asserts the refusal and
// then reads the durable rows back to show they did not move.

func wantCode(t *testing.T, err error, code string, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected a refusal, got nil", what)
	}
	if got := lease.CodeOf(err); got != code {
		t.Fatalf("%s: code = %q (%v), want %q", what, got, err, code)
	}
}

func wantIs(t *testing.T, err, target error, what string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: err = %v, want %v", what, err, target)
	}
}

func TestTodo_WF_RUN_002(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newLeaseFixture(t, db, "lease-primary")

	// 1. A first claim mints fence token 1 and says so.
	first := f.acquire(t, holderA, fixedInstant, 30*time.Second)
	if first.Fence.Token != 1 {
		t.Fatalf("first fence token = %d, want 1", first.Fence.Token)
	}
	if first.Evidence.Kind != lease.TransitionAcquired || first.Evidence.PriorToken != 0 {
		t.Fatalf("first acquisition evidence = %+v, want ACQUIRED with no prior token", first.Evidence)
	}
	if first.Evidence.HolderID != holderA.HolderID() {
		t.Fatalf("evidence holder = %q, want %q", first.Evidence.HolderID, holderA.HolderID())
	}
	if first.ExpiresAt != fixedInstant.Add(30*time.Second).UTC() {
		t.Fatalf("expiry = %s, want %s", first.ExpiresAt, fixedInstant.Add(30*time.Second).UTC())
	}

	// 2. The holder's own fence verifies while the window is open.
	f.do(t, func(tx dbport.Tx) error {
		held, err := f.manager.Verify(ctx, tx, first.Fence, fixedInstant.Add(time.Second))
		if err != nil {
			return err
		}
		if held.Evidence.Kind != lease.TransitionVerified {
			t.Fatalf("verify evidence = %q, want VERIFIED", held.Evidence.Kind)
		}
		return nil
	})

	// 3. Renewal extends the window. The same fence that would have been dead
	//    at the original expiry is now good past it -- and the token does not
	//    change, because renewing is not re-acquiring.
	var renewed lease.Grant
	f.do(t, func(tx dbport.Tx) error {
		var err error
		renewed, err = f.manager.Renew(ctx, tx, first.Fence, fixedInstant.Add(20*time.Second), time.Minute)
		return err
	})
	if renewed.Fence.Token != first.Fence.Token {
		t.Fatalf("renewal changed the fence token: %d -> %d", first.Fence.Token, renewed.Fence.Token)
	}
	if renewed.Evidence.Kind != lease.TransitionRenewed {
		t.Fatalf("renewal evidence = %q, want RENEWED", renewed.Evidence.Kind)
	}
	f.do(t, func(tx dbport.Tx) error {
		_, err := f.manager.Verify(ctx, tx, first.Fence, fixedInstant.Add(40*time.Second))
		return err
	})

	// 4. Expiry is observed by the caller against its own clock, never swept.
	var live, lapsed lease.Observation
	f.do(t, func(tx dbport.Tx) error {
		var err error
		live, err = f.manager.Observe(ctx, tx, f.tenant, f.resource, fixedInstant.Add(40*time.Second))
		return err
	})
	if !live.Held || live.Expired || live.Token != 1 || live.HolderID != holderA.HolderID() {
		t.Fatalf("observation inside the window = %+v", live)
	}
	afterLapse := fixedInstant.Add(20*time.Second + time.Minute + time.Second)
	f.do(t, func(tx dbport.Tx) error {
		var err error
		lapsed, err = f.manager.Observe(ctx, tx, f.tenant, f.resource, afterLapse)
		return err
	})
	if !lapsed.Held || !lapsed.Expired {
		t.Fatalf("observation past the window = %+v, want held-but-expired", lapsed)
	}

	// 5. A second workload takes the lapsed lease. The token advances, and the
	//    transition is recorded as a takeover rather than a first claim.
	second := f.acquire(t, holderB, afterLapse, 30*time.Second)
	if second.Fence.Token != 2 {
		t.Fatalf("takeover fence token = %d, want 2", second.Fence.Token)
	}
	if second.Evidence.Kind != lease.TransitionTakenOver || second.Evidence.PriorToken != 1 {
		t.Fatalf("takeover evidence = %+v, want TAKEN_OVER from token 1", second.Evidence)
	}

	// 6. The stale holder comes back. It cannot tell by looking at itself that
	//    it lost the resource -- comparison is what refuses it.
	err := f.try(func(tx dbport.Tx) error {
		_, verr := f.manager.Verify(ctx, tx, first.Fence, afterLapse.Add(time.Second))
		return verr
	})
	wantCode(t, err, lease.CodeFenceStale, "a superseded holder verifying its old fence")
	wantIs(t, err, lease.ErrFenceStale, "a superseded holder verifying its old fence")

	// 7. Release ends the claim, and the released fence is then lost rather
	//    than stale: there is no live lease to be behind.
	f.do(t, func(tx dbport.Tx) error {
		ev, rerr := f.manager.Release(ctx, tx, second.Fence, afterLapse.Add(2*time.Second))
		if rerr != nil {
			return rerr
		}
		if ev.Kind != lease.TransitionReleased || ev.Token != 2 {
			t.Fatalf("release evidence = %+v, want RELEASED at token 2", ev)
		}
		return nil
	})
	err = f.try(func(tx dbport.Tx) error {
		_, verr := f.manager.Verify(ctx, tx, second.Fence, afterLapse.Add(3*time.Second))
		return verr
	})
	wantCode(t, err, lease.CodeLeaseLost, "verifying a released fence")
	wantIs(t, err, lease.ErrLeaseLost, "verifying a released fence")

	// 8. The durable rows back every record the live calls returned: two
	//    leases, four transitions, tokens 1 then 2, holders A then B.
	var history []lease.Evidence
	f.do(t, func(tx dbport.Tx) error {
		var herr error
		history, herr = f.manager.History(ctx, tx, f.tenant, f.resource)
		return herr
	})
	if len(history) != 4 {
		t.Fatalf("history = %d records, want 4 (two openings and two closings)", len(history))
	}
	wantHistory := []struct {
		kind   lease.TransitionKind
		token  uint64
		holder string
	}{
		{lease.TransitionAcquired, 1, holderA.HolderID()},
		{lease.TransitionExpired, 1, holderA.HolderID()},
		{lease.TransitionAcquired, 2, holderB.HolderID()},
		{lease.TransitionReleased, 2, holderB.HolderID()},
	}
	for i, want := range wantHistory {
		got := history[i]
		if got.Kind != want.kind || got.Token != want.token || got.HolderID != want.holder {
			t.Fatalf("history[%d] = %s/%d/%s, want %s/%d/%s",
				i, got.Kind, got.Token, got.HolderID, want.kind, want.token, want.holder)
		}
		if got.Digest() == "" {
			t.Fatalf("history[%d] carries no evidence digest", i)
		}
	}
}

// RACE: N concurrent acquirers on N real connections yield exactly one holder.
func TestTodo_WF_RUN_002_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "lease-race")
	resource := lease.Resource{Kind: lease.ResourceNodeExecution, ID: "node:contended"}

	const racers = 8
	conns := make([]*pgxadapter.Conn, 0, racers)
	for range racers {
		conns = append(conns, appConn(t, db))
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		grants  []lease.Grant
		refused []error
		manager lease.Manager
	)
	start := make(chan struct{})
	for i, conn := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			holder := lease.Identity{
				WorkloadRef: "workload:hcmnext-workflow-runtime",
				InstanceRef: "replica:racer-" + itoaTest(i),
			}
			<-start
			var grant lease.Grant
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				var aerr error
				grant, aerr = manager.Acquire(ctx, tx, lease.AcquireRequest{
					TenantID: tenant, Resource: resource, Holder: holder,
					Now: fixedInstant, TTL: time.Minute,
				})
				return aerr
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				refused = append(refused, err)
				return
			}
			grants = append(grants, grant)
		}()
	}
	close(start)
	wg.Wait()

	if len(grants) != 1 {
		t.Fatalf("%d acquirers succeeded, want exactly 1 (refusals: %v)", len(grants), refused)
	}
	if len(refused) != racers-1 {
		t.Fatalf("%d refusals, want %d", len(refused), racers-1)
	}
	for _, err := range refused {
		wantIs(t, err, lease.ErrHeld, "a losing concurrent acquirer")
		if code := lease.CodeOf(err); code != lease.CodeLeaseHeld {
			t.Fatalf("losing acquirer code = %q, want %q (%v)", code, lease.CodeLeaseHeld, err)
		}
	}
	if grants[0].Fence.Token != 1 {
		t.Fatalf("winner's fence token = %d, want 1", grants[0].Fence.Token)
	}

	// One HELD row, and it is the winner's.
	var held int
	var holderID string
	db.QueryRow(ctx, `SELECT count(*), coalesce(max(holder_id), '') FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3 AND lease_state = 'HELD'`,
		tenant, resource.Kind, resource.ID).Scan(&held, &holderID)
	if held != 1 {
		t.Fatalf("%d HELD lease rows, want 1", held)
	}
	if holderID != grants[0].Fence.Holder.HolderID() {
		t.Fatalf("HELD row holder = %q, want the winner %q", holderID, grants[0].Fence.Holder.HolderID())
	}
	// And no losing acquirer wrote a row of its own.
	var total int
	db.QueryRow(ctx, `SELECT count(*) FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3`,
		tenant, resource.Kind, resource.ID).Scan(&total)
	if total != 1 {
		t.Fatalf("%d lease rows in total, want 1: a refused acquirer wrote something", total)
	}
}

// FAULT: every refusal leaves the durable state exactly as it was.
func TestTodo_WF_RUN_002_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newLeaseFixture(t, db, "lease-fault")

	grant := f.acquire(t, holderA, fixedInstant, time.Minute)
	before := leaseRow(t, db, f.tenant, f.resource)

	// A live lease is not takeable.
	_, err := f.tryAcquire(holderB, fixedInstant.Add(time.Second), time.Minute)
	wantCode(t, err, lease.CodeLeaseHeld, "acquiring a live lease")
	wantIs(t, err, lease.ErrHeld, "acquiring a live lease")

	// Nor is it expirable: this package refuses to declare a live holder dead.
	err = f.try(func(tx dbport.Tx) error {
		_, eerr := f.manager.Expire(ctx, tx, f.tenant, f.resource, fixedInstant.Add(time.Second))
		return eerr
	})
	wantCode(t, err, lease.CodeLeaseLive, "expiring a live lease")
	wantIs(t, err, lease.ErrLeaseLive, "expiring a live lease")

	// A fence naming another lease line is foreign, not merely stale.
	foreign := grant.Fence
	foreign.LeaseID = uuid.New()
	err = f.try(func(tx dbport.Tx) error {
		_, verr := f.manager.Verify(ctx, tx, foreign, fixedInstant.Add(time.Second))
		return verr
	})
	wantCode(t, err, lease.CodeFenceForeign, "verifying a fence from another lease line")
	wantIs(t, err, lease.ErrFenceStale, "verifying a fence from another lease line")

	// So is one whose holder is not the live holder, even at the right token.
	wrongHolder := grant.Fence
	wrongHolder.Holder = holderB
	err = f.try(func(tx dbport.Tx) error {
		_, verr := f.manager.Verify(ctx, tx, wrongHolder, fixedInstant.Add(time.Second))
		return verr
	})
	wantCode(t, err, lease.CodeFenceForeign, "verifying another workload's fence at the right token")

	// The holder's own fence stops working the moment its window closes: an
	// expired worker must not write, and it finds that out by comparison
	// against its own clock reading rather than by being swept.
	err = f.try(func(tx dbport.Tx) error {
		_, verr := f.manager.Verify(ctx, tx, grant.Fence, fixedInstant.Add(2*time.Minute))
		return verr
	})
	wantCode(t, err, lease.CodeLeaseLost, "verifying past the holder's own expiry")
	wantIs(t, err, lease.ErrLeaseLost, "verifying past the holder's own expiry")

	// A renewal presented past the window is refused for the same reason.
	err = f.try(func(tx dbport.Tx) error {
		_, rerr := f.manager.Renew(ctx, tx, grant.Fence, fixedInstant.Add(2*time.Minute), time.Minute)
		return rerr
	})
	wantCode(t, err, lease.CodeLeaseLost, "renewing past the holder's own expiry")

	// Nothing above moved the row.
	after := leaseRow(t, db, f.tenant, f.resource)
	if before != after {
		t.Fatalf("a refused call changed the lease row:\nbefore %+v\nafter  %+v", before, after)
	}

	// A rolled-back acquisition persists nothing at all.
	rollbackTenant := insertTenant(t, db, "lease-fault-rollback")
	conn := appConn(t, db)
	txCtx := context.Background()
	tx, berr := conn.Begin(txCtx)
	if berr != nil {
		t.Fatalf("begin: %v", berr)
	}
	if terr := tenancy.WithTenant(txCtx, tx, rollbackTenant); terr != nil {
		t.Fatalf("scope tenant: %v", terr)
	}
	if _, aerr := (lease.Manager{}).Acquire(txCtx, tx, lease.AcquireRequest{
		TenantID: rollbackTenant, Resource: f.resource, Holder: holderA,
		Now: fixedInstant, TTL: time.Minute,
	}); aerr != nil {
		t.Fatalf("acquire inside the doomed transaction: %v", aerr)
	}
	if rerr := tx.Rollback(txCtx); rerr != nil {
		t.Fatalf("rollback: %v", rerr)
	}
	var rows int
	db.QueryRow(ctx, `SELECT count(*) FROM workflow_lease WHERE tenant_id = $1`, rollbackTenant).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d lease rows survived a rolled-back acquisition, want 0", rows)
	}
}

// MUTATION: the guards are load-bearing, and the fence line is monotonic.
func TestTodo_WF_RUN_002_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newLeaseFixture(t, db, "lease-mutation")

	// Three successive claims: acquire, release, acquire, expire, acquire.
	one := f.acquire(t, holderA, fixedInstant, time.Minute)
	f.do(t, func(tx dbport.Tx) error {
		_, err := f.manager.Release(ctx, tx, one.Fence, fixedInstant.Add(time.Second))
		return err
	})
	two := f.acquire(t, holderB, fixedInstant.Add(2*time.Second), time.Minute)
	lapse := fixedInstant.Add(2 * time.Second).Add(time.Minute).Add(time.Second)
	f.do(t, func(tx dbport.Tx) error {
		_, err := f.manager.Expire(ctx, tx, f.tenant, f.resource, lapse)
		return err
	})
	three := f.acquire(t, holderA, lapse.Add(time.Second), time.Minute)

	if one.Fence.Token != 1 || two.Fence.Token != 2 || three.Fence.Token != 3 {
		t.Fatalf("tokens = %d,%d,%d; want 1,2,3 -- the fence line must be monotonic across the whole history",
			one.Fence.Token, two.Fence.Token, three.Fence.Token)
	}
	// A released token is never reissued: that is what makes an old holder's
	// token comparable rather than ambiguous.
	if two.Fence.Token == one.Fence.Token || three.Fence.Token == two.Fence.Token {
		t.Fatalf("a fence token was reused")
	}

	// The right token with the wrong lease id is refused, so the token alone
	// is not the credential -- mutate either half and the fence stops working.
	for name, mutate := range map[string]func(f *lease.Fence){
		"token one behind":     func(fc *lease.Fence) { fc.Token = 2 },
		"token one ahead":      func(fc *lease.Fence) { fc.Token = 4 },
		"another lease id":     func(fc *lease.Fence) { fc.LeaseID = one.Fence.LeaseID },
		"another holder":       func(fc *lease.Fence) { fc.Holder = holderB },
		"another resource id":  func(fc *lease.Fence) { fc.Resource.ID = "instance:someone-else" },
		"another resource kin": func(fc *lease.Fence) { fc.Resource.Kind = lease.ResourceQueue },
	} {
		fence := three.Fence
		mutate(&fence)
		err := f.try(func(tx dbport.Tx) error {
			_, verr := f.manager.Verify(ctx, tx, fence, lapse.Add(2*time.Second))
			return verr
		})
		if err == nil {
			t.Fatalf("a fence with %s was accepted", name)
		}
		if !errors.Is(err, lease.ErrFenceStale) && !errors.Is(err, lease.ErrLeaseLost) {
			t.Fatalf("a fence with %s: err = %v, want ErrFenceStale or ErrLeaseLost", name, err)
		}
	}

	// The unmutated fence still works, so the cases above failed for the
	// reason claimed and not because the whole lease was broken.
	f.do(t, func(tx dbport.Tx) error {
		_, err := f.manager.Verify(ctx, tx, three.Fence, lapse.Add(2*time.Second))
		return err
	})

	// Evidence digests separate the transitions rather than merely labelling
	// them: the same lease row acquired and taken over do not share a digest.
	if one.Evidence.Digest() == two.Evidence.Digest() || two.Evidence.Digest() == three.Evidence.Digest() {
		t.Fatalf("two distinct lease transitions share an evidence digest")
	}
}

// CONFORMANCE (beyond the ticket's declared matrix): the WF-RUN-000 gate
// blocks scheduler code, so this package must contain no clock read, no
// ticker, no sleep and no goroutine. Asserting it against the package's own
// source is the only way the claim stays true as the package grows.
func TestTodo_WF_RUN_002_Conformance(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	banned := map[string]bool{"Now": true, "Sleep": true, "Tick": true, "NewTicker": true, "NewTimer": true, "After": true}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt:
				t.Fatalf("%s starts a goroutine; this package runs nothing of its own", name)
			case *ast.SelectorExpr:
				ident, ok := node.X.(*ast.Ident)
				if ok && ident.Name == "time" && banned[node.Sel.Name] {
					t.Fatalf("%s calls time.%s; every instant is the caller's", name, node.Sel.Name)
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatalf("scanned no package source files; the conformance check proved nothing")
	}
}

// --- small local helpers ----------------------------------------------

// leaseRowSnapshot is the comparable subset of a workflow_lease row a fault
// case reads back to prove a refusal wrote nothing.
type leaseRowSnapshot struct {
	LeaseID  uuid.UUID
	State    string
	Fence    int64
	HolderID string
	Version  int64
	Expires  time.Time
}

func leaseRow(t *testing.T, db *pgtest.DB, tenant uuid.UUID, res lease.Resource) leaseRowSnapshot {
	t.Helper()
	var out leaseRowSnapshot
	db.QueryRow(context.Background(), `
		SELECT lease_id, lease_state, fence_token, holder_id, lease_version, expires_at
		FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = $2 AND resource_id = $3 AND lease_state = 'HELD'`,
		tenant, res.Kind, res.ID).Scan(&out.LeaseID, &out.State, &out.Fence, &out.HolderID, &out.Version, &out.Expires)
	out.Expires = out.Expires.UTC()
	return out
}

func itoaTest(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
