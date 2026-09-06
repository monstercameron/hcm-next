package pagedef

import "testing"

// TestTodo_WEB_002_Browser is named for the TEST MATRIX entry
// (planning/todos.md WEB-002) but is an explicit, documented skip rather
// than a silent omission: pagedef defines a Go value contract with no
// rendering surface, DOM, or browser of its own -- it is consumed by a
// renderer (tools/uxqual/render/journey, tools/uxqual/render/ssr, ...),
// and browser/accessibility-tooling qualification belongs to that
// renderer's own test suite, not to the contract it renders. There is
// nothing here for a browser harness to load.
func TestTodo_WEB_002_Browser(t *testing.T) {
	t.Skip("pagedef is a Go value contract with no rendering surface of its own; " +
		"browser qualification belongs to the renderer that consumes a PageDefinition " +
		"(tools/uxqual/render/*), not to this contract package")
}
