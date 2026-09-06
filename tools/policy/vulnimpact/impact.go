package vulnimpact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/monstercameron/hcm-next/tools/policy/sbom"
)

// Severity is the typed severity carried by a vulnerability finding.
type Severity string

const (
	SeverityUnknown  Severity = "UNKNOWN"
	SeverityLow      Severity = "LOW"
	SeverityModerate Severity = "MODERATE"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// Vulnerability is the in-memory vulnerability record consumed by the
// resolver. ID is optional for ad-hoc queries but is retained when supplied
// so disclosure and remediation records can share the same identity.
type Vulnerability struct {
	ID                   string   `json:"id,omitempty"`
	Module               string   `json:"module"`
	AffectedVersionRange string   `json:"affected_version_range"`
	Severity             Severity `json:"severity"`
}

// Artifact binds a stable artifact identity to one CycloneDX SBOM. ID may be
// empty when the SBOM root component name is the artifact identity.
type Artifact struct {
	ID   string        `json:"id,omitempty"`
	SBOM sbom.Document `json:"sbom"`
}

// DeploymentMap is the explicit in-memory deployment port: artifact identity
// to the tenants currently receiving that artifact.
type DeploymentMap map[string][]string

// Finding is one affected artifact/component result. Digest is the canonical
// identity of this finding; SBOMDigest is the canonical digest of the SBOM
// document that supplied its evidence.
type Finding struct {
	VulnerabilityID string   `json:"vulnerability_id,omitempty"`
	ArtifactID      string   `json:"artifact_id"`
	ArtifactVersion string   `json:"artifact_version"`
	Module          string   `json:"module"`
	Version         string   `json:"version"`
	Severity        Severity `json:"severity"`
	SBOMDigest      string   `json:"sbom_digest"`
	Digest          string   `json:"digest"`
}

// CanonicalDigest returns the finding's content identity.
func (f Finding) CanonicalDigest() string { return f.Digest }

// Explain renders a concise audit-safe finding description.
func (f Finding) Explain() string {
	return fmt.Sprintf("vulnerability %s affects %s@%s in artifact %s@%s with severity %s (sbom %s, finding %s)", f.VulnerabilityID, f.Module, f.Version, f.ArtifactID, f.ArtifactVersion, f.Severity, f.SBOMDigest, f.Digest)
}

// TenantImpact groups affected tenants by artifact, preserving the explicit
// deployment boundary in the report.
type TenantImpact struct {
	ArtifactID string   `json:"artifact_id"`
	TenantIDs  []string `json:"tenant_ids"`
}

// Report is the complete bounded impact result for one vulnerability.
type Report struct {
	Vulnerability        Vulnerability  `json:"vulnerability"`
	Findings             []Finding      `json:"findings"`
	AffectedArtifacts    []string       `json:"affected_artifacts"`
	AffectedTenants      []string       `json:"affected_tenants"`
	TenantImpacts        []TenantImpact `json:"tenant_impacts"`
	Confidence           string         `json:"confidence"`
	Watermark            string         `json:"watermark"`
	RemediationOwner     string         `json:"remediation_owner"`
	CompensatingControl  string         `json:"compensating_control"`
	CustomerNotification bool           `json:"customer_notification"`
	Digest               string         `json:"digest"`
}

// Explain renders a deterministic report summary. Programmatic consumers
// should branch on the typed fields rather than parse this string.
func (r Report) Explain() string {
	return fmt.Sprintf("vulnerability impact %s for %s (%s): %d finding(s), %d artifact(s), %d tenant(s), confidence %s, watermark %s, remediation owner %s, customer notification %t, digest %s", r.Vulnerability.ID, r.Vulnerability.Module, r.Vulnerability.Severity, len(r.Findings), len(r.AffectedArtifacts), len(r.AffectedTenants), r.Confidence, r.Watermark, r.RemediationOwner, r.CustomerNotification, r.Digest)
}

// Resolve checks one SBOM and returns the affected components it proves for
// vulnerability. The returned slice is sorted and empty when the module is
// absent or all matching versions are outside the supplied range.
func Resolve(document sbom.Document, vulnerability Vulnerability) ([]Finding, error) {
	return ResolveArtifacts([]Artifact{{SBOM: document}}, vulnerability)
}

// ResolveArtifacts checks an explicit artifact inventory. It rejects
// malformed vulnerability records and duplicate artifact identities so an
// incomplete or ambiguous inventory cannot be reported as unaffected.
func ResolveArtifacts(artifacts []Artifact, vulnerability Vulnerability) ([]Finding, error) {
	rangeSet, err := parseRange(vulnerability.AffectedVersionRange)
	if err != nil {
		return nil, fmt.Errorf("vulnimpact: invalid affected version range %q: %w", vulnerability.AffectedVersionRange, err)
	}
	if vulnerability.Module == "" {
		return nil, fmt.Errorf("vulnimpact: vulnerability module is required")
	}
	if !validSeverity(vulnerability.Severity) {
		return nil, fmt.Errorf("vulnimpact: invalid severity %q", vulnerability.Severity)
	}

	seen := make(map[string]bool, len(artifacts))
	var findings []Finding
	for _, artifact := range artifacts {
		artifactID := artifact.ID
		if artifactID == "" {
			artifactID = artifact.SBOM.Metadata.Component.Name
		}
		if artifactID == "" {
			return nil, fmt.Errorf("vulnimpact: artifact has no id and SBOM root has no name")
		}
		if seen[artifactID] {
			return nil, fmt.Errorf("vulnimpact: duplicate artifact %q", artifactID)
		}
		seen[artifactID] = true
		artifactVersion := artifact.SBOM.Metadata.Component.Version
		if artifactVersion == "" {
			return nil, fmt.Errorf("vulnimpact: artifact %q has no SBOM root version", artifactID)
		}
		sbomDigest, err := CanonicalSBOMDigest(artifact.SBOM)
		if err != nil {
			return nil, err
		}

		components := make([]sbom.Component, 0, len(artifact.SBOM.Components)+1)
		components = append(components, artifact.SBOM.Metadata.Component)
		components = append(components, artifact.SBOM.Components...)
		for _, component := range components {
			if component.Name != vulnerability.Module || !rangeSet.matches(component.Version) {
				continue
			}
			finding := Finding{
				VulnerabilityID: vulnerability.ID,
				ArtifactID:      artifactID,
				ArtifactVersion: artifactVersion,
				Module:          component.Name,
				Version:         component.Version,
				Severity:        vulnerability.Severity,
				SBOMDigest:      sbomDigest,
			}
			finding.Digest = findingDigest(finding)
			findings = append(findings, finding)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ArtifactID != findings[j].ArtifactID {
			return findings[i].ArtifactID < findings[j].ArtifactID
		}
		if findings[i].Module != findings[j].Module {
			return findings[i].Module < findings[j].Module
		}
		return findings[i].Version < findings[j].Version
	})
	return findings, nil
}

// Analyze resolves vulnerability through artifacts and then joins only the
// affected artifact identities to the explicit tenant deployment map.
func Analyze(artifacts []Artifact, vulnerability Vulnerability, deployments DeploymentMap) (Report, error) {
	findings, err := ResolveArtifacts(artifacts, vulnerability)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Vulnerability:       vulnerability,
		Findings:            findings,
		Confidence:          "COMPLETE",
		RemediationOwner:    "dependency-security",
		CompensatingControl: "block affected artifacts from release admission",
	}

	artifactSet := make(map[string]bool)
	for _, finding := range findings {
		artifactSet[finding.ArtifactID] = true
	}
	for artifactID := range artifactSet {
		report.AffectedArtifacts = append(report.AffectedArtifacts, artifactID)
	}
	sort.Strings(report.AffectedArtifacts)

	tenantSet := make(map[string]bool)
	for _, artifactID := range report.AffectedArtifacts {
		tenants := uniqueSorted(deployments[artifactID])
		if len(tenants) == 0 {
			continue
		}
		report.TenantImpacts = append(report.TenantImpacts, TenantImpact{ArtifactID: artifactID, TenantIDs: tenants})
		for _, tenant := range tenants {
			tenantSet[tenant] = true
		}
	}
	sort.Slice(report.TenantImpacts, func(i, j int) bool { return report.TenantImpacts[i].ArtifactID < report.TenantImpacts[j].ArtifactID })
	for tenant := range tenantSet {
		report.AffectedTenants = append(report.AffectedTenants, tenant)
	}
	sort.Strings(report.AffectedTenants)
	report.CustomerNotification = len(report.AffectedTenants) > 0
	report.Watermark = watermark(findings)
	report.Digest = reportDigest(report)
	return report, nil
}

// CanonicalSBOMDigest hashes the stable JSON representation of the modeled
// CycloneDX document. It is independent of source-file whitespace.
func CanonicalSBOMDigest(document sbom.Document) (string, error) {
	b, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("vulnimpact: canonicalize SBOM: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func validSeverity(severity Severity) bool {
	switch severity {
	case SeverityLow, SeverityModerate, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

func findingDigest(f Finding) string {
	view := struct {
		VulnerabilityID string   `json:"vulnerability_id,omitempty"`
		ArtifactID      string   `json:"artifact_id"`
		ArtifactVersion string   `json:"artifact_version"`
		Module          string   `json:"module"`
		Version         string   `json:"version"`
		Severity        Severity `json:"severity"`
		SBOMDigest      string   `json:"sbom_digest"`
	}{f.VulnerabilityID, f.ArtifactID, f.ArtifactVersion, f.Module, f.Version, f.Severity, f.SBOMDigest}
	b, _ := json.Marshal(view)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func reportDigest(r Report) string {
	view := struct {
		Vulnerability     Vulnerability  `json:"vulnerability"`
		Findings          []Finding      `json:"findings"`
		AffectedArtifacts []string       `json:"affected_artifacts"`
		AffectedTenants   []string       `json:"affected_tenants"`
		TenantImpacts     []TenantImpact `json:"tenant_impacts"`
		Confidence        string         `json:"confidence"`
		Watermark         string         `json:"watermark"`
		RemediationOwner  string         `json:"remediation_owner"`
		Compensating      string         `json:"compensating_control"`
		Notify            bool           `json:"customer_notification"`
	}{r.Vulnerability, r.Findings, r.AffectedArtifacts, r.AffectedTenants, r.TenantImpacts, r.Confidence, r.Watermark, r.RemediationOwner, r.CompensatingControl, r.CustomerNotification}
	b, _ := json.Marshal(view)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func watermark(findings []Finding) string {
	digests := make([]string, 0, len(findings))
	seen := map[string]bool{}
	for _, finding := range findings {
		if !seen[finding.SBOMDigest] {
			seen[finding.SBOMDigest] = true
			digests = append(digests, finding.SBOMDigest)
		}
	}
	sort.Strings(digests)
	return strings.Join(digests, ",")
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

type versionRange []versionAlternative

type versionAlternative []versionConstraint

type versionConstraint struct {
	operator string
	version  string
}

func (r versionRange) matches(version string) bool {
	version = normalizeVersion(version)
	if !semver.IsValid(version) {
		return false
	}
	for _, alternative := range r {
		matched := true
		for _, constraint := range alternative {
			comparison := semver.Compare(version, constraint.version)
			switch constraint.operator {
			case "=":
				matched = matched && comparison == 0
			case ">":
				matched = matched && comparison > 0
			case ">=":
				matched = matched && comparison >= 0
			case "<":
				matched = matched && comparison < 0
			case "<=":
				matched = matched && comparison <= 0
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func parseRange(raw string) (versionRange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("range is empty")
	}
	var result versionRange
	for _, alternativeRaw := range strings.Split(raw, "||") {
		alternativeRaw = strings.TrimSpace(alternativeRaw)
		if alternativeRaw == "" {
			return nil, fmt.Errorf("range contains an empty alternative")
		}
		constraints, err := parseAlternative(alternativeRaw)
		if err != nil {
			return nil, err
		}
		result = append(result, constraints)
	}
	return result, nil
}

func parseAlternative(raw string) (versionAlternative, error) {
	fields := strings.Fields(strings.ReplaceAll(raw, ",", " "))
	if len(fields) == 3 && fields[1] == "-" {
		lower, err := normalizeConstraintVersion(fields[0])
		if err != nil {
			return nil, err
		}
		upper, err := normalizeConstraintVersion(fields[2])
		if err != nil {
			return nil, err
		}
		return versionAlternative{{operator: ">=", version: lower}, {operator: "<=", version: upper}}, nil
	}
	var result versionAlternative
	for _, field := range fields {
		constraints, err := parseConstraint(field)
		if err != nil {
			return nil, err
		}
		result = append(result, constraints...)
	}
	return result, nil
}

func parseConstraint(raw string) (versionAlternative, error) {
	if raw == "*" || strings.EqualFold(raw, "x") {
		return nil, nil
	}
	operator := "="
	versionText := raw
	for _, candidate := range []string{">=", "<=", ">", "<", "=", "~", "^"} {
		if strings.HasPrefix(raw, candidate) {
			operator = candidate
			versionText = strings.TrimSpace(strings.TrimPrefix(raw, candidate))
			break
		}
	}
	if versionText == "" {
		return nil, fmt.Errorf("constraint %q has no version", raw)
	}
	if strings.ContainsAny(versionText, "xX*") || versionPartCount(versionText) < 3 {
		if operator != "=" {
			return nil, fmt.Errorf("wildcard or abbreviated version %q cannot use operator %q", versionText, operator)
		}
		return wildcardConstraints(versionText)
	}
	version, err := normalizeConstraintVersion(versionText)
	if err != nil {
		return nil, err
	}
	switch operator {
	case "~":
		return []versionConstraint{{">=", version}, {"<", nextMinor(version)}}, nil
	case "^":
		return []versionConstraint{{">=", version}, {"<", caretUpper(version)}}, nil
	default:
		return []versionConstraint{{operator, version}}, nil
	}
}

func wildcardConstraints(raw string) (versionAlternative, error) {
	text := strings.TrimPrefix(strings.TrimPrefix(raw, "v"), "V")
	parts := strings.Split(text, ".")
	if len(parts) > 3 {
		return nil, fmt.Errorf("invalid abbreviated version %q", raw)
	}
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid abbreviated version %q", raw)
		}
		if strings.EqualFold(part, "x") || part == "*" {
			if i == 0 {
				return nil, fmt.Errorf("major wildcard %q is not bounded", raw)
			}
			break
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "x")
	}
	wildcard := 2
	for i, part := range parts {
		if strings.EqualFold(part, "x") || part == "*" {
			wildcard = i
			break
		}
	}
	lowerParts := []string{"0", "0", "0"}
	for i := 0; i < wildcard; i++ {
		lowerParts[i] = parts[i]
	}
	lower, err := normalizeConstraintVersion(strings.Join(lowerParts, "."))
	if err != nil {
		return nil, err
	}
	upperParts := make([]int, 3)
	for i := 0; i < wildcard; i++ {
		var value int
		if _, err := fmt.Sscanf(parts[i], "%d", &value); err != nil {
			return nil, fmt.Errorf("invalid abbreviated version %q", raw)
		}
		upperParts[i] = value
	}
	// A wildcard denotes the next value of the preceding specified
	// component: 1.x means [1.0.0,2.0.0), while 1.2.x means
	// [1.2.0,1.3.0).
	if wildcard == 0 {
		return nil, fmt.Errorf("major wildcard %q is not bounded", raw)
	}
	upperParts[wildcard-1]++
	for i := wildcard; i < len(upperParts); i++ {
		upperParts[i] = 0
	}
	upperText := make([]string, len(upperParts))
	for i, value := range upperParts {
		upperText[i] = fmt.Sprintf("%d", value)
	}
	upper, err := normalizeConstraintVersion(strings.Join(upperText, "."))
	if err != nil {
		return nil, err
	}
	return versionAlternative{{">=", lower}, {"<", upper}}, nil
}

func normalizeConstraintVersion(raw string) (string, error) {
	version := normalizeVersion(raw)
	if !semver.IsValid(version) {
		return "", fmt.Errorf("invalid semantic version %q", raw)
	}
	return version, nil
}

func normalizeVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] == 'v' || raw[0] == 'V' {
		if len(raw) > 0 && raw[0] == 'V' {
			return "v" + raw[1:]
		}
		return raw
	}
	return "v" + raw
}

func versionPartCount(raw string) int {
	text := strings.TrimPrefix(strings.TrimPrefix(raw, "v"), "V")
	return len(strings.SplitN(text, "+", 2)[0]) - len(strings.ReplaceAll(strings.SplitN(text, "+", 2)[0], ".", "")) + 1
}

func nextMinor(version string) string {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	minor := 0
	if len(parts) > 1 {
		_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	}
	major := 0
	_, _ = fmt.Sscanf(parts[0], "%d", &major)
	return fmt.Sprintf("v%d.%d.0", major, minor+1)
}

func caretUpper(version string) string {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	major, minor, patch := 0, 0, 0
	_, _ = fmt.Sscanf(parts[0], "%d", &major)
	if len(parts) > 1 {
		_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	}
	if len(parts) > 2 {
		_, _ = fmt.Sscanf(parts[2], "%d", &patch)
	}
	if major > 0 {
		return fmt.Sprintf("v%d.0.0", major+1)
	}
	if minor > 0 {
		return fmt.Sprintf("v0.%d.0", minor+1)
	}
	return fmt.Sprintf("v0.0.%d", patch+1)
}
