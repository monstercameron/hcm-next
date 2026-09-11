// Command workflowdesign regenerates the checked-in design registries from a
// design record sidecar.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("workflowdesign", flag.ContinueOnError)
	seeds := flags.String("seeds", "", "path to the design record sidecar")
	outGo := flags.String("out-go", "", "path to write the Go registry")
	outProto := flags.String("out-proto", "", "path to write the Protobuf registry")
	outGolden := flags.String("out-golden", "", "path to write the golden report")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *seeds == "" || *outGo == "" || *outProto == "" || *outGolden == "" {
		fmt.Fprintln(stderr, "workflowdesign: -seeds, -out-go, -out-proto and -out-golden are required")
		return 2
	}
	records, err := workflowdesign.LoadRecords(*seeds)
	if err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	report := workflowdesign.ValidateRecords(records)
	if !report.OK() {
		fmt.Fprintf(stderr, "workflowdesign: %d finding(s): %+v\n", len(report.Findings), report.Findings)
		return 1
	}
	goRegistry, err := workflowdesign.EmitGoRegistry(records)
	if err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	protoRegistry, err := workflowdesign.EmitProtoRegistry(records)
	if err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	golden, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	if err := os.WriteFile(*outGo, []byte(goRegistry), 0o644); err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	if err := os.WriteFile(*outProto, []byte(protoRegistry), 0o644); err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	if err := os.WriteFile(*outGolden, append(golden, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, "workflowdesign:", err)
		return 1
	}
	fmt.Fprintf(stdout, "workflowdesign: %d records, digest %s\n", len(records), report.Digest)
	return 0
}
