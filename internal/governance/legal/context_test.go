package legal

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// --- shared test fixtures -------------------------------------------------

func testCAJurisdiction() Jurisdiction { return Jurisdiction{Country: "US", State: "CA"} }
func testNYJurisdiction() Jurisdiction { return Jurisdiction{Country: "US", State: "NY"} }
func testTXJurisdiction() Jurisdiction { return Jurisdiction{Country: "US", State: "TX"} }

func mustDate(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("NewLocalDate(%d,%d,%d): %v", year, month, day, err)
	}
	return d
}

func mustInstant(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	i, err := values.NewInstantFromUnix(unixSec, 0)
	if err != nil {
		t.Fatalf("NewInstantFromUnix(%d): %v", unixSec, err)
	}
	return i
}

func mustKnownAt(t *testing.T, unixSec int64) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(mustInstant(t, unixSec))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return k
}

// fixedSigner returns a deterministic ed25519 signer for reproducible golden
// tests: the seed is 32 fixed bytes, never crypto/rand.
func fixedSigner(t *testing.T, seedByte byte) *Signer {
	t.Helper()
	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	signer, err := NewSigner(priv)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return signer
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry()
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := reg.Register(ca); err != nil {
		t.Fatalf("Register(CA): %v", err)
	}
	ny, err := NewYorkPromotionPack()
	if err != nil {
		t.Fatalf("NewYorkPromotionPack: %v", err)
	}
	if err := reg.Register(ny); err != nil {
		t.Fatalf("Register(NY): %v", err)
	}
	return reg
}

// validInput returns a complete, resolvable input for the given jurisdiction,
// non-remote, effective 2026-03-01, known at a fixed instant.
func validInput(t *testing.T, j Jurisdiction) LegalContextInput {
	t.Helper()
	return LegalContextInput{
		LegalEntityID:          "legal-entity-1",
		WorkLocation:           j,
		EmploymentJurisdiction: j,
		EffectiveDate:          mustDate(t, 2026, time.March, 1),
		KnownAt:                mustKnownAt(t, 1_770_000_000),
	}
}

// --- TestTodo_LEGAL_001 (PRIMARY): RED and GREEN behavior ------------------

func TestTodo_LEGAL_001(t *testing.T) {
	registry := testRegistry(t)
	signer := fixedSigner(t, 0x01)
	now := mustInstant(t, 1_770_100_000)

	t.Run("RED", func(t *testing.T) {
		base := validInput(t, testCAJurisdiction())

		cases := []struct {
			name    string
			mutate  func(LegalContextInput) LegalContextInput
			wantErr error
		}{
			{
				name: "missing legal entity",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.LegalEntityID = ""
					return in
				},
			},
			{
				name: "missing work location",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.WorkLocation = Jurisdiction{}
					return in
				},
			},
			{
				name: "missing employment jurisdiction",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.EmploymentJurisdiction = Jurisdiction{}
					return in
				},
			},
			{
				name: "missing effective time",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.EffectiveDate = values.LocalDate{}
					return in
				},
			},
			{
				name: "missing known time",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.KnownAt = values.KnownAt{}
					return in
				},
			},
			{
				name: "locale alone never supplies jurisdiction",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.Locale = "en-US"
					in.WorkLocation = Jurisdiction{}
					in.EmploymentJurisdiction = Jurisdiction{}
					return in
				},
			},
			{
				name: "on-site work location disagrees with employment jurisdiction",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.EmploymentJurisdiction = testNYJurisdiction()
					return in
				},
			},
			{
				name: "missing rule release for the resolved jurisdiction",
				mutate: func(in LegalContextInput) LegalContextInput {
					in.WorkLocation = testTXJurisdiction()
					in.EmploymentJurisdiction = testTXJurisdiction()
					return in
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ctx, err := Resolve(tc.mutate(base), registry, signer, now)
				if err == nil {
					t.Fatalf("Resolve succeeded, want LEGAL_CONTEXT_UNKNOWN; got context %+v", ctx)
				}
				if !errors.Is(err, ErrLegalContextUnknown) {
					t.Fatalf("error %v does not wrap ErrLegalContextUnknown", err)
				}
				if ctx != nil {
					t.Fatalf("Resolve returned a non-nil context alongside an error")
				}
			})
		}

		t.Run("nil registry", func(t *testing.T) {
			if _, err := Resolve(base, nil, signer, now); !errors.Is(err, ErrLegalContextUnknown) {
				t.Fatalf("nil registry: got %v, want ErrLegalContextUnknown", err)
			}
		})
		t.Run("nil signer", func(t *testing.T) {
			if _, err := Resolve(base, registry, nil, now); !errors.Is(err, ErrLegalContextUnknown) {
				t.Fatalf("nil signer: got %v, want ErrLegalContextUnknown", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		in := validInput(t, testCAJurisdiction())
		ctx, err := Resolve(in, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got := ctx.Jurisdiction(); got != testCAJurisdiction() {
			t.Errorf("Jurisdiction() = %v, want %v", got, testCAJurisdiction())
		}
		if got := ctx.LegalEntityID(); got != in.LegalEntityID {
			t.Errorf("LegalEntityID() = %q, want %q", got, in.LegalEntityID)
		}
		releases := ctx.RulePackReleases()
		if len(releases) != 1 {
			t.Fatalf("RulePackReleases() has %d entries, want 1", len(releases))
		}
		if releases[0].PackID != "us-ca-promotion-base-pay-change" || releases[0].Version != 1 {
			t.Errorf("release = %+v, want the seeded CA promotion pack v1", releases[0])
		}
		if ctx.EffectiveDate().Compare(in.EffectiveDate) != 0 {
			t.Errorf("EffectiveDate() = %s, want %s", ctx.EffectiveDate(), in.EffectiveDate)
		}
		if ctx.KnownAt().Instant().Compare(in.KnownAt.Instant()) != 0 {
			t.Errorf("KnownAt() = %s, want %s", ctx.KnownAt(), in.KnownAt)
		}
		if ctx.RecordedAt().Instant().Compare(now) != 0 {
			t.Errorf("RecordedAt() = %s, want the resolver clock %s", ctx.RecordedAt(), now)
		}
		prov := ctx.Provenance()
		if prov.WorkLocationBasis == "" || prov.EmploymentJurisdictionBasis == "" || prov.RemoteWorkPolicyApplied == "" {
			t.Errorf("Provenance() has an empty field: %+v", prov)
		}
		if ctx.Confidence() != ConfidenceVerified {
			t.Errorf("Confidence() = %s, want VERIFIED for agreeing on-site facts", ctx.Confidence())
		}
		if len(ctx.Digest()) != 64 {
			t.Errorf("Digest() has length %d, want 64 (hex sha256)", len(ctx.Digest()))
		}
		if err := ctx.Verify(); err != nil {
			t.Errorf("Verify() on a freshly resolved context: %v", err)
		}
	})

	t.Run("GREEN remote work with disagreeing employment jurisdiction is ASSERTED, not ambiguous", func(t *testing.T) {
		in := validInput(t, testCAJurisdiction())
		in.RemoteWork = true
		in.EmploymentJurisdiction = testNYJurisdiction() // employer designates NY; worker physically works from CA
		ctx, err := Resolve(in, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got := ctx.Jurisdiction(); got != testCAJurisdiction() {
			t.Errorf("remote work: Jurisdiction() = %v, want physical work location %v", got, testCAJurisdiction())
		}
		if ctx.Confidence() != ConfidenceAsserted {
			t.Errorf("Confidence() = %s, want ASSERTED when employment jurisdiction disagrees under remote work", ctx.Confidence())
		}
		if err := ctx.Verify(); err != nil {
			t.Errorf("Verify(): %v", err)
		}
	})
}

// --- TestTodo_LEGAL_001_Golden ---------------------------------------------

// TestTodo_LEGAL_001_Golden fixes the signer key, the resolver clock, and the
// proposal facts, and asserts an exact digest and an exact sorted obligation
// set. Any change to the canonical encoding or to the evaluation logic that
// is not an intentional, reviewed change will break this test.
func TestTodo_LEGAL_001_Golden(t *testing.T) {
	const wantDigest = "d90ec990a9d0f9db3cf42b1439f0cabf92d60478dc6a6962eb814457da87d0a5"

	registry := testRegistry(t)
	signer := fixedSigner(t, 0x2a)
	now := mustInstant(t, 1_772_000_000)

	in := LegalContextInput{
		LegalEntityID:          "golden-legal-entity",
		WorkLocation:           testCAJurisdiction(),
		EmploymentJurisdiction: testCAJurisdiction(),
		EffectiveDate:          mustDate(t, 2026, time.April, 15),
		KnownAt:                mustKnownAt(t, 1_771_900_000),
	}
	ctx, err := Resolve(in, registry, signer, now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := ctx.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ctx.Digest() != wantDigest {
		t.Fatalf("Digest() = %s, want golden %s (canonical encoding or resolution logic changed)", ctx.Digest(), wantDigest)
	}

	currentPay, err := values.NewMoney("8000.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney(current): %v", err)
	}
	newPay, err := values.NewMoney("9500.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney(new): %v", err)
	}
	proposal := PromotionProposalSnapshot{
		WorkerID:              "worker-golden-1",
		LegalEntityID:         in.LegalEntityID,
		EffectiveDate:         in.EffectiveDate,
		CurrentBasePay:        currentPay,
		NewBasePay:            newPay,
		PayFrequency:          "SEMIMONTHLY",
		IsInternalPromotion:   true,
		CollectsSalaryHistory: true,
		OnProtectedLeave:      true,
		HasExistingNonCompete: true,
	}
	result, err := Evaluate(ctx, proposal, registry)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != LegalEvaluationStatusAllowWithObligations {
		t.Fatalf("Status = %s, want ALLOW_WITH_OBLIGATIONS", result.Status)
	}

	wantIDs := []string{
		"ca-notice-pay-rate-change",
		"ca-field-restriction-salary-history",
		"ca-retention-wage-job-title-history",
		"ca-retention-wage-statements",
		"ca-leave-interaction-paid-sick-leave",
		"ca-pay-frequency-semimonthly",
		"ca-pay-transparency-scale-on-request",
		"ca-noncompete-void",
	}
	var gotIDs []string
	for _, o := range result.Obligations {
		gotIDs = append(gotIDs, o.ID)
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("obligation IDs (in order) = %v, want %v", gotIDs, wantIDs)
	}
	for _, o := range result.Obligations {
		if err := o.Citation.Validate(); err != nil {
			t.Errorf("obligation %s carries an invalid citation: %v", o.ID, err)
		}
		if o.Citation.Status != ReviewStatusUnreviewed {
			t.Errorf("obligation %s citation status = %s, want UNREVIEWED for a seed pack", o.ID, o.Citation.Status)
		}
		if !o.Binding.NonRemovable {
			t.Errorf("obligation %s binding is removable; every seeded obligation is statutory and mandatory", o.ID)
		}
	}
}

// --- TestTodo_LEGAL_001_Race -------------------------------------------------

// TestTodo_LEGAL_001_Race exercises concurrent Resolve, Evaluate, and
// Registry.Register calls against shared state. The environment this ticket
// runs in does not support `go test -race` (windows/arm64), so this test
// proves correctness under concurrency by cross-checking results rather than
// relying on the race detector.
func TestTodo_LEGAL_001_Race(t *testing.T) {
	registry := testRegistry(t)
	signer := fixedSigner(t, 0x03)
	now := mustInstant(t, 1_770_500_000)
	in := validInput(t, testCAJurisdiction())

	want, err := Resolve(in, registry, signer, now)
	if err != nil {
		t.Fatalf("Resolve (baseline): %v", err)
	}
	wantResult, err := Evaluate(want, PromotionProposalSnapshot{IsInternalPromotion: true}, registry)
	if err != nil {
		t.Fatalf("Evaluate (baseline): %v", err)
	}

	const goroutines = 50
	const iterations = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ctx, err := Resolve(in, registry, signer, now)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: Resolve: %w", id, i, err)
					return
				}
				if ctx.Digest() != want.Digest() {
					errs <- fmt.Errorf("goroutine %d iter %d: digest %s != baseline %s", id, i, ctx.Digest(), want.Digest())
					return
				}
				if err := ctx.Verify(); err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: Verify: %w", id, i, err)
					return
				}
				result, err := Evaluate(ctx, PromotionProposalSnapshot{IsInternalPromotion: true}, registry)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: Evaluate: %w", id, i, err)
					return
				}
				if len(result.Obligations) != len(wantResult.Obligations) {
					errs <- fmt.Errorf("goroutine %d iter %d: %d obligations, want %d",
						id, i, len(result.Obligations), len(wantResult.Obligations))
					return
				}
				// Also read the registry concurrently with the writer goroutine
				// below to exercise Registry's lock discipline.
				if _, err := registry.Lookup(testCAJurisdiction(), in.EffectiveDate); err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: Lookup: %w", id, i, err)
					return
				}
			}
		}(g)
	}

	// A concurrent writer registers distinct, never-colliding jurisdictions so
	// its writes never race with the readers' fixed CA/NY lookups above.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			pack, err := CaliforniaPromotionPack()
			if err != nil {
				errs <- fmt.Errorf("writer iter %d: build pack: %w", i, err)
				return
			}
			pack.Jurisdiction = Jurisdiction{Country: "US", State: "ZZ"}
			pack.PackID = fmt.Sprintf("concurrent-fixture-%d", i)
			if err := registry.Register(pack); err != nil {
				errs <- fmt.Errorf("writer iter %d: Register: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// --- TestTodo_LEGAL_001_Security ---------------------------------------------

func TestTodo_LEGAL_001_Security(t *testing.T) {
	registry := testRegistry(t)
	signer := fixedSigner(t, 0x04)
	otherSigner := fixedSigner(t, 0x05)
	now := mustInstant(t, 1_770_600_000)
	in := validInput(t, testCAJurisdiction())

	ctx, err := Resolve(in, registry, signer, now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := ctx.Verify(); err != nil {
		t.Fatalf("baseline Verify(): %v", err)
	}

	t.Run("tampered effective date invalidates the digest", func(t *testing.T) {
		tampered := *ctx
		tampered.effectiveDate = mustDate(t, 2027, time.January, 1)
		if err := tampered.Verify(); !errors.Is(err, ErrDigestMismatch) {
			t.Errorf("Verify() on a tampered field = %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("tampered digest is rejected even though the signature bytes are untouched", func(t *testing.T) {
		tampered := *ctx
		tampered.digest = "0000000000000000000000000000000000000000000000000000000000000000"
		if err := tampered.Verify(); !errors.Is(err, ErrDigestMismatch) {
			t.Errorf("Verify() with a swapped digest = %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("flipped signature byte fails verification", func(t *testing.T) {
		tampered := *ctx
		sig := slices.Clone(ctx.signature.Bytes)
		sig[0] ^= 0xFF
		tampered.signature = Signature{PublicKey: ctx.signature.PublicKey, Bytes: sig}
		if err := tampered.Verify(); !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("Verify() with a flipped signature byte = %v, want ErrSignatureInvalid", err)
		}
	})

	t.Run("signature from a different context does not verify here", func(t *testing.T) {
		other, err := Resolve(validInput(t, testNYJurisdiction()), registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve(other): %v", err)
		}
		tampered := *ctx
		tampered.signature = other.signature
		if err := tampered.Verify(); err == nil {
			t.Errorf("Verify() accepted a signature minted over a different context's digest")
		}
	})

	t.Run("VerifyWithKey accepts only the signing key", func(t *testing.T) {
		if err := ctx.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Errorf("VerifyWithKey(correct key): %v", err)
		}
		if err := ctx.VerifyWithKey(otherSigner.PublicKey()); err == nil {
			t.Errorf("VerifyWithKey(wrong key) succeeded, want rejection")
		}
		if err := ctx.VerifyWithKey([]byte("too-short")); err == nil {
			t.Errorf("VerifyWithKey(malformed key) succeeded, want rejection")
		}
	})

	t.Run("Signature accessor never exposes the live internal slices", func(t *testing.T) {
		sig := ctx.Signature()
		sig.Bytes[0] ^= 0xFF
		sig.PublicKey[0] ^= 0xFF
		if err := ctx.Verify(); err != nil {
			t.Errorf("mutating a returned Signature copy corrupted the context: %v", err)
		}
	})
}

// --- TestTodo_LEGAL_001_Mutation ---------------------------------------------

// TestTodo_LEGAL_001_Mutation targets exact boundaries and defensive copies
// that a subtly wrong implementation (an off-by-one, a swapped comparison, a
// missing clone) would still pass a looser test against.
func TestTodo_LEGAL_001_Mutation(t *testing.T) {
	registry := testRegistry(t)
	signer := fixedSigner(t, 0x06)
	now := mustInstant(t, 1_770_700_000)

	t.Run("RulePackReleases is a defensive copy", func(t *testing.T) {
		ctx, err := Resolve(validInput(t, testCAJurisdiction()), registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		releases := ctx.RulePackReleases()
		releases[0].PackID = "corrupted"
		if ctx.RulePackReleases()[0].PackID == "corrupted" {
			t.Fatal("mutating the returned release slice corrupted internal state")
		}
	})

	t.Run("digest is sensitive to effective date by exactly one day", func(t *testing.T) {
		in1 := validInput(t, testCAJurisdiction())
		in2 := in1
		in2.EffectiveDate = in1.EffectiveDate.AddDays(1)

		ctx1, err := Resolve(in1, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve(day 1): %v", err)
		}
		ctx2, err := Resolve(in2, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve(day 2): %v", err)
		}
		if ctx1.Digest() == ctx2.Digest() {
			t.Fatal("digests are identical for effective dates one day apart; canonical encoding is dropping the effective date")
		}
	})

	t.Run("resolution is deterministic for identical input, clock, and key", func(t *testing.T) {
		in := validInput(t, testCAJurisdiction())
		ctxA, err := Resolve(in, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve A: %v", err)
		}
		ctxB, err := Resolve(in, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve B: %v", err)
		}
		if ctxA.Digest() != ctxB.Digest() {
			t.Fatalf("two resolutions of identical input produced different digests: %s vs %s", ctxA.Digest(), ctxB.Digest())
		}
		if !bytes.Equal(ctxA.signature.Bytes, ctxB.signature.Bytes) {
			t.Fatalf("two resolutions of identical input produced different signatures")
		}
	})

	t.Run("EffectiveWindow boundary is half-open: start included, end excluded", func(t *testing.T) {
		start := mustDate(t, 2026, time.January, 1)
		end := mustDate(t, 2027, time.January, 1)
		window, err := NewClosedEffectiveWindow(start, end)
		if err != nil {
			t.Fatalf("NewClosedEffectiveWindow: %v", err)
		}
		if !window.Contains(start) {
			t.Error("window does not contain its own inclusive start")
		}
		if window.Contains(end) {
			t.Error("window contains its own exclusive end")
		}
		if !window.Contains(end.AddDays(-1)) {
			t.Error("window does not contain the day immediately before its end")
		}
		if window.Contains(start.AddDays(-1)) {
			t.Error("window contains the day immediately before its start")
		}
	})

	t.Run("evaluation status flips exactly on obligation count", func(t *testing.T) {
		// A minimal pack with zero unconditional obligations proves the
		// RESOLVED_ALLOW branch is reachable, not just its opposite.
		start := mustDate(t, 2026, time.January, 1)
		window, err := NewOpenEffectiveWindow(start)
		if err != nil {
			t.Fatalf("NewOpenEffectiveWindow: %v", err)
		}
		emptyPack := RulePack{
			PackID:       "empty-fixture-pack",
			Version:      1,
			Jurisdiction: testTXJurisdiction(),
			Window:       window,
		}
		emptyRegistry := NewRegistry()
		if err := emptyRegistry.Register(emptyPack); err != nil {
			t.Fatalf("Register(empty): %v", err)
		}
		ctx, err := Resolve(validInput(t, testTXJurisdiction()), emptyRegistry, signer, now)
		if err != nil {
			t.Fatalf("Resolve against empty pack: %v", err)
		}
		result, err := Evaluate(ctx, PromotionProposalSnapshot{IsInternalPromotion: true, CollectsSalaryHistory: true}, emptyRegistry)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != LegalEvaluationStatusResolvedAllow {
			t.Errorf("Status = %s, want RESOLVED_ALLOW when the pack has zero obligations", result.Status)
		}
		if len(result.Obligations) != 0 {
			t.Errorf("Obligations = %v, want none", result.Obligations)
		}
	})
}
