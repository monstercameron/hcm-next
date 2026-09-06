package oidc_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/authn/oidc"
)

func TestHTTPTokenExchanger_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.PostForm.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", r.PostForm.Get("grant_type"))
		}
		if r.PostForm.Get("code_verifier") != "verifier-abc" {
			t.Errorf("code_verifier = %q, want verifier-abc", r.PostForm.Get("code_verifier"))
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "client-1" || pass != "secret-1" {
			t.Errorf("basic auth = (%q, %q, %v), want (client-1, secret-1, true)", user, pass, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"at-1","token_type":"Bearer","expires_in":3600,"id_token":"idtok-1","scope":"openid"}`)
	}))
	defer srv.Close()

	ex := oidc.HTTPTokenExchanger{}
	resp, err := ex.Exchange(context.Background(), oidc.TokenRequest{
		TokenEndpoint: srv.URL,
		Code:          "code-1",
		RedirectURI:   redirectURI,
		ClientID:      "client-1",
		ClientSecret:  "secret-1",
		CodeVerifier:  "verifier-abc",
	})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if resp.AccessToken != "at-1" || resp.IDToken != "idtok-1" {
		t.Fatalf("Exchange response = %+v, want access_token=at-1 id_token=idtok-1", resp)
	}
	if resp.Error != "" {
		t.Fatalf("Exchange response Error = %q, want empty", resp.Error)
	}
}

func TestHTTPTokenExchanger_OAuthError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"code expired"}`)
	}))
	defer srv.Close()

	ex := oidc.HTTPTokenExchanger{}
	resp, err := ex.Exchange(context.Background(), oidc.TokenRequest{TokenEndpoint: srv.URL, Code: "expired"})
	if err != nil {
		t.Fatalf("Exchange: %v (an OAuth-level refusal must come back as a populated TokenResponse, not a Go error)", err)
	}
	if resp.Error != "invalid_grant" {
		t.Fatalf("Error = %q, want invalid_grant", resp.Error)
	}
	if resp.AccessToken != "" {
		t.Fatalf("AccessToken = %q, want empty on error", resp.AccessToken)
	}
}

func TestHTTPTokenExchanger_UnparsableErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "<html>gateway error</html>")
	}))
	defer srv.Close()

	ex := oidc.HTTPTokenExchanger{}
	if _, err := ex.Exchange(context.Background(), oidc.TokenRequest{TokenEndpoint: srv.URL}); err == nil {
		t.Fatalf("Exchange: got nil error for an unparsable 500 response, want an error")
	}
}

func TestHTTPTokenExchanger_OversizedResponseRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"`+strings.Repeat("a", 70<<10)+`"}`)
	}))
	defer srv.Close()

	ex := oidc.HTTPTokenExchanger{}
	if _, err := ex.Exchange(context.Background(), oidc.TokenRequest{TokenEndpoint: srv.URL}); err == nil {
		t.Fatalf("Exchange: got nil error for an oversized response body, want an error")
	}
}

func TestHTTPTokenExchanger_PublicClientOmitsBasicAuth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); ok {
			t.Errorf("a public client request (no ClientSecret) must not send HTTP Basic auth")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"at","id_token":"idtok"}`)
	}))
	defer srv.Close()

	ex := oidc.HTTPTokenExchanger{}
	if _, err := ex.Exchange(context.Background(), oidc.TokenRequest{TokenEndpoint: srv.URL, ClientID: "public-client"}); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
}
