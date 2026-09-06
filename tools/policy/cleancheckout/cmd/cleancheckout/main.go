// Command cleancheckout verifies the root Go module from a tracked-tree-only
// temporary checkout.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/hcm-next/tools/policy/cleancheckout"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cleancheckout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the repository root")
	jsonOutput := fs.Bool("json", false, "emit the machine-readable report")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := cleancheckout.Check(*root)
	if err != nil && report.Checks == nil {
		fmt.Fprintf(stderr, "cleancheckout: %v\n", err)
		return 2
	}
	if *jsonOutput {
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			fmt.Fprintf(stderr, "cleancheckout: encode report: %v\n", marshalErr)
			return 2
		}
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprint(stdout, report.Text())
	}
	if err != nil {
		return 1
	}
	return 0
}
