package productui

import (
	"fmt"
	"strconv"
	"strings"
)

// Registered preview axes: the dimensions presentation renders.
// Locales come from the product locale catalog; color modes are
// the concrete appearance presets (system resolves on-device and
// previews as nothing); viewport widths are author-chosen
// positive pixel counts. New axes register when the renderer
// supports them — authors never invent axes here.
const (
	PreviewAxisLocale        = "locale"
	PreviewAxisColorMode     = "color-mode"
	PreviewAxisViewportWidth = "viewport-width"
)

// previewColorModes are the previewable appearance presets.
var previewColorModes = map[string]bool{"light": true, "dark": true}

// PreviewDimension is one preview axis with its cases, in author
// order. Cases keep author order: locales read left to right,
// widths narrow to wide, as declared.
type PreviewDimension struct {
	Axis  string
	Cases []string
}

// PreviewPlan is one governed preview: the page, the fixture set
// it renders with (declared — fixture existence stays with the
// studio), and the dimension axes. Empty dimensions preview the
// default render as one caseless cell.
type PreviewPlan struct {
	Page       PageID
	Fixture    string
	Dimensions []PreviewDimension
}

// PreviewValue is one cell coordinate.
type PreviewValue struct {
	Axis string
	Case string
}

// PreviewCase is one matrix cell: its stable ID — axis=case
// pairs joined in dimension order, or "default" for the
// caseless cell — plus its values.
type PreviewCase struct {
	ID     string
	Values []PreviewValue
}

// PreviewVerdict is the plan answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type PreviewVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidatePreviewPlan checks one preview plan: a named page, a
// named fixture, known axes without duplicates, and per-axis
// cases without blanks or duplicates that satisfy the axis
// contract — catalog locales, previewable color modes, positive
// viewport widths. Case structure validates even on unknown
// axes; contract checks need a known axis. Violations accumulate
// in fixed order: page, fixture, then per dimension in order.
func ValidatePreviewPlan(plan PreviewPlan) PreviewVerdict {
	var reasons []string
	if plan.Page == "" {
		reasons = append(reasons, "missing preview page")
	}
	if plan.Fixture == "" {
		reasons = append(reasons, "missing preview fixture")
	}
	locales := map[string]bool{}
	for _, locale := range SupportedProductLocales() {
		locales[locale] = true
	}
	seenAxes := map[string]bool{}
	for _, dimension := range plan.Dimensions {
		axisKnown := dimension.Axis == PreviewAxisLocale || dimension.Axis == PreviewAxisColorMode || dimension.Axis == PreviewAxisViewportWidth
		switch {
		case !axisKnown:
			reasons = append(reasons, fmt.Sprintf("unknown preview axis %q", dimension.Axis))
		case seenAxes[dimension.Axis]:
			reasons = append(reasons, fmt.Sprintf("duplicate preview axis %q", dimension.Axis))
		default:
			seenAxes[dimension.Axis] = true
		}
		seenCases := map[string]bool{}
		for _, cell := range dimension.Cases {
			switch {
			case cell == "":
				reasons = append(reasons, fmt.Sprintf("blank preview case for axis %q", dimension.Axis))
			case seenCases[cell]:
				reasons = append(reasons, fmt.Sprintf("duplicate preview case %q for axis %q", cell, dimension.Axis))
			default:
				seenCases[cell] = true
				switch dimension.Axis {
				case PreviewAxisLocale:
					if !locales[cell] {
						reasons = append(reasons, fmt.Sprintf("unsupported preview locale %q", cell))
					}
				case PreviewAxisColorMode:
					if !previewColorModes[cell] {
						reasons = append(reasons, fmt.Sprintf("unsupported preview color mode %q", cell))
					}
				case PreviewAxisViewportWidth:
					if width, err := strconv.Atoi(cell); err != nil || width <= 0 {
						reasons = append(reasons, fmt.Sprintf("unsupported preview viewport width %q", cell))
					}
				}
			}
		}
	}
	if len(reasons) > 0 {
		return PreviewVerdict{Compatible: false, Reasons: reasons}
	}
	return PreviewVerdict{Compatible: true}
}

// ExpandPreviewPlan expands one valid plan into its case matrix:
// the cartesian product in dimension order, first axis slowest.
// Invalid plans refuse with the joined verdict reasons; the
// default render expands to one caseless cell.
func ExpandPreviewPlan(plan PreviewPlan) ([]PreviewCase, error) {
	if verdict := ValidatePreviewPlan(plan); !verdict.Compatible {
		return nil, fmt.Errorf("invalid preview plan: %s", strings.Join(verdict.Reasons, "; "))
	}
	if len(plan.Dimensions) == 0 {
		return []PreviewCase{{ID: "default"}}, nil
	}
	cases := []PreviewCase{{}}
	for _, dimension := range plan.Dimensions {
		var next []PreviewCase
		for _, cell := range cases {
			for _, choice := range dimension.Cases {
				values := append(append([]PreviewValue(nil), cell.Values...), PreviewValue{Axis: dimension.Axis, Case: choice})
				parts := make([]string, 0, len(values))
				for _, value := range values {
					parts = append(parts, value.Axis+"="+value.Case)
				}
				next = append(next, PreviewCase{ID: strings.Join(parts, "|"), Values: values})
			}
		}
		cases = next
	}
	return cases, nil
}
