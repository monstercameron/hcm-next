// Command intentcoverage reconciles the accepted BusinessIntent catalog
// against the todo backlog (GOV-026) and fails on any orphan not covered by
// an exact, owner-backed current allowlist.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("intentcoverage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	allowlistPath := flags.String("allowlist", filepath.Join("tools", "planning", "intentcoverage", "testdata", "orphans.allowlist.json"), "exact orphan allowlist")
	outPath := flags.String("out", "", "optional report JSON output")
	updateAllowlist := flags.Bool("update-allowlist", false, "write the current orphan snapshot as an owner-backed baseline")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	allowPath := *allowlistPath
	if !filepath.IsAbs(allowPath) {
		allowPath = filepath.Join(*root, allowPath)
	}
	allowlist, err := intentcoverage.LoadAllowlist(allowPath)
	if err != nil {
		fmt.Fprintf(stderr, "intentcoverage: %v\n", err)
		return 2
	}

	snap, testNames, err := intentcoverage.LoadRepository(*root)
	if err != nil {
		fmt.Fprintf(stderr, "intentcoverage: %v\n", err)
		return 2
	}

	if *updateAllowlist {
		report := intentcoverage.Reconcile(snap, intentcoverage.Options{TestNames: testNames})
		if err := intentcoverage.WriteAllowlist(allowPath, intentcoverage.BuildAllowlist(report.Orphans, "2026-09-05")); err != nil {
			fmt.Fprintf(stderr, "intentcoverage: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "intentcoverage: wrote %d baseline orphan(s) to %s\n", len(report.Orphans), allowPath)
		return 0
	}

	report := intentcoverage.Reconcile(snap, intentcoverage.Options{Allowlist: allowlist, TestNames: testNames})
	for _, o := range report.Orphans {
		fmt.Fprintf(stdout, "%s: %s: %s\n", o.Kind, o.ID, o.Detail)
	}
	for _, a := range report.Allowlisted {
		fmt.Fprintf(stdout, "allowlisted %s: %s owner=%s\n", a.Kind, a.ID, a.Owner)
	}

	data, err := report.JSON()
	if err != nil {
		fmt.Fprintf(stderr, "intentcoverage: %v\n", err)
		return 2
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			fmt.Fprintf(stderr, "intentcoverage: write report: %v\n", err)
			return 2
		}
	}

	fmt.Fprintf(stdout, "intentcoverage: baseline=%d extension=%d gaps=%d workflows=%d todos=%d reverse_edges=%d orphans=%d allowlisted=%d new=%d digest=%s\n",
		report.BaselineIntentCount, report.ExtensionIntentCount, report.GapCount, report.WorkflowBindingCount, report.TodoBindingCount,
		len(report.ReverseEdges), len(report.Orphans), len(report.Allowlisted), len(report.NewOrphans), report.Digest)
	for _, ns := range []string{intentcoverage.NamespaceBaseline, intentcoverage.NamespaceExtension} {
		counts, ok := report.StatusCounts[ns]
		if !ok {
			continue
		}
		fmt.Fprintf(stdout, "intentcoverage: %s status counts: conceptual=%d catalogued=%d contracted=%d implemented=%d verified=%d\n",
			ns, counts[intentcoverage.Conceptual], counts[intentcoverage.Catalogued], counts[intentcoverage.Contracted],
			counts[intentcoverage.Implemented], counts[intentcoverage.Verified])
	}

	if len(report.NewOrphans) != 0 {
		return 1
	}
	fmt.Fprintln(stdout, "intentcoverage: PASS")
	return 0
}
