// Package leave owns the intent-only leave.request contract.
//
// This package deliberately stops at request composition. It does not resolve
// eligibility, balances, legal context, or any other current truth, and it
// performs no persistence or external effect.
package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	// RequestLeaveIntentType is the stable catalog identity for leave.request.
	RequestLeaveIntentType = "hcmnext.workforce.leave_request"
	// RequestLeaveIntentVersion is the first version of the typed contract.
	RequestLeaveIntentVersion = 1
)

var (
	ErrInvalidRequest = errors.New("leave: invalid request")
	ErrForbiddenField = errors.New("leave: forbidden caller field")
	ErrMissingContext = errors.New("leave: trusted context is required")
)

var workerIDPattern = regexp.MustCompile(`^worker:[a-z0-9-]{1,64}$`)
var revisionPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)
var clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

// Mode is the caller's requested scheduling shape. It is not an eligibility
// or entitlement decision.
type Mode string

const (
	ModeContinuous      Mode = "CONTINUOUS"
	ModeIntermittent    Mode = "INTERMITTENT"
	ModeReducedSchedule Mode = "REDUCED_SCHEDULE"
)

// RequestLeave is the complete caller-controlled payload. Derived truth and
// trusted context intentionally have no fields in this type.
type RequestLeave struct {
	WorkerID               string
	LeaveType              string
	Interval               values.EffectiveInterval
	Mode                   Mode
	Reason                 string
	ExpectedWorkerRevision string
	ClientRequestID        string
	EvidenceRefs           []string
}

// TrustedContext is supplied by the authenticated boundary, never selected by
// a caller. It is carried separately from RequestLeave for this reason.
type TrustedContext struct {
	TenantID            values.TenantId
	OrganizationScopeID string
	PrincipalID         string
}

// ChildKind identifies the bounded child-intent vocabulary. The request does
// not claim that any child is eligible or executable; composition resolves that
// later from server-side truth.
type ChildKind string

const (
	ChildLeave        ChildKind = "leave"
	ChildAvailability ChildKind = "availability"
	ChildBalance      ChildKind = "balance"
	ChildPayroll      ChildKind = "payroll"
	ChildBenefits     ChildKind = "benefits"
	ChildSchedule     ChildKind = "schedule"
	ChildAccess       ChildKind = "access"
	ChildReturn       ChildKind = "return"
)

var childKinds = []ChildKind{ChildLeave, ChildAvailability, ChildBalance, ChildPayroll, ChildBenefits, ChildSchedule, ChildAccess, ChildReturn}

// ProcessRequest is the intent-kernel view of a leave request. Process is a
// definition attribute: the kernel family remains CHANGE_REQUEST and writes
// are carried only by explicitly bound child intents.
type ProcessRequest struct {
	DefinitionType    string
	DefinitionVersion int
	Family            intent.Family
	ChildKinds        []ChildKind
	Request           RequestLeave
	Context           TrustedContext
	CanonicalDigest   string
}

// Validate checks caller-controlled semantics and rejects missing required
// values without attempting to answer any domain question.
func (r RequestLeave) Validate() error {
	switch {
	case !workerIDPattern.MatchString(r.WorkerID):
		return fmt.Errorf("%w: worker_id", ErrInvalidRequest)
	case strings.TrimSpace(r.LeaveType) == "":
		return fmt.Errorf("%w: leave_type", ErrInvalidRequest)
	case r.Interval.Validate() != nil:
		return fmt.Errorf("%w: interval: %w", ErrInvalidRequest, r.Interval.Validate())
	case r.Mode != ModeContinuous && r.Mode != ModeIntermittent && r.Mode != ModeReducedSchedule:
		return fmt.Errorf("%w: mode", ErrInvalidRequest)
	case strings.TrimSpace(r.Reason) == "":
		return fmt.Errorf("%w: reason", ErrInvalidRequest)
	case !revisionPattern.MatchString(r.ExpectedWorkerRevision):
		return fmt.Errorf("%w: expected_worker_revision", ErrInvalidRequest)
	case !clientIDPattern.MatchString(r.ClientRequestID):
		return fmt.Errorf("%w: client_request_id", ErrInvalidRequest)
	}
	seen := make(map[string]struct{}, len(r.EvidenceRefs))
	for _, ref := range r.EvidenceRefs {
		if strings.TrimSpace(ref) == "" || !strings.HasPrefix(ref, "evidence:") {
			return fmt.Errorf("%w: evidence_ref", ErrInvalidRequest)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: duplicate evidence_ref", ErrInvalidRequest)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

// ValidateRawFields is used by adapters before decoding. It makes forbidden
// derived/trusted assertions fail closed instead of being silently ignored.
func ValidateRawFields(fields map[string]string) error {
	allowed := map[string]bool{"worker_id": true, "leave_type": true, "mode": true, "reason": true, "expected_worker_revision": true, "client_request_id": true, "evidence_ref": true}
	for field := range fields {
		if !allowed[field] {
			return fmt.Errorf("%w: %s", ErrForbiddenField, field)
		}
	}
	return nil
}

// Bind validates and injects trusted context, then returns the child-bound
// ProcessRequest. No state, eligibility, entitlement, or effect is produced.
func Bind(r RequestLeave, c TrustedContext) (ProcessRequest, error) {
	if err := r.Validate(); err != nil {
		return ProcessRequest{}, err
	}
	if err := c.TenantID.Validate(); err != nil || strings.TrimSpace(c.OrganizationScopeID) == "" || strings.TrimSpace(c.PrincipalID) == "" {
		return ProcessRequest{}, ErrMissingContext
	}
	digest := sha256.Sum256(canonical(r, c))
	children := append([]ChildKind(nil), childKinds...)
	return ProcessRequest{Request: r, Context: c, DefinitionType: RequestLeaveIntentType, DefinitionVersion: RequestLeaveIntentVersion, Family: intent.FamilyChangeRequest, ChildKinds: children, CanonicalDigest: hex.EncodeToString(digest[:])}, nil
}

func canonical(r RequestLeave, c TrustedContext) []byte {
	evidence := append([]string(nil), r.EvidenceRefs...)
	sort.Strings(evidence)
	return []byte(strings.Join([]string{r.WorkerID, r.LeaveType, string(r.Interval.Canonical()), string(r.Mode), r.Reason, r.ExpectedWorkerRevision, r.ClientRequestID, strings.Join(evidence, ","), string(c.TenantID), c.OrganizationScopeID, c.PrincipalID}, "\x1f"))
}
