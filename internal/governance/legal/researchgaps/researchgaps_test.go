package researchgaps

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func gapDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func gapKnownAt(t *testing.T) values.KnownAt {
	t.Helper()
	instant, err := values.NewInstantFromUnix(1788609600, 0)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(instant)
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func TestTodo_LEGAL_TOOL_013(t *testing.T) {
	registry, err := LoadFixture()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 6 {
		t.Fatalf("entries = %d, want Oregon plus five named cities", len(registry.Entries))
	}
	for _, entry := range registry.Entries {
		if entry.Status != StatusResearchMissing || entry.Review != ReviewUnreviewed {
			t.Fatalf("entry %s = status %s review %s, want research missing/unreviewed", entry.ID, entry.Status, entry.Review)
		}
		if len(entry.RequiredFields) < 6 {
			t.Fatalf("entry %s does not describe a complete future pack shape", entry.ID)
		}
	}
	oregon, err := registry.Lookup("OR", gapDate(t, "2026-09-05"), gapKnownAt(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(oregon.Citation.Authority, "ORS 653.870") {
		t.Fatalf("Oregon citation = %q, want ORS 653.870", oregon.Citation.Authority)
	}
	err = registry.ValidatePublication(PublicationEvidence{
		JurisdictionID:   "OR",
		ResearchFile:     oregon.ResearchFile,
		ReviewedResearch: false,
	}, gapDate(t, "2026-09-05"), gapKnownAt(t))
	if !errors.Is(err, ErrPackBlocked) {
		t.Fatalf("publication error = %v, want ErrPackBlocked", err)
	}
}

func TestTodo_LEGAL_TOOL_013_Golden(t *testing.T) {
	registry, err := LoadFixture()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Digest == "" || registry.Digest != registry.ComputeDigest() {
		t.Fatalf("digest = %q, recomputed = %q", registry.Digest, registry.ComputeDigest())
	}
	want := "researchgaps schema=1 version=1 window=[2026-01-01,) known_at=2026-09-05T12:00:00Z entries=6 digest="
	if !strings.HasPrefix(registry.Explain(), want) {
		t.Fatalf("Explain() = %q, want prefix %q", registry.Explain(), want)
	}
}
