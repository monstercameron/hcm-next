package ssrshell

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// ShellCSS is this package's entire stylesheet: a visually-hidden utility
// class used only by the skip-to-main-content link. It carries no brand
// token values (color, typography, spacing) -- BrandTokens are named by a
// PageDefinition and resolved by whatever governed token layer serves the
// final production page (see the package doc comment's "What this package
// does and does not own"); this renderer invents no CSS for them.
func ShellCSS() string {
	return shellTypedStylesheet()
}

// shellStylesheetHash pins the exact stylesheet [ShellCSS] returns, computed
// once at package init the same way
// internal/humanwork/workspace/render.go's stylesheetHash and
// journey_shell.go's journeyStylesheetHash pin their own inline stylesheets:
// the hash is derived from the same function the document actually emits,
// so a change to the CSS can never silently desync from the policy that
// admits it.
var shellStylesheetHash = cspHashSource(ShellCSS())

// cspHashSource renders one CSP hash-source expression ("sha256-<base64>")
// for an inline block, matching the sha256Source helper already used by
// internal/humanwork/workspace for its own inline stylesheets and loader
// script.
func cspHashSource(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// ContentSecurityPolicy returns the CSP header value a caller should set on
// a response carrying a document [Render] produced.
//
// It is deny-by-default with exactly two allowances:
//
//   - style-src: [ShellCSS] by hash, and nothing else -- no 'unsafe-inline',
//     because the shell emits that exact stylesheet and no other.
//   - script-src: 'none'. This is not a placeholder pending a later
//     tightening pass: [Render] never emits an executable script (its one
//     <script> element is type="application/json", a data island, not a
//     program), so no allowance is needed at all.
//
// Every other directive -- images, connections, forms, framing, the
// document's own base URI -- is refused, because a semantic shell with
// empty widget-slot mount points has no legitimate reason to load an
// image, open a connection, submit a form, or be framed before a later
// layer fills those slots in.
func ContentSecurityPolicy() string {
	directives := []string{
		"default-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
		"script-src 'none'",
		"style-src '" + shellStylesheetHash + "'",
		"img-src 'none'",
		"connect-src 'none'",
	}
	return strings.Join(directives, "; ")
}
