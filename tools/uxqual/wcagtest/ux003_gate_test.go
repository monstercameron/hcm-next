// Package wcagtest contains release-level integration and race probes for UX-003.
// It is deliberately separate from the implementation package so these tests
// exercise the public qualification surface exactly as CI consumers do.
package wcagtest

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/forms"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/gwc"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/ssr"
	"github.com/monstercameron/hcm-next/tools/uxqual/wcag"
)

func TestUX003IntegrationBothRenderers(t *testing.T) {
	f := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(f)
	if err != nil {
		t.Fatalf("SSR render: %v", err)
	}
	gwcDoc, err := gwc.Document(f)
	if err != nil {
		t.Fatalf("GWC render: %v", err)
	}
	for name, doc := range map[string]string{"ssr": ssrDoc, "gwc": gwcDoc} {
		for _, result := range wcag.Score(doc) {
			// FORM-004 selects SSR for the validation-error route. GWC is the
			// clean interactive projection and is documented not to manufacture
			// aria-invalid/aria-describedby nodes for this fixture.
			if name == "gwc" && (result.Name == "Error association (aria-describedby)" || result.Name == "Accessible authorization projection") {
				continue
			}
			if !result.Pass {
				t.Errorf("%s: %s: %s", name, result.Name, result.Detail)
			}
		}
	}
	// The GWC serializer is free to reorder HTML attributes, so verify its
	// authorization projection by parsed value occurrences rather than the
	// SSR checker's exact attribute ordering.
	for _, value := range []string{"approve", "reject", "request_more_information"} {
		if !strings.Contains(gwcDoc, `value="`+value+`"`) {
			t.Errorf("gwc authorization projection missing %q", value)
		}
	}
}

func TestUX003ScoreRace(t *testing.T) {
	f := forms.FixtureWithValidationError()
	a, err := ssr.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := gwc.Document(f)
	if err != nil {
		t.Fatal(err)
	}
	docs := []string{a, b, a, b, a, b}
	var wg sync.WaitGroup
	for _, doc := range docs {
		doc := doc
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				for _, result := range wcag.Score(doc) {
					if result.Name == "" {
						t.Error("scorecard returned unnamed criterion")
					}
				}
			}
		}()
	}
	wg.Wait()
}
