package configregistry

import (
	"testing"
	"time"
)

func testObject(scope Scope, kind Kind, id string, revision uint32, body string) ConfigurationObject {
	o := ConfigurationObject{
		Kind:               kind,
		ID:                 id,
		Revision:           revision,
		Body:               []byte(body),
		SchemaRef:          "schema/v1",
		Scope:              scope,
		PublisherPrincipal: "pub-1",
		PublishedAt:        time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
	o.CanonicalBodyDigest = computeBodyDigest(o.Body)
	o.digest = computeRecordDigest(o)
	return o
}

func TestRegistryPutAndGetObject(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	scope := Scope{TenantID: "t1"}
	obj := testObject(scope, KindWorkflow, "w1", 1, "body")

	if err := r.PutObject(obj); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	got, found, err := r.GetObject(obj.Ref())
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if !found {
		t.Fatal("GetObject: not found after PutObject")
	}
	if got.CanonicalBodyDigest != obj.CanonicalBodyDigest {
		t.Fatalf("GetObject returned a different record: %+v", got)
	}

	if _, found, err := r.GetObject(ObjectRef{Scope: scope, Kind: KindWorkflow, ID: "unknown", Revision: 1}); err != nil {
		t.Fatalf("GetObject(unknown): %v", err)
	} else if found {
		t.Fatal("GetObject reported found for a key never put")
	}
}

func TestRegistryPutObjectRejectsMissingIdentity(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if err := r.PutObject(ConfigurationObject{Scope: Scope{TenantID: "t1"}}); CodeOf(err) != CodeMissingID {
		t.Fatalf("PutObject with no id: CodeOf(err) = %q, want %q", CodeOf(err), CodeMissingID)
	}
	if err := r.PutObject(ConfigurationObject{ID: "x"}); CodeOf(err) != CodeMissingScope {
		t.Fatalf("PutObject with no scope: CodeOf(err) = %q, want %q", CodeOf(err), CodeMissingScope)
	}
}

func TestRegistryGetObjectReturnsIndependentCopies(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	scope := Scope{TenantID: "t1"}
	obj := testObject(scope, KindWorkflow, "w1", 1, "body")
	if err := r.PutObject(obj); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	got, _, _ := r.GetObject(obj.Ref())
	got.Body[0] = 'X'
	got2, _, _ := r.GetObject(obj.Ref())
	if got2.Body[0] == 'X' {
		t.Fatal("mutating a returned object reached the registry's own storage")
	}
}

func TestRegistryListRevisionsIsOldestFirst(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	scope := Scope{TenantID: "t1"}
	one := testObject(scope, KindWorkflow, "w1", 1, "v1")
	two := testObject(scope, KindWorkflow, "w1", 2, "v2")
	three := testObject(scope, KindWorkflow, "w1", 3, "v3")
	for _, o := range []ConfigurationObject{three, one, two} {
		if err := r.PutObject(o); err != nil {
			t.Fatalf("PutObject: %v", err)
		}
	}
	// revisionOrder tracks insertion order (three, one, two), not numeric
	// revision order — ListRevisions is a publish-order log, not a sort.
	got, err := r.ListRevisions(scope, KindWorkflow, "w1")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListRevisions returned %d entries, want 3", len(got))
	}
	if got[0].Revision != 3 || got[1].Revision != 1 || got[2].Revision != 2 {
		t.Fatalf("ListRevisions order = %v, want insertion order [3,1,2]", []uint32{got[0].Revision, got[1].Revision, got[2].Revision})
	}
}

func TestRegistryListRevisionsUnknownGroupIsEmptyNotError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	got, err := r.ListRevisions(Scope{TenantID: "t1"}, KindWorkflow, "never-published")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListRevisions = %v, want empty", got)
	}
}

func TestRegistryActivationHistoryAppendsAndNeverDeletes(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	scope := Scope{TenantID: "t1"}
	first := ActivationRecord{Scope: scope, Kind: KindPolicy, ID: "p1", Revision: 1, ActivatedBy: "alice", ActivatedAt: time.Unix(1, 0)}
	second := ActivationRecord{Scope: scope, Kind: KindPolicy, ID: "p1", Revision: 2, ActivatedBy: "bob", ActivatedAt: time.Unix(2, 0)}

	if err := r.PutActivation(first); err != nil {
		t.Fatalf("PutActivation(first): %v", err)
	}
	if err := r.PutActivation(second); err != nil {
		t.Fatalf("PutActivation(second): %v", err)
	}

	latest, found, err := r.GetLatestActivation(scope, KindPolicy, "p1")
	if err != nil {
		t.Fatalf("GetLatestActivation: %v", err)
	}
	if !found || latest.Revision != 2 {
		t.Fatalf("GetLatestActivation = %+v, found=%v, want revision 2", latest, found)
	}

	history, err := r.ListActivations(scope, KindPolicy, "p1")
	if err != nil {
		t.Fatalf("ListActivations: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("ListActivations returned %d records, want 2 (superseded record must remain)", len(history))
	}
	if history[0].Revision != 1 || history[1].Revision != 2 {
		t.Fatalf("ListActivations order = %+v, want [1,2] oldest first", history)
	}
}

func TestRegistryGetLatestActivationUnknownGroup(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	_, found, err := r.GetLatestActivation(Scope{TenantID: "t1"}, KindPolicy, "never-activated")
	if err != nil {
		t.Fatalf("GetLatestActivation: %v", err)
	}
	if found {
		t.Fatal("GetLatestActivation reported found for a group with no activations")
	}
}

func TestRegistryPutActivationRejectsMissingID(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if err := r.PutActivation(ActivationRecord{}); CodeOf(err) != CodeMissingID {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeMissingID)
	}
}

func TestRegistryImplementsStore(t *testing.T) {
	t.Parallel()
	var _ Store = NewRegistry()
}
