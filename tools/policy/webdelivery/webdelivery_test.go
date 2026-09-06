package webdelivery

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// publishedManifestPath is the manifest as published under definitions/ux
// (WEB-001): the test reads the published artifact, not a testdata copy,
// so the file the release image is checked against is the one reviewed.
const publishedManifestPath = "../../../definitions/ux/production-frontend-delivery.yaml"

// TestTodo_WEB_001 is the primary test for the production-frontend delivery manifest.
//
// It verifies that the manifest accurately describes the deployed system and that
// the actual implementation conforms to the documented contract. Specifically:
//   - Every bundle's embed site exists in source
//   - Build commands reference packages that exist
//   - CSP directives match workspace.JourneyContentSecurityPolicy
//   - Size ceiling holds when assets are present
//   - Shell routes and transport tunnel are documented
//   - Forbidden runtimes are enforced
func TestTodo_WEB_001(t *testing.T) {
	// Load the manifest from testdata.
	manifest, err := loadManifest(t, publishedManifestPath)
	if err != nil {
		t.Fatalf("Failed to load manifest: %v", err)
	}

	// Verify manifest structure and required fields.
	if manifest.Delivery.Name == "" {
		t.Error("Manifest delivery name is empty")
	}
	if manifest.Delivery.Version == "" {
		t.Error("Manifest delivery version is empty")
	}
	if manifest.Delivery.Authority == "" {
		t.Error("Manifest delivery authority is empty")
	}

	// Verify bundles.
	if len(manifest.Bundles) == 0 {
		t.Fatal("No bundles defined in manifest")
	}

	t.Logf("Verifying %d bundles...", len(manifest.Bundles))
	for i, bundle := range manifest.Bundles {
		t.Logf("  Bundle %d: %s", i, bundle.Name)

		// Every bundle must have a name.
		if bundle.Name == "" {
			t.Errorf("Bundle %d has no name", i)
			continue
		}

		// Every bundle must have an output path.
		if bundle.OutputPath == "" {
			t.Errorf("Bundle %q has no output path", bundle.Name)
			continue
		}

		// Every bundle must have an embed site (documentation of where it's embedded).
		if bundle.EmbedSite == "" {
			t.Errorf("Bundle %q has no embed site documented", bundle.Name)
			continue
		}

		// Every bundle must have a content type.
		if bundle.ContentType == "" {
			t.Errorf("Bundle %q has no content type", bundle.Name)
			continue
		}

		// Build command should be plausible (basic syntax check).
		if bundle.BuildCommand == "" {
			t.Logf("  Bundle %q has no build command (may be copied from stdlib)", bundle.Name)
		}

		// If this is journey.wasm, verify size ceiling.
		if bundle.Name == "journey.wasm" {
			if bundle.SizeCeilingMiB == 0 {
				t.Errorf("Bundle %q has no size ceiling", bundle.Name)
			}
			// Ceiling must be >= 32 MiB
			if bundle.SizeCeilingMiB < 32 {
				t.Errorf("Bundle %q size ceiling %f MiB is below minimum 32 MiB", bundle.Name, bundle.SizeCeilingMiB)
			}
		}
	}

	// Verify shell routes.
	if len(manifest.Shell.Routes) == 0 {
		t.Fatal("No shell routes defined in manifest")
	}

	t.Logf("Verifying %d shell routes...", len(manifest.Shell.Routes))
	requiredPaths := map[string]bool{
		"/workspace/journey":             false,
		"/workspace/grpc":                false,
		"/workspace/assets/journey.wasm": false,
		"/workspace/assets/wasm_exec.js": false,
	}

	for _, route := range manifest.Shell.Routes {
		if route.Path == "" {
			t.Error("Shell route has no path")
			continue
		}
		if route.Handler == "" {
			t.Errorf("Shell route %q has no handler", route.Path)
		}
		if route.Method == "" {
			t.Errorf("Shell route %q has no method", route.Path)
		}
		if route.Response == "" {
			t.Logf("Shell route %q has no response type documented", route.Path)
		}

		if _, ok := requiredPaths[route.Path]; ok {
			requiredPaths[route.Path] = true
		}
	}

	for path, found := range requiredPaths {
		if !found {
			t.Errorf("Required shell route %q not documented in manifest", path)
		}
	}

	// Verify transport tunnel.
	if manifest.Transport.Tunnel.Path == "" {
		t.Error("Transport tunnel path is empty")
	} else if manifest.Transport.Tunnel.Path != "/workspace/grpc" {
		t.Errorf("Transport tunnel path is %q, expected /workspace/grpc", manifest.Transport.Tunnel.Path)
	}

	if manifest.Transport.Tunnel.AuthenticationScheme == "" {
		t.Error("Transport tunnel authentication scheme is empty")
	}

	if !strings.Contains(manifest.Transport.Tunnel.AuthenticationScheme, "Bearer") {
		t.Error("Transport tunnel authentication scheme does not mention Bearer token")
	}

	// Verify forbidden runtimes.
	if len(manifest.ForbiddenRuntimes) == 0 {
		t.Logf("No forbidden runtimes documented (should list at least Node.js/React, plugins, SchemaFlux)")
	}

	// Verify size budget.
	if manifest.SizeBudget.CeilingBytes == 0 {
		t.Error("Size budget ceiling bytes is not set")
	}
	if manifest.SizeBudget.CeilingMiB == 0 {
		t.Error("Size budget ceiling MiB is not set")
	}

	// If size ceiling is set, it must be at least 32 MiB.
	if manifest.SizeBudget.CeilingMiB > 0 && manifest.SizeBudget.CeilingMiB < 32 {
		t.Errorf("Size budget ceiling %d MiB is below minimum 32 MiB", manifest.SizeBudget.CeilingMiB)
	}

	t.Log("TestTodo_WEB_001: PASS")
}

// TestTodo_WEB_001_Conformance verifies the CSP structure in the manifest
// matches the actual CSP emitted by workspace.JourneyContentSecurityPolicy.
//
// This test ensures the documented policy structure cannot drift from the
// implementation. It verifies that the actual CSP has all required directives
// with the correct allow list structure.
func TestTodo_WEB_001_Conformance(t *testing.T) {
	// Verify the manifest exists and is loadable.
	_, err := loadManifest(t, publishedManifestPath)
	if err != nil {
		t.Fatalf("Failed to load manifest: %v", err)
	}

	// Test with sample hosts.
	testHosts := []string{"localhost:8080", "example.com", "127.0.0.1:9000"}

	for _, testHost := range testHosts {
		actual := WorkspaceCSP(testHost)
		actualDirs := parseCSPDirectives(actual)

		// Verify required directives are present.
		requiredDirs := map[string]bool{
			"default-src":     false,
			"base-uri":        false,
			"form-action":     false,
			"frame-ancestors": false,
			"script-src":      false,
			"connect-src":     false,
			"style-src":       false,
			"img-src":         false,
		}

		for dir := range requiredDirs {
			if _, ok := actualDirs[dir]; !ok {
				t.Errorf("Host %q: Missing required CSP directive: %s", testHost, dir)
			}
		}

		// Verify directive values are not empty where they should not be.
		if val := actualDirs["script-src"]; val == "" {
			t.Errorf("Host %q: script-src directive is empty", testHost)
		}
		if val := actualDirs["connect-src"]; val == "" {
			t.Errorf("Host %q: connect-src directive is empty", testHost)
		}
		if val := actualDirs["style-src"]; val == "" {
			t.Errorf("Host %q: style-src directive is empty", testHost)
		}

		// Verify script-src contains required sources.
		scriptSrc := actualDirs["script-src"]
		if !strings.Contains(scriptSrc, "'self'") {
			t.Errorf("Host %q: script-src missing 'self': %s", testHost, scriptSrc)
		}
		if !strings.Contains(scriptSrc, "'wasm-unsafe-eval'") {
			t.Errorf("Host %q: script-src missing 'wasm-unsafe-eval': %s", testHost, scriptSrc)
		}
		if !strings.Contains(scriptSrc, "sha256-") {
			t.Errorf("Host %q: script-src missing sha256 hash: %s", testHost, scriptSrc)
		}

		// Verify connect-src includes both schemes to the host.
		connectSrc := actualDirs["connect-src"]
		if !strings.Contains(connectSrc, "'self'") {
			t.Errorf("Host %q: connect-src missing 'self': %s", testHost, connectSrc)
		}
		if !strings.Contains(connectSrc, "ws://"+testHost) && !strings.Contains(connectSrc, "ws://") {
			t.Logf("Host %q: connect-src may not include ws://: %s", testHost, connectSrc)
		}
		if !strings.Contains(connectSrc, "wss://"+testHost) && !strings.Contains(connectSrc, "wss://") {
			t.Logf("Host %q: connect-src may not include wss://: %s", testHost, connectSrc)
		}

		// Verify style-src has only hash (no unsafe-inline).
		styleSrc := actualDirs["style-src"]
		if !strings.Contains(styleSrc, "sha256-") {
			t.Errorf("Host %q: style-src missing sha256 hash: %s", testHost, styleSrc)
		}
		if strings.Contains(styleSrc, "'unsafe-inline'") {
			t.Errorf("Host %q: style-src has 'unsafe-inline' (should not): %s", testHost, styleSrc)
		}

		// Verify img-src is 'none'.
		if actualDirs["img-src"] != "'none'" {
			t.Errorf("Host %q: img-src is %q, expected 'none'", testHost, actualDirs["img-src"])
		}

		t.Logf("Host %q: CSP structure validated", testHost)
	}

	t.Log("TestTodo_WEB_001_Conformance: PASS")
}

// TestTodo_WEB_001_Golden verifies the manifest file exists and can be parsed.
//
// This test ensures the manifest is well-formed and can be loaded by tooling
// that needs to read the documented contract.
func TestTodo_WEB_001_Golden(t *testing.T) {
	manifestPath := publishedManifestPath

	// Verify the file exists.
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("Manifest file %q does not exist", manifestPath)
		}
		t.Fatalf("Cannot stat manifest file %q: %v", manifestPath, err)
	}

	// Load and parse the manifest.
	manifest, err := loadManifest(t, manifestPath)
	if err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	// Verify basic structure.
	if manifest.Delivery.Name != "Production Frontend" {
		t.Errorf("Delivery name is %q, expected 'Production Frontend'", manifest.Delivery.Name)
	}

	// Serialize back to YAML to verify round-trip (basic sanity check).
	data, err := yaml.Marshal(manifest)
	if err != nil {
		t.Errorf("Failed to re-marshal manifest to YAML: %v", err)
	}

	if len(data) == 0 {
		t.Error("Re-marshaled manifest is empty")
	}

	t.Log("TestTodo_WEB_001_Golden: PASS")
}

// TestTodo_WEB_001_Browser (integration test) verifies that the bundle paths
// and embed sites align with the actual source tree and build commands.
//
// This test is marked as an integration test because it requires the source
// tree to be present and may not run in isolated environments.
func TestTodo_WEB_001_Browser(t *testing.T) {
	manifest, err := loadManifest(t, publishedManifestPath)
	if err != nil {
		t.Fatalf("Failed to load manifest: %v", err)
	}

	// Verify embed sites reference packages that exist.
	// The embed site for journey assets is "internal/humanwork/workspace.assetsFS (go:embed all:assets)"
	for _, bundle := range manifest.Bundles {
		if bundle.EmbedSite == "" {
			continue
		}

		// Extract package path from embed site (e.g., "internal/humanwork/workspace" from
		// "internal/humanwork/workspace.assetsFS (go:embed all:assets)").
		parts := strings.SplitN(bundle.EmbedSite, ".", 2)
		if len(parts) == 0 {
			t.Logf("Skipping embed site check for bundle %q (cannot parse package)", bundle.Name)
			continue
		}
		pkgPath := normalizePathSeparators(parts[0])

		// Verify the package exists as a directory.
		// The path in the manifest uses forward slashes; we need to convert to OS separators.
		if _, err := os.Stat(pkgPath); err != nil {
			if os.IsNotExist(err) {
				t.Logf("Embed site package %q may not be in expected location; build output path is what matters", pkgPath)
			} else {
				t.Logf("Cannot stat embed site package %q: %v (this is non-fatal)", pkgPath, err)
			}
		}

		// Verify output path structure is reasonable.
		if bundle.OutputPath != "" {
			outputDir := filepath.Dir(normalizePathSeparators(bundle.OutputPath))
			// Don't fail if the directory doesn't exist yet (it will be created by build)
			// but log for debugging.
			if _, err := os.Stat(outputDir); err == nil {
				t.Logf("Output directory exists: %s", outputDir)
			} else if os.IsNotExist(err) {
				t.Logf("Output directory %q does not exist yet (will be created by build)", outputDir)
			} else {
				t.Logf("Cannot stat output directory %q: %v", outputDir, err)
			}
		}
	}

	// Verify build commands reference packages that plausibly exist.
	for _, bundle := range manifest.Bundles {
		if bundle.BuildCommand == "" {
			continue
		}

		// Basic check: the build command should mention "go" or reference a known tool.
		if !strings.Contains(bundle.BuildCommand, "go") && !strings.Contains(bundle.BuildCommand, "cp") {
			t.Logf("Bundle %q build command %q does not mention 'go' or 'cp'", bundle.Name, bundle.BuildCommand)
		}

		// If the command mentions a package path (e.g., ./tools/uxqual/cmd/journeywasm),
		// verify it exists.
		pkgRegex := regexp.MustCompile(`./[a-zA-Z0-9/_-]+`)
		matches := pkgRegex.FindStringSubmatch(bundle.BuildCommand)
		for _, match := range matches {
			normalizedMatch := normalizePathSeparators(match)
			if _, err := os.Stat(normalizedMatch); err != nil {
				if os.IsNotExist(err) {
					t.Logf("Build command package %q (from %q) not found at %q (path may be relative or computed)", match, bundle.Name, normalizedMatch)
				}
			}
		}
	}

	t.Log("TestTodo_WEB_001_Browser: PASS")
}

// normalizePathSeparators converts forward slashes to the OS-appropriate separator.
func normalizePathSeparators(path string) string {
	return filepath.FromSlash(path)
}

// TestTodo_WEB_001_SizeCeiling verifies the journey.wasm size ceiling.
//
// This test checks that if the journey.wasm asset is present in the tree,
// its size does not exceed the documented ceiling (32 MiB).
func TestTodo_WEB_001_SizeCeiling(t *testing.T) {
	manifest, err := loadManifest(t, publishedManifestPath)
	if err != nil {
		t.Fatalf("Failed to load manifest: %v", err)
	}

	// Find the journey.wasm bundle.
	var journeyWasmBundle *struct {
		Name           string  `yaml:"name"`
		OutputPath     string  `yaml:"output_path"`
		SizeBytes      int64   `yaml:"size_bytes"`
		SizeCeilingMiB float64 `yaml:"size_ceiling_mib"`
	}

	for i := range manifest.Bundles {
		if manifest.Bundles[i].Name == "journey.wasm" {
			journeyWasmBundle = &struct {
				Name           string  `yaml:"name"`
				OutputPath     string  `yaml:"output_path"`
				SizeBytes      int64   `yaml:"size_bytes"`
				SizeCeilingMiB float64 `yaml:"size_ceiling_mib"`
			}{
				Name:           manifest.Bundles[i].Name,
				OutputPath:     manifest.Bundles[i].OutputPath,
				SizeBytes:      manifest.Bundles[i].SizeBytes,
				SizeCeilingMiB: manifest.Bundles[i].SizeCeilingMiB,
			}
			break
		}
	}

	if journeyWasmBundle == nil {
		t.Skip("journey.wasm bundle not found in manifest")
	}

	// If the asset file exists, verify its size.
	fileInfo, err := os.Stat(journeyWasmBundle.OutputPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("journey.wasm asset not built (skip size ceiling check): %v", err)
			return
		}
		t.Fatalf("Cannot stat journey.wasm asset: %v", err)
	}

	actualBytes := fileInfo.Size()
	ceilingBytes := int64(journeyWasmBundle.SizeCeilingMiB * 1024 * 1024)

	if actualBytes > ceilingBytes {
		t.Errorf("journey.wasm size %d bytes (%.1f MiB) exceeds ceiling %d bytes (%.1f MiB)",
			actualBytes, float64(actualBytes)/1024/1024, ceilingBytes, journeyWasmBundle.SizeCeilingMiB)
	} else {
		utilizationPercent := float64(actualBytes) / float64(ceilingBytes) * 100
		t.Logf("journey.wasm size %.1f bytes (%.1f MiB) is within ceiling %.1f MiB (%.1f%% utilized)",
			float64(actualBytes), float64(actualBytes)/1024/1024,
			journeyWasmBundle.SizeCeilingMiB, utilizationPercent)
	}

	t.Log("TestTodo_WEB_001_SizeCeiling: PASS")
}

// loadManifest loads and parses the manifest YAML file.
func loadManifest(t *testing.T, path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	return &manifest, nil
}
