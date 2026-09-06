// Command riskbinding enforces GOV-023 over the checked-in risk table.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/hcm-next/tools/planning/riskbinding"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("riskbinding", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	table := fs.String("table", filepath.Join("tools", "planning", "riskbinding", "testdata", "risk-table.json"), "declared risk table")
	registry := fs.String("registry", filepath.Join("definitions", "planning", "todo-registry.json"), "generated todo registry")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	tablePath := *table
	if !filepath.IsAbs(tablePath) {
		tablePath = filepath.Join(*root, tablePath)
	}
	registryPath := *registry
	if !filepath.IsAbs(registryPath) {
		registryPath = filepath.Join(*root, registryPath)
	}
	report, err := riskbinding.EvaluateRepository(*root, tablePath, registryPath)
	if err != nil {
		fmt.Fprintf(stderr, "riskbinding: %v\n", err)
		return 2
	}
	for _, finding := range report.Findings {
		fmt.Fprintln(stdout, finding)
	}
	for _, gap := range report.AllowlistedGaps {
		fmt.Fprintf(stdout, "allowlisted gap: %s/%s owner=%s\n", gap.RiskID, gap.Kind, gap.Owner)
	}
	architecture := report.ByCategory[riskbinding.Architecture]
	product := report.ByCategory[riskbinding.Product]
	fmt.Fprintf(stdout, "riskbinding: architecture unbound=%d/%d gaps=%d new=%d; product unbound=%d/%d gaps=%d new=%d\n", architecture.Unbound, architecture.Total, architecture.GapCount, architecture.NewGapCount, product.Unbound, product.Total, product.GapCount, product.NewGapCount)
	if len(report.Violations()) != 0 {
		fmt.Fprintf(stdout, "riskbinding: %d violation(s)\n", len(report.Violations()))
		return 1
	}
	fmt.Fprintln(stdout, "riskbinding: PASS")
	return 0
}
