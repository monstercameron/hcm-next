// Command archdoc generates the checked-in architecture document without
// writing to planning/. The caller chooses the output path with -out.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/archdoc"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("archdoc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	out := flags.String("out", "architecture-generated.md", "generated Markdown output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	document, err := archdoc.Generate(*root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(*out, document, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	_, err = fmt.Fprintf(stdout, "generated %s (%d bytes)\n", *out, len(document))
	return err
}
