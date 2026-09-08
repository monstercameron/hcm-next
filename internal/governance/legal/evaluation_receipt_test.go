package legal

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func receiptFixture(t *testing.T) (*LegalContext, *Registry, *Signer, values.Instant) {
	t.Helper()
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	for i := range packs {
		packs[i].ReviewStatus = ReviewStatusUnreviewed
	}
	packs[0].PayFrequencyConstraints = []PayFrequencyConstraint{{
		ID: "shared-pay-frequency", MinimumFrequency: "state-fixture",
		Citation: packs[0].PreemptionAssertions[0].Citation,
	}}
	packs[1].PayFrequencyConstraints = []PayFrequencyConstraint{{
		ID: "shared-pay-frequency", MinimumFrequency: "locality-fixture",
		Citation: packs[0].PreemptionAssertions[0].Citation,
	}}
	registry := NewRegistry()
	for _, pack := range packs {
		if err := registry.Register(pack); err != nil {
			t.Fatal(err)
		}
	}
	signer := fixedSigner(t, 0x24)
	ctx, err := Resolve(validInput(t, set.Overlays[0]), registry, signer, mustInstant(t, 1_770_100_000))
	if err != nil {
		t.Fatal(err)
	}
	return ctx, registry, signer, mustInstant(t, 1_770_200_000)
}

func TestTodo_LEGAL_014(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LegalContextDigest != ctx.Digest() || receipt.AttributionRuleFired != AttributionA1 {
		t.Fatalf("context evidence = %+v", receipt)
	}
	if len(receipt.PinnedReleases) != 2 || len(receipt.ObligationsApplied) != 2 || len(receipt.ObligationsNotApplicable) != 1 || len(receipt.PreemptionsApplied) != 1 {
		t.Fatalf("receipt sets = %+v", receipt)
	}
	if receipt.ObligationsApplied[0].BodyDigest == receipt.ObligationsApplied[1].BodyDigest {
		t.Fatal("same-ID obligations from different releases lost their exact body identity")
	}
	if len(receipt.CompositionTrace) != 1 || receipt.CompositionTrace[0].Kind != ObligationTypePayFrequency {
		t.Fatalf("trigger-filtered trace = %+v", receipt.CompositionTrace)
	}
	b, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip LegalEvaluationReceipt
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_LEGAL_014_Golden(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	const want = "88b634b5384e0e230cdb4e95757c86fde8158b50ece43befd97f7a196bf3f762"
	if got := gotDigest(receipt.CanonicalBytes()); got != want {
		t.Fatalf("canonical digest = %s, want %s", got, want)
	}
}

func TestTodo_LEGAL_014_Attribution(t *testing.T) {
	for _, rule := range []AttributionRule{AttributionA1, AttributionA2, AttributionA3, AttributionA4, AttributionA5, AttributionA6} {
		t.Run(string(rule), func(t *testing.T) {
			ctx, registry, signer, at := receiptFixture(t)
			ctx.attributionRule = rule
			ctx.digest, ctx.signature = signer.SignDigest(ctx.canonicalBytes())
			receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.AttributionRuleFired != rule {
				t.Fatalf("attribution = %s, want %s", receipt.AttributionRuleFired, rule)
			}
		})
	}
}

func TestTodo_LEGAL_014_Race(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	results := make(chan string, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			r, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- string(r.CanonicalBytes())
		}()
	}
	want := <-results
	for i := 1; i < cap(results); i++ {
		if got := <-results; got != want {
			t.Fatal("concurrent receipts differ")
		}
	}
}

func TestTodo_LEGAL_014_Security(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := receipt.VerifyWithKey(fixedSigner(t, 0x25).PublicKey()); !errors.Is(err, ErrReceiptInvalid) {
		t.Fatalf("wrong authority error = %v", err)
	}
	receipt.Status = LegalEvaluationStatusResolvedAllow
	if err := receipt.Verify(); err == nil {
		t.Fatal("tampered receipt verified")
	}
}

func TestTodo_LEGAL_014_Recovery(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	registry = NewRegistry()
	if err := receipt.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatalf("offline verification: %v", err)
	}
}

func TestTodo_LEGAL_014_Mutation(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	base, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*LegalEvaluationReceipt)
	}{
		{"context", func(r *LegalEvaluationReceipt) { r.LegalContextDigest = "" }},
		{"releases", func(r *LegalEvaluationReceipt) { r.PinnedReleases = nil }},
		{"attribution", func(r *LegalEvaluationReceipt) { r.AttributionRuleFired = "" }},
		{"not-applicable", func(r *LegalEvaluationReceipt) { r.ObligationsNotApplicable = nil }},
		{"trace", func(r *LegalEvaluationReceipt) { r.CompositionTrace = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := base
			tc.mutate(&mutated)
			mutated.Digest, mutated.Signature = signer.SignDigest(mutated.CanonicalBytes())
			if err := mutated.Verify(); !errors.Is(err, ErrReceiptInvalid) {
				t.Fatalf("signed incomplete receipt error = %v", err)
			}
		})
	}
}

func TestTodo_LEGAL_014_TypedBodyDigestMutation(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	base, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	mutatedRegistry := NewRegistry()
	for _, release := range ctx.RulePackReleases() {
		pack, getErr := registry.GetExact(release)
		if getErr != nil {
			t.Fatal(getErr)
		}
		for i := range pack.PayFrequencyConstraints {
			pack.PayFrequencyConstraints[i].MinimumFrequency += "-changed"
		}
		pack.Digest = ""
		pack.Signatures = nil
		if err := mutatedRegistry.Register(*pack); err != nil {
			t.Fatal(err)
		}
	}
	mutated, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, mutatedRegistry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.ObligationsApplied) != len(mutated.ObligationsApplied) {
		t.Fatalf("metadata changed obligation cardinality: %d != %d", len(base.ObligationsApplied), len(mutated.ObligationsApplied))
	}
	for i := range base.ObligationsApplied {
		if base.ObligationsApplied[i].ID != mutated.ObligationsApplied[i].ID || base.ObligationsApplied[i].PackID != mutated.ObligationsApplied[i].PackID {
			t.Fatalf("obligation metadata changed: %+v != %+v", base.ObligationsApplied[i], mutated.ObligationsApplied[i])
		}
		if base.ObligationsApplied[i].BodyDigest == mutated.ObligationsApplied[i].BodyDigest {
			t.Fatalf("typed body mutation retained digest for %s/%s", base.ObligationsApplied[i].PackID, base.ObligationsApplied[i].ID)
		}
	}
	if err := mutated.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatalf("mutated receipt offline verification: %v", err)
	}
}
