// Command enginecoverage enforces explicit reusable-computation ownership for
// the drafted intent catalog.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/hcm-next/tools/policy/enginecoverage"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("enginecoverage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := enginecoverage.Scan(*root, enginecoverage.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "enginecoverage: %v\n", err)
		return 2
	}
	for _, finding := range report.Findings {
		if finding.Kind == "IMPLICIT" {
			fmt.Fprintf(stdout, "enginecoverage: IMPLICIT %s %s owner=%s allowlisted=%t (%s)\n", finding.Intent, finding.Computation, finding.Owner, finding.Allowlisted, finding.Detail)
		}
	}
	if violations := report.Violations(); len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintf(stderr, "enginecoverage: GAP %s %s/%s %s: %s\n", violation.Kind, violation.Intent, violation.Computation, violation.Package, violation.Detail)
		}
		return 1
	}
	fmt.Fprintf(stdout, "enginecoverage: PASS (%d responsibility rows; digest %s)\n", len(report.Responsibilities), report.Digest())
	return 0
}
