// Package iac owns the typed, provider-neutral record used to qualify an
// infrastructure toolchain and state authority. It captures a human decision
// without making that decision: Placeholder returns clearly labelled fixture
// values, while Validate proves the mechanics required before qualification.
package iac

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Version reports the IAC-013 qualification record version.
func Version() int { return 1 }

// Explain describes the toolchain qualification contract.
func Explain() string {
	return "IAC-013 v1: pinned IaC engine/providers/modules/images, fenced state, separated roles, and tested recovery"
}

// Artifact is one versioned non-module input to planning or apply.
type Artifact struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Digest           string `json:"digest"`
	Source           string `json:"source"`
	License          string `json:"license"`
	VulnerabilityRef string `json:"vulnerability_ref"`
	Owner            string `json:"owner"`
	UpdateSLA        string `json:"update_sla"`
	ReplacementPath  string `json:"replacement_path"`
}

// StateAuthority describes the isolated, encrypted, lockable state backend.
type StateAuthority struct {
	Backend       string `json:"backend"`
	KeyReference  string `json:"key_reference"`
	Encrypted     bool   `json:"encrypted"`
	Isolated      bool   `json:"isolated"`
	Locking       bool   `json:"locking"`
	Backup        bool   `json:"backup"`
	RestoreTested bool   `json:"restore_tested"`
}

// RecoveryProfile records the state-loss and lifecycle rehearsal.
type RecoveryProfile struct {
	PlanApplyDestroyRestoreTested bool   `json:"plan_apply_destroy_restore_tested"`
	StateLossRunbook              string `json:"state_loss_runbook"`
	RestoreEvidence               string `json:"restore_evidence"`
}

// Selection is the decision record. The concrete provider/vendor fields are
// intentionally data, not imports, so a human may select them later.
type Selection struct {
	Engine            Artifact        `json:"engine"`
	Providers         []Artifact      `json:"providers"`
	Modules           []Artifact      `json:"modules"`
	Images            []Artifact      `json:"images"`
	State             StateAuthority  `json:"state"`
	PlanRole          string          `json:"plan_role"`
	ApplyRole         string          `json:"apply_role"`
	ReviewerRole      string          `json:"reviewer_role"`
	SignedManifest    bool            `json:"signed_manifest"`
	PlanVerified      bool            `json:"plan_verified"`
	SecretOutputFree  bool            `json:"secret_output_free"`
	ReproducibleApply bool            `json:"reproducible_apply"`
	DriftExplicit     bool            `json:"drift_explicit"`
	Recovery          RecoveryProfile `json:"recovery"`
}

// Violation is one qualification gap.
type Violation struct {
	Field  string
	Code   string
	Detail string
}

// Report is an admission-ready qualification result.
type Report struct {
	SelectionDigest string
	Violations      []Violation
}

// OK reports whether a selection has met every mechanical qualification.
func (r Report) OK() bool { return len(r.Violations) == 0 }

// Validate returns all mechanical gaps in deterministic order. It does not
// assert that any provider or vendor is the human's final choice.
func Validate(s Selection) []Violation {
	var out []Violation
	checkArtifact := func(field string, a Artifact) {
		if strings.TrimSpace(a.Name) == "" {
			out = append(out, Violation{Field: field + ".name", Code: "MISSING_ARTIFACT", Detail: "artifact name is required"})
		}
		if strings.TrimSpace(a.Version) == "" || !validDigest(a.Digest) {
			out = append(out, Violation{Field: field + ".digest", Code: "UNPINNED_ARTIFACT", Detail: "artifact requires an explicit version and 64-hex sha256 digest"})
		}
		for name, value := range map[string]string{"source": a.Source, "license": a.License, "vulnerability_ref": a.VulnerabilityRef, "owner": a.Owner, "update_sla": a.UpdateSLA, "replacement_path": a.ReplacementPath} {
			if strings.TrimSpace(value) == "" {
				out = append(out, Violation{Field: field + "." + name, Code: "INCOMPLETE_INVENTORY", Detail: "non-module input requires source, license, vulnerability, owner, update SLA, and replacement path"})
			}
		}
	}
	checkArtifact("engine", s.Engine)
	if len(s.Providers) == 0 {
		out = append(out, Violation{Field: "providers", Code: "MISSING_PROVIDERS", Detail: "at least one pinned provider is required"})
	}
	if len(s.Modules) == 0 {
		out = append(out, Violation{Field: "modules", Code: "MISSING_MODULES", Detail: "at least one pinned module is required"})
	}
	if len(s.Images) == 0 {
		out = append(out, Violation{Field: "images", Code: "MISSING_IMAGES", Detail: "workload images must be included in the signed manifest"})
	}
	for i, a := range s.Providers {
		checkArtifact(fmt.Sprintf("providers[%d]", i), a)
	}
	for i, a := range s.Modules {
		checkArtifact(fmt.Sprintf("modules[%d]", i), a)
	}
	for i, a := range s.Images {
		checkArtifact(fmt.Sprintf("images[%d]", i), a)
	}
	if strings.TrimSpace(s.State.Backend) == "" || strings.TrimSpace(s.State.KeyReference) == "" {
		out = append(out, Violation{Field: "state", Code: "INCOMPLETE_STATE", Detail: "state backend and encryption key reference are required"})
	}
	if !s.State.Encrypted {
		out = append(out, Violation{Field: "state.encrypted", Code: "UNENCRYPTED_STATE", Detail: "state must be encrypted"})
	}
	if !s.State.Isolated {
		out = append(out, Violation{Field: "state.isolated", Code: "SHARED_STATE", Detail: "state must be isolated per environment/cell"})
	}
	if !s.State.Locking {
		out = append(out, Violation{Field: "state.locking", Code: "UNLOCKED_STATE", Detail: "state backend must provide an authoritative lock"})
	}
	if !s.State.Backup {
		out = append(out, Violation{Field: "state.backup", Code: "MISSING_STATE_BACKUP", Detail: "state backup is required"})
	}
	if !s.State.RestoreTested {
		out = append(out, Violation{Field: "state.restore_tested", Code: "UNTESTED_STATE_RESTORE", Detail: "state restore must be rehearsed"})
	}
	if strings.TrimSpace(s.PlanRole) == "" || strings.TrimSpace(s.ApplyRole) == "" || strings.TrimSpace(s.ReviewerRole) == "" {
		out = append(out, Violation{Field: "roles", Code: "INCOMPLETE_ROLES", Detail: "plan, apply, and reviewer identities are required"})
	} else if s.PlanRole == s.ApplyRole || s.ApplyRole == s.ReviewerRole || s.PlanRole == s.ReviewerRole {
		out = append(out, Violation{Field: "roles", Code: "ROLE_COLLISION", Detail: "plan, apply, and reviewer identities must be distinct"})
	}
	for field, value := range map[string]bool{"signed_manifest": s.SignedManifest, "plan_verified": s.PlanVerified, "secret_output_free": s.SecretOutputFree, "reproducible_apply": s.ReproducibleApply, "drift_explicit": s.DriftExplicit, "recovery.plan_apply_destroy_restore_tested": s.Recovery.PlanApplyDestroyRestoreTested} {
		if !value {
			out = append(out, Violation{Field: field, Code: "MISSING_EVIDENCE", Detail: "qualification evidence is required"})
		}
	}
	if strings.TrimSpace(s.Recovery.StateLossRunbook) == "" || strings.TrimSpace(s.Recovery.RestoreEvidence) == "" {
		out = append(out, Violation{Field: "recovery", Code: "INCOMPLETE_RECOVERY", Detail: "state-loss runbook and restore evidence are required"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// Qualify evaluates a selection and binds its digest to the report.
func Qualify(s Selection) Report {
	digest, _ := Digest(s)
	return Report{SelectionDigest: digest, Violations: Validate(s)}
}

// Check returns a stable error for the first qualification gap.
func Check(s Selection) error {
	report := Qualify(s)
	if !report.OK() {
		v := report.Violations[0]
		return fmt.Errorf("iac toolchain: %s %s: %s", v.Code, v.Field, v.Detail)
	}
	return nil
}

// CanonicalJSON renders a deterministic selection record. It is allowed to
// render an incomplete placeholder so a reviewer can inspect what remains.
func CanonicalJSON(s Selection) ([]byte, error) {
	copySelection := s
	copySelection.Providers = append([]Artifact(nil), s.Providers...)
	copySelection.Modules = append([]Artifact(nil), s.Modules...)
	copySelection.Images = append([]Artifact(nil), s.Images...)
	byName := func(a, b Artifact) bool { return a.Name < b.Name }
	sort.SliceStable(copySelection.Providers, func(i, j int) bool { return byName(copySelection.Providers[i], copySelection.Providers[j]) })
	sort.SliceStable(copySelection.Modules, func(i, j int) bool { return byName(copySelection.Modules[i], copySelection.Modules[j]) })
	sort.SliceStable(copySelection.Images, func(i, j int) bool { return byName(copySelection.Images[i], copySelection.Images[j]) })
	data, err := json.MarshalIndent(copySelection, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("iac toolchain: encode selection: %w", err)
	}
	return append(data, '\n'), nil
}

// Digest returns the selection record's content identity.
func Digest(s Selection) (string, error) {
	data, err := CanonicalJSON(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Placeholder returns a clearly labelled, non-qualified record. It is a
// mechanics fixture, not a provider, customer, vendor, or price decision.
func Placeholder() Selection {
	return Selection{
		Engine:   Artifact{Name: "PLACEHOLDER_ENGINE", Version: "PLACEHOLDER_VERSION", Digest: "sha256:" + strings.Repeat("0", 64), Source: "PLACEHOLDER_SOURCE", License: "PLACEHOLDER_LICENSE", VulnerabilityRef: "PLACEHOLDER_VULNERABILITY_REVIEW", Owner: "PLACEHOLDER_OWNER", UpdateSLA: "PLACEHOLDER_UPDATE_SLA", ReplacementPath: "PLACEHOLDER_REPLACEMENT"},
		State:    StateAuthority{Backend: "PLACEHOLDER_STATE_BACKEND", KeyReference: "PLACEHOLDER_KEY_REFERENCE"},
		PlanRole: "PLACEHOLDER_PLAN_ROLE", ApplyRole: "PLACEHOLDER_APPLY_ROLE", ReviewerRole: "PLACEHOLDER_REVIEWER_ROLE",
	}
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
