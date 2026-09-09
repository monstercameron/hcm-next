// Package releaseboundary enforces the P1B release-image boundary.
//
// The SBOM is the authoritative boundary artifact. This package deliberately
// does not walk the repository or inspect development dependencies: those may
// contain the legacy implementation and developer tooling without belonging
// to the production image.
package releaseboundary

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/sbom"
)

// forbiddenMarkers identify runtime/package names excluded by the Go-only
// constitution. Matching is case-insensitive and applies to the SBOM subject
// and component identity, never to arbitrary repository paths.
var forbiddenMarkers = []string{"node", "nodejs", "npm", "javascript", "typescript", "react", "vite", "next", "nextjs"}

// Check validates the SBOM and rejects an excluded runtime or package in the
// shipped component set. Development tools are out of scope because they are
// not represented in a release SBOM.
func Check(document sbom.Document) error {
	if err := sbom.Validate(document); err != nil {
		return fmt.Errorf("release boundary: invalid SBOM: %w", err)
	}
	if marker := forbiddenMarker(document.Subject.Name); marker != "" {
		return fmt.Errorf("release boundary: subject %q names excluded runtime %q", document.Subject.Name, marker)
	}
	for _, component := range document.Components {
		if marker := forbiddenMarker(component.Name); marker != "" {
			return fmt.Errorf("release boundary: component %q contains excluded runtime/package %q", component.Name, marker)
		}
		// Replacement metadata is part of the shipped module identity. Check it
		// as well so a benign-looking module path cannot hide an excluded
		// runtime in its replacement path.
		if component.Replacement != nil {
			if marker := forbiddenMarker(component.Replacement.Name); marker != "" {
				return fmt.Errorf("release boundary: component %q replacement names excluded runtime/package %q", component.Name, marker)
			}
		}
	}
	return nil
}

// CheckFile reads and checks a canonical release SBOM from disk.
func CheckFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("release boundary: SBOM path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("release boundary: read SBOM: %w", err)
	}
	var document sbom.Document
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("release boundary: parse SBOM: %w", err)
	}
	return Check(document)
}

func forbiddenMarker(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	// Match package/path tokens rather than arbitrary substrings: a Go module
	// such as reactive-streams must not be mistaken for the React runtime.
	tokens := strings.FieldsFunc(value, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, marker := range forbiddenMarkers {
		for _, token := range tokens {
			if token == marker {
				return marker
			}
		}
	}
	return ""
}
