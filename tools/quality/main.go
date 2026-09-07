// Command quality is the TOOL-011 authoritative quality gate: it runs
// gofmt -l, go vet ./..., go tool staticcheck ./..., and the registry-driven
// frontend localization/accessibility matrix over the root
// module and exits non-zero if any of them reports a problem. Run it with
// `go run ./tools/quality` from the repository root.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// findRepoRoot walks up from the working directory looking for go.mod, so
// `go run ./tools/quality` behaves the same whether invoked from the
// repository root or from a subdirectory.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s", dir)
		}
		dir = parent
	}
}

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "quality: %v\n", err)
		os.Exit(2)
	}

	ok := true

	fmt.Println("== gofmt -l ==")
	violations, err := gofmtViolations(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quality: gofmt check failed to run: %v\n", err)
		ok = false
	} else if len(violations) > 0 {
		ok = false
		fmt.Println("gofmt reported unformatted files:")
		for _, v := range violations {
			fmt.Printf("  - %s\n", v)
		}
	} else {
		fmt.Println("no formatting drift")
	}

	fmt.Println("== go vet ./... ==")
	if out, passed := runGoVet(root, "./..."); !passed {
		ok = false
		fmt.Print(out)
	} else {
		fmt.Println("go vet: clean")
	}

	fmt.Println("== go tool staticcheck ./... ==")
	if out, passed := runStaticcheck(root, "./..."); !passed {
		ok = false
		fmt.Print(out)
	} else {
		fmt.Println("staticcheck: clean")
	}

	fmt.Println("== frontend i18n + accessibility ==")
	if out, passed := runFrontendExperienceGate(root); !passed {
		ok = false
		fmt.Print(out)
	} else {
		fmt.Println("frontend i18n + accessibility: clean")
	}

	if !ok {
		fmt.Fprintln(os.Stderr, "quality: FAILED")
		os.Exit(1)
	}
	fmt.Println("quality: PASSED")
}
