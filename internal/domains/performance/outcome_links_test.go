package performance_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/performance"
)

func finalRatingForOutcome(t *testing.T) performance.FinalRating {
	t.Helper()
	caseValue := ratingCaseFixture(t)
	final, err := caseValue.Finalize(reviewInstant(t, "2026-10-15T00:00:00Z"))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return final
}

func TestTodo_PERFORMANCE_007(t *testing.T) {
	final := finalRatingForOutcome(t)
	opinion, err := performance.OpinionFromFinalRating(final)
	if err != nil {
		t.Fatalf("opinion: %v", err)
	}
	history, err := performance.NewOutcomeHistory([]performance.OpinionRecord{opinion})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	before, err := history.History()
	if err != nil {
		t.Fatal(err)
	}

	linked, link, err := history.LinkOutcome(performance.OutcomeLinkRequest{
		FinalRating: final, OutcomeKind: performance.OutcomeKindPromotionIntent,
		OutcomeRef: "intent/promotion-1", LinkingPrincipal: "principal/hr-1",
		EffectiveAt: reviewInstant(t, "2026-11-01T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link.Action != performance.OutcomeLinkActionLink || link.Digest == "" || link.RatingDigest != final.CanonicalDigest {
		t.Fatalf("link = %+v", link)
	}
	afterLink, err := linked.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterLink.OpinionRecords) != 1 || afterLink.OpinionRecords[0].Digest != before.OpinionRecords[0].Digest || len(afterLink.OutcomeLinks) != 1 {
		t.Fatalf("separate streams after link = %+v", afterLink)
	}

	unlinked, unlink, err := linked.UnlinkOutcome(performance.OutcomeUnlinkRequest{
		LinkDigest: link.Digest, LinkingPrincipal: "principal/hr-1",
		EffectiveAt: reviewInstant(t, "2026-11-02T00:00:00Z"), Reason: "downstream request withdrawn",
	})
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if unlink.Action != performance.OutcomeLinkActionUnlink || unlink.Digest == link.Digest || unlink.Reason == "" {
		t.Fatalf("unlink = %+v", unlink)
	}
	afterUnlink, err := unlinked.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterUnlink.OpinionRecords) != 1 || afterUnlink.OpinionRecords[0].Digest != before.OpinionRecords[0].Digest || len(afterUnlink.OutcomeLinks) != 2 {
		t.Fatalf("unlink deleted or rewrote a stream = %+v", afterUnlink)
	}
	explanation, err := unlinked.Explain()
	if err != nil || explanation.OpinionCount != 1 || explanation.OutcomeLinkCount != 2 {
		t.Fatalf("explanation = %+v, err=%v", explanation, err)
	}
}

func TestTodo_PERFORMANCE_007_Mutation(t *testing.T) {
	final := finalRatingForOutcome(t)
	ref, err := performance.NewFinalRatingReference(final, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref.State = performance.RatingRevisionStateSuperseded
	ref.Superseded = true
	if _, err := performance.LinkOutcome(performance.OutcomeLinkRequest{
		RatingRef: ref, OutcomeKind: performance.OutcomeKindRetentionFlag,
		OutcomeRef: "retention/flag-1", LinkingPrincipal: "principal/hr-1",
		EffectiveAt: reviewInstant(t, "2026-11-01T00:00:00Z"),
	}); !errors.Is(err, performance.ErrOutcomeRatingSuperseded) {
		t.Fatalf("superseded rating error = %v", err)
	}

	caseValue := ratingCaseFixture(t)
	if _, err := performance.LinkOutcome(caseValue, performance.OutcomeKindPromotionIntent, "intent/not-final", "principal/hr-1", reviewInstant(t, "2026-11-01T00:00:00Z")); !errors.Is(err, performance.ErrOutcomeRatingNotFinal) {
		t.Fatalf("non-final rating error = %v", err)
	}

	opinion, err := performance.OpinionFromFinalRating(final)
	if err != nil {
		t.Fatal(err)
	}
	history, err := performance.NewOutcomeHistory([]performance.OpinionRecord{opinion})
	if err != nil {
		t.Fatal(err)
	}
	linked, link, err := history.LinkOutcome(final, performance.OutcomeKindDevelopmentPlan, "plan/1", "principal/hr-1", reviewInstant(t, "2026-11-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	priorOpinionDigest := history.OpinionRecords[0].Digest
	mutated := linked
	mutated.OpinionRecords[0].Digest = "sha256:tampered"
	if _, err := mutated.Explain(); err == nil {
		t.Fatal("tampered opinion was accepted")
	}
	if history.OpinionRecords[0].Digest != priorOpinionDigest || final.CanonicalDigest == "" {
		t.Fatal("adding the outcome rewrote prior opinion state")
	}
	if _, _, err := linked.UnlinkOutcome(performance.OutcomeUnlinkRequest{LinkDigest: link.Digest, LinkingPrincipal: "principal/hr-1", EffectiveAt: reviewInstant(t, "2026-11-02T00:00:00Z")}); err == nil {
		t.Fatal("unlink without reason was accepted")
	}
}
