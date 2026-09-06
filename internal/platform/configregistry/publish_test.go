package configregistry

import (
	"testing"
	"time"
)

func newObj(scope Scope, kind Kind, id string, revision uint32, body string) ConfigurationObject {
	return ConfigurationObject{
		Kind:               kind,
		ID:                 id,
		Revision:           revision,
		Body:               []byte(body),
		SchemaRef:          "schema/v1",
		Scope:              scope,
		PublisherPrincipal: "pub-1",
		PublishedAt:        time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
}

func TestPublishMintsAndPersists(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	scope := Scope{TenantID: "t1"}
	obj, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "body-v1"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if obj.CanonicalBodyDigest == "" || obj.Digest() == "" {
		t.Fatalf("Publish did not mint digests: %+v", obj)
	}
	stored, found, err := store.GetObject(obj.Ref())
	if err != nil || !found {
		t.Fatalf("stored record not found: found=%v err=%v", found, err)
	}
	if stored.Digest() != obj.Digest() {
		t.Fatal("stored record digest does not match minted digest")
	}
}

func TestPublishRepublishOfIdenticalContentIsIdempotent(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	scope := Scope{TenantID: "t1"}
	first, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "same-body"))
	if err != nil {
		t.Fatalf("Publish (first): %v", err)
	}
	second, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "same-body"))
	if err != nil {
		t.Fatalf("Publish (second, identical): %v", err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("re-publishing identical content minted a new record: %s != %s", first.Digest(), second.Digest())
	}
}

func TestPublishDifferentBodyUnderSameRevisionIsRefused(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	scope := Scope{TenantID: "t1"}
	if _, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "body-a")); err != nil {
		t.Fatalf("Publish (first): %v", err)
	}
	_, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "body-b"))
	if err == nil {
		t.Fatal("Publish with a different body under the same revision: want error, got nil")
	}
	if CodeOf(err) != CodeRevisionConflict {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeRevisionConflict)
	}
}

func TestPublishRefusesInvalidObjects(t *testing.T) {
	t.Parallel()
	scope := Scope{TenantID: "t1"}
	base := newObj(scope, KindWorkflow, "w1", 1, "body")

	cases := []struct {
		name    string
		mutate  func(ConfigurationObject) ConfigurationObject
		wantErr string
	}{
		{"invalid kind", func(o ConfigurationObject) ConfigurationObject { o.Kind = "BOGUS"; return o }, CodeInvalidKind},
		{"no id", func(o ConfigurationObject) ConfigurationObject { o.ID = ""; return o }, CodeMissingID},
		{"zero revision", func(o ConfigurationObject) ConfigurationObject { o.Revision = 0; return o }, CodeInvalidRevision},
		{"no tenant", func(o ConfigurationObject) ConfigurationObject { o.Scope = Scope{}; return o }, CodeMissingScope},
		{"no schema ref", func(o ConfigurationObject) ConfigurationObject { o.SchemaRef = ""; return o }, CodeMissingSchemaRef},
		{"no publisher", func(o ConfigurationObject) ConfigurationObject { o.PublisherPrincipal = ""; return o }, CodeMissingPublisher},
		{"zero published at", func(o ConfigurationObject) ConfigurationObject { o.PublishedAt = time.Time{}; return o }, CodeMissingPublishedAt},
		{"empty body", func(o ConfigurationObject) ConfigurationObject { o.Body = nil; return o }, CodeEmptyBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Publish(NewRegistry(), tc.mutate(base))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if CodeOf(err) != tc.wantErr {
				t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), tc.wantErr)
			}
		})
	}
}

func TestPublishRefusesMismatchedCanonicalBodyDigest(t *testing.T) {
	t.Parallel()
	obj := newObj(Scope{TenantID: "t1"}, KindWorkflow, "w1", 1, "body")
	obj.CanonicalBodyDigest = "not-the-real-digest"
	_, err := Publish(NewRegistry(), obj)
	if CodeOf(err) != CodeBodyDigestMismatch {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeBodyDigestMismatch)
	}
}

func TestPublishRefusesNilStore(t *testing.T) {
	t.Parallel()
	_, err := Publish(nil, newObj(Scope{TenantID: "t1"}, KindWorkflow, "w1", 1, "body"))
	if CodeOf(err) != CodeNoStore {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeNoStore)
	}
}

func TestActivateOnlyPublishedRevisions(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	ref := ObjectRef{Scope: Scope{TenantID: "t1"}, Kind: KindWorkflow, ID: "w1", Revision: 1}
	_, err := Activate(store, ref, ActivationEvidence{ActivatedBy: "alice", ActivatedAt: time.Now()})
	if CodeOf(err) != CodeUnknownRevision {
		t.Fatalf("Activate on an unpublished revision: CodeOf(err) = %q, want %q", CodeOf(err), CodeUnknownRevision)
	}

	scope := Scope{TenantID: "t1"}
	published, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "body"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	rec, err := Activate(store, published.Ref(), ActivationEvidence{ActivatedBy: "alice", Authority: "admin", ActivatedAt: time.Unix(100, 0)})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if rec.ObjectDigest != published.Digest() {
		t.Fatalf("ActivationRecord.ObjectDigest = %q, want %q", rec.ObjectDigest, published.Digest())
	}
}

func TestActivateRefusesUnattributedOrUndatedEvidence(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	scope := Scope{TenantID: "t1"}
	published, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "body"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := Activate(store, published.Ref(), ActivationEvidence{ActivatedAt: time.Now()}); CodeOf(err) != CodeUnauthorizedActivation {
		t.Fatalf("Activate with no ActivatedBy: CodeOf(err) = %q, want %q", CodeOf(err), CodeUnauthorizedActivation)
	}
	if _, err := Activate(store, published.Ref(), ActivationEvidence{ActivatedBy: "alice"}); CodeOf(err) != CodeMissingActivationTime {
		t.Fatalf("Activate with no ActivatedAt: CodeOf(err) = %q, want %q", CodeOf(err), CodeMissingActivationTime)
	}
	if _, err := Activate(nil, published.Ref(), ActivationEvidence{ActivatedBy: "alice", ActivatedAt: time.Now()}); CodeOf(err) != CodeNoStore {
		t.Fatalf("Activate with nil store: CodeOf(err) = %q, want %q", CodeOf(err), CodeNoStore)
	}
}

func TestActivateSupersessionKeepsHistory(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	scope := Scope{TenantID: "t1"}
	v1, err := Publish(store, newObj(scope, KindPolicy, "p1", 1, "body-v1"))
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	v2, err := Publish(store, newObj(scope, KindPolicy, "p1", 2, "body-v2"))
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}

	if _, err := Activate(store, v1.Ref(), ActivationEvidence{ActivatedBy: "alice", ActivatedAt: time.Unix(1, 0)}); err != nil {
		t.Fatalf("Activate v1: %v", err)
	}
	resolved, err := Resolve(store, scope, KindPolicy, "p1")
	if err != nil || resolved.Revision != 1 {
		t.Fatalf("Resolve after activating v1: revision=%d err=%v", resolved.Revision, err)
	}

	if _, err := Activate(store, v2.Ref(), ActivationEvidence{ActivatedBy: "bob", ActivatedAt: time.Unix(2, 0)}); err != nil {
		t.Fatalf("Activate v2: %v", err)
	}
	resolved, err = Resolve(store, scope, KindPolicy, "p1")
	if err != nil || resolved.Revision != 2 {
		t.Fatalf("Resolve after activating v2: revision=%d err=%v", resolved.Revision, err)
	}

	history, err := store.ListActivations(scope, KindPolicy, "p1")
	if err != nil {
		t.Fatalf("ListActivations: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("superseded activation was not retained: history has %d entries, want 2", len(history))
	}
	if history[0].Revision != 1 || history[0].ActivatedBy != "alice" {
		t.Fatalf("first activation record changed: %+v", history[0])
	}
}

func TestResolveNeverGuesses(t *testing.T) {
	t.Parallel()
	store := NewRegistry()
	scope := Scope{TenantID: "t1"}
	if _, err := Publish(store, newObj(scope, KindWorkflow, "w1", 1, "body")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// Published but never activated: Resolve must refuse rather than
	// falling back to "the only" or "the latest" published revision.
	_, err := Resolve(store, scope, KindWorkflow, "w1")
	if CodeOf(err) != CodeNoActiveRevision {
		t.Fatalf("Resolve on an unactivated id: CodeOf(err) = %q, want %q", CodeOf(err), CodeNoActiveRevision)
	}
	if _, err := Resolve(nil, scope, KindWorkflow, "w1"); CodeOf(err) != CodeNoStore {
		t.Fatalf("Resolve with nil store: CodeOf(err) = %q, want %q", CodeOf(err), CodeNoStore)
	}
}
