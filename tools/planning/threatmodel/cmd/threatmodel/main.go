// Command threatmodel enforces SECARCH-006 over the real release surfaces.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatmodel"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("threatmodel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	fixture := fs.String("fixture", filepath.Join("tools", "planning", "threatmodel", "testdata", "threat-model.yaml"), "threat-model YAML fixture")
	layout := fs.String("layout", filepath.Join("definitions", "architecture", "repository-layout.yaml"), "repository layout manifest")
	risk := fs.String("risk-table", filepath.Join("tools", "planning", "riskbinding", "testdata", "risk-table.json"), "riskbinding table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	model, err := threatmodel.ValidateRepository(*root, *fixture, *layout, *risk, now())
	if err != nil {
		fmt.Fprintf(stderr, "threatmodel: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, model.Explain())
	fmt.Fprintln(stdout, "threatmodel: PASS")
	return 0
}
