// Package threatmodel loads and validates the release-surface threat model.
//
// The model is an offline, versioned planning artifact. Loading computes a
// digest for every surface record and for the complete model; validation then
// checks that the digests still describe the loaded values.
package threatmodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/riskbinding"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"gopkg.in/yaml.v3"
)

const schemaVersion = 1

// ReleaseSurfaces are the production roots that require a threat-model
// record. The values mirror the release-surface disposition in the layout
// contract.
var ReleaseSurfaces = []string{"authn", "data", "transport", "trust"}

// Model is the complete versioned threat-model artifact.
type Model struct {
	SchemaVersion int       `yaml:"schema_version" json:"schema_version"`
	Release       string    `yaml:"release" json:"release"`
	Surfaces      []Surface `yaml:"surfaces" json:"surfaces"`
	Digest        string    `yaml:"-" json:"digest"`
}

// Surface is one immutable release-surface threat-model revision.
type Surface struct {
	Name                       string                `yaml:"name" json:"name"`
	Revision                   int                   `yaml:"revision" json:"revision"`
	Purpose                    string                `yaml:"purpose" json:"purpose"`
	Assets                     []Asset               `yaml:"assets" json:"assets"`
	TrustBoundaries            []TrustBoundary       `yaml:"trust_boundaries" json:"trust_boundaries"`
	Threats                    []Threat              `yaml:"threats" json:"threats"`
	SecurityAcceptanceCriteria []AcceptanceCriterion `yaml:"security_acceptance_criteria" json:"security_acceptance_criteria"`
	AcceptedGaps               []AcceptedGap         `yaml:"accepted_gaps" json:"accepted_gaps"`
	Digest                     string                `yaml:"-" json:"digest"`
}

// Asset identifies a protected value or capability without containing the
// value itself.
type Asset struct {
	Name           string `yaml:"name" json:"name"`
	Classification string `yaml:"classification" json:"classification"`
	Protection     string `yaml:"protection" json:"protection"`
}

// TrustBoundary describes a change in authority or trust across components.
type TrustBoundary struct {
	Name     string   `yaml:"name" json:"name"`
	From     string   `yaml:"from" json:"from"`
	To       string   `yaml:"to" json:"to"`
	Controls []string `yaml:"controls" json:"controls"`
}

// Threat is a STRIDE threat linked to an existing risk-register row.
type Threat struct {
	ID           string       `yaml:"id" json:"id"`
	STRIDE       string       `yaml:"stride" json:"stride"`
	Scenario     string       `yaml:"scenario" json:"scenario"`
	RiskID       string       `yaml:"risk_id" json:"risk_id"`
	Mitigations  []Mitigation `yaml:"mitigations" json:"mitigations"`
	ResidualRisk string       `yaml:"residual_risk" json:"residual_risk"`
}

// Mitigation names the semantic package and the tests that provide evidence.
type Mitigation struct {
	Package string   `yaml:"package" json:"package"`
	Tests   []string `yaml:"tests" json:"tests"`
	Control string   `yaml:"control" json:"control"`
}

// AcceptanceCriterion is a release-gating security requirement.
type AcceptanceCriterion struct {
	ID          string   `yaml:"id" json:"id"`
	Requirement string   `yaml:"requirement" json:"requirement"`
	Tests       []string `yaml:"tests" json:"tests"`
}

// AcceptedGap is allowed only with a reviewed, expiring release exception.
type AcceptedGap struct {
	ID          string    `yaml:"id" json:"id"`
	Description string    `yaml:"description" json:"description"`
	RiskID      string    `yaml:"risk_id" json:"risk_id"`
	Exception   Exception `yaml:"exception" json:"exception"`
}

// Exception is the review record for one accepted residual gap.
type Exception struct {
	Reviewer  string `yaml:"reviewer" json:"reviewer"`
	Reviewed  string `yaml:"reviewed" json:"reviewed"`
	Expires   string `yaml:"expires" json:"expires"`
	Rationale string `yaml:"rationale" json:"rationale"`
}

// ValidationError is a typed refusal whose Field identifies the invalid
// artifact field.
type ValidationError struct {
	Field  string
	Reason string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("threatmodel: field %s: %s", e.Field, e.Reason)
}

// Version returns the threat-model schema revision.
func Version() int { return schemaVersion }

// Explain returns an audit-safe summary that contains no model identifiers or
// artifact contents.
func (m Model) Explain() string {
	threats, gaps := 0, 0
	for _, surface := range m.Surfaces {
		threats += len(surface.Threats)
		gaps += len(surface.AcceptedGaps)
	}
	return fmt.Sprintf("threat model verified (%d release surfaces, %d threats, %d accepted gaps)", len(m.Surfaces), threats, gaps)
}

// Explain is the package-level Explain-shaped helper for callers that retain
// the model as a value.
func Explain(m Model) string { return m.Explain() }

// Load reads the YAML fixture, validates its structure, and computes the
// immutable digests for every surface and for the complete model.
func Load(path string) (Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Model{}, fmt.Errorf("threatmodel: read fixture %s: %w", path, err)
	}
	var model Model
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&model); err != nil {
		return Model{}, fmt.Errorf("threatmodel: parse fixture %s: %w", path, err)
	}
	if err := validateShape(model); err != nil {
		return Model{}, err
	}
	model = withDigests(model)
	return model, nil
}

// ValidateAt checks release-surface coverage and expiry as of now. Dates are
// inclusive through the stated expiry date.
func (m Model) ValidateAt(now time.Time, required []string, risks riskbinding.Table) error {
	if err := validateShape(m); err != nil {
		return err
	}
	if err := m.VerifyDigest(); err != nil {
		return err
	}
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		name = strings.TrimSpace(name)
		if name != "" {
			requiredSet[name] = true
		}
	}
	seen := make(map[string]bool, len(m.Surfaces))
	riskIDs, boundIDs := riskIndexes(risks)
	for i, surface := range m.Surfaces {
		if seen[surface.Name] {
			return fieldError(fmt.Sprintf("surfaces[%d].name", i), "duplicate release surface")
		}
		seen[surface.Name] = true
		if err := validateSurfaceLinks(surface, i, riskIDs, boundIDs, now); err != nil {
			return err
		}
	}
	var missing []string
	for name := range requiredSet {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return fieldError("surfaces", "missing release-surface record for "+strings.Join(missing, ", "))
	}
	return nil
}

// ValidateRepository loads the real layout and risk register, then validates
// the fixture against their release-surface and risk-id registries.
func ValidateRepository(root, fixturePath, layoutPath, riskPath string, now time.Time) (Model, error) {
	model, err := Load(rooted(root, fixturePath))
	if err != nil {
		return Model{}, err
	}
	manifest, err := layout.Load(rooted(root, layoutPath))
	if err != nil {
		return Model{}, err
	}
	risks, err := riskbinding.Load(rooted(root, riskPath))
	if err != nil {
		return Model{}, err
	}
	required := layoutReleaseSurfaces(manifest)
	if len(required) == 0 {
		return Model{}, fieldError("layout.internal_package_roots", "no release surfaces found")
	}
	if err := model.ValidateAt(now, required, risks); err != nil {
		return Model{}, err
	}
	return model, nil
}

// VerifyDigest detects mutation of a loaded model or one of its surface
// revisions.
func (m Model) VerifyDigest() error {
	for i, surface := range m.Surfaces {
		want, err := digestSurface(surface)
		if err != nil {
			return err
		}
		if surface.Digest == "" || surface.Digest != want {
			return fieldError(fmt.Sprintf("surfaces[%d].digest", i), "digest does not match the immutable revision")
		}
	}
	want, err := digestModel(m)
	if err != nil {
		return err
	}
	if m.Digest == "" || m.Digest != want {
		return fieldError("digest", "digest does not match the immutable model revision")
	}
	return nil
}

func validateShape(model Model) error {
	if model.SchemaVersion != schemaVersion {
		return fieldError("schema_version", fmt.Sprintf("unsupported schema version %d", model.SchemaVersion))
	}
	if strings.TrimSpace(model.Release) == "" {
		return fieldError("release", "release revision is required")
	}
	if len(model.Surfaces) == 0 {
		return fieldError("surfaces", "at least one release-surface record is required")
	}
	seen := map[string]bool{}
	for i, surface := range model.Surfaces {
		if strings.TrimSpace(surface.Name) == "" {
			return fieldError(fmt.Sprintf("surfaces[%d].name", i), "surface name is required")
		}
		if seen[surface.Name] {
			return fieldError(fmt.Sprintf("surfaces[%d].name", i), "duplicate release surface")
		}
		seen[surface.Name] = true
		if surface.Revision < 1 {
			return fieldError(fmt.Sprintf("surfaces[%d].revision", i), "revision must be positive")
		}
		if strings.TrimSpace(surface.Purpose) == "" {
			return fieldError(fmt.Sprintf("surfaces[%d].purpose", i), "purpose is required")
		}
		if len(surface.Assets) == 0 {
			return fieldError(fmt.Sprintf("surfaces[%d].assets", i), "at least one asset is required")
		}
		for j, asset := range surface.Assets {
			if strings.TrimSpace(asset.Name) == "" || strings.TrimSpace(asset.Classification) == "" || strings.TrimSpace(asset.Protection) == "" {
				return fieldError(fmt.Sprintf("surfaces[%d].assets[%d]", i, j), "name, classification, and protection are required")
			}
		}
		if len(surface.TrustBoundaries) == 0 {
			return fieldError(fmt.Sprintf("surfaces[%d].trust_boundaries", i), "at least one trust boundary is required")
		}
		for j, boundary := range surface.TrustBoundaries {
			if strings.TrimSpace(boundary.Name) == "" || strings.TrimSpace(boundary.From) == "" || strings.TrimSpace(boundary.To) == "" || len(boundary.Controls) == 0 {
				return fieldError(fmt.Sprintf("surfaces[%d].trust_boundaries[%d]", i, j), "name, from, to, and controls are required")
			}
		}
		if len(surface.Threats) == 0 {
			return fieldError(fmt.Sprintf("surfaces[%d].threats", i), "at least one STRIDE threat is required")
		}
		for j, threat := range surface.Threats {
			if strings.TrimSpace(threat.ID) == "" || strings.TrimSpace(threat.Scenario) == "" || strings.TrimSpace(threat.RiskID) == "" || strings.TrimSpace(threat.ResidualRisk) == "" {
				return fieldError(fmt.Sprintf("surfaces[%d].threats[%d]", i, j), "id, scenario, risk_id, and residual_risk are required")
			}
			if !validStride(threat.STRIDE) {
				return fieldError(fmt.Sprintf("surfaces[%d].threats[%d].stride", i, j), "must be one STRIDE category")
			}
			if len(threat.Mitigations) == 0 {
				return fieldError(fmt.Sprintf("surfaces[%d].threats[%d].mitigations", i, j), "at least one mitigation is required")
			}
			for k, mitigation := range threat.Mitigations {
				if strings.TrimSpace(mitigation.Package) == "" || len(mitigation.Tests) == 0 || strings.TrimSpace(mitigation.Control) == "" {
					return fieldError(fmt.Sprintf("surfaces[%d].threats[%d].mitigations[%d]", i, j, k), "package, tests, and control are required")
				}
				for l, test := range mitigation.Tests {
					if strings.TrimSpace(test) == "" {
						return fieldError(fmt.Sprintf("surfaces[%d].threats[%d].mitigations[%d].tests[%d]", i, j, k, l), "test name is required")
					}
				}
			}
		}
		if len(surface.SecurityAcceptanceCriteria) == 0 {
			return fieldError(fmt.Sprintf("surfaces[%d].security_acceptance_criteria", i), "at least one security-acceptance criterion is required")
		}
		for j, criterion := range surface.SecurityAcceptanceCriteria {
			if strings.TrimSpace(criterion.ID) == "" || strings.TrimSpace(criterion.Requirement) == "" || len(criterion.Tests) == 0 {
				return fieldError(fmt.Sprintf("surfaces[%d].security_acceptance_criteria[%d]", i, j), "id, requirement, and tests are required")
			}
		}
		for j, gap := range surface.AcceptedGaps {
			prefix := fmt.Sprintf("surfaces[%d].accepted_gaps[%d]", i, j)
			if strings.TrimSpace(gap.ID) == "" || strings.TrimSpace(gap.Description) == "" || strings.TrimSpace(gap.RiskID) == "" {
				return fieldError(prefix, "id, description, and risk_id are required")
			}
			if strings.TrimSpace(gap.Exception.Reviewer) == "" || strings.TrimSpace(gap.Exception.Reviewed) == "" || strings.TrimSpace(gap.Exception.Expires) == "" || strings.TrimSpace(gap.Exception.Rationale) == "" {
				return fieldError(prefix+".exception", "reviewer, reviewed, expires, and rationale are required")
			}
			if !validDate(gap.Exception.Reviewed) || !validDate(gap.Exception.Expires) {
				return fieldError(prefix+".exception", "reviewed and expires must be YYYY-MM-DD")
			}
		}
	}
	return nil
}

func validateSurfaceLinks(surface Surface, index int, riskIDs, boundIDs map[string]bool, now time.Time) error {
	for j, threat := range surface.Threats {
		field := fmt.Sprintf("surfaces[%d].threats[%d].risk_id", index, j)
		if !riskIDs[threat.RiskID] {
			return fieldError(field, "risk id is absent from riskbinding")
		}
		if !boundIDs[threat.RiskID] {
			return fieldError(field, "risk id has no binding row in riskbinding")
		}
	}
	for j, gap := range surface.AcceptedGaps {
		field := fmt.Sprintf("surfaces[%d].accepted_gaps[%d].risk_id", index, j)
		if !riskIDs[gap.RiskID] || !boundIDs[gap.RiskID] {
			return fieldError(field, "risk id is absent or unbound in riskbinding")
		}
		if strings.TrimSpace(gap.Exception.Reviewed) > now.UTC().Format("2006-01-02") {
			return fieldError(field+".exception.reviewed", "review date is in the future")
		}
		if now.UTC().Format("2006-01-02") > gap.Exception.Expires {
			return fieldError(field+".exception.expires", "release exception is expired")
		}
	}
	return nil
}

func riskIndexes(table riskbinding.Table) (map[string]bool, map[string]bool) {
	risks := make(map[string]bool, len(table.Risks))
	for _, risk := range table.Risks {
		risks[risk.ID] = true
	}
	bound := make(map[string]bool, len(table.Bindings))
	for _, binding := range table.Bindings {
		bound[binding.RiskID] = true
	}
	return risks, bound
}

func layoutReleaseSurfaces(manifest *layout.Manifest) []string {
	allowed := make(map[string]bool, len(ReleaseSurfaces))
	for _, surface := range ReleaseSurfaces {
		allowed[surface] = true
	}
	var surfaces []string
	for _, root := range manifest.InternalPackageRoots {
		if allowed[root.Name] {
			surfaces = append(surfaces, root.Name)
		}
	}
	sort.Strings(surfaces)
	return surfaces
}

func withDigests(model Model) Model {
	copyModel := model
	copyModel.Surfaces = append([]Surface(nil), model.Surfaces...)
	for i := range copyModel.Surfaces {
		copyModel.Surfaces[i].Digest, _ = digestSurface(copyModel.Surfaces[i])
	}
	copyModel.Digest, _ = digestModel(copyModel)
	return copyModel
}

func digestSurface(surface Surface) (string, error) {
	surface.Digest = ""
	b, err := json.Marshal(surfacePayload{Surface: surface})
	if err != nil {
		return "", fmt.Errorf("threatmodel: marshal surface revision: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func digestModel(model Model) (string, error) {
	payload := modelPayload{SchemaVersion: model.SchemaVersion, Release: model.Release}
	for _, surface := range model.Surfaces {
		surface.Digest = ""
		payload.Surfaces = append(payload.Surfaces, surfacePayload{Surface: surface})
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("threatmodel: marshal model revision: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type surfacePayload struct {
	Surface
	Digest string `json:"-" yaml:"-"`
}

type modelPayload struct {
	SchemaVersion int              `json:"schema_version"`
	Release       string           `json:"release"`
	Surfaces      []surfacePayload `json:"surfaces"`
}

func validStride(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SPOOFING", "TAMPERING", "REPUDIATION", "INFORMATION_DISCLOSURE", "DENIAL_OF_SERVICE", "ELEVATION_OF_PRIVILEGE":
		return true
	default:
		return false
	}
}

func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func fieldError(field, reason string) error { return ValidationError{Field: field, Reason: reason} }

func rooted(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
