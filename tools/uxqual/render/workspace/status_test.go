package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

func TestStatusRegionPolite(t *testing.T) {
	out := renderNode(t, StatusRegion(pagedef.LiveRegionPolite))
	for _, want := range []string{`id="` + StatusElementID + `"`, `role="status"`, `aria-live="polite"`, `aria-atomic="true"`} {
		if !strings.Contains(out, want) {
			t.Errorf("StatusRegion(polite) missing %q: %s", want, out)
		}
	}
}

func TestStatusRegionAssertive(t *testing.T) {
	out := renderNode(t, StatusRegion(pagedef.LiveRegionAssertive))
	for _, want := range []string{`role="alert"`, `aria-live="assertive"`} {
		if !strings.Contains(out, want) {
			t.Errorf("StatusRegion(assertive) missing %q: %s", want, out)
		}
	}
}

func TestStatusRegionOffRendersNothing(t *testing.T) {
	out := renderNode(t, StatusRegion(pagedef.LiveRegionOff))
	if out != "" {
		t.Errorf("StatusRegion(off) = %q, want an empty render", out)
	}
}
