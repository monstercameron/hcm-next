// Command apigate runs the offline Protobuf compatibility and consumer
// adoption gate.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/hcm-next/tools/policy/apigate"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apigate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := apigate.Run(*root, apigate.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "apigate: %v\n", err)
		return 2
	}
	if !report.OK() {
		fmt.Fprintf(stderr, "apigate: FAIL: %d buf violation(s), %d consumer impact(s); register %s\n", len(report.Compatibility.Violations), len(report.ConsumerFindings), report.RegisterDigest)
		for _, violation := range report.Compatibility.Violations {
			fmt.Fprintf(stderr, "  - %s: %s\n", violation.Path, violation.Message)
		}
		return 1
	}
	fmt.Fprintf(stdout, "apigate: PASS: register %s\n", report.RegisterDigest)
	return 0
}
