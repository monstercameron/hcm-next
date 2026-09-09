// Command endpointmanifest regenerates definitions/api/endpoint-manifest.json
// from internal/transport/manifest.Build(). Run it with
// `go run ./tools/policy/endpointmanifest/cmd/endpointmanifest` from the
// repository root after any change to the generated Protobuf service
// descriptors, the BOOTSTRAP capability registry or the intent definition
// catalog; run `npx prettier --write definitions/api` afterward to match
// the repository's formatting convention.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/endpointmanifest"
)

func main() {
	outputPath := flag.String("output", endpointmanifest.DefaultPath, "path to the output JSON file")
	flag.Parse()

	m, err := manifest.Build()
	if err != nil {
		log.Fatalf("failed to build the endpoint manifest: %v", err)
	}

	jsonBytes, err := m.CanonicalJSON()
	if err != nil {
		log.Fatalf("failed to encode the endpoint manifest: %v", err)
	}

	if dir := filepath.Dir(*outputPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("failed to create output directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(*outputPath, jsonBytes, 0o644); err != nil {
		log.Fatalf("failed to write %s: %v", *outputPath, err)
	}

	fmt.Printf("Generated %s with %d endpoints\n", *outputPath, len(m.Endpoints))
}
