package seed_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/seed"
)

// TestTodo_DB_019_Golden pins the exact shape and digest of the Promotion
// fixture plan: the embedded corpus (internal/data/seed/testdata) is fixed,
// so Plan reads no wall clock and no randomness, and this value must never
// change unless that corpus does.
//
// The plan shrank from 21 to 5 registrations, and the digest changed with
// it, on 2026-09-03: DB-019's follow-up re-pointed the seeder at the
// person/employment/position/compensation aggregate tables migrations
// 00011-00013 now provide (see seed.go's package doc), so the per-worker
// ENTITY (person/position) and RELATIONSHIP (employment) registrations and
// the per-band CONFIG registrations no longer exist here -- those facts now
// live in the physical aggregate tables, loaded via aggregates.LoadFixtures
// -- leaving only the corpus-level SCHEMA/CAPABILITY/INTENT registrations,
// the whole-catalog CONFIG reference row, and the new aggregateCorpusKey
// SCHEMA marker that gates the one LoadFixtures call.
func TestTodo_DB_019_Golden(t *testing.T) {
	t.Parallel()
	plan, err := seed.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	const wantCount = 5
	if len(plan) != wantCount {
		t.Fatalf("plan holds %d registrations, want %d", len(plan), wantCount)
	}

	wantKindCounts := map[string]int{
		"SCHEMA":     2, // the corpus registration, plus the aggregate-corpus marker
		"CAPABILITY": 1,
		"INTENT":     1,
		"CONFIG":     1, // the band catalog as a whole; individual bands are aggregate rows now
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
	if len(wantKindCounts) != len(gotKindCounts) {
		t.Errorf("plan holds kinds %v, want exactly %v", gotKindCounts, wantKindCounts)
	}

	const wantDigest = "18222b3a4c458b0eb2fa205ebfc81be71b3d41d995b6e076e816d75acae79811"
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
