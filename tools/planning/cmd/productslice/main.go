// Command productslice generates definitions/planning/product-slices.yaml:
// ALIGN-001's ProductSliceDefinition registry for the admitted Promotion
// slice. It loads the real registries the definition names (feature/intent
// coverage, the capability BOOTSTRAP registry, the live Phase 1 production
// package closure, and the todo registry), validates the definition against
// them, and refuses to write a file that would not pass its own drift test.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "productslice:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("productslice", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "repository root (default: auto-detected from this binary's source)")
	out := flags.String("out", "", "output path (default: <root>/definitions/planning/product-slices.yaml)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	resolvedRoot := *root
	if resolvedRoot == "" {
		detected, err := productslice.RepoRoot()
		if err != nil {
			return fmt.Errorf("detecting repository root: %w", err)
		}
		resolvedRoot = detected
	}

	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(resolvedRoot, "definitions", "planning", "product-slices.yaml")
	}

	slice := productslice.PromotionSliceDefinition()

	registries, err := productslice.LoadLiveRegistries(resolvedRoot)
	if err != nil {
		return fmt.Errorf("loading live registries: %w", err)
	}

	if violations := slice.Validate(registries); len(violations) > 0 {
		for _, line := range productslice.ViolationStrings(violations) {
			fmt.Fprintln(stderr, "  -", line)
		}
		return fmt.Errorf("promotion slice failed validation against the live registries (%d violations)", len(violations))
	}

	registry := productslice.NewRegistry(slice)
	data, err := productslice.RenderRegistryFile(registry)
	if err != nil {
		return fmt.Errorf("rendering registry file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	_, err = fmt.Fprintf(stdout, "wrote %s (%s)\n", outPath, slice.Explain())
	return err
}
