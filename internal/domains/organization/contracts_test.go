package organization

import (
	"errors"
	"testing"
	"time"
)

func org(id string) OrganizationUnit {
	return OrganizationUnit{ID: id, Tenant: "t1", Name: id, Type: "TEAM", EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func edge(id, from, to string) RelationshipEdge {
	return RelationshipEdge{ID: id, Tenant: "t1", Source: from, Target: to, Type: Hierarchy, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func TestReadAsOfAndScopeClosure(t *testing.T) {
	s := Snapshot{Tenant: "t1", Watermark: "org:v7", ResolverPolicyVersion: "policy:v2", Units: []OrganizationUnit{org("root"), org("child"), org("other")}, Edges: []RelationshipEdge{edge("e1", "root", "child")}}
	r, err := Read(s, ReadRequest{Tenant: "t1", Root: "child", AsOf: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || r.Watermark != "org:v7" {
		t.Fatalf("result=%+v", r)
	}
}
func TestValidateRejectsCycleDuplicateAndCrossTenant(t *testing.T) {
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b")}, Edges: []RelationshipEdge{edge("1", "a", "b"), edge("2", "b", "a")}}).Validate(at); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle err=%v", err)
	}
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b"), org("c")}, Edges: []RelationshipEdge{edge("1", "a", "b"), edge("2", "c", "b")}}).Validate(at); !errors.Is(err, ErrDuplicateParent) {
		t.Fatalf("duplicate err=%v", err)
	}
	x := edge("1", "a", "b")
	x.Tenant = "t2"
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b")}, Edges: []RelationshipEdge{x}}).Validate(at); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("tenant err=%v", err)
	}
}
func TestReadFailsClosedAuthorization(t *testing.T) {
	s := Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("root"), org("child")}, Edges: []RelationshipEdge{edge("e", "root", "child")}}
	_, err := Read(s, ReadRequest{Tenant: "t1", Root: "root", AsOf: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Authorize: func(n OrganizationUnit) bool { return n.ID == "root" }})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Read(s, ReadRequest{Tenant: "t1", Root: "child", AsOf: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Authorize: func(n OrganizationUnit) bool { return false }})
	if len(r.Units) != 0 {
		t.Fatalf("unauthorized units leaked: %+v", r.Units)
	}
}
