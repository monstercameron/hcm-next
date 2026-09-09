// Command controlcrosswalk validates and regenerates the security-control
// crosswalk against definitions/planning/todo-registry.json (GOV-030).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/controlcrosswalk"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("controlcrosswalk", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	fixture := fs.String("fixture", filepath.Join("tools", "planning", "controlcrosswalk", "testdata", "security-control-crosswalk.yaml"), "YAML crosswalk seed")
	registry := fs.String("registry", filepath.Join("definitions", "planning", "todo-registry.json"), "generated todo registry")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	fixturePath := *fixture
	if !filepath.IsAbs(fixturePath) {
		fixturePath = filepath.Join(*root, fixturePath)
	}
	registryPath := *registry
	if !filepath.IsAbs(registryPath) {
		registryPath = filepath.Join(*root, registryPath)
	}
	definition, err := controlcrosswalk.Load(fixturePath)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 2
	}
	todos, err := controlcrosswalk.LoadRegistry(registryPath)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 2
	}
	testNames, err := scanEvidenceTests(*root)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: scan test names: %v\n", err)
		return 2
	}
	revision, err := controlcrosswalk.Regenerate(definition, todos, testNames)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 1
	}
	if err := controlcrosswalk.Verify(revision); err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", revision.Explain())
	fmt.Fprintln(stdout, "controlcrosswalk: PASS")
	return 0
}

func scanEvidenceTests(root string) (map[string]bool, error) {
	names := make(map[string]bool)
	for _, sourceRoot := range []string{"internal", "tools", "cmd", "gen"} {
		rootNames, err := traceability.ScanTestNames(filepath.Join(root, sourceRoot))
		if err != nil {
			return nil, err
		}
		for name := range rootNames {
			names[name] = true
		}
	}
	return names, nil
}
