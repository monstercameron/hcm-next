// Package sbom generates a CycloneDX 1.5 JSON software bill of materials for
// this module's own dependency graph (TOOL-017), and provides the
// completeness checks a validation gate runs against the generated document.
//
// See doc.go for the full rationale, including why this package reads
// go.mod/go.sum/`go mod graph` instead of `go list -m -json all`.
package sbom

// Document is the top-level CycloneDX 1.5 BOM object. Only the fields this
// generator populates are modeled; CycloneDX documents may carry many more
// optional sections, all safely omitted here.
type Document struct {
	BOMFormat         string             `json:"bomFormat"`
	SpecVersion       string             `json:"specVersion"`
	SerialNumber      string             `json:"serialNumber,omitempty"`
	Version           int                `json:"version"`
	Metadata          Metadata           `json:"metadata"`
	Components        []Component        `json:"components"`
	Dependencies      []Dependency       `json:"dependencies,omitempty"`
	LicenseExceptions []LicenseException `json:"licenseExceptions,omitempty"`
}

// Metadata carries the BOM's own provenance: when it was generated, by what
// tool, and the root component (the subject the whole BOM describes).
type Metadata struct {
	Timestamp string    `json:"timestamp"`
	Tools     []Tool    `json:"tools,omitempty"`
	Component Component `json:"component"`
}

// Tool identifies the generator that produced the document, so a later
// investigation can tell which generator/version emitted a given BOM.
type Tool struct {
	Vendor  string `json:"vendor,omitempty"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Component is one entry in the BOM: either the root application (in
// Metadata.Component) or one Go module in the dependency graph (in
// Document.Components).
type Component struct {
	BOMRef  string `json:"bom-ref,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
	PURL    string `json:"purl"`
	Scope   string `json:"scope,omitempty"`
	Hashes  []Hash `json:"hashes,omitempty"`
	License string `json:"license,omitempty"`
}

// Hash is one content-hash record. Alg values follow CycloneDX's
// hash-alg enumeration (e.g. "SHA-256"); Content is the lowercase hex
// digest, per the CycloneDX schema (go.sum's own base64 h1 hashes are
// decoded and re-encoded as hex to match).
type Hash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

// Dependency records one node's outbound edges in the dependency graph:
// Ref is that node's bom-ref/purl, DependsOn the bom-refs/purls of modules
// it (as far as this generator can prove, see doc.go) requires.
type Dependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

// Component types this generator emits.
const (
	ComponentTypeApplication = "application"
	ComponentTypeLibrary     = "library"
)

// Component scope values this generator emits.
const (
	ScopeRequired = "required"
	ScopeOptional = "optional"
)

// BOMFormat/SpecVersion constants for the document this package emits.
const (
	BOMFormatCycloneDX = "CycloneDX"
	SpecVersion15      = "1.5"
)

// HashAlgSHA256 is the only hash algorithm this generator currently emits
// (go.sum only records SHA-256-based h1 hashes).
const HashAlgSHA256 = "SHA-256"

// UnknownLicense is emitted when no SPDX expression is declared by a
// component's own module metadata or license evidence. It is deliberately a
// visible value rather than an empty field so a consumer cannot mistake an
// unassessed license for an omitted one.
const UnknownLicense = "UNKNOWN"

// LicenseEvidence is the auditable result of resolving one component's
// license. Source is the module-owned file that declared or identified the
// expression (for example "go.mod" or "LICENSE").
type LicenseEvidence struct {
	Expression string
	Source     string
}

// LicenseException is a reviewed, time-bounded explanation for a component
// whose license remains UNKNOWN. Exceptions are part of the generated
// document so a release consumer can see why the missing identity was not
// silently treated as acceptable.
type LicenseException struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	Reason    string `json:"reason"`
	Reviewer  string `json:"reviewer"`
	Expiry    string `json:"expiry"`
}

// Complete reports the missing fields in a license exception.
func (e LicenseException) Complete() []string {
	var missing []string
	if e.Component == "" {
		missing = append(missing, "component")
	}
	if e.Version == "" {
		missing = append(missing, "version")
	}
	if e.Reason == "" {
		missing = append(missing, "reason")
	}
	if e.Reviewer == "" {
		missing = append(missing, "reviewer")
	}
	if e.Expiry == "" {
		missing = append(missing, "expiry")
	}
	return missing
}

// Covers reports whether an exception addresses the exact component
// revision. A license exception never applies to another version.
func (e LicenseException) Covers(c Component) bool {
	return e.Component == c.Name && e.Version == c.Version
}
