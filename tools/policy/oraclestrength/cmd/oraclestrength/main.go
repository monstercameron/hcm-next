package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/hcm-next/tools/policy/oraclestrength"
)

// Run executes the GOV-021 command and returns a process-style exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("oraclestrength", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := ""
	registry := ""
	allowlist := ""
	flags.StringVar(&root, "root", "", "repository root (defaults to the current directory)")
	flags.StringVar(&registry, "registry", "", "compiled todo registry path")
	flags.StringVar(&allowlist, "allowlist", "", "reviewed exception JSON path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stderr, "oraclestrength: resolve root: %v\n", err)
		return 2
	}
	if registry == "" {
		registry = filepath.Join(root, "definitions", "planning", "todo-registry.json")
	}
	if allowlist == "" {
		allowlist = filepath.Join(root, "tools", "policy", "oraclestrength", "allowlist.json")
	}
	report, err := oraclestrength.CheckRegistry(root, registry, allowlist)
	if err != nil {
		fmt.Fprintf(stderr, "oraclestrength: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "GOV-021 oracle strength: %d oracle(s), %d finding(s), %d allowlisted\n", len(report.Oracles), len(report.Findings), len(report.Allowlisted))
	for _, finding := range report.Findings {
		fmt.Fprintln(stdout, finding.String())
	}
	for _, finding := range report.Allowlisted {
		fmt.Fprintf(stdout, "ALLOWLISTED: %s\n", finding.String())
	}
	if len(report.Findings) != 0 {
		return 1
	}
	return 0
}

func main() { os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr)) }
