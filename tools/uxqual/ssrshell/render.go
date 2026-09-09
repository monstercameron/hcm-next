package ssrshell

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// PageIslandElementID is the id of the data island every rendered shell
// carries: a <script type="application/json"> element naming the page id,
// version, and digest of the PageDefinition the shell was rendered from.
// It is not the JourneyConfigElementID island
// internal/humanwork/workspace/journey_shell.go writes (that one carries a
// live-view client's connection configuration); this island exists so a
// later layer -- or a test -- can confirm which governed page definition,
// at which version and digest, produced the document it is looking at,
// without re-deriving it from the rendered markup.
const PageIslandElementID = "page-definition"

// LiveRegionElementID is the stable cross-renderer id of a governed page's
// one explicit live region. GWC aliases this value so SSR and hydrated trees
// cannot silently choose different announcement targets.
const LiveRegionElementID = "live-region"

// RenderedShell is [Render]'s result: the shell document and a digest of
// its own bytes.
//
// RenderedShell.Digest is not [pagedef.PageDefinition.Digest] -- that one
// identifies the input contract; this one identifies the output bytes, so
// a caller (or a golden test) can detect a change in what was actually
// served without re-rendering and diffing the whole document.
type RenderedShell struct {
	HTML   string
	Digest string
}

// Bytes returns the shell document as a byte slice.
func (r RenderedShell) Bytes() []byte { return []byte(r.HTML) }

// pageIsland is the JSON schema of the data island [PageIslandElementID]
// carries.
type pageIsland struct {
	PageID  string `json:"page_id"`
	Version int    `json:"version"`
	Digest  string `json:"digest"`
}

// headingView, widgetView, regionView, and liveRegionView are the template
// data ssrshell builds from a pagedef.PageDefinition. They exist as a
// separate view model (rather than executing the template directly against
// pagedef types) so the template only ever sees plain strings and ints it
// can escape safely, never a pagedef.RegionKind or other typed value whose
// String() a future change could alter without this package noticing.
type headingView struct {
	Level int
	Text  string
}

type widgetView struct {
	ID        string
	WidgetRef string
}

type regionView struct {
	ID        string
	Tag       string
	AriaLabel string
	Heading   *headingView
	Widgets   []widgetView
}

type liveRegionView struct {
	// Politeness is the aria-live attribute value: "polite" or "assertive".
	Politeness string
	// Role is the ARIA role that matches Politeness: "status" for polite,
	// "alert" for assertive.
	Role string
}

type documentView struct {
	Lang       string
	Title      string
	CSS        template.CSS
	LiveRegion *liveRegionView
	Regions    []regionView
}

// Render turns pd into a semantic HTML shell document.
//
// It refuses (returns an error, renders nothing) any PageDefinition that
// pagedef.Validate itself refuses: this package renders validated page
// definitions, it does not re-validate ad hoc or attempt to render around a
// violation. See renderValid and this package's Security tests for what
// still holds even when that guarantee is bypassed.
func Render(pd pagedef.PageDefinition) (RenderedShell, error) {
	if violations := pd.Validate(); len(violations) != 0 {
		return RenderedShell{}, fmt.Errorf("ssrshell: refusing to render invalid PageDefinition %q: %v", pd.PageID, violations)
	}
	return renderValid(pd)
}

// renderValid renders pd without first calling pagedef.Validate. It is
// unexported: [Render] is the only path a caller outside this package (or
// this package's own non-Security tests) should use. It exists as a
// separate function so the Security tests can prove the template layer's
// own autoescaping holds independently of pagedef's validation -- defense
// in depth, not a second way to skip validation in production use.
func renderValid(pd pagedef.PageDefinition) (RenderedShell, error) {
	dv, err := buildDocumentView(pd)
	if err != nil {
		return RenderedShell{}, err
	}

	var buf bytes.Buffer
	if err := shellTemplate.Execute(&buf, dv); err != nil {
		return RenderedShell{}, fmt.Errorf("ssrshell: execute template: %w", err)
	}

	island, err := json.Marshal(pageIsland{
		PageID:  pd.PageID,
		Version: pd.Version,
		Digest:  pd.Digest(),
	})
	if err != nil {
		return RenderedShell{}, fmt.Errorf("ssrshell: marshal data island: %w", err)
	}
	// encoding/json's default Marshal behavior HTML-escapes '<', '>', and
	// '&' to their \uXXXX forms, so this island can never close the
	// surrounding <script> element or introduce markup of its own -- the
	// exact property internal/humanwork/workspace/journey_shell.go's
	// JourneyConfig island relies on and documents for the same reason.
	// json.Marshal never leaves that behavior implicit here: no
	// encoder.SetEscapeHTML(false) is used anywhere in this package.
	buf.WriteString(`<script type="application/json" id="` + PageIslandElementID + `">`)
	buf.Write(island)
	buf.WriteString("</script>\n</body>\n</html>\n")

	html := buf.String()
	sum := sha256.Sum256([]byte(html))
	return RenderedShell{HTML: html, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// buildDocumentView projects pd onto the template's view model. It is the
// one place a pagedef.RegionKind is resolved to a landmark tag; every
// region's kind must resolve (landmarksByKind covers pagedef's whole closed
// vocabulary), or buildDocumentView returns an error naming the offending
// region rather than silently falling back to some default element.
func buildDocumentView(pd pagedef.PageDefinition) (documentView, error) {
	regions := make([]regionView, 0, len(pd.Regions))
	title := pd.PageID
	titleSet := false

	for _, r := range pd.Regions {
		tag, label, ok := LandmarkForRegionKind(r.Kind)
		if !ok {
			return documentView{}, fmt.Errorf("ssrshell: region %q has kind %q with no documented landmark mapping", r.ID, r.Kind)
		}

		var hv *headingView
		if r.Heading != nil {
			hv = &headingView{Level: r.Heading.Level, Text: r.Heading.Text}
			if !titleSet && r.Heading.Level == 1 {
				title = r.Heading.Text
				titleSet = true
			}
		}

		widgets := make([]widgetView, 0, len(r.Widgets))
		for _, w := range r.Widgets {
			widgets = append(widgets, widgetView{ID: w.ID, WidgetRef: w.WidgetRef})
		}

		regions = append(regions, regionView{
			ID:        r.ID,
			Tag:       tag,
			AriaLabel: label + ": " + r.ID,
			Heading:   hv,
			Widgets:   widgets,
		})
	}

	var lrv *liveRegionView
	switch pd.Accessibility.LiveRegion {
	case pagedef.LiveRegionPolite:
		lrv = &liveRegionView{Politeness: "polite", Role: "status"}
	case pagedef.LiveRegionAssertive:
		lrv = &liveRegionView{Politeness: "assertive", Role: "alert"}
	case pagedef.LiveRegionOff:
		lrv = nil
	}

	return documentView{
		Lang:       "en",
		Title:      title,
		CSS:        template.CSS(ShellCSS()),
		LiveRegion: lrv,
		Regions:    regions,
	}, nil
}

// shellTemplate is the whole of this package's html/template surface.
//
// Every tag name it writes is literal text in the template source itself
// (never {{.Something}} in tag-name position, which html/template's
// contextual autoescaper refuses to parse at all): the landmark tag for a
// region and the heading level for its heading are both chosen by Go code
// with a fixed, literal {{if eq ...}} branch per possible value rather than
// interpolated. Every place a PageDefinition-derived string actually
// appears -- an id, an aria-label, heading text, a widget ref -- appears
// only in an attribute-value or text-content position, where
// html/template's autoescaper applies the correct context-specific
// escaping automatically. There is no template.HTML or template.JS value
// anywhere in this file: nothing this package writes into the template is
// pre-trusted.
var shellTemplate = template.Must(template.New("shell").Parse(
	`{{define "region-body"}}` +
		`{{if .Heading}}` +
		`{{if eq .Heading.Level 1}}<h1 id="heading-{{.ID}}">{{.Heading.Text}}</h1>
` +
		`{{else if eq .Heading.Level 2}}<h2 id="heading-{{.ID}}">{{.Heading.Text}}</h2>
` +
		`{{else if eq .Heading.Level 3}}<h3 id="heading-{{.ID}}">{{.Heading.Text}}</h3>
` +
		`{{else if eq .Heading.Level 4}}<h4 id="heading-{{.ID}}">{{.Heading.Text}}</h4>
` +
		`{{else if eq .Heading.Level 5}}<h5 id="heading-{{.ID}}">{{.Heading.Text}}</h5>
` +
		`{{else}}<h6 id="heading-{{.ID}}">{{.Heading.Text}}</h6>
` +
		`{{end}}` +
		`{{end}}` +
		`{{range .Widgets}}<div class="widget-slot" data-slot-id="{{.ID}}" data-widget-ref="{{.WidgetRef}}" role="presentation"></div>
` +
		`{{end}}` +
		`{{end}}` +
		`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head>
<body>
<a class="visually-hidden" href="#main-content">Skip to main content</a>
` +
		`{{if .LiveRegion}}<div id="` + LiveRegionElementID + `" role="{{.LiveRegion.Role}}" aria-live="{{.LiveRegion.Politeness}}" aria-atomic="true"></div>
{{end}}` +
		`{{range .Regions}}` +
		`{{if eq .Tag "header"}}<header id="region-{{.ID}}" aria-label="{{.AriaLabel}}">
{{template "region-body" .}}</header>
` +
		`{{else if eq .Tag "nav"}}<nav id="region-{{.ID}}" aria-label="{{.AriaLabel}}">
{{template "region-body" .}}</nav>
` +
		`{{else if eq .Tag "main"}}<main id="main-content" aria-label="{{.AriaLabel}}">
{{template "region-body" .}}</main>
` +
		`{{else if eq .Tag "aside"}}<aside id="region-{{.ID}}" aria-label="{{.AriaLabel}}">
{{template "region-body" .}}</aside>
` +
		`{{else if eq .Tag "footer"}}<footer id="region-{{.ID}}" aria-label="{{.AriaLabel}}">
{{template "region-body" .}}</footer>
` +
		`{{else}}<section id="region-{{.ID}}" aria-label="{{.AriaLabel}}">
{{template "region-body" .}}</section>
` +
		`{{end}}` +
		`{{end}}`,
))
