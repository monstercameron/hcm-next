package reciprocity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testKnownAt(t *testing.T, text string) values.KnownAt {
	t.Helper()
	instant, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(instant))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func testDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func testJurisdiction(state string) legal.Jurisdiction {
	return legal.Jurisdiction{Country: "US", State: state}
}

func TestTodo_LEGAL_TOOL_012(t *testing.T) {
	registry, err := LoadFixture()
	if err != nil {
		t.Fatalf("LoadFixture() error = %v", err)
	}
	if err := registry.ValidateActivePairs(testKnownAt(t, "2026-09-05T12:00:00Z"), testDate(t, "2026-09-05")); err != nil {
		t.Fatalf("active pair gate error = %v", err)
	}
	port := NewPort(registry)
	resolution, err := port.Resolve(ResolveRequest{
		Residence:     testJurisdiction("DC"),
		Work:          testJurisdiction("MD"),
		EffectiveDate: testDate(t, "2026-09-05"),
		KnownAt:       testKnownAt(t, "2026-09-05T12:00:00Z"),
	})
	if err != nil {
		t.Fatalf("agreement resolution error = %v", err)
	}
	if resolution.Status != StatusReciprocityAgreement {
		t.Fatalf("status = %s, want %s", resolution.Status, StatusReciprocityAgreement)
	}
	if len(resolution.Conditions) == 0 || len(resolution.Forms) == 0 {
		t.Fatalf("agreement omitted conditions or forms: %+v", resolution)
	}
	_, err = port.Resolve(ResolveRequest{
		Residence:     testJurisdiction("AK"),
		Work:          testJurisdiction("WA"),
		EffectiveDate: testDate(t, "2026-09-05"),
		KnownAt:       testKnownAt(t, "2026-09-05T12:00:00Z"),
	})
	if !errors.Is(err, ErrUnresearchedPair) {
		t.Fatalf("unresearched pair error = %v, want ErrUnresearchedPair", err)
	}
}

func TestTodo_LEGAL_TOOL_012_Golden(t *testing.T) {
	registry, err := LoadFixture()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Digest == "" || registry.Digest != registry.ComputeDigest() {
		t.Fatalf("digest = %q, recomputed = %q", registry.Digest, registry.ComputeDigest())
	}
	want := "reciprocity schema=1 version=1 window=[2026-01-01,) known_at=2026-09-05T12:00:00Z states=51 agreements=3 digest="
	if !strings.HasPrefix(registry.Explain(), want) {
		t.Fatalf("Explain() = %q, want prefix %q", registry.Explain(), want)
	}
}

func TestTodo_LEGAL_TOOL_012_Conformance(t *testing.T) {
	registry, err := LoadFixture()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.StatesSorted()) != 51 {
		t.Fatalf("state rows = %d, want 51 including DC", len(registry.States))
	}
	reviewed := map[string]string{
		"AK": "Alaska has no reciprocity agreements with other states for unemployment insurance or payroll-tax purposes.",
		"CT": "No reciprocity doctrine in Connecticut statute.",
		"DE": "Delaware has no reciprocity with any state.",
		"GA": "No reciprocal income-tax agreement exists between Georgia and other states; employers must withhold separately for each state.",
	}
	for _, row := range registry.States {
		if row.Citation.SourceFile == "" || row.Citation.Section == "" || row.Citation.Authority == "" {
			t.Fatalf("incomplete citation for %s: %+v", row.Code, row.Citation)
		}
		if want, ok := reviewed[row.Code]; ok {
			if row.Status != StatusNoReciprocityConfirmed || row.Review != ReviewReviewed || row.Citation.Authority != want {
				t.Fatalf("reviewed row %s = %+v, want confirmed reviewed citation", row.Code, row)
			}
		}
	}
}
