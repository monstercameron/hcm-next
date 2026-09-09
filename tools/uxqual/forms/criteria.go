package forms

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// fieldAttrs walks doc and returns the attribute map for every
// input/textarea/select element keyed by its id attribute (hidden inputs
// excluded, matching qual.FieldIDsInDocumentOrder's own filter), plus the
// set of every element id present anywhere in the document (used to resolve
// an aria-describedby target).
func fieldAttrs(doc string) (fields map[string]map[string]string, allIDs map[string]bool, err error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil, nil, err
	}
	fields = map[string]map[string]string{}
	allIDs = map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if id := attrs["id"]; id != "" {
				allIDs[id] = true
			}
			switch n.DataAtom {
			case atom.Input:
				if attrs["type"] != "hidden" && attrs["id"] != "" {
					fields[attrs["id"]] = attrs
				}
			case atom.Textarea, atom.Select:
				if attrs["id"] != "" {
					fields[attrs["id"]] = attrs
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return fields, allIDs, nil
}

// CheckRequiredFieldSemantics is a FORM-004 criterion tools/uxqual/qual does
// not define: every field id in requiredFieldIDs must render BOTH the
// native HTML `required` attribute (so a sighted mouse-only user's browser
// blocks submission and shows its own native validation message) and
// `aria-required="true"` (so a screen reader announces the same
// requiredness). Neither alone clears FORM-004's bar: a renderer that sets
// only one has a real gap between what a mouse-only user's browser enforces
// and what an assistive-technology user is told.
func CheckRequiredFieldSemantics(doc string, requiredFieldIDs []string) qual.CriterionResult {
	const name = "Required-field semantics"
	fields, _, err := fieldAttrs(doc)
	if err != nil {
		return qual.CriterionResult{Name: name, Pass: false, Detail: "parse error: " + err.Error()}
	}
	var missingField, missingNative, missingAria []string
	for _, id := range requiredFieldIDs {
		attrs, ok := fields[id]
		if !ok {
			missingField = append(missingField, id)
			continue
		}
		if _, hasRequired := attrs["required"]; !hasRequired {
			missingNative = append(missingNative, id)
		}
		if attrs["aria-required"] != "true" {
			missingAria = append(missingAria, id)
		}
	}
	var problems []string
	if len(missingField) > 0 {
		problems = append(problems, "no rendered control for required field(s): "+strings.Join(missingField, ", "))
	}
	if len(missingNative) > 0 {
		problems = append(problems, "missing native `required` attribute on: "+strings.Join(missingNative, ", "))
	}
	if len(missingAria) > 0 {
		problems = append(problems, `missing aria-required="true" on: `+strings.Join(missingAria, ", "))
	}
	if len(problems) > 0 {
		return qual.CriterionResult{Name: name, Pass: false, Detail: strings.Join(problems, "; ")}
	}
	return qual.CriterionResult{Name: name, Pass: true,
		Detail: fmt.Sprintf(`%d required field(s) carry both native required and aria-required="true"`, len(requiredFieldIDs))}
}

// CheckErrorAssociation is a FORM-004 criterion tools/uxqual/qual does not
// define: every field id in fieldIDsWithErrors must be marked
// aria-invalid="true" and carry an aria-describedby whose referenced id(s)
// all resolve to an element actually present in the document. A screen
// reader cannot announce an error message that is not there, and
// aria-describedby naming a dangling id announces nothing.
func CheckErrorAssociation(doc string, fieldIDsWithErrors []string) qual.CriterionResult {
	const name = "Error association (aria-describedby)"
	fields, allIDs, err := fieldAttrs(doc)
	if err != nil {
		return qual.CriterionResult{Name: name, Pass: false, Detail: "parse error: " + err.Error()}
	}
	var problems []string
	for _, id := range fieldIDsWithErrors {
		attrs, ok := fields[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("no rendered control for %q", id))
			continue
		}
		if attrs["aria-invalid"] != "true" {
			problems = append(problems, fmt.Sprintf(`%q missing aria-invalid="true"`, id))
		}
		describedBy := strings.TrimSpace(attrs["aria-describedby"])
		if describedBy == "" {
			problems = append(problems, fmt.Sprintf("%q missing aria-describedby", id))
			continue
		}
		for _, ref := range strings.Fields(describedBy) {
			if !allIDs[ref] {
				problems = append(problems, fmt.Sprintf("%q aria-describedby references missing id %q", id, ref))
			}
		}
	}
	if len(problems) > 0 {
		return qual.CriterionResult{Name: name, Pass: false, Detail: strings.Join(problems, "; ")}
	}
	return qual.CriterionResult{Name: name, Pass: true,
		Detail: fmt.Sprintf("%d field(s) with a validation error carry aria-invalid and a resolvable aria-describedby", len(fieldIDsWithErrors))}
}
