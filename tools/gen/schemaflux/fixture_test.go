package schemaflux_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	upstream "github.com/monstercameron/schemaflux"

	sfx "github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux"
)

func definitionsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(findRepoRoot(t), "schema", "schemaflux", "business_intents", "v1")
}

func loadValidCatalog(t *testing.T) *sfx.Catalog {
	t.Helper()
	defs, err := sfx.LoadDefinitions(definitionsPath(t))
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	manifest, err := sfx.LoadCapabilityManifest(filepath.Join("testdata", "capability_manifest.yaml"))
	if err != nil {
		t.Fatalf("LoadCapabilityManifest: %v", err)
	}
	catalog, errs := sfx.Compile(defs, manifest)
	if len(errs) != 0 {
		t.Fatalf("Compile on the valid fixture reported %d unresolved reference(s), want 0: %v", len(errs), errs)
	}
	return catalog
}

// TestSchemaFluxOfflineFixture is the TOOL-004 primary test: the
// qualification fixture from planning/specs/go-only-technology-
// constitution.md "SchemaFlux (preferred definition generation)".
//
// It disqualifies github.com/monstercameron/schemaflux empirically (every
// content-producing call requires a model, and its offline stand-in returns
// unusable placeholder text), then proves the fallback generator this
// package implements instead compiles the fourteen definitions plus the P1A
// capability manifest to byte-identical output twice, offline, with zero
// network calls and zero unresolved references.
func TestSchemaFluxOfflineFixture(t *testing.T) {
	t.Run("SchemaFluxRequiresNetworkForRealGeneration", func(t *testing.T) {
		bt := installBlockingTransport(t)

		if err := upstream.Init("sk-test-fake-key-network-guard-only"); err != nil {
			t.Fatalf("upstream schemaflux.Init: %v", err)
		}

		type registryEntry struct {
			IntentTypeID string `json:"intent_type_id"`
		}
		before := bt.dials.Load()
		_, err := upstream.Generate[registryEntry](
			"produce a business intent registry entry",
			upstream.NewGenerateOptions(),
		)
		if err == nil {
			t.Fatal("expected SchemaFlux.Generate to fail with network access blocked, got nil error; " +
				"if this starts passing, SchemaFlux has grown an offline generation path and TOOL-004 must be re-run")
		}
		if bt.dials.Load() <= before {
			t.Fatal("SchemaFlux.Generate returned an error without attempting any network call; " +
				"cannot conclude it requires a model call from this alone")
		}
		t.Logf("confirmed: SchemaFlux.Generate attempted %d network dial(s) before failing under the blocked transport", bt.dials.Load()-before)
	})

	t.Run("SchemaFluxOfflineStandInIsUnusablePlaceholderText", func(t *testing.T) {
		// SCHEMAFLUX_PROVIDER=local / WithMockProvider is SchemaFlux's own
		// deliberate offline path. Its own internal/llm/mockshape.go says what
		// it deliberately does not do: "pretend to be a model. The values are
		// obviously synthetic." Prove that empirically rather than merely
		// citing the comment, so a future SchemaFlux release that changes this
		// behavior fails this test rather than going unnoticed: feed it a
		// prompt naming the real input (the fourteen hcmnext definitions) and
		// confirm the answer neither echoes that content nor is a real
		// definition — it is canned, shape-correct filler.
		installBlockingTransport(t) // still forbid the network on this path too.

		client := upstream.NewClient("").WithMockProvider()
		upstream.SetDefaultClient(client)
		t.Cleanup(func() { upstream.SetDefaultClient(nil) })

		out, err := upstream.Summarize(
			"the fourteen hcmnext business intent definitions under schema/schemaflux",
			upstream.SummarizeOptions{},
		)
		if err != nil {
			t.Fatalf("mock-provider Summarize returned an error: %v", err)
		}
		if strings.Contains(out, "fourteen") || strings.Contains(out, "hcmnext") {
			t.Fatalf("expected obviously synthetic filler unrelated to the real input, got %q; "+
				"if SchemaFlux's mock provider now reflects real input content, TOOL-004 must be re-run", out)
		}
		t.Logf("mock provider's synthetic, input-independent answer: %q", out)
	})

	t.Run("FallbackGeneratorNeverTouchesTheNetwork", func(t *testing.T) {
		bt := installBlockingTransport(t)
		before := bt.dials.Load()

		catalog := loadValidCatalog(t)
		if _, err := sfx.Generate(catalog, "schemaflux"); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if _, err := sfx.CrossCheckCompiled(catalog); err != nil {
			t.Fatalf("CrossCheckCompiled: %v", err)
		}

		if got := bt.dials.Load(); got != before {
			t.Fatalf("fallback generation attempted %d network dial(s); want 0", got-before)
		}
	})

	t.Run("FallbackGeneratorIsByteIdenticalAcrossTwoRuns", func(t *testing.T) {
		installBlockingTransport(t) // belt and suspenders: no network for the whole run.

		defs, err := sfx.LoadDefinitions(definitionsPath(t))
		if err != nil {
			t.Fatalf("LoadDefinitions: %v", err)
		}
		manifest, err := sfx.LoadCapabilityManifest(filepath.Join("testdata", "capability_manifest.yaml"))
		if err != nil {
			t.Fatalf("LoadCapabilityManifest: %v", err)
		}

		run := func() sfx.Artifacts {
			catalog, errs := sfx.Compile(defs, manifest)
			if len(errs) != 0 {
				t.Fatalf("Compile: %v", errs)
			}
			artifacts, err := sfx.Generate(catalog, "schemaflux")
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			return artifacts
		}

		first := run()
		second := run()

		if string(first.RegistryGo) != string(second.RegistryGo) {
			t.Error("registry Go source differs between two runs over the same input")
		}
		if string(first.CatalogMD) != string(second.CatalogMD) {
			t.Error("catalog markdown differs between two runs over the same input")
		}
		if string(first.FixturesJSON) != string(second.FixturesJSON) {
			t.Error("fixtures JSON differs between two runs over the same input")
		}

		// Simulate "two machines" by writing both runs' output into two
		// independent temp directories under different absolute paths and
		// re-reading them: nothing in the generated bytes may depend on the
		// machine-specific output path, so a byte comparison of the files on
		// disk must still match the in-memory comparison above.
		dir1 := t.TempDir()
		dir2 := filepath.Join(t.TempDir(), "a", "differently", "nested", "path")
		if err := sfx.WriteAll(first, dir1); err != nil {
			t.Fatalf("WriteAll dir1: %v", err)
		}
		if err := sfx.WriteAll(second, dir2); err != nil {
			t.Fatalf("WriteAll dir2: %v", err)
		}

		for _, name := range []string{"registry_generated.go", "catalog.md", "fixtures.json"} {
			a := readFile(t, filepath.Join(dir1, name))
			b := readFile(t, filepath.Join(dir2, name))
			if string(a) != string(b) {
				t.Errorf("%s differs between two independent output directories", name)
			}
		}

		digest := sfx.CombinedDigest(first.RegistryGo, first.CatalogMD, first.FixturesJSON)
		if digest != sfx.CombinedDigest(second.RegistryGo, second.CatalogMD, second.FixturesJSON) {
			t.Error("combined content digest differs between two runs")
		}
		t.Logf("stable content digest: %s", digest)
	})

	t.Run("NoUnresolvedReferencesInTheValidFixture", func(t *testing.T) {
		catalog := loadValidCatalog(t)
		if len(catalog.Definitions) != 14 {
			t.Errorf("compiled %d definitions, want 14", len(catalog.Definitions))
		}
		if len(catalog.CapabilityManifest) != 10 {
			t.Errorf("compiled %d capability manifest entries, want 10", len(catalog.CapabilityManifest))
		}
	})

	t.Run("RecordQualificationDecision", func(t *testing.T) {
		catalog := loadValidCatalog(t)
		artifacts, err := sfx.Generate(catalog, "schemaflux")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		mismatches, err := sfx.CrossCheckCompiled(catalog)
		if err != nil {
			t.Fatalf("CrossCheckCompiled: %v", err)
		}

		second, err := sfx.Generate(catalog, "schemaflux")
		if err != nil {
			t.Fatalf("Generate (second run): %v", err)
		}
		byteIdentical := string(artifacts.RegistryGo) == string(second.RegistryGo) &&
			string(artifacts.CatalogMD) == string(second.CatalogMD) &&
			string(artifacts.FixturesJSON) == string(second.FixturesJSON)

		record := sfx.QualificationRecord{
			SchemaFluxVersion:    "v1.2.0",
			DefinitionsCompiled:  len(catalog.Definitions),
			CapabilityEntries:    len(catalog.CapabilityManifest),
			NetworkCallsMade:     0,
			RunsCompared:         2,
			ByteIdentical:        byteIdentical,
			CrossCheckMismatches: len(mismatches),
			ContentDigest:        sfx.CombinedDigest(artifacts.RegistryGo, artifacts.CatalogMD, artifacts.FixturesJSON),
		}

		out := filepath.Join(findRepoRoot(t), "definitions", "generation", "schemaflux-qualification.yaml")
		if err := sfx.WriteFile(out, record.YAML()); err != nil {
			t.Fatalf("write %s: %v", out, err)
		}
		t.Logf("wrote qualification record to %s (digest %s)", out, record.ContentDigest)
	})
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
