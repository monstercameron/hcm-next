package attest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// EvidenceAuthorization is the server-side decision supplied to an export.
// Raw subject and response identifiers are not copied into the package.
type EvidenceAuthorization struct {
	Tenant           string
	Purpose          string
	Recipient        string
	RedactionProfile string
	DecisionDigest   string
	Allowed          bool
}

// EvidencePackage is a digest-bound, redacted, offline-verifiable receipt.
// It contains references and proofs, not the subject's raw value or response
// reason text.
type EvidencePackage struct {
	Schema              string         `json:"schema"`
	TenantDigest        string         `json:"tenant_digest"`
	Purpose             string         `json:"purpose"`
	RecipientDigest     string         `json:"recipient_digest"`
	RedactionProfile    string         `json:"redaction_profile"`
	AuthorizationDigest string         `json:"authorization_digest"`
	StatementIDDigest   string         `json:"statement_id_digest"`
	StatementVersion    uint64         `json:"statement_version"`
	StatementDigest     string         `json:"statement_digest"`
	BindingDigest       string         `json:"binding_digest"`
	ResponseDigest      string         `json:"response_digest"`
	ResponseStatus      string         `json:"response_status"`
	ResponseKind        string         `json:"response_kind"`
	RecordedAt          string         `json:"recorded_at"`
	TimeEvidenceID      string         `json:"time_evidence_id"`
	TimeSource          string         `json:"time_source"`
	Corrections         []EvidenceLink `json:"corrections,omitempty"`
	Digest              string         `json:"digest"`
}

// EvidenceLink retains correction/revocation lineage without disclosing raw
// response identifiers or free-text reasons.
type EvidenceLink struct {
	Kind            string `json:"kind"`
	TargetDigest    string `json:"target_digest"`
	ReasonDigest    string `json:"reason_digest"`
	AuthorityDigest string `json:"authority_digest"`
}

type ExportRequest struct {
	Authorization    EvidenceAuthorization
	StatementID      string
	StatementVersion uint64
	StatementDigest  string
	BindingDigest    string
	Response         Response
	History          []Response
}

// Verification is deliberately stable and non-sensitive. Invalid identifies
// the first material field/state/version that prevented offline verification.
type Verification struct {
	Valid   bool
	Status  string
	Digest  string
	Invalid string
}

// Export applies the supplied authorization and constructs a redacted package.
func Export(req ExportRequest) (EvidencePackage, error) {
	if !req.Authorization.Allowed {
		return EvidencePackage{}, fmt.Errorf("%w: export authorization denied", ErrEvidencePackage)
	}
	if strings.TrimSpace(req.Authorization.Tenant) == "" || strings.TrimSpace(req.Authorization.Purpose) == "" || strings.TrimSpace(req.Authorization.Recipient) == "" || strings.TrimSpace(req.Authorization.RedactionProfile) == "" || strings.TrimSpace(req.Authorization.DecisionDigest) == "" {
		return EvidencePackage{}, fmt.Errorf("%w: incomplete export authorization", ErrEvidencePackage)
	}
	if req.StatementVersion == 0 || strings.TrimSpace(req.StatementID) == "" || strings.TrimSpace(req.StatementDigest) == "" || strings.TrimSpace(req.BindingDigest) == "" {
		return EvidencePackage{}, fmt.Errorf("%w: incomplete statement evidence", ErrEvidencePackage)
	}
	if req.Response.Status == "" || req.Response.Digest == "" || req.Response.RecordedAt.Validate() != nil {
		return EvidencePackage{}, fmt.Errorf("%w: incomplete response evidence", ErrEvidencePackage)
	}
	p := EvidencePackage{
		Schema: "hcmnext.attestation.evidence/1", TenantDigest: hash("tenant", req.Authorization.Tenant), Purpose: req.Authorization.Purpose,
		RecipientDigest: hash("recipient", req.Authorization.Recipient), RedactionProfile: req.Authorization.RedactionProfile,
		AuthorizationDigest: req.Authorization.DecisionDigest, StatementIDDigest: hash("statement", req.StatementID), StatementVersion: req.StatementVersion,
		StatementDigest: req.StatementDigest, BindingDigest: req.BindingDigest, ResponseDigest: req.Response.Digest,
		ResponseStatus: string(req.Response.Status), ResponseKind: string(req.Response.Kind), RecordedAt: req.Response.RecordedAt.At.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		TimeEvidenceID: req.Response.RecordedAt.EvidenceID, TimeSource: req.Response.RecordedAt.Source,
	}
	for _, row := range req.History {
		if row.Kind == AssertionResponse {
			continue
		}
		p.Corrections = append(p.Corrections, EvidenceLink{Kind: string(row.Kind), TargetDigest: hash("target", row.CorrectsResponseID), ReasonDigest: hash("reason", row.Reason), AuthorityDigest: hash("authority", row.Authority)})
	}
	sort.Slice(p.Corrections, func(i, j int) bool { return p.Corrections[i].TargetDigest < p.Corrections[j].TargetDigest })
	p.Digest = p.ContentDigest()
	return p, nil
}

// ExportEvidencePackage is the descriptive export spelling.
func ExportEvidencePackage(req ExportRequest) (EvidencePackage, error) { return Export(req) }

func hash(profile, value string) string {
	h := sha256.Sum256([]byte(profile + "\x00" + value))
	return "sha256:" + hex.EncodeToString(h[:])
}

func (p EvidencePackage) canonical() ([]byte, error) {
	clone := p
	clone.Digest = ""
	return json.Marshal(clone)
}

// ContentDigest is the digest of all package fields except Digest itself.
func (p EvidencePackage) ContentDigest() string {
	b, _ := p.canonical()
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// Verify performs offline integrity, completeness and redaction checks.
func (p EvidencePackage) Verify() Verification {
	r := Verification{Digest: p.ContentDigest(), Status: "UNKNOWN"}
	check := func(ok bool, field string) bool {
		if !ok && r.Invalid == "" {
			r.Invalid = field
		}
		return ok
	}
	check(p.Schema == "hcmnext.attestation.evidence/1", "schema")
	check(p.TenantDigest != "", "tenant_digest")
	check(p.Purpose != "", "purpose")
	check(p.RecipientDigest != "", "recipient_digest")
	check(p.RedactionProfile != "", "redaction_profile")
	check(p.AuthorizationDigest != "", "authorization_digest")
	check(p.StatementVersion > 0, "statement_version")
	check(p.StatementDigest != "", "statement_digest")
	check(p.BindingDigest != "", "binding_digest")
	check(p.ResponseDigest != "", "response_digest")
	check(p.ResponseStatus == string(ResponseAccepted) || p.ResponseStatus == string(ResponseRefused) || p.ResponseStatus == string(ResponseUnknown), "response_status")
	check(p.ResponseKind == string(AssertionResponse) || p.ResponseKind == string(AssertionCorrection) || p.ResponseKind == string(AssertionRevocation), "response_kind")
	check(p.RecordedAt != "" && p.TimeEvidenceID != "" && p.TimeSource != "", "time")
	check(!strings.Contains(p.Purpose, "\x00"), "purpose")
	if p.Digest == "" || p.Digest != r.Digest {
		check(false, "digest")
	}
	for i, link := range p.Corrections {
		check(link.Kind == string(AssertionCorrection) || link.Kind == string(AssertionRevocation), fmt.Sprintf("corrections[%d].kind", i))
		check(link.TargetDigest != "" && link.ReasonDigest != "" && link.AuthorityDigest != "", fmt.Sprintf("corrections[%d]", i))
	}
	if r.Invalid != "" {
		r.Status = "ATTEST_007_REJECTED"
		return r
	}
	r.Valid, r.Status = true, "COMPLETE"
	return r
}

// VerifyEvidencePackage is the concise package-level verifier.
func VerifyEvidencePackage(p EvidencePackage) Verification { return p.Verify() }

// VerifyEvidence is a concise offline-verification spelling.
func VerifyEvidence(p EvidencePackage) Verification { return p.Verify() }

// Marshal returns the portable JSON representation after verification.
func (p EvidencePackage) Marshal() ([]byte, error) {
	if result := p.Verify(); !result.Valid {
		return nil, errors.New(result.Status + ": " + result.Invalid)
	}
	return json.Marshal(p)
}
