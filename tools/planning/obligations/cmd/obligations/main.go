// Command obligations generates the normative planning requirement registry.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/hcm-next/tools/planning/obligations"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("obligations", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	out := flags.String("out", "requirements.json", "JSON registry output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	registry, err := obligations.Scan(*root)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(*out); statErr == nil {
		previous, loadErr := obligations.LoadJSON(*out)
		if loadErr != nil {
			return loadErr
		}
		registry = obligations.AttachLineage(registry, previous)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat prior registry %s: %w", *out, statErr)
	}
	data, err := registry.JSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	_, err = fmt.Fprintf(stdout, "generated %s with %d obligations\n", *out, len(registry.Obligations))
	return err
}
