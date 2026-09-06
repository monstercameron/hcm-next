package ssrshell

import "github.com/monstercameron/hcm-next/tools/uxqual/pagedef"

// LandmarkTags is the closed set of HTML elements this package ever emits
// for a region: the six landmark-shaped elements the WEB-025 todo names
// (header/nav/main/aside/footer/section). It is exported so a conformance
// check outside this package can confirm [LandmarkForRegionKind] never
// answers with anything else.
var LandmarkTags = []string{"header", "nav", "main", "aside", "footer", "section"}

// landmark is one documented mapping from a pagedef.RegionKind to the HTML
// element this renderer wraps that region in, plus the short label used to
// build the region's accessible name (see buildDocumentView).
type landmark struct {
	Tag   string
	Label string
}

// landmarksByKind is the one place every member of pagedef's closed
// RegionKind vocabulary is mapped to a documented landmark. It exists
// (rather than a switch inlined into the renderer) so
// TestTodo_WEB_025_Conformance can iterate pagedef.RegionKinds() and assert
// every one of them has an entry here, the same way pagedef's own
// regionKindDocs proves its vocabulary is fully documented.
//
// The mapping follows the frontend plan's "Page anatomy" section:
//
//   - shell -> header: the one applic­ation-shell landmark at the top of the
//     page (product, tenant scope, navigation, account context).
//   - page_identity, authority_context -> section: neither is a distinct
//     ARIA landmark role in its own right in the frontend plan's anatomy;
//     each becomes a labelled <section>, which the ARIA-in-HTML mapping
//     turns into a "region" landmark by virtue of carrying an accessible
//     name (aria-label).
//   - local_navigation -> nav: sections/steps/views navigation.
//   - primary -> main: the page's one dominant hierarchy. A PageDefinition
//     is refused by pagedef.Validate unless it has at least one; this
//     package does not further enforce "at most one" (pagedef does not
//     either), so a PageDefinition with more than one Primary region would
//     render more than one <main>, which this package documents as a
//     caller responsibility rather than silently working around.
//   - supporting, utility -> aside: both are complementary content by the
//     frontend plan's own description ("reflows below primary content" /
//     "user-opened ... never the only place for required content").
//     Rendering both as <aside> with distinct accessible names (see
//     buildDocumentView) keeps them as two distinguishable complementary
//     landmarks rather than inventing a seventh HTML element the WEB-025
//     todo does not name.
//   - completion -> footer: review, consequences, and the focus-safe final
//     action, which this package renders as the page's closing landmark.
var landmarksByKind = map[pagedef.RegionKind]landmark{
	pagedef.RegionShell:            {Tag: "header", Label: "Application shell"},
	pagedef.RegionPageIdentity:     {Tag: "section", Label: "Page identity"},
	pagedef.RegionAuthorityContext: {Tag: "section", Label: "Authority context"},
	pagedef.RegionLocalNavigation:  {Tag: "nav", Label: "Local navigation"},
	pagedef.RegionPrimary:          {Tag: "main", Label: "Primary"},
	pagedef.RegionSupporting:       {Tag: "aside", Label: "Supporting"},
	pagedef.RegionUtility:          {Tag: "aside", Label: "Utility"},
	pagedef.RegionCompletion:       {Tag: "footer", Label: "Completion"},
}

// LandmarkForRegionKind returns the HTML tag and short label this package
// documents for kind, and whether kind is a member of the vocabulary it
// knows how to render at all. A region whose kind is not in pagedef's own
// closed vocabulary (which should never happen once pagedef.Validate has
// run) reports ok == false rather than guessing at a tag.
func LandmarkForRegionKind(kind pagedef.RegionKind) (tag, label string, ok bool) {
	lm, ok := landmarksByKind[kind]
	return lm.Tag, lm.Label, ok
}
