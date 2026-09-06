package configregistry

import (
	"testing"
	"time"
)

func TestKindValid(t *testing.T) {
	t.Parallel()
	valid := []Kind{
		KindWorkflow, KindPolicy, KindSchema, KindRule, KindConnector,
		KindAgent, KindReference, KindMapping, KindCapability,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("Kind(%q).Valid() = false, want true", k)
		}
	}
	invalid := []Kind{"", "workflow", "BOGUS", "CONFIG"}
	for _, k := range invalid {
		if k.Valid() {
			t.Errorf("Kind(%q).Valid() = true, want false", k)
		}
	}
}

func TestScopeKeyDistinguishesCellsWithinATenant(t *testing.T) {
	t.Parallel()
	a := Scope{TenantID: "t1", CellID: "c1"}
	b := Scope{TenantID: "t1", CellID: "c2"}
	c := Scope{TenantID: "t1"}
	if a.key() == b.key() {
		t.Fatalf("distinct cells collide: %q", a.key())
	}
	if a.key() == c.key() {
		t.Fatalf("cell-scoped and tenant-wide scopes collide: %q", a.key())
	}
}

func TestScopeValidRequiresTenantID(t *testing.T) {
	t.Parallel()
	if (Scope{}).valid() {
		t.Fatal("empty Scope reports valid")
	}
	if !(Scope{TenantID: "t1"}).valid() {
		t.Fatal("Scope with only TenantID reports invalid")
	}
}

func TestConfigurationObjectDigestAndVerify(t *testing.T) {
	t.Parallel()
	obj := ConfigurationObject{
		Kind:               KindWorkflow,
		ID:                 "onboarding",
		Revision:           1,
		Body:               []byte("body-v1"),
		SchemaRef:          "hcmnext.test.schema/v1",
		Scope:              Scope{TenantID: "tenant-a"},
		PublisherPrincipal: "publisher-1",
		PublishedAt:        time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	obj.CanonicalBodyDigest = computeBodyDigest(obj.Body)
	obj.digest = computeRecordDigest(obj)

	if obj.Digest() == "" {
		t.Fatal("Digest() is empty on a minted object")
	}
	if err := obj.Verify(); err != nil {
		t.Fatalf("Verify() on an untouched object: %v", err)
	}

	tampered := obj
	tampered.SchemaRef = "hcmnext.test.schema/v2"
	if err := tampered.Verify(); err == nil {
		t.Fatal("Verify() on a tampered copy: want error, got nil")
	} else if CodeOf(err) != CodeRecordMutated {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeRecordMutated)
	}
}

func TestRehydrateReproducesDigestAndVerify(t *testing.T) {
	t.Parallel()
	obj := ConfigurationObject{
		Kind:               KindMapping,
		ID:                 "m1",
		Revision:           1,
		Body:               []byte("mapped-body"),
		SchemaRef:          "hcmnext.test.schema/v1",
		Scope:              Scope{TenantID: "tenant-a"},
		PublisherPrincipal: "publisher-1",
		PublishedAt:        time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	obj.CanonicalBodyDigest = computeBodyDigest(obj.Body)
	stored := computeRecordDigest(obj)

	// Simulate a Store adapter outside this package: it read back every
	// exported field plus the digest the durable row itself carries, but
	// has no way to set the unexported digest field directly.
	loaded := ConfigurationObject{
		Kind: obj.Kind, ID: obj.ID, Revision: obj.Revision,
		Body: obj.Body, CanonicalBodyDigest: obj.CanonicalBodyDigest,
		SchemaRef: obj.SchemaRef, Scope: obj.Scope,
		PublisherPrincipal: obj.PublisherPrincipal, PublishedAt: obj.PublishedAt,
	}
	rehydrated, err := Rehydrate(loaded, stored)
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	if rehydrated.Digest() != stored {
		t.Fatalf("Digest() = %s, want %s", rehydrated.Digest(), stored)
	}
	if err := rehydrated.Verify(); err != nil {
		t.Fatalf("Verify() on a rehydrated object: %v", err)
	}
}

func TestRehydrateRefusesMismatchedStoredDigest(t *testing.T) {
	t.Parallel()
	obj := ConfigurationObject{
		Kind: KindMapping, ID: "m1", Revision: 1,
		Body: []byte("body"), SchemaRef: "s/v1",
		Scope: Scope{TenantID: "t1"}, PublisherPrincipal: "p1",
		PublishedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := Rehydrate(obj, "0000000000000000000000000000000000000000000000000000000000000000")
	if CodeOf(err) != CodeRecordMutated {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeRecordMutated)
	}
}

func TestConfigurationObjectCloneIsIndependent(t *testing.T) {
	t.Parallel()
	obj := ConfigurationObject{Body: []byte("original")}
	clone := obj.clone()
	clone.Body[0] = 'X'
	if obj.Body[0] == 'X' {
		t.Fatal("mutating the clone's Body reached the original")
	}
}

func TestConfigurationObjectRef(t *testing.T) {
	t.Parallel()
	obj := ConfigurationObject{
		Kind:     KindPolicy,
		ID:       "p1",
		Revision: 3,
		Scope:    Scope{TenantID: "t1", CellID: "c1"},
	}
	ref := obj.Ref()
	if ref.Kind != KindPolicy || ref.ID != "p1" || ref.Revision != 3 {
		t.Fatalf("Ref() = %+v, unexpected", ref)
	}
	if ref.Scope.TenantID != "t1" || ref.Scope.CellID != "c1" {
		t.Fatalf("Ref().Scope = %+v, unexpected", ref.Scope)
	}
}

func TestActivationRecordRefAndClone(t *testing.T) {
	t.Parallel()
	rec := ActivationRecord{
		Scope:    Scope{TenantID: "t1"},
		Kind:     KindRule,
		ID:       "r1",
		Revision: 2,
	}
	ref := rec.Ref()
	if ref.Kind != KindRule || ref.ID != "r1" || ref.Revision != 2 {
		t.Fatalf("Ref() = %+v, unexpected", ref)
	}
	if clone := rec.clone(); clone != rec {
		t.Fatalf("clone() = %+v, want %+v", clone, rec)
	}
}
