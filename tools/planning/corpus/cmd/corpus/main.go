// Command corpus reconciles the four planning corpus sources and fails on
// any orphan not covered by an exact, owner-backed current allowlist.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/hcm-next/tools/planning/corpus"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("corpus", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	allowlistPath := flags.String("allowlist", filepath.Join("tools", "planning", "corpus", "testdata", "orphans.allowlist.json"), "exact orphan allowlist")
	outPath := flags.String("out", "", "optional report JSON output")
	updateAllowlist := flags.Bool("update-allowlist", false, "write the current orphan snapshot as an owner-backed baseline")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	allowPath := *allowlistPath
	if !filepath.IsAbs(allowPath) {
		allowPath = filepath.Join(*root, allowPath)
	}
	allowlist, err := corpus.LoadAllowlist(allowPath)
	if err != nil {
		fmt.Fprintf(stderr, "corpus: %v\n", err)
		return 2
	}
	if *updateAllowlist {
		report, err := corpus.ReconcileRepository(*root)
		if err != nil {
			fmt.Fprintf(stderr, "corpus: %v\n", err)
			return 2
		}
		if err := corpus.WriteAllowlist(allowPath, corpus.BuildAllowlist(report.Orphans, "2026-09-05")); err != nil {
			fmt.Fprintf(stderr, "corpus: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "corpus: wrote %d baseline orphan(s) to %s\n", len(report.Orphans), allowPath)
		return 0
	}
	report, err := corpus.ReconcileRepository(*root, corpus.Options{Allowlist: allowlist})
	if err != nil {
		fmt.Fprintf(stderr, "corpus: %v\n", err)
		return 2
	}
	for _, orphan := range report.Orphans {
		fmt.Fprintf(stdout, "%s: %s: %s\n", orphan.Kind, orphan.ID, orphan.Detail)
	}
	for _, orphan := range report.Allowlisted {
		fmt.Fprintf(stdout, "allowlisted %s: %s owner=%s\n", orphan.Kind, orphan.ID, orphan.Owner)
	}
	data, err := report.JSON()
	if err != nil {
		fmt.Fprintf(stderr, "corpus: %v\n", err)
		return 2
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			fmt.Fprintf(stderr, "corpus: write report: %v\n", err)
			return 2
		}
	}
	fmt.Fprintf(stdout, "corpus: spec=%d workflow=%d model=%d todo=%d matrix=%s orphans=%d allowlisted=%d new=%d\n", report.SourceCounts[corpus.SourceSpec], report.SourceCounts[corpus.SourceWorkflow], report.SourceCounts[corpus.SourceModel], report.SourceCounts[corpus.SourceTodo], report.MatrixDigest(), len(report.Orphans), len(report.Allowlisted), len(report.NewOrphans))
	if len(report.NewOrphans) != 0 {
		return 1
	}
	fmt.Fprintln(stdout, "corpus: PASS")
	return 0
}
