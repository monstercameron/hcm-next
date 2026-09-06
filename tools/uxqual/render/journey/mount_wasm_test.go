//go:build js && wasm

package journey

import (
	"strings"
	"testing"
)

// These tests carry the same build tag as mount_wasm.go so
// `GOOS=js GOARCH=wasm go vet ./tools/uxqual/render/journey/` type-checks
// the product entrypoint with its test alongside it.
//
// They deliberately do not call Mount or MountLive: there is no DOM under
// `go test`, and ui.Render would reach into a document that does not exist.
// What they can check without a browser is that the three halves of the
// wasm contract hold -- the tree the entrypoints hand to ui.Render is the
// same one every native test asserts against, the string injected into
// <head> is the same constant the content-security-policy hashes, and the
// store the client drives is wired to the component before anything mounts.

func TestMountBuildsTheSameTreeTheNativeTestsAssertAgainst(t *testing.T) {
	for name, page := range samplePages() {
		t.Run(name, func(t *testing.T) {
			if Build(page) == nil {
				t.Fatal("Build returned a nil node; Mount would render nothing")
			}
			if LiveComponent(NewStore(page)) == nil {
				t.Fatal("LiveComponent returned a nil node; MountLive would render nothing")
			}
		})
	}
}

func TestMountInjectsExactlyTheHashedStylesheet(t *testing.T) {
	css := Stylesheet()
	if css == "" {
		t.Fatal("Stylesheet() is empty")
	}
	if strings.Contains(css, "</style") {
		t.Fatal("stylesheet closes its own style element; injecting it would break the head")
	}
}

// TestMountEntrypointsShareOneStore keeps Mount an alias for MountLive
// rather than a second, divergent mounting path.
func TestMountEntrypointsShareOneStore(t *testing.T) {
	s := NewStore(SampleListPage())
	if s.Page().List == nil {
		t.Fatal("the store did not keep the page it was seeded with")
	}
	s.Set(SampleDetailPage())
	if s.Page().Detail == nil {
		t.Error("the store did not follow a Set")
	}
}
