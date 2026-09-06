package provenance

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadStatement reads and parses a Statement JSON document from path (e.g.
// definitions/supply-chain/provenance.json). It returns a descriptive error
// - never a panic - for a missing file or malformed JSON, so a corrupted or
// partially-written provenance file fails a caller's admission check
// cleanly instead of crashing it.
func LoadStatement(path string) (*Statement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provenance: reading %s: %w", path, err)
	}
	var s Statement
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("provenance: parsing %s: %w", path, err)
	}
	return &s, nil
}

// WriteStatement marshals s as indented JSON (with a trailing newline) and
// writes it to path.
func WriteStatement(path string, s Statement) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("provenance: encoding statement: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("provenance: writing %s: %w", path, err)
	}
	return nil
}
