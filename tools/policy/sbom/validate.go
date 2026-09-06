package sbom

import (
	"fmt"
	"sort"
	"time"
)

// RootModulePath is the module path this repository's root component must
// resolve to (TOOL-017's GREEN clause names it explicitly).
const RootModulePath = "github.com/monstercameron/hcm-next"

// Completeness reports every way a generated Document fails TOOL-017's
// completeness contract. An empty result means the document is complete.
type Completeness struct {
	MissingRequires       []string // "path@version" from go.mod not found as a component
	VersionlessRefs       []string // component/root identifiers (name or "root") with an empty version
	RootMismatch          string   // non-empty when the root component's name is wrong
	MissingLicenses       []string // vendored "path@version" entries without a license or exception
	InvalidLicenseRecords []string // malformed, expired, or misapplied license exceptions
}

// Empty reports whether the document is complete.
func (c Completeness) Empty() bool {
	return len(c.MissingRequires) == 0 && len(c.VersionlessRefs) == 0 && c.RootMismatch == "" && len(c.MissingLicenses) == 0 && len(c.InvalidLicenseRecords) == 0
}

// Errors renders every finding as a human-readable line, for test failure
// messages and CLI diagnostics.
func (c Completeness) Errors() []string {
	var out []string
	if c.RootMismatch != "" {
		out = append(out, c.RootMismatch)
	}
	for _, m := range c.MissingRequires {
		out = append(out, fmt.Sprintf("go.mod requires %s but the SBOM has no matching component", m))
	}
	for _, v := range c.VersionlessRefs {
		out = append(out, fmt.Sprintf("component %s has no version", v))
	}
	for _, license := range c.MissingLicenses {
		out = append(out, fmt.Sprintf("component %s has no declared license and no complete exception", license))
	}
	for _, record := range c.InvalidLicenseRecords {
		out = append(out, fmt.Sprintf("license exception %s", record))
	}
	return out
}

// ValidateCompleteness checks doc against every go.mod require in root:
// every required module must appear as a component at the same version,
// the root component's name must be RootModulePath, and no component
// (including the root) may have an empty version.
func ValidateCompleteness(doc *Document, root string) (Completeness, error) {
	requires, err := ParseRequires(root)
	if err != nil {
		return Completeness{}, err
	}
	return validateCompletenessAt(doc, requires, time.Now().UTC()), nil
}

// ValidateCompletenessAt is the deterministic form of ValidateCompleteness;
// it is used by conformance tests and release tooling that has a pinned
// evaluation time.
func ValidateCompletenessAt(doc *Document, root string, asOf time.Time) (Completeness, error) {
	requires, err := ParseRequires(root)
	if err != nil {
		return Completeness{}, err
	}
	return validateCompletenessAt(doc, requires, asOf), nil
}

func validateCompletenessAt(doc *Document, requires []Require, asOf time.Time) Completeness {
	result := ValidateCompletenessAgainst(doc, requires)
	result = mergeCompleteness(result, ValidateLicenseCompletenessAt(doc, asOf))
	return result
}

// ValidateCompletenessAgainst is ValidateCompleteness without the go.mod
// read, for callers (and tests) that already have the require list, or want
// to check a document against a synthetic one.
func ValidateCompletenessAgainst(doc *Document, requires []Require) Completeness {
	var result Completeness

	if doc.Metadata.Component.Name != RootModulePath {
		result.RootMismatch = fmt.Sprintf("root component name = %q, want %q", doc.Metadata.Component.Name, RootModulePath)
	}
	if doc.Metadata.Component.Version == "" {
		result.VersionlessRefs = append(result.VersionlessRefs, "root ("+doc.Metadata.Component.Name+")")
	}

	present := make(map[string]string, len(doc.Components)) // path -> version
	for _, c := range doc.Components {
		present[c.Name] = c.Version
		if c.Version == "" {
			result.VersionlessRefs = append(result.VersionlessRefs, c.Name)
		}
	}

	for _, r := range requires {
		version, ok := present[r.Path]
		if !ok || version != r.Version {
			result.MissingRequires = append(result.MissingRequires, r.Path+"@"+r.Version)
		}
	}

	return result
}

// ValidateLicenseCompletenessAt verifies that every dependency component in a
// document has a declared license expression. UNKNOWN and an empty value are
// both unresolved. A complete, exact-revision exception is acceptable only
// until its expiry date; a root metadata component is not checked because it
// is the release subject, not a vendored dependency.
func ValidateLicenseCompletenessAt(doc *Document, asOf time.Time) Completeness {
	var result Completeness
	if doc == nil {
		result.InvalidLicenseRecords = []string{"document is nil"}
		return result
	}

	exceptions := make(map[string][]LicenseException, len(doc.LicenseExceptions))
	for _, exception := range doc.LicenseExceptions {
		key := exception.Component + "@" + exception.Version
		if missing := exception.Complete(); len(missing) > 0 {
			result.InvalidLicenseRecords = append(result.InvalidLicenseRecords, fmt.Sprintf("%s is missing %v", key, missing))
			continue
		}
		expiry, err := time.Parse("2006-01-02", exception.Expiry)
		if err != nil {
			result.InvalidLicenseRecords = append(result.InvalidLicenseRecords, fmt.Sprintf("%s has invalid expiry %q", key, exception.Expiry))
			continue
		}
		if !expiry.After(asOf.UTC().Truncate(24 * time.Hour)) {
			result.InvalidLicenseRecords = append(result.InvalidLicenseRecords, fmt.Sprintf("%s expired on %s", key, exception.Expiry))
			continue
		}
		exceptions[key] = append(exceptions[key], exception)
	}

	for _, component := range doc.Components {
		if component.License != "" && component.License != UnknownLicense {
			continue
		}
		key := component.Name + "@" + component.Version
		if len(exceptions[key]) == 0 {
			result.MissingLicenses = append(result.MissingLicenses, key)
		}
	}
	sort.Strings(result.MissingLicenses)
	sort.Strings(result.InvalidLicenseRecords)
	return result
}

func mergeCompleteness(left, right Completeness) Completeness {
	left.MissingLicenses = append(left.MissingLicenses, right.MissingLicenses...)
	left.InvalidLicenseRecords = append(left.InvalidLicenseRecords, right.InvalidLicenseRecords...)
	return left
}
