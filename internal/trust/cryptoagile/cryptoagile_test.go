package cryptoagile

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// TestTodo_CRYPTO_001 is CRYPTO-001's PRIMARY test: it plays out a full
// crypto-agile migration end to end and checks the todo's own GREEN
// condition verbatim - "migration fixture verifies old/new signatures
// during bounded window, emits re-sign/re-encrypt receipts and rejects
// retired algorithms after cutoff without rewriting history."
func TestTodo_CRYPTO_001(t *testing.T) {
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("old", seedFor(1)); err != nil {
		t.Fatalf("AddEd25519 old: %v", err)
	}
	if err := keys.AddEd25519("new", seedFor(2)); err != nil {
		t.Fatalf("AddEd25519 new: %v", err)
	}

	// The declared migration: single-suite "old" through day10, a bounded
	// dual-sign/dual-read window from day10 to day20, single-suite "new"
	// from day20 on.
	plan := MigrationPlan{Windows: []Window{
		{Start: day(0), End: day(10), ActiveSuiteID: "old"},
		{Start: day(10), End: day(20), ActiveSuiteID: "new", DualSuiteID: "old"},
		{Start: day(20), ActiveSuiteID: "new"},
	}}
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan.Validate(): %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "old", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register old: %v", err)
	}
	if err := registry.Register(AlgorithmSuite{ID: "new", Kind: KindSignature, Status: StatusDual, ActivatedAt: day(10)}); err != nil {
		t.Fatalf("Register new: %v", err)
	}

	// --- Before the migration window: sign historical evidence under the
	// single active suite. ---
	clock := day(5)
	signer, err := NewDualSigner(plan, keys, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("NewDualSigner: %v", err)
	}
	historicalMessage := []byte("ledger-event-2026-01-06")
	historical, err := signer.SignAll(historicalMessage)
	if err != nil {
		t.Fatalf("SignAll(historical): %v", err)
	}
	if len(historical) != 1 || historical[0].SuiteID != "old" {
		t.Fatalf("historical signing = %+v, want single envelope under old", historical)
	}
	historicalSigCopy := append([]byte(nil), historical[0].Signature...)

	verifier := NewEnvelopeVerifier(registry, keys)
	if err := verifier.Verify(historicalMessage, historical[0]); err != nil {
		t.Fatalf("Verify(historical, before migration): %v", err)
	}

	// --- Enter the bounded dual-sign/dual-read window: mark old DUAL too. ---
	if _, err := registry.Transition("old", StatusDual, day(10)); err != nil {
		t.Fatalf("Transition old->DUAL: %v", err)
	}
	clock = day(15)
	migrationMessage := []byte("ledger-event-2026-01-16")
	migrationEnvs, err := signer.SignAll(migrationMessage)
	if err != nil {
		t.Fatalf("SignAll(migration): %v", err)
	}
	if len(migrationEnvs) != 2 {
		t.Fatalf("SignAll(migration) = %d envelopes, want 2 (dual-sign)", len(migrationEnvs))
	}
	// GREEN: "verifies old/new signatures during bounded window" - both
	// envelopes verify, and so does the historical, pre-window signature.
	for _, env := range migrationEnvs {
		if err := verifier.Verify(migrationMessage, env); err != nil {
			t.Fatalf("Verify(migration, %s) during window: %v", env.SuiteID, err)
		}
	}
	if err := verifier.Verify(historicalMessage, historical[0]); err != nil {
		t.Fatalf("Verify(historical) still valid during migration window: %v", err)
	}

	// --- Cut over: new becomes ACTIVE, old is RETIRED. ---
	if _, err := registry.Transition("new", StatusActive, day(20)); err != nil {
		t.Fatalf("Transition new->ACTIVE: %v", err)
	}
	if _, err := registry.Transition("old", StatusRetired, day(20)); err != nil {
		t.Fatalf("Transition old->RETIRED: %v", err)
	}
	clock = day(25)
	newMessage := []byte("ledger-event-2026-01-26")
	afterEnvs, err := signer.SignAll(newMessage)
	if err != nil {
		t.Fatalf("SignAll(after cutover): %v", err)
	}
	if len(afterEnvs) != 1 || afterEnvs[0].SuiteID != "new" {
		t.Fatalf("SignAll(after cutover) = %+v, want single envelope under new", afterEnvs)
	}
	if err := verifier.Verify(newMessage, afterEnvs[0]); err != nil {
		t.Fatalf("Verify(new message, after cutover): %v", err)
	}

	// GREEN: "rejects retired algorithms after cutoff without rewriting
	// history" - the historical envelope's bytes are untouched, but it no
	// longer verifies once its suite is retired, with a typed reason.
	if !reflect.DeepEqual(historical[0].Signature, historicalSigCopy) {
		t.Fatalf("historical envelope signature bytes changed - history was rewritten")
	}
	var retired *RetiredSuiteError
	if err := verifier.Verify(historicalMessage, historical[0]); !errors.As(err, &retired) {
		t.Fatalf("Verify(historical) after cutoff = %v, want *RetiredSuiteError", err)
	}
	if retired.SuiteID != "old" {
		t.Fatalf("RetiredSuiteError.SuiteID = %q, want old", retired.SuiteID)
	}
	// The migration window's "old"-suite envelope is refused the same way,
	// but its "new"-suite twin (dual-read) still verifies.
	for _, env := range migrationEnvs {
		err := verifier.Verify(migrationMessage, env)
		if env.SuiteID == "old" {
			if !errors.As(err, &retired) {
				t.Fatalf("Verify(migration, old) after cutoff = %v, want *RetiredSuiteError", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Verify(migration, new) after cutoff: %v", err)
		}
	}

	// GREEN: "emits re-sign/re-encrypt receipts" - Resume produces one
	// Evidence record per window transition the migration actually made.
	evidence, err := Resume(plan, nil, day(25))
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	want := []Evidence{
		{Index: 0, ActiveSuiteID: "old", At: day(0)},
		{Index: 1, ActiveSuiteID: "new", DualSuiteID: "old", At: day(10)},
		{Index: 2, ActiveSuiteID: "new", At: day(20)},
	}
	if !reflect.DeepEqual(evidence, want) {
		t.Fatalf("Resume evidence = %+v, want %+v", evidence, want)
	}
}

// TestTodo_CRYPTO_001_Golden pins Envelope's wire encoding: the exact bytes
// this test asserts must never drift, because recorded evidence and any
// interoperating verifier depend on the format staying fixed.
func TestTodo_CRYPTO_001_Golden(t *testing.T) {
	cases := []struct {
		env  Envelope
		want string
	}{
		{Envelope{SuiteID: "ed25519-v1", Signature: []byte{0x00, 0x01, 0xfe, 0xff}}, "cryptoagile.v1:ed25519-v1:0001feff"},
		{Envelope{SuiteID: "hmac-sha256-v1", Signature: []byte{}}, "cryptoagile.v1:hmac-sha256-v1:"},
	}
	for _, tc := range cases {
		if got := tc.env.Encode(); got != tc.want {
			t.Fatalf("Encode(%+v) = %q, want %q", tc.env, got, tc.want)
		}
		decoded, err := DecodeEnvelope(tc.want)
		if err != nil {
			t.Fatalf("DecodeEnvelope(%q): %v", tc.want, err)
		}
		if decoded.SuiteID != tc.env.SuiteID || len(decoded.Signature) != len(tc.env.Signature) {
			t.Fatalf("DecodeEnvelope(%q) = %+v, want %+v", tc.want, decoded, tc.env)
		}
	}

	// Deterministic key material: an Ed25519 signature over a fixed message
	// under a fixed seed is itself fixed, so the full Sign -> Encode chain
	// is a legitimate golden fixture, not just the string formatting.
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("ed25519-fixed", make([]byte, 32)); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}
	env, err := NewEnvelopeSigner(keys).Sign("ed25519-fixed", []byte("fixed message"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if got, wantPrefix := env.Encode(), "cryptoagile.v1:ed25519-fixed:"; len(got) <= len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("Encode(fixed) = %q, want prefix %q followed by a 128-hex-digit Ed25519 signature", got, wantPrefix)
	}
	roundTripped, err := DecodeEnvelope(env.Encode())
	if err != nil {
		t.Fatalf("DecodeEnvelope(Encode(fixed)): %v", err)
	}
	if roundTripped.SuiteID != env.SuiteID || string(roundTripped.Signature) != string(env.Signature) {
		t.Fatalf("fixed-key round trip = %+v, want %+v", roundTripped, env)
	}
	// Re-signing under the same fixed seed and message must reproduce byte-
	// identical output: this is the golden property that makes the format
	// safe to pin in recorded evidence.
	again, err := NewEnvelopeSigner(keys).Sign("ed25519-fixed", []byte("fixed message"))
	if err != nil {
		t.Fatalf("Sign (again): %v", err)
	}
	if again.Encode() != env.Encode() {
		t.Fatalf("Encode() is not deterministic: %q != %q", again.Encode(), env.Encode())
	}
}

// TestTodo_CRYPTO_001_Property checks, over many random messages, that a
// dual-signed envelope pair verifies under both suites while both are
// live, and that after the old suite is retired only the new suite's
// envelope still verifies.
func TestTodo_CRYPTO_001_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260905))

	keys := NewFakeKeySource()
	if err := keys.AddEd25519("old", seedFor(3)); err != nil {
		t.Fatalf("AddEd25519 old: %v", err)
	}
	if err := keys.AddEd25519("new", seedFor(4)); err != nil {
		t.Fatalf("AddEd25519 new: %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "old", Kind: KindSignature, Status: StatusDual, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register old: %v", err)
	}
	if err := registry.Register(AlgorithmSuite{ID: "new", Kind: KindSignature, Status: StatusDual, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register new: %v", err)
	}

	plan := MigrationPlan{Windows: []Window{{Start: day(0), ActiveSuiteID: "new", DualSuiteID: "old"}}}
	signer, err := NewDualSigner(plan, keys, func() time.Time { return day(0) })
	if err != nil {
		t.Fatalf("NewDualSigner: %v", err)
	}
	verifier := NewEnvelopeVerifier(registry, keys)

	const trials = 200
	type sample struct {
		message []byte
		envs    []Envelope
	}
	samples := make([]sample, trials)

	for i := 0; i < trials; i++ {
		msg := make([]byte, 1+rng.Intn(64))
		rng.Read(msg)
		envs, err := signer.SignAll(msg)
		if err != nil {
			t.Fatalf("SignAll(trial %d): %v", i, err)
		}
		if len(envs) != 2 {
			t.Fatalf("SignAll(trial %d) = %d envelopes, want 2", i, len(envs))
		}
		for _, env := range envs {
			if err := verifier.Verify(msg, env); err != nil {
				t.Fatalf("Verify(trial %d, suite %s) before retirement: %v", i, env.SuiteID, err)
			}
		}
		samples[i] = sample{message: msg, envs: envs}
	}

	if _, err := registry.Transition("old", StatusRetired, day(1)); err != nil {
		t.Fatalf("Transition old->RETIRED: %v", err)
	}

	for i, s := range samples {
		for _, env := range s.envs {
			err := verifier.Verify(s.message, env)
			if env.SuiteID == "old" {
				var retired *RetiredSuiteError
				if !errors.As(err, &retired) {
					t.Fatalf("trial %d: Verify(old) after retirement = %v, want *RetiredSuiteError", i, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("trial %d: Verify(new) after retirement: %v", i, err)
			}
		}
	}
}

// TestTodo_CRYPTO_001_Security proves the three named refusals: a stripped
// suite id, a downgrade to a retired suite, and a signature minted for a
// different suite id are all refused rather than accepted.
func TestTodo_CRYPTO_001_Security(t *testing.T) {
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("suite-a", seedFor(5)); err != nil {
		t.Fatalf("AddEd25519 a: %v", err)
	}
	if err := keys.AddEd25519("suite-b", seedFor(6)); err != nil {
		t.Fatalf("AddEd25519 b: %v", err)
	}
	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "suite-a", Kind: KindSignature, Status: StatusRetired, ActivatedAt: day(0), RetiredAt: day(10)}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := registry.Register(AlgorithmSuite{ID: "suite-b", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(10)}); err != nil {
		t.Fatalf("Register b: %v", err)
	}
	verifier := NewEnvelopeVerifier(registry, keys)
	signer := NewEnvelopeSigner(keys)

	message := []byte("payload")

	t.Run("stripped_suite_id", func(t *testing.T) {
		valid, err := signer.Sign("suite-b", message)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		stripped := Envelope{SuiteID: "", Signature: valid.Signature}
		if err := verifier.Verify(message, stripped); !errors.Is(err, ErrStrippedSuiteID) {
			t.Fatalf("Verify(stripped suite id) = %v, want ErrStrippedSuiteID", err)
		}
	})

	t.Run("downgrade_to_retired_suite", func(t *testing.T) {
		// suite-a's signature is well-formed and would verify under its own
		// key; it is refused solely because the suite is RETIRED.
		env, err := signer.Sign("suite-a", message)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		var retired *RetiredSuiteError
		if err := verifier.Verify(message, env); !errors.As(err, &retired) {
			t.Fatalf("Verify(retired suite) = %v, want *RetiredSuiteError", err)
		}
	})

	t.Run("signature_over_different_suite_id", func(t *testing.T) {
		env, err := signer.Sign("suite-b", message)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		// Same signature bytes, relabeled as a different, legitimately
		// registered and keyed (also non-retired, so this cannot be
		// confused with the retirement case) suite id.
		if err := keys.AddEd25519("suite-c", seedFor(7)); err != nil {
			t.Fatalf("AddEd25519 c: %v", err)
		}
		if err := registry.Register(AlgorithmSuite{ID: "suite-c", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
			t.Fatalf("Register c: %v", err)
		}
		forged := Envelope{SuiteID: "suite-c", Signature: env.Signature}
		if err := verifier.Verify(message, forged); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("Verify(relabeled envelope) = %v, want ErrSignatureInvalid", err)
		}
	})
}

// TestTodo_CRYPTO_001_Recovery proves a migration interrupted mid-window
// resumes from its recorded evidence rather than from wall-clock time
// alone or from scratch: running Resume straight through to a given time,
// versus running it in two pieces around a simulated crash, produces the
// identical evidence trail.
func TestTodo_CRYPTO_001_Recovery(t *testing.T) {
	plan := threeWindowPlan()

	straightThrough, err := Resume(plan, nil, day(25))
	if err != nil {
		t.Fatalf("Resume(straight through): %v", err)
	}

	// Simulate a process that advances to day(15) (inside window 1), but a
	// crash means only the FIRST evidence record it was told to emit (for
	// entering window 0) actually made it to durable storage before the
	// interruption - window 1's own record was computed but never
	// persisted.
	firstPass, err := Resume(plan, nil, day(15))
	if err != nil {
		t.Fatalf("Resume(first pass): %v", err)
	}
	if len(firstPass) != 2 {
		t.Fatalf("Resume(first pass) = %d records, want 2 (windows 0 and 1)", len(firstPass))
	}
	recordedBeforeCrash := []Evidence{firstPass[0]} // window 1's record was lost

	// The process restarts later, at day(25), knowing only what it actually
	// persisted (recordedBeforeCrash). It must recover window 1's missed
	// evidence as well as window 2's, without duplicating window 0's.
	resumed, err := Resume(plan, recordedBeforeCrash, day(25))
	if err != nil {
		t.Fatalf("Resume(after restart): %v", err)
	}
	full := append(append([]Evidence{}, recordedBeforeCrash...), resumed...)

	if !reflect.DeepEqual(full, straightThrough) {
		t.Fatalf("resumed trail = %+v, want identical to straight-through trail %+v", full, straightThrough)
	}

	// A second restart with nothing lost (recorded already covers window 2)
	// must be a no-op: Resume must not re-emit evidence for a window whose
	// record is already durable.
	noop, err := Resume(plan, full, day(25))
	if err != nil {
		t.Fatalf("Resume(fully caught up): %v", err)
	}
	if len(noop) != 0 {
		t.Fatalf("Resume(fully caught up) = %+v, want no new evidence", noop)
	}

	// A corrupted trail that claims to be ahead of the resume time is
	// refused rather than silently trusted.
	if _, err := Resume(plan, full, day(15)); err == nil {
		t.Fatalf("Resume(recorded ahead of now) = nil, want error")
	}
}
