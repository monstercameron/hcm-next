package page

import (
	"fmt"
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/floorplan"
	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"github.com/monstercameron/hcm-next/tools/uxqual/ssrshell"
)

// RootElementIDPrefix names the root node's id: "page-" + PageID. It is
// exported so a caller composing this tree into a larger shell (see
// tools/uxqual/render/workspace) can find it without restating the literal.
const RootElementIDPrefix = "page-"

// Render builds res.Page's component tree using res.Floorplan's declared
// regions and reg's registered widgets, in the page's own definition order.
//
// It refuses (returns a nil Node and an error) three distinct ways, and
// never falls back to free HTML for any of them:
//
//  1. res.Page fails [pagedef.PageDefinition.Validate] -- the same primary
//     defense tools/uxqual/ssrshell.Render applies before rendering
//     anything.
//  2. A region's kind has no landmark mapping in
//     [ssrshell.LandmarkForRegionKind] -- this should never happen once
//     Validate has run (pagedef's closed RegionKind vocabulary and
//     ssrshell's landmark map are total over each other), so this is
//     treated as an internal consistency fault rather than a normal
//     refusal.
//  3. A widget slot names a ref reg has no entry for: a
//     [*UnregisteredWidgetError], wrapping [ErrUnregisteredWidget].
//
// res.Floorplan supplies only validated responsive presentation metadata.
// floorplan.Registry.Resolve has already checked that every region res.Page
// declares is present in the floorplan. This package still preserves the
// PageDefinition's region order (the frontend plan's page anatomy); responsive
// rules can change layout within a region, never which regions or actions exist
// or where they appear in the DOM.
func Render(res floorplan.Resolution, reg *Registry) (ui.Node, error) {
	pd := res.Page
	if violations := pd.Validate(); len(violations) != 0 {
		return nil, fmt.Errorf("page: refusing to render invalid PageDefinition %q: %v", pd.PageID, violations)
	}
	return buildResponsivePage(pd, res.Floorplan, reg)
}

// buildPage renders pd without first calling pagedef.Validate. It is
// unexported so [Render] is the only path a caller outside this package (or
// this package's own Security tests) uses -- the same "renderValid" split
// tools/uxqual/ssrshell uses to prove its escaping holds even bypassing
// Validate, applied here to prove GWC's own SSR escaping (see doc.go) holds
// the same way.
func buildPage(pd pagedef.PageDefinition, reg *Registry) (ui.Node, error) {
	return buildPageWithResponsiveProps(pd, reg, nil)
}

func buildResponsivePage(pd pagedef.PageDefinition, fp floorplan.Floorplan, reg *Registry) (ui.Node, error) {
	responsive, err := responsiveProps(fp)
	if err != nil {
		return nil, fmt.Errorf("page: refusing to render invalid responsive floorplan: %w", err)
	}
	return buildPageWithResponsiveProps(pd, reg, responsive)
}

func buildPageWithResponsiveProps(pd pagedef.PageDefinition, reg *Registry, responsive map[string]html.Props) (ui.Node, error) {
	children := make([]ui.Node, 0, len(pd.Regions))
	for _, region := range pd.Regions {
		node, err := buildRegionWithResponsiveProps(pd, region, reg, responsive)
		if err != nil {
			return nil, err
		}
		children = append(children, node)
	}
	return html.Div(html.Props{
		ID:   RootElementIDPrefix + pd.PageID,
		Data: map[string]string{"page-version": strconv.Itoa(pd.Version)},
	}, children...), nil
}

// buildRegion renders one region: its landmark element, its heading (if
// any), and each of its widget slots, in declaration order.
func buildRegion(pd pagedef.PageDefinition, region pagedef.Region, reg *Registry) (ui.Node, error) {
	return buildRegionWithResponsiveProps(pd, region, reg, nil)
}

func buildRegionWithResponsiveProps(pd pagedef.PageDefinition, region pagedef.Region, reg *Registry, responsive map[string]html.Props) (ui.Node, error) {
	tag, label, ok := ssrshell.LandmarkForRegionKind(region.Kind)
	if !ok {
		return nil, fmt.Errorf("page: region %q has kind %q with no documented landmark mapping", region.ID, region.Kind)
	}

	children := make([]ui.Node, 0, len(region.Widgets)+1)
	if region.Heading != nil {
		children = append(children, heading(region.ID, *region.Heading))
	}
	for _, slot := range region.Widgets {
		node, err := buildSlot(pd, region, slot, reg)
		if err != nil {
			return nil, err
		}
		children = append(children, node)
	}

	id := "region-" + region.ID
	if tag == "main" {
		// The <main> landmark carries the well-known skip-link target id,
		// matching tools/uxqual/ssrshell's own convention, so a caller that
		// composes this tree behind a skip link (see
		// tools/uxqual/render/workspace) can target the same id regardless
		// of which region happens to be Primary.
		id = "main-content"
	}
	props := html.Props{ID: id, Aria: map[string]string{"label": label + ": " + region.ID}}
	if responsive != nil {
		regionProps, ok := responsive[region.ID]
		if !ok {
			return nil, fmt.Errorf("page: responsive region %q is not declared by floorplan", region.ID)
		}
		props.Class = regionProps.Class
		props.Data = regionProps.Data
	}
	return landmarkElement(tag, props, children...)
}

// responsiveProps is the only floorplan-to-DOM projection. One stable
// DOM carries every bounded breakpoint result so viewport changes, text zoom,
// and container resizing reflow entirely in renderer-owned CSS. Attribute
// names are fixed here and values come only from validated closed vocabularies
// and bounded integers; page definitions cannot supply classes or CSS.
// Layouts are computed once per page, not once per region.
func responsiveProps(fp floorplan.Floorplan) (map[string]html.Props, error) {
	props := make(map[string]html.Props, len(fp.Regions))
	for _, region := range fp.Regions {
		props[region.Name] = html.Props{
			Class: "layout-region",
			Data:  make(map[string]string, len(floorplan.Breakpoints())*3),
		}
	}
	layouts, err := fp.TransformAll()
	if err != nil {
		return nil, err
	}
	for _, layout := range layouts {
		breakpoint := layout.Breakpoint
		for _, effective := range layout.Regions {
			regionProps := props[effective.Region.Name]
			prefix := "layout-" + string(breakpoint) + "-"
			regionProps.Data[prefix+"mode"] = string(effective.Region.Layout.Mode)
			regionProps.Data[prefix+"columns"] = strconv.Itoa(effective.Region.Layout.MaxColumns)
			regionProps.Data[prefix+"stacked"] = strconv.FormatBool(effective.Stacked)
		}
	}
	return props, nil
}

// buildSlot resolves slot's widget ref against reg and wraps the result in
// a container that carries the same data-slot-id / data-widget-ref
// attributes tools/uxqual/ssrshell's empty mount point div carries, so a
// later mount step can match this tree's slot container against the shell's
// placeholder by id.
func buildSlot(pd pagedef.PageDefinition, region pagedef.Region, slot pagedef.WidgetSlot, reg *Registry) (ui.Node, error) {
	ctor, found := reg.Lookup(slot.WidgetRef)
	if !found {
		return nil, &UnregisteredWidgetError{
			PageID:    pd.PageID,
			RegionID:  region.ID,
			SlotID:    slot.ID,
			WidgetRef: slot.WidgetRef,
		}
	}
	content := ctor(WidgetContext{
		PageID:      pd.PageID,
		PageVersion: pd.Version,
		Region:      region,
		Slot:        slot,
	})
	if content == nil {
		return nil, fmt.Errorf("page: widget %q for slot %q (page %q) returned a nil node", slot.WidgetRef, slot.ID, pd.PageID)
	}
	return html.Div(html.Props{
		Class: "widget-slot",
		Data:  map[string]string{"slot-id": slot.ID, "widget-ref": slot.WidgetRef},
	}, content), nil
}

// heading renders one region's heading at its declared level. Level is
// validated 1-6 by pagedef.Validate before Render ever calls this.
func heading(regionID string, h pagedef.Heading) ui.Node {
	id := "heading-" + regionID
	text := ui.Text(h.Text)
	switch h.Level {
	case 1:
		return html.H1(html.Props{ID: id}, text)
	case 2:
		return html.H2(html.Props{ID: id}, text)
	case 3:
		return html.H3(html.Props{ID: id}, text)
	case 4:
		return html.H4(html.Props{ID: id}, text)
	case 5:
		return html.H5(html.Props{ID: id}, text)
	default:
		return html.H6(html.Props{ID: id}, text)
	}
}

// landmarkElement builds the element ssrshell.LandmarkForRegionKind named
// for tag. tag is always one of ssrshell.LandmarkTags -- landmarksByKind
// (unexported in ssrshell) is total over pagedef's closed RegionKind
// vocabulary and LandmarkForRegionKind never returns ok==true for anything
// else -- so the default case is an internal-consistency fault, not a
// reachable "unknown region kind" path (that is refused earlier, in
// buildRegion).
func landmarkElement(tag string, props html.Props, children ...ui.Node) (ui.Node, error) {
	switch tag {
	case "header":
		return html.Header(props, children...), nil
	case "nav":
		return html.Nav(props, children...), nil
	case "main":
		return html.Main(props, children...), nil
	case "aside":
		return html.Aside(props, children...), nil
	case "footer":
		return html.Footer(props, children...), nil
	case "section":
		return html.Section(props, children...), nil
	default:
		return nil, fmt.Errorf("page: internal error: unrecognized landmark tag %q", tag)
	}
}
