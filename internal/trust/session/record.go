package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ID is an opaque, server-generated session identifier. It is never derived
// from, or equal to, a bearer secret: presenting an ID proves nothing by
// itself.
type ID string

// RefreshToken is an opaque bearer secret. Presenting the current one for a
// session proves possession and rotates it; presenting any retired one is a
// replay. A RefreshToken's text is never logged: [Manager] and [Record] only
// ever retain its SHA-256 hash.
type RefreshToken string

// tokenEncoding is strict, unpadded base64url, matching the rest of the
// trust plane's token encoding so that a token has exactly one spelling.
var tokenEncoding = base64.RawURLEncoding.Strict()

// Status is a session's lifecycle state.
type Status uint8

// Session lifecycle states. StatusUnspecified is the zero value and is
// never a legal state on a constructed [Record].
const (
	StatusUnspecified Status = iota
	StatusActive
	StatusExpiredIdle
	StatusExpiredAbsolute
	StatusRevoked
)

var statusWire = map[Status]string{
	StatusActive:          "ACTIVE",
	StatusExpiredIdle:     "EXPIRED_IDLE",
	StatusExpiredAbsolute: "EXPIRED_ABSOLUTE",
	StatusRevoked:         "REVOKED",
}

// String returns the stable wire token.
func (s Status) String() string {
	if w, ok := statusWire[s]; ok {
		return w
	}
	return "STATUS_UNSPECIFIED"
}

// Valid reports whether s is a declared, non-zero status.
func (s Status) Valid() bool { _, ok := statusWire[s]; return ok }

// Live reports whether s is a status under which the session may still be
// used (only StatusActive). Every other status, including one this package
// adds in the future, is not live by default: a caller checking Live never
// needs updating when a new terminal status is added.
func (s Status) Live() bool { return s == StatusActive }

// Record is one immutable snapshot of a session. [Manager] is the only
// producer; every accessor returns a value or a copy, never a reference into
// [Manager]'s own state.
type Record struct {
	id                   ID
	tenant               values.TenantId
	subject              string
	principalFingerprint string
	assurance            trust.Assurance
	status               Status
	revokedReason        string
	createdAt            time.Time
	lastActivityAt       time.Time
	idleTimeout          time.Duration
	absoluteExpiresAt    time.Time
	rotationCount        int
}

// ID returns the session identifier.
func (r Record) ID() ID { return r.id }

// Tenant returns the tenant the session was created for.
func (r Record) Tenant() values.TenantId { return r.tenant }

// Subject returns the subject the session was created for.
func (r Record) Subject() string { return r.subject }

// PrincipalFingerprint returns the fingerprint of the [trust.Principal] the
// session was created from.
func (r Record) PrincipalFingerprint() string { return r.principalFingerprint }

// Assurance returns the assurance level recorded at session creation (or at
// the most recent authorized step-up; TRUST-003 records only creation-time
// assurance).
func (r Record) Assurance() trust.Assurance { return r.assurance }

// Status returns the session's current lifecycle state as of the last
// operation that observed it. It is not recomputed by this accessor: call
// [Manager.Get] to have idle/absolute expiry re-evaluated against the
// current time.
func (r Record) Status() Status { return r.status }

// RevokedReason returns the stable reason token recorded when the session
// left StatusActive, or the empty string while it is still active.
func (r Record) RevokedReason() string { return r.revokedReason }

// CreatedAt returns when the session was created.
func (r Record) CreatedAt() time.Time { return r.createdAt }

// LastActivityAt returns the instant idle-timeout is measured from.
func (r Record) LastActivityAt() time.Time { return r.lastActivityAt }

// AbsoluteExpiresAt returns the instant after which the session can never be
// used again, regardless of activity.
func (r Record) AbsoluteExpiresAt() time.Time { return r.absoluteExpiresAt }

// IdleTimeout returns the configured idle window.
func (r Record) IdleTimeout() time.Duration { return r.idleTimeout }

// RotationCount returns how many times the session's refresh token has been
// rotated.
func (r Record) RotationCount() int { return r.rotationCount }

// String returns a redacted, log-safe description. It never contains a
// refresh token or a token hash.
func (r Record) String() string {
	return fmt.Sprintf("session(id=%s tenant=%s status=%s rotations=%d)", r.id, r.tenant, r.status, r.rotationCount)
}

// EvidenceKind names one durable session-lifecycle transition or denial.
type EvidenceKind uint8

// Evidence kinds.
const (
	EvidenceUnspecified EvidenceKind = iota
	EvidenceCreated
	EvidenceRotated
	EvidenceDenied
	EvidenceRevoked
	EvidenceExpired
)

var evidenceKindWire = map[EvidenceKind]string{
	EvidenceCreated: "CREATED",
	EvidenceRotated: "ROTATED",
	EvidenceDenied:  "DENIED",
	EvidenceRevoked: "REVOKED",
	EvidenceExpired: "EXPIRED",
}

// String returns the stable wire token.
func (k EvidenceKind) String() string {
	if w, ok := evidenceKindWire[k]; ok {
		return w
	}
	return "EVIDENCE_UNSPECIFIED"
}

// Stable reason tokens recorded on [Evidence] and returned in error text.
// They are policy tokens, not display strings: a caller (or an alert rule)
// matches on the token, never on the English sentence around it.
const (
	ReasonIdleTimeout       = "idle_timeout"
	ReasonAbsoluteTimeout   = "absolute_timeout"
	ReasonManualRevoke      = "manual_revoke"
	ReasonRefreshReplay     = "refresh_replay_detected"
	ReasonTenantMismatch    = "tenant_mismatch"
	ReasonAssuranceMismatch = "assurance_mismatch"
)

// Evidence is one durable, append-only record of a session transition or a
// denial. It never carries a refresh token or a token hash.
type Evidence struct {
	ID        string
	SessionID ID
	Kind      EvidenceKind
	Reason    string
	At        time.Time
}

// Session errors. All are matchable with errors.Is.
var (
	ErrSessionNotFound   = errors.New("session: no session with this identifier")
	ErrSessionNotActive  = errors.New("session: session is not active")
	ErrRefreshUnknown    = errors.New("session: refresh token is not recognized")
	ErrRefreshReplay     = errors.New("session: refresh token was already used; the session has been revoked")
	ErrTenantMismatch    = errors.New("session: claimed tenant does not match the session's recorded tenant")
	ErrAssuranceMismatch = errors.New("session: claimed assurance does not match the session's recorded assurance")
	ErrInvalidCreateSpec = errors.New("session: create spec is incomplete")
)

// newOpaqueToken returns a fresh, unpredictable opaque token and its
// SHA-256 hash, hex-encoded. The raw token is bearer material; only the
// hash is ever retained by [Manager].
func newOpaqueToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("session: generating token: %w", err)
	}
	raw = tokenEncoding.EncodeToString(buf)
	return raw, hashToken(raw), nil
}

// hashToken returns the hex-encoded SHA-256 hash of a raw token. Hashing
// means a leaked index (session ID to hash) never discloses the bearer
// secret itself.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newID returns a fresh, unpredictable opaque session identifier.
func newID() (ID, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("session: generating id: %w", err)
	}
	return ID("sess_" + tokenEncoding.EncodeToString(buf)), nil
}

// newEvidenceID derives a durable evidence identifier from its own content,
// so that two evidence records are never accidentally indistinguishable.
func newEvidenceID(sessionID ID, kind EvidenceKind, reason string, at time.Time, seq int) string {
	h := sha256.New()
	fmt.Fprintf(h, "session=%s;kind=%s;reason=%s;at=%d;seq=%d", sessionID, kind, reason, at.UnixNano(), seq)
	return "ev:session:" + hex.EncodeToString(h.Sum(nil))[:32]
}
