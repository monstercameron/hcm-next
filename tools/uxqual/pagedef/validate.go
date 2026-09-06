package pagedef

import (
	"fmt"
	"regexp"
	"strings"
)

// Violation names one field that fails WEB-002's contract and why.
type Violation struct {
	Path   string
	Reason string
}

// Error satisfies the error interface so a Violation can be returned,
// wrapped, or logged like any other error.
func (v Violation) Error() string {
	return v.Path + ": " + v.Reason
}

// validLandmarks is the closed ARIA landmark-role vocabulary
// Accessibility.Landmarks is checked against.
var validLandmarks = map[string]bool{
	"banner":        true,
	"navigation":    true,
	"main":          true,
	"complementary": true,
	"contentinfo":   true,
	"search":        true,
	"form":          true,
	"region":        true,
}

// brandTokenPattern requires a dotted, lowercase, namespaced identifier
// (e.g. "brand.color.primary"): a reference to a semantic token, never a
// bare word that could be mistaken for a raw value.
var brandTokenPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

// hasForbiddenMarkup reports whether s could be, or could introduce, raw
// HTML or script: any angle bracket (which is all raw HTML/XML tagging
// needs) or a "javascript:" URI scheme. It is deliberately conservative --
// a page definition field never has a legitimate reason to carry either --
// rather than trying to parse and allowlist a safe subset of markup.
func hasForbiddenMarkup(s string) bool {
	if strings.ContainsAny(s, "<>") {
		return true
	}
	return strings.Contains(strings.ToLower(s), "javascript:")
}

// Validate checks pd against every WEB-002 structural rule: no free HTML or
// script in any text field, every region kind in the closed vocabulary,
// every widget referenced by id only, every data binding and action naming
// a real registered RPC (see [KnownRPCs]), every action carrying a required
// role, heading order monotonic across the page, and the accessibility and
// brand-token requirements present and well-formed.
//
// It returns every violation found (nil when pd is valid), not just the
// first, so a page author or a CI check sees the whole list in one pass.
func (pd PageDefinition) Validate() []Violation {
	var out []Violation
	add := func(path, format string, args ...any) {
		out = append(out, Violation{Path: path, Reason: fmt.Sprintf(format, args...)})
	}
	checkText := func(path, s string) {
		if hasForbiddenMarkup(s) {
			add(path, "must not contain HTML markup or a javascript: URI, got %q", s)
		}
	}

	if pd.PageID == "" {
		add("page_id", "required")
	}
	checkText("page_id", pd.PageID)

	if pd.Version < 1 {
		add("version", "must be >= 1, got %d", pd.Version)
	}

	if pd.FloorplanRef == "" {
		add("floorplan_ref", "required")
	}
	checkText("floorplan_ref", pd.FloorplanRef)

	if len(pd.Regions) == 0 {
		add("regions", "at least one region is required")
	}

	knownRPCs := KnownRPCs()
	seenRegionIDs := map[string]bool{}
	hasPrimary := false
	var headingLevels []int

	for i, r := range pd.Regions {
		rp := fmt.Sprintf("regions[%d]", i)

		if r.ID == "" {
			add(rp+".id", "required")
		}
		checkText(rp+".id", r.ID)
		if r.ID != "" && seenRegionIDs[r.ID] {
			add(rp+".id", "duplicate region id %q", r.ID)
		}
		seenRegionIDs[r.ID] = true

		if _, ok := regionKindDocs[r.Kind]; !ok {
			add(rp+".kind", "region kind %q is not in the closed vocabulary", r.Kind)
		}
		if r.Kind == RegionPrimary {
			hasPrimary = true
		}

		if r.Heading != nil {
			if r.Heading.Level < 1 || r.Heading.Level > 6 {
				add(rp+".heading.level", "must be 1-6, got %d", r.Heading.Level)
			} else {
				headingLevels = append(headingLevels, r.Heading.Level)
			}
			checkText(rp+".heading.text", r.Heading.Text)
		}

		for j, w := range r.Widgets {
			wp := fmt.Sprintf("%s.widgets[%d]", rp, j)
			if w.ID == "" {
				add(wp+".id", "required")
			}
			checkText(wp+".id", w.ID)
			if w.WidgetRef == "" {
				add(wp+".widget_ref", "required")
			}
			checkText(wp+".widget_ref", w.WidgetRef)
		}

		for j, b := range r.Bindings {
			bp := fmt.Sprintf("%s.bindings[%d]", rp, j)
			if b.ID == "" {
				add(bp+".id", "required")
			}
			checkText(bp+".id", b.ID)
			switch {
			case b.RPC == "":
				add(bp+".rpc", "required")
			case !knownRPCs[b.RPC]:
				add(bp+".rpc", "rpc %q is not a registered RPC of the journey, intents, registry, or admin service", b.RPC)
			}
		}

		for j, a := range r.Actions {
			ap := fmt.Sprintf("%s.actions[%d]", rp, j)
			if a.ID == "" {
				add(ap+".id", "required")
			}
			checkText(ap+".id", a.ID)
			switch {
			case a.RPC == "":
				add(ap+".rpc", "required")
			case !knownRPCs[a.RPC]:
				add(ap+".rpc", "rpc %q is not a registered RPC of the journey, intents, registry, or admin service", a.RPC)
			}
			if a.RequiredRole == "" {
				add(ap+".required_role", "every action must declare a required role")
			}
			checkText(ap+".required_role", a.RequiredRole)
		}
	}

	if !hasPrimary {
		add("regions", "at least one region must be RegionPrimary")
	}
	if !monotonicHeadingOrder(headingLevels) {
		add("regions[].heading.level", "heading levels must not skip a level going deeper (e.g. h1 directly to h3 with no h2 between them): got %v", headingLevels)
	}

	if len(pd.Accessibility.Landmarks) == 0 {
		add("accessibility.landmarks", "at least one landmark is required")
	}
	for i, lm := range pd.Accessibility.Landmarks {
		lp := fmt.Sprintf("accessibility.landmarks[%d]", i)
		checkText(lp, lm)
		if !validLandmarks[lm] {
			add(lp, "landmark %q is not in the closed ARIA landmark vocabulary", lm)
		}
	}
	switch pd.Accessibility.LiveRegion {
	case LiveRegionOff, LiveRegionPolite, LiveRegionAssertive:
	default:
		add("accessibility.live_region", "must explicitly be one of off/polite/assertive, got %q", pd.Accessibility.LiveRegion)
	}

	for i, t := range pd.BrandTokens {
		tp := fmt.Sprintf("brand_tokens[%d]", i)
		if t == "" {
			add(tp, "required")
			continue
		}
		checkText(tp, t)
		if !brandTokenPattern.MatchString(t) {
			add(tp, "brand token %q must be a dotted, lowercase, namespaced identifier (e.g. \"brand.color.primary\"), not a literal color or CSS value", t)
		}
	}

	return out
}

// monotonicHeadingOrder reports whether levels (in document order) never
// skips a level going deeper. Going back up to any earlier level is always
// allowed -- that is how a new section starts -- but going deeper must
// advance by exactly one level at a time, matching WCAG's "headings do not
// skip levels" guidance.
func monotonicHeadingOrder(levels []int) bool {
	for i := 1; i < len(levels); i++ {
		if levels[i] > levels[i-1]+1 {
			return false
		}
	}
	return true
}
