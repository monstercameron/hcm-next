package productslice

import (
	"strings"
	"testing"
)

func TestProductSliceDefinitionDigestStableAcrossListOrder(t *testing.T) {
	a := validFixtureSlice()
	a.BusinessIntents = []string{"hcmnext.people.promote_worker/v1", "hcmnext.rewards.simulate_compensation/v1"}

	b := a.clone()
	b.BusinessIntents = []string{"hcmnext.rewards.simulate_compensation/v1", "hcmnext.people.promote_worker/v1"}

	if a.Digest() != b.Digest() {
		t.Fatalf("digest depends on reference list order: %s vs %s", a.Digest(), b.Digest())
	}
}

func TestProductSliceDefinitionDigestChangesWithContent(t *testing.T) {
	a := validFixtureSlice()
	b := a.clone()
	b.Version = 2

	if a.Digest() == b.Digest() {
		t.Fatal("digest did not change when version changed")
	}
}

func TestProductSliceDefinitionDigestIsSHA256(t *testing.T) {
	d := validFixtureSlice().Digest()
	if !strings.HasPrefix(d, "sha256:") {
		t.Fatalf("digest %q does not carry the sha256: prefix", d)
	}
	if len(strings.TrimPrefix(d, "sha256:")) != 64 {
		t.Fatalf("digest %q is not a 64-hex-character sha256 sum", d)
	}
}

func TestProductSliceDefinitionExplainIsBounded(t *testing.T) {
	explain := validFixtureSlice().Explain()
	if !strings.Contains(explain, "promotion@1") {
		t.Errorf("Explain() = %q, want it to name the slice id and version", explain)
	}
	if !strings.Contains(explain, "sha256:") {
		t.Errorf("Explain() = %q, want it to name the digest", explain)
	}
	// Explain must never enumerate individual refs -- it stays a bounded
	// summary regardless of how large a slice's lists grow.
	if strings.Contains(explain, "promotion.journeys.list") {
		t.Errorf("Explain() = %q, unexpectedly enumerated a page ref", explain)
	}
}
