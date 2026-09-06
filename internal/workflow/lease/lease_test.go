package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// refusingExecutor fails the test if any statement reaches the database. Every
// test in this file uses it, which is how "refused before any statement runs"
// becomes an assertion rather than a claim in a comment.
type refusingExecutor struct{ t *testing.T }

func (e refusingExecutor) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	e.t.Fatalf("a refused call still issued a statement: %s", sql)
	return 0, nil
}

func (e refusingExecutor) Query(_ context.Context, sql string, _ ...any) (dbport.Rows, error) {
	e.t.Fatalf("a refused call still issued a query: %s", sql)
	return nil, nil
}

func (e refusingExecutor) QueryRow(_ context.Context, sql string, _ ...any) dbport.Row {
	e.t.Fatalf("a refused call still issued a query: %s", sql)
	return nil
}

var _ Executor = refusingExecutor{}

var (
	testTenant   = uuid.MustParse("55555555-5555-4555-8555-555555555555")
	testLeaseID  = uuid.MustParse("66666666-6666-4666-8666-666666666666")
	testResource = Resource{Kind: ResourceWorkflowInstance, ID: "instance:1"}
	testNow      = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	testHolder   = Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:1"}
)

func wellFormedFence() Fence {
	return Fence{TenantID: testTenant, Resource: testResource, LeaseID: testLeaseID, Holder: testHolder, Token: 1}
}

func TestFence_ValidateRefusesEveryMalformedShape(t *testing.T) {
	if err := wellFormedFence().Validate(); err != nil {
		t.Fatalf("a well-formed fence was refused: %v", err)
	}
	cases := map[string]func(f *Fence){
		"no tenant":   func(f *Fence) { f.TenantID = uuid.Nil },
		"no resource": func(f *Fence) { f.Resource = Resource{} },
		"no lease id": func(f *Fence) { f.LeaseID = uuid.Nil },
		"no holder":   func(f *Fence) { f.Holder = Identity{} },
		"bare host":   func(f *Fence) { f.Holder = Identity{WorkloadRef: "host-1", InstanceRef: "p"} },
		"zero token":  func(f *Fence) { f.Token = 0 },
	}
	for name, mutate := range cases {
		f := wellFormedFence()
		mutate(&f)
		err := f.Validate()
		if err == nil {
			t.Fatalf("%s was accepted as a fence", name)
		}
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestAcquire_RefusesAMalformedRequestWithoutTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	ex := refusingExecutor{t: t}
	var m Manager

	cases := map[string]AcquireRequest{
		"no tenant": {Resource: testResource, Holder: testHolder, Now: testNow, TTL: time.Minute},
		"undeclared resource kind": {
			TenantID: testTenant, Resource: Resource{Kind: "TENANT", ID: "x"},
			Holder: testHolder, Now: testNow, TTL: time.Minute,
		},
		"bare hostname holder": {
			TenantID: testTenant, Resource: testResource,
			Holder: Identity{WorkloadRef: "host-1", InstanceRef: "p"}, Now: testNow, TTL: time.Minute,
		},
		"no clock reading": {TenantID: testTenant, Resource: testResource, Holder: testHolder, TTL: time.Minute},
		"non-positive ttl": {TenantID: testTenant, Resource: testResource, Holder: testHolder, Now: testNow},
	}
	for name, req := range cases {
		if _, err := m.Acquire(ctx, ex, req); err == nil {
			t.Fatalf("%s was accepted", name)
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestRenewReleaseExpireObserve_RefuseMalformedInputBeforeAnyStatement(t *testing.T) {
	ctx := context.Background()
	ex := refusingExecutor{t: t}
	var m Manager
	fence := wellFormedFence()

	if _, err := m.Renew(ctx, ex, fence, time.Time{}, time.Minute); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Renew with no clock reading: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Renew(ctx, ex, fence, testNow, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Renew with a zero ttl: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Renew(ctx, ex, Fence{}, testNow, time.Minute); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Renew with an empty fence: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Release(ctx, ex, fence, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Release with no clock reading: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Expire(ctx, ex, uuid.Nil, testResource, testNow); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Expire with no tenant: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Expire(ctx, ex, testTenant, testResource, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Expire with no clock reading: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Observe(ctx, ex, testTenant, testResource, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Observe with no clock reading: err = %v, want ErrInvalid", err)
	}
	if _, err := m.Verify(ctx, ex, Fence{}, testNow); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Verify with an empty fence: err = %v, want ErrInvalid", err)
	}
	if _, err := m.History(ctx, ex, uuid.Nil, testResource); !errors.Is(err, ErrInvalid) {
		t.Fatalf("History with no tenant: err = %v, want ErrInvalid", err)
	}
}

// The package must contain no clock, no ticker and no goroutine: expiry is
// observed by a caller, never swept (the WF-RUN-000 gate). The source-level
// proof lives in the conformance test in wfrun002_test.go; this one holds the
// API shape, which is what makes that possible -- every entry point takes its
// instant.
func TestManager_EveryEntryPointTakesTheCallersInstant(t *testing.T) {
	ctx := context.Background()
	ex := refusingExecutor{t: t}
	var m Manager

	// A zero instant is refused everywhere it is load-bearing, so no method
	// can quietly fall back to time.Now.
	if _, err := m.Acquire(ctx, ex, AcquireRequest{
		TenantID: testTenant, Resource: testResource, Holder: testHolder, TTL: time.Minute,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Acquire accepted a zero instant: %v", err)
	}
	// Verify is the exception, and it is a documented one: a zero instant
	// means "compare tokens only", which is what a holder tidying up after
	// itself needs. It still refuses a malformed fence first, so it never
	// reaches the database here.
	if _, err := m.Verify(ctx, ex, Fence{Token: 1}, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Verify accepted a malformed fence: %v", err)
	}
}

func TestItoa_RendersFenceTokens(t *testing.T) {
	for in, want := range map[uint64]string{0: "0", 1: "1", 42: "42", 18446744073709551615: "18446744073709551615"} {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestClosingTransition_MapsSettledStatesAndLeavesHeldOpen(t *testing.T) {
	for state, want := range map[string]TransitionKind{
		"RELEASED": TransitionReleased,
		"EXPIRED":  TransitionExpired,
		"REVOKED":  TransitionRevoked,
	} {
		got, ok := closingTransition(state)
		if !ok || got != want {
			t.Fatalf("closingTransition(%q) = %q,%v; want %q,true", state, got, ok, want)
		}
	}
	if _, ok := closingTransition("HELD"); ok {
		t.Fatalf("a HELD lease closes nothing yet")
	}
}
