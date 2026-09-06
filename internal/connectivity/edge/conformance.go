package edge

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const edge008Version = 1

var (
	ErrInvalidIngressLimits = errors.New("edge: invalid ingress limits")
	ErrInvalidAttackCorpus  = errors.New("edge: invalid attack corpus")
	ErrEvidenceSigner       = errors.New("edge: invalid release evidence signer")
)

// IngressLimits is the pinned, fail-closed budget for an externally supplied
// request. It is deliberately scalar so the boundary can be checked before
// any domain, ledger, queue, or provider work is planned.
type IngressLimits struct {
	ContractVersion      int
	MaxBodyBytes         int64
	MaxHeaderBytes       int64
	MaxHeaderRead        time.Duration
	MaxBodyRead          time.Duration
	MaxActiveConnections int
}

// IngressAttempt is the bounded metadata observed by the edge before an
// accepted request can enter an application-owned execution path.
type IngressAttempt struct {
	RequestID         string
	TenantID          string
	ContractVersion   int
	BodyBytes         int64
	HeaderBytes       int64
	HeaderRead        time.Duration
	BodyRead          time.Duration
	ActiveConnections int
	TrustState        string
}

// EffectPlan describes work that would be created after admission. A rejected
// attempt always returns a zero EffectPlan, making the side-effect fence
// inspectable without coupling this contract to a storage implementation.
type EffectPlan struct {
	AuthoritativeRows int
	BusinessEvents    int
	OutboxEntries     int
	HumanWork         int
	ProviderRequests  int
}

// IngressDecision is the stable edge result. Field, State, and Version are
// included on rejection so evidence identifies the exact failed contract.
type IngressDecision struct {
	Allowed bool
	Code    string
	Field   string
	State   string
	Version int
	Effects EffectPlan
}

// Rejection is a typed, payload-free edge admission failure.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version int
}

func (r *Rejection) Error() string {
	if r == nil {
		return "edge: admission rejected"
	}
	return fmt.Sprintf("%s field=%s state=%s version=%d", r.Code, r.Field, r.State, r.Version)
}

// EvaluateIngress enforces the EDGE-008 request fence. It is pure: the plan
// is returned only for an admitted request, and no storage or provider call is
// made by this package.
func EvaluateIngress(limits IngressLimits, attempt IngressAttempt, plan EffectPlan) (IngressDecision, error) {
	if err := validateIngressLimits(limits); err != nil {
		return IngressDecision{}, err
	}
	reject := func(field, state string) (IngressDecision, error) {
		decision := IngressDecision{Code: "EDGE_008_REJECTED", Field: field, State: state, Version: limits.ContractVersion}
		return decision, &Rejection{Code: decision.Code, Field: field, State: state, Version: decision.Version}
	}
	if attempt.ContractVersion != limits.ContractVersion {
		return reject("contract_version", "unsupported_version")
	}
	if strings.TrimSpace(attempt.RequestID) == "" {
		return reject("request_id", "missing")
	}
	if strings.TrimSpace(attempt.TenantID) == "" {
		return reject("tenant_id", "missing")
	}
	if attempt.BodyBytes < 0 || attempt.BodyBytes > limits.MaxBodyBytes {
		return reject("body_bytes", "oversized")
	}
	if attempt.HeaderBytes < 0 || attempt.HeaderBytes > limits.MaxHeaderBytes {
		return reject("header_bytes", "oversized")
	}
	if attempt.HeaderRead < 0 || attempt.HeaderRead > limits.MaxHeaderRead {
		return reject("header_read", "slow")
	}
	if attempt.BodyRead < 0 || attempt.BodyRead > limits.MaxBodyRead {
		return reject("body_read", "slow")
	}
	if attempt.ActiveConnections < 0 || attempt.ActiveConnections > limits.MaxActiveConnections {
		return reject("active_connections", "over_limit")
	}
	if attempt.TrustState != "VERIFIED" {
		return reject("trust_state", "unverified")
	}
	return IngressDecision{Allowed: true, Code: "EDGE_008_ACCEPTED", Version: limits.ContractVersion, Effects: plan}, nil
}

func validateIngressLimits(limits IngressLimits) error {
	if limits.ContractVersion != edge008Version || limits.MaxBodyBytes <= 0 || limits.MaxHeaderBytes <= 0 || limits.MaxHeaderRead <= 0 || limits.MaxBodyRead <= 0 || limits.MaxActiveConnections <= 0 {
		return ErrInvalidIngressLimits
	}
	return nil
}

// AttackCase is one pinned adversarial input and its expected rejection state.
type AttackCase struct {
	ID            string
	Category      string
	Attempt       IngressAttempt
	ExpectedState string
}

// AttackCorpus is the immutable input set used for EDGE-008 release evidence.
type AttackCorpus struct {
	Version string
	Cases   []AttackCase
}

// Digest returns a canonical identity for the corpus.
func (c AttackCorpus) Digest() (string, error) {
	if err := validateAttackCorpus(c); err != nil {
		return "", err
	}
	cases := append([]AttackCase(nil), c.Cases...)
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	b, err := json.Marshal(struct {
		Version string       `json:"version"`
		Cases   []AttackCase `json:"cases"`
	}{c.Version, cases})
	if err != nil {
		return "", err
	}
	return digestBytes(b), nil
}

func validateAttackCorpus(c AttackCorpus) error {
	version := strings.ToLower(strings.TrimSpace(c.Version))
	if version == "" || strings.ContainsAny(version, "*?") || version == "latest" || version == "current" || version == "floating" || len(c.Cases) == 0 {
		return ErrInvalidAttackCorpus
	}
	seen := make(map[string]struct{}, len(c.Cases))
	for _, item := range c.Cases {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Category) == "" || item.ExpectedState == "" {
			return ErrInvalidAttackCorpus
		}
		if _, ok := seen[item.ID]; ok {
			return ErrInvalidAttackCorpus
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

// ReleaseEvidence is signed evidence that every corpus case was rejected by
// the pinned ingress fence without any planned side effect.
type ReleaseEvidence struct {
	ContractVersion int    `json:"contract_version"`
	CorpusVersion   string `json:"corpus_version"`
	CorpusDigest    string `json:"corpus_digest"`
	Cases           int    `json:"cases"`
	Rejected        int    `json:"rejected"`
	Status          string `json:"status"`
	EvidenceDigest  string `json:"evidence_digest"`
	Signature       string `json:"signature"`
}

// RunConformance executes a pinned adversarial corpus and signs its result.
func RunConformance(limits IngressLimits, corpus AttackCorpus, signer ed25519.PrivateKey) (ReleaseEvidence, error) {
	if len(signer) != ed25519.PrivateKeySize {
		return ReleaseEvidence{}, ErrEvidenceSigner
	}
	corpusDigest, err := corpus.Digest()
	if err != nil {
		return ReleaseEvidence{}, err
	}
	for _, item := range corpus.Cases {
		decision, evalErr := EvaluateIngress(limits, item.Attempt, EffectPlan{AuthoritativeRows: 1, BusinessEvents: 1, OutboxEntries: 1, HumanWork: 1, ProviderRequests: 1})
		if evalErr == nil || decision.Allowed || decision.State != item.ExpectedState || decision.Effects != (EffectPlan{}) {
			return ReleaseEvidence{}, fmt.Errorf("%w: case %s was not fenced", ErrInvalidAttackCorpus, item.ID)
		}
	}
	evidence := ReleaseEvidence{ContractVersion: limits.ContractVersion, CorpusVersion: corpus.Version, CorpusDigest: corpusDigest, Cases: len(corpus.Cases), Rejected: len(corpus.Cases), Status: "READY"}
	evidence.EvidenceDigest = digestEvidence(evidence)
	evidence.Signature = hex.EncodeToString(ed25519.Sign(signer, evidenceSigningBytes(evidence)))
	return evidence, nil
}

// Verify checks the signature without exposing attack payloads.
func (e ReleaseEvidence) Verify(key ed25519.PublicKey) bool {
	if len(key) != ed25519.PublicKeySize || e.Status != "READY" || e.Cases == 0 || e.Cases != e.Rejected || e.EvidenceDigest != digestEvidence(e) {
		return false
	}
	signature, err := hex.DecodeString(e.Signature)
	return err == nil && ed25519.Verify(key, evidenceSigningBytes(e), signature)
}

func digestEvidence(e ReleaseEvidence) string {
	e.Signature = ""
	e.EvidenceDigest = ""
	return digestBytes(evidenceSigningBytes(e))
}

func evidenceSigningBytes(e ReleaseEvidence) []byte {
	e.Signature = ""
	e.EvidenceDigest = ""
	b, _ := json.Marshal(e)
	return b
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ExplainConformance describes the EDGE-008 contract without including input
// payloads, tenant identifiers, or provider details.
func ExplainConformance() string {
	return "EDGE-008 v1: pinned ingress byte/time/trust budgets, zero-effect rejection, and signed adversarial evidence"
}
