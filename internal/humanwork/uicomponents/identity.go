// Package uicomponents owns renderer-level primitives shared by Human Capital Management Suite's
// product pages and focused workflow pages. The primitives carry no routes,
// business facts, or authorization decisions; feature packages supply those
// through small props contracts.
package uicomponents

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AvatarProps is the presentation-only contract for an employee avatar.
// Class lets each shell bind the primitive to its own design tokens without
// duplicating the accessible markup.
type AvatarProps struct {
	Name       string
	Initials   string
	PhotoURL   string
	Class      string
	Decorative bool
}

// Avatar renders the shared employee/principal identity marker.
func Avatar(props AvatarProps) ui.Node {
	if photoURL := strings.TrimSpace(props.PhotoURL); photoURL != "" {
		htmlProps := html.Props{
			Class: strings.TrimSpace(props.Class), Src: photoURL,
			Loading: "lazy", Width: "64", Height: "64",
			Raw: map[string]any{"decoding": "async"},
		}
		if props.Decorative {
			// GoWebComponents omits zero-valued typed attributes, so force the
			// required empty alternative through Raw for decorative images.
			htmlProps.Raw["alt"] = ""
			htmlProps.Aria = map[string]string{"hidden": "true"}
		} else {
			htmlProps.Alt = strings.TrimSpace(props.Name)
		}
		return html.Img(htmlProps)
	}
	label := strings.TrimSpace(props.Initials)
	if label == "" {
		label = Initials(props.Name)
	}
	htmlProps := html.Props{Class: strings.TrimSpace(props.Class)}
	if props.Decorative {
		htmlProps.Aria = map[string]string{"hidden": "true"}
	} else if name := strings.TrimSpace(props.Name); name != "" {
		htmlProps.Aria = map[string]string{"label": name}
	}
	return html.Span(htmlProps, ui.Text(label))
}

// Initials returns the first rune of the first and last words in name. It is
// Unicode-safe and is shared so a person has the same avatar label on the
// directory, profile, work queue, and workflow pages.
func Initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	first := []rune(parts[0])
	result := first[:1]
	if len(parts) > 1 {
		last := []rune(parts[len(parts)-1])
		result = append(result, last[:1]...)
	}
	return strings.ToUpper(string(result))
}
