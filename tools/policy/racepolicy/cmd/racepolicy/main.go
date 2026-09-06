// Command racepolicy runs the TOOL-012 concurrent-package race-coverage
// policy against a Go module tree and reports every violation: a package
// that imports "sync"/"sync/atomic" or starts a goroutine, but has no test
// file that would resolve (and therefore run under `-race`) on the Linux
// CI job.
//
// Exit code is 0 with no violations, 1 with at least one, 2 on a usage or
// scan error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"

	"github.com/monstercameron/hcm-next/tools/policy/racepolicy"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("racepolicy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the Go module root to scan")
	modulePath := fs.String("module", "", "module path (default: the \"module\" directive from -root/go.mod)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	effectiveModule := *modulePath
	if effectiveModule == "" {
		data, err := os.ReadFile(filepath.Join(*root, "go.mod"))
		if err != nil {
			fmt.Fprintf(stderr, "racepolicy: -module not given and reading go.mod failed: %v\n", err)
			return 2
		}
		effectiveModule = modfile.ModulePath(data)
		if effectiveModule == "" {
			fmt.Fprintf(stderr, "racepolicy: -module not given and go.mod has no module directive\n")
			return 2
		}
	}

	report, err := racepolicy.Evaluate(*root, effectiveModule)
	if err != nil {
		fmt.Fprintf(stderr, "racepolicy: %v\n", err)
		return 2
	}

	fmt.Fprintf(stdout, "racepolicy: %d concurrent package(s) declared\n", len(report.Findings))
	violations := report.Violations()
	if len(violations) == 0 {
		fmt.Fprintln(stdout, "racepolicy: PASS (every concurrent package has a Linux-raceable test suite)")
		return 0
	}

	fmt.Fprintf(stdout, "racepolicy: FAIL (%d violation(s))\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(stdout, "  - %s: %s\n", v.Package.ImportPath, v.Detail)
	}
	return 1
}
