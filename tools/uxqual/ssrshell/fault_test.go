package ssrshell

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// TestTodo_WEB_025_Fault proves this package fails closed and legibly, not
// by panicking, when it is handed a Region whose Kind is not in
// landmarksByKind's mapping. pagedef.Validate already refuses any RegionKind
// outside its own closed vocabulary, so this should never reach [Render] in
// production; the fault this test covers is the two vocabularies (pagedef's
// and this package's) drifting apart -- a new RegionKind added to pagedef
// without a corresponding entry here. That must be a clear, immediate
// error from this package, not an unmapped region silently rendering as
// nothing or a nil-pointer panic somewhere inside html/template.
func TestTodo_WEB_025_Fault(t *testing.T) {
	t.Run("an undocumented RegionKind produces a named error, not a panic", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions = append(pd.Regions, pagedef.Region{
			ID:   "mystery",
			Kind: pagedef.RegionKind("mystery_kind_not_in_the_vocabulary"),
		})

		// Render itself would refuse this via pagedef.Validate before ever
		// reaching this package's own landmark-mapping code -- confirm
		// that first, since it is the primary defense.
		if _, err := Render(pd); err == nil {
			t.Fatalf("Render() with an unknown RegionKind = nil error, want pagedef.Validate to refuse it")
		}

		// Then prove this package's own defense: even if that validation
		// were somehow skipped, renderValid must fail cleanly rather than
		// panic or silently drop the region.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("renderValid panicked on an undocumented RegionKind instead of returning an error: %v", r)
				}
			}()
			_, err := renderValid(pd)
			if err == nil {
				t.Fatalf("renderValid(pd) with an undocumented RegionKind = nil error, want a named refusal")
			}
			if !strings.Contains(err.Error(), "mystery") {
				t.Errorf("error %q does not name the offending region", err.Error())
			}
			if !strings.Contains(err.Error(), "mystery_kind_not_in_the_vocabulary") {
				t.Errorf("error %q does not name the offending kind", err.Error())
			}
		}()
	})

	t.Run("zero regions does not panic (Validate refuses it before Render, but renderValid alone must still fail closed, not crash)", func(t *testing.T) {
		pd := validMinimalPage()
		pd.Regions = nil

		if _, err := Render(pd); err == nil {
			t.Fatalf("Render() with zero regions = nil error, want pagedef.Validate to refuse it")
		}

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("renderValid panicked on zero regions instead of returning a document or a clean error: %v", r)
			}
		}()
		rs, err := renderValid(pd)
		if err != nil {
			// A document with no regions at all is a legitimate, if
			// unusual, thing for the lower-level function to produce
			// without crashing; either outcome is acceptable here as long
			// as it is not a panic.
			t.Logf("renderValid(pd) with zero regions returned an error (acceptable): %v", err)
			return
		}
		if !strings.Contains(rs.HTML, "<!doctype html>") {
			t.Errorf("renderValid(pd) with zero regions produced a non-document: %q", rs.HTML)
		}
	})

	t.Run("a region with no heading and no widgets renders its landmark with an empty body, not an error", func(t *testing.T) {
		pd := validMinimalPage()
		// The real "shell" region in validMinimalPage already has no
		// heading and no widgets; this is asserting that this is a normal
		// case, not a fault, distinguishing it from the true fault cases
		// above.
		rs, err := Render(pd)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if !strings.Contains(rs.HTML, `<header id="region-shell"`) {
			t.Errorf("expected the heading-less, widget-less shell region to still render its header landmark")
		}
	})
}
