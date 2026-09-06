package lease_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/lease"
)

// fakePort is a minimal, deterministic [lease.Port]: it issues a custody
// lease that echoes the requested handle/operation/ttl exactly, unless
// widen is set, in which case it returns a lease for a different operation
// to exercise Mint's defence-in-depth check.
type fakePort struct {
	now    func() time.Time
	widen  bool
	nextID int
}

func (f *fakePort) IssueLease(_ custody.Context, h custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	f.nextID++
	returnedOp := op
	if f.widen {
		returnedOp = custody.Sign
		if op == custody.Sign {
			returnedOp = custody.Encrypt
		}
	}
	return custody.Lease{
		ID:        "custody-lease-" + itoa(f.nextID),
		Handle:    h,
		Operation: returnedOp,
		ExpiresAt: f.now().Add(ttl),
	}, nil
}

func itoa(n int) string {
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}
	return string(out)
}

func testHandle() custody.Handle {
	return custody.Handle{ID: "conn-1", Kind: custody.Secret, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
}

func testRequest() lease.Request {
	return lease.Request{
		Handle:      testHandle(),
		Workload:    "worker-a",
		Tenant:      "tenant-a",
		Purpose:     "payroll-file-delivery",
		Destination: "sftp.bank.example",
		Operation:   custody.Encrypt,
		TTL:         time.Minute,
	}
}

func newManager(t *testing.T, clock func() time.Time) (*lease.Manager, *fakePort) {
	t.Helper()
	port := &fakePort{now: clock}
	mgr, err := lease.NewManager(port, clock)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr, port
}

// TestTodo_TRUST_016 is the PRIMARY test: a lease minted for one destination
// dispatches successfully there and exactly once; an expired lease, a
// revoked lease, a lease presented to the wrong destination, a lease used
// for the wrong operation and a duplicate presentation of an already-used
// lease are all refused; and Mint itself refuses an over-broad (unscoped or
// administrative-operation) request before it ever reaches the custody port.
func TestTodo_TRUST_016(t *testing.T) {
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	t.Run("a freshly minted lease dispatches once to its bound destination and operation", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, mintEv, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if mintEv.Kind != lease.EventMint || mintEv.Outcome != "granted" {
			t.Fatalf("mint evidence = %+v, want granted mint", mintEv)
		}
		useEv, err := mgr.Use(cl, cl.Destination, cl.Operation)
		if err != nil {
			t.Fatalf("Use: %v", err)
		}
		if useEv.Kind != lease.EventUse || useEv.Outcome != "granted" {
			t.Fatalf("use evidence = %+v, want granted use", useEv)
		}
	})

	t.Run("a used lease cannot dispatch a second time", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); err != nil {
			t.Fatalf("first Use: %v", err)
		}
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); !errors.Is(err, lease.ErrAlreadyUsed) {
			t.Fatalf("second Use err = %v, want ErrAlreadyUsed", err)
		}
	})

	t.Run("an expired lease cannot dispatch an external write", func(t *testing.T) {
		var current time.Time = fixed
		clockVar := func() time.Time { return current }
		mgr, _ := newManager(t, clockVar)
		req := testRequest()
		req.TTL = time.Second
		cl, _, err := mgr.Mint(req)
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		current = current.Add(2 * time.Second)
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); !errors.Is(err, lease.ErrExpired) {
			t.Fatalf("Use err = %v, want ErrExpired", err)
		}
	})

	t.Run("a revoked lease cannot dispatch an external write", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		revokeEv, err := mgr.Revoke(cl.ID, "connector decommissioned")
		if err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if revokeEv.Kind != lease.EventRevoke {
			t.Fatalf("revoke evidence kind = %q, want revoke", revokeEv.Kind)
		}
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); !errors.Is(err, lease.ErrRevoked) {
			t.Fatalf("Use err = %v, want ErrRevoked", err)
		}
	})

	t.Run("a lease presented to the wrong destination cannot dispatch", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if _, err := mgr.Use(cl, "sftp.attacker.example", cl.Operation); !errors.Is(err, lease.ErrDestinationMismatch) {
			t.Fatalf("Use err = %v, want ErrDestinationMismatch", err)
		}
	})

	t.Run("a lease used for the wrong operation is refused as an over-broad use", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if _, err := mgr.Use(cl, cl.Destination, custody.Decrypt); !errors.Is(err, lease.ErrOperationNotPermitted) {
			t.Fatalf("Use err = %v, want ErrOperationNotPermitted", err)
		}
	})

	t.Run("Mint refuses an unscoped operation", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		req := testRequest()
		req.Operation = ""
		if _, _, err := mgr.Mint(req); !errors.Is(err, lease.ErrInvalidRequest) {
			t.Fatalf("Mint err = %v, want ErrInvalidRequest", err)
		}
	})

	t.Run("Mint refuses an administrative operation as over-broad", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		req := testRequest()
		req.Operation = custody.Rotate
		if _, _, err := mgr.Mint(req); !errors.Is(err, lease.ErrInvalidRequest) {
			t.Fatalf("Mint err = %v, want ErrInvalidRequest", err)
		}
	})
}

// FuzzTodo_TRUST_016 is the FUZZ matrix test: Use, on arbitrary destination
// and operation strings against a lease that really was minted, only ever
// succeeds when both exactly equal what was minted, and never panics.
func FuzzTodo_TRUST_016(f *testing.F) {
	f.Add("sftp.bank.example", "encrypt")
	f.Add("sftp.attacker.example", "encrypt")
	f.Add("sftp.bank.example", "decrypt")
	f.Add("", "")
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }
	f.Fuzz(func(t *testing.T, destination, operation string) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		_, err = mgr.Use(cl, destination, custody.Operation(operation))
		succeeded := err == nil
		wantSucceed := destination == cl.Destination && custody.Operation(operation) == cl.Operation
		if succeeded != wantSucceed {
			t.Fatalf("Use(%q,%q) succeeded=%v, want %v (lease destination=%q operation=%q)",
				destination, operation, succeeded, wantSucceed, cl.Destination, cl.Operation)
		}
	})
}

// TestTodo_TRUST_016_Race proves single-use consumption is safe under
// concurrent presentation of the identical lease: exactly one of many
// simultaneous Use calls succeeds, the rest see ErrAlreadyUsed, and exactly
// one granted use [Evidence] record is ever appended.
func TestTodo_TRUST_016_Race(t *testing.T) {
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }
	mgr, _ := newManager(t, clock)
	cl, _, err := mgr.Mint(testRequest())
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	const workers = 50
	var wg sync.WaitGroup
	var successes int
	var mu sync.Mutex
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := mgr.Use(cl, cl.Destination, cl.Operation); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1", successes)
	}
	granted := 0
	for _, ev := range mgr.Events() {
		if ev.Kind == lease.EventUse && ev.Outcome == "granted" {
			granted++
		}
	}
	if granted != 1 {
		t.Fatalf("granted use evidence records = %d, want exactly 1", granted)
	}
}

// TestTodo_TRUST_016_Integration exercises the full mint -> use -> revoke
// lifecycle end to end through a fake custody [lease.Port], including the
// defence-in-depth check that refuses a lease a misbehaving provider tried
// to widen to a different operation than what was requested.
func TestTodo_TRUST_016_Integration(t *testing.T) {
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	mgr, port := newManager(t, clock)
	cl, mintEv, err := mgr.Mint(testRequest())
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if cl.CustodyLeaseID == "" {
		t.Fatal("CredentialLease has no underlying custody lease id")
	}
	if mintEv.LeaseID != cl.ID {
		t.Fatalf("mint evidence lease id = %q, want %q", mintEv.LeaseID, cl.ID)
	}

	if _, err := mgr.Use(cl, cl.Destination, cl.Operation); err != nil {
		t.Fatalf("Use: %v", err)
	}
	if _, err := mgr.Revoke(cl.ID, "lifecycle complete"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	// Revoke is idempotent.
	if _, err := mgr.Revoke(cl.ID, "lifecycle complete"); err != nil {
		t.Fatalf("second Revoke: %v", err)
	}

	events := mgr.Events()
	var kinds []lease.EventKind
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) < 4 {
		t.Fatalf("events = %v, want at least mint/use/revoke/revoke", kinds)
	}

	port.widen = true
	if _, _, err := mgr.Mint(testRequest()); !errors.Is(err, lease.ErrInvalidLease) {
		t.Fatalf("Mint against a widening provider: err = %v, want ErrInvalidLease", err)
	}
}

// TestTodo_TRUST_016_Security proves the evaluator fails closed against
// adversarial presentation: an unknown lease id is refused, a lease whose
// caller-held copy was tampered with (a different destination stamped onto
// an otherwise-identical struct) is refused as tampered rather than
// evaluated against the tampered value, and every denial - not only every
// grant - is durably recorded as evidence.
func TestTodo_TRUST_016_Security(t *testing.T) {
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	t.Run("an unknown lease id is refused", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		forged := lease.CredentialLease{ID: "clx-forged", Destination: "sftp.bank.example", Operation: custody.Encrypt}
		if _, err := mgr.Use(forged, "sftp.bank.example", custody.Encrypt); !errors.Is(err, lease.ErrUnknown) {
			t.Fatalf("Use err = %v, want ErrUnknown", err)
		}
	})

	t.Run("a tampered copy of a real lease is refused even when the id is genuine", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		tampered := cl
		tampered.Destination = "sftp.attacker.example"
		if _, err := mgr.Use(tampered, "sftp.attacker.example", cl.Operation); !errors.Is(err, lease.ErrTampered) {
			t.Fatalf("Use err = %v, want ErrTampered", err)
		}
		// The genuine lease must still be usable: tampering with a copy did
		// not consume or corrupt the stored record.
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); err != nil {
			t.Fatalf("genuine lease Use after a tampered attempt: %v", err)
		}
	})

	t.Run("a wrong nonce alone is enough to refuse, even with every other field correct", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		guessed := cl
		guessed.Nonce = "0000000000000000000000000000000"
		if _, err := mgr.Use(guessed, cl.Destination, cl.Operation); !errors.Is(err, lease.ErrTampered) {
			t.Fatalf("Use err = %v, want ErrTampered", err)
		}
	})

	t.Run("every denial is recorded as evidence, not only grants", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if _, err := mgr.Use(cl, "sftp.attacker.example", cl.Operation); !errors.Is(err, lease.ErrDestinationMismatch) {
			t.Fatalf("Use: %v", err)
		}
		found := false
		for _, ev := range mgr.Events() {
			if ev.Kind == lease.EventUse && ev.Outcome == "denied" && ev.Reason == "destination_mismatch" {
				found = true
			}
		}
		if !found {
			t.Fatal("no denied use evidence with reason destination_mismatch was recorded")
		}
	})
}

// TestTodo_TRUST_016_Mutation proves every refusal branch in Use is
// load-bearing: a scenario engineered to trip exactly one check, with every
// other check satisfied, must be refused with that check's specific error -
// a mutant that deletes or short-circuits one branch would let at least one
// of these through as a grant.
func TestTodo_TRUST_016_Mutation(t *testing.T) {
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	cases := []struct {
		name    string
		mutate  func(cl *lease.CredentialLease)
		useDest string
		useOp   custody.Operation
		wantErr error
	}{
		{
			name:    "destination check",
			mutate:  func(*lease.CredentialLease) {},
			useDest: "sftp.attacker.example",
			wantErr: lease.ErrDestinationMismatch,
		},
		{
			name:    "operation check",
			mutate:  func(*lease.CredentialLease) {},
			useOp:   custody.Decrypt,
			wantErr: lease.ErrOperationNotPermitted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _ := newManager(t, clock)
			cl, _, err := mgr.Mint(testRequest())
			if err != nil {
				t.Fatalf("Mint: %v", err)
			}
			tc.mutate(&cl)
			dest := tc.useDest
			if dest == "" {
				dest = cl.Destination
			}
			op := tc.useOp
			if op == "" {
				op = cl.Operation
			}
			if _, err := mgr.Use(cl, dest, op); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Use err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	t.Run("revoked-before-expired check order still refuses an expired, revoked lease as revoked or expired, never granted", func(t *testing.T) {
		var current = fixed
		clockVar := func() time.Time { return current }
		mgr, _ := newManager(t, clockVar)
		req := testRequest()
		req.TTL = time.Second
		cl, _, err := mgr.Mint(req)
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if _, err := mgr.Revoke(cl.ID, "test"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		current = current.Add(2 * time.Second)
		_, err = mgr.Use(cl, cl.Destination, cl.Operation)
		if !errors.Is(err, lease.ErrRevoked) && !errors.Is(err, lease.ErrExpired) {
			t.Fatalf("Use err = %v, want ErrRevoked or ErrExpired, never a grant", err)
		}
	})

	t.Run("a used-then-revoked lease is still refused, never silently reusable", func(t *testing.T) {
		mgr, _ := newManager(t, clock)
		cl, _, err := mgr.Mint(testRequest())
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); err != nil {
			t.Fatalf("first Use: %v", err)
		}
		if _, err := mgr.Revoke(cl.ID, "cleanup"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if _, err := mgr.Use(cl, cl.Destination, cl.Operation); err == nil {
			t.Fatal("Use after used-then-revoked succeeded, want a refusal")
		}
	})
}

type invalidLeasePort struct {
	err error
}

func (p invalidLeasePort) IssueLease(_ custody.Context, _ custody.Handle, _ custody.Operation, _ time.Duration) (custody.Lease, error) {
	if p.err != nil {
		return custody.Lease{}, p.err
	}
	return custody.Lease{}, nil
}

func TestLease_PublicAPIs_RequestBoundariesAndCopies(t *testing.T) {
	fixed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if _, err := lease.NewManager(nil, nil); !errors.Is(err, lease.ErrInvalidRequest) {
		t.Fatalf("nil port err=%v", err)
	}
	defaultManager, err := lease.NewManager(&fakePort{now: time.Now}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := defaultManager.Mint(testRequest()); err != nil {
		t.Fatalf("default clock manager Mint: %v", err)
	}
	if got := (lease.Evidence{Kind: lease.EventUse, LeaseID: "clx", Destination: "destination", Operation: custody.Encrypt, Outcome: "denied", Reason: "tampered"}).String(); got == "" || !strings.Contains(got, "tampered") {
		t.Fatalf("Evidence.String=%q", got)
	}

	cases := []struct {
		name   string
		mutate func(*lease.Request)
	}{
		{"invalid handle", func(r *lease.Request) { r.Handle = custody.Handle{} }},
		{"workload padded", func(r *lease.Request) { r.Workload = " workload" }},
		{"tenant padded", func(r *lease.Request) { r.Tenant = " tenant-a" }},
		{"purpose empty", func(r *lease.Request) { r.Purpose = "" }},
		{"destination empty", func(r *lease.Request) { r.Destination = "" }},
		{"tenant mismatch", func(r *lease.Request) { r.Tenant = "tenant-b" }},
		{"operation empty", func(r *lease.Request) { r.Operation = "" }},
		{"rotate forbidden", func(r *lease.Request) { r.Operation = custody.Rotate }},
		{"revoke forbidden", func(r *lease.Request) { r.Operation = custody.Revoke }},
		{"ttl zero", func(r *lease.Request) { r.TTL = 0 }},
		{"ttl negative", func(r *lease.Request) { r.TTL = -time.Second }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _ := newManager(t, func() time.Time { return fixed })
			req := testRequest()
			tc.mutate(&req)
			if _, _, err := mgr.Mint(req); err == nil || !errors.Is(err, lease.ErrInvalidRequest) {
				t.Fatalf("Mint err=%v, want ErrInvalidRequest", err)
			}
		})
	}
	providerErr := errors.New("provider unavailable")
	mgr, err := lease.NewManager(invalidLeasePort{err: providerErr}, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.Mint(testRequest()); !errors.Is(err, providerErr) {
		t.Fatalf("provider error=%v", err)
	}
	badLeaseManager, err := lease.NewManager(invalidLeasePort{}, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := badLeaseManager.Mint(testRequest()); err == nil {
		t.Fatal("invalid custody lease was accepted")
	}

	if _, err := mgr.Revoke("unknown", "cleanup"); !errors.Is(err, lease.ErrUnknown) {
		t.Fatalf("unknown revoke err=%v", err)
	}
	working, _ := newManager(t, func() time.Time { return fixed })
	cl, _, err := working.Mint(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := working.Revoke(cl.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := working.Revoke(cl.ID, "second"); err != nil {
		t.Fatal(err)
	}
	events := working.Events()
	if len(events) != 3 {
		t.Fatalf("event count=%d, want mint+2 revoke", len(events))
	}
	events[0].Reason = "mutated"
	if working.Events()[0].Reason == "mutated" {
		t.Fatal("Events did not return a copy")
	}
}
