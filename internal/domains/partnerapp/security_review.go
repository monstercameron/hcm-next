package partnerapp

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidSecurityReview = errors.New("partnerapp: invalid security and data-processing review")
	ErrReviewExpired         = errors.New("partnerapp: security and data-processing review has expired")
	ErrMandatoryReviewFailed = errors.New("partnerapp: mandatory review control did not pass")
)

// ReviewControl is the closed checklist vocabulary for application security
// and data processing.
type ReviewControl string

const (
	ControlDataMinimization   ReviewControl = "DATA_MINIMIZATION"
	ControlRetention          ReviewControl = "RETENTION"
	ControlSubProcessors      ReviewControl = "SUB_PROCESSORS"
	ControlBreachNotification ReviewControl = "BREACH_NOTIFICATION"
	ControlEncryptionTransit  ReviewControl = "ENCRYPTION_IN_TRANSIT"
	ControlEncryptionAtRest   ReviewControl = "ENCRYPTION_AT_REST"
)

const (
	ControlEncryptionInTransit = ControlEncryptionTransit
	ControlSubprocessors       = ControlSubProcessors
)

// ReviewFinding is the closed finding vocabulary. NOT_APPLICABLE remains an
// explicit finding and never silently becomes PASS for a mandatory control.
type ReviewFinding string

const (
	FindingPass          ReviewFinding = "PASS"
	FindingFail          ReviewFinding = "FAIL"
	FindingNotApplicable ReviewFinding = "NOT_APPLICABLE"
)

// ReviewItem is one control result with evidence. Evidence is reference-only;
// this package never retains a report, secret, or processor payload.
type ReviewItem struct {
	Control     ReviewControl
	Finding     ReviewFinding
	EvidenceRef string
}

// SecurityReviewRequest contains the review evidence and the declared
// validity period. All time decisions use caller-supplied instants.
type SecurityReviewRequest struct {
	Submitter          string
	OwnerRef           string
	Reviewer           string
	SignatureRef       string
	ReviewedAt         time.Time
	ValidFor           time.Duration
	DataClasses        []string
	ScopeRefs          []string
	DestinationRefs    []string
	ProcessorRefs      []string
	ResidencyRefs      []string
	RetentionPolicyRef string
	ThreatModelRef     string
	VulnerabilityRef   string
	Items              []ReviewItem
}

// DataProcessingReview is an immutable, signed review record for one
// application version. It is separate from ApplicationVersion so the APP-001
// revision shape remains compatible while APP-002 adds an explicit review
// gate.
type DataProcessingReview struct {
	ApplicationID      string
	Version            string
	Submitter          string
	OwnerRef           string
	Reviewer           string
	SignatureRef       string
	ReviewedAt         time.Time
	ExpiresAt          time.Time
	DataClasses        []string
	ScopeRefs          []string
	DestinationRefs    []string
	ProcessorRefs      []string
	ResidencyRefs      []string
	RetentionPolicyRef string
	ThreatModelRef     string
	VulnerabilityRef   string
	Items              []ReviewItem
	Digest             string
}

// SecurityReview and SecurityReviewItem are descriptive aliases for callers
// that use the security vocabulary.
type SecurityReview = DataProcessingReview
type SecurityReviewItem = ReviewItem

// NewSecurityReview creates a review bound to v. The period is declared by
// the caller and is frozen into ExpiresAt; no wall clock is consulted.
func NewSecurityReview(v ApplicationVersion, req SecurityReviewRequest) (DataProcessingReview, error) {
	if err := v.Verify(); err != nil {
		return DataProcessingReview{}, err
	}
	if req.ReviewedAt.IsZero() || req.ValidFor <= 0 {
		return DataProcessingReview{}, fmt.Errorf("%w: reviewed-at and a positive validity period are required", ErrInvalidSecurityReview)
	}
	r := DataProcessingReview{
		ApplicationID: v.ApplicationID, Version: v.Version, Submitter: req.Submitter, OwnerRef: req.OwnerRef, Reviewer: req.Reviewer,
		SignatureRef: req.SignatureRef, ReviewedAt: req.ReviewedAt.UTC(), ExpiresAt: req.ReviewedAt.UTC().Add(req.ValidFor),
		DataClasses: append([]string(nil), req.DataClasses...),
		ScopeRefs:   append([]string(nil), req.ScopeRefs...), DestinationRefs: append([]string(nil), req.DestinationRefs...),
		ProcessorRefs: append([]string(nil), req.ProcessorRefs...), ResidencyRefs: append([]string(nil), req.ResidencyRefs...),
		RetentionPolicyRef: req.RetentionPolicyRef, ThreatModelRef: req.ThreatModelRef, VulnerabilityRef: req.VulnerabilityRef,
		Items: append([]ReviewItem(nil), req.Items...),
	}
	if len(r.DataClasses) == 0 {
		r.DataClasses = append([]string(nil), v.DataClasses...)
	}
	if err := r.Validate(); err != nil {
		return DataProcessingReview{}, err
	}
	r.Digest = reviewDigest(r)
	return r, nil
}

// CreateSecurityReview is an alias for [NewSecurityReview].
func CreateSecurityReview(v ApplicationVersion, req SecurityReviewRequest) (DataProcessingReview, error) {
	return NewSecurityReview(v, req)
}

// Validate checks the review's closed controls, evidence, identity binding,
// declared processing scope and digest.
func (r DataProcessingReview) Validate() error {
	if strings.TrimSpace(r.ApplicationID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Submitter) == "" || strings.TrimSpace(r.OwnerRef) == "" || strings.TrimSpace(r.Reviewer) == "" || r.Submitter == r.Reviewer {
		return fmt.Errorf("%w: application/version, owner and distinct submitter/reviewer are required", ErrInvalidSecurityReview)
	}
	if strings.TrimSpace(r.SignatureRef) == "" || r.ReviewedAt.IsZero() || r.ExpiresAt.IsZero() || !r.ExpiresAt.After(r.ReviewedAt) {
		return fmt.Errorf("%w: signed review and a positive expiry are required", ErrInvalidSecurityReview)
	}
	if err := reviewRefs(r.DataClasses, "data class"); err != nil {
		return err
	}
	for name, refs := range map[string][]string{
		"scope": r.ScopeRefs, "destination": r.DestinationRefs, "processor": r.ProcessorRefs, "residency": r.ResidencyRefs,
	} {
		if err := reviewRefs(refs, name); err != nil {
			return err
		}
	}
	for name, ref := range map[string]string{
		"retention policy": r.RetentionPolicyRef, "threat model": r.ThreatModelRef, "vulnerability": r.VulnerabilityRef,
	} {
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(ref) != ref {
			return fmt.Errorf("%w: %s evidence is required", ErrInvalidSecurityReview, name)
		}
	}
	if len(r.Items) != len(mandatoryReviewControls()) {
		return fmt.Errorf("%w: exactly one item for each mandatory control is required", ErrInvalidSecurityReview)
	}
	seen := make(map[ReviewControl]struct{}, len(r.Items))
	for _, item := range r.Items {
		if !item.Control.valid() || !item.Finding.valid() || strings.TrimSpace(item.EvidenceRef) == "" || strings.TrimSpace(item.EvidenceRef) != item.EvidenceRef {
			return fmt.Errorf("%w: every control needs a closed finding and evidence ref", ErrInvalidSecurityReview)
		}
		if _, ok := seen[item.Control]; ok {
			return fmt.Errorf("%w: duplicate control %q", ErrInvalidSecurityReview, item.Control)
		}
		seen[item.Control] = struct{}{}
	}
	for _, control := range mandatoryReviewControls() {
		if _, ok := seen[control]; !ok {
			return fmt.Errorf("%w: missing control %q", ErrInvalidSecurityReview, control)
		}
	}
	if r.Digest != "" && r.Digest != reviewDigest(r) {
		return fmt.Errorf("%w: review digest does not match", ErrInvalidSecurityReview)
	}
	return nil
}

// Verify checks an already minted review, including its digest.
func (r DataProcessingReview) Verify() error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Digest == "" || r.Digest != reviewDigest(r) {
		return fmt.Errorf("%w: review digest is absent or changed", ErrInvalidSecurityReview)
	}
	return nil
}

// ValidAt reports whether r is still effective at now. Equality at expiry is
// expired, making a boundary decision deterministic.
func (r DataProcessingReview) ValidAt(now time.Time) error {
	if err := r.Verify(); err != nil {
		return err
	}
	if now.IsZero() || !now.Before(r.ExpiresAt) {
		return fmt.Errorf("%w: expires at %s", ErrReviewExpired, r.ExpiresAt.Format(time.RFC3339Nano))
	}
	return nil
}

// ActivateWithSecurityReview is the APP-002 activation gate. It refuses
// failed, incomplete, expired, or version-mismatched reviews before delegating
// to the immutable APP-001 lifecycle transition.
func (v ApplicationVersion) ActivateWithSecurityReview(r DataProcessingReview, now time.Time) (ApplicationVersion, error) {
	if r.ApplicationID != v.ApplicationID || r.Version != v.Version || !sameStringSet(r.DataClasses, v.DataClasses) {
		return ApplicationVersion{}, fmt.Errorf("%w: review is bound to %s/%s", ErrInvalidSecurityReview, v.ApplicationID, v.Version)
	}
	if err := r.ValidAt(now); err != nil {
		return ApplicationVersion{}, err
	}
	for _, item := range r.Items {
		if item.Finding != FindingPass {
			return ApplicationVersion{}, fmt.Errorf("%w: %s=%s", ErrMandatoryReviewFailed, item.Control, item.Finding)
		}
	}
	return v.Activate()
}

// ActivateReviewed is the functional form of [ApplicationVersion.ActivateWithSecurityReview].
func ActivateReviewed(v ApplicationVersion, r DataProcessingReview, now time.Time) (ApplicationVersion, error) {
	return v.ActivateWithSecurityReview(r, now)
}

// Explain returns a bounded review summary that names controls and expiry but
// does not disclose evidence content.
func (r DataProcessingReview) Explain() string {
	return fmt.Sprintf("partnerapp security review app=%s version=%s reviewer=%s controls=%d expires_at=%s digest=%s", r.ApplicationID, r.Version, r.Reviewer, len(r.Items), r.ExpiresAt.UTC().Format(time.RFC3339Nano), r.Digest)
}

func (c ReviewControl) valid() bool {
	for _, item := range mandatoryReviewControls() {
		if c == item {
			return true
		}
	}
	return false
}

func (f ReviewFinding) valid() bool {
	return f == FindingPass || f == FindingFail || f == FindingNotApplicable
}

func mandatoryReviewControls() []ReviewControl {
	return []ReviewControl{ControlDataMinimization, ControlRetention, ControlSubProcessors, ControlBreachNotification, ControlEncryptionTransit, ControlEncryptionAtRest}
}

func reviewRefs(refs []string, label string) error {
	if len(refs) == 0 {
		return fmt.Errorf("%w: at least one %s ref is required", ErrInvalidSecurityReview, label)
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(ref) != ref || ref == "*" {
			return fmt.Errorf("%w: %s ref %q is not exact", ErrInvalidSecurityReview, label, ref)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: duplicate %s ref %q", ErrInvalidSecurityReview, label, ref)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func reviewDigest(r DataProcessingReview) string {
	w := canonicalbytes.New("hcmnext.domains.partnerapp.DataProcessingReview", 1).
		String("application_id", r.ApplicationID).String("version", r.Version).
		String("submitter", r.Submitter).String("owner_ref", r.OwnerRef).String("reviewer", r.Reviewer).String("signature_ref", r.SignatureRef).
		String("reviewed_at", r.ReviewedAt.UTC().Format(time.RFC3339Nano)).String("expires_at", r.ExpiresAt.UTC().Format(time.RFC3339Nano)).
		SortedStrings("data_class", r.DataClasses).
		SortedStrings("scope_ref", r.ScopeRefs).SortedStrings("destination_ref", r.DestinationRefs).
		SortedStrings("processor_ref", r.ProcessorRefs).SortedStrings("residency_ref", r.ResidencyRefs).
		String("retention_policy_ref", r.RetentionPolicyRef).String("threat_model_ref", r.ThreatModelRef).String("vulnerability_ref", r.VulnerabilityRef)
	items := append([]ReviewItem(nil), r.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Control < items[j].Control })
	for _, item := range items {
		w.String("control", string(item.Control)).String("finding", string(item.Finding)).String("evidence_ref", item.EvidenceRef)
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a := append([]string(nil), left...)
	b := append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
