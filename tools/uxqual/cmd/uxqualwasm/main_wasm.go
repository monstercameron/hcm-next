//go:build js && wasm

// Command uxqualwasm is the GWC/WASM entrypoint for the Promotion
// workspace: `GOOS=js GOARCH=wasm go build ./tools/uxqual/cmd/uxqualwasm`.
// As recorded in definitions/ux/workspace-renderer-decision.yaml, this
// build currently fails because GWC v5.0.1's html/ui packages need go.sum
// entries this lane cannot add; see tools/uxqual/render/gwc/doc.go.
package main

import (
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

func main() {
	gwc.Mount(testdata.PromotionFixture(), "#app")
}
