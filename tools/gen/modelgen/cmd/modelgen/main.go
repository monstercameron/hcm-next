// Command modelgen regenerates gen/go/hcmnext/model from the compiled
// internal/intent/model registry (MSRC-007). Run it from anywhere inside the
// repository:
//
//	go run ./tools/gen/modelgen/cmd/modelgen
//
// It is a thin wrapper: every decision (which Go types are recognized, how a
// field or the registry is rendered, where the output goes) lives in
// tools/gen/modelgen, which is exercised directly by that package's own test
// suite. main here only resolves the repository root, calls the library, and
// reports what it wrote.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/modelgen"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

// run performs the regeneration and reports to out/errOut, returning the
// process exit code. It is the whole of main's logic, factored out so
// main_test.go can exercise it without forking a subprocess.
func run(out, errOut io.Writer) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(errOut, "modelgen: %v\n", err)
		return 1
	}
	root, err := modelgen.RepoRoot(wd)
	if err != nil {
		fmt.Fprintf(errOut, "modelgen: %v\n", err)
		return 1
	}
	files, err := modelgen.GenerateAll()
	if err != nil {
		fmt.Fprintf(errOut, "modelgen: %v\n", err)
		return 1
	}
	if err := modelgen.WriteAll(root, files); err != nil {
		fmt.Fprintf(errOut, "modelgen: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "modelgen: wrote %s/%s (%s)\n", modelgen.OutputDir, modelgen.OutputFile, modelgen.OutputDigest(files))
	return 0
}
