package diagnosticsession

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RegistryID      = "ADMIN-006"
	ContractVersion = "hcmnext.admincenter.diagnostic-session/1"
	DefaultTTL      = 10 * time.Minute
	MaxTTL          = 15 * time.Minute
	PurposeSupport  = "support_diagnostics"
)

var (
	ErrInvalidScope = errors.New("diagnostic session: invalid JIT scope")
	ErrExpired      = errors.New("diagnostic session: expired")
	ErrActionDenied = errors.New("diagnostic session: UI action is not allowlisted")
)

// Scope is the complete JIT grant. An empty resource or action set is denied
// by New, preventing accidental conversion of a support session into a
// broad operator credential.
type Scope struct {
	TenantID  string
	SubjectID string
	CaseID    string
	Purpose   string
	Resources []string
	Actions   []UIAction
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Session is an immutable-at-the-boundary diagnostic session handle.
type Session struct{ scope Scope }

func New(scope Scope) (Session, error) {
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.SubjectID) == "" || strings.TrimSpace(scope.CaseID) == "" || scope.Purpose != PurposeSupport || len(scope.Resources) == 0 || len(scope.Actions) == 0 {
		return Session{}, ErrInvalidScope
	}
	if scope.IssuedAt.IsZero() {
		scope.IssuedAt = time.Now().UTC()
	}
	if scope.ExpiresAt.IsZero() {
		scope.ExpiresAt = scope.IssuedAt.Add(DefaultTTL)
	}
	if !scope.ExpiresAt.After(scope.IssuedAt) || scope.ExpiresAt.Sub(scope.IssuedAt) > MaxTTL {
		return Session{}, fmt.Errorf("%w: TTL exceeds %s", ErrInvalidScope, MaxTTL)
	}
	resources := append([]string(nil), scope.Resources...)
	actions := append([]UIAction(nil), scope.Actions...)
	for _, r := range resources {
		if strings.TrimSpace(r) == "" {
			return Session{}, ErrInvalidScope
		}
	}
	for _, a := range actions {
		if !IsSafeUIAction(a) {
			return Session{}, fmt.Errorf("%w: %q", ErrActionDenied, a)
		}
	}
	scope.Resources, scope.Actions = resources, actions
	return Session{scope: scope}, nil
}

func (s Session) Scope() Scope {
	c := s.scope
	c.Resources = append([]string(nil), c.Resources...)
	c.Actions = append([]UIAction(nil), c.Actions...)
	return c
}
func (s Session) Expired(now time.Time) bool {
	return s.scope.ExpiresAt.IsZero() || !now.Before(s.scope.ExpiresAt)
}
func (s Session) Validate(now time.Time) error {
	if s.Expired(now) {
		return ErrExpired
	}
	return nil
}
func (s Session) AllowsResource(resource string) bool {
	for _, r := range s.scope.Resources {
		if r == resource {
			return true
		}
	}
	return false
}
func (s Session) AllowsAction(action UIAction) bool {
	for _, a := range s.scope.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// UIAction is intentionally a closed set of read-only support affordances.
type UIAction string

const (
	ActionViewSummary           UIAction = "view_summary"
	ActionViewTimeline          UIAction = "view_timeline"
	ActionViewEvidence          UIAction = "view_evidence"
	ActionCopyRedactedReference UIAction = "copy_redacted_reference"
)

func IsSafeUIAction(a UIAction) bool {
	switch a {
	case ActionViewSummary, ActionViewTimeline, ActionViewEvidence, ActionCopyRedactedReference:
		return true
	}
	return false
}
