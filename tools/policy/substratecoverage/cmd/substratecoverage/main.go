// Command substratecoverage enforces explicit ownership for production
// substrate responsibilities.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/hcm-next/tools/policy/substratecoverage"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("substratecoverage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := substratecoverage.Scan(*root, substratecoverage.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "substratecoverage: %v\n", err)
		return 2
	}
	for _, finding := range report.Findings {
		if finding.Kind == "IMPLICIT" {
			fmt.Fprintf(stdout, "substratecoverage: IMPLICIT %s owner=%s allowlisted=%t (%s)\n", finding.Package, finding.Owner, finding.Allowlisted, finding.Detail)
		}
	}
	if violations := report.Violations(); len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintf(stderr, "substratecoverage: GAP %s %s: %s\n", violation.Kind, violation.Package, violation.Detail)
		}
		return 1
	}
	fmt.Fprintf(stdout, "substratecoverage: PASS (%d declared; digest %s)\n", len(report.Responsibilities), report.Digest())
	return 0
}
