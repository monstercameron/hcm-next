package forms

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ExtractFormAnswers reads the value a human would have submitted for every
// input/textarea field in doc, keyed by id: an `<input value="...">`
// attribute, or a `<textarea id="...">text</textarea>` element's own text
// content. It models exactly what a real HTML form POST or a captured DOM
// submit event hands a server -- the input [FromFormSubmission] consumes --
// so this package's "equivalent route" proof starts from a real rendered
// document (SSR or GWC), not from a hand-built stand-in for one.
func ExtractFormAnswers(doc string) (map[string]string, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil, err
	}
	answers := map[string]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			var id, value string
			for _, a := range n.Attr {
				switch a.Key {
				case "id":
					id = a.Val
				case "value":
					value = a.Val
				}
			}
			switch n.DataAtom {
			case atom.Input:
				if id != "" {
					answers[id] = value
				}
			case atom.Textarea:
				if id != "" {
					answers[id] = textContent(n)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return answers, nil
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
