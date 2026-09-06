package workspace

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/journey"
)

const (
	// PathProductPrefix is the authenticated production UI subtree.
	PathProductPrefix = "/workspace/app/"
	// PathProductHome is the default human-facing page for this cell.
	PathProductHome = PathProductPrefix + "home"
)

// serveProduct serves a CSP-pinned shell. The existing Go/WASM client loads
// the reusable productui tree and calls the canonical JourneyService through
// the cell's gRPC-over-WebSocket tunnel; no JSON shadow API is introduced.
func (h *Handler) serveProduct(w http.ResponseWriter, r *http.Request) {
	if _, ok := productui.LookupRoute(r.URL.Path); !ok {
		h.serveNotFound(w, r)
		return
	}
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	config := JourneyConfig{
		TunnelURL: JourneyTunnelURL(r), Bearer: normalizeBearerInput(BearerFromRequest(r, h.devBrowserLogin)),
		Roles: []string{}, JourneysPath: PathJourney,
	}
	if principal != nil {
		config.Tenant = string(principal.Tenant())
		config.Subject = principal.Subject()
		config.Roles = principal.Roles()
		config.Purpose = principal.DefaultPurpose()
	}
	doc, err := productShellDocument(config, JourneyBundleBuilt())
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", ProductContentSecurityPolicy(r.Host))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(doc))
}

func productShellDocument(config JourneyConfig, bundleBuilt bool) (string, error) {
	island, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString("<title>HCM Next</title><style>")
	b.WriteString(productStylesheet())
	b.WriteString("</style></head><body>")
	b.WriteString(`<div id="` + JourneyRootElementID + `"><main class="main"><section class="surface empty-state" role="status"><h1>Connecting to HCM Next</h1><p class="muted">Loading authorized data from the live cell…</p>`)
	if !bundleBuilt {
		b.WriteString(`<p>This build carries no browser client. Build it with <code>`)
		b.WriteString(html.EscapeString(journeyBuildCommand))
		b.WriteString(`</code>.</p>`)
	}
	b.WriteString(`</section></main></div>`)
	b.WriteString(`<script type="application/json" id="` + JourneyConfigElementID + `">`)
	b.Write(island)
	b.WriteString("</script>")
	if bundleBuilt {
		b.WriteString(`<script src="` + PathWasmExec + `"></script>`)
		b.WriteString("<script>" + journeyLoaderSource + "</script>")
	}
	b.WriteString("</body></html>")
	return b.String(), nil
}

func productStylesheet() string {
	// Journey CSS comes first so the product shell retains ownership of
	// global layout while the prefixed journey components keep their tokens.
	return journey.Stylesheet() + productui.Stylesheet()
}

var productStylesheetHash = sha256Source(productStylesheet())

// ProductContentSecurityPolicy allows exactly the shared Go/WASM loader, the
// same-origin gRPC tunnel, and the product component stylesheet.
func ProductContentSecurityPolicy(host string) string {
	directives := []string{
		"default-src 'none'", "base-uri 'none'", "form-action 'self'", "frame-ancestors 'none'",
		"script-src '" + journeyLoaderHash + "' 'self' 'wasm-unsafe-eval'",
	}
	connect := "connect-src 'self'"
	if authority := sanitizeHostAuthority(host); authority != "" {
		connect += " ws://" + authority + " wss://" + authority
	}
	return strings.Join(append(directives, connect, "style-src '"+productStylesheetHash+"'", "img-src 'self'"), "; ")
}
