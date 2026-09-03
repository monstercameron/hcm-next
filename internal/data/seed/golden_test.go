package seed_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/seed"
)

// TestTodo_DB_019_Golden pins the exact shape and digest of the Promotion
// fixture plan: the embedded corpus (internal/data/seed/testdata) is fixed,
// so Plan reads no wall clock and no randomness, and this value must never
// change unless that corpus does.
func TestTodo_DB_019_Golden(t *testing.T) {
	t.Parallel()
	plan, err := seed.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	const wantCount = 21
	if len(plan) != wantCount {
		t.Fatalf("plan holds %d registrations, want %d", len(plan), wantCount)
	}

	wantKindCounts := map[string]int{
		"SCHEMA":       1,
		"CAPABILITY":   1,
		"INTENT":       1,
		"CONFIG":       6, // the band catalog as a whole, plus one per band
		"ENTITY":       8, // person + position, four workers each
		"RELATIONSHIP": 4, // employment, one per worker
	}
	gotKindCounts := map[string]int{}
	for _, r := range plan {
		gotKindCounts[r.Kind]++
	}
	for kind, want := range wantKindCounts {
		if got := gotKindCounts[kind]; got != want {
			t.Errorf("plan holds %d %s registrations, want %d", got, kind, want)
		}
	}

	const wantDigest = "4f5fc52ae2489688893404934d2759991a12ed36f0c159ae9298325c6c4888a0"
	if got := seed.ArtifactDigest(plan); got != wantDigest {
		t.Fatalf("plan digest is %s, want %s (the embedded corpus changed, or Plan's ordering/content did)",
			got, wantDigest)
	}

	// Calling Plan twice must reproduce the identical digest: this is the
	// reproducibility property DB-019 requires, checked independently of the
	// pinned literal above.
	plan2, err := seed.Plan()
	if err != nil {
		t.Fatalf("plan (second call): %v", err)
	}
	if got := seed.ArtifactDigest(plan2); got != wantDigest {
		t.Fatalf("a second Plan() call produced digest %s, want %s", got, wantDigest)
	}
}
