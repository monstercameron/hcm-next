package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTransientPopoverUsesSharedSurfaceContract(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "test-menu", Class: "test-root", TriggerClass: "test-trigger", Label: "Open test menu",
		Trigger: []ui.Node{ui.Text("Open")}, PanelClass: "test-panel", Children: []ui.Node{ui.Text("Contents")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="popover-root test-root"`, `data-hcm-transient-popover="test-menu"`,
		`data-hcm-popover-grace-ms="180"`, `class="test-trigger"`, `aria-label="Open test menu"`,
		`class="popover-surface test-panel"`, `data-hcm-popover-surface="true"`, "Contents",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("shared popover missing %q in %s", want, markup)
		}
	}
}

func TestPopoverSurfaceSupportsStatefulListboxes(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(PopoverSurface, PopoverSurfaceProps{
		ID: "results", Class: "search-results", Raw: map[string]any{"role": "listbox"}, Children: []ui.Node{ui.Text("Result")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="results"`, `class="popover-surface search-results"`, `role="listbox"`, "Result"} {
		if !strings.Contains(markup, want) {
			t.Errorf("shared stateful surface missing %q in %s", want, markup)
		}
	}
}
