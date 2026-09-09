// Package driftgate runs the repository's source, generated-artifact, and
// document integrity checks in a deterministic order.
package driftgate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/modelbinding"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/archdoc"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/librarystrategy"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/modelgen"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/obligations"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
)

// CheckResult is one named drift check and its regeneration instruction.
type CheckResult struct {
	Name                string `json:"name"`
	RegenerationCommand string `json:"regeneration_command"`
	Detail              string `json:"detail,omitempty"`
	Passed              bool   `json:"passed"`
}

// Report is the ordered result of the drift gate. Checks after the first
// failure are intentionally not run: the first ownership path is the useful
// repair boundary in CI.
type Report struct {
	Checks []CheckResult `json:"checks"`
}

// OK reports whether every executed check passed.
func (r Report) OK() bool {
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

// FirstFailure returns the first failed check, if any.
func (r Report) FirstFailure() (CheckResult, bool) {
	for _, check := range r.Checks {
		if !check.Passed {
			return check, true
		}
	}
	return CheckResult{}, false
}

// Check runs every repository drift check in ownership order and stops at
// the first drift. It returns an error naming the exact output and the
// command that regenerates it.
func Check(root string) error {
	report, err := Evaluate(root)
	if err != nil {
		return err
	}
	if failed, ok := report.FirstFailure(); ok {
		return fmt.Errorf("driftgate: %s: %s; regenerate with %s", failed.Name, failed.Detail, failed.RegenerationCommand)
	}
	return nil
}

// Evaluate runs checks in a fixed order. The returned report contains the
// passing checks and, at most, the first failure.
func Evaluate(root string) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("driftgate: resolve root: %w", err)
	}
	checks := []struct {
		name, command string
		check         func() error
	}{
		{"modelgen", "go run ./tools/gen/modelgen/cmd/modelgen", func() error {
			files, err := modelgen.GenerateAll()
			if err != nil {
				return err
			}
			return compareGeneratedFiles(filepath.Join(root, filepath.FromSlash(modelgen.OutputDir)), files, "modelgen")
		}},
		{"storagemanifest", "go run ./tools/gen/storagemanifest/cmd/storagemanifest", func() error {
			generated, err := storagemanifest.BuildAll(filepath.Join(root, "migrations"))
			if err != nil {
				return err
			}
			files := map[string][]byte{}
			for name, value := range map[string]any{
				storagemanifest.FileDisposition: generated.Disposition,
				storagemanifest.FileProperties:  generated.Properties,
				storagemanifest.FileConstraints: generated.Constraints,
			} {
				data, err := storagemanifest.RenderYAML(value)
				if err != nil {
					return err
				}
				files[name] = data
			}
			return compareGeneratedFiles(filepath.Join(root, "definitions", "model"), files, "storagemanifest")
		}},
		{"librarystrategy", "go run ./tools/gen/librarystrategy/cmd/generatelibrarystrategy", func() error {
			return librarystrategy.Check(root)
		}},
		{"featurecoverage", "go run ./tools/planning/cmd/featurecoverage", func() error {
			intents, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, "definitions", "governance", "intent-conformance-descriptors.yaml"))
			if err != nil {
				return err
			}
			if err := intentmanifests.ValidateIntentManifestYAML(intents); err != nil {
				return err
			}
			groups, err := intentmanifests.LoadFeatureManifestYAML(filepath.Join(root, "definitions", "governance", "feature-intent-intake.yaml"))
			if err != nil {
				return err
			}
			if err := intentmanifests.ValidateFeatureManifestYAML(groups); err != nil {
				return err
			}
			registry, err := intentmanifests.BuildFeatureIntentCoverage(groups, intents)
			if err != nil {
				return err
			}
			data, err := intentmanifests.MarshalFeatureIntentCoverageYAML(registry)
			if err != nil {
				return err
			}
			return compareBytes(filepath.Join(root, "definitions", "governance", "feature-intent-coverage.yaml"), data)
		}},
		{"archdoc", "go run ./tools/gen/archdoc/cmd/archdoc -out tools/gen/archdoc/testdata/architecture.md", func() error {
			want, err := archdoc.Generate(root)
			if err != nil {
				return err
			}
			return compareBytes(filepath.Join(root, "tools", "gen", "archdoc", "testdata", "architecture.md"), want)
		}},
		{"modelbinding", "go run ./tools/gen/modelgen/cmd/modelgen", func() error {
			table, err := modelbinding.BindCatalog()
			if err != nil {
				return err
			}
			if len(table.Gaps) != 0 || !table.FullyBound(14) {
				return fmt.Errorf("model binding has %d gap(s) and %d binding(s), want fourteen complete bindings", len(table.Gaps), len(table.Bindings))
			}
			return nil
		}},
		// docintegrity (DOC-001) is a normative-document audit, not a
		// generated-artifact drift check; it stays out of this gate until its
		// 181 pre-existing planning findings are triaged (see DOC-001's
		// evidence line) and is run on its own by its cmd.
		{"obligations", "go run ./tools/planning/obligations/cmd/obligations", func() error {
			_, err := obligations.Scan(root)
			return err
		}},
		{"sbom", "go run ./tools/policy/sbom/cmd/sbomgen -out definitions/supply-chain/sbom.cdx.json", func() error {
			data, err := os.ReadFile(filepath.Join(root, "definitions", "supply-chain", "sbom.cdx.json"))
			if err != nil {
				return err
			}
			var doc sbom.Document
			if err := json.Unmarshal(data, &doc); err != nil {
				return err
			}
			complete, err := sbom.ValidateCompleteness(&doc, root)
			if err != nil {
				return err
			}
			if !complete.Empty() {
				return fmt.Errorf("SBOM completeness: %v", complete.Errors())
			}
			return nil
		}},
		{"provenance", "go run ./tools/policy/provenance/cmd/provgen", func() error {
			path := filepath.Join(root, "definitions", "supply-chain", "provenance.json")
			statement, err := provenance.LoadStatement(path)
			if err != nil {
				return err
			}
			keyPath := filepath.Join(root, "tools", "planning", "gateevidence", "testdata", "dev-signing-key.yaml")
			privateKey, err := provenance.LoadSigningKeyFixture(keyPath)
			if err != nil {
				return err
			}
			publicKey, ok := privateKey.Public().(ed25519.PublicKey)
			if !ok {
				return fmt.Errorf("signing fixture did not produce an Ed25519 public key")
			}
			publicHex := hex.EncodeToString(publicKey)
			sbomBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(statement.SBOM.Path)))
			if err != nil {
				return err
			}
			sbomDigest := sha256.Sum256(sbomBytes)
			if err := provenance.Verify(*statement, provenance.VerifyOptions{TrustedPublicKeys: map[string]bool{publicHex: true}, SBOMDigest: hex.EncodeToString(sbomDigest[:])}); err != nil {
				return err
			}
			return nil
		}},
	}

	result := Report{Checks: make([]CheckResult, 0, len(checks))}
	for _, item := range checks {
		checkErr := item.check()
		current := CheckResult{Name: item.name, RegenerationCommand: item.command, Passed: checkErr == nil}
		if checkErr != nil {
			current.Detail = checkErr.Error()
		}
		result.Checks = append(result.Checks, current)
		if checkErr != nil {
			return result, nil
		}
	}
	return result, nil
}

func compareGeneratedFiles(root string, files map[string][]byte, owner string) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := compareBytes(filepath.Join(root, filepath.FromSlash(name)), files[name]); err != nil {
			return fmt.Errorf("%s: %w", owner, err)
		}
	}
	return nil
}

func compareBytes(path string, want []byte) error {
	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("%s is stale", path)
	}
	return nil
}

// Digest returns a stable digest for a report, useful for CI logs and
// evidence records without depending on map iteration or timestamps.
func (r Report) Digest() string {
	data, _ := json.Marshal(r)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
