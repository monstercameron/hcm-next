// Package depmanifest implements the LIB-001 dependency-role and
// ownership policy: classifying every module go.mod requires against
// definitions/architecture/dependency-roles.yaml.
package depmanifest

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	RoleProjectCore            = "PROJECT_CORE"
	RoleInfrastructureMechanic = "INFRASTRUCTURE_MECHANIC"
	RoleDevTestOnly            = "DEV_TEST_ONLY"
	RoleProhibited             = "PROHIBITED"
)

// ModuleRow is one exact-match classification row.
type ModuleRow struct {
	Path                 string   `yaml:"path"`
	Version              string   `yaml:"version"`
	Role                 string   `yaml:"role"`
	SemanticOwner        string   `yaml:"semantic_owner"`
	LicenseOwner         string   `yaml:"license_owner"`
	SecurityOwner        string   `yaml:"security_owner"`
	AllowedImportRoots   []string `yaml:"allowed_import_roots"`
	DirectImportExpected bool     `yaml:"direct_import_expected"`
	UpgradeSLA           string   `yaml:"upgrade_sla"`
	Exposure             string   `yaml:"exposure"`
	ReplacementStrategy  string   `yaml:"replacement_strategy"`
}

// FamilyRule is a prefix-based default classification.
type FamilyRule struct {
	Prefix              string   `yaml:"prefix"`
	Role                string   `yaml:"role"`
	SemanticOwner       string   `yaml:"semantic_owner"`
	LicenseOwner        string   `yaml:"license_owner"`
	SecurityOwner       string   `yaml:"security_owner"`
	UpgradeSLA          string   `yaml:"upgrade_sla"`
	Exposure            string   `yaml:"exposure"`
	ReplacementStrategy string   `yaml:"replacement_strategy"`
	AllowedImportRoots  []string `yaml:"allowed_import_roots"`
}

// ProjectCoreReserved is a documented PROJECT_CORE-eligible name; only
// modules named here may ever be classified PROJECT_CORE.
type ProjectCoreReserved struct {
	Name string `yaml:"name"`
	Note string `yaml:"note"`
}

// Manifest is the parsed form of dependency-roles.yaml.
type Manifest struct {
	Version             int                   `yaml:"version"`
	Module              string                `yaml:"module"`
	ProjectCoreReserved []ProjectCoreReserved `yaml:"project_core_reserved"`
	FamilyRules         []FamilyRule          `yaml:"family_rules"`
	Modules             []ModuleRow           `yaml:"modules"`
}

// Load reads and parses the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("depmanifest: reading manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("depmanifest: parsing manifest: %w", err)
	}
	return &m, nil
}

// Classification is what Classify returns for one module path.
type Classification struct {
	Found bool
	Row   ModuleRow // populated whether matched exactly or via a family rule
	Exact bool      // true when an exact `modules[].path` row matched
}

// Classify looks up modulePath, first against exact module rows, then
// against family_rules prefixes (first match wins). It returns
// Found:false when nothing matches.
func (m *Manifest) Classify(modulePath string) Classification {
	for _, row := range m.Modules {
		if row.Path == modulePath {
			return Classification{Found: true, Row: row, Exact: true}
		}
	}
	for _, fam := range m.FamilyRules {
		if strings.HasPrefix(modulePath, fam.Prefix) {
			return Classification{
				Found: true,
				Row: ModuleRow{
					Path:                modulePath,
					Role:                fam.Role,
					SemanticOwner:       fam.SemanticOwner,
					LicenseOwner:        fam.LicenseOwner,
					SecurityOwner:       fam.SecurityOwner,
					AllowedImportRoots:  fam.AllowedImportRoots,
					UpgradeSLA:          fam.UpgradeSLA,
					Exposure:            fam.Exposure,
					ReplacementStrategy: fam.ReplacementStrategy,
				},
			}
		}
	}
	return Classification{Found: false}
}

// IsProjectCoreEligible reports whether name is one of the manifest's
// documented project_core_reserved entries.
func (m *Manifest) IsProjectCoreEligible(name string) bool {
	for _, r := range m.ProjectCoreReserved {
		if r.Name == name {
			return true
		}
	}
	return false
}

// ValidRole reports whether role is one of the four allowed role values.
func ValidRole(role string) bool {
	switch role {
	case RoleProjectCore, RoleInfrastructureMechanic, RoleDevTestOnly, RoleProhibited:
		return true
	default:
		return false
	}
}

// RowIsComplete reports whether row carries every field LIB-001 requires:
// version, role, owner, allowed import roots, license/security owner,
// upgrade SLA and replacement path. AllowedImportRoots may legitimately be
// empty (transitive-only dependencies), so it is not required to be
// non-empty, only present as a field (always true for a decoded struct).
func RowIsComplete(row ModuleRow) []string {
	var missing []string
	if row.Version == "" {
		missing = append(missing, "version")
	}
	if !ValidRole(row.Role) {
		missing = append(missing, "role")
	}
	if row.SemanticOwner == "" {
		missing = append(missing, "semantic_owner")
	}
	if row.LicenseOwner == "" {
		missing = append(missing, "license_owner")
	}
	if row.SecurityOwner == "" {
		missing = append(missing, "security_owner")
	}
	if row.UpgradeSLA == "" {
		missing = append(missing, "upgrade_sla")
	}
	if row.Exposure == "" {
		missing = append(missing, "exposure")
	}
	if row.ReplacementStrategy == "" {
		missing = append(missing, "replacement_strategy")
	}
	return missing
}

// GoModRequire is one entry from go.mod's require block.
type GoModRequire struct {
	Path     string
	Version  string
	Indirect bool
}
