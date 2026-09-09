package recover

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// unitHolder is a well-formed workload identity for the wiring tests below.
// A bare hostname is refused by WF-RUN-002's own rule, which is what
// TestRecover_NewRefusesIncompleteWiring exercises.
var unitHolder = lease.Identity{
	WorkloadRef: "workload:hcmnext-workflow-runtime",
	InstanceRef: "replica:unit-1",
}

type stubBeginner struct{}

func (stubBeginner) Begin(context.Context) (dbport.Tx, error) { return nil, errors.New("no database") }

type stubEffect struct{}

func (stubEffect) Perform(context.Context, dbport.Tx, EffectRequest) (EffectResult, error) {
	return EffectResult{}, errors.New("not called")
}

func (stubEffect) Replay(context.Context, Executor, EffectRequest, idempotency.ResultIdentity) (frontier.NodeOutcome, error) {
	return frontier.NodeOutcome{}, errors.New("not called")
}

type stubVerifier struct{}

func (stubVerifier) VerifyFence(context.Context, runtime.Executor, uuid.UUID, runtime.Fence) error {
	return nil
}

func validOptions() Options {
	return Options{
		DB:          stubBeginner{},
		Clock:       FixedClock(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)),
		Holder:      unitHolder,
		LeaseTTL:    time.Minute,
		Idempotency: idempotency.PostgresStore{},
		Retention:   idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Effects:     stubEffect{},
		Sink:        runtime.NewMemorySink(),
		Verifier:    stubVerifier{},
	}
}

func TestRecover_NewAcceptsCompleteWiringAndDefaultsTheFailpoint(t *testing.T) {
	r, err := New(validOptions())
	if err != nil {
		t.Fatalf("complete wiring was refused: %v", err)
	}
	if r.opts.Failpoints == nil {
		t.Fatal("New left the failpoint port nil; an ordinary recovery would panic")
	}
	if err := r.opts.Failpoints.Check(context.Background(), PhaseAfterResultCommit); err != nil {
		t.Fatalf("the default failpoint crashed: %v", err)
	}
}

func TestRecover_NewRefusesIncompleteWiring(t *testing.T) {
	cases := map[string]func(*Options){
		"no database":            func(o *Options) { o.DB = nil },
		"no clock":               func(o *Options) { o.Clock = nil },
		"no idempotency store":   func(o *Options) { o.Idempotency = nil },
		"no effect":              func(o *Options) { o.Effects = nil },
		"no continuation sink":   func(o *Options) { o.Sink = nil },
		"no fence verifier":      func(o *Options) { o.Verifier = nil },
		"no lease ttl":           func(o *Options) { o.LeaseTTL = 0 },
		"a bare hostname holder": func(o *Options) { o.Holder = lease.Identity{WorkloadRef: "host-7", InstanceRef: "pid-1"} },
		"an unhonorable policy": func(o *Options) {
			o.Retention = idempotency.RetentionPolicy{Retention: time.Hour, RetryWindow: 24 * time.Hour}
		},
	}
	for name, mutate := range cases {
		opts := validOptions()
		mutate(&opts)
		if _, err := New(opts); err == nil {
			t.Fatalf("wiring with %s was accepted", name)
		} else if CodeOf(err) != CodeInvalid {
			t.Fatalf("wiring with %s: code = %q, want %q (%v)", name, CodeOf(err), CodeInvalid, err)
		}
	}
}

func TestRecover_RefusesAMalformedRequestBeforeOpeningATransaction(t *testing.T) {
	r, err := New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// stubBeginner always fails, so reaching the database at all would
	// produce a storage refusal rather than an invalid-request one.
	_, err = r.Recover(context.Background(), Request{})
	if CodeOf(err) != CodeInvalid {
		t.Fatalf("code = %q, want %q (%v)", CodeOf(err), CodeInvalid, err)
	}
}

func TestRecover_DispatchEffectRefusesAMalformedAttempt(t *testing.T) {
	r, err := New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.DispatchEffect(context.Background(), nil, Request{}, 1, runtime.Fence{}, time.Now()); CodeOf(err) != CodeInvalid {
		t.Fatalf("code = %q, want %q (%v)", CodeOf(err), CodeInvalid, err)
	}
}

func TestRecover_RefsForRecordsOnlyThePresentReferences(t *testing.T) {
	refs := refsFor(idempotency.ResultIdentity{ResultRef: "result-1", EventRef: "stream/1"})
	if refs.CapabilityExecutionID != "result-1" {
		t.Fatalf("capability execution id = %q", refs.CapabilityExecutionID)
	}
	if len(refs.EffectRefs) != 1 || refs.EffectRefs[0] != "stream/1" {
		t.Fatalf("effect refs = %v, want only the present event ref", refs.EffectRefs)
	}
	empty := refsFor(idempotency.ResultIdentity{})
	if len(empty.EffectRefs) != 0 {
		t.Fatalf("an empty identity produced effect refs %v", empty.EffectRefs)
	}
}
