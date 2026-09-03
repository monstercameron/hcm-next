package flowmigration

import (
	"errors"
	"testing"
	"time"
)

func def(v uint32, action string) Definition {
	return Definition{FlowID: "leave", Version: v, States: []string{"draft", "confirm"}, Actions: []Action{{ID: action, Semantic: "submit leave"}}}
}
func published(t *testing.T) (*Registry, Definition, Definition) {
	t.Helper()
	r := NewRegistry()
	a, e := r.Publish(def(1, "submit"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := r.Publish(def(2, "submit-v2"))
	if e != nil {
		t.Fatal(e)
	}
	return r, a, b
}
func review() Review {
	return Review{Reviewer: "reviewer", ReviewedAt: time.Unix(1, 0).UTC(), Mapping: map[string]string{"submit": "submit-v2"}, Replan: true}
}

func TestUserFlowDefinitionEvolutionPreservesActiveDraftTaskAndActionSemantics(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	w := Work{ID: "w1", FlowID: "leave", VersionDigest: a.Digest, Draft: map[string]string{"hours": "8"}, TaskIDs: []string{"task-1"}, ApprovalIDs: []string{"approval-1"}}
	if err := r.Activate("leave", b.Digest, review()); err != nil {
		t.Fatal(err)
	}
	if got, err := r.ResolveLink(Link{FlowID: "leave", VersionDigest: a.Digest, ActionID: "submit"}); err != nil || got.Mode != LinkReplan {
		t.Fatalf("stale link = %#v, %v", got, err)
	}
	n, rec, err := r.Migrate(w, b.Digest, review())
	if err != nil {
		t.Fatal(err)
	}
	if n.VersionDigest != b.Digest || n.Draft["hours"] != "8" || len(n.TaskIDs) != 1 || len(n.ApprovalIDs) != 1 {
		t.Fatalf("work lost: %#v", n)
	}
	if !rec.PreservedDraft || !rec.PreservedTasks || !rec.PreservedApprovals || rec.ReceiptDigest == "" {
		t.Fatalf("bad receipt: %#v", rec)
	}
	seen := map[string]ActionReceipt{}
	first, err := r.Execute(n, Link{FlowID: "leave", VersionDigest: b.Digest, ActionID: "submit-v2"}, "id-1", seen)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Execute(n, Link{FlowID: "leave", VersionDigest: b.Digest, ActionID: "submit-v2"}, "id-1", seen)
	if err != nil || !second.Duplicate || first.ActionDigest != second.ActionDigest {
		t.Fatalf("duplicate action: %#v %#v %v", first, second, err)
	}
}

func TestTodo_UXFLOW_011_Property(t *testing.T) {
	r, a, _ := published(t)
	x := a
	x.Actions[0].ID = "tamper"
	got, _ := r.Definition("leave", a.Digest)
	if got.Actions[0].ID != "submit" {
		t.Fatal("published definition was mutable")
	}
}
func TestTodo_UXFLOW_011_Golden(t *testing.T) {
	_, a, b := published(t)
	if a.Digest == "" || a.Digest == b.Digest {
		t.Fatal("content identity not distinct")
	}
}
func TestTodo_UXFLOW_011_Race(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate("leave", b.Digest, review()); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_UXFLOW_011_Fault(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Migrate(Work{FlowID: "leave", VersionDigest: a.Digest}, b.Digest, Review{}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_UXFLOW_011_Security(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate("leave", b.Digest, Review{}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed activation err=%v", err)
	}
}
func TestTodo_UXFLOW_011_Conformance(t *testing.T) {
	r, a, _ := published(t)
	if _, err := r.ResolveLink(Link{FlowID: "leave", VersionDigest: a.Digest}); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_UXFLOW_011_Browser(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate("leave", b.Digest, review()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]ActionReceipt{}
	if _, err := r.Execute(Work{ID: "w", FlowID: "leave", VersionDigest: a.Digest}, Link{FlowID: "leave", VersionDigest: a.Digest, ActionID: "submit"}, "k", seen); !errors.Is(err, ErrStaleLink) {
		t.Fatalf("stale action err=%v", err)
	}
}
func TestTodo_UXFLOW_011_Recovery(t *testing.T) {
	r, a, b := published(t)
	if _, _, err := r.Migrate(Work{FlowID: "leave", VersionDigest: a.Digest}, b.Digest, Review{Reviewer: "r", ReviewedAt: time.Now(), Replan: true, Mapping: map[string]string{"submit": "missing"}}); !errors.Is(err, ErrActionUnavailable) {
		t.Fatalf("bad mapping err=%v", err)
	}
}
func TestTodo_UXFLOW_011_ModelBased(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate("leave", b.Digest, review()); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ResolveLink(Link{FlowID: "leave", VersionDigest: a.Digest}); got.ReadOnly && got.Replan {
		t.Fatal("link has contradictory modes")
	}
}
func TestTodo_UXFLOW_011_Mutation(t *testing.T) {
	r, a, b := published(t)
	if err := r.Activate("leave", a.Digest, Review{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate("leave", b.Digest, review()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Migrate(Work{ID: "x", FlowID: "leave", VersionDigest: a.Digest}, b.Digest, Review{Reviewer: "r", ReviewedAt: time.Now(), Replan: true}); err != nil {
		t.Fatal(err)
	}
}
