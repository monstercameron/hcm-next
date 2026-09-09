// Package storageproduct implements ALIGN-016's storage-to-product-slice
// reverse index. The index is a deterministic policy view over the existing
// storage alignment and product-slice registries; it is not a second storage
// or product catalog.
package storageproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableownership"
)

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// Finding is a malformed source or ambiguous reverse-index relationship.
type Finding struct {
	Table  string `json:"table"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (f Finding) Error() string {
	if f.Table == "" {
		return "storageproduct: " + f.Code + ": " + f.Detail
	}
	return fmt.Sprintf("storageproduct: %s: %s: %s", f.Table, f.Code, f.Detail)
}

// Entry is one storage row in the reverse index. ProductSlices is empty when
// the table is valid storage but no admitted product slice has declared the
// owning or consuming package. Such a row is retained rather than dropped:
// absence is evidence that the storage is not currently admitted to a slice.
type Entry struct {
	Table         string   `json:"table"`
	Role          string   `json:"role"`
	OwnerPackage  string   `json:"owner_package"`
	Migration     string   `json:"migration"`
	TenantScoped  bool     `json:"tenant_scoped"`
	Consumers     []string `json:"consumers,omitempty"`
	ProductSlices []string `json:"product_slices,omitempty"`
}

// Index is the generated, in-memory reverse index. It never writes a
// generated file: definitions remain the source authorities.
type Index struct {
	SchemaVersion int       `json:"schema_version"`
	Entries       []Entry   `json:"entries"`
	Findings      []Finding `json:"findings,omitempty"`
}

// OK reports whether the source inputs were structurally usable. An empty
// ProductSlices list on an entry is not itself a finding because a storage
// table may belong to a future or not-yet-admitted product slice.
func (i Index) OK() bool { return len(i.Findings) == 0 }

// Digest is a stable identity for the generated reverse index.
func (i Index) Digest() string {
	type canonical struct {
		SchemaVersion int       `json:"schema_version"`
		Entries       []Entry   `json:"entries"`
		Findings      []Finding `json:"findings,omitempty"`
	}
	b, _ := json.Marshal(canonical{i.SchemaVersion, i.Entries, i.Findings})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns bounded structural facts suitable for logs and evidence.
func (i Index) Explain() string {
	return fmt.Sprintf("storage-to-product index v%d with %d table(s), %d finding(s) (%s)", schemaVersion, len(i.Entries), len(i.Findings), i.Digest())
}

// Evaluate loads the authoritative storage and product-slice registries and
// scans Go source only for consumer evidence. It does not connect to a
// database, write a generated artifact, or edit either registry.
func Evaluate(root string) (Index, error) {
	inventory, err := tableinventory.Scan(root)
	if err != nil {
		return Index{}, err
	}
	alignment, err := tableinventory.Generate(root)
	if err != nil {
		return Index{}, err
	}
	registry, err := productslice.LoadRegistryYAML(filepath.Join(root, "definitions", "planning", "product-slices.yaml"))
	if err != nil {
		return Index{}, fmt.Errorf("storageproduct: load product slices: %w", err)
	}
	if err := registry.VerifyDigest(); err != nil {
		return Index{}, err
	}
	consumers, err := tableownership.DiscoverConsumers(root, inventory.Tables)
	if err != nil {
		return Index{}, fmt.Errorf("storageproduct: discover consumers: %w", err)
	}
	return Generate(alignment, registry.Slices, consumers), nil
}

// Check evaluates the reverse index and returns all structural findings.
func Check(root string) error {
	index, err := Evaluate(root)
	if err != nil {
		return err
	}
	if !index.OK() {
		parts := make([]string, len(index.Findings))
		for i, finding := range index.Findings {
			parts[i] = finding.Error()
		}
		return fmt.Errorf("%s", strings.Join(parts, "; "))
	}
	return nil
}

// Generate builds a reverse index from an already-generated storage
// alignment, admitted product slices, and table consumer evidence. The pure
// function is the testable policy core; callers may obtain the inputs from
// any source that preserves these declarations.
func Generate(alignment tableinventory.AlignmentRegistry, slices []productslice.ProductSliceDefinition, consumers map[string][]string) Index {
	findings := validateInputs(alignment, slices)
	entries := make([]Entry, 0, len(alignment.Tables))
	for _, table := range alignment.Tables {
		name := strings.TrimSpace(table.Table)
		if name == "" {
			continue
		}
		packages := normalizeStrings(consumers[name])
		if len(packages) == 0 {
			packages = normalizeStrings(consumers[strings.ToLower(name)])
		}
		matches := matchingSlices(table.Owner, packages, slices)
		entries = append(entries, Entry{
			Table:         name,
			Role:          table.Role,
			OwnerPackage:  table.Owner,
			Migration:     table.Migration,
			TenantScoped:  table.TenantScoped,
			Consumers:     packages,
			ProductSlices: matches,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Table < entries[j].Table })
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		return findings[i].Code < findings[j].Code
	})
	return Index{SchemaVersion: schemaVersion, Entries: entries, Findings: findings}
}

func validateInputs(alignment tableinventory.AlignmentRegistry, slices []productslice.ProductSliceDefinition) []Finding {
	var findings []Finding
	seen := make(map[string]bool, len(alignment.Tables))
	for _, table := range alignment.Tables {
		name := strings.TrimSpace(table.Table)
		if name == "" {
			findings = append(findings, Finding{Code: "TABLE_NAME_MISSING", Detail: "alignment row has no table name"})
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			findings = append(findings, Finding{Table: name, Code: "TABLE_DUPLICATE", Detail: "alignment contains more than one row for the table"})
		}
		seen[key] = true
	}
	seenSlices := make(map[string]bool, len(slices))
	for _, slice := range slices {
		id := strings.TrimSpace(slice.SliceID)
		if id == "" {
			findings = append(findings, Finding{Code: "SLICE_ID_MISSING", Detail: "product slice has no slice_id"})
			continue
		}
		if slice.Version < 1 {
			findings = append(findings, Finding{Code: "SLICE_VERSION_INVALID", Detail: fmt.Sprintf("product slice %q has version %d", id, slice.Version)})
		}
		if seenSlices[id] {
			findings = append(findings, Finding{Code: "SLICE_DUPLICATE", Detail: fmt.Sprintf("product slice %q appears more than once", id)})
		}
		seenSlices[id] = true
	}
	return findings
}

func matchingSlices(owner string, consumers []string, slices []productslice.ProductSliceDefinition) []string {
	owner = normalizePackage(owner)
	consumerSet := make(map[string]bool, len(consumers))
	for _, consumer := range consumers {
		consumerSet[normalizePackage(consumer)] = true
	}
	var matches []string
	for _, slice := range slices {
		for _, declared := range slice.Packages {
			pkg := normalizePackage(declared)
			if pkg == "" {
				continue
			}
			if pkg == owner || consumerSet[pkg] {
				matches = append(matches, fmt.Sprintf("%s@%d", slice.SliceID, slice.Version))
				break
			}
		}
	}
	sort.Strings(matches)
	return matches
}

func normalizePackage(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	const module = "github.com/monstercameron/human-capital-management-suite/"
	return strings.TrimPrefix(pkg, module)
}

func normalizeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if value == "" || (len(result) > 0 && result[len(result)-1] == value) {
			continue
		}
		result = append(result, value)
	}
	return result
}
