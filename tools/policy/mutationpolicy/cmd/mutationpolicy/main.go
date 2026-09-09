package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/mutationpolicy"
)

// Run executes the GOV-019 command and returns a process-style exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mutationpolicy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := ""
	flags.StringVar(&root, "root", "", "repository root (defaults to the module root)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stderr, "mutationpolicy: resolve root: %v\n", err)
		return 2
	}
	report, err := mutationpolicy.Scan(root)
	if err != nil {
		fmt.Fprintf(stderr, "mutationpolicy: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "GOV-019 mutation policy: %d package(s), %d gap(s)\n", len(report.Packages), len(report.Gaps))
	for _, gap := range report.Gaps {
		fmt.Fprintln(stdout, gap.String())
	}
	if len(report.Gaps) != 0 {
		return 1
	}
	return 0
}

func main() { os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr)) }
