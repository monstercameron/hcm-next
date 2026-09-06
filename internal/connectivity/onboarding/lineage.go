package onboarding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// LineageSchema is the stable offline package schema identifier.
const LineageSchema = "hcmnext.onboarding.lineage/1"

var (
	ErrLineageInvalid  = errors.New("onboarding: invalid lineage package")
	ErrLineageTampered = errors.New("onboarding: lineage package is tampered")
)

// LineageMember is the complete causal path for one loaded value. References
// are identifiers or digests only; the package never copies source payloads.
type LineageMember struct {
	Sequence             uint64
	ValueID              string
	TenantID             string
	Object               string
	ExternalID           string
	SourceSnapshot       string
	SourceRow            string
	SourceOffset         int64
	SourceDigest         string
	TransformRef         string
	TransformDigest      string
	CrosswalkRef         string
	CrosswalkDigest      string
	IdentityOutcome      IdentityOutcome
	CanonicalIdentityRef string
	ProposalRef          string
	TransactionRef       string
	ObservationRef       string
	CorrectionRef        string
	Digest               string
}

func (m LineageMember) validate(tenant, snapshot string) error {
	if m.Sequence == 0 || strings.TrimSpace(m.ValueID) == "" || m.TenantID != tenant || strings.TrimSpace(m.Object) == "" || strings.TrimSpace(m.ExternalID) == "" || m.SourceSnapshot != snapshot || strings.TrimSpace(m.SourceRow) == "" || m.SourceOffset < 0 || !validLineageDigest(m.SourceDigest) || strings.TrimSpace(m.TransformRef) == "" || !validLineageDigest(m.TransformDigest) || strings.TrimSpace(m.CrosswalkRef) == "" || !validLineageDigest(m.CrosswalkDigest) || !m.IdentityOutcome.Valid() || strings.TrimSpace(m.CanonicalIdentityRef) == "" || strings.TrimSpace(m.ProposalRef) == "" || strings.TrimSpace(m.TransactionRef) == "" || strings.TrimSpace(m.ObservationRef) == "" || strings.TrimSpace(m.CorrectionRef) == "" {
		return fmt.Errorf("%w: member %q is missing a causal edge", ErrLineageInvalid, m.ValueID)
	}
	want := m.memberDigest()
	if m.Digest != "" && m.Digest != want {
		return fmt.Errorf("%w: member %q digest mismatch", ErrLineageTampered, m.ValueID)
	}
	return nil
}

// LineageEvidence binds a package to the source-freeze/delta and
// reconciliation evidence supplied by the later onboarding gates. The
// package exports their references; it does not execute those gates.
type LineageEvidence struct {
	FreezeEpoch          string
	DeltaDigest          string
	ReconciliationDigest string
}

// LineagePackage is a self-contained, payload-free onboarding lineage
// package. Its digest covers metadata, dependency evidence, member count and
// every member digest, making missing and tampered members detectable offline.
type LineagePackage struct {
	Schema         string
	TenantID       string
	RunID          string
	ManifestDigest string
	SourceSnapshot string
	Evidence       LineageEvidence
	Members        []LineageMember
	Digest         string
}

type canonicalLineageMember struct {
	Sequence             uint64 `json:"sequence"`
	ValueID              string `json:"value_id"`
	TenantID             string `json:"tenant_id"`
	Object               string `json:"object"`
	ExternalID           string `json:"external_id"`
	SourceSnapshot       string `json:"source_snapshot"`
	SourceRow            string `json:"source_row"`
	SourceOffset         int64  `json:"source_offset"`
	SourceDigest         string `json:"source_digest"`
	TransformRef         string `json:"transform_ref"`
	TransformDigest      string `json:"transform_digest"`
	CrosswalkRef         string `json:"crosswalk_ref"`
	CrosswalkDigest      string `json:"crosswalk_digest"`
	IdentityOutcome      string `json:"identity_outcome"`
	CanonicalIdentityRef string `json:"canonical_identity_ref"`
	ProposalRef          string `json:"proposal_ref"`
	TransactionRef       string `json:"transaction_ref"`
	ObservationRef       string `json:"observation_ref"`
	CorrectionRef        string `json:"correction_ref"`
}

func (m LineageMember) canonical() canonicalLineageMember {
	return canonicalLineageMember{m.Sequence, m.ValueID, m.TenantID, m.Object, m.ExternalID, m.SourceSnapshot, m.SourceRow, m.SourceOffset, m.SourceDigest, m.TransformRef, m.TransformDigest, m.CrosswalkRef, m.CrosswalkDigest, string(m.IdentityOutcome), m.CanonicalIdentityRef, m.ProposalRef, m.TransactionRef, m.ObservationRef, m.CorrectionRef}
}

func (m LineageMember) memberDigest() string {
	raw, _ := json.Marshal(m.canonical())
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Digest computes the digest of one member without trusting its recorded
// Digest field.
func (m LineageMember) DigestValue() string { return m.memberDigest() }

type canonicalLineagePackage struct {
	Schema         string                 `json:"schema"`
	TenantID       string                 `json:"tenant_id"`
	RunID          string                 `json:"run_id"`
	ManifestDigest string                 `json:"manifest_digest"`
	SourceSnapshot string                 `json:"source_snapshot"`
	Evidence       LineageEvidence        `json:"evidence"`
	Members        []canonicalLineageItem `json:"members"`
}

type canonicalLineageItem struct {
	Member canonicalLineageMember `json:"member"`
	Digest string                 `json:"digest"`
}

func (p LineagePackage) canonical() ([]byte, error) {
	items := make([]canonicalLineageItem, 0, len(p.Members))
	for _, member := range p.Members {
		items = append(items, canonicalLineageItem{Member: member.canonical(), Digest: member.Digest})
	}
	return json.Marshal(canonicalLineagePackage{Schema: p.Schema, TenantID: p.TenantID, RunID: p.RunID, ManifestDigest: p.ManifestDigest, SourceSnapshot: p.SourceSnapshot, Evidence: p.Evidence, Members: items})
}

func (p LineagePackage) digestValue() string {
	raw, _ := p.canonical()
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// NewLineagePackage validates and seals a package. Members are copied and
// sorted by sequence so a retry or map-derived input has deterministic bytes.
func NewLineagePackage(p LineagePackage) (LineagePackage, error) {
	if strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.RunID) == "" || !validLineageDigest(p.ManifestDigest) || strings.TrimSpace(p.SourceSnapshot) == "" || strings.TrimSpace(p.Evidence.FreezeEpoch) == "" || !validLineageDigest(p.Evidence.DeltaDigest) || !validLineageDigest(p.Evidence.ReconciliationDigest) || len(p.Members) == 0 {
		return LineagePackage{}, ErrLineageInvalid
	}
	p.Schema = LineageSchema
	p.Members = append([]LineageMember(nil), p.Members...)
	sort.SliceStable(p.Members, func(i, j int) bool { return p.Members[i].Sequence < p.Members[j].Sequence })
	for i := range p.Members {
		if err := p.Members[i].validate(p.TenantID, p.SourceSnapshot); err != nil {
			return LineagePackage{}, err
		}
		p.Members[i].Digest = p.Members[i].memberDigest()
	}
	p.Digest = p.digestValue()
	return p, nil
}

// Verify checks package identity, every member's causal edges and every
// member digest without consulting a database, connector or network.
func (p LineagePackage) Verify() error {
	if p.Schema != LineageSchema || strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.RunID) == "" || !validLineageDigest(p.ManifestDigest) || strings.TrimSpace(p.SourceSnapshot) == "" || strings.TrimSpace(p.Evidence.FreezeEpoch) == "" || !validLineageDigest(p.Evidence.DeltaDigest) || !validLineageDigest(p.Evidence.ReconciliationDigest) || len(p.Members) == 0 || p.Digest == "" {
		return ErrLineageInvalid
	}
	last := uint64(0)
	for _, member := range p.Members {
		if member.Sequence <= last {
			return fmt.Errorf("%w: member sequence is not strictly increasing", ErrLineageTampered)
		}
		last = member.Sequence
		if err := member.validate(p.TenantID, p.SourceSnapshot); err != nil {
			return err
		}
		if member.Digest != member.memberDigest() {
			return fmt.Errorf("%w: member %q digest mismatch", ErrLineageTampered, member.ValueID)
		}
	}
	if p.Digest != p.digestValue() {
		return ErrLineageTampered
	}
	return nil
}

// VerifyOffline is the concise verifier used by export/download adapters.
func VerifyOffline(p LineagePackage) bool { return p.Verify() == nil }

// VerifyLineagePackage is an explicit package-level verifier alias.
func VerifyLineagePackage(p LineagePackage) error { return p.Verify() }

// Explain returns bounded package facts and digests, never source values.
func (p LineagePackage) Explain() string {
	return fmt.Sprintf("onboarding lineage schema=%s tenant=%s run=%s snapshot=%s members=%d digest=%s", p.Schema, p.TenantID, p.RunID, p.SourceSnapshot, len(p.Members), p.Digest)
}

// ExplainLineage is the package-level Explain-shaped entry point.
func ExplainLineage(p LineagePackage) string { return p.Explain() }

func validLineageDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
