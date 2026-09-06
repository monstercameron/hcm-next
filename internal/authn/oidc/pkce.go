package oidc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// b64 is unpadded base64url, the encoding RFC 7636 and OIDC both use for
// PKCE verifiers/challenges, state, nonce and the at_hash comparison value.
var b64 = base64.RawURLEncoding

// stateNonceBytes is the byte length of a generated state or nonce value
// before base64url encoding (256 bits; encodes to 43 characters).
const stateNonceBytes = 32

// randomToken returns a fresh crypto/rand-sourced token of nBytes random
// bytes, base64url-encoded. [Flow.BeginAuthorization] uses it for both state
// and nonce: two independently drawn values, never derived from each other,
// so learning one never helps guess the other.
func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: generate random token: %w", err)
	}
	return b64.EncodeToString(buf), nil
}

// deriveVerifier deterministically derives the PKCE code_verifier for one
// authorization attempt from its state value and the flow's own secret,
// rather than ever persisting the verifier itself. See doc.go's "Code
// verifier custody" section for the rationale. The result is 43 characters
// of base64url text (32 raw bytes), within RFC 7636's required 43-128
// character range and drawn entirely from its allowed unreserved character
// set (base64url's alphabet is a subset of ALPHA / DIGIT / "-" / "." / "_"
// / "~").
func deriveVerifier(secret []byte, state string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(state))
	return b64.EncodeToString(mac.Sum(nil))
}

// codeChallengeS256 computes the RFC 7636 S256 code_challenge for verifier:
// BASE64URL-ENCODE(SHA256(ASCII(verifier))). This package supports no other
// code_challenge_method -- AUTHN-002 scopes this flow to "PKCE (S256 only)"
// and the plain method (code_challenge == code_verifier) provides no
// protection against an adversary who can observe the authorization
// request, so it is never offered.
func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return b64.EncodeToString(sum[:])
}
