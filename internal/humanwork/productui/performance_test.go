package productui

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// BenchmarkProductPageContent keeps every registered page in the performance
// corpus. It measures the leaf render independently from the persistent shell,
// matching the browser router's production composition.
func BenchmarkProductPageContent(b *testing.B) {
	for _, definition := range PageDefinitions() {
		definition := definition
		b.Run(string(definition.ID), func(b *testing.B) {
			view := testView(definition.ID)
			IndexPeople(view.People)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ui.RenderToString(BuildPageContent(view)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkProductShell(b *testing.B) {
	view := testView(PageHome)
	IndexPeople(view.People)
	content := BuildPageContent(view)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(BuildShell(view, content, true)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProductDocument covers the cold-entry SSR path, including the
// immutable platform stylesheet cache shared by every route.
func BenchmarkProductDocument(b *testing.B) {
	view := testView(PageHome)
	IndexPeople(view.People)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Render(view); err != nil {
			b.Fatal(err)
		}
	}
}
