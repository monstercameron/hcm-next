// Command generatelibrarystrategy updates the generated library-strategy
// inventory in README.md from the current architecture manifests.
//
//	go run ./tools/gen/librarystrategy/cmd/generatelibrarystrategy
package main

import (
	"fmt"
	"os"

	"github.com/monstercameron/hcm-next/tools/gen/librarystrategy"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generatelibrarystrategy:", err)
		os.Exit(1)
	}
	if err := librarystrategy.UpdateREADME(root); err != nil {
		fmt.Fprintln(os.Stderr, "generatelibrarystrategy:", err)
		os.Exit(1)
	}
}
