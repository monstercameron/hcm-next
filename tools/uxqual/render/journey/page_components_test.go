package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestPageHeaderIsReusableWithAndWithoutActions(t *testing.T) {
	withAction := renderNode(t, pageHeader(pageHeaderProps{
		Eyebrow: "Career", Title: "Promote Jane", Lead: "Review the change.",
		Actions: []ui.Node{html.A(html.Props{Href: "#/journeys"}, html.Text("All journeys"))},
	}))
	for _, want := range []string{"Career", "Promote Jane", "Review the change.", "All journeys", "jn-context-actions"} {
		if !strings.Contains(withAction, want) {
			t.Fatalf("header missing %q: %s", want, withAction)
		}
	}
	withoutAction := renderNode(t, pageHeader(pageHeaderProps{Title: "Promotion journeys"}))
	if strings.Contains(withoutAction, "jn-context-actions") {
		t.Fatalf("empty actions rendered a wrapper: %s", withoutAction)
	}
}

func TestEngineUnavailableCalloutIsSharedAndConditional(t *testing.T) {
	if engineUnavailableCallout(true, "not used") != nil || engineUnavailableCallout(false, "") != nil {
		t.Fatal("available or unexplained engine rendered a callout")
	}
	out := renderNode(t, engineUnavailableCallout(false, "Ask an operator."))
	if !strings.Contains(out, "Execution is not composed") || !strings.Contains(out, "Ask an operator.") {
		t.Fatalf("callout = %s", out)
	}
}

func TestLoadingPanelDoesNotMountDisabledControls(t *testing.T) {
	out := renderNode(t, loadingPanel("Loading employee context", "Reading the employee."))
	for _, want := range []string{`aria-busy="true"`, "Loading employee context", "Reading the employee."} {
		if !strings.Contains(out, want) {
			t.Fatalf("loading panel missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "disabled") || strings.Contains(out, "<form") {
		t.Fatalf("loading panel mounted eventual controls: %s", out)
	}
}
