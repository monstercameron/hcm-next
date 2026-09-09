// Command conformance is CONF-001's deterministic conformance runner for
// the reference workflow documents under planning/reference-workflows. It
// parses each document into a typed model, validates it against the ten
// core plus three structural primitive vocabulary in
// planning/specs/workflow-runtime.md and the boundary rules in
// planning/workflows/_engine/workflow-context-contract.md, and writes a
// deterministic JSON and/or Markdown conformance report.
//
// Run it with `go run ./tools/conformance` from the repository root, or
// with `-root`, `-out`, and `-format` to override the defaults.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/discover"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/report"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/runner"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("conformance", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "directory of reference workflow documents (default: <repo>/planning/reference-workflows)")
	out := fs.String("out", "", "output directory for the report (default: <repo>/tools/conformance/out)")
	format := fs.String("format", "both", "output format: json, markdown, or both")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *format != "json" && *format != "markdown" && *format != "both" {
		fmt.Fprintf(stderr, "conformance: invalid -format %q (want json, markdown, or both)\n", *format)
		return 2
	}

	repoRoot, err := reporoot.Find()
	if err != nil {
		fmt.Fprintf(stderr, "conformance: %v\n", err)
		return 2
	}

	refRoot := *root
	if refRoot == "" {
		refRoot = filepath.Join(repoRoot, "planning", "reference-workflows")
	}
	outDir := *out
	if outDir == "" {
		// "out" matches the repository's root .gitignore ("**/out/"), so
		// a bare `go run ./tools/conformance` never leaves generated
		// report files sitting next to this package's source, and never
		// collides with the report package's own directory.
		outDir = filepath.Join(repoRoot, "tools", "conformance", "out")
	}
	runtimeSpecPath := filepath.Join(repoRoot, "planning", "specs", "workflow-runtime.md")
	contextSpecPath := filepath.Join(repoRoot, "planning", "workflows", "_engine", "workflow-context-contract.md")

	v, err := vocab.Load(runtimeSpecPath)
	if err != nil {
		fmt.Fprintf(stderr, "conformance: %v\n", err)
		return 1
	}

	results, err := discover.Documents(repoRoot, refRoot, v)
	if err != nil {
		fmt.Fprintf(stderr, "conformance: %v\n", err)
		return 1
	}
	if len(results) == 0 {
		fmt.Fprintf(stderr, "conformance: no reference workflow documents found under %s\n", refRoot)
		return 1
	}

	rpt := report.Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, results, runner.DefaultFixedClock)

	if err := report.WriteFiles(outDir, *format, rpt); err != nil {
		fmt.Fprintf(stderr, "conformance: writing report: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "conformance: wrote report to %s (format=%s)\n", outDir, *format)
	fmt.Fprintf(stdout, "conformance: manifest digest %s\n", rpt.ManifestDigest)

	failed := false
	for _, doc := range rpt.Documents {
		for _, wf := range doc.Workflows {
			if wf.Summary.Fail > 0 {
				failed = true
				fmt.Fprintf(stdout, "conformance: %s (%s) has %d FAIL check(s)\n", wf.ID, doc.SourcePath, wf.Summary.Fail)
			}
		}
	}
	if failed {
		fmt.Fprintln(stdout, "conformance: one or more checks FAILED")
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
