// Package readiness compiles a side-effect-free customer onboarding rehearsal.
// It records disposition and provenance for every field, record, configuration,
// and identity without becoming a second migration or identity engine.
package readiness

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const schemaVersion = 1

// Version reports the rehearsal evidence schema version.
func Version() int { return schemaVersion }

// Explain describes the no-mutation readiness contract.
func Explain() string {
	return "CUSTOMER-002 v1: exhaustive source disposition, provenance, identity crosswalk, reconciliation, and no-mutation evidence"
}

// Disposition is the only permitted outcome for a rehearsal input.
type Disposition string

const (
	Accept     Disposition = "ACCEPT"
	Transform  Disposition = "TRANSFORM"
	Quarantine Disposition = "QUARANTINE"
	Reject     Disposition = "REJECT"
	Unknown    Disposition = "UNKNOWN"
)

// SourceField carries the metadata needed to avoid silent defaulting.
type SourceField struct {
	Ref            string      `json:"ref"`
	CanonicalPath  string      `json:"canonical_path"`
	Present        bool        `json:"present"`
	AuthorityRef   string      `json:"authority_ref"`
	Classification string      `json:"classification"`
	Purpose        string      `json:"purpose"`
	EffectiveAt    string      `json:"effective_at"`
	Hint           Disposition `json:"hint"`
}

// SourceRecord is one source row and its item-level fields.
type SourceRecord struct {
	Ref    string        `json:"ref"`
	Fields []SourceField `json:"fields"`
}

// Configuration is one source configuration object with an explicit version.
type Configuration struct {
	Ref          string `json:"ref"`
	Version      string `json:"version"`
	Digest       string `json:"digest"`
	Compatible   bool   `json:"compatible"`
	AuthorityRef string `json:"authority_ref"`
}

// IdentityCrosswalk is the source-to-canonical identity mapping evidence.
type IdentityCrosswalk struct {
	SourceRef    string  `json:"source_ref"`
	CanonicalRef string  `json:"canonical_ref"`
	AuthorityRef string  `json:"authority_ref"`
	MatchMethod  string  `json:"match_method"`
	Confidence   float64 `json:"confidence"`
}

// Rehearsal is a bounded, simulated onboarding input. No field contains a
// source value; refs and digests are sufficient for evidence mechanics.
type Rehearsal struct {
	SchemaVersion  int                 `json:"schema_version"`
	RehearsalID    string              `json:"rehearsal_id"`
	TenantRef      string              `json:"tenant_ref"`
	SourceSystem   string              `json:"source_system"`
	Fields         []SourceField       `json:"fields"`
	Records        []SourceRecord      `json:"records"`
	Configurations []Configuration     `json:"configurations"`
	Identities     []IdentityCrosswalk `json:"identities"`
}

// Issue identifies a missing readiness fact.
type Issue struct{ Field, Code, Detail string }

// ItemEvidence is the disposition and reason for one input item.
type ItemEvidence struct {
	Kind         string      `json:"kind"`
	Ref          string      `json:"ref"`
	Disposition  Disposition `json:"disposition"`
	Reason       string      `json:"reason"`
	SourceDigest string      `json:"source_digest"`
}

// Reconciliation proves that aggregate totals do not hide item-level drift.
type Reconciliation struct {
	InputFields              int `json:"input_fields"`
	ClassifiedFields         int `json:"classified_fields"`
	InputRecords             int `json:"input_records"`
	ClassifiedRecords        int `json:"classified_records"`
	InputConfigurations      int `json:"input_configurations"`
	ClassifiedConfigurations int `json:"classified_configurations"`
	InputIdentities          int `json:"input_identities"`
	ClassifiedIdentities     int `json:"classified_identities"`
	EffectiveDates           int `json:"effective_dates"`
	ClassifiedEffectiveDates int `json:"classified_effective_dates"`
}

// Result is the complete rehearsal output. ZeroMutation is always true for a
// result produced by Evaluate.
type Result struct {
	RehearsalID    string         `json:"rehearsal_id"`
	Items          []ItemEvidence `json:"items"`
	Reconciliation Reconciliation `json:"reconciliation"`
	Ready          bool           `json:"ready"`
	ZeroMutation   bool           `json:"zero_mutation"`
	Blockers       []string       `json:"blockers"`
	Digest         string         `json:"digest"`
}

// SignedEvidence is a detached signature over Result.Digest.
type SignedEvidence struct {
	Result    Result `json:"result"`
	SignerRef string `json:"signer_ref"`
	Signature []byte `json:"signature"`
}

// Validate checks the rehearsal envelope, not source values.
func Validate(r Rehearsal) []Issue {
	var out []Issue
	add := func(field, code, detail string) { out = append(out, Issue{field, code, detail}) }
	if r.SchemaVersion != schemaVersion {
		add("schema_version", "UNSUPPORTED_SCHEMA", "schema version must be 1")
	}
	if strings.TrimSpace(r.RehearsalID) == "" {
		add("rehearsal_id", "MISSING_ID", "rehearsal id is required")
	}
	if strings.TrimSpace(r.TenantRef) == "" {
		add("tenant_ref", "MISSING_TENANT", "tenant ref is required")
	}
	if strings.TrimSpace(r.SourceSystem) == "" {
		add("source_system", "MISSING_SOURCE", "source system is required")
	}
	checkField := func(field string, value SourceField) {
		if strings.TrimSpace(value.Ref) == "" || strings.TrimSpace(value.CanonicalPath) == "" || strings.TrimSpace(value.AuthorityRef) == "" || strings.TrimSpace(value.Classification) == "" || strings.TrimSpace(value.Purpose) == "" || strings.TrimSpace(value.EffectiveAt) == "" {
			add(field, "INCOMPLETE_FIELD_PROVENANCE", "field requires ref, canonical path, presence, authority, classification, purpose, and effective time")
		}
		if value.Hint != "" && !validDisposition(value.Hint) {
			add(field+".hint", "INVALID_DISPOSITION", "unknown disposition is not allowed")
		}
	}
	for i, field := range r.Fields {
		checkField(fmt.Sprintf("fields[%d]", i), field)
	}
	for i, record := range r.Records {
		if strings.TrimSpace(record.Ref) == "" {
			add(fmt.Sprintf("records[%d].ref", i), "MISSING_RECORD_REF", "record ref is required")
		}
		if len(record.Fields) == 0 {
			add(fmt.Sprintf("records[%d].fields", i), "MISSING_RECORD_FIELDS", "record fields are required")
		}
		for j, field := range record.Fields {
			checkField(fmt.Sprintf("records[%d].fields[%d]", i, j), field)
		}
	}
	for i, c := range r.Configurations {
		if strings.TrimSpace(c.Ref) == "" || strings.TrimSpace(c.Version) == "" || strings.TrimSpace(c.Digest) == "" || strings.TrimSpace(c.AuthorityRef) == "" {
			add(fmt.Sprintf("configurations[%d]", i), "INCOMPLETE_CONFIGURATION", "configuration requires ref, version, digest, and authority")
		}
	}
	for i, identity := range r.Identities {
		if strings.TrimSpace(identity.SourceRef) == "" || strings.TrimSpace(identity.CanonicalRef) == "" || strings.TrimSpace(identity.AuthorityRef) == "" || strings.TrimSpace(identity.MatchMethod) == "" || identity.Confidence < 0 || identity.Confidence > 1 {
			add(fmt.Sprintf("identities[%d]", i), "INCOMPLETE_IDENTITY", "identity crosswalk requires bounded confidence and authority")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Check returns the first envelope defect.
func Check(r Rehearsal) error {
	if issues := Validate(r); len(issues) > 0 {
		return fmt.Errorf("readiness: %s %s: %s", issues[0].Code, issues[0].Field, issues[0].Detail)
	}
	return nil
}

// Evaluate classifies every input and reconciles item counts. It performs no
// writes and does not mutate the supplied rehearsal.
func Evaluate(r Rehearsal) (Result, error) {
	if err := Check(r); err != nil {
		return Result{}, err
	}
	result := Result{RehearsalID: r.RehearsalID, ZeroMutation: true}
	add := func(kind, ref string, disposition Disposition, reason string) {
		result.Items = append(result.Items, ItemEvidence{Kind: kind, Ref: ref, Disposition: disposition, Reason: reason, SourceDigest: sourceDigest(kind, ref)})
	}
	classifyField := func(kind string, field SourceField) {
		d, reason := fieldDisposition(field)
		add(kind, field.Ref, d, reason)
		result.Reconciliation.InputFields++
		result.Reconciliation.ClassifiedFields++
		if field.EffectiveAt != "" {
			result.Reconciliation.EffectiveDates++
			if d != Unknown {
				result.Reconciliation.ClassifiedEffectiveDates++
			}
		}
	}
	for _, field := range r.Fields {
		classifyField("FIELD", field)
	}
	for _, record := range r.Records {
		d, reason := dispositionForRecord(record)
		add("RECORD", record.Ref, d, reason)
		result.Reconciliation.InputRecords++
		result.Reconciliation.ClassifiedRecords++
		for _, field := range record.Fields {
			classifyField("RECORD_FIELD", field)
		}
	}
	for _, c := range r.Configurations {
		d, reason := dispositionForConfiguration(c)
		add("CONFIGURATION", c.Ref, d, reason)
		result.Reconciliation.InputConfigurations++
		result.Reconciliation.ClassifiedConfigurations++
	}
	for _, identity := range r.Identities {
		d, reason := dispositionForIdentity(identity)
		add("IDENTITY", identity.SourceRef, d, reason)
		result.Reconciliation.InputIdentities++
		result.Reconciliation.ClassifiedIdentities++
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].Kind != result.Items[j].Kind {
			return result.Items[i].Kind < result.Items[j].Kind
		}
		return result.Items[i].Ref < result.Items[j].Ref
	})
	for _, item := range result.Items {
		if item.Disposition != Accept && item.Disposition != Transform {
			result.Blockers = append(result.Blockers, item.Kind+":"+item.Ref+":"+string(item.Disposition))
		}
	}
	sort.Strings(result.Blockers)
	result.Ready = len(result.Blockers) == 0
	digest, err := resultDigest(result)
	if err != nil {
		return Result{}, err
	}
	result.Digest = digest
	return result, nil
}

// Sign creates signed readiness evidence over the deterministic result.
func Sign(result Result, signerRef string, key ed25519.PrivateKey) (SignedEvidence, error) {
	if strings.TrimSpace(signerRef) == "" || len(key) != ed25519.PrivateKeySize || result.Digest == "" {
		return SignedEvidence{}, errors.New("readiness: signer, private key, and result digest are required")
	}
	return SignedEvidence{Result: result, SignerRef: signerRef, Signature: ed25519.Sign(key, []byte(result.Digest))}, nil
}

// Verify checks both the result digest and its detached signature.
func Verify(evidence SignedEvidence, key ed25519.PublicKey) error {
	digest, err := resultDigest(evidence.Result)
	if err != nil || digest != evidence.Result.Digest {
		return errors.New("readiness: result digest mismatch")
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, []byte(evidence.Result.Digest), evidence.Signature) {
		return errors.New("readiness: invalid evidence signature")
	}
	return nil
}

func validDisposition(d Disposition) bool {
	switch d {
	case Accept, Transform, Quarantine, Reject, Unknown:
		return true
	default:
		return false
	}
}
func fieldDisposition(f SourceField) (Disposition, string) {
	if !f.Present {
		return Unknown, "source field presence is not proven; no default is applied"
	}
	if f.Hint != "" {
		return f.Hint, "source mapping supplied an explicit disposition"
	}
	return Accept, "provenance is complete and no transformation was declared"
}
func dispositionForRecord(r SourceRecord) (Disposition, string) {
	if len(r.Fields) == 0 {
		return Unknown, "record has no item-level fields"
	}
	for _, f := range r.Fields {
		if f.Hint == Reject {
			return Reject, "a contained field is rejected"
		}
		if f.Hint == Quarantine {
			return Quarantine, "a contained field is quarantined"
		}
		if f.Hint == Unknown {
			return Unknown, "a contained field is unknown"
		}
	}
	return Accept, "all contained fields are classified"
}
func dispositionForConfiguration(c Configuration) (Disposition, string) {
	if !c.Compatible {
		return Quarantine, "configuration compatibility is not proven"
	}
	return Accept, "versioned configuration is compatible"
}
func dispositionForIdentity(i IdentityCrosswalk) (Disposition, string) {
	if i.Confidence < 0.99 {
		return Quarantine, "identity confidence requires review"
	}
	return Accept, "identity crosswalk is authoritative and high confidence"
}
func sourceDigest(kind, ref string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + ref))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func resultDigest(result Result) (string, error) {
	copyResult := result
	copyResult.Digest = ""
	data, err := json.Marshal(copyResult)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
