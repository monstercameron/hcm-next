package depadmission

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// License disposition values ClassifyLicense returns.
const (
	DispositionAllow   = "ALLOW"
	DispositionDeny    = "DENY"
	DispositionUnknown = "UNKNOWN"
)

// Finding disposition values Evaluate assigns.
const (
	FindingBlocked  = "BLOCK"    // reachable, no valid exception: admission fails
	FindingExcepted = "EXCEPTED" // reachable, but a complete, unexpired, digest-bound exception covers it
	FindingRecorded = "RECORD"   // unreachable: recorded, never dropped, does not by itself block
)

// LicenseOverride is a reviewed, module-specific license declaration for a
// module whose repository ships no machine-detectable LICENSE/COPYING
// file (for example an unpublished internal module). It exists precisely
// so "no file found" never has to be silently treated as an allow.
type LicenseOverride struct {
	SPDX  string `yaml:"spdx"`
	Owner string `yaml:"owner"`
	Note  string `yaml:"note"`
}

// LicensePolicy is the license.* section of dependency-admission.yaml.
type LicensePolicy struct {
	Allow              []string                   `yaml:"allow"`
	Deny               []string                   `yaml:"deny"`
	Overrides          map[string]LicenseOverride `yaml:"overrides"`
	UnknownDisposition string                     `yaml:"unknown_license_disposition"`
}

// VulnerabilityException is one governed, time-boxed, digest-bound waiver
// of a specific reachable finding. TOOL-024's RED clause requires every
// field here to be present for the exception to count at all; Complete
// enforces that.
type VulnerabilityException struct {
	Module              string `yaml:"module"`
	VulnerabilityID     string `yaml:"vulnerability_id"`
	ModuleVersion       string `yaml:"module_version"`
	Owner               string `yaml:"owner"`
	Expiry              string `yaml:"expiry"` // YYYY-MM-DD
	CompensatingControl string `yaml:"compensating_control"`
}

// Complete reports which required fields are missing. A non-empty result
// means the exception cannot be honored: RED requires an incomplete
// exception to behave as if it were absent.
func (e VulnerabilityException) Complete() []string {
	var missing []string
	if e.Module == "" {
		missing = append(missing, "module")
	}
	if e.VulnerabilityID == "" {
		missing = append(missing, "vulnerability_id")
	}
	if e.ModuleVersion == "" {
		missing = append(missing, "module_version")
	}
	if e.Owner == "" {
		missing = append(missing, "owner")
	}
	if e.Expiry == "" {
		missing = append(missing, "expiry")
	}
	if e.CompensatingControl == "" {
		missing = append(missing, "compensating_control")
	}
	return missing
}

// Expired reports whether e's expiry date is on or before now. An
// unparseable expiry is treated as expired: a malformed date must never
// silently extend a waiver.
func (e VulnerabilityException) Expired(now time.Time) bool {
	t, err := time.Parse("2006-01-02", e.Expiry)
	if err != nil {
		return true
	}
	return !now.Before(t)
}

// VulnerabilityPolicy is the vulnerability.* section of
// dependency-admission.yaml.
type VulnerabilityPolicy struct {
	Exceptions []VulnerabilityException `yaml:"exceptions"`
}

// Policy is the parsed form of dependency-admission.yaml.
type Policy struct {
	Version       int                 `yaml:"version"`
	License       LicensePolicy       `yaml:"license"`
	Vulnerability VulnerabilityPolicy `yaml:"vulnerability"`
}

// LoadPolicy reads and parses the policy manifest at path.
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("depadmission: reading policy %s: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("depadmission: parsing policy %s: %w", path, err)
	}
	return &p, nil
}

// ClassifyLicense reports the license.allow/license.deny disposition for
// spdx. An empty spdx (no file recognized and no override) is always
// UNKNOWN regardless of policy contents: unknown can only be escalated to
// a hard failure by the caller, never silently allowed.
func (p *Policy) ClassifyLicense(spdx string) string {
	if spdx == "" {
		return DispositionUnknown
	}
	for _, allowed := range p.License.Allow {
		if allowed == spdx {
			return DispositionAllow
		}
	}
	for _, denied := range p.License.Deny {
		if denied == spdx {
			return DispositionDeny
		}
	}
	return DispositionUnknown
}

// FindException returns the exception matching module/version/vulnID, or
// nil. Matching is exact on all three fields: an exception is digest-bound
// to one pinned module version, so bumping the dependency (even to a
// still-vulnerable version) requires a new, reviewed exception.
func (p *Policy) FindException(module, version, vulnID string) *VulnerabilityException {
	for i := range p.Vulnerability.Exceptions {
		e := &p.Vulnerability.Exceptions[i]
		if e.Module == module && e.ModuleVersion == version && e.VulnerabilityID == vulnID {
			return e
		}
	}
	return nil
}
