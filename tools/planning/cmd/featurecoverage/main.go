// Command featurecoverage generates the machine-readable feature-to-intent
// coverage registry from the checked-in governance manifests.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/monstercameron/hcm-next/tools/planning/intentmanifests"
)

func main() {
	intakePath := flag.String("intake", filepath.Join("definitions", "governance", "feature-intent-intake.yaml"), "feature intake manifest")
	intentsPath := flag.String("intents", filepath.Join("definitions", "governance", "intent-conformance-descriptors.yaml"), "intent catalog manifest")
	outputPath := flag.String("output", filepath.Join("definitions", "governance", "feature-intent-coverage.yaml"), "generated coverage registry")
	flag.Parse()

	intents, err := intentmanifests.LoadIntentManifestYAML(*intentsPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := intentmanifests.ValidateIntentManifestYAML(intents); err != nil {
		log.Fatal(err)
	}
	groups, err := intentmanifests.LoadFeatureManifestYAML(*intakePath)
	if err != nil {
		log.Fatal(err)
	}
	if err := intentmanifests.ValidateFeatureManifestYAML(groups); err != nil {
		log.Fatal(err)
	}
	registry, err := intentmanifests.BuildFeatureIntentCoverage(groups, intents)
	if err != nil {
		log.Fatal(err)
	}
	data, err := intentmanifests.MarshalFeatureIntentCoverageYAML(registry)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Generated %s with %d features and digest %s\n", *outputPath, registry.FeatureCount, registry.Digest)
}
