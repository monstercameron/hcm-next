// Package pseudonym owns scoped, unlinkable pseudonymous subject identifiers.
// It deliberately has no persistence or transport dependency: callers must
// validate a request before entering their authoritative write transaction.
package pseudonym

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const tokenPrefix = "ps1_"

var (
	ErrSecretRequired   = errors.New("pseudonym: secret is required")
	ErrVersionRequired  = errors.New("pseudonym: version is required")
	ErrScopeInvalid     = errors.New("pseudonym: scope must name exactly one subject boundary")
	ErrSubjectRequired  = errors.New("pseudonym: subject is required")
	ErrPseudonymInvalid = errors.New("pseudonym: pseudonym does not match the declared scope")
)

// Scope is the one boundary in which an identifier is stable. Exactly one
// field must be populated; a token therefore cannot be joined between tenant,
// program, campaign, or case scopes.
type Scope struct {
	Tenant   string
	Program  string
	Campaign string
	Case     string
}

func (s Scope) Validate() error {
	n := 0
	for _, v := range []string{s.Tenant, s.Program, s.Campaign, s.Case} {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	if n != 1 {
		return ErrScopeInvalid
	}
	return nil
}

func (s Scope) label() string {
	if s.Tenant != "" {
		return "tenant:" + s.Tenant
	}
	if s.Program != "" {
		return "program:" + s.Program
	}
	if s.Campaign != "" {
		return "campaign:" + s.Campaign
	}
	return "case:" + s.Case
}

// Request is checked before any authoritative or external effect is allowed.
// Subject is an opaque stable input and is never returned in a Receipt.
type Request struct {
	Subject   string
	Pseudonym string
	Scope     Scope
	Version   string
}

// Effects makes the zero-effect rejection contract explicit for adapters and
// tests. Production adapters can map these categories to their own sinks.
type Effects struct {
	AuthoritativeRows int
	BusinessEvents    int
	OutboxEntries     int
	HumanWork         int
	ProviderRequests  int
}

func (e Effects) IsZero() bool { return e == Effects{} }

// Receipt contains only the scoped pseudonym and contract version.
type Receipt struct {
	Pseudonym, Version string
	Scope              Scope
}

// Rejection preserves machine-readable evidence for an invalid request.
type Rejection struct{ Code, Field, State, Version string }

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", r.Code, r.Field, r.State, r.Version)
}

// Engine derives and validates identifiers under one secret and contract
// version. Secret material is copied and never included in errors or receipts.
type Engine struct {
	secret  []byte
	version string
}

func New(secret []byte, version string) (*Engine, error) {
	if len(secret) == 0 {
		return nil, ErrSecretRequired
	}
	if strings.TrimSpace(version) == "" {
		return nil, ErrVersionRequired
	}
	return &Engine{secret: append([]byte(nil), secret...), version: version}, nil
}

func (e *Engine) Version() string { return e.version }

// Derive returns a deterministic token only within scope. HMAC prevents a
// token from being recomputed by an offline guesser without the secret.
func (e *Engine) Derive(subject string, scope Scope) (string, error) {
	if strings.TrimSpace(subject) == "" {
		return "", ErrSubjectRequired
	}
	if err := scope.Validate(); err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, e.secret)
	_, _ = fmt.Fprintf(m, "hcmnext/anon-002\x00%s\x00%s\x00%s", e.version, scope.label(), subject)
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// Accept validates every identity-bearing field before effects are admitted.
// The returned Effects is always zero on error, including malformed seeded
// cross-scope pseudonyms.
func (e *Engine) Accept(req Request) (Receipt, Effects, error) {
	zero := Effects{}
	if err := req.Scope.Validate(); err != nil {
		return Receipt{}, zero, reject("scope", "INVALID", req.Version)
	}
	if req.Version != e.version {
		return Receipt{}, zero, reject("version", "MISMATCH", req.Version)
	}
	if strings.TrimSpace(req.Subject) == "" {
		return Receipt{}, zero, reject("subject", "MISSING", req.Version)
	}
	if strings.TrimSpace(req.Pseudonym) == "" {
		return Receipt{}, zero, reject("pseudonym", "MISSING", req.Version)
	}
	want, _ := e.Derive(req.Subject, req.Scope)
	if !hmac.Equal([]byte(want), []byte(req.Pseudonym)) {
		return Receipt{}, zero, reject("pseudonym", "CORRELATES_OUTSIDE_SCOPE", req.Version)
	}
	return Receipt{Pseudonym: req.Pseudonym, Version: e.version, Scope: req.Scope}, Effects{AuthoritativeRows: 1}, nil
}

func reject(field, state, version string) error {
	return &Rejection{Code: "ANON_002_REJECTED", Field: field, State: state, Version: version}
}
