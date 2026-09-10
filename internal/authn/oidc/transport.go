package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxTokenResponseBytes bounds the token endpoint's response body before any
// allocation that depends on its length.
const maxTokenResponseBytes = 64 << 10

// TokenRequest is the authorization_code grant request [Flow.HandleCallback]
// asks a [TokenExchanger] to perform. It carries only what a code+PKCE
// exchange needs; there is no field for a scope or grant type other than
// authorization_code, because this package never issues any other kind of
// request.
type TokenRequest struct {
	TokenEndpoint string

	Code         string
	RedirectURI  string
	ClientID     string
	ClientSecret string // empty for a public client: HTTP Basic auth is omitted.
	CodeVerifier string
}

// TokenResponse is the token endpoint's answer, normalized to the fields
// this package inspects. Error and ErrorDescription are populated instead
// of AccessToken/IDToken when the exchange was refused (RFC 6749 §5.2); a
// [TokenExchanger] implementation is expected to report a non-2xx HTTP
// response this way rather than as a Go error, so [Flow.HandleCallback] can
// tell "the network call failed" (a Go error from [TokenExchanger.Exchange])
// apart from "the authorization server refused the grant" (a populated
// Error field) and classify each with its own typed reason.
type TokenResponse struct {
	AccessToken string
	TokenType   string
	ExpiresIn   int64
	IDToken     string
	Scope       string

	Error            string
	ErrorDescription string
}

// TokenExchanger is the injected transport port behind the token endpoint
// call: an in-memory fake identity provider in tests, [HTTPTokenExchanger]
// over net/http in production. This is the seam LIB-010's deferral requires
// -- see doc.go.
type TokenExchanger interface {
	Exchange(ctx context.Context, req TokenRequest) (TokenResponse, error)
}

// HTTPTokenExchanger is the production [TokenExchanger]: a plain
// net/http.Client POSTing the standard RFC 6749 §4.1.3 form-encoded
// authorization_code request.
type HTTPTokenExchanger struct {
	// Client performs the HTTP round trip. Nil means an http.Client with a
	// 15-second timeout -- a token exchange is a synchronous, user-facing
	// redirect step and must never hang indefinitely.
	Client *http.Client
}

// defaultTokenExchangeTimeout bounds the production HTTP client when
// [HTTPTokenExchanger.Client] is nil.
const defaultTokenExchangeTimeout = 15 * time.Second

// tokenErrorWire and tokenSuccessWire are the two JSON shapes a token
// endpoint's response body can take (RFC 6749 §5.1 and §5.2). Decoding into
// one flexible struct covers both without guessing from the HTTP status
// code alone, which some identity providers report inconsistently.
type tokenWire struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	IDToken          string `json:"id_token"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Exchange implements [TokenExchanger] over net/http.
func (h HTTPTokenExchanger) Exchange(ctx context.Context, req TokenRequest) (TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", req.Code)
	form.Set("redirect_uri", req.RedirectURI)
	form.Set("client_id", req.ClientID)
	form.Set("code_verifier", req.CodeVerifier)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oidc: build token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	if req.ClientSecret != "" {
		httpReq.SetBasicAuth(req.ClientID, req.ClientSecret)
	}

	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: defaultTokenExchangeTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oidc: token endpoint request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oidc: read token endpoint response: %w", err)
	}
	if len(body) > maxTokenResponseBytes {
		return TokenResponse{}, fmt.Errorf("oidc: token endpoint response exceeds %d bytes", maxTokenResponseBytes)
	}

	var wire tokenWire
	if err := json.Unmarshal(body, &wire); err != nil {
		if resp.StatusCode >= 400 {
			return TokenResponse{}, fmt.Errorf("oidc: token endpoint returned status %d with unparsable body", resp.StatusCode)
		}
		return TokenResponse{}, fmt.Errorf("oidc: decode token endpoint response: %w", err)
	}
	if wire.Error == "" && resp.StatusCode >= 400 {
		return TokenResponse{}, fmt.Errorf("oidc: token endpoint returned status %d with no error field", resp.StatusCode)
	}
	return TokenResponse(wire), nil
}

var _ TokenExchanger = HTTPTokenExchanger{}
