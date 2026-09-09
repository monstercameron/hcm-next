// Package controlcrosswalk compiles the reviewed security-control seed into a
// registry-backed, versioned crosswalk (GOV-030).
package controlcrosswalk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
	"gopkg.in/yaml.v3"
)

const schemaVersion = 1

// Status is derived from the ticked state of the cited todo records.
type Status string

const (
	Implemented Status = "IMPLEMENTED"
	Partial     Status = "PARTIAL"
	Missing     Status = "MISSING"
)

// Ownership identifies whether the control is provided by this product or
// inherited from a platform/provider boundary.
type Ownership string

const (
	Owned     Ownership = "OWNED"
	Inherited Ownership = "INHERITED"
)

// FrameworkRefs names the pinned framework requirement represented by a row.
type FrameworkRefs struct {
	NIST80053 string `yaml:"nist_800_53" json:"nist_800_53"`
	CSF20     string `yaml:"csf_2_0" json:"csf_2_0"`
	ISO27001  string `yaml:"iso_27001_annex_a" json:"iso_27001_annex_a"`
	SOC2      string `yaml:"soc_2_tsc" json:"soc_2_tsc"`
	CIS       string `yaml:"cis_controls" json:"cis_controls"`
	ASVS      string `yaml:"asvs_5_0_0" json:"asvs_5_0_0"`
}

// FrameworkVersions pins the editions represented by every framework column.
type FrameworkVersions struct {
	NIST80053 string `yaml:"nist_800_53" json:"nist_800_53"`
	CSF20     string `yaml:"csf_2_0" json:"csf_2_0"`
	ISO27001  string `yaml:"iso_27001" json:"iso_27001"`
	SOC2      string `yaml:"soc_2_tsc" json:"soc_2_tsc"`
	CIS       string `yaml:"cis_controls" json:"cis_controls"`
	ASVS      string `yaml:"asvs" json:"asvs"`
}

// EvidencePointer is a reviewed pointer into the generated todo registry.
// TestName is resolved from the registry and is never accepted from YAML.
type EvidencePointer struct {
	TodoID   string `yaml:"todo_id" json:"todo_id"`
	TestName string `json:"test_name"`
}

// ControlSeed is the hand-reviewed YAML portion of one crosswalk record.
// DeclaredStatus exists only to detect forbidden hand-typed status claims; it
// is not used as the source of the generated Status.
type ControlSeed struct {
	ID             string            `yaml:"id"`
	Scope          string            `yaml:"scope"`
	Title          string            `yaml:"title"`
	Frameworks     FrameworkRefs     `yaml:"frameworks"`
	OwnerPackage   string            `yaml:"owner_package"`
	TodoIDs        []string          `yaml:"todo_ids"`
	Evidence       []EvidencePointer `yaml:"evidence"`
	Ownership      Ownership         `yaml:"ownership"`
	DeclaredStatus string            `yaml:"status,omitempty"`
}

// Definition is the reviewed YAML seed for a versioned crosswalk.
type Definition struct {
	SchemaVersion     int               `yaml:"schema_version"`
	Version           string            `yaml:"version"`
	Source            string            `yaml:"source"`
	FrameworkVersions FrameworkVersions `yaml:"framework_versions"`
	Controls          []ControlSeed     `yaml:"controls"`
}

// Control is an immutable, registry-resolved crosswalk record.
type Control struct {
	ID           string            `json:"id"`
	Scope        string            `json:"scope"`
	Title        string            `json:"title"`
	Frameworks   FrameworkRefs     `json:"frameworks"`
	OwnerPackage string            `json:"owner_package"`
	TodoIDs      []string          `json:"todo_ids"`
	Evidence     []EvidencePointer `json:"evidence"`
	Ownership    Ownership         `json:"ownership"`
	Status       Status            `json:"status"`
	Digest       string            `json:"digest"`
}

// Revision is the generated crosswalk snapshot. Its Digest binds every
// resolved record, including registry-derived status and evidence pointers.
type Revision struct {
	SchemaVersion     int               `json:"schema_version"`
	Version           string            `json:"version"`
	Source            string            `json:"source"`
	FrameworkVersions FrameworkVersions `json:"framework_versions"`
	Controls          []Control         `json:"controls"`
	Digest            string            `json:"digest"`
}

// Finding is a typed refusal. Field always identifies the rejected input
// field, so callers do not need to parse prose to locate a defect.
type Finding struct {
	ControlID string `json:"control_id,omitempty"`
	Field     string `json:"field"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
}

func (f Finding) Error() string {
	if f.ControlID == "" {
		return fmt.Sprintf("%s: %s: %s", f.Field, f.Code, f.Detail)
	}
	return fmt.Sprintf("%s.%s: %s: %s", f.ControlID, f.Field, f.Code, f.Detail)
}

// ValidationError collects all typed refusals from one compilation attempt.
type ValidationError struct {
	Findings []Finding
}

func (e *ValidationError) Error() string {
	if len(e.Findings) == 0 {
		return "controlcrosswalk: validation failed"
	}
	parts := make([]string, 0, len(e.Findings))
	for _, finding := range e.Findings {
		parts = append(parts, finding.Error())
	}
	return "controlcrosswalk: " + strings.Join(parts, "; ")
}

// Load reads a YAML crosswalk seed.
func Load(path string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, fmt.Errorf("read crosswalk fixture %s: %w", path, err)
	}
	var definition Definition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return Definition{}, fmt.Errorf("parse crosswalk fixture %s: %w", path, err)
	}
	return definition, nil
}

// LoadRegistry reads the generated GOV-002 registry without re-generating it.
func LoadRegistry(path string) ([]todoregistry.Todo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read todo registry %s: %w", path, err)
	}
	var todos []todoregistry.Todo
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil, fmt.Errorf("parse todo registry %s: %w", path, err)
	}
	return todos, nil
}

// Compiler wires the reviewed seed to a detached todo-registry snapshot.
type Compiler struct {
	definition Definition
	todos      []todoregistry.Todo
}

// NewCompiler returns a compiler that does not retain mutable caller slices.
func NewCompiler(definition Definition, todos []todoregistry.Todo) Compiler {
	copyTodos := append([]todoregistry.Todo(nil), todos...)
	copyDefinition := definition
	copyDefinition.Controls = make([]ControlSeed, len(definition.Controls))
	for index, control := range definition.Controls {
		copyDefinition.Controls[index] = control
		copyDefinition.Controls[index].TodoIDs = append([]string(nil), control.TodoIDs...)
		copyDefinition.Controls[index].Evidence = append([]EvidencePointer(nil), control.Evidence...)
	}
	return Compiler{definition: copyDefinition, todos: copyTodos}
}

// Regenerate resolves the seed against the registry and returns a digested
// revision. testNames is optional; when supplied, completed todo evidence
// test names must also exist in the scanned test tree.
func (c Compiler) Regenerate(testNames map[string]bool) (Revision, error) {
	return regenerate(c.definition, c.todos, testNames)
}

// Regenerate is the convenience form for callers that already have registry
// rows in memory.
func Regenerate(definition Definition, todos []todoregistry.Todo, testNames map[string]bool) (Revision, error) {
	return NewCompiler(definition, todos).Regenerate(testNames)
}

func regenerate(definition Definition, todos []todoregistry.Todo, testNames map[string]bool) (Revision, error) {
	findings := validateDefinition(definition)
	byID := make(map[string]todoregistry.Todo, len(todos))
	for _, todo := range todos {
		if todo.ID != "" {
			byID[todo.ID] = todo
		}
	}

	controls := make([]Control, 0, len(definition.Controls))
	for index, seed := range definition.Controls {
		field := func(name string) string { return fmt.Sprintf("controls[%d].%s", index, name) }
		if seed.ID == "" {
			continue
		}
		for _, id := range seed.TodoIDs {
			todo, ok := byID[id]
			if !ok {
				findings = append(findings, Finding{ControlID: seed.ID, Field: field("todo_ids"), Code: "UNRESOLVED_TODO", Detail: id})
				continue
			}
			if testNames != nil && todo.Done {
				names := traceability.ExtractEvidenceTestNames(todo.Evidence)
				for _, name := range names {
					if !testNames[name] {
						findings = append(findings, Finding{ControlID: seed.ID, Field: field("evidence"), Code: "UNRESOLVED_EVIDENCE_TEST", Detail: name})
					}
				}
			}
		}
		for _, pointer := range seed.Evidence {
			todo, ok := byID[pointer.TodoID]
			if !ok {
				findings = append(findings, Finding{ControlID: seed.ID, Field: field("evidence"), Code: "UNRESOLVED_EVIDENCE", Detail: pointer.TodoID})
				continue
			}
			if pointer.TodoID != "" && !contains(seed.TodoIDs, pointer.TodoID) {
				findings = append(findings, Finding{ControlID: seed.ID, Field: field("evidence"), Code: "EVIDENCE_TODO_NOT_CITED", Detail: pointer.TodoID})
			}
			if pointer.TodoID != "" && todo.Test == "" {
				findings = append(findings, Finding{ControlID: seed.ID, Field: field("evidence"), Code: "TODO_MISSING_TEST", Detail: pointer.TodoID})
			}
		}
		status := deriveStatus(seed.TodoIDs, byID)
		if seed.DeclaredStatus != "" && seed.DeclaredStatus != string(status) {
			findings = append(findings, Finding{ControlID: seed.ID, Field: field("status"), Code: "STATUS_NOT_DERIVED", Detail: fmt.Sprintf("declared %q, registry derives %q", seed.DeclaredStatus, status)})
		}
		if len(seed.TodoIDs) == 0 || len(seed.Evidence) == 0 {
			continue
		}
		resolvedEvidence := make([]EvidencePointer, 0, len(seed.Evidence))
		for _, pointer := range seed.Evidence {
			if todo, ok := byID[pointer.TodoID]; ok {
				resolvedEvidence = append(resolvedEvidence, EvidencePointer{TodoID: pointer.TodoID, TestName: todo.Test})
			}
		}
		sort.Slice(resolvedEvidence, func(i, j int) bool { return resolvedEvidence[i].TodoID < resolvedEvidence[j].TodoID })
		control := Control{
			ID: seed.ID, Scope: seed.Scope, Title: seed.Title, Frameworks: seed.Frameworks,
			OwnerPackage: seed.OwnerPackage, TodoIDs: sortedUnique(seed.TodoIDs),
			Evidence: resolvedEvidence, Ownership: seed.Ownership, Status: status,
		}
		control.Digest = digestValue(controlWithoutDigest(control))
		controls = append(controls, control)
	}
	if len(findings) != 0 {
		sortFindings(findings)
		return Revision{}, &ValidationError{Findings: findings}
	}
	sort.Slice(controls, func(i, j int) bool { return controls[i].ID < controls[j].ID })
	revision := Revision{SchemaVersion: schemaVersion, Version: definition.Version, Source: definition.Source, FrameworkVersions: definition.FrameworkVersions, Controls: controls}
	revision.Digest = digestValue(revisionWithoutDigest(revision))
	return revision, nil
}

// Verify recomputes all record and revision digests without exposing record
// contents in its refusal.
func Verify(revision Revision) error {
	for index, control := range revision.Controls {
		want := digestValue(controlWithoutDigest(control))
		if control.Digest != want {
			return fmt.Errorf("controls[%d].digest: DIGEST_MISMATCH", index)
		}
	}
	if revision.Digest != digestValue(revisionWithoutDigest(revision)) {
		return fmt.Errorf("digest: DIGEST_MISMATCH")
	}
	return nil
}

// JSON returns deterministic JSON for audit artifacts.
func (r Revision) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal crosswalk revision: %w", err)
	}
	return append(data, '\n'), nil
}

// Explain returns an audit-safe summary: no control IDs, todo IDs, package
// names, secrets, account numbers, or other raw identifiers are included.
func (r Revision) Explain() string {
	counts := map[Status]int{}
	owned := map[Ownership]int{}
	for _, control := range r.Controls {
		counts[control.Status]++
		owned[control.Ownership]++
	}
	return fmt.Sprintf("security-control-crosswalk version=%s controls=%d implemented=%d partial=%d missing=%d owned=%d inherited=%d digest=%s", r.Version, len(r.Controls), counts[Implemented], counts[Partial], counts[Missing], owned[Owned], owned[Inherited], r.Digest)
}

// Explain describes the package contract without any record data.
func Explain() string {
	return "versioned registry-backed security-control crosswalk with derived maturity and digested revisions"
}

func validateDefinition(definition Definition) []Finding {
	var findings []Finding
	if definition.SchemaVersion != schemaVersion {
		findings = append(findings, Finding{Field: "schema_version", Code: "INVALID_SCHEMA_VERSION", Detail: fmt.Sprintf("got %d, want %d", definition.SchemaVersion, schemaVersion)})
	}
	if strings.TrimSpace(definition.Version) == "" {
		findings = append(findings, Finding{Field: "version", Code: "REQUIRED", Detail: "version is required"})
	}
	if strings.TrimSpace(definition.Source) == "" {
		findings = append(findings, Finding{Field: "source", Code: "REQUIRED", Detail: "source is required"})
	}
	for name, value := range map[string]string{
		"framework_versions.nist_800_53":  definition.FrameworkVersions.NIST80053,
		"framework_versions.csf_2_0":      definition.FrameworkVersions.CSF20,
		"framework_versions.iso_27001":    definition.FrameworkVersions.ISO27001,
		"framework_versions.soc_2_tsc":    definition.FrameworkVersions.SOC2,
		"framework_versions.cis_controls": definition.FrameworkVersions.CIS,
		"framework_versions.asvs":         definition.FrameworkVersions.ASVS,
	} {
		if strings.TrimSpace(value) == "" {
			findings = append(findings, Finding{Field: name, Code: "REQUIRED", Detail: "framework edition is required"})
		}
	}
	seen := make(map[string]bool, len(definition.Controls))
	for index, control := range definition.Controls {
		field := func(name string) string { return fmt.Sprintf("controls[%d].%s", index, name) }
		if control.ID == "" {
			findings = append(findings, Finding{Field: field("id"), Code: "REQUIRED", Detail: "control id is required"})
		} else if seen[control.ID] {
			findings = append(findings, Finding{ControlID: control.ID, Field: field("id"), Code: "DUPLICATE", Detail: control.ID})
		} else {
			seen[control.ID] = true
		}
		for name, value := range map[string]string{
			"scope": control.Scope, "title": control.Title, "owner_package": control.OwnerPackage,
			"frameworks.nist_800_53": control.Frameworks.NIST80053, "frameworks.csf_2_0": control.Frameworks.CSF20,
			"frameworks.iso_27001_annex_a": control.Frameworks.ISO27001, "frameworks.soc_2_tsc": control.Frameworks.SOC2,
			"frameworks.cis_controls": control.Frameworks.CIS, "frameworks.asvs_5_0_0": control.Frameworks.ASVS,
		} {
			if strings.TrimSpace(value) == "" {
				findings = append(findings, Finding{ControlID: control.ID, Field: field(name), Code: "REQUIRED", Detail: "field is required"})
			}
		}
		if control.Ownership != Owned && control.Ownership != Inherited {
			findings = append(findings, Finding{ControlID: control.ID, Field: field("ownership"), Code: "INVALID_OWNERSHIP", Detail: string(control.Ownership)})
		}
		if len(control.TodoIDs) == 0 {
			findings = append(findings, Finding{ControlID: control.ID, Field: field("todo_ids"), Code: "REQUIRED", Detail: "at least one todo is required"})
		}
		if len(control.Evidence) == 0 {
			findings = append(findings, Finding{ControlID: control.ID, Field: field("evidence"), Code: "REQUIRED", Detail: "at least one evidence pointer is required"})
		}
	}
	return findings
}

func deriveStatus(ids []string, byID map[string]todoregistry.Todo) Status {
	done := 0
	for _, id := range ids {
		if todo, ok := byID[id]; ok && todo.Done {
			done++
		}
	}
	if done == len(ids) && len(ids) != 0 {
		return Implemented
	}
	if done != 0 {
		return Partial
	}
	return Missing
}

func controlWithoutDigest(control Control) Control     { control.Digest = ""; return control }
func revisionWithoutDigest(revision Revision) Revision { revision.Digest = ""; return revision }

func digestValue(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if strings.TrimSpace(value) != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ControlID != findings[j].ControlID {
			return findings[i].ControlID < findings[j].ControlID
		}
		if findings[i].Field != findings[j].Field {
			return findings[i].Field < findings[j].Field
		}
		return findings[i].Code < findings[j].Code
	})
}
