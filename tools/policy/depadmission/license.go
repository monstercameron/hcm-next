package depadmission

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SPDX identifiers this package's classifier can recognize. These are the
// identifiers definitions/architecture/dependency-admission.yaml's
// license.allow / license.deny lists reference.
const (
	SPDXMIT          = "MIT"
	SPDXApache20     = "Apache-2.0"
	SPDXBSD2         = "BSD-2-Clause"
	SPDXBSD3         = "BSD-3-Clause"
	SPDXISC          = "ISC"
	SPDXMPL20        = "MPL-2.0"
	SPDXGPL2         = "GPL-2.0"
	SPDXGPL3         = "GPL-3.0"
	SPDXAGPL3        = "AGPL-3.0"
	SPDXSSPL1        = "SSPL-1.0"
	SPDXPublicDomain = "Public-Domain"
)

// licenseFileNames are the file basenames DetectLicense looks for, in
// order of preference, at the top level of a module directory. Go tooling
// and most module authors use one of these; "LICENCE" (British spelling)
// is included because at least one direct dependency
// (github.com/joho/godotenv) ships only that spelling.
var licenseFileNames = []string{
	"LICENSE",
	"LICENSE.txt",
	"LICENSE.md",
	"LICENSE-MIT",
	"LICENSE-APACHE",
	"LICENCE",
	"LICENCE.txt",
	"COPYING",
	"COPYING.txt",
}

// LicenseResult is what DetectLicense reports for one module directory.
type LicenseResult struct {
	// SPDX is the recognized identifier, or "" when no file was found or
	// none recognized.
	SPDX string
	// Found reports whether a candidate license file existed at all
	// (distinct from SPDX=="": a file can exist and still be
	// unrecognized).
	Found bool
	// Evidence is the file DetectLicense classified, relative to dir.
	Evidence string
}

// DetectLicense reads the top-level candidate license files in dir (a
// module's extracted source directory, e.g. from `go list -m -json`'s Dir
// field) and classifies the first one found. It never recurses: a nested
// LICENSE file belonging to a vendored sub-component is not this module's
// own license.
func DetectLicense(dir string) (LicenseResult, error) {
	for _, name := range licenseFileNames {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return LicenseResult{}, fmt.Errorf("depadmission: reading %s: %w", path, err)
		}
		return LicenseResult{
			SPDX:     classifyLicenseText(string(data)),
			Found:    true,
			Evidence: name,
		}, nil
	}
	return LicenseResult{}, nil
}

// classifyLicenseText applies ordered, keyword-based heuristics to a
// license file's text. Order matters: more specific families (AGPL, GPL
// version, BSD clause count) are checked before the family they would
// otherwise be mistaken for. It returns "" when nothing matches, which the
// caller must treat as unknown, never as an implicit allow.
func classifyLicenseText(text string) string {
	upper := strings.ToUpper(text)

	switch {
	case strings.Contains(upper, "GNU AFFERO GENERAL PUBLIC LICENSE"):
		return SPDXAGPL3
	case strings.Contains(upper, "SERVER SIDE PUBLIC LICENSE"):
		return SPDXSSPL1
	case strings.Contains(upper, "GNU GENERAL PUBLIC LICENSE") && strings.Contains(upper, "VERSION 3"):
		return SPDXGPL3
	case strings.Contains(upper, "GNU GENERAL PUBLIC LICENSE") && strings.Contains(upper, "VERSION 2"):
		return SPDXGPL2
	case strings.Contains(upper, "MOZILLA PUBLIC LICENSE") && strings.Contains(text, "2.0"):
		return SPDXMPL20
	case strings.Contains(upper, "PUBLIC DOMAIN"):
		return SPDXPublicDomain
	case strings.Contains(upper, "APACHE LICENSE") && strings.Contains(text, "2.0"):
		return SPDXApache20
	case strings.Contains(upper, "ISC LICENSE") || strings.Contains(upper, "PERMISSION TO USE, COPY, MODIFY, AND/OR DISTRIBUTE"):
		return SPDXISC
	case strings.Contains(upper, "REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS"):
		if strings.Contains(upper, "NEITHER THE NAME") {
			return SPDXBSD3
		}
		return SPDXBSD2
	case strings.Contains(upper, "MIT LICENSE") || strings.Contains(text, "Permission is hereby granted, free of charge"):
		return SPDXMIT
	default:
		return ""
	}
}

// ModuleInfo is the subset of `go list -m -json` per-module output this
// package needs.
type ModuleInfo struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Main     bool   `json:"Main"`
	Indirect bool   `json:"Indirect"`
	Dir      string `json:"Dir"`
}

// ListModules runs `go list -m -json` from root over exactly the given
// module paths and returns each one's module-cache extraction directory.
//
// This deliberately takes an explicit path list rather than the "all"
// pattern. `go list -m -json all` walks the *entire* module graph,
// including the go.mod files of modules no package here ever imports
// (module graph pruning already keeps go.mod's own require block to what
// is actually needed - see dependency-roles.yaml's "Scope note"), and a
// single malformed go.mod anywhere in that wider graph fails the whole
// call; this repository's own dependency tree currently demonstrates
// exactly that failure mode. Querying the explicit require-block paths
// (from depmanifest.ParseGoModRequires) only resolves modules already
// pinned in this module's own build list, so it succeeds whenever `go
// build ./...` does. It requires the module cache to already hold every
// named module (true after `go build ./...` or `go mod download`); it
// makes no network request itself.
func ListModules(root string, modulePaths []string) ([]ModuleInfo, error) {
	if len(modulePaths) == 0 {
		return nil, nil
	}

	args := append([]string{"list", "-m", "-json"}, modulePaths...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("depadmission: go list -m -json %s: %w\nstderr:\n%s", modulePaths, err, stderr.String())
	}

	var modules []ModuleInfo
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var m ModuleInfo
		if err := decoder.Decode(&m); err != nil {
			return nil, fmt.Errorf("depadmission: decoding go list -m output: %w", err)
		}
		modules = append(modules, m)
	}
	return modules, nil
}
