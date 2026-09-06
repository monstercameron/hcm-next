package presentation

import (
	"fmt"
	"sort"
	"strings"
)

type ReleaseGate string

const (
	GateA  ReleaseGate = "GATE_A"
	GateB  ReleaseGate = "GATE_B"
	GateC  ReleaseGate = "GATE_C"
	Phase2 ReleaseGate = "PHASE_2"
	Phase3 ReleaseGate = "PHASE_3"
	Phase4 ReleaseGate = "PHASE_4"
)

type ScopeEntry struct {
	Capability string      `json:"capability"`
	Gate       ReleaseGate `json:"gate"`
	Authority  string      `json:"authority"`
	ReadOnly   bool        `json:"read_only"`
}

func ValidateReleaseScope(entries []ScopeEntry) error {
	seen := map[string]bool{}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Capability) == "" || strings.TrimSpace(entry.Authority) == "" || seen[entry.Capability] {
			return fmt.Errorf("%w: release scope needs unique capabilities and named authority", ErrInvalid)
		}
		seen[entry.Capability] = true
		switch entry.Gate {
		case GateA, GateB, GateC, Phase2, Phase3, Phase4:
		default:
			return fmt.Errorf("%w: unknown release gate %q", ErrInvalid, entry.Gate)
		}
	}
	return nil
}

type EvidenceArtifact struct {
	Kind         string   `json:"kind"`
	URI          string   `json:"uri"`
	SHA256       string   `json:"sha256"`
	Requirements []string `json:"requirements"`
}

type EvidenceBundle struct {
	ReleaseID string             `json:"release_id"`
	Artifacts []EvidenceArtifact `json:"artifacts"`
}

var requiredEvidenceKinds = []string{
	"unit", "browser", "accessibility", "security", "localization", "performance", "recovery",
}

func (b EvidenceBundle) Validate() error {
	if strings.TrimSpace(b.ReleaseID) == "" {
		return fmt.Errorf("%w: release id is required", ErrInvalid)
	}
	kinds := map[string]bool{}
	for _, artifact := range b.Artifacts {
		if strings.TrimSpace(artifact.Kind) == "" || strings.TrimSpace(artifact.URI) == "" ||
			!strings.HasPrefix(artifact.SHA256, "sha256:") || len(artifact.SHA256) != 71 ||
			len(artifact.Requirements) == 0 || kinds[artifact.Kind] {
			return fmt.Errorf("%w: each evidence artifact needs a unique kind, URI, digest, and requirements", ErrInvalid)
		}
		if strings.ContainsAny(artifact.URI, "\r\n<>") {
			return fmt.Errorf("%w: evidence URI contains unsafe text", ErrInvalid)
		}
		kinds[artifact.Kind] = true
	}
	for _, kind := range requiredEvidenceKinds {
		if !kinds[kind] {
			return fmt.Errorf("%w: required %s evidence is missing", ErrInvalid, kind)
		}
	}
	return nil
}

type OwnershipBoundary struct {
	Concern       string   `json:"concern"`
	Owner         string   `json:"owner"`
	MayDecide     []string `json:"may_decide"`
	MustNotDecide []string `json:"must_not_decide"`
}

func ValidateOwnership(boundaries []OwnershipBoundary) error {
	seen := map[string]bool{}
	for _, boundary := range boundaries {
		if strings.TrimSpace(boundary.Concern) == "" || strings.TrimSpace(boundary.Owner) == "" || seen[boundary.Concern] || len(boundary.MayDecide) == 0 || len(boundary.MustNotDecide) == 0 {
			return fmt.Errorf("%w: ownership boundaries need one owner plus positive and negative authority", ErrInvalid)
		}
		seen[boundary.Concern] = true
	}
	return nil
}

type Threat struct {
	ID       string   `yaml:"id" json:"id"`
	Boundary string   `yaml:"boundary" json:"boundary"`
	Attack   string   `yaml:"attack" json:"attack"`
	Controls []string `yaml:"controls" json:"controls"`
	Evidence []string `yaml:"evidence" json:"evidence"`
}

type ThreatModel struct {
	Version     string   `yaml:"version" json:"version"`
	Owner       string   `yaml:"owner" json:"owner"`
	TrustBounds []string `yaml:"trust_boundaries" json:"trust_boundaries"`
	Threats     []Threat `yaml:"threats" json:"threats"`
}

func (m ThreatModel) Validate() error {
	if strings.TrimSpace(m.Version) == "" || strings.TrimSpace(m.Owner) == "" || len(m.TrustBounds) < 3 || len(m.Threats) == 0 {
		return fmt.Errorf("%w: threat model needs version, owner, trust boundaries, and threats", ErrInvalid)
	}
	knownBounds := map[string]bool{}
	for _, boundary := range m.TrustBounds {
		knownBounds[boundary] = true
	}
	seen := map[string]bool{}
	for _, threat := range m.Threats {
		if strings.TrimSpace(threat.ID) == "" || seen[threat.ID] || !knownBounds[threat.Boundary] || strings.TrimSpace(threat.Attack) == "" || len(threat.Controls) == 0 || len(threat.Evidence) == 0 {
			return fmt.Errorf("%w: every threat needs a unique id, known boundary, attack, controls, and evidence", ErrInvalid)
		}
		seen[threat.ID] = true
	}
	return nil
}

func CanonicalOwnership(boundaries []OwnershipBoundary) []OwnershipBoundary {
	result := append([]OwnershipBoundary(nil), boundaries...)
	sort.Slice(result, func(i, j int) bool { return result[i].Concern < result[j].Concern })
	return result
}
