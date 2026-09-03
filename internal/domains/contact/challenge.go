package contact

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type ChallengeStatus string

const (
	ChallengeVerified ChallengeStatus = "VERIFIED"
	ChallengeExpired  ChallengeStatus = "EXPIRED"
	ChallengeConsumed ChallengeStatus = "CONSUMED"
	ChallengeInvalid  ChallengeStatus = "INVALID"
)

var (
	ErrChallengeTokenRequired = errors.New("contact: challenge token is required")
	ErrChallengeConsumed      = errors.New("contact: challenge has already been consumed")
)

// VerificationChallenge contains no bearer secret. TokenHash is a
// domain-separated digest of the one-time token and its immutable scope.
type VerificationChallenge struct {
	Subject    values.EntityRef
	EndpointID string
	Purpose    string
	ExpiresAt  time.Time
	TokenHash  [32]byte
	ConsumedAt *time.Time
}

// IssueChallenge creates a challenge and returns the one-time token to send to
// the endpoint. Callers must not persist the returned token; only the hash in
// the challenge is durable.
func IssueChallenge(subject values.EntityRef, endpointID, purpose, token string, now time.Time, ttl time.Duration) (VerificationChallenge, string, error) {
	if err := subject.Validate(); err != nil {
		return VerificationChallenge{}, "", fmt.Errorf("contact: subject: %w", err)
	}
	if endpointID == "" {
		return VerificationChallenge{}, "", fmt.Errorf("%w: endpoint", ErrInvalidEndpoint)
	}
	if purpose == "" {
		return VerificationChallenge{}, "", ErrInvalidPurpose
	}
	if token == "" {
		return VerificationChallenge{}, "", ErrChallengeTokenRequired
	}
	if now.IsZero() || ttl <= 0 {
		return VerificationChallenge{}, "", fmt.Errorf("contact: invalid challenge expiry")
	}
	if ttl > 24*time.Hour {
		return VerificationChallenge{}, "", fmt.Errorf("contact: challenge ttl exceeds 24 hours")
	}
	c := VerificationChallenge{Subject: subject, EndpointID: endpointID, Purpose: purpose, ExpiresAt: now.Add(ttl)}
	c.TokenHash = hashToken(subject, endpointID, purpose, token)
	return c, token, nil
}

func hashToken(subject values.EntityRef, endpointID, purpose, token string) [32]byte {
	return sha256.Sum256([]byte("hcmnext/contact-challenge/v1\x00" + subject.String() + "\x00" + endpointID + "\x00" + purpose + "\x00" + token))
}

func (c *VerificationChallenge) Validate() error {
	if c == nil {
		return fmt.Errorf("contact: nil challenge")
	}
	if err := c.Subject.Validate(); err != nil {
		return fmt.Errorf("contact: subject: %w", err)
	}
	if c.EndpointID == "" || c.Purpose == "" || c.ExpiresAt.IsZero() {
		return fmt.Errorf("contact: incomplete challenge")
	}
	if c.TokenHash == [32]byte{} {
		return fmt.Errorf("contact: challenge token hash is missing")
	}
	return nil
}

// Verify consumes a valid challenge atomically from the caller's perspective.
// A replay returns CONSUMED; wrong subject/endpoint/purpose/token returns
// INVALID without changing state. Expired challenges remain auditable.
func (c *VerificationChallenge) Verify(subject values.EntityRef, endpointID, purpose, token string, now time.Time) ChallengeStatus {
	if c == nil || c.Validate() != nil || subject != c.Subject || endpointID != c.EndpointID || purpose != c.Purpose || token == "" {
		return ChallengeInvalid
	}
	if c.ConsumedAt != nil {
		return ChallengeConsumed
	}
	if now.IsZero() || !now.Before(c.ExpiresAt) {
		return ChallengeExpired
	}
	h := hashToken(subject, endpointID, purpose, token)
	if subtle.ConstantTimeCompare(h[:], c.TokenHash[:]) != 1 {
		return ChallengeInvalid
	}
	t := now.UTC()
	c.ConsumedAt = &t
	return ChallengeVerified
}

// VerifyChallenge is the functional form for callers that prefer a helper.
func VerifyChallenge(c *VerificationChallenge, subject values.EntityRef, endpointID, purpose, token string, now time.Time) ChallengeStatus {
	return c.Verify(subject, endpointID, purpose, token, now)
}
