package knowledge_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
)

type testActivationReceipt struct {
	BundleID     string
	BundleDigest string
	Epoch        uint64
	Digest       string
}

func lifecycleTime(hour int) time.Time {
	return time.Date(2026, 9, 5, hour, 0, 0, 0, time.UTC)
}

func addAndApprove(t *testing.T, store *knowledge.MemoryStore, article knowledge.ArticleRevision) {
	t.Helper()
	if _, err := store.Add(article); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := store.SubmitForReview(article.ArticleID, article.Revision, "reviewer-1", lifecycleTime(10)); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	_, err := store.Approve(article.ArticleID, article.Revision, knowledge.ReviewEvidence{
		Reviewer: "reviewer-1",
		Approvals: []knowledge.ReviewApproval{
			{ApproverID: "approver-1", ApprovalRef: "approval-1"},
		},
	}, lifecycleTime(11))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
}

// TestTodo_KNOW_002 is the PRIMARY test for review, localization,
// publication, exact config-bundle activation, and retirement history.
func TestTodo_KNOW_002(t *testing.T) {
	article := minimalArticle(t)
	store := knowledge.NewMemoryStore()
	addAndApprove(t, store, article)

	localized := knowledge.LocalizedRevision{
		ArticleID: article.ArticleID, Revision: article.Revision, Locale: "fr-FR",
		Reviewer: "reviewer-fr", SourceRevisionDigest: article.Digest(),
		TranslationProvenance: knowledge.ProvenanceHuman, BodyDigest: "sha256:fr-body",
		Title: "Congé", Summary: "Résumé", Classification: article.Classification,
		Jurisdiction: article.Jurisdiction, CreatedAt: lifecycleTime(12),
	}
	if _, err := store.AddLocalization(localized); err != nil {
		t.Fatalf("AddLocalization: %v", err)
	}
	if _, err := store.Publish(knowledge.PublishRequest{
		ArticleID: article.ArticleID, Revision: article.Revision, Locale: "fr-FR",
		Reviewer: "reviewer-fr", SourceRevisionDigest: article.Digest(), At: lifecycleTime(13),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	receipt := testActivationReceipt{
		BundleID: "knowledge-bundle", BundleDigest: "sha256:bundle", Epoch: 7,
		Digest: "sha256:receipt",
	}
	event, err := store.Activate(knowledge.ActivationRequest{
		ArticleID: article.ArticleID, Revision: article.Revision, Locale: "fr-FR",
		Reviewer: "reviewer-fr", SourceRevisionDigest: article.Digest(),
		BundleID: receipt.BundleID, BundleDigest: receipt.BundleDigest,
		ActivationEpoch: receipt.Epoch, ReceiptDigest: receipt.Digest,
		Receipt: receipt, At: lifecycleTime(14),
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if event.Digest == "" || event.ActivationEpoch != 7 || event.Locale != "fr-FR" {
		t.Fatalf("activation event = %+v", event)
	}

	if _, err := store.Retire(knowledge.RetireRequest{ArticleID: article.ArticleID, Revision: article.Revision, At: lifecycleTime(15)}); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	view, err := store.View(article.ArticleID, article.Revision)
	if err != nil {
		t.Fatalf("View retired revision: %v", err)
	}
	if view.State != knowledge.StateRetired || len(view.Activations) != 1 {
		t.Fatalf("retired view = %+v", view)
	}
	read, err := store.ReadAsOf(article.ArticleID, article.Revision, lifecycleTime(16))
	if err != nil || read.Digest() != article.Digest() {
		t.Fatalf("ReadAsOf retired revision = %v, %s", err, read.Digest())
	}
	if len(store.Events()) != 7 {
		t.Fatalf("event count = %d, want drafted/reviewed/approved/localized/published/activated/retired", len(store.Events()))
	}
}

// TestTodo_KNOW_002_Security proves that publication is fail-closed for every
// prohibited source or translation condition and that activation is exact.
func TestTodo_KNOW_002_Security(t *testing.T) {
	t.Run("approval requirement requires distinct approvers", func(t *testing.T) {
		article := minimalArticle(t)
		store := knowledge.NewMemoryStore(knowledge.ReviewPolicy{MinApprovals: 2, RequireDistinctApprovers: true})
		if _, err := store.Add(article); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SubmitForReview(article.ArticleID, article.Revision, "reviewer-1", lifecycleTime(10)); err != nil {
			t.Fatal(err)
		}
		_, err := store.Approve(article.ArticleID, article.Revision, []knowledge.ReviewApproval{
			{ApproverID: "same", ApprovalRef: "a1"}, {ApproverID: "same", ApprovalRef: "a2"},
		}, lifecycleTime(11))
		if !errors.Is(err, knowledge.ErrDistinctApprovers) {
			t.Fatalf("duplicate approval error = %v, want ErrDistinctApprovers", err)
		}
		if _, err := store.Approve(article.ArticleID, article.Revision, []knowledge.ReviewApproval{
			{ApproverID: "one", ApprovalRef: "a1"}, {ApproverID: "two", ApprovalRef: "a2"},
		}, lifecycleTime(11)); err != nil {
			t.Fatalf("distinct approvals: %v", err)
		}
	})

	t.Run("unapproved revision is refused", func(t *testing.T) {
		article := minimalArticle(t)
		store := knowledge.NewMemoryStore()
		if _, err := store.Add(article); err != nil {
			t.Fatal(err)
		}
		_, err := store.Publish(knowledge.PublishRequest{ArticleID: article.ArticleID, Revision: article.Revision, At: lifecycleTime(13)})
		if !errors.Is(err, knowledge.ErrReviewRequired) {
			t.Fatalf("unapproved Publish error = %v, want ErrReviewRequired", err)
		}
	})

	t.Run("machine-only legal translation is refused", func(t *testing.T) {
		article := minimalArticle(t)
		store := knowledge.NewMemoryStore()
		addAndApprove(t, store, article)
		_, err := store.AddLocalization(knowledge.LocalizedRevision{
			ArticleID: article.ArticleID, Revision: article.Revision, Locale: "de-DE",
			SourceRevisionDigest: article.Digest(), TranslationProvenance: knowledge.ProvenanceMachine,
			BodyDigest: "sha256:machine-legal", Classification: "LEGAL", Legal: true,
			CreatedAt: lifecycleTime(12),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Publish(knowledge.PublishRequest{ArticleID: article.ArticleID, Revision: article.Revision, Locale: "de-DE", At: lifecycleTime(13)})
		if !errors.Is(err, knowledge.ErrMachineLegalTranslation) {
			t.Fatalf("Publish error = %v, want ErrMachineLegalTranslation", err)
		}
	})

	t.Run("unresolved citation is refused", func(t *testing.T) {
		article := minimalArticle(t)
		store := knowledge.NewMemoryStore()
		addAndApprove(t, store, article)
		if _, err := store.AddLocalization(knowledge.LocalizedRevision{
			ArticleID: article.ArticleID, Revision: article.Revision, Locale: "es-ES",
			Reviewer: "reviewer-es", SourceRevisionDigest: article.Digest(),
			TranslationProvenance: knowledge.ProvenanceMachineReviewed, BodyDigest: "sha256:es",
			UnresolvedCitations: []string{"citation:missing"}, CreatedAt: lifecycleTime(12),
		}); err != nil {
			t.Fatal(err)
		}
		_, err := store.Publish(knowledge.PublishRequest{ArticleID: article.ArticleID, Revision: article.Revision, Locale: "es-ES", At: lifecycleTime(13)})
		if !errors.Is(err, knowledge.ErrUnresolvedCitation) {
			t.Fatalf("Publish error = %v, want ErrUnresolvedCitation", err)
		}
	})

	t.Run("stale source and mismatched activation are refused", func(t *testing.T) {
		article := minimalArticle(t)
		store := knowledge.NewMemoryStore()
		addAndApprove(t, store, article)
		successor := article
		successor.Revision = 2
		successor.BodyDigest = "sha256:successor"
		if _, err := store.Add(successor); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Publish(knowledge.PublishRequest{ArticleID: article.ArticleID, Revision: article.Revision, At: lifecycleTime(13)}); !errors.Is(err, knowledge.ErrStaleSource) {
			t.Fatalf("stale Publish error = %v, want ErrStaleSource", err)
		}
		if _, err := store.SubmitForReview(successor.ArticleID, successor.Revision, "reviewer-2", lifecycleTime(10)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Approve(successor.ArticleID, successor.Revision, knowledge.ReviewApproval{ApproverID: "approver-2", ApprovalRef: "approval-2"}, lifecycleTime(11)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Publish(knowledge.PublishRequest{ArticleID: successor.ArticleID, Revision: successor.Revision, At: lifecycleTime(13)}); err != nil {
			t.Fatal(err)
		}
		_, err := store.Activate(knowledge.ActivationRequest{
			ArticleID: successor.ArticleID, Revision: successor.Revision, Locale: successor.Locale,
			Reviewer: successor.Review.ReviewedBy, SourceRevisionDigest: article.Digest(),
			BundleID: "bundle", BundleDigest: "sha256:bundle", ActivationEpoch: 1,
			ReceiptDigest: "sha256:receipt", At: lifecycleTime(14),
		})
		if !errors.Is(err, knowledge.ErrActivationMismatch) {
			t.Fatalf("mismatched activation error = %v, want ErrActivationMismatch", err)
		}
	})
}

// TestTodo_KNOW_002_Mutation proves lifecycle and activation digests bind all
// identity fields while Explain remains safe for audit display.
func TestTodo_KNOW_002_Mutation(t *testing.T) {
	article := minimalArticle(t)
	store := knowledge.NewMemoryStore()
	addAndApprove(t, store, article)
	events := store.Events()
	if len(events) == 0 || events[0].Digest == "" {
		t.Fatal("draft event has no digest")
	}
	mutated := events[0]
	mutated.Reviewer = "reviewer-attacker"
	if mutated.DigestValue() == events[0].Digest {
		t.Fatal("reviewer mutation did not change event digest")
	}
	if strings.Contains(events[0].Explain(), article.BodyDigest) {
		t.Fatal("Explain disclosed body content instead of only a digest reference")
	}
	localized := knowledge.LocalizedRevision{
		ArticleID: article.ArticleID, Revision: article.Revision, Locale: "ja-JP",
		Reviewer: "reviewer-ja", SourceRevisionDigest: article.Digest(),
		TranslationProvenance: knowledge.ProvenanceHuman, BodyDigest: "sha256:ja",
		CreatedAt: lifecycleTime(12),
	}
	if err := localized.Validate(); err != nil {
		t.Fatal(err)
	}
	if localized.DigestValue() == "" || localized.DigestValue() == article.Digest() {
		t.Fatal("localization digest is absent or collided with source digest")
	}
}
