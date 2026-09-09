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

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
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
	upstreamText := flag.String("upstream", defaultUpstream, "live Human Capital Management Suite HTTP cell origin")
	tenant := flag.String("tenant", devprofile.Tenant, "tenant used by the local-dev credential")
	flag.Parse()
	upstream, err := url.Parse(*upstreamText)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		log.Fatalf("frontenddev: invalid upstream %q", *upstreamText)
	}
	bearer, err := gatewayBearer(*profile, os.Getenv(envDevBearer), os.Getenv(envDevHMACKey), *tenant, time.Now().UTC())
	if err != nil {
		log.Fatal(err)
	}
	if *profile == devprofile.Name {
		if !devprofile.IsLoopbackAddress(*listen) || !devprofile.IsLoopbackHost(upstream.Hostname()) {
			log.Fatal("frontenddev: local-dev profile requires loopback listen and upstream addresses")
		}
	}
	server := &http.Server{Addr: *listen, Handler: frontendHandler(upstream, bearer, *profile == devprofile.Name), ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(os.Stdout, "Human Capital Management Suite live gateway: http://%s/ -> %s\n", *listen, upstream)
	log.Fatal(server.ListenAndServe())
}

// gatewayBearer leaves the local-dev browser unauthenticated so the production
// workspace's persona login can establish its cookie-backed session. An
// explicit bearer remains an intentional escape hatch for automation.
func gatewayBearer(profile, existing, key, tenant string, now time.Time) (string, error) {
	if strings.TrimSpace(existing) != "" {
		return developmentBearer(profile, existing, key, tenant, now)
	}
	if profile == devprofile.Name {
		return "", nil
	}
	return developmentBearer(profile, existing, key, tenant, now)
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

func frontendHandler(upstream *url.URL, bearer string, allowOpaqueBrowserOrigin bool) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstream)
			// The gateway and the cell form one browser origin. Preserve the
			// authority the browser actually reached so the cell's same-origin
			// policy, absolute WebSocket URL, cookies, and redirects all bind to
			// the public development address rather than the private upstream.
			request.Out.Host = request.In.Host
			request.SetXForwarded()
			// Codex's sandboxed in-app browser has an opaque origin and therefore
			// emits "Origin: null" even for a form submitted to the page that
			// rendered it. Only the explicitly selected local-dev profile may
			// translate that opaque origin, and only when the public authority is
			// loopback. The cell still requires its own HttpOnly SameSite CSRF
			// cookie after this translation. Standard and non-loopback gateways
			// continue to reject opaque origins.
			if allowOpaqueBrowserOrigin && request.Out.Header.Get("Origin") == "null" {
				if publicOrigin, ok := loopbackPublicOrigin(request.In); ok {
					request.Out.Header.Set("Origin", publicOrigin)
				}
			}
			if request.Out.Header.Get("Authorization") == "" && strings.TrimSpace(bearer) != "" {
				request.Out.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "live Human Capital Management Suite cell unavailable", http.StatusBadGateway)
		},
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

func loopbackPublicOrigin(request *http.Request) (string, bool) {
	if request == nil || strings.TrimSpace(request.Host) == "" {
		return "", false
	}
	publicURL, err := url.Parse("http://" + request.Host)
	if err != nil || !devprofile.IsLoopbackHost(publicURL.Hostname()) {
		return "", false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(request.Host), true
}
