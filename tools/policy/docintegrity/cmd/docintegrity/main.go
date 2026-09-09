// Command docintegrity enforces DOC-001 over the planning Markdown corpus.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/docintegrity"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docintegrity", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	registry := fs.String("registry", filepath.Join("definitions", "planning", "todo-registry.json"), "path to the generated todo registry")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	registryPath := *registry
	if !filepath.IsAbs(registryPath) {
		registryPath = filepath.Join(*root, registryPath)
	}
	report, err := docintegrity.EvaluateRepository(*root, registryPath)
	if err != nil {
		fmt.Fprintf(stderr, "docintegrity: %v\n", err)
		return 2
	}
	for _, finding := range report.Findings {
		status := ""
		if finding.Allowlisted {
			status = fmt.Sprintf(" [allowlisted owner=%s]", finding.Owner)
		}
		fmt.Fprintf(stdout, "%s%s\n", finding, status)
	}
	violations := report.Violations()
	fmt.Fprintf(stdout, "docintegrity: %d normative document(s), %d todo reference(s), %d orphan(s) (%d allowlisted), %d violation(s)\n", len(report.NormativeDocuments), len(report.TodoReferences), report.OrphanCount, report.AllowlistedOrphans, len(violations))
	if len(violations) != 0 {
		return 1
	}
	fmt.Fprintln(stdout, "docintegrity: PASS")
	return 0
}
