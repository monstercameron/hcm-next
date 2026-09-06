package search_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/search"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestAllowAllDisclosesEveryValidSubject holds doc.go's claim about
// [search.AllowAll]: it is a [search.Discloser] that never itself withholds
// a syntactically valid subject, so tests and tenant-wide-grant callers can
// use it without writing their own trivial adapter.
func TestAllowAllDisclosesEveryValidSubject(t *testing.T) {
	t.Parallel()
	subject := values.EntityRef{Tenant: "acme-corp", Kind: values.Kind(search.KindWorker), Id: "018f5a2e-6b3a-7c3a-8b7a-1a2b3c4d5e6f"}
	got, err := search.AllowAll.Disclosable(context.Background(), subject)
	if err != nil {
		t.Fatalf("AllowAll.Disclosable: %v", err)
	}
	if !got {
		t.Fatal("AllowAll.Disclosable() = false, want true")
	}
}

// TestDiscloserFuncAdapts proves [search.DiscloserFunc] really implements
// [search.Discloser] by calling through the interface.
func TestDiscloserFuncAdapts(t *testing.T) {
	t.Parallel()
	var called bool
	var d search.Discloser = search.DiscloserFunc(func(context.Context, values.EntityRef) (bool, error) {
		called = true
		return false, nil
	})
	got, err := d.Disclosable(context.Background(), values.EntityRef{})
	if err != nil {
		t.Fatalf("Disclosable: %v", err)
	}
	if got {
		t.Fatal("Disclosable() = true, want false")
	}
	if !called {
		t.Fatal("DiscloserFunc did not call through to the wrapped function")
	}
}
