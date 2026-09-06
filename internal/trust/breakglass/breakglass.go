// Package breakglass implements a bounded emergency-access grant. Opening a
// grant requires an incident reference, justification, a distinct approver,
// named capabilities, and a finite TTL. Use records evidence, expiry causes
// automatic containment that lists every revoked capability, and any used
// grant must receive a distinct post-use review.
package breakglass

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// HardTTL is the maximum lifetime of a break-glass grant. There is no API
// path for a non-expiring or standing grant.
const HardTTL = time.Hour

// MaxTTL is an alias that makes the hard bound explicit to callers.
const MaxTTL = HardTTL

var (
	ErrInvalidRequest        = errors.New("breakglass: invalid grant request")
	ErrApproverIsUser        = errors.New("breakglass: approver must be distinct from user")
	ErrTTLRequired           = errors.New("breakglass: a positive bounded ttl is required")
	ErrTTLExceedsHardLimit   = errors.New("breakglass: ttl exceeds hard limit")
	ErrGrantExpired          = errors.New("breakglass: grant expired and was contained")
	ErrGrantContained        = errors.New("breakglass: grant is contained")
	ErrCapabilityNotGranted  = errors.New("breakglass: capability is not granted")
	ErrReviewRequired        = errors.New("breakglass: post-use review is required")
	ErrReviewAlreadyRecorded = errors.New("breakglass: post-use review already recorded")
	ErrReviewerIsUser        = errors.New("breakglass: reviewer must be distinct from user")
	ErrNoUseToReview         = errors.New("breakglass: no use is awaiting review")
)

// Request describes the narrow emergency authority being requested.
type Request struct {
	User          string
	IncidentRef   string
	Justification string
	Capabilities  []string
	TTL           time.Duration
}

func (r Request) validate() error {
	for _, item := range []struct{ name, value string }{
		{"user", r.User}, {"incident reference", r.IncidentRef}, {"justification", r.Justification},
	} {
		if strings.TrimSpace(item.value) == "" || strings.TrimSpace(item.value) != item.value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidRequest, item.name)
		}
	}
	if len(r.Capabilities) == 0 {
		return fmt.Errorf("%w: at least one capability is required", ErrInvalidRequest)
	}
	seen := make(map[string]struct{}, len(r.Capabilities))
	for _, capability := range r.Capabilities {
		if strings.TrimSpace(capability) == "" || strings.TrimSpace(capability) != capability || capability == "*" {
			return fmt.Errorf("%w: capability must be named and narrow", ErrInvalidRequest)
		}
		if _, duplicate := seen[capability]; duplicate {
			return fmt.Errorf("%w: duplicate capability %q", ErrInvalidRequest, capability)
		}
		seen[capability] = struct{}{}
	}
	if r.TTL <= 0 {
		return ErrTTLRequired
	}
	if r.TTL > HardTTL {
		return ErrTTLExceedsHardLimit
	}
	return nil
}

// Approval is the second-person authorization for a break-glass grant.
type Approval struct {
	Approver string
	At       time.Time
}

func (a Approval) validate(user string) error {
	if strings.TrimSpace(a.Approver) == "" || a.Approver != strings.TrimSpace(a.Approver) || a.At.IsZero() {
		return fmt.Errorf("%w: approval requires an approver and timestamp", ErrInvalidRequest)
	}
	if strings.EqualFold(a.Approver, user) {
		return ErrApproverIsUser
	}
	return nil
}

// EvidenceKind is the complete lifecycle vocabulary for a grant.
type EvidenceKind string

const (
	EvidenceOpened    EvidenceKind = "OPENED"
	EvidenceUsed      EvidenceKind = "USED"
	EvidenceContained EvidenceKind = "CONTAINED"
	EvidenceReviewed  EvidenceKind = "REVIEWED"
)

// Evidence is safe lifecycle evidence. It contains identifiers and action
// descriptions, never the emergency payload or any secret material.
type Evidence struct {
	Kind        EvidenceKind
	At          time.Time
	Actor       string
	Capability  string
	Detail      string
	RevokedCaps []string
}

// ReviewOutcome is the closed result vocabulary for mandatory post-use
// review.
type ReviewOutcome string

const (
	ReviewJustified ReviewOutcome = "JUSTIFIED"
	ReviewViolation ReviewOutcome = "VIOLATION"
)

// Review is the immutable post-use review record.
type Review struct {
	Reviewer      string
	Outcome       ReviewOutcome
	Justification string
	At            time.Time
}

// Containment records the automatic revocation of every capability in a
// grant. RevokedCapabilities is sorted and copied so the complete set cannot
// be hidden by mutating a caller-owned slice.
type Containment struct {
	At                  time.Time
	Actor               string
	RevokedCapabilities []string
}

// Grant is a bounded emergency grant. Its lifecycle state and evidence are
// protected because expiry, use, containment, and review may be called from
// different request paths concurrently.
type Grant struct {
	ID            string
	User          string
	IncidentRef   string
	Justification string
	Capabilities  []string
	Approver      string
	ApprovedAt    time.Time
	OpenedAt      time.Time
	ExpiresAt     time.Time

	mu        sync.Mutex
	contained *Containment
	used      bool
	review    *Review
	evidence  []Evidence
}

// Open creates and evidences a grant. It is the only constructor; there is no
// constructor for a standing grant or for an unapproved pending grant.
func Open(id string, request Request, approval Approval, now time.Time) (*Grant, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
		return nil, fmt.Errorf("%w: grant id is required", ErrInvalidRequest)
	}
	if err := request.validate(); err != nil {
		return nil, err
	}
	if err := approval.validate(request.User); err != nil {
		return nil, err
	}
	opened := now.UTC()
	if opened.IsZero() {
		return nil, fmt.Errorf("%w: opened time is required", ErrInvalidRequest)
	}
	caps := append([]string(nil), request.Capabilities...)
	sort.Strings(caps)
	g := &Grant{
		ID:            id,
		User:          request.User,
		IncidentRef:   request.IncidentRef,
		Justification: request.Justification,
		Capabilities:  caps,
		Approver:      approval.Approver,
		ApprovedAt:    approval.At.UTC(),
		OpenedAt:      opened,
		ExpiresAt:     opened.Add(request.TTL),
	}
	g.evidence = append(g.evidence, Evidence{
		Kind: EvidenceOpened, At: opened, Actor: approval.Approver,
		Detail: fmt.Sprintf("incident=%s ttl=%s", request.IncidentRef, request.TTL),
	})
	return g, nil
}

// activeLocked returns whether a grant is usable and lazily contains it at
// expiry. The mutex must be held.
func (g *Grant) activeLocked(now time.Time) bool {
	if g.contained != nil {
		return false
	}
	if !now.UTC().Before(g.ExpiresAt) {
		g.containLocked(now, "system")
		return false
	}
	return true
}

func (g *Grant) containLocked(now time.Time, actor string) {
	if g.contained != nil {
		return
	}
	caps := append([]string(nil), g.Capabilities...)
	sort.Strings(caps)
	containment := &Containment{At: now.UTC(), Actor: actor, RevokedCapabilities: caps}
	g.contained = containment
	g.evidence = append(g.evidence, Evidence{
		Kind: EvidenceContained, At: containment.At, Actor: actor,
		Detail:      "automatic capability revocation",
		RevokedCaps: append([]string(nil), caps...),
	})
}

// IsActive reports whether the grant can still be used. An expired grant is
// contained as part of this call, including a complete capability list.
func (g *Grant) IsActive(now time.Time) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.activeLocked(now)
}

// Use records a capability use. It refuses unknown capabilities and inactive
// grants. A successful use marks the grant as requiring a post-use review.
func (g *Grant) Use(capability, action string, now time.Time) error {
	if g == nil {
		return ErrGrantContained
	}
	if strings.TrimSpace(capability) == "" || strings.TrimSpace(action) == "" {
		return fmt.Errorf("%w: capability and action are required", ErrInvalidRequest)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.activeLocked(now) {
		if now.UTC().Before(g.ExpiresAt) {
			return ErrGrantContained
		}
		return ErrGrantExpired
	}
	if !contains(g.Capabilities, capability) {
		return ErrCapabilityNotGranted
	}
	g.used = true
	g.evidence = append(g.evidence, Evidence{Kind: EvidenceUsed, At: now.UTC(), Actor: g.User, Capability: capability, Detail: action})
	return nil
}

// Contain explicitly contains a grant. Expiry uses the same path with the
// system actor. It is idempotent and always leaves the complete revoked list
// visible through Containment and Evidence.
func (g *Grant) Contain(by string, now time.Time) error {
	if g == nil {
		return ErrGrantContained
	}
	if strings.TrimSpace(by) == "" {
		return fmt.Errorf("%w: containment actor is required", ErrInvalidRequest)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.contained != nil {
		return nil
	}
	g.containLocked(now, by)
	return nil
}

// Containment returns a copy of the automatic or explicit containment record.
func (g *Grant) Containment() (Containment, bool) {
	if g == nil {
		return Containment{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.contained == nil {
		return Containment{}, false
	}
	copyRecord := *g.contained
	copyRecord.RevokedCapabilities = append([]string(nil), g.contained.RevokedCapabilities...)
	return copyRecord, true
}

// PostUseReview records the mandatory review after one or more uses. The
// reviewer must not be the user, and only the closed outcomes are accepted.
func (g *Grant) PostUseReview(reviewer string, outcome ReviewOutcome, justification string, now time.Time) error {
	if g == nil {
		return ErrNoUseToReview
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(justification) == "" || strings.TrimSpace(reviewer) != reviewer || strings.TrimSpace(justification) != justification {
		return fmt.Errorf("%w: reviewer and justification are required", ErrInvalidRequest)
	}
	if strings.EqualFold(reviewer, g.User) {
		return ErrReviewerIsUser
	}
	if outcome != ReviewJustified && outcome != ReviewViolation {
		return fmt.Errorf("%w: unknown review outcome %q", ErrInvalidRequest, outcome)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.used {
		return ErrNoUseToReview
	}
	if g.review != nil {
		return ErrReviewAlreadyRecorded
	}
	review := &Review{Reviewer: reviewer, Outcome: outcome, Justification: justification, At: now.UTC()}
	g.review = review
	g.evidence = append(g.evidence, Evidence{Kind: EvidenceReviewed, At: review.At, Actor: reviewer, Detail: string(outcome) + ": " + justification})
	return nil
}

// Review returns the post-use review, if one has been recorded.
func (g *Grant) Review() (Review, bool) {
	if g == nil {
		return Review{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.review == nil {
		return Review{}, false
	}
	return *g.review, true
}

// Evidence returns a copy of the append-only lifecycle evidence.
func (g *Grant) Evidence() []Evidence {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]Evidence, len(g.evidence))
	for i, evidence := range g.evidence {
		result[i] = evidence
		result[i].RevokedCaps = append([]string(nil), evidence.RevokedCaps...)
	}
	return result
}

// ReviewRequired reports whether a used grant still lacks its mandatory
// post-use review.
func (g *Grant) ReviewRequired() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used && g.review == nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
