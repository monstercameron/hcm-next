// Package workflowpromotion owns the WF-DISC-004 promotion gate. It admits
// an exploratory workflow to CONTRACTED only when the complete closure set
// resolves: exploratory and graph digests, compiled schemas, versioned
// capabilities, typed behaviors, parity records for touched legacy, an owner
// with a live review expiry, and proven negative fixtures. Success yields a
// signed promotion receipt; any gap fails with the complete closure set and
// publishes nothing. It is kernel-pure: validation and ed25519 signatures
// only, no database, network or mutable global.
package workflowpromotion

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdecisions"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

// ContractedState is the only state a promotion receipt may assert.
// Implementation stays a separate evidence transition by design.
const ContractedState = "CONTRACTED"

// SchemaRef binds one compiled schema by content digest.
type SchemaRef struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// Behavior is one workflow behavior with its static type.
type Behavior struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Deviation records one deliberate legacy departure with its reason.
type Deviation struct {
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

// PromotionClosure is the complete promotion input.
type PromotionClosure struct {
	ExploratoryDigest string                              `json:"exploratory_digest"`
	GraphDigest       string                              `json:"graph_digest"`
	Schemas           []SchemaRef                         `json:"schemas"`
	Capabilities      map[string]string                   `json:"capabilities"`
	Behaviors         []Behavior                          `json:"behaviors"`
	LegacyTouched     bool                                `json:"legacy_touched"`
	Deviations        []Deviation                         `json:"deviations"`
	Owner             string                              `json:"owner"`
	ReviewExpiry      string                              `json:"review_expiry"`
	SafeDefault       string                              `json:"safe_default"`
	Fixtures          []workflowdecisions.NegativeFixture `json:"fixtures"`
}

// FixtureResult binds one executed negative fixture to its outcome.
type FixtureResult struct {
	Action  string `json:"action"`
	Outcome string `json:"outcome"`
}

// PromotionReceipt is the signed promotion record.
type PromotionReceipt struct {
	ExploratoryDigest string            `json:"exploratory_digest"`
	GraphDigest       string            `json:"graph_digest"`
	Schemas           []SchemaRef       `json:"schemas"`
	Capabilities      map[string]string `json:"capabilities"`
	Behaviors         []Behavior        `json:"behaviors"`
	Deviations        []Deviation       `json:"deviations"`
	Owner             string            `json:"owner"`
	ReviewExpiry      string            `json:"review_expiry"`
	FixtureResults    []FixtureResult   `json:"fixture_results"`
	State             string            `json:"state"`
	KeyID             string            `json:"key_id"`
	Digest            string            `json:"digest"`
	Signature         string            `json:"signature"`
}

// ClosureFinding is one exact promotion gap.
type ClosureFinding struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// PromotionError carries the complete closure set. The caller publishes
// nothing when this error is returned.
type PromotionError struct {
	Findings []ClosureFinding `json:"findings"`
}

func (e *PromotionError) Error() string {
	codes := make([]string, 0, len(e.Findings))
	for _, finding := range e.Findings {
		codes = append(codes, finding.Code)
	}
	return "workflowpromotion: open closure set: " + strings.Join(codes, ", ")
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

var floatingVersions = map[string]bool{
	"": true, "latest": true, "*": true, "main": true, "master": true,
}

// Promote validates the closure at the given today date (YYYY-MM-DD) and, on
// success, returns the signed CONTRACTED receipt. Any gap returns a
// PromotionError carrying every missing item and no receipt.
func Promote(closure PromotionClosure, priv ed25519.PrivateKey, today string) (PromotionReceipt, error) {
	findings := checkClosure(closure, today)
	if len(findings) > 0 {
		return PromotionReceipt{}, &PromotionError{Findings: findings}
	}
	results := make([]FixtureResult, 0, len(closure.Fixtures))
	for _, fixture := range closure.Fixtures {
		results = append(results, FixtureResult{
			Action:  fixture.Action,
			Outcome: workflowdecisions.ApplyDefault(closure.SafeDefault, fixture),
		})
	}
	capabilities := make(map[string]string, len(closure.Capabilities))
	for name, version := range closure.Capabilities {
		capabilities[name] = version
	}
	receipt := PromotionReceipt{
		ExploratoryDigest: closure.ExploratoryDigest,
		GraphDigest:       closure.GraphDigest,
		Schemas:           append([]SchemaRef(nil), closure.Schemas...),
		Capabilities:      capabilities,
		Behaviors:         append([]Behavior(nil), closure.Behaviors...),
		Deviations:        append([]Deviation(nil), closure.Deviations...),
		Owner:             closure.Owner,
		ReviewExpiry:      closure.ReviewExpiry,
		FixtureResults:    results,
		State:             ContractedState,
		KeyID:             keyID(priv),
	}
	digest, err := receiptDigest(receipt)
	if err != nil {
		return PromotionReceipt{}, fmt.Errorf("workflowpromotion: digest receipt: %w", err)
	}
	receipt.Digest = digest
	signature, err := provenance.SignDigest(priv, strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		return PromotionReceipt{}, fmt.Errorf("workflowpromotion: sign receipt: %w", err)
	}
	receipt.Signature = signature
	return receipt, nil
}

// VerifyReceipt recomputes the receipt digest from its own fields, verifies
// the ed25519 signature and enforces the review expiry at today.
func VerifyReceipt(receipt PromotionReceipt, pubHex, today string) (bool, string) {
	if receipt.State != ContractedState {
		return false, "receipt asserts state " + receipt.State + ", want CONTRACTED"
	}
	if receipt.Signature == "" {
		return false, "receipt carries no signature"
	}
	digest, err := receiptDigest(receipt)
	if err != nil {
		return false, "receipt digest failed: " + err.Error()
	}
	if digest != receipt.Digest {
		return false, "receipt content does not match its digest"
	}
	valid, err := provenance.VerifyDigestSignature(pubHex, strings.TrimPrefix(receipt.Digest, "sha256:"), receipt.Signature)
	if err != nil {
		return false, "signature check failed: " + err.Error()
	}
	if !valid {
		return false, "signature does not verify under the given key"
	}
	if receipt.ReviewExpiry < today {
		return false, "review expired on " + receipt.ReviewExpiry
	}
	return true, ""
}

func checkClosure(closure PromotionClosure, today string) []ClosureFinding {
	var findings []ClosureFinding
	add := func(code, field, detail string) {
		findings = append(findings, ClosureFinding{Code: code, Field: field, Detail: detail})
	}
	if !digestPattern.MatchString(closure.ExploratoryDigest) {
		add("MISSING_EXPLORATORY_DIGEST", "exploratory_digest", "promotion needs the exact exploratory digest")
	}
	if !digestPattern.MatchString(closure.GraphDigest) {
		add("MISSING_GRAPH_DIGEST", "graph_digest", "promotion needs the exact workflow graph digest")
	}
	if len(closure.Schemas) == 0 {
		add("UNRESOLVED_SCHEMA", "schemas", "promotion names no compiled schemas")
	}
	for _, schema := range closure.Schemas {
		if strings.TrimSpace(schema.Name) == "" || !digestPattern.MatchString(schema.Digest) {
			add("UNRESOLVED_SCHEMA", "schemas", "schema "+schema.Name+" carries no compiled digest")
		}
	}
	if len(closure.Capabilities) == 0 {
		add("UNVERSIONED_CAPABILITY", "capabilities", "promotion versions no capabilities")
	}
	names := make([]string, 0, len(closure.Capabilities))
	for name := range closure.Capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if floatingVersions[closure.Capabilities[name]] {
			add("UNVERSIONED_CAPABILITY", "capabilities", "capability "+name+" floats instead of pinning a version")
		}
	}
	if len(closure.Behaviors) == 0 {
		add("UNTYPED_BEHAVIOR", "behaviors", "promotion declares no typed behaviors")
	}
	for _, behavior := range closure.Behaviors {
		if strings.TrimSpace(behavior.Type) == "" || behavior.Type == "UNKNOWN" {
			add("UNTYPED_BEHAVIOR", "behaviors", "behavior "+behavior.Name+" carries no static type")
		}
	}
	if closure.LegacyTouched && len(closure.Deviations) == 0 {
		add("MISSING_PARITY_RECORD", "deviations", "touched legacy has no parity or deviation record")
	}
	for _, deviation := range closure.Deviations {
		if strings.TrimSpace(deviation.Reason) == "" {
			add("UNSAFE_LEGACY_SHORTCUT", "deviations", "deviation "+deviation.Description+" states no reason")
		}
	}
	if strings.TrimSpace(closure.Owner) == "" {
		add("MISSING_OWNER", "owner", "promotion names no owner")
	}
	if strings.TrimSpace(closure.ReviewExpiry) == "" {
		add("INVALID_EXPIRY", "review_expiry", "promotion names no review expiry")
	} else if !validDate(closure.ReviewExpiry) {
		add("INVALID_EXPIRY", "review_expiry", "review expiry must use YYYY-MM-DD")
	} else if closure.ReviewExpiry < today {
		add("EXPIRED_REVIEW", "review_expiry", "review expired before promotion")
	}
	if len(closure.Fixtures) == 0 {
		add("MISSING_NEGATIVE_FIXTURES", "fixtures", "promotion proves no negative fixture")
	}
	for _, fixture := range closure.Fixtures {
		if workflowdecisions.ApplyDefault(closure.SafeDefault, fixture) != fixture.Expect {
			add("UNSAFE_FIXTURE", "fixtures", "negative fixture "+fixture.Action+" does not prove the safe default")
		}
	}
	return findings
}

func validDate(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	for index, char := range value {
		switch index {
		case 4, 7:
			if char != '-' {
				return false
			}
		default:
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func keyID(priv ed25519.PrivateKey) string {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) < 8 {
		return ""
	}
	return hex.EncodeToString(pub[:8])
}

func receiptDigest(receipt PromotionReceipt) (string, error) {
	projection := struct {
		ExploratoryDigest string            `json:"exploratory_digest"`
		GraphDigest       string            `json:"graph_digest"`
		Schemas           []SchemaRef       `json:"schemas"`
		Capabilities      map[string]string `json:"capabilities"`
		Behaviors         []Behavior        `json:"behaviors"`
		Deviations        []Deviation       `json:"deviations"`
		Owner             string            `json:"owner"`
		ReviewExpiry      string            `json:"review_expiry"`
		FixtureResults    []FixtureResult   `json:"fixture_results"`
		State             string            `json:"state"`
		KeyID             string            `json:"key_id"`
	}{
		ExploratoryDigest: receipt.ExploratoryDigest,
		GraphDigest:       receipt.GraphDigest,
		Schemas:           receipt.Schemas,
		Capabilities:      receipt.Capabilities,
		Behaviors:         receipt.Behaviors,
		Deviations:        receipt.Deviations,
		Owner:             receipt.Owner,
		ReviewExpiry:      receipt.ReviewExpiry,
		FixtureResults:    receipt.FixtureResults,
		State:             receipt.State,
		KeyID:             receipt.KeyID,
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
