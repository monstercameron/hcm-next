package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// BrandLogoProps is the narrow presentation contract for the customer-owned
// identity slot. The server supplies an already-governed same-origin asset;
// the component never decides tenant identity or fetch policy.
type BrandLogoProps struct {
	Name    string
	Mark    string
	LogoURL string
	Class   string
}

// BrandLogo renders a stable header-sized logo slot. The text signature is
// always present as a resilient compact-navigation and load-failure fallback,
// while one visually-hidden name gives the surrounding home link a reliable
// accessible name whether the logo is an image or text.
func BrandLogo(props BrandLogoProps) ui.Node {
	name := normalizedBrandText(props.Name, 40, DefaultCustomerTheme().BrandName, false)
	mark := normalizedBrandText(props.Mark, 3, DefaultCustomerTheme().BrandMark, true)
	logoURL := normalizedBrandLogoURL(props.LogoURL)
	state := "fallback"
	imageProps := html.Props{
		Class: "brand-logo-image", Width: "180", Height: "40", Loading: "eager",
		Aria: map[string]string{"hidden": "true"},
		Data: map[string]string{"hcm-brand-logo": ""},
		Raw:  map[string]any{"alt": "", "decoding": "async"},
	}
	if logoURL != "" {
		state = "configured"
		imageProps.Src = logoURL
	}
	className := strings.TrimSpace("brand-logo-slot " + props.Class)
	return html.Span(html.Props{Class: className, Data: map[string]string{"hcm-brand-logo-slot": "", "hcm-brand-logo-state": state}},
		html.Img(imageProps),
		html.Span(html.Props{Class: "brand-logo-fallback", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{Class: "wordmark-mark", Data: map[string]string{"hcm-brand-mark": ""}}, ui.Text(mark)),
			html.Span(html.Props{Class: "wordmark-label", Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(name)),
		),
		html.Span(html.Props{Class: "sr-only", Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(name)),
	)
}
