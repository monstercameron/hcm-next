// Command frontenddev is a same-origin development gateway to a live HCM
// Next cell. It contains no fixtures and renders no alternate application:
// HTML, WASM, gRPC-over-WebSocket traffic, authentication, and business data
// all come from the production server behind it.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/devprofile"
)

const (
	defaultListen   = "127.0.0.1:8768"
	defaultUpstream = "http://127.0.0.1:8080"
	envDevBearer    = "HCMNEXT_DEV_BEARER"
	envDevHMACKey   = "HCMNEXT_DEV_HMAC_KEY"
	standardProfile = "standard"
)

func main() {
	profile := flag.String("profile", standardProfile, "development defaults profile: standard or local-dev")
	listen := flag.String("listen", defaultListen, "local address for the frontend development gateway")
	upstreamText := flag.String("upstream", defaultUpstream, "live HCM Next HTTP cell origin")
	tenant := flag.String("tenant", devprofile.Tenant, "tenant used by the local-dev credential")
	flag.Parse()
	upstream, err := url.Parse(*upstreamText)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		log.Fatalf("frontenddev: invalid upstream %q", *upstreamText)
	}
	bearer, err := developmentBearer(*profile, os.Getenv(envDevBearer), os.Getenv(envDevHMACKey), *tenant, time.Now().UTC())
	if err != nil {
		log.Fatal(err)
	}
	if *profile == devprofile.Name {
		if !devprofile.IsLoopbackAddress(*listen) || !devprofile.IsLoopbackHost(upstream.Hostname()) {
			log.Fatal("frontenddev: local-dev profile requires loopback listen and upstream addresses")
		}
	}
	server := &http.Server{Addr: *listen, Handler: frontendHandler(upstream, bearer), ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(os.Stdout, "HCM Next live gateway: http://%s/ -> %s\n", *listen, upstream)
	log.Fatal(server.ListenAndServe())
}

func developmentBearer(profile, existing, key, tenant string, now time.Time) (string, error) {
	if profile != standardProfile && profile != devprofile.Name {
		return "", fmt.Errorf("frontenddev: -profile must be %q or %q; got %q", standardProfile, devprofile.Name, profile)
	}
	if strings.TrimSpace(existing) != "" {
		return strings.TrimSpace(existing), nil
	}
	if profile == standardProfile {
		return "", nil
	}
	if strings.TrimSpace(key) == "" {
		key = devprofile.HMACKey
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(key), Issuer: devprofile.Issuer, Audience: devprofile.Audience,
	})
	if err != nil {
		return "", fmt.Errorf("frontenddev: local-dev credential verifier: %w", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: devprofile.Issuer, Audience: devprofile.Audience,
		Subject: devprofile.Subject, SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: devprofile.OrgScope,
		Roles:               strings.Split(devprofile.Roles, ","), Purposes: []string{devprofile.Purpose},
		AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef:   "session-hcmnext-local-dev-profile",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(8 * time.Hour).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("frontenddev: mint local-dev credential: %w", err)
	}
	return token, nil
}

func frontendHandler(upstream *url.URL, bearer string) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		if request.Header.Get("Authorization") == "" && strings.TrimSpace(bearer) != "" {
			request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "live HCM Next cell unavailable", http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			http.Redirect(w, r, "/workspace/app/home", http.StatusTemporaryRedirect)
			return
		case strings.HasPrefix(r.URL.Path, "/app/"):
			target := "/workspace" + r.URL.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}
