package cryptoagile

import (
	"errors"
	"testing"
	"time"
)

func TestAlgorithmSuite_Validate(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	cases := []struct {
		name    string
		suite   AlgorithmSuite
		wantErr error
	}{
		{"missing id", AlgorithmSuite{Kind: KindSignature, Status: StatusActive, ActivatedAt: t0}, ErrSuiteID},
		{"bad kind", AlgorithmSuite{ID: "s1", Kind: "rot13", Status: StatusActive, ActivatedAt: t0}, ErrSuiteKind},
		{"bad status", AlgorithmSuite{ID: "s1", Kind: KindSignature, Status: "PENDING", ActivatedAt: t0}, ErrSuiteStatus},
		{"active without activation", AlgorithmSuite{ID: "s1", Kind: KindSignature, Status: StatusActive}, ErrSuiteActivation},
		{"dual without activation", AlgorithmSuite{ID: "s1", Kind: KindMAC, Status: StatusDual}, ErrSuiteActivation},
		{"retired without retirement", AlgorithmSuite{ID: "s1", Kind: KindSignature, Status: StatusRetired, ActivatedAt: t0}, ErrSuiteRetirement},
		{"retired before activation", AlgorithmSuite{ID: "s1", Kind: KindSignature, Status: StatusRetired, ActivatedAt: t1, RetiredAt: t0}, ErrSuiteRetirement},
		{"valid active", AlgorithmSuite{ID: "s1", Kind: KindSignature, Status: StatusActive, ActivatedAt: t0}, nil},
		{"valid retired", AlgorithmSuite{ID: "s1", Kind: KindSignature, Status: StatusRetired, ActivatedAt: t0, RetiredAt: t1}, nil},
		{"digest kind", AlgorithmSuite{ID: "sha256", Kind: KindDigest, Status: StatusActive, ActivatedAt: t0}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.suite.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewRegistry()

	if err := r.Register(AlgorithmSuite{ID: "hmac-v1", Kind: KindMAC, Status: StatusActive, ActivatedAt: t0}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("hmac-v1")
	if !ok || got.ID != "hmac-v1" {
		t.Fatalf("Get(hmac-v1) = %+v, %v", got, ok)
	}

	if _, ok := r.Get("nope"); ok {
		t.Fatalf("Get(nope) reported ok=true for an unregistered suite")
	}

	if err := r.Register(AlgorithmSuite{ID: "hmac-v1", Kind: KindMAC, Status: StatusActive, ActivatedAt: t0}); !errors.Is(err, ErrDuplicateSuite) {
		t.Fatalf("Register duplicate = %v, want ErrDuplicateSuite", err)
	}

	if err := r.Register(AlgorithmSuite{ID: "", Kind: KindMAC, Status: StatusActive, ActivatedAt: t0}); !errors.Is(err, ErrSuiteID) {
		t.Fatalf("Register invalid = %v, want ErrSuiteID", err)
	}
}

func TestRegistry_Transition(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(30 * 24 * time.Hour)
	r := NewRegistry()
	if err := r.Register(AlgorithmSuite{ID: "a", Kind: KindSignature, Status: StatusActive, ActivatedAt: t0}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	prev, err := r.Transition("a", StatusRetired, t1)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if prev.Status != StatusActive {
		t.Fatalf("Transition previous status = %v, want ACTIVE", prev.Status)
	}

	now, ok := r.Get("a")
	if !ok {
		t.Fatalf("Get(a) after transition: not found")
	}
	if now.Status != StatusRetired || !now.RetiredAt.Equal(t1) {
		t.Fatalf("Get(a) after transition = %+v", now)
	}
	if !r.IsRetired("a") {
		t.Fatalf("IsRetired(a) = false after retiring it")
	}
	if r.IsRetired("unknown") {
		t.Fatalf("IsRetired(unknown) = true for an unregistered suite")
	}

	if _, err := r.Transition("missing", StatusActive, t0); !errors.Is(err, ErrUnknownSuite) {
		t.Fatalf("Transition(missing) = %v, want ErrUnknownSuite", err)
	}
}

// TestRegistry_TransitionRefusesInvalidResult proves Transition validates
// its own result: retiring a suite at an instant at-or-before its
// activation is refused rather than silently recorded.
func TestRegistry_TransitionRefusesInvalidResult(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewRegistry()
	if err := r.Register(AlgorithmSuite{ID: "a", Kind: KindSignature, Status: StatusActive, ActivatedAt: t0}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Transition("a", StatusRetired, t0.Add(-time.Hour)); !errors.Is(err, ErrSuiteRetirement) {
		t.Fatalf("Transition backwards retirement = %v, want ErrSuiteRetirement", err)
	}
	// The registry must still show the pre-transition state.
	got, _ := r.Get("a")
	if got.Status != StatusActive {
		t.Fatalf("Get(a) after refused transition = %+v, want unchanged ACTIVE", got)
	}
}
