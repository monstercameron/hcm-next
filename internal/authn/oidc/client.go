package oidc

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ClientRegistration is the per-(tenant, issuer) binding this package needs
// beyond an [issuerregistry.Issuer]'s pinned verification material: the
// OAuth client identity and the pinned authorization/token endpoints and
// redirect URI. See doc.go's "Issuer records only pin verification
// material, not endpoints" section for why this is a separate record rather
// than additional fields on Issuer.
//
// A [ClientRegistration] is looked up, never taken from an incoming
// request: [Flow.BeginAuthorization] resolves RedirectURI, ClientID and the
// endpoints from here, so nothing an attacker puts in a query string or
// form field ever substitutes for the tenant's actual registered client.
type ClientRegistration struct {
	Tenant    values.TenantId
	IssuerURL string

	// ClientID is the OAuth client_id this registration authenticates as.
	ClientID string
	// ClientSecret authenticates a confidential client at the token
	// endpoint (HTTP Basic auth). Empty means a public client: PKCE alone
	// is this flow's proof of possession, matching AUTHN-002's own scope
	// ("PKCE (S256 only)").
	ClientSecret string
	// RedirectURI is the exact redirect_uri this flow always sends, both
	// in the authorization request and the token exchange (RFC 6749
	// requires the same value at both steps). It must have been registered
	// with the identity provider out of band; this package does not itself
	// register redirect URIs anywhere.
	RedirectURI string

	// AuthorizationEndpoint and TokenEndpoint are the issuer's pinned OIDC
	// endpoints. Like [issuerregistry.JWKSSource], these are configuration
	// facts an operator pins, never fetched from a discovery document.
	AuthorizationEndpoint string
	TokenEndpoint         string

	// Scopes are the OAuth scopes the authorization request declares. Must
	// include "openid" -- this is an OpenID Connect flow, not bare OAuth.
	Scopes []string
}

// validate checks every field [Flow.BeginAuthorization] requires before it
// is ever used to build an authorization request or a token exchange.
func (c ClientRegistration) validate() error {
	if err := c.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidClientRegistration, err)
	}
	if strings.TrimSpace(c.IssuerURL) == "" {
		return fmt.Errorf("%w: issuer url is required", ErrInvalidClientRegistration)
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("%w: client id is required", ErrInvalidClientRegistration)
	}
	for _, field := range []struct {
		name string
		raw  string
	}{
		{"redirect uri", c.RedirectURI},
		{"authorization endpoint", c.AuthorizationEndpoint},
		{"token endpoint", c.TokenEndpoint},
	} {
		if err := validateAbsoluteURL(field.name, field.raw); err != nil {
			return err
		}
	}
	if !slices.Contains(c.Scopes, "openid") {
		return fmt.Errorf("%w: scopes must include \"openid\"", ErrInvalidClientRegistration)
	}
	return nil
}

func validateAbsoluteURL(name, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidClientRegistration, name)
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: %s %q does not parse as an absolute URL", ErrInvalidClientRegistration, name, raw)
	}
	return nil
}

// ClientSource resolves the [ClientRegistration] for one (tenant, issuer)
// pair. It is the seam a production composition root wires to whatever
// governs client registrations (today, none exists beyond this package;
// [StaticClientSource] is the in-memory adapter this package ships).
type ClientSource interface {
	LookupClient(tenant values.TenantId, issuerURL string) (ClientRegistration, bool, error)
}

// StaticClientSource is a fixed, in-memory [ClientSource]: a map from
// (tenant, issuer) to registration. It is not safe for concurrent writes
// after construction -- build it once with [NewStaticClientSource] and
// [StaticClientSource.WithClient] and then only read it -- matching
// internal/trust/federation.StaticKeySource's own concurrency contract.
type StaticClientSource struct {
	clients map[values.TenantId]map[string]ClientRegistration
}

// NewStaticClientSource returns an empty static client source.
func NewStaticClientSource() *StaticClientSource {
	return &StaticClientSource{clients: make(map[values.TenantId]map[string]ClientRegistration)}
}

// WithClient registers c and returns the receiver, so several registrations
// can be chained onto one source. It does not validate c: validation happens
// at use time in [Flow.BeginAuthorization], so every [ClientSource]
// implementation is held to the same fail-closed standard regardless of
// whether it validates on write.
func (s *StaticClientSource) WithClient(c ClientRegistration) *StaticClientSource {
	if s.clients[c.Tenant] == nil {
		s.clients[c.Tenant] = make(map[string]ClientRegistration)
	}
	s.clients[c.Tenant][c.IssuerURL] = c
	return s
}

// LookupClient implements [ClientSource].
func (s *StaticClientSource) LookupClient(tenant values.TenantId, issuerURL string) (ClientRegistration, bool, error) {
	c, ok := s.clients[tenant][issuerURL]
	return c, ok, nil
}

var _ ClientSource = (*StaticClientSource)(nil)
