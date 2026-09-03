package artifacts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/artifacts"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// TestTodo_DATA_016 proves the two halves of DATA-016 together: reference
// accounting tracks exactly the owners currently holding a reference (add is
// idempotent, remove requires an active reference, the count is the number
// of distinct current owners, never a running tally of every event), and
// retrieval authorization is enforced from that same state -- a subject
// whose scope covers a current reference may retrieve; one whose scope
// covers no current reference may not, even for the same artifact.
func TestTodo_DATA_016(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	rec := f.mustPut(t, f.putRequest([]byte("q1 compensation adjustment proposal")))
	proposalOwner := artifacts.OwnerRef{Kind: artifacts.OwnerProposalRevision, ID: "intent-42:rev-1"}
	observationOwner := artifacts.OwnerRef{Kind: artifacts.OwnerObservation, ID: "obs-7"}

	if n, err := artifactsReferenceCount(f, rec.ContentID); err != nil || n != 0 {
		t.Fatalf("fresh artifact reference count = %d, %v; want 0, nil", n, err)
	}

	t.Run("add is idempotent", func(t *testing.T) {
		f.mustAddReference(t, rec.ContentID, proposalOwner)
		f.mustAddReference(t, rec.ContentID, proposalOwner)
		if n, err := artifactsReferenceCount(f, rec.ContentID); err != nil || n != 1 {
			t.Fatalf("reference count after two adds by the same owner = %d, %v; want 1, nil", n, err)
		}
	})

	t.Run("a distinct owner adds a second reference", func(t *testing.T) {
		f.mustAddReference(t, rec.ContentID, observationOwner)
		if n, err := artifactsReferenceCount(f, rec.ContentID); err != nil || n != 2 {
			t.Fatalf("reference count with two distinct owners = %d, %v; want 2, nil", n, err)
		}
	})

	t.Run("removing a reference the owner never held is refused", func(t *testing.T) {
		neverHeld := artifacts.OwnerRef{Kind: artifacts.OwnerReceipt, ID: "receipt-99"}
		err := f.removeReference(rec.ContentID, neverHeld)
		var notFound artifacts.ErrReferenceNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("removing a never-held reference returned %v, want ErrReferenceNotFound", err)
		}
	})

	t.Run("removing an active reference drops the count", func(t *testing.T) {
		if err := f.removeReference(rec.ContentID, observationOwner); err != nil {
			t.Fatalf("remove reference: %v", err)
		}
		if n, err := artifactsReferenceCount(f, rec.ContentID); err != nil || n != 1 {
			t.Fatalf("reference count after removing one of two owners = %d, %v; want 1, nil", n, err)
		}
	})

	t.Run("a subject scope covering the current reference may retrieve", func(t *testing.T) {
		auth := artifacts.RetrievalAuthorization{
			Purpose:                "compensation.review",
			AllowedClassifications: []model.ClassificationLabel{rec.Classification},
			Scope:                  artifacts.SubjectScope{AllowedOwnerRefs: []string{proposalOwner.ID}},
			RequestedBy:            "hr:reviewer",
		}
		content, _, err := f.retrieve(rec.ContentID, auth, fixedNow)
		if err != nil {
			t.Fatalf("retrieve in scope: %v", err)
		}
		if string(content) == "" {
			t.Fatal("retrieve in scope returned no bytes")
		}
	})

	t.Run("a subject scope covering no current reference is denied", func(t *testing.T) {
		auth := artifacts.RetrievalAuthorization{
			Purpose:                "compensation.review",
			AllowedClassifications: []model.ClassificationLabel{rec.Classification},
			Scope:                  artifacts.SubjectScope{AllowedOwnerRefs: []string{"someone-unrelated"}},
			RequestedBy:            "outsider",
		}
		before := refusalCount(t, f, rec.ContentID)
		_, _, err := f.retrieve(rec.ContentID, auth, fixedNow)
		var denied artifacts.ErrRetrievalDenied
		if !errors.As(err, &denied) {
			t.Fatalf("retrieve out of scope returned %v, want ErrRetrievalDenied", err)
		}
		if after := refusalCount(t, f, rec.ContentID); after != before+1 {
			t.Fatalf("refusal count %d, want %d", after, before+1)
		}
	})
}

// TestTodo_DATA_016_Golden pins the refusal evidence id's shape and
// determinism: a denial always produces an "ev:artifact:refusal:" prefixed,
// 32 hex character evidence id, and evaluating the identical decision at the
// identical instant twice produces the identical evidence id both times --
// the same replay-stable property internal/trust/authz.Decision.EvidenceID
// carries.
func TestTodo_DATA_016_Golden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	rec := f.mustPut(t, f.putRequest([]byte("golden refusal fixture")))
	auth := artifacts.RetrievalAuthorization{
		Purpose:                "unauthorized.purpose",
		AllowedClassifications: nil,
		Scope:                  artifacts.SubjectScope{AllSubjects: true},
		RequestedBy:            "auditor",
	}

	_, _, err1 := f.retrieve(rec.ContentID, auth, fixedNow)
	_, _, err2 := f.retrieve(rec.ContentID, auth, fixedNow)

	var denied1, denied2 artifacts.ErrRetrievalDenied
	if !errors.As(err1, &denied1) || !errors.As(err2, &denied2) {
		t.Fatalf("expected two ErrRetrievalDenied, got %v and %v", err1, err2)
	}

	evidenceIDs := latestRefusalEvidenceIDs(t, f, rec.ContentID, 2)
	if len(evidenceIDs) != 2 {
		t.Fatalf("recorded %d refusals, want 2", len(evidenceIDs))
	}
	for _, id := range evidenceIDs {
		const prefix = "ev:artifact:refusal:"
		if len(id) != len(prefix)+32 || id[:len(prefix)] != prefix {
			t.Fatalf("evidence id %q does not match the ev:artifact:refusal:<32 hex> shape", id)
		}
	}
	if evidenceIDs[0] != evidenceIDs[1] {
		t.Fatalf("two identical denials at the identical instant produced evidence ids %s and %s, want identical", evidenceIDs[0], evidenceIDs[1])
	}
}

// TestTodo_DATA_016_Security proves the RED cases: a denied purpose, a
// classification outside the allow-list, a guessed/never-written content id,
// a cross-tenant lookup and a stale (expired) authorization grant all read
// no bytes and each is refused with evidence.
func TestTodo_DATA_016_Security(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	rec := f.mustPut(t, f.putRequest([]byte("classified content")))
	owner := artifacts.OwnerRef{Kind: artifacts.OwnerReceipt, ID: "receipt-1"}
	f.mustAddReference(t, rec.ContentID, owner)

	t.Run("no declared purpose cannot read bytes", func(t *testing.T) {
		auth := artifacts.RetrievalAuthorization{
			AllowedClassifications: []model.ClassificationLabel{rec.Classification},
			Scope:                  artifacts.SubjectScope{AllSubjects: true},
			RequestedBy:            "no-purpose-caller",
		}
		content, gotRec, err := f.retrieve(rec.ContentID, auth, fixedNow)
		if content != nil {
			t.Fatal("a purposeless retrieval returned bytes")
		}
		if gotRec != (artifacts.Record{}) {
			t.Fatal("a purposeless retrieval returned a populated record")
		}
		var denied artifacts.ErrRetrievalDenied
		if !errors.As(err, &denied) {
			t.Fatalf("purposeless retrieve returned %v, want ErrRetrievalDenied", err)
		}
	})

	t.Run("a classification outside the allow-list cannot read bytes", func(t *testing.T) {
		auth := artifacts.RetrievalAuthorization{
			Purpose:                "some.purpose",
			AllowedClassifications: []model.ClassificationLabel{model.ClassPublic},
			Scope:                  artifacts.SubjectScope{AllSubjects: true},
			RequestedBy:            "wrong-clearance",
		}
		if rec.Classification == model.ClassPublic {
			t.Fatal("fixture artifact is unexpectedly PUBLIC; the allow-list would not exclude it")
		}
		content, _, err := f.retrieve(rec.ContentID, auth, fixedNow)
		if content != nil {
			t.Fatal("an out-of-classification retrieval returned bytes")
		}
		var denied artifacts.ErrRetrievalDenied
		if !errors.As(err, &denied) {
			t.Fatalf("out-of-classification retrieve returned %v, want ErrRetrievalDenied", err)
		}
	})

	t.Run("a guessed content id cannot read bytes, and is still evidenced", func(t *testing.T) {
		guessed := sha256Hex([]byte("never actually written"))
		before := refusalCount(t, f, guessed)
		content, _, err := f.retrieve(guessed, allowAll("guesser"), fixedNow)
		if content != nil {
			t.Fatal("retrieving a guessed content id returned bytes")
		}
		var denied artifacts.ErrRetrievalDenied
		if !errors.As(err, &denied) {
			t.Fatalf("guessed content id retrieve returned %v, want ErrRetrievalDenied", err)
		}
		if after := refusalCount(t, f, guessed); after != before+1 {
			t.Fatalf("refusal count for the guessed id %d, want %d", after, before+1)
		}
	})

	t.Run("a cross-tenant lookup cannot read bytes", func(t *testing.T) {
		otherTenantID := insertTenant(t, f.db, "cross-tenant")
		otherTenant := fixture{db: f.db, schema: f.schema, tenant: otherTenantID}
		content, _, err := otherTenant.retrieve(rec.ContentID, allowAll("cross-tenant-caller"), fixedNow)
		if content != nil {
			t.Fatal("a cross-tenant retrieval returned bytes")
		}
		var denied artifacts.ErrRetrievalDenied
		if !errors.As(err, &denied) {
			t.Fatalf("cross-tenant retrieve returned %v, want ErrRetrievalDenied", err)
		}

		// The same isolation holds one layer down, at the database itself:
		// migrations/00010_artifacts.sql's row level security policy, not
		// just this package's own tenant_id filter, keeps the row out of
		// reach of the least-privilege role scoped to the wrong tenant.
		if rlsVisibleCrossTenant(t, f, otherTenantID, rec.ContentID) {
			t.Fatal("hcmnext_app scoped to another tenant could see this artifact row via row level security")
		}
	})

	t.Run("an expired authorization grant cannot read bytes", func(t *testing.T) {
		auth := allowAll("stale-grant-holder")
		auth.ExpiresAt = fixedNow.Add(-time.Minute)
		content, _, err := f.retrieve(rec.ContentID, auth, fixedNow)
		if content != nil {
			t.Fatal("an expired grant returned bytes")
		}
		var denied artifacts.ErrRetrievalDenied
		if !errors.As(err, &denied) {
			t.Fatalf("expired grant retrieve returned %v, want ErrRetrievalDenied", err)
		}
	})

	t.Run("a denial's evidence is only durable if the caller commits", func(t *testing.T) {
		guessed := sha256Hex([]byte("committed vs rolled back denial"))
		auth := artifacts.RetrievalAuthorization{Purpose: "test.read", RequestedBy: "caller"}

		// A caller that rolls back on ErrRetrievalDenied -- the mistake
		// [artifacts.Retrieve]'s own doc comment warns against -- loses the
		// refusal row it just wrote.
		ctx := context.Background()
		tx, err := f.db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if _, _, err := artifacts.Retrieve(ctx, tx, f.schema, f.tenant, guessed, auth, fixedNow); err == nil {
			t.Fatal("retrieving a never-written content id succeeded")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if n := refusalCount(t, f, guessed); n != 0 {
			t.Fatalf("refusal count after a rolled-back denial = %d, want 0", n)
		}

		// The fixture's own retrieve helper commits on ErrRetrievalDenied,
		// exactly as callers must, and the same decision now persists.
		if _, _, err := f.retrieve(guessed, auth, fixedNow); err == nil {
			t.Fatal("retrieving a never-written content id succeeded")
		}
		if n := refusalCount(t, f, guessed); n != 1 {
			t.Fatalf("refusal count after a committed denial = %d, want 1", n)
		}
	})
}
