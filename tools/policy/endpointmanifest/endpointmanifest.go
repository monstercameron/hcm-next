// Package endpointmanifest implements the ENDPOINT-001/PROTO-010 policy
// check: the checked-in definitions/api/endpoint-manifest.json document
// must be exactly what internal/transport/manifest.Build() produces right
// now, from the live Protobuf descriptors, the BOOTSTRAP capability
// registry and the fourteen intent definitions. A mismatch means the file
// was hand-edited or the source tree changed without regenerating it —
// either way, a handwritten route could otherwise slip past review.
package endpointmanifest

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

// DefaultPath is the checked-in generated manifest this checker validates,
// relative to the repository root.
const DefaultPath = "definitions/api/endpoint-manifest.json"

// Check verifies that the JSON document at path parses into exactly the
// [manifest.EndpointManifest] that [manifest.Build] produces from the
// current source tree. Comparison is structural (parse both sides, compare
// the resulting values), not byte-for-byte: a prettier reformatting pass
// must never make this check fail on whitespace alone.
func Check(path string) error {
	want, err := manifest.Build()
	if err != nil {
		return fmt.Errorf("endpointmanifest: building the live manifest: %w", err)
	}
	got, err := Load(path)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, got) {
		return fmt.Errorf(
			"endpointmanifest: %s is stale relative to internal/transport/manifest.Build(); regenerate it with `go run ./tools/policy/endpointmanifest/cmd/endpointmanifest`",
			path,
		)
	}
	return nil
}

// Load reads and parses the checked-in manifest document at path.
func Load(path string) (*manifest.EndpointManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("endpointmanifest: reading %s: %w", path, err)
	}
	var m manifest.EndpointManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("endpointmanifest: parsing %s: %w", path, err)
	}
	return &m, nil
}
