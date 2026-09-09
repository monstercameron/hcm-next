package releaseadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

// Version is the release-admission policy contract revision.
const Version = 1

// Status is the outcome recorded by an admission decision.
type Status string

const (
	StatusAdmit  Status = "ADMIT"
	StatusReject Status = "REJECT"
)

// Policy contains the trust material and release constraints used by
// Evaluate. TrustedPublicKeys and AllowedBuilders are maps so a caller cannot
// accidentally depend on slice ordering. PinnedPublicKeys and
// BuilderAllowlist are accepted as a convenient declarative form and are
// folded into the same exact-match sets.
type Policy struct {
	TrustedPublicKeys map[string]bool
	PinnedPublicKeys  []string
	AllowedBuilders   map[string]bool
	BuilderAllowlist  []string
	SBOMDigest        string
	// RequireScannerEvidence enables the release-surface scanner gate. It is
	// opt-in for legacy callers that predate SECARCH-007.
	RequireScannerEvidence bool
	ScannerEvidence        []ScannerEvidence
}

// Decision is the immutable admission record. It contains no private key,
// artifact bytes, or protected payload. Reasons are sorted so identical
// inputs produce identical evidence.
type Decision struct {
	Status                Status   `json:"status"`
	Admitted              bool     `json:"admitted"`
	PolicyVersion         int      `json:"policy_version"`
	BuilderID             string   `json:"builder_id"`
	SignerPublicKey       string   `json:"signer_public_key"`
	SBOMDigest            string   `json:"sbom_digest"`
	StatementDigest       string   `json:"statement_digest"`
	ScannerEvidenceDigest string   `json:"scanner_evidence_digest,omitempty"`
	Reasons               []string `json:"reasons,omitempty"`
	Digest                string   `json:"digest"`
}

// Evaluate verifies a provenance statement against p and records every
// policy failure. An admission is possible only with a configured pinned key
// set, builder allowlist, SBOM digest, structurally complete statement,
// trusted valid signature, matching SBOM digest, and allow-listed builder.
func Evaluate(p Policy, statement provenance.Statement) Decision {
	decision := Decision{
		Status:        StatusReject,
		PolicyVersion: Version,
		BuilderID:     statement.Builder.ID,
		SBOMDigest:    statement.SBOM.SHA256,
	}
	if statement.Signature != nil {
		decision.SignerPublicKey = statement.Signature.PublicKey
	}
	if digest, err := statement.CanonicalDigest(); err == nil {
		decision.StatementDigest = digest
	} else {
		decision.Reasons = append(decision.Reasons, "statement canonical digest failed: "+err.Error())
	}

	trusted := pinnedKeys(p)
	builders := allowedBuilders(p)
	if len(trusted) == 0 {
		decision.Reasons = append(decision.Reasons, "policy has no pinned public keys")
	}
	if len(builders) == 0 {
		decision.Reasons = append(decision.Reasons, "policy has no allowed builders")
	}
	if p.SBOMDigest == "" {
		decision.Reasons = append(decision.Reasons, "policy has no required SBOM digest")
	}
	if statement.Builder.ID == "" || !builders[statement.Builder.ID] {
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("builder %q is not allow-listed", statement.Builder.ID))
	}

	if err := provenance.Verify(statement, provenance.VerifyOptions{
		TrustedPublicKeys: trusted,
		SBOMDigest:        p.SBOMDigest,
	}); err != nil {
		decision.Reasons = append(decision.Reasons, err.Error())
	}
	if p.RequireScannerEvidence || len(p.ScannerEvidence) != 0 {
		decision.ScannerEvidenceDigest, _ = scannerEvidenceDigest(p.ScannerEvidence)
		for _, reason := range evaluateScannerEvidence(p.ScannerEvidence, p.RequireScannerEvidence, time.Now().UTC()) {
			decision.Reasons = append(decision.Reasons, reason)
		}
	}

	sort.Strings(decision.Reasons)
	if len(decision.Reasons) == 0 {
		decision.Status = StatusAdmit
		decision.Admitted = true
	}
	decision.Digest = decisionDigest(decision)
	return decision
}

// Admit is a named alias for Evaluate for callers expressing the operation
// as a release-admission action.
func Admit(p Policy, statement provenance.Statement) Decision {
	return Evaluate(p, statement)
}

// Verify returns nil only when Evaluate records ADMIT.
func Verify(p Policy, statement provenance.Statement) error {
	decision := Evaluate(p, statement)
	if decision.Admitted {
		return nil
	}
	return fmt.Errorf("releaseadmission: %s: %s", decision.Status, strings.Join(decision.Reasons, "; "))
}

// Explain renders an audit-safe, deterministic description of the decision.
func (d Decision) Explain() string {
	if len(d.Reasons) == 0 {
		return fmt.Sprintf("release admission %s (builder %s, sbom %s, statement %s, decision %s)", d.Status, d.BuilderID, d.SBOMDigest, d.StatementDigest, d.Digest)
	}
	return fmt.Sprintf("release admission %s (builder %s, sbom %s, statement %s, decision %s; reasons: %s)", d.Status, d.BuilderID, d.SBOMDigest, d.StatementDigest, d.Digest, strings.Join(d.Reasons, "; "))
}

func pinnedKeys(p Policy) map[string]bool {
	keys := make(map[string]bool, len(p.TrustedPublicKeys)+len(p.PinnedPublicKeys))
	for key, trusted := range p.TrustedPublicKeys {
		if trusted {
			keys[key] = true
		}
	}
	for _, key := range p.PinnedPublicKeys {
		if key != "" {
			keys[key] = true
		}
	}
	return keys
}

func allowedBuilders(p Policy) map[string]bool {
	builders := make(map[string]bool, len(p.AllowedBuilders)+len(p.BuilderAllowlist))
	for builder, allowed := range p.AllowedBuilders {
		if allowed {
			builders[builder] = true
		}
	}
	for _, builder := range p.BuilderAllowlist {
		if builder != "" {
			builders[builder] = true
		}
	}
	return builders
}

func decisionDigest(d Decision) string {
	view := struct {
		Status                Status   `json:"status"`
		Admitted              bool     `json:"admitted"`
		PolicyVersion         int      `json:"policy_version"`
		BuilderID             string   `json:"builder_id"`
		SignerPublicKey       string   `json:"signer_public_key"`
		SBOMDigest            string   `json:"sbom_digest"`
		StatementDigest       string   `json:"statement_digest"`
		ScannerEvidenceDigest string   `json:"scanner_evidence_digest,omitempty"`
		Reasons               []string `json:"reasons,omitempty"`
	}{d.Status, d.Admitted, d.PolicyVersion, d.BuilderID, d.SignerPublicKey, d.SBOMDigest, d.StatementDigest, d.ScannerEvidenceDigest, d.Reasons}
	b, _ := json.Marshal(view)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
