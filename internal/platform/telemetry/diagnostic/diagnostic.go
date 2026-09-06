// Package diagnostic owns the governed runtime diagnostic-elevation contract.
// Elevation is a configuration snapshot, not a package-global log switch.
package diagnostic

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

func Explain() string { return "scoped expiring governed diagnostic elevation" }

type Level uint8

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
	LevelDebug
)

func (l Level) valid() bool { return l <= LevelDebug }

type Request struct {
	Revision     uint64
	Actor        string
	Approver     string
	Purpose      string
	Scope        string
	Level        Level
	VolumeBudget int
	StartsAt     time.Time
	ExpiresAt    time.Time
	Signature    string
}

type Snapshot struct {
	Revision     uint64
	Actor        string
	Purpose      string
	Scope        string
	Level        Level
	VolumeBudget int
	StartsAt     time.Time
	ExpiresAt    time.Time
	Digest       string
}

type Decision struct {
	Allowed  bool
	Reason   string
	Revision uint64
	Digest   string
}

type Controller struct {
	mu       sync.RWMutex
	snapshot Snapshot
	used     int
}

var (
	ErrInvalidRequest = errors.New("diagnostic: invalid elevation request")
	ErrStaleRevision  = errors.New("diagnostic: stale configuration revision")
)

func Validate(request Request) error {
	if request.Revision == 0 || strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.Approver) == "" || request.Actor == request.Approver || strings.TrimSpace(request.Purpose) == "" || !validScope(request.Scope) || !request.Level.valid() || request.VolumeBudget <= 0 || request.StartsAt.IsZero() || request.ExpiresAt.IsZero() || !request.StartsAt.Before(request.ExpiresAt) || request.ExpiresAt.Sub(request.StartsAt) > time.Hour || strings.TrimSpace(request.Signature) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func validScope(scope string) bool {
	return (strings.HasPrefix(scope, "tenant:") || strings.HasPrefix(scope, "correlation:")) && !strings.ContainsAny(scope, " \t\r\n") && !strings.Contains(scope, "*")
}

func Digest(request Request) string {
	payload := fmt.Sprintf("diagnostic.v%d\x00%d\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s", schemaVersion, request.Revision, request.Actor, request.Approver, request.Purpose, request.Scope, request.Level, request.StartsAt.UTC().Format(time.RFC3339Nano), request.ExpiresAt.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func NewController() *Controller { return &Controller{} }

func (c *Controller) Apply(request Request) (Snapshot, error) {
	if err := Validate(request); err != nil {
		return Snapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.Revision <= c.snapshot.Revision {
		return Snapshot{}, ErrStaleRevision
	}
	snapshot := Snapshot{Revision: request.Revision, Actor: request.Actor, Purpose: request.Purpose, Scope: request.Scope, Level: request.Level, VolumeBudget: request.VolumeBudget, StartsAt: request.StartsAt.UTC(), ExpiresAt: request.ExpiresAt.UTC(), Digest: Digest(request)}
	c.snapshot = snapshot
	c.used = 0
	return snapshot, nil
}

func (c *Controller) Decide(scope string, level Level, at time.Time) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snapshot
	decision := Decision{Revision: s.Revision, Digest: s.Digest}
	if s.Revision == 0 || at.Before(s.StartsAt) || !at.Before(s.ExpiresAt) {
		decision.Reason = "EXPIRED_OR_NOT_ACTIVE"
		return decision
	}
	if scope != s.Scope {
		decision.Reason = "SCOPE_MISMATCH"
		return decision
	}
	if level > s.Level {
		decision.Reason = "LEVEL_NOT_GRANTED"
		return decision
	}
	if c.used >= s.VolumeBudget {
		decision.Reason = "VOLUME_BUDGET_EXHAUSTED"
		return decision
	}
	c.used++
	decision.Allowed = true
	decision.Reason = "ALLOWED"
	return decision
}

func (s Snapshot) Explain() string {
	return fmt.Sprintf("diagnostic revision=%d scope=%s level=%d expires=%s", s.Revision, s.Scope, s.Level, s.ExpiresAt.UTC().Format(time.RFC3339Nano))
}
