// Command provgen generates and signs the SLSA-style build provenance
// statement for the hcmnext binary (TOOL-018/SUPPLY-001) and writes it to
// -out (or definitions/supply-chain/provenance.json by default).
//
// The no-argument invocation is the checked-in release evidence command:
//
//	go run ./tools/policy/provenance/cmd/provgen
//
// Production callers should override the development key with a real signing
// key. provgen never writes a built binary anywhere durable: it builds
// cmd/hcmnext into a temporary directory purely to hash it, then removes that
// directory before the process exits - only the resulting JSON statement
// (names and digests, never bytes) is written.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

const (
	defaultKeyPath    = "tools/planning/gateevidence/testdata/dev-signing-key.yaml"
	defaultOutputPath = "definitions/supply-chain/provenance.json"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("provgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the Go module root to build from")
	pattern := fs.String("pattern", "./cmd/hcmnext", "Go package pattern to build")
	subject := fs.String("subject", "hcmnext", "name recorded for the built artifact's subject")
	sbomPath := fs.String("sbom", provenance.DefaultSBOMPath, "path (relative to -root) of the CycloneDX SBOM to reference")
	key := fs.String("key", defaultKeyPath, "path to a signing key fixture (schema_version/algorithm/public_key/private_key); use -unsigned for a development-only unsigned statement")
	unsigned := fs.Bool("unsigned", false, "write an unsigned statement instead of requiring -key (development use only)")
	out := fs.String("out", defaultOutputPath, "path to write the signed provenance JSON document (default: definitions/supply-chain/provenance.json; use -out= for stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	stmt, err := provenance.Generate(*root, provenance.Options{
		Pattern:     *pattern,
		SubjectName: *subject,
		SBOMPath:    *sbomPath,
	})
	if err != nil {
		fmt.Fprintf(stderr, "provgen: %v\n", err)
		return 1
	}

	if violations := stmt.Validate(); len(violations) != 0 {
		for _, v := range violations {
			fmt.Fprintf(stderr, "provgen: %s\n", v)
		}
		return 1
	}

	final := *stmt
	if !*unsigned {
		keyPath := rootedPath(*root, *key)
		priv, err := provenance.LoadSigningKeyFixture(keyPath)
		if err != nil {
			fmt.Fprintf(stderr, "provgen: %v\n", err)
			return 1
		}
		signed, err := provenance.SignStatement(priv, *stmt, *key)
		if err != nil {
			fmt.Fprintf(stderr, "provgen: signing statement: %v\n", err)
			return 1
		}
		final = signed
	}

	data, err := json.MarshalIndent(final, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "provgen: encoding statement: %v\n", err)
		return 1
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "provgen: writing stdout: %v\n", err)
			return 1
		}
		return 0
	}
	outPath := rootedPath(*root, *out)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "provgen: writing %s: %v\n", outPath, err)
		return 1
	}
	fmt.Fprintf(stdout, "provgen: wrote %s (%d subject(s))\n", outPath, len(final.Subjects))
	return 0
}

func rootedPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
