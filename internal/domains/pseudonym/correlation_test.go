package pseudonym_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/pseudonym"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

func queryContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "analytics-worker", Tenant: "tenant-1", Region: "us-east-1", Purpose: "analytics", Destination: "aggregate"}}
}

func queryPseudonym(id, scope string) pseudonym.Pseudonym {
	return pseudonym.Pseudonym{ID: id, Value: id, Generation: 1, Tenant: "tenant-1", Scope: scope, Purpose: "analytics"}
}

func querySurface(t *testing.T, budget int) *pseudonym.QuerySurface {
	t.Helper()
	surface, err := pseudonym.NewQuerySurface(pseudonym.QueryConfig{MinimumCohortSize: 2, QueriesPerPrincipal: budget, Window: time.Hour, Clock: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	return surface
}

// TestTodo_ANON_005 proves membership probes are indistinguishable for
// present and absent pseudonyms, while safe aggregates remain cohort-bounded.
func TestTodo_ANON_005(t *testing.T) {
	surface := querySurface(t, 10)
	present := queryPseudonym("psn-present", "program-a")
	absent := queryPseudonym("psn-absent", "program-a")
	if err := surface.Register(present); err != nil {
		t.Fatal(err)
	}
	ctx := queryContext()
	presentResult, presentErr := surface.Query(ctx, pseudonym.QueryRequest{Kind: pseudonym.QueryMembership, Principal: "analyst", Scope: "program-a", Pseudonym: present.ID})
	absentResult, absentErr := surface.Query(ctx, pseudonym.QueryRequest{Kind: pseudonym.QueryMembership, Principal: "analyst", Scope: "program-a", Pseudonym: absent.ID})
	if !errors.Is(presentErr, pseudonym.ErrExistenceInference) || !errors.Is(absentErr, pseudonym.ErrExistenceInference) {
		t.Fatalf("membership errors = %v, %v", presentErr, absentErr)
	}
	if !reflect.DeepEqual(presentResult, absentResult) {
		t.Fatalf("membership oracle results differ: %+v vs %+v", presentResult, absentResult)
	}
	aggregate, err := surface.Query(ctx, pseudonym.QueryRequest{Kind: pseudonym.QueryAggregate, Principal: "analyst", Scope: "program-a", Cohort: []string{present.ID, absent.ID}})
	if err != nil || aggregate.Decision != pseudonym.QueryAllowed || aggregate.Count != 1 || aggregate.CohortSize != 2 {
		t.Fatalf("aggregate = %+v, %v", aggregate, err)
	}
	if _, err := surface.Query(ctx, pseudonym.QueryRequest{Kind: pseudonym.QueryAggregate, Principal: "analyst", Scope: "program-a", Cohort: []string{present.ID}}); !errors.Is(err, pseudonym.ErrCohortTooSmall) {
		t.Fatalf("small cohort err = %v", err)
	}
	if got := len(surface.Evidence()); got != 3 {
		t.Fatalf("refusal evidence count = %d, want 3", got)
	}
}

// TestTodo_ANON_005_Security proves cross-scope joins and repeated queries
// are refused and every refusal has digest-only evidence.
func TestTodo_ANON_005_Security(t *testing.T) {
	surface := querySurface(t, 2)
	ctx := queryContext()
	first := queryPseudonym("psn-one", "program-a")
	if err := surface.Register(first); err != nil {
		t.Fatal(err)
	}
	join := pseudonym.QueryRequest{Kind: pseudonym.QueryJoin, Principal: "analyst", Scope: "program-a", Scopes: []string{"program-a", "program-b"}, Cohort: []string{first.ID, "psn-two"}}
	if _, err := surface.Query(ctx, join); !errors.Is(err, pseudonym.ErrCrossScopeJoin) {
		t.Fatalf("cross-scope join err = %v", err)
	}
	aggregate := pseudonym.QueryRequest{Kind: pseudonym.QueryAggregate, Principal: "analyst", Scope: "program-a", Cohort: []string{first.ID, "psn-two"}}
	if _, err := surface.Query(ctx, aggregate); err != nil {
		t.Fatal(err)
	}
	if _, err := surface.Query(ctx, aggregate); !errors.Is(err, pseudonym.ErrQueryBudget) {
		t.Fatalf("budget err = %v", err)
	}
	evidence := surface.Evidence()
	if len(evidence) != 2 {
		t.Fatalf("evidence = %+v, want two refusals", evidence)
	}
	for _, item := range evidence {
		if item.Digest == "" || item.RequestDigest == "" || strings.Contains(item.RequestDigest, first.ID) {
			t.Fatalf("unsafe evidence = %+v", item)
		}
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), first.ID) || strings.Contains(pseudonym.ExplainCorrelation(), first.ID) {
		t.Fatal("query evidence or Explain carried a pseudonym")
	}
}
