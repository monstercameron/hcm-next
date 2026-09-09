// Command genfixtures writes the SSR and GWC renderers' real output for the
// shared Promotion workspace fixture (tools/uxqual/testdata) to
// tools/uxqual/testdata/rendered/{ssr,gwc}.html, for the Playwright browser
// pass in tools/uxqual/browser (a real, non-Go process; it cannot call the
// Go renderers directly, so it loads these static files instead).
//
// Run it before `npx playwright test --config=tools/uxqual/browser/playwright.config.ts`:
//
//	go run ./tools/uxqual/cmd/genfixtures
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

func main() {
	fixture := testdata.PromotionFixture()

	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		log.Fatalf("ssr.Render: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		log.Fatalf("gwc.Document: %v", err)
	}

	outDir := filepath.Join("tools", "uxqual", "testdata", "rendered")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", outDir, err)
	}
	write := func(name, content string) {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		log.Printf("wrote %s (%d bytes)", path, len(content))
	}
	write("ssr.html", ssrDoc)
	write("gwc.html", gwcDoc)
}
