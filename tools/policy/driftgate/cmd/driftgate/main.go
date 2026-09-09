// Command driftgate fails on the first source/generated/document drift.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/driftgate"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("driftgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := driftgate.Evaluate(*root)
	if err != nil {
		fmt.Fprintf(stderr, "driftgate: %v\n", err)
		return 2
	}
	if failed, ok := report.FirstFailure(); ok {
		fmt.Fprintf(stderr, "driftgate: FAIL: %s: %s; regenerate with %s\n", failed.Name, failed.Detail, failed.RegenerationCommand)
		return 1
	}
	fmt.Fprintf(stdout, "driftgate: PASS (%d checks; digest %s)\n", len(report.Checks), report.Digest())
	return 0
}
