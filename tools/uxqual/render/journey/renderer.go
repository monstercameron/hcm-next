package journey

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Build constructs the journey page component tree for p. It does not
// include the stylesheet (see tools/uxqual/render/gwc.Build for why: GWC has
// no raw-text sink on the render path, so CSS is assembled as a plain string
// by Document rather than as a text node).
//
// The product surface is the live one: Mount / MountLive (mount_wasm.go,
// GOOS=js GOARCH=wasm) render this tree into the DOM and the client talks
// to the engine over gRPC on a WebSocket. RenderToString and Document
// render the identical tree without a browser, which is how this package is
// tested -- they are the test path, not a second product.
//
// The same tree serves both because every interaction is expressed as an
// optional callback on the contract. When Page carries them, Build wires
// GWC event handlers and the browser's own navigation and POST are
// prevented; when it does not, the tree is plain links and plain forms that
// a test can assert against as markup.
//
// Exactly one of p.List, p.Proposal and p.Detail is expected to be set. When
// none is, the chrome still renders -- masthead, live region, an empty main
// landmark and the footer -- rather than panicking, so a projection bug
// degrades to a blank page with working navigation instead of a 500.
func Build(p Page) ui.Node {
	mainProps := html.Props{ID: skipTarget, Class: "jn-main"}
	if networkPending(p) {
		mainProps.Raw = map[string]any{"aria-busy": "true"}
	}
	main := html.Main(mainProps,
		html.Div(html.Props{Class: "jn-shell"}, pageBody(p)),
	)

	return html.Div(html.Props{Class: "jn-page"},
		skipLink(),
		masthead(p),
		noticeRegion(p.Notice),
		main,
		footer(p.Footer),
	)
}

// BuildContent constructs the journey-owned content without its standalone
// masthead, main landmark, or footer. Product shells use it to make Journeys
// a first-class route while retaining the exact same forms, actions, live
// notices, and detail presentation as the standalone surface. The embedded
// content keeps its own page heading, so the host must not add a second h1.
func BuildContent(p Page) ui.Node {
	props := html.Props{Class: "jn-embedded"}
	if networkPending(p) {
		props.Class += " jn-network-pending"
		props.Raw = map[string]any{"aria-busy": "true"}
	}
	return html.Div(props,
		noticeRegion(p.Notice),
		html.Div(html.Props{Class: "jn-shell"}, embeddedPageBody(p)),
	)
}

func embeddedPageBody(p Page) ui.Node {
	var body ui.Node
	if p.List != nil {
		body = embeddedListView(p, *p.List)
	} else {
		body = resolvedPageBody(p)
	}
	return networkAwarePageBody(p, body)
}

func pageBody(p Page) ui.Node {
	return networkAwarePageBody(p, resolvedPageBody(p))
}

func resolvedPageBody(p Page) ui.Node {
	switch {
	case p.Detail != nil:
		return detailView(p, *p.Detail)
	case p.Proposal != nil:
		return proposalView(p, *p.Proposal)
	case p.List != nil:
		return listView(p, *p.List)
	default:
		return nil
	}
}

// RenderToString renders the component tree (without the stylesheet) with
// GWC's native SSR path.
func RenderToString(p Page) (string, error) {
	return ui.RenderToString(Build(p))
}

var titleEscaper = strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;")

// Document renders the complete standalone HTML document for p.
//
// Its shape is load-bearing: the server pins sha256(Stylesheet()) in the
// response's content-security-policy style-src, so the one inline <style>
// here must contain exactly Stylesheet() and nothing else -- no minifier
// pass, no concatenated second sheet, no whitespace around it. There is no
// <script>, no <link> and no <img> anywhere in the output, because the same
// policy is default-src 'none'; the wasm client is loaded by the shell the
// server sends, not from here.
func Document(p Page) (string, error) {
	body, err := RenderToString(p)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString("<title>")
	b.WriteString(titleEscaper.Replace(p.Title))
	b.WriteString("</title><style>")
	b.WriteString(Stylesheet())
	b.WriteString("</style></head><body>")
	b.WriteString(body)
	b.WriteString("</body></html>")
	return b.String(), nil
}
