package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/devprofile"
)

func TestFrontendDevForwardsToLiveCellWithoutRenderingFixtures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Live-Cell", "true")
		_, _ = w.Write([]byte(r.URL.RequestURI() + " " + r.Header.Get("Authorization")))
	}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)
	server := httptest.NewServer(frontendHandler(target, "live-token"))
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/workspace/app/work?nav=collapsed")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.Header.Get("X-Live-Cell") != "true" || string(body) != "/workspace/app/work?nav=collapsed Bearer live-token" {
		t.Fatalf("gateway did not return the live cell response: header=%q body=%q", response.Header.Get("X-Live-Cell"), body)
	}
	if strings.Contains(string(body), "Maya Chen") {
		t.Fatal("development gateway rendered fixture content")
	}
}

func TestFrontendDevRedirectsLegacyPreviewRoutesToProductionWorkspace(t *testing.T) {
	target, _ := url.Parse("http://cell.invalid")
	request := httptest.NewRequest(http.MethodGet, "/app/home?nav=collapsed", nil)
	recorder := httptest.NewRecorder()
	frontendHandler(target, "").ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Location") != "/workspace/app/home?nav=collapsed" {
		t.Fatalf("legacy redirect = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestLocalDevProfileMintsTheRealAuthenticatedPrincipal(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	token, err := developmentBearer(devprofile.Name, "", "", devprofile.Tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(devprofile.HMACKey), Issuer: devprofile.Issuer, Audience: devprofile.Audience,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
	if err != nil {
		t.Fatalf("profile credential does not pass the production verifier: %v", err)
	}
	if principal.Subject() != devprofile.Subject || principal.Tenant() != devprofile.Tenant {
		t.Fatalf("profile principal = subject %q tenant %q", principal.Subject(), principal.Tenant())
	}
}

func TestLocalDevProfilePreservesAnExplicitBearerAndLoopbackBoundary(t *testing.T) {
	got, err := developmentBearer(devprofile.Name, " supplied ", "bad", "", time.Time{})
	if err != nil || got != "supplied" {
		t.Fatalf("explicit bearer = %q, %v", got, err)
	}
	if !devprofile.IsLoopbackAddress("127.0.0.1:8768") || !devprofile.IsLoopbackHost("::1") || devprofile.IsLoopbackAddress("0.0.0.0:8768") || devprofile.IsLoopbackHost("dev.example") {
		t.Fatal("local-dev loopback boundary classification is incorrect")
	}
	if _, err := developmentBearer("production-ish", "", "", devprofile.Tenant, time.Now()); err == nil {
		t.Fatal("unknown profile was accepted")
	}
}
