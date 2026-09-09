package configbundle

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// FeatureVariant is one weighted result of an enabled feature rule. Weights
// are percentages and must total 100 when variants are present.
type FeatureVariant struct {
	Name   string `json:"name"`
	Weight uint8  `json:"weight"`
}

// FeatureRule is the local, deterministic policy for one feature.
type FeatureRule struct {
	Version        uint64           `json:"version"`
	Enabled        bool             `json:"enabled"`
	RolloutPercent uint8            `json:"rollout_percent"`
	Salt           string           `json:"salt"`
	KillSwitch     bool             `json:"kill_switch"`
	Variants       []FeatureVariant `json:"variants,omitempty"`
}

// FeatureState is the signed, tenant/org-scoped feature image evaluated by a
// service without a control-plane or network dependency.
type FeatureState struct {
	TenantID     string                 `json:"tenant_id"`
	OrgID        string                 `json:"org_id"`
	Version      uint64                 `json:"version"`
	IssuedAt     time.Time              `json:"issued_at"`
	ExpiresAt    time.Time              `json:"expires_at"`
	KillSwitches map[string]bool        `json:"kill_switches,omitempty"`
	Features     map[string]FeatureRule `json:"features"`
}

// SignedFeatureState is the detached-signature wire form of FeatureState.
type SignedFeatureState struct {
	State     FeatureState    `json:"state"`
	Digest    string          `json:"digest"`
	Signature BundleSignature `json:"signature"`
}

// FeatureRequest supplies every scope component used by local evaluation.
type FeatureRequest struct {
	TenantID string `json:"tenant_id"`
	OrgID    string `json:"org_id"`
	UserID   string `json:"user_id"`
	Feature  string `json:"feature"`
}

// FeatureDecision is a versioned local decision. Denials retain a stable
// reason so readiness and rollout telemetry can distinguish kill switches,
// rollout exclusion, and unknown features without inspecting policy bodies.
type FeatureDecision struct {
	Decision     string `json:"decision"`
	Variant      string `json:"variant,omitempty"`
	StateVersion uint64 `json:"state_version"`
	RuleVersion  uint64 `json:"rule_version"`
	Reason       string `json:"reason"`
}

const (
	FeatureAllow = "ALLOW"
	FeatureDeny  = "DENY"
)

// SignFeatureState validates and signs one feature state. The signature is
// over the canonical state digest and carries the immutable signer identity.
func SignFeatureState(state FeatureState, keyHandle, keyVersion string, privateKey ed25519.PrivateKey) (SignedFeatureState, error) {
	if strings.TrimSpace(keyHandle) == "" || strings.TrimSpace(keyVersion) == "" || len(privateKey) != ed25519.PrivateKeySize {
		return SignedFeatureState{}, featureRefusal("INVALID_SIGNING_KEY", "signature", "key identity and Ed25519 private key are required", ErrFeatureStateInvalid)
	}
	digest, err := state.DigestValue()
	if err != nil {
		return SignedFeatureState{}, err
	}
	raw, err := digestBytes(digest)
	if err != nil {
		return SignedFeatureState{}, err
	}
	return SignedFeatureState{State: cloneFeatureState(state), Digest: digest, Signature: BundleSignature{Algorithm: "ed25519", KeyHandle: strings.TrimSpace(keyHandle), KeyVersion: strings.TrimSpace(keyVersion), KeyID: strings.TrimSpace(keyHandle), Value: encodeSignature(ed25519.Sign(privateKey, raw))}}, nil
}

// Canonical returns deterministic bytes for the state, independent of map or
// variant ordering.
func (s FeatureState) Canonical() []byte {
	w, err := s.canonical()
	if err != nil {
		return nil
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s FeatureState) canonical() (*canonicalbytes.Writer, error) {
	if err := validateFeatureState(s); err != nil {
		return nil, err
	}
	w := canonicalbytes.New("hcmnext.platform.configbundle.FeatureState", 1).
		String("tenant_id", s.TenantID).
		String("org_id", s.OrgID).
		Int("version", int64(s.Version)).
		String("issued_at", s.IssuedAt.UTC().Format(time.RFC3339Nano)).
		String("expires_at", s.ExpiresAt.UTC().Format(time.RFC3339Nano))
	killNames := make([]string, 0, len(s.KillSwitches))
	for name, enabled := range s.KillSwitches {
		if enabled {
			killNames = append(killNames, name)
		}
	}
	sort.Strings(killNames)
	w.SortedStrings("kill_switch", killNames)
	featureNames := make([]string, 0, len(s.Features))
	for name := range s.Features {
		featureNames = append(featureNames, name)
	}
	sort.Strings(featureNames)
	w.Count("feature", len(featureNames))
	for _, name := range featureNames {
		rule := s.Features[name]
		nested := canonicalbytes.New("hcmnext.platform.configbundle.FeatureRule", 1).
			String("name", name).
			Int("version", int64(rule.Version)).
			Bool("enabled", rule.Enabled).
			Int("rollout_percent", int64(rule.RolloutPercent)).
			String("salt", rule.Salt).
			Bool("kill_switch", rule.KillSwitch).
			Count("variant", len(rule.Variants))
		for _, variant := range sortedVariants(rule.Variants) {
			nested.String("variant_name", variant.Name).Int("variant_weight", int64(variant.Weight))
		}
		w.Nested("feature:"+name, nested)
	}
	return w, nil
}

// DigestValue computes the state digest without trusting any signed fields.
func (s FeatureState) DigestValue() (string, error) {
	w, err := s.canonical()
	if err != nil {
		return "", err
	}
	return w.Digest()
}

// Verify checks state identity and its detached Ed25519 signature.
func (s SignedFeatureState) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(s.Signature); err != nil {
		return featureRefusal("INVALID_FEATURE_SIGNATURE", "signature", "feature-state signature identity is invalid", err)
	}
	digest, err := s.State.DigestValue()
	if err != nil {
		return err
	}
	if digest != s.Digest {
		return featureRefusal("FEATURE_STATE_MUTATED", "digest", "recorded state digest differs from canonical state", ErrFeatureStateInvalid)
	}
	raw, err := digestBytes(s.Digest)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(s.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, raw, signature) {
		return featureRefusal("INVALID_FEATURE_SIGNATURE", "signature.value", "feature-state signature verification failed", ErrFeatureSignatureInvalid)
	}
	return nil
}

// Evaluate is the value-oriented spelling for callers holding one signed
// state. It uses the supplied local evaluation time.
func (s SignedFeatureState) Evaluate(publicKey ed25519.PublicKey, request FeatureRequest, now time.Time) (FeatureDecision, error) {
	return EvaluateFeatureAt(s, publicKey, request, now)
}

// FeatureEvaluator is a local evaluator. It contains only trust material and
// a clock; no remote callback is available on purpose.
type FeatureEvaluator struct {
	PublicKey ed25519.PublicKey
	Now       func() time.Time
}

// Evaluate evaluates at the configured clock (or UTC wall time when no clock
// is configured).
func (e FeatureEvaluator) Evaluate(state SignedFeatureState, request FeatureRequest) (FeatureDecision, error) {
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	return EvaluateFeatureAt(state, e.PublicKey, request, now)
}

// EvaluateFeature evaluates once using the current UTC time.
func EvaluateFeature(state SignedFeatureState, publicKey ed25519.PublicKey, request FeatureRequest) (FeatureDecision, error) {
	return EvaluateFeatureAt(state, publicKey, request, time.Now().UTC())
}

// EvaluateFeatureAt verifies and evaluates a signed state at a supplied time.
func EvaluateFeatureAt(state SignedFeatureState, publicKey ed25519.PublicKey, request FeatureRequest, now time.Time) (FeatureDecision, error) {
	if err := state.Verify(publicKey); err != nil {
		return FeatureDecision{}, err
	}
	if strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.OrgID) == "" || strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.Feature) == "" {
		return FeatureDecision{}, featureRefusal("MISSING_FEATURE_SCOPE", "request", "tenant, org, user, and feature are required", ErrFeatureScopeRequired)
	}
	if request.TenantID != state.State.TenantID || request.OrgID != state.State.OrgID {
		return FeatureDecision{}, featureRefusal("FEATURE_SCOPE_MISMATCH", "request", "request scope differs from signed state scope", ErrFeatureScopeMismatch)
	}
	if now.Before(state.State.IssuedAt) || !now.Before(state.State.ExpiresAt) {
		return FeatureDecision{}, featureRefusal("FEATURE_STATE_EXPIRED", "expires_at", "signed feature state is outside its validity window", ErrFeatureStateExpired)
	}
	rule, ok := state.State.Features[request.Feature]
	base := FeatureDecision{Decision: FeatureDeny, StateVersion: state.State.Version, Reason: "FEATURE_UNKNOWN"}
	if !ok {
		return base, nil
	}
	base.RuleVersion = rule.Version
	if state.State.KillSwitches["*"] || state.State.KillSwitches[request.Feature] || rule.KillSwitch {
		base.Reason = "KILL_SWITCH"
		return base, nil
	}
	if !rule.Enabled {
		base.Reason = "FEATURE_DISABLED"
		return base, nil
	}
	bucket := deterministicBucket(request, rule.Salt, "rollout")
	if bucket >= uint64(rule.RolloutPercent) {
		base.Reason = "ROLLOUT_EXCLUDED"
		return base, nil
	}
	base.Decision = FeatureAllow
	base.Reason = "ROLLOUT_INCLUDED"
	if len(rule.Variants) > 0 {
		variantBucket := deterministicBucket(request, rule.Salt, "variant")
		cursor := uint64(0)
		for _, variant := range sortedVariants(rule.Variants) {
			cursor += uint64(variant.Weight)
			if variantBucket < cursor {
				base.Variant = variant.Name
				break
			}
		}
	}
	return base, nil
}

func deterministicBucket(request FeatureRequest, salt, purpose string) uint64 {
	h := sha256.New()
	h.Write([]byte("hcmnext.feature/v1\x00"))
	for _, value := range []string{purpose, request.TenantID, request.OrgID, request.UserID, request.Feature, salt} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		h.Write(length[:])
		h.Write([]byte(value))
	}
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8]) % 100
}

func validateFeatureState(s FeatureState) error {
	if strings.TrimSpace(s.TenantID) == "" || strings.TrimSpace(s.OrgID) == "" || s.Version == 0 {
		return featureRefusal("INVALID_FEATURE_STATE", "scope", "tenant, org, and positive state version are required", ErrFeatureStateInvalid)
	}
	if s.IssuedAt.IsZero() || s.ExpiresAt.IsZero() || !s.ExpiresAt.After(s.IssuedAt) {
		return featureRefusal("INVALID_FEATURE_WINDOW", "validity", "issued and expiry times must form a non-empty interval", ErrFeatureStateInvalid)
	}
	for name, rule := range s.Features {
		if strings.TrimSpace(name) == "" || rule.Version == 0 || rule.RolloutPercent > 100 {
			return featureRefusal("INVALID_FEATURE_RULE", "features", "feature names, rule versions, and rollout percentages must be valid", ErrFeatureStateInvalid)
		}
		if total := variantWeight(rule.Variants); total != 0 && total != 100 {
			return featureRefusal("INVALID_VARIANT_WEIGHTS", "features."+name, "variant weights must total 100", ErrFeatureStateInvalid)
		}
		for _, variant := range rule.Variants {
			if strings.TrimSpace(variant.Name) == "" || variant.Weight == 0 {
				return featureRefusal("INVALID_VARIANT", "features."+name, "variant names and positive weights are required", ErrFeatureStateInvalid)
			}
		}
	}
	return nil
}

func variantWeight(variants []FeatureVariant) uint16 {
	var total uint16
	for _, variant := range variants {
		total += uint16(variant.Weight)
	}
	return total
}

func sortedVariants(variants []FeatureVariant) []FeatureVariant {
	out := append([]FeatureVariant(nil), variants...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneFeatureState(state FeatureState) FeatureState {
	state.KillSwitches = cloneBoolMap(state.KillSwitches)
	features := state.Features
	state.Features = make(map[string]FeatureRule, len(features))
	for name, rule := range features {
		rule.Variants = append([]FeatureVariant(nil), rule.Variants...)
		state.Features[name] = rule
	}
	return state
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	if input == nil {
		return nil
	}
	out := make(map[string]bool, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func featureRefusal(code, field, detail string, cause error) error {
	return &Error{Code: code, Ref: field, Detail: detail, Cause: cause}
}

var (
	ErrFeatureStateInvalid     = errors.New("configbundle: feature state is invalid")
	ErrFeatureSignatureInvalid = errors.New("configbundle: feature state signature is invalid")
	ErrFeatureScopeRequired    = errors.New("configbundle: feature evaluation scope is required")
	ErrFeatureScopeMismatch    = errors.New("configbundle: feature evaluation scope does not match state")
	ErrFeatureStateExpired     = errors.New("configbundle: feature state is expired")
)
