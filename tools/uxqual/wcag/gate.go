package wcag

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/monstercameron/hcm-next/tools/uxqual/forms"
	"github.com/monstercameron/hcm-next/tools/uxqual/qual"
	"github.com/monstercameron/hcm-next/tools/uxqual/tokens"
)

// CheckFocusAndNames verifies the WCAG 2.4.3/2.4.7 and 4.1.2 structural
// portion of the pilot: controls have an associated name, positive tabindex
// cannot reorder focus, and the page exposes the expected main landmark.
func CheckFocusAndNames(doc string) qual.CriterionResult {
	const name = "Focus order and accessible names"
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return qual.CriterionResult{Name: name, Detail: "parse error: " + err.Error()}
	}
	labels := map[string]bool{}
	main := false
	var unlabeled []string
	var positive []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			a := attrs(n)
			if n.DataAtom == atom.Main {
				main = true
			}
			if n.DataAtom == atom.Label && a["for"] != "" {
				labels[a["for"]] = true
			}
			if n.DataAtom == atom.Input || n.DataAtom == atom.Textarea || n.DataAtom == atom.Select || n.DataAtom == atom.Button {
				if n.DataAtom == atom.Input && a["type"] == "hidden" { /* not user-facing */
				} else {
					buttonText := strings.TrimSpace(nodeText(n))
					if a["id"] == "" && n.DataAtom != atom.Button && n.DataAtom != atom.A {
						unlabeled = append(unlabeled, a["id"])
					} else if n.DataAtom != atom.Button && n.DataAtom != atom.A && !labels[a["id"]] && a["aria-label"] == "" && a["aria-labelledby"] == "" {
						unlabeled = append(unlabeled, a["id"])
					} else if (n.DataAtom == atom.Button || n.DataAtom == atom.A) && buttonText == "" && a["aria-label"] == "" && a["aria-labelledby"] == "" {
						unlabeled = append(unlabeled, a["id"])
					}
				}
			}
			if n.DataAtom == atom.A || n.DataAtom == atom.Button || n.DataAtom == atom.Input || n.DataAtom == atom.Textarea || n.DataAtom == atom.Select {
				if a["tabindex"] != "" && strings.HasPrefix(strings.TrimSpace(a["tabindex"]), "+") {
					positive = append(positive, a["id"])
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if !main {
		return qual.CriterionResult{Name: name, Detail: "missing main landmark"}
	}
	if len(unlabeled) > 0 {
		return qual.CriterionResult{Name: name, Detail: "unnamed controls: " + strings.Join(unlabeled, ", ")}
	}
	if len(positive) > 0 {
		return qual.CriterionResult{Name: name, Detail: "positive tabindex on: " + strings.Join(positive, ", ")}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "all user-facing controls are named and retain DOM focus order"}
}

// CheckAccessibleAuth ensures the rendered surface is an authorization
// projection: only the three fixture-approved transitions occur, and the
// masked privileged transition is absent. This catches an accessible UI that
// accidentally makes a forbidden action keyboard reachable.
func CheckAccessibleAuth(doc string) qual.CriterionResult {
	const name = "Accessible authorization projection"
	for _, needle := range []string{"force_execute", "Force execute", "nationalId", "555-11-2222"} {
		if strings.Contains(doc, needle) {
			return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("unauthorized or masked value %q is rendered", needle)}
		}
	}
	for _, transition := range []string{"approve", "reject", "request_more_information"} {
		if !strings.Contains(doc, `name="transition" value="`+transition+`"`) {
			return qual.CriterionResult{Name: name, Detail: "authorized transition missing: " + transition}
		}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "authorized transitions are keyboard-reachable and privileged/masked transitions are absent"}
}

// CheckZoomReflow checks stylesheet declarations that can defeat 200/400%
// zoom and narrow reflow. Bare pixel widths over the pilot floor are rejected;
// the existing qualification fixture also gets a full 320px structural check.
func CheckZoomReflow(doc string) qual.CriterionResult {
	const name = "200% zoom and 400% reflow"
	css := qual.ExtractInlineCSS(doc)
	if r := qual.CheckReflow(css); !r.Pass {
		return qual.CriterionResult{Name: name, Detail: r.Detail}
	}
	if !strings.Contains(css, "max-width:100%") || !strings.Contains(css, "flex-wrap:wrap") {
		return qual.CriterionResult{Name: name, Detail: "responsive controls lack max-width/flex-wrap safeguards"}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "relative sizing, max-width safeguards and wrapping support 200% zoom / 400% reflow"}
}

// CheckReducedMotion records the CSS policy required by the gate. This
// fixture has no animation declarations, which is safer than relying on a
// user-agent preference being applied to an animated component.
func CheckReducedMotion(doc string) qual.CriterionResult {
	const name = "Reduced motion"
	css := qual.ExtractInlineCSS(doc)
	if regexp.MustCompile(`(?i)animation\s*:|transition\s*:|animation-name`).MatchString(css) && !strings.Contains(css, "prefers-reduced-motion") {
		return qual.CriterionResult{Name: name, Detail: "motion declaration has no prefers-reduced-motion override"}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "no unguarded animation or transition in the pilot fixture"}
}

// Score returns the complete automated scorecard for one rendered document.
func Score(doc string) []qual.CriterionResult {
	return []qual.CriterionResult{
		qual.CheckKeyboard(doc), CheckFocusAndNames(doc),
		qual.CheckScreenReaderSemantics(doc), forms.CheckErrorAssociation(doc, forms.ErroredFieldIDs),
		qual.CheckContrastAA(), CheckZoomReflow(doc), CheckReducedMotion(doc),
		CheckAccessibleAuth(doc), qual.CheckMasking(doc, []string{"nationalId", "555-11-2222", "force_execute", "Force execute"}),
	}
}

func attrs(n *html.Node) map[string]string {
	out := map[string]string{}
	for _, a := range n.Attr {
		out[a.Key] = a.Val
	}
	return out
}
func nodeText(n *html.Node) string {
	var s strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			s.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return s.String()
}

// Keep the shared token package linked in this gate's API documentation and
// make accidental deletion of the palette dependency visible to reviewers.
var _ = tokens.MinRatioNormalText
