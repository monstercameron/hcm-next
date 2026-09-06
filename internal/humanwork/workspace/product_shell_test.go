package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

func TestProductShellCarriesAuthenticatedLiveClientConfiguration(t *testing.T) {
	h, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome+"?nav=collapsed", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	config := island(t, recorder.Body.String())
	if config.TunnelURL != "ws://cell.test"+PathTunnel || config.Bearer != token || config.Tenant != shellTenant || config.Subject != shellSubject {
		t.Fatalf("product config = %+v", config)
	}
	for _, forbidden := range []string{"Maya Chen", "Northstar Group", "Validation passed", "Manager change completed"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("product shell contains fixture content %q", forbidden)
		}
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != ProductContentSecurityPolicy("cell.test") {
		t.Fatalf("product CSP = %q", got)
	}
}

func TestProductShellDeclaresInitialLocaleBeforeWASMBoot(t *testing.T) {
	h, token := newShellHandler(t, false)
	tests := []struct {
		locale string
		want   []string
	}{
		{locale: "ar", want: []string{`lang="ar"`, `dir="rtl"`, `data-hcm-locale="ar"`, `data-hcm-catalog="product-ui.v1"`, `data-hcm-message-fallback="en-US"`}},
		{locale: "zz", want: []string{`lang="en-US"`, `dir="ltr"`, `data-hcm-locale="en-US"`, `data-hcm-locale-fallback="unsupported_locale"`}},
	}
	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome+"?locale="+test.locale, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
			}
			for _, want := range test.want {
				if !strings.Contains(recorder.Body.String(), want) {
					t.Errorf("product shell missing %q", want)
				}
			}
		})
	}
}

func TestProductShellDeclaresColorModeBeforeWASMBoot(t *testing.T) {
	doc, err := productShellDocument(JourneyConfig{TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-color-mode="system"`, `<meta name="color-scheme" content="light dark">`} {
		if !strings.Contains(doc, want) {
			t.Errorf("product shell missing initial color-mode contract %q", want)
		}
	}
}

func TestProductShellRefusesUnknownAndUnauthenticatedRoutes(t *testing.T) {
	h, token := newShellHandler(t, false)
	unknown := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductPrefix+"unknown", nil)
	unknown.Header.Set("Authorization", "Bearer "+token)
	unknownRecorder := httptest.NewRecorder()
	h.ServeHTTP(unknownRecorder, unknown)
	if unknownRecorder.Code != http.StatusNotFound {
		t.Fatalf("unknown product page = %d", unknownRecorder.Code)
	}

	unauthenticated := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
	unauthenticatedRecorder := httptest.NewRecorder()
	h.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated product page = %d", unauthenticatedRecorder.Code)
	}
}

func TestProductShellUsesOnlyPinnedStylesAndSharedWASMClient(t *testing.T) {
	doc, err := productShellDocument(JourneyConfig{TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<style>`, PathWasmExec, PathJourneyWasm, `id="` + JourneyRootElementID + `"`, `id="` + JourneyConfigElementID + `"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("product shell missing %q", want)
		}
	}
	for _, want := range []string{"--jn-accent", ".jn-embedded", ".app-shell", "--accent:#006b57"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("product shell does not carry integrated journey style %q", want)
		}
	}
	policy := ProductContentSecurityPolicy("cell.test")
	if strings.Contains(policy, "'unsafe-inline'") || !strings.Contains(policy, productStylesheetHash) || !strings.Contains(policy, journeyLoaderHash) || !strings.Contains(policy, "img-src 'self'") {
		t.Fatalf("product CSP is not pinned: %s", policy)
	}
}

func TestProductShellUsesTheRouteShapedLoadingProxyBeforeWASMStarts(t *testing.T) {
	config := JourneyConfig{
		TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token",
		Tenant: "harborcare-demo", Subject: "local-developer", Purpose: "compensation_review",
	}
	doc, err := productShellDocumentForRoute(config, true, productui.ResolveProductLocale("en-US"), productui.PageHistory)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell is-loading"`, `class="loading-proxy loading-proxy-history"`,
		`aria-busy="true"`, "Loading authorized data from the live cell", "Harborcare Demo", "Compensation Review",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("initial product shell missing %q", want)
		}
	}
	if strings.Contains(doc, "0 promotion journeys are visible") {
		t.Fatal("initial product shell exposed an unresolved count as zero")
	}
}
