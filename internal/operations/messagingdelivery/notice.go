package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Notice requirement outcomes: the closed assessment vocabulary.
const (
	NoticeSatisfied      = "SATISFIED"
	NoticeUnsatisfied    = "UNSATISFIED"
	NoticeDisputed       = "DISPUTED"
	NoticeRepairRequired = "REPAIR_REQUIRED"
)

// NoticeRequirement is one legal-notice delivery requirement. Notification,
// acknowledgement and legal signature stay distinct: each binds its own
// proof, and provider delivery alone is never legal evidence.
type NoticeRequirement struct {
	ID                string
	Recipient         string
	RecipientVerified bool
	RecipientProof    string
	ContentDigest     string
	ContentVersion    string
	Timestamp         int64
	JurisdictionRule  string
	AckProof          string
	SignatureDigest   string
	DisputeProof      string
}

// NoticeAssessment is the immutable assessment with its evidence package.
type NoticeAssessment struct {
	RequirementID string
	Outcome       string
	Gaps          []string
	Evidence      []string
	Digest        string
}

func noticeDigest(requirement NoticeRequirement, outcome string, gaps []string) string {
	sorted := append([]string(nil), gaps...)
	sort.Strings(sorted)
	parts := []string{
		"legal-notice", requirement.ID, requirement.Recipient,
		fmt.Sprint(requirement.RecipientVerified), requirement.RecipientProof,
		requirement.ContentDigest, requirement.ContentVersion,
		fmt.Sprint(requirement.Timestamp), requirement.JurisdictionRule,
		requirement.AckProof, requirement.SignatureDigest, requirement.DisputeProof,
		outcome,
	}
	parts = append(parts, sorted...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AssessRequirement evaluates one legal-notice requirement. A verified
// recipient, exact content bound to its version, timestamp,
// acknowledgement, signature and jurisdiction rule must all bind before
// SATISFIED; a proven dispute reports DISPUTED; repairable gaps report
// REPAIR_REQUIRED; anything else is UNSATISFIED.
func AssessRequirement(requirement NoticeRequirement, knownRules map[string]bool) (NoticeAssessment, error) {
	return AssessContentRequirement(requirement, knownRules, nil)
}

// AssessContentRequirement evaluates against a content registry binding
// digests to versions. A version that does not match the registered
// version for its digest is a stale-content gap, never satisfied.
func AssessContentRequirement(requirement NoticeRequirement, knownRules map[string]bool, contentVersions map[string]string) (NoticeAssessment, error) {
	if strings.TrimSpace(requirement.ID) == "" || strings.TrimSpace(requirement.Recipient) == "" {
		return NoticeAssessment{}, fmt.Errorf("messagingdelivery: notice requirement needs an id and a recipient")
	}
	assessment := NoticeAssessment{RequirementID: requirement.ID}
	gap := func(name string) {
		assessment.Gaps = append(assessment.Gaps, name)
	}
	if !requirement.RecipientVerified || strings.TrimSpace(requirement.RecipientProof) == "" {
		gap("recipient-unverified")
	}
	if strings.TrimSpace(requirement.ContentDigest) == "" || strings.TrimSpace(requirement.ContentVersion) == "" {
		gap("content-unbound")
	} else if want, ok := contentVersions[requirement.ContentDigest]; ok && want != requirement.ContentVersion {
		gap("content-version-mismatch")
	}
	if requirement.Timestamp <= 0 {
		gap("timestamp-missing")
	}
	if strings.TrimSpace(requirement.JurisdictionRule) == "" || !knownRules[requirement.JurisdictionRule] {
		gap("jurisdiction-rule-unknown")
	}
	repairable := false
	if strings.TrimSpace(requirement.AckProof) == "" {
		gap("ack-missing")
		repairable = true
	}
	if strings.TrimSpace(requirement.SignatureDigest) == "" {
		gap("signature-missing")
	}
	switch {
	case strings.TrimSpace(requirement.DisputeProof) != "":
		assessment.Outcome = NoticeDisputed
	case len(assessment.Gaps) == 0:
		assessment.Outcome = NoticeSatisfied
	case repairable && len(assessment.Gaps) == 1:
		assessment.Outcome = NoticeRepairRequired
	default:
		assessment.Outcome = NoticeUnsatisfied
	}
	for _, element := range []string{"recipient:" + requirement.Recipient, "content:" + requirement.ContentDigest, "rule:" + requirement.JurisdictionRule, "ack:" + requirement.AckProof, "signature:" + requirement.SignatureDigest} {
		if !strings.HasSuffix(element, ":") {
			assessment.Evidence = append(assessment.Evidence, element)
		}
	}
	sort.Strings(assessment.Evidence)
	assessment.Digest = noticeDigest(requirement, assessment.Outcome, assessment.Gaps)
	return assessment, nil
}

// Verify recomputes the assessment seal.
func (assessment NoticeAssessment) Verify(requirement NoticeRequirement) error {
	if assessment.Digest == "" || noticeDigest(requirement, assessment.Outcome, assessment.Gaps) != assessment.Digest {
		return fmt.Errorf("messagingdelivery: notice assessment seal is broken")
	}
	return nil
}

// RequirementRegistry guards requirements for concurrent assessment.
type RequirementRegistry struct {
	mu           sync.Mutex
	requirements map[string]NoticeRequirement
	rules        map[string]bool
}

// NewRequirementRegistry starts a registry with known jurisdiction rules.
func NewRequirementRegistry(rules []string) *RequirementRegistry {
	known := make(map[string]bool, len(rules))
	for _, rule := range rules {
		known[rule] = true
	}
	return &RequirementRegistry{requirements: make(map[string]NoticeRequirement), rules: known}
}

// Register publishes one requirement. Duplicates refuse.
func (registry *RequirementRegistry) Register(requirement NoticeRequirement) error {
	if registry == nil {
		return fmt.Errorf("messagingdelivery: nil requirement registry")
	}
	if strings.TrimSpace(requirement.ID) == "" {
		return fmt.Errorf("messagingdelivery: requirement id is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, dup := registry.requirements[requirement.ID]; dup {
		return fmt.Errorf("messagingdelivery: requirement %s is already registered", requirement.ID)
	}
	registry.requirements[requirement.ID] = requirement
	return nil
}

// Assess evaluates one registered requirement.
func (registry *RequirementRegistry) Assess(id string) (NoticeAssessment, error) {
	if registry == nil {
		return NoticeAssessment{}, fmt.Errorf("messagingdelivery: nil requirement registry")
	}
	registry.mu.Lock()
	requirement, ok := registry.requirements[id]
	rules := make(map[string]bool, len(registry.rules))
	for rule := range registry.rules {
		rules[rule] = true
	}
	registry.mu.Unlock()
	if !ok {
		return NoticeAssessment{}, fmt.Errorf("messagingdelivery: unknown requirement %s", id)
	}
	return AssessRequirement(requirement, rules)
}
