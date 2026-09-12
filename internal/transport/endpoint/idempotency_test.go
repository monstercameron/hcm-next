package endpoint

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// TestEndpointIdempotencyAndRevisionParityUnderConcurrentReplay is the
// PRIMARY test for ENDPOINT-004: concurrent replay of the same request must
// produce exactly one logical effect with identical results across every
// caller, and a stale expected-revision precondition must refuse the write
// while naming the resource's current revision.
func TestEndpointIdempotencyAndRevisionParityUnderConcurrentReplay(t *testing.T) {
	t.Run("concurrent replay produces one effect and identical outcomes", func(t *testing.T) {
		coord := NewCoordinator()
		scope := Scope{Principal: "user-42", Tenant: "tenant-9", Capability: "payroll.run.apply"}
		payload := []byte(`{"run_id":"r-1","amount_cents":500000}`)
		rev := uint64(7)
		var effectCount int32

		const goroutines = 32
		outcomes := make([]Outcome, goroutines)
		errs := make([]error, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func(i int) {
				defer wg.Done()
				req := Request{
					Scope:            scope,
					HeaderKey:        "retry-abc",
					MessageKey:       "retry-abc",
					Payload:          payload,
					ExpectedRevision: &rev,
					CurrentRevision:  rev,
				}
				outcomes[i], errs[i] = coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
					atomic.AddInt32(&effectCount, 1)
					return Outcome{
						ResultDigest:   "payroll-run-committed",
						EvidenceDigest: "evidence:rev=8",
						Status:         "OK",
						SideEffects:    SideEffects{Effects: 1, Events: 1},
					}, nil
				})
			}(i)
		}
		wg.Wait()

		if got := atomic.LoadInt32(&effectCount); got != 1 {
			t.Fatalf("effect ran %d times across %d concurrent callers, want exactly 1", got, goroutines)
		}
		first := outcomes[0]
		for i, o := range outcomes {
			if errs[i] != nil {
				t.Fatalf("caller %d: unexpected error %v", i, errs[i])
			}
			if o != first {
				t.Fatalf("caller %d observed a different outcome than caller 0: %+v vs %+v", i, o, first)
			}
		}
		if first.SideEffects.Effects != 1 {
			t.Fatalf("stored outcome effects = %d, want 1", first.SideEffects.Effects)
		}
		if first.ResultDigest != "payroll-run-committed" {
			t.Fatalf("stored outcome result digest = %q, want the committed result", first.ResultDigest)
		}
	})

	t.Run("stale revision refuses the write and names the current revision", func(t *testing.T) {
		coord := NewCoordinator()
		scope := Scope{Principal: "user-42", Tenant: "tenant-9", Capability: "payroll.run.apply"}
		payload := []byte(`{"run_id":"r-2"}`)
		stale := uint64(3)
		var effectCount int32
		req := Request{
			Scope:            scope,
			HeaderKey:        "retry-stale-1",
			Payload:          payload,
			ExpectedRevision: &stale,
			CurrentRevision:  9,
		}
		_, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			atomic.AddInt32(&effectCount, 1)
			return Outcome{Status: "OK"}, nil
		})
		var conflict *RevisionConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("expected *RevisionConflict, got %v", err)
		}
		if !errors.Is(err, ErrRevisionMismatch) {
			t.Fatalf("expected errors.Is(err, ErrRevisionMismatch) to hold for %v", err)
		}
		if conflict.Current != 9 {
			t.Fatalf("current revision = %d, want 9", conflict.Current)
		}
		if got := atomic.LoadInt32(&effectCount); got != 0 {
			t.Fatalf("effect ran %d times despite a stale expected revision, want 0", got)
		}
	})
}

// TestTodo_ENDPOINT_004_Property proves the canonical digest is stable and
// total: identical inputs always produce the identical digest, every
// distinguishing field changes it, header/message unification behaves
// symmetrically, and header extraction is independent of header insertion
// order.
func TestTodo_ENDPOINT_004_Property(t *testing.T) {
	scope := Scope{Principal: "p", Tenant: "t", Capability: "c"}

	d1, err := CanonicalDigest(scope, "key-1", "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d2, err := CanonicalDigest(scope, "key-1", "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest is not stable across identical calls: %s vs %s", d1, d2)
	}

	// Header-only and message-only present the same logical key; the
	// unified digest must not depend on which channel carried it.
	dHeaderOnly, err := CanonicalDigest(scope, "key-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dMessageOnly, err := CanonicalDigest(scope, "", "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d1 != dHeaderOnly || d1 != dMessageOnly {
		t.Fatalf("header-only (%s) and message-only (%s) digests must equal the combined digest (%s)", dHeaderOnly, dMessageOnly, d1)
	}

	// Header extraction must not depend on header insertion order.
	h1 := http.Header{}
	h1.Add(IdempotencyKeyHeader, "abc")
	h2 := http.Header{}
	h2.Add("X-Other", "z")
	h2.Add(IdempotencyKeyHeader, "abc")
	h2.Add("X-Another", "y")
	if got, want := HeaderIdempotencyKey(h1), HeaderIdempotencyKey(h2); got != want {
		t.Fatalf("header extraction depends on insertion order: %q vs %q", got, want)
	}

	// Length-prefixing must prevent concatenation collisions across field
	// boundaries: ("ab","c") must not hash the same as ("a","bc").
	scopeA := Scope{Principal: "ab", Tenant: "c", Capability: "cap"}
	scopeB := Scope{Principal: "a", Tenant: "bc", Capability: "cap"}
	dA, err := CanonicalDigest(scopeA, "k", "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dB, err := CanonicalDigest(scopeB, "k", "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dA == dB {
		t.Fatalf("concatenation collision: (%q,%q) and (%q,%q) hashed the same", scopeA.Principal, scopeA.Tenant, scopeB.Principal, scopeB.Tenant)
	}

	// Totality: every distinguishing field must change the digest, and no
	// two variants may collide with each other either.
	variants := map[string]struct {
		scope Scope
		key   string
	}{
		"principal":  {Scope{Principal: "other", Tenant: scope.Tenant, Capability: scope.Capability}, "key-1"},
		"tenant":     {Scope{Principal: scope.Principal, Tenant: "other", Capability: scope.Capability}, "key-1"},
		"capability": {Scope{Principal: scope.Principal, Tenant: scope.Tenant, Capability: "other"}, "key-1"},
		"key":        {scope, "key-2"},
	}
	seen := map[string]string{"base": d1}
	for name, v := range variants {
		got, err := CanonicalDigest(v.scope, v.key, v.key)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		for otherName, otherDigest := range seen {
			if got == otherDigest {
				t.Fatalf("%s collided with %s digest %s", name, otherName, got)
			}
		}
		seen[name] = got
	}

	// Mismatched header/message keys must error rather than silently pick
	// one side.
	if _, err := CanonicalDigest(scope, "key-1", "key-2"); !errors.Is(err, ErrIdempotencyKeyMismatch) {
		t.Fatalf("expected ErrIdempotencyKeyMismatch, got %v", err)
	}

	// Zero values are refused, never treated as valid/matching.
	if _, err := CanonicalDigest(scope, "", ""); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("expected ErrIdempotencyKeyRequired, got %v", err)
	}
	if _, err := CanonicalDigest(Scope{}, "key-1", "key-1"); !errors.Is(err, ErrScopeRequired) {
		t.Fatalf("expected ErrScopeRequired, got %v", err)
	}
	if _, err := CanonicalDigest(Scope{Principal: "p", Tenant: "t"}, "key-1", "key-1"); !errors.Is(err, ErrScopeRequired) {
		t.Fatalf("expected ErrScopeRequired for a blank capability, got %v", err)
	}
}

// TestTodo_ENDPOINT_004_Race runs real goroutines against one idempotency
// key. Exactly one of them must execute the effect; every other goroutine
// ("loser") must observe the winner's stored outcome rather than racing it.
func TestTodo_ENDPOINT_004_Race(t *testing.T) {
	coord := NewCoordinator()
	scope := Scope{Principal: "u2", Tenant: "t2", Capability: "cap.b"}
	payload := []byte(`{"x":1}`)
	var effectCount int32
	release := make(chan struct{})
	// The winning goroutine blocks inside the effect until every other
	// goroutine has had a chance to attempt Do. If a second effect could
	// run concurrently, it would not be blocked on release and would
	// increment effectCount past 1 before we close release.
	effect := func(ctx context.Context) (Outcome, error) {
		atomic.AddInt32(&effectCount, 1)
		<-release
		return Outcome{Status: "OK", ResultDigest: "committed-once"}, nil
	}

	const n = 16
	results := make([]Outcome, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	started := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			started <- struct{}{}
			req := Request{Scope: scope, HeaderKey: "race-key", Payload: payload}
			results[i], errs[i] = coord.Do(context.Background(), req, effect)
		}(i)
	}
	for i := 0; i < n; i++ {
		<-started
	}
	// Every goroutine has at least been scheduled; release the winner now.
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&effectCount); got != 1 {
		t.Fatalf("effect ran %d times across %d goroutines, want exactly 1", got, n)
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: unexpected error %v", i, errs[i])
		}
		if results[i].ResultDigest != "committed-once" {
			t.Fatalf("goroutine %d observed %q, want the winner's stored outcome", i, results[i].ResultDigest)
		}
	}
}

// TestTodo_ENDPOINT_004_Integration drives the reusable transport-parity
// harness (Vector/Compare from harness.go) so that a header-keyed HTTP path
// and message-keyed direct/gRPC paths are shown to converge on the exact
// same canonical digest and therefore the exact same stored effect.
func TestTodo_ENDPOINT_004_Integration(t *testing.T) {
	coord := NewCoordinator()
	scope := Scope{Principal: "user-1", Tenant: "tenant-1", Capability: "cap.write"}
	payload := []byte(`{"amount":10}`)
	var effectCount int32

	call := func(t *testing.T, headerKey, messageKey string) Outcome {
		t.Helper()
		req := Request{Scope: scope, HeaderKey: headerKey, MessageKey: messageKey, Payload: payload}
		outcome, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			atomic.AddInt32(&effectCount, 1)
			return Outcome{
				RequestDigest:  "req-1",
				ResultDigest:   "result-1",
				EvidenceDigest: "evidence-1",
				Status:         "OK",
				SideEffects:    SideEffects{Effects: 1},
			}, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return outcome
	}

	v := Vector{
		Name:   "idempotent-write-unified-across-transports",
		Direct: func(ctx context.Context) Outcome { return call(t, "", "retry-key-1") },
		GRPC:   func(ctx context.Context) Outcome { return call(t, "", "retry-key-1") },
		HTTP:   func(ctx context.Context) Outcome { return call(t, "retry-key-1", "") },
	}
	report := Compare(context.Background(), v)
	if err := report.Assert(); err != nil {
		t.Fatalf("transports diverged even though the header and message keys unify to one digest: %v", err)
	}
	if got := atomic.LoadInt32(&effectCount); got != 1 {
		t.Fatalf("effect ran %d times across 3 transports, want exactly 1 logical operation", got)
	}
}

// TestTodo_ENDPOINT_004_Fault proves that retrying after an ambiguous
// response (the effect's own outcome is unknown/errored) replays the stored
// ambiguous result instead of invoking the effect a second time.
func TestTodo_ENDPOINT_004_Fault(t *testing.T) {
	coord := NewCoordinator()
	scope := Scope{Principal: "u3", Tenant: "t3", Capability: "cap.c"}
	payload := []byte(`{"op":"withdraw"}`)
	var effectCount int32
	ambiguous := errors.New("endpoint: upstream response was ambiguous (timeout after send)")
	effect := func(ctx context.Context) (Outcome, error) {
		atomic.AddInt32(&effectCount, 1)
		return Outcome{Status: "UNKNOWN", EvidenceDigest: "evidence:ambiguous-commit"}, ambiguous
	}
	req := Request{Scope: scope, HeaderKey: "retry-k9", Payload: payload}

	first, err1 := coord.Do(context.Background(), req, effect)
	if !errors.Is(err1, ambiguous) {
		t.Fatalf("first attempt: expected the ambiguous error, got %v", err1)
	}
	if first.Status != "UNKNOWN" {
		t.Fatalf("first attempt status = %q, want UNKNOWN", first.Status)
	}

	// The client cannot tell whether the write landed, so it retries with
	// the SAME idempotency key and payload rather than minting a new one.
	second, err2 := coord.Do(context.Background(), req, effect)
	if !errors.Is(err2, ambiguous) {
		t.Fatalf("retry: expected the same ambiguous error replayed, got %v", err2)
	}
	if second != first {
		t.Fatalf("retry observed a different outcome than the original attempt: %+v vs %+v", second, first)
	}
	if got := atomic.LoadInt32(&effectCount); got != 1 {
		t.Fatalf("effect ran %d times across the retry, want exactly 1", got)
	}
}

// TestTodo_ENDPOINT_004_Security proves the digest scope is load-bearing:
// the same idempotency key and the same payload under a different
// principal, tenant or capability must never collide into another caller's
// stored result.
func TestTodo_ENDPOINT_004_Security(t *testing.T) {
	coord := NewCoordinator()
	payload := []byte(`{"amount":1}`)
	base := Scope{Principal: "alice", Tenant: "tenant-a", Capability: "cap.write"}

	var baseCount int32
	baseReq := Request{Scope: base, HeaderKey: "shared-key", Payload: payload}
	baseOutcome, err := coord.Do(context.Background(), baseReq, func(ctx context.Context) (Outcome, error) {
		atomic.AddInt32(&baseCount, 1)
		return Outcome{ResultDigest: "alice-result", Status: "OK"}, nil
	})
	if err != nil {
		t.Fatalf("base request: unexpected error %v", err)
	}

	variants := []struct {
		name  string
		scope Scope
	}{
		{"different principal", Scope{Principal: "mallory", Tenant: base.Tenant, Capability: base.Capability}},
		{"different tenant", Scope{Principal: base.Principal, Tenant: "tenant-b", Capability: base.Capability}},
		{"different capability", Scope{Principal: base.Principal, Tenant: base.Tenant, Capability: "cap.read"}},
	}

	var otherCount int32
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			req := Request{Scope: v.scope, HeaderKey: "shared-key", Payload: payload}
			outcome, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
				atomic.AddInt32(&otherCount, 1)
				return Outcome{ResultDigest: v.name + "-result", Status: "OK"}, nil
			})
			if err != nil {
				t.Fatalf("%s: unexpected error %v", v.name, err)
			}
			if outcome.ResultDigest == baseOutcome.ResultDigest {
				t.Fatalf("%s: reused alice's stored result instead of running its own effect", v.name)
			}
		})
	}

	if got := atomic.LoadInt32(&baseCount); got != 1 {
		t.Fatalf("base effect ran %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&otherCount); got != int32(len(variants)) {
		t.Fatalf("other-scope effects ran %d times, want %d (one per distinct scope)", got, len(variants))
	}
}

// TestTodo_ENDPOINT_004_Conformance proves stale/zero-revision handling:
// no precondition requested runs unconditionally; a zero expected or
// current revision is refused rather than treated as a match; a stale
// revision returns precondition evidence naming the current revision
// without the write landing; a matching revision commits.
func TestTodo_ENDPOINT_004_Conformance(t *testing.T) {
	scope := Scope{Principal: "u4", Tenant: "t4", Capability: "cap.d"}
	payload := []byte(`{"field":"value"}`)

	t.Run("no expected revision runs the effect unconditionally", func(t *testing.T) {
		coord := NewCoordinator()
		var ran bool
		req := Request{Scope: scope, HeaderKey: "k-no-rev", Payload: payload}
		_, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			ran = true
			return Outcome{Status: "OK"}, nil
		})
		if err != nil || !ran {
			t.Fatalf("expected the effect to run without a revision precondition, err=%v ran=%v", err, ran)
		}
	})

	t.Run("zero expected revision is refused, not treated as a match", func(t *testing.T) {
		coord := NewCoordinator()
		zero := uint64(0)
		var ran bool
		req := Request{Scope: scope, HeaderKey: "k-zero-exp", Payload: payload, ExpectedRevision: &zero, CurrentRevision: 5}
		_, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			ran = true
			return Outcome{Status: "OK"}, nil
		})
		if !errors.Is(err, ErrRevisionRequired) {
			t.Fatalf("expected ErrRevisionRequired, got %v", err)
		}
		if ran {
			t.Fatalf("effect ran despite a zero expected revision")
		}
	})

	t.Run("zero current revision is refused, not treated as a match", func(t *testing.T) {
		coord := NewCoordinator()
		expected := uint64(1)
		var ran bool
		req := Request{Scope: scope, HeaderKey: "k-zero-cur", Payload: payload, ExpectedRevision: &expected, CurrentRevision: 0}
		_, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			ran = true
			return Outcome{Status: "OK"}, nil
		})
		if !errors.Is(err, ErrRevisionRequired) {
			t.Fatalf("expected ErrRevisionRequired, got %v", err)
		}
		if ran {
			t.Fatalf("effect ran despite a zero current revision")
		}
	})

	t.Run("stale revision returns precondition evidence naming the current revision", func(t *testing.T) {
		coord := NewCoordinator()
		expected := uint64(4)
		var ran bool
		req := Request{Scope: scope, HeaderKey: "k-stale", Payload: payload, ExpectedRevision: &expected, CurrentRevision: 6}
		_, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			ran = true
			return Outcome{Status: "OK"}, nil
		})
		var conflict *RevisionConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("expected *RevisionConflict, got %v", err)
		}
		if conflict.Current != 6 {
			t.Fatalf("current revision = %d, want 6", conflict.Current)
		}
		if ran {
			t.Fatalf("effect ran despite a stale expected revision")
		}
	})

	t.Run("matching revision commits", func(t *testing.T) {
		coord := NewCoordinator()
		expected := uint64(6)
		var ran bool
		req := Request{Scope: scope, HeaderKey: "k-match", Payload: payload, ExpectedRevision: &expected, CurrentRevision: 6}
		_, err := coord.Do(context.Background(), req, func(ctx context.Context) (Outcome, error) {
			ran = true
			return Outcome{Status: "OK"}, nil
		})
		if err != nil || !ran {
			t.Fatalf("expected the effect to run when revisions match, err=%v ran=%v", err, ran)
		}
	})
}

// TestTodo_ENDPOINT_004_Mutation proves the sharpest RED clause: the same
// idempotency key replayed with a changed payload returns a conflict, never
// the prior success, for several distinct kinds of payload mutation.
func TestTodo_ENDPOINT_004_Mutation(t *testing.T) {
	coord := NewCoordinator()
	scope := Scope{Principal: "u1", Tenant: "t1", Capability: "cap.a"}
	var effectCount int32
	effect := func(ctx context.Context) (Outcome, error) {
		atomic.AddInt32(&effectCount, 1)
		return Outcome{Status: "OK", SideEffects: SideEffects{Effects: 1}}, nil
	}

	first := Request{Scope: scope, HeaderKey: "k1", Payload: []byte(`{"amount":10}`)}
	if _, err := coord.Do(context.Background(), first, effect); err != nil {
		t.Fatalf("first call: unexpected error %v", err)
	}

	mutations := []struct {
		name    string
		payload []byte
	}{
		{"amount changed", []byte(`{"amount":11}`)},
		{"field added", []byte(`{"amount":10,"note":"x"}`)},
		{"field removed", []byte(`{}`)},
		{"value re-typed as string", []byte(`{"amount":"10"}`)},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			req := Request{Scope: scope, HeaderKey: "k1", Payload: m.payload}
			_, err := coord.Do(context.Background(), req, effect)
			var conflict *PayloadConflict
			if !errors.As(err, &conflict) {
				t.Fatalf("payload %q: expected *PayloadConflict, got %v", m.payload, err)
			}
			if !errors.Is(err, ErrIdempotencyConflict) {
				t.Fatalf("payload %q: expected errors.Is(err, ErrIdempotencyConflict)", m.payload)
			}
		})
	}

	if got := atomic.LoadInt32(&effectCount); got != 1 {
		t.Fatalf("effect ran %d times, want exactly 1 (only the original request)", got)
	}

	// A replay of the ORIGINAL payload must still return the original
	// stored success, proving the conflict path never poisoned the record.
	replay, err := coord.Do(context.Background(), first, effect)
	if err != nil {
		t.Fatalf("replay of original payload: unexpected error %v", err)
	}
	if replay.SideEffects.Effects != 1 {
		t.Fatalf("replay effects = %d, want 1 (stored, not re-executed)", replay.SideEffects.Effects)
	}
	if got := atomic.LoadInt32(&effectCount); got != 1 {
		t.Fatalf("effect ran %d times after replay, want still exactly 1", got)
	}
}
