package workspace

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"strings"

	"github.com/monstercameron/hcm-next/tools/uxqual/contract"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/ssr"
	"github.com/monstercameron/hcm-next/tools/uxqual/tokens"
)

// Route paths this workspace serves. They are constants because the
// discovery document, the rendered form targets and the mux all have to agree
// on them, and three literals that agree today are three literals that can
// disagree tomorrow.
const (
	// RoutePrefix is the subtree the cell's edge mux mounts this handler on.
	RoutePrefix = "/workspace/"
	// PathPromotion renders the workspace for the worker named by the
	// "worker" query parameter.
	PathPromotion = "/workspace/promotion"
	// PathSimulate accepts the submitted request form and re-renders.
	PathSimulate = "/workspace/promotion/simulate"
	// PathReceiptPrefix addresses one zero-effect receipt by its digest.
	PathReceiptPrefix = "/workspace/promotion/receipt/"
	// PathAssetPrefix serves the progressive-enhancement bundle, when one has
	// been built into this package.
	PathAssetPrefix = "/workspace/assets/"
	// PathWasm is the GWC/WASM bundle.
	PathWasm = PathAssetPrefix + assetWasm
	// PathWasmExec is the Go WASM runtime shim the bundle needs.
	PathWasmExec = PathAssetPrefix + assetWasmExec
	// PathLogin renders (GET) and accepts (POST) the dev-only pasted-token
	// sign-in form. It is registered only when Options.DevBrowserLogin is
	// set; otherwise this cell serves no route here at all.
	PathLogin = "/workspace/login"
	// PathLogout clears the session cookie PathLogin set. Registered under
	// the same condition as PathLogin.
	PathLogout = "/workspace/logout"
)

// formID is the id the request form is given so that the action buttons -
// which the frozen SSR template renders in their own sibling forms - can name
// it as their form owner. It is the HTML5 form-owner attribute, not script:
// the page still submits with JavaScript disabled.
const formID = "promotion-request"

// Form field names the submitted request carries beyond the contract's own
// field ids.
const (
	// ParamWorker names the worker on both the query string and the form.
	ParamWorker = "worker"
	// ParamCSRF carries the per-session token.
	ParamCSRF = "csrf_token"
	// ParamTransition carries which declared action was invoked.
	ParamTransition = "transition"
	// ParamLocale preserves a presentation choice across a normal HTML form
	// submission. It has no authority beyond formatting and translations.
	ParamLocale = "locale"
)

// Render produces the served HTML document for one page.
//
// The document is produced by the frozen SSR renderer (tools/uxqual/render/ssr)
// and then bound to this workspace's routes by [bindForms]. Binding is a
// post-processing step rather than a renderer change on purpose: the renderer
// is a finished, qualified artifact this package must not edit, and it
// deliberately knows nothing about URLs - its forms post to "#". What is
// added here is exactly the routing and the CSRF token, never content.
func Render(c contract.WorkspaceContract, csrf, worker string, enhanced bool) (string, error) {
	doc, err := ssr.Render(c)
	if err != nil {
		return "", fmt.Errorf("workspace: render the workspace document: %w", err)
	}
	doc = bindForms(doc, csrf, worker)
	if enhanced {
		doc = strings.Replace(doc, "</body>", loaderScript()+"\n</body>", 1)
	}
	return doc, nil
}

// RenderPage renders a Page with its presentation context. Its locale is
// applied after the already-masked contract is built, so this function can
// never unmask a field or change a governed simulation answer.
func RenderPage(page Page, csrf, worker string, enhanced bool) (string, error) {
	doc, err := ssr.Render(page.Contract)
	if err != nil {
		return "", fmt.Errorf("workspace: render the workspace document: %w", err)
	}
	doc = bindFormsForLocale(doc, csrf, worker, page.Locale.Resolved)
	doc = localizeDocument(doc, page.Locale, page.Query, page.TranslationDiagnostics)
	if enhanced {
		doc = strings.Replace(doc, "</body>", loaderScript()+"\n</body>", 1)
	}
	return doc, nil
}

// bindForms retargets the frozen renderer's forms at this workspace's routes
// and injects the hidden inputs a submission needs.
//
// The frozen template puts the request fields in one form and each action's
// button in its own sibling form, which is correct for a renderer that knows
// nothing about where it is served but would submit an empty request here. The
// binding gives the request form an id and makes every action control name it
// as its form owner, so pressing an action submits the fields the person
// filled in. That is a plain HTML5 mechanism; nothing here needs script.
func bindForms(doc, csrf, worker string) string {
	return bindFormsForLocale(doc, csrf, worker, "")
}

func bindFormsForLocale(doc, csrf, worker, locale string) string {
	hidden := hiddenInput(ParamCSRF, csrf) + hiddenInput(ParamWorker, worker)
	if locale != "" {
		hidden += hiddenInput(ParamLocale, locale)
	}

	replacements := []struct{ from, to string }{
		// The action forms first: their opening tag is a superset of the
		// request form's, so replacing the shorter literal first would
		// rewrite half of the longer one.
		{
			`<form method="post" action="#" style="display:inline">`,
			`<form method="post" action="` + PathSimulate + `" style="display:inline">`,
		},
		{
			`<form method="post" action="#">`,
			`<form id="` + formID + `" method="post" action="` + PathSimulate + `">` + hidden,
		},
		{
			`<input type="hidden" name="transition" value="`,
			`<input form="` + formID + `" type="hidden" name="transition" value="`,
		},
		{
			`<input type="text" id="reason-`,
			`<input form="` + formID + `" type="text" id="reason-`,
		},
		{
			`<button type="submit" data-variant="`,
			`<button form="` + formID + `" type="submit" data-variant="`,
		},
	}
	for _, r := range replacements {
		doc = strings.ReplaceAll(doc, r.from, r.to)
	}
	return doc
}

// localizeDocument annotates a frozen SSR document with presentation-only
// facts. The canonical date remains in its native date control, while a
// named-month label makes the localized display and the submitted ISO value
// independently visible. This keeps the browser's date semantics intact and
// makes format conversion auditable without JavaScript.
func localizeDocument(doc string, locale LocaleContext, query Query, diagnostics []TranslationDiagnostic) string {
	if locale.Resolved == "" {
		return doc
	}
	doc = strings.Replace(doc, `<html lang="en">`, `<html lang="`+html.EscapeString(locale.Resolved)+`">`, 1)

	var notice strings.Builder
	notice.WriteString(`<section class="locale-context" data-locale="`)
	notice.WriteString(html.EscapeString(locale.Resolved))
	notice.WriteString(`" role="status"><p>Display locale: `)
	notice.WriteString(html.EscapeString(locale.Resolved))
	notice.WriteString(`.</p>`)
	if locale.Fallback == LocaleFallbackUnsupported {
		notice.WriteString(`<p data-locale-fallback="`)
		notice.WriteString(html.EscapeString(string(locale.Fallback)))
		notice.WriteString(`">Requested locale `)
		notice.WriteString(html.EscapeString(locale.Requested))
		notice.WriteString(` is unsupported; using `)
		notice.WriteString(html.EscapeString(locale.Resolved))
		notice.WriteString(`.</p>`)
	}
	if formattedDate, err := FormatDate(locale, query.EffectiveDate); err == nil {
		notice.WriteString(`<p>Effective date display: <time datetime="`)
		notice.WriteString(html.EscapeString(query.EffectiveDate))
		notice.WriteString(`">`)
		notice.WriteString(html.EscapeString(formattedDate))
		notice.WriteString(`</time>. Submitted as canonical ISO `)
		notice.WriteString(html.EscapeString(query.EffectiveDate))
		notice.WriteString(`.</p>`)
	}
	for _, diagnostic := range diagnostics {
		notice.WriteString(`<p class="error" data-l10n-missing="`)
		notice.WriteString(html.EscapeString(diagnostic.Key))
		notice.WriteString(`">Missing translation for `)
		notice.WriteString(html.EscapeString(diagnostic.Key))
		notice.WriteString(`; English fallback: `)
		notice.WriteString(html.EscapeString(diagnostic.Fallback))
		notice.WriteString(`.</p>`)
	}
	notice.WriteString(`</section>`)
	return strings.Replace(doc, `<main id="main-content">`, `<main id="main-content">`+notice.String(), 1)
}

func hiddenInput(name, value string) string {
	return `<input type="hidden" name="` + html.EscapeString(name) +
		`" value="` + html.EscapeString(value) + `">`
}

// loaderScript is the one inline script the content-security-policy admits: a
// progressive-enhancement loader that starts the GWC/WASM renderer over an
// already-complete document.
//
// It is emitted only when both bundle halves are embedded, and it never
// removes anything: if instantiation fails, the server-rendered page is still
// the page. The script is a constant so its hash can be pinned in the policy;
// a loader assembled at run time could not be.
func loaderScript() string {
	return "<script>" + loaderSource + "</script>"
}

const loaderSource = `(function(){` +
	`if(!window.WebAssembly||!window.Go){return}` +
	`var g=new window.Go();` +
	`window.WebAssembly.instantiateStreaming(fetch("` + PathWasm + `"),g.importObject)` +
	`.then(function(r){g.run(r.instance)}).catch(function(){});` +
	`})();`

// ContentSecurityPolicy returns the policy header value for a workspace
// document.
//
// It is a deny-by-default policy with two exact allowances, both pinned by
// hash rather than by 'unsafe-inline': the frozen stylesheet the renderer
// inlines, and - only when the bundle is served - the progressive-enhancement
// loader and the wasm_exec shim it needs. Everything else, including images,
// fonts, frames and any other origin, is refused.
func ContentSecurityPolicy(enhanced bool) string {
	directives := []string{
		"default-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"style-src '" + stylesheetHash + "'",
	}
	if enhanced {
		directives = append(directives,
			"script-src '"+sha256Source(loaderSource)+"' 'self' 'wasm-unsafe-eval'",
			"connect-src 'self'")
	} else {
		directives = append(directives, "script-src 'none'")
	}
	return strings.Join(directives, "; ")
}

// stylesheetHash pins the exact stylesheet the frozen renderer inlines. It is
// computed once from tokens.WorkspaceCSS(), which is the same string the
// renderer emits, so the policy cannot drift from the document.
var stylesheetHash = sha256Source(tokens.WorkspaceCSS())

// sha256Source renders one CSP source expression for an inline block.
func sha256Source(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}
