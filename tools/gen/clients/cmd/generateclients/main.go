// Command generateclients runs the TOOL-007 generator, writing the typed
// capability clients to internal/transport/clients relative to the current
// working directory (normally the repository root).
//
//	go run ./tools/gen/clients/cmd/generateclients
package main

import (
	"fmt"
	"os"

	clientsgen "github.com/monstercameron/human-capital-management-suite/tools/gen/clients"
)

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generateclients:", err)
		os.Exit(1)
	}
	if err := clientsgen.WriteAll(repoRoot); err != nil {
		fmt.Fprintln(os.Stderr, "generateclients:", err)
		os.Exit(1)
	}
}
