package gen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// pinnedModuleVersionRe matches a go.mod require line's version token, e.g.
// "v1.36.12" or "v1.6.2". A bare "latest", an empty string, or anything not
// starting with a concrete "vMAJOR.MINOR.PATCH" is treated as floating.
var pinnedModuleVersionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+`)

// requireLineRe matches one go.mod require-block line:
// "\t<module path> <version> [// indirect]".
var requireLineRe = regexp.MustCompile(`(?m)^[ \t]*(\S+)[ \t]+(\S+)[ \t]*(?://.*)?$`)

// remotePluginRe detects a buf.gen.yaml plugin entry that uses a remote
// (BSR-hosted) plugin instead of a go.mod-pinned local one.
var remotePluginRe = regexp.MustCompile(`(?m)^\s*-?\s*remote:\s*\S+`)

// generatorPins is the set of facts TestGeneratorLockRejectsFloatingVersion
// extracts from go.mod and buf.gen.yaml and records in gen/TOOLS.lock.
type generatorPins struct {
	BufVersion             string `json:"buf_version"`
	ProtocGenGoVersion     string `json:"protoc_gen_go_version"`
	ProtocGenGoGrpcVersion string `json:"protoc_gen_go_grpc_version"`
}

// extractRequiredVersion returns the pinned version go.mod's require block
// declares for modulePath, or an error if it is missing or not a concrete
// semantic version.
func extractRequiredVersion(goModText, modulePath string) (string, error) {
	for _, m := range requireLineRe.FindAllStringSubmatch(goModText, -1) {
		if m[1] != modulePath {
			continue
		}
		version := m[2]
		if !pinnedModuleVersionRe.MatchString(version) {
			return "", fmt.Errorf("module %s has a non-pinned version %q in go.mod", modulePath, version)
		}
		return version, nil
	}
	return "", fmt.Errorf("module %s has no require entry in go.mod", modulePath)
}

// validateGeneratorPins is the RED/GREEN gate for TOOL-002: it fails closed
// whenever buf.gen.yaml names a remote plugin, or whenever go.mod does not
// pin protoc-gen-go/protoc-gen-go-grpc to an exact version.
func validateGeneratorPins(goModText, bufGenYAMLText string) (generatorPins, error) {
	if loc := remotePluginRe.FindString(bufGenYAMLText); loc != "" {
		return generatorPins{}, fmt.Errorf("buf.gen.yaml declares a remote plugin (%q); only go.mod-pinned local plugins are allowed", loc)
	}

	goVersion, err := extractRequiredVersion(goModText, "google.golang.org/protobuf")
	if err != nil {
		return generatorPins{}, fmt.Errorf("protoc-gen-go (google.golang.org/protobuf): %w", err)
	}
	grpcVersion, err := extractRequiredVersion(goModText, "google.golang.org/grpc/cmd/protoc-gen-go-grpc")
	if err != nil {
		return generatorPins{}, fmt.Errorf("protoc-gen-go-grpc: %w", err)
	}

	return generatorPins{
		ProtocGenGoVersion:     goVersion,
		ProtocGenGoGrpcVersion: grpcVersion,
	}, nil
}

// TestGeneratorLockRejectsFloatingVersion is the TOOL-002 primary test. It
// proves validateGeneratorPins accepts this repository's current pins,
// rejects a synthetic remote-plugin buf.gen.yaml and a synthetic unpinned
// go.mod, and records the accepted pins plus the installed buf CLI version
// in gen/TOOLS.lock.
func TestGeneratorLockRejectsFloatingVersion(t *testing.T) {
	repoRoot := findRepoRoot(t)

	goModText := readFile(t, filepath.Join(repoRoot, "go.mod"))
	bufGenYAMLText := readFile(t, filepath.Join(repoRoot, "buf.gen.yaml"))

	t.Run("CurrentRepoIsPinned", func(t *testing.T) {
		pins, err := validateGeneratorPins(goModText, bufGenYAMLText)
		if err != nil {
			t.Fatalf("expected the checked-in generator pins to validate, got: %v", err)
		}
		if pins.ProtocGenGoVersion == "" || pins.ProtocGenGoGrpcVersion == "" {
			t.Fatalf("expected non-empty pinned versions, got %+v", pins)
		}
	})

	t.Run("RejectsRemotePlugin", func(t *testing.T) {
		floating := "version: v2\nplugins:\n  - remote: buf.build/protocolbuffers/go\n    out: gen/go\n"
		if _, err := validateGeneratorPins(goModText, floating); err == nil {
			t.Fatal("expected a remote plugin declaration to be rejected as floating, got nil error")
		}
	})

	t.Run("RejectsMissingVersionPin", func(t *testing.T) {
		unpinnedGoMod := "module example.com/x\n\ngo 1.26.3\n\ntool (\n\tgoogle.golang.org/protobuf/cmd/protoc-gen-go\n)\n"
		if _, err := validateGeneratorPins(unpinnedGoMod, bufGenYAMLText); err == nil {
			t.Fatal("expected a go.mod with no require entry for protoc-gen-go to be rejected, got nil error")
		}
	})

	t.Run("RejectsNonSemverVersion", func(t *testing.T) {
		floatingGoMod := "module example.com/x\n\ngo 1.26.3\n\nrequire (\n\tgoogle.golang.org/protobuf latest\n\tgoogle.golang.org/grpc/cmd/protoc-gen-go-grpc v1.6.2\n)\n"
		if _, err := validateGeneratorPins(floatingGoMod, bufGenYAMLText); err == nil {
			t.Fatal("expected version \"latest\" to be rejected as floating, got nil error")
		}
	})

	t.Run("WriteAndVerifyToolsLock", func(t *testing.T) {
		pins, err := validateGeneratorPins(goModText, bufGenYAMLText)
		if err != nil {
			t.Fatalf("validateGeneratorPins: %v", err)
		}
		pins.BufVersion = bufVersion(t, repoRoot)

		lockPath := filepath.Join(repoRoot, "gen", "TOOLS.lock")
		encoded, err := json.MarshalIndent(pins, "", "  ")
		if err != nil {
			t.Fatalf("marshal TOOLS.lock: %v", err)
		}
		encoded = append(encoded, '\n')
		if err := os.WriteFile(lockPath, encoded, 0o644); err != nil {
			t.Fatalf("write %s: %v", lockPath, err)
		}

		raw, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read back %s: %v", lockPath, err)
		}
		var readBack generatorPins
		if err := json.Unmarshal(raw, &readBack); err != nil {
			t.Fatalf("unmarshal %s: %v", lockPath, err)
		}
		if readBack != pins {
			t.Fatalf("gen/TOOLS.lock does not match computed pins:\nwant %+v\ngot  %+v", pins, readBack)
		}
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
