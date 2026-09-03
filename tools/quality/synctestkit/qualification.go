package synctestkit

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// EvidenceRow is one test-to-package binding recorded in the qualification
// manifest.
type EvidenceRow struct {
	Test    string `yaml:"test"`
	Package string `yaml:"package"`
}

// Scope is the qualification's declared coverage boundary: what
// testing/synctest is trusted to prove, and what it explicitly is not.
type Scope struct {
	Covers   []string `yaml:"covers"`
	Excludes []string `yaml:"excludes"`
}

// Qualification is the parsed form of
// definitions/toolchain/synctest-qualification.yaml, the TOOL-021
// qualification verdict for testing/synctest.
type Qualification struct {
	Version            int               `yaml:"version"`
	Tool               string            `yaml:"tool"`
	QualifiedGoVersion string            `yaml:"qualified_go_version"`
	Verdict            string            `yaml:"verdict"`
	Scope              Scope             `yaml:"scope"`
	Routing            map[string]string `yaml:"routing"`
	Evidence           []EvidenceRow     `yaml:"evidence"`
	Command            string            `yaml:"command"`
	Recorded           string            `yaml:"recorded"`
}

// LoadQualification reads and parses the qualification manifest at path.
func LoadQualification(path string) (Qualification, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Qualification{}, fmt.Errorf("synctestkit: reading qualification manifest: %w", err)
	}
	var q Qualification
	if err := yaml.Unmarshal(data, &q); err != nil {
		return Qualification{}, fmt.Errorf("synctestkit: parsing qualification manifest: %w", err)
	}
	return q, nil
}
