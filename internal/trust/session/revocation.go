package session

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const CodeSessionRevoked = "SESSION_REVOKED"

var ErrSessionRevoked = errors.New("session: session has been revoked")

// RevocationChecker is the small control port a workflow driver consults at
// every advancement boundary. Implementations must read the current
// server-side session state; a previously returned Record is not sufficient.
// checkedAt is supplied by the boundary so expiry and evidence use the same
// instant as the operation being guarded.
type RevocationChecker interface {
	CheckRevocation(context.Context, string, time.Time) error
}

// RevocationError is the typed refusal returned when the named session is
// specifically revoked. It preserves the package's ordinary inactive-session
// sentinel while exposing the stable code a workflow boundary records.
type RevocationError struct {
	SessionRef string
	Status     Status
}

func (e *RevocationError) Error() string {
	return fmt.Sprintf("session: %s for %s", CodeSessionRevoked, e.SessionRef)
}

func (e *RevocationError) Unwrap() []error {
	return []error{ErrSessionRevoked, ErrSessionNotActive}
}

func (e *RevocationError) Code() string { return CodeSessionRevoked }

// CodeOf returns the stable revocation code carried by err, or an empty
// string when err is not a typed session-revocation refusal.
func CodeOf(err error) string {
	var typed *RevocationError
	if errors.As(err, &typed) {
		return typed.Code()
	}
	return ""
}

// CheckRevocation implements [RevocationChecker] for the in-memory manager.
// The explicit boundary instant is used for timeout evaluation instead of
// allowing a driver to accidentally check a stale Record snapshot.
func (m *Manager) CheckRevocation(_ context.Context, sessionRef string, checkedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.sessions[ID(sessionRef)]
	if !ok {
		return ErrSessionNotFound
	}
	at := checkedAt.UTC()
	if checkedAt.IsZero() {
		at = m.now().UTC()
	}
	if rec.status == StatusActive {
		if next, reason, expired := EvaluateExpiry(rec.status, rec.lastActivityAt, rec.absoluteExpiresAt, rec.idleTimeout, at); expired {
			rec.status = next
			rec.revokedReason = reason
			m.recordEvidenceLocked(rec.id, EvidenceExpired, reason, at)
		}
	}
	if rec.status == StatusRevoked {
		return &RevocationError{SessionRef: sessionRef, Status: rec.status}
	}
	if rec.status != StatusActive {
		return fmt.Errorf("%w: %s", ErrSessionNotActive, rec.status)
	}
	return nil
}

// CheckRevocation implements [RevocationChecker] for the durable manager.
// Store.Get performs the read and any required expiry transition atomically,
// so every replica observes a committed revoke before this returns success.
func (m *PersistentManager) CheckRevocation(ctx context.Context, sessionRef string, checkedAt time.Time) error {
	at := checkedAt.UTC()
	if checkedAt.IsZero() {
		at = m.now().UTC()
	}
	rec, err := m.store.Get(ctx, ID(sessionRef), at)
	if err != nil {
		return err
	}
	if rec.Status == StatusRevoked {
		return &RevocationError{SessionRef: sessionRef, Status: rec.Status}
	}
	if rec.Status != StatusActive {
		return fmt.Errorf("%w: %s", ErrSessionNotActive, rec.Status)
	}
	return nil
}

var _ RevocationChecker = (*Manager)(nil)
var _ RevocationChecker = (*PersistentManager)(nil)
