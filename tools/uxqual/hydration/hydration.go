// Package hydration verifies the semantic boundary between the server's
// HTML and the post-mount GWC tree. It deliberately compares the governed
// page skeleton and state-bearing controls, not ordinary widget internals: a
// registered widget may replace a placeholder's interior while its slot and
// security boundary remain stable.
//
// The package has no business or authorization authority. Contract is built
// from a validated pagedef.PageDefinition and carries already-resolved
// authorization dispositions and browser-state preservation obligations.
// HTML is untrusted input. Evidence must live under the governed page scope,
// and executable markup, ambiguous attributes, detached state markers, and
// missing, duplicate, malformed, or mismatched semantics are refused through
// stable errors that never include source markup or values.
package hydration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"github.com/monstercameron/hcm-next/tools/uxqual/ssrshell"
)

// ErrorCode identifies a stable refusal class. Error messages intentionally
// contain only schema field names; untrusted page markup and values never
// cross this package's diagnostic boundary.
type ErrorCode string

const (
	ErrorInvalid   ErrorCode = "invalid"
	ErrorMissing   ErrorCode = "missing"
	ErrorDuplicate ErrorCode = "duplicate"
	ErrorMismatch  ErrorCode = "mismatch"
	ErrorUntrusted ErrorCode = "untrusted"
)

var (
	ErrInvalid   = errors.New("hydration: invalid evidence")
	ErrMissing   = errors.New("hydration: missing evidence")
	ErrDuplicate = errors.New("hydration: duplicate evidence")
	ErrMismatch  = errors.New("hydration: mismatched evidence")
	ErrUntrusted = errors.New("hydration: untrusted evidence")
)

// Error is a sanitized, errors.Is-compatible hydration refusal.
type Error struct {
	Code  ErrorCode
	Field string
}

func (e *Error) Error() string { return "hydration: " + string(e.Code) + " " + e.Field }

func (e *Error) Unwrap() error {
	switch e.Code {
	case ErrorInvalid:
		return ErrInvalid
	case ErrorMissing:
		return ErrMissing
	case ErrorDuplicate:
		return ErrDuplicate
	case ErrorMismatch:
		return ErrMismatch
	case ErrorUntrusted:
		return ErrUntrusted
	default:
		return ErrInvalid
	}
}

func refusal(code ErrorCode, field string) error { return &Error{Code: code, Field: field} }

// PageIdentity is the immutable identity the two renderers must share.
type PageIdentity struct {
	PageID  string `json:"page_id"`
	Version int    `json:"version"`
	Digest  string `json:"digest"`
}

// Region is the semantic region projection. Widget content is intentionally
// absent; only the ordered landmark, accessible name, heading, and slot
// identity are governed here.
type Region struct {
	ID          string       `json:"id"`
	Landmark    string       `json:"landmark"`
	AriaLabel   string       `json:"aria_label"`
	Heading     *Heading     `json:"heading,omitempty"`
	WidgetSlots []WidgetSlot `json:"widget_slots,omitempty"`
}

type Heading struct {
	Level int    `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

type WidgetSlot struct {
	ID   string `json:"id"`
	Ref  string `json:"ref"`
	Role string `json:"role"`
}

// LiveRegion records the page's assistive-technology announcement contract.
type LiveRegion struct {
	Present bool   `json:"present"`
	Role    string `json:"role,omitempty"`
	Polite  string `json:"politeness,omitempty"`
	Atomic  string `json:"atomic,omitempty"`
}

// AuthorizationDisposition is a server-resolved marker. Disposition values
// are closed so presentation cannot invent a new authority outcome.
type AuthorizationDisposition struct {
	ID          string `json:"id"`
	Disposition string `json:"disposition"`
}

const (
	DispositionAllow          = "allow"
	DispositionDeny           = "deny"
	DispositionRestrict       = "restrict"
	DispositionReviewRequired = "review_required"
)

// Preservation is the explicit state hydration must retain. The checker
// reads values from real form-control attributes/text, errors from real live
// error nodes, and idempotency from hidden inputs. data-hydration-* attributes
// may associate a node with a field or mark the focus target, but may not
// self-assert a value or error string.
type Preservation struct {
	FocusID         string            `json:"focus_id,omitempty"`
	FormValues      map[string]string `json:"form_values,omitempty"`
	FieldErrors     map[string]string `json:"field_errors,omitempty"`
	IdempotencyKeys map[string]string `json:"idempotency_keys,omitempty"`
}

// Contract is the renderer-independent parity contract. Use FromPage so page
// identity, digest, region landmarks, headings, slots, and live-region
// semantics all come from the governed PageDefinition rather than markup.
type Contract struct {
	Identity       PageIdentity               `json:"identity"`
	Regions        []Region                   `json:"regions"`
	LiveRegion     LiveRegion                 `json:"live_region"`
	Authorizations []AuthorizationDisposition `json:"authorizations,omitempty"`
	Preservation   Preservation               `json:"preservation"`
}

// FromPage creates a parity contract from a validated governed page. The
// authorization and preservation arguments are already-resolved evidence;
// this function does not evaluate policy or grant authority.
func FromPage(pd pagedef.PageDefinition, authorizations []AuthorizationDisposition, preservation Preservation) (Contract, error) {
	if violations := pd.Validate(); len(violations) != 0 {
		return Contract{}, refusal(ErrorInvalid, "page_definition")
	}
	regions := make([]Region, 0, len(pd.Regions))
	for _, r := range pd.Regions {
		tag, label, ok := ssrshell.LandmarkForRegionKind(r.Kind)
		if !ok {
			return Contract{}, refusal(ErrorInvalid, "regions.landmark")
		}
		out := Region{ID: r.ID, Landmark: tag, AriaLabel: label + ": " + r.ID}
		if r.Heading != nil {
			out.Heading = &Heading{Level: r.Heading.Level, ID: "heading-" + r.ID, Text: r.Heading.Text}
		}
		for _, slot := range r.Widgets {
			out.WidgetSlots = append(out.WidgetSlots, WidgetSlot{ID: slot.ID, Ref: slot.WidgetRef, Role: "presentation"})
		}
		regions = append(regions, out)
	}
	live := LiveRegion{}
	switch pd.Accessibility.LiveRegion {
	case pagedef.LiveRegionOff:
	case pagedef.LiveRegionPolite:
		live = LiveRegion{Present: true, Role: "status", Polite: "polite", Atomic: "true"}
	case pagedef.LiveRegionAssertive:
		live = LiveRegion{Present: true, Role: "alert", Polite: "assertive", Atomic: "true"}
	default:
		return Contract{}, refusal(ErrorInvalid, "live_region")
	}
	c := Contract{
		Identity:       PageIdentity{PageID: pd.PageID, Version: pd.Version, Digest: pd.Digest()},
		Regions:        regions,
		LiveRegion:     live,
		Authorizations: append([]AuthorizationDisposition(nil), authorizations...),
		Preservation:   clonePreservation(preservation),
	}
	if err := validateContract(c); err != nil {
		return Contract{}, err
	}
	return c, nil
}

// Projection is the normalized semantic view extracted from one document.
type Projection struct {
	Identity       PageIdentity               `json:"identity"`
	Regions        []Region                   `json:"regions"`
	LiveRegion     LiveRegion                 `json:"live_region"`
	Authorizations []AuthorizationDisposition `json:"authorizations,omitempty"`
	Preservation   Preservation               `json:"preservation"`
}

// Report is successful parity evidence. Digest is over the normalized
// semantic projection, not raw HTML, so safe widget-interior differences may
// occur without weakening the governed proof.
type Report struct {
	Equal  bool       `json:"equal"`
	Digest string     `json:"digest"`
	SSR    Projection `json:"ssr"`
	GWC    Projection `json:"gwc"`
}

// Compare parses both documents and proves exact semantic parity.
func Compare(ssrHTML, gwcHTML string, c Contract) (Report, error) {
	if err := validateContract(c); err != nil {
		return Report{}, err
	}
	ssr, err := project(ssrHTML, c, true)
	if err != nil {
		return Report{}, err
	}
	gwc, err := project(gwcHTML, c, false)
	if err != nil {
		return Report{}, err
	}
	if !semanticEqual(ssr, gwc) {
		return Report{}, refusal(ErrorMismatch, "projection")
	}
	digest, err := semanticDigest(ssr)
	if err != nil {
		return Report{}, refusal(ErrorInvalid, "projection_digest")
	}
	return Report{Equal: true, Digest: digest, SSR: ssr, GWC: gwc}, nil
}

// Check is an alias suitable for gate runners.
func Check(ssrHTML, gwcHTML string, c Contract) (Report, error) {
	return Compare(ssrHTML, gwcHTML, c)
}

// ProjectSSR extracts and validates a server document's normalized semantic
// projection.
func ProjectSSR(document string, c Contract) (Projection, error) {
	if err := validateContract(c); err != nil {
		return Projection{}, err
	}
	return project(document, c, true)
}

// ProjectGWC extracts and validates a mounted GWC snapshot. Ordinary widget
// interiors may differ, but the safety and state-bearing checks still apply.
func ProjectGWC(document string, c Contract) (Projection, error) {
	if err := validateContract(c); err != nil {
		return Projection{}, err
	}
	return project(document, c, false)
}

const (
	maxDocumentBytes = 8 << 20
	maxDOMNodes      = 100_000
	maxDOMDepth      = 256
	maxAttributes    = 128
	maxIdentifier    = 512
	maxEvidenceValue = 1 << 20
	maxRegions       = 256
	maxSlots         = 4096
	maxEvidenceItems = 4096
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validateContract(c Contract) error {
	if !validIdentifier(c.Identity.PageID) {
		return refusal(ErrorInvalid, "identity.page_id")
	}
	if c.Identity.Version < 1 {
		return refusal(ErrorInvalid, "identity.version")
	}
	if !digestPattern.MatchString(c.Identity.Digest) {
		return refusal(ErrorInvalid, "identity.digest")
	}
	if len(c.Regions) == 0 {
		return refusal(ErrorMissing, "regions")
	}
	if len(c.Regions) > maxRegions {
		return refusal(ErrorInvalid, "regions")
	}
	seenRegions := make(map[string]bool, len(c.Regions))
	seenDOMIDs := map[string]bool{"page-" + c.Identity.PageID: true}
	totalSlots := 0
	for _, r := range c.Regions {
		if !validIdentifier(r.ID) {
			return refusal(ErrorInvalid, "regions.id")
		}
		if seenRegions[r.ID] {
			return refusal(ErrorDuplicate, "regions.id")
		}
		seenRegions[r.ID] = true
		knownLandmark := false
		for _, tag := range ssrshell.LandmarkTags {
			if r.Landmark == tag {
				knownLandmark = true
				break
			}
		}
		if !knownLandmark {
			return refusal(ErrorUntrusted, "regions.landmark")
		}
		if !validText(r.AriaLabel) || r.AriaLabel == "" {
			return refusal(ErrorInvalid, "regions.aria_label")
		}
		domID := regionDOMID(r)
		if seenDOMIDs[domID] {
			return refusal(ErrorDuplicate, "regions.dom_id")
		}
		seenDOMIDs[domID] = true
		if r.Heading != nil {
			if r.Heading.Level < 1 || r.Heading.Level > 6 || r.Heading.ID != "heading-"+r.ID || !validText(r.Heading.Text) {
				return refusal(ErrorInvalid, "regions.heading")
			}
			if seenDOMIDs[r.Heading.ID] {
				return refusal(ErrorDuplicate, "regions.heading.id")
			}
			seenDOMIDs[r.Heading.ID] = true
		}
		slots := make(map[string]bool, len(r.WidgetSlots))
		for _, slot := range r.WidgetSlots {
			if !validIdentifier(slot.ID) || !validIdentifier(slot.Ref) || slot.Role != "presentation" {
				return refusal(ErrorInvalid, "widget_slots")
			}
			if slots[slot.ID] {
				return refusal(ErrorDuplicate, "widget_slots.id")
			}
			slots[slot.ID] = true
			totalSlots++
		}
	}
	if totalSlots > maxSlots {
		return refusal(ErrorInvalid, "widget_slots")
	}
	switch c.LiveRegion {
	case LiveRegion{}:
	case LiveRegion{Present: true, Role: "status", Polite: "polite", Atomic: "true"}:
	case LiveRegion{Present: true, Role: "alert", Polite: "assertive", Atomic: "true"}:
	default:
		return refusal(ErrorInvalid, "live_region")
	}
	if c.LiveRegion.Present && seenDOMIDs[ssrshell.LiveRegionElementID] {
		return refusal(ErrorDuplicate, "live_region.id")
	}
	seenAuth := make(map[string]bool, len(c.Authorizations))
	if len(c.Authorizations) > maxEvidenceItems {
		return refusal(ErrorInvalid, "authorizations")
	}
	for _, a := range c.Authorizations {
		if !validIdentifier(a.ID) {
			return refusal(ErrorInvalid, "authorizations.id")
		}
		if seenAuth[a.ID] {
			return refusal(ErrorDuplicate, "authorizations.id")
		}
		seenAuth[a.ID] = true
		if !validDisposition(a.Disposition) {
			return refusal(ErrorUntrusted, "authorizations.disposition")
		}
	}
	return validatePreservation(c.Preservation)
}

func validatePreservation(p Preservation) error {
	if p.FocusID != "" && !validIdentifier(p.FocusID) {
		return refusal(ErrorUntrusted, "preservation.focus_id")
	}
	fields := []struct {
		name   string
		values map[string]string
	}{
		{name: "form_values", values: p.FormValues},
		{name: "field_errors", values: p.FieldErrors},
		{name: "idempotency_keys", values: p.IdempotencyKeys},
	}
	total := 0
	for _, field := range fields {
		for key, value := range field.values {
			if !validIdentifier(key) {
				return refusal(ErrorUntrusted, "preservation."+field.name+".key")
			}
			if !validEvidenceValue(value) {
				return refusal(ErrorUntrusted, "preservation."+field.name+".value")
			}
			if field.name == "idempotency_keys" && value == "" {
				return refusal(ErrorUntrusted, "preservation.idempotency_keys.value")
			}
			total++
		}
	}
	if total > maxEvidenceItems {
		return refusal(ErrorInvalid, "preservation")
	}
	return nil
}

func validIdentifier(s string) bool {
	if s == "" || len(s) > maxIdentifier || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u202a' || r == '\u202b' || r == '\u202d' || r == '\u202e' || r == '\u2066' || r == '\u2067' || r == '\u2068' || r == '\u2069' {
			return false
		}
	}
	return true
}

func validText(s string) bool {
	return len(s) <= maxEvidenceValue && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func validEvidenceValue(s string) bool { return validText(s) }

func validDisposition(s string) bool {
	switch s {
	case DispositionAllow, DispositionDeny, DispositionRestrict, DispositionReviewRequired:
		return true
	default:
		return false
	}
}

func clonePreservation(p Preservation) Preservation {
	return Preservation{
		FocusID:         p.FocusID,
		FormValues:      cloneMap(p.FormValues),
		FieldErrors:     cloneMap(p.FieldErrors),
		IdempotencyKeys: cloneMap(p.IdempotencyKeys),
	}
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type pageIsland struct {
	PageID  string
	Version int
	Digest  string
}

func project(source string, c Contract, ssr bool) (Projection, error) {
	document, err := parseHTML(source)
	if err != nil {
		return Projection{}, err
	}
	scope, err := documentScope(document, c, ssr)
	if err != nil {
		return Projection{}, err
	}
	facts, err := inspectDOM(scope, ssr)
	if err != nil {
		return Projection{}, err
	}
	identity, err := identityFrom(scope, facts, c, ssr)
	if err != nil {
		return Projection{}, err
	}
	if err := validateScopeChildren(scope, facts, c, ssr); err != nil {
		return Projection{}, err
	}
	regions, slots, err := regionsFrom(facts, c)
	if err != nil {
		return Projection{}, err
	}
	live, err := liveFrom(facts, c)
	if err != nil {
		return Projection{}, err
	}
	auth, err := authFrom(scope, slots, c.Authorizations)
	if err != nil {
		return Projection{}, err
	}
	preserve, err := preservationFrom(scope, slots, c.Preservation)
	if err != nil {
		return Projection{}, err
	}
	return Projection{
		Identity:       identity,
		Regions:        regions,
		LiveRegion:     live,
		Authorizations: auth,
		Preservation:   preserve,
	}, nil
}

func parseHTML(source string) (*html.Node, error) {
	if strings.TrimSpace(source) == "" {
		return nil, refusal(ErrorMissing, "html")
	}
	if len(source) > maxDocumentBytes || !utf8.ValidString(source) || strings.ContainsRune(source, 0) {
		return nil, refusal(ErrorUntrusted, "html")
	}
	// html.Parse follows browser error recovery and, like browsers, keeps one
	// value when an element repeats an attribute. Inspect the token stream
	// first so an attacker cannot smuggle two identity/state values through
	// that lossy normalization.
	if err := preflightTokens(source); err != nil {
		return nil, err
	}
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return nil, refusal(ErrorInvalid, "html")
	}
	return root, nil
}

func preflightTokens(source string) error {
	z := html.NewTokenizer(strings.NewReader(source))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return nil
			}
			return refusal(ErrorInvalid, "html")
		case html.StartTagToken, html.SelfClosingTagToken:
			if err := checkRawAttributes(z.Raw()); err != nil {
				return err
			}
		}
	}
}

// x/net/html intentionally drops repeated attributes to match browser parse
// recovery. Raw token inspection preserves just enough lexical information to
// reject that ambiguity before it disappears; it does not attempt to parse a
// second HTML grammar.
func checkRawAttributes(raw []byte) error {
	i := 1 // opening '<'
	for i < len(raw) && !asciiSpace(raw[i]) && raw[i] != '>' && raw[i] != '/' {
		i++
	}
	seen := make(map[string]bool)
	count := 0
	for i < len(raw) {
		for i < len(raw) && asciiSpace(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] == '>' || (raw[i] == '/' && i+1 < len(raw) && raw[i+1] == '>') {
			return nil
		}
		start := i
		for i < len(raw) && !asciiSpace(raw[i]) && raw[i] != '=' && raw[i] != '>' && raw[i] != '/' {
			i++
		}
		if start == i {
			return refusal(ErrorUntrusted, "dom.attributes")
		}
		count++
		if count > maxAttributes {
			return refusal(ErrorUntrusted, "dom.attributes")
		}
		name := strings.ToLower(string(raw[start:i]))
		if seen[name] {
			return refusal(ErrorDuplicate, "dom.attributes")
		}
		seen[name] = true
		for i < len(raw) && asciiSpace(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '=' {
			continue
		}
		i++
		for i < len(raw) && asciiSpace(raw[i]) {
			i++
		}
		if i >= len(raw) {
			return refusal(ErrorInvalid, "html")
		}
		if raw[i] == '\'' || raw[i] == '"' {
			quote := raw[i]
			i++
			for i < len(raw) && raw[i] != quote {
				i++
			}
			if i >= len(raw) {
				return refusal(ErrorInvalid, "html")
			}
			i++
			continue
		}
		for i < len(raw) && !asciiSpace(raw[i]) && raw[i] != '>' {
			i++
		}
	}
	return nil
}

func asciiSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func documentScope(document *html.Node, c Contract, ssr bool) (*html.Node, error) {
	if ssr {
		doctypes := findNodes(document, func(n *html.Node) bool { return n.Type == html.DoctypeNode })
		if len(doctypes) == 0 {
			return nil, refusal(ErrorMissing, "document.doctype")
		}
		if len(doctypes) != 1 {
			return nil, refusal(ErrorDuplicate, "document.doctype")
		}
		if !strings.EqualFold(doctypes[0].Data, "html") {
			return nil, refusal(ErrorUntrusted, "document.doctype")
		}
		bodies := findNodes(document, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "body"
		})
		if len(bodies) == 0 {
			return nil, refusal(ErrorMissing, "document.body")
		}
		if len(bodies) != 1 {
			return nil, refusal(ErrorDuplicate, "document.body")
		}
		return bodies[0], nil
	}

	wantID := "page-" + c.Identity.PageID
	roots := findNodes(document, func(n *html.Node) bool {
		return n.Type == html.ElementNode && attr(n, "id") == wantID
	})
	if len(roots) == 0 {
		return nil, refusal(ErrorMissing, "identity.page_root")
	}
	if len(roots) != 1 {
		return nil, refusal(ErrorDuplicate, "identity.page_root")
	}
	if roots[0].Data != "div" {
		return nil, refusal(ErrorMismatch, "identity.page_root")
	}
	return roots[0], nil
}

type domFacts struct {
	scope *html.Node
	ids   map[string]*html.Node
}

func inspectDOM(scope *html.Node, ssr bool) (domFacts, error) {
	facts := domFacts{scope: scope, ids: make(map[string]*html.Node)}
	type item struct {
		n     *html.Node
		depth int
	}
	stack := []item{{n: scope}}
	nodes := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		nodes++
		if nodes > maxDOMNodes || current.depth > maxDOMDepth {
			return domFacts{}, refusal(ErrorUntrusted, "dom.complexity")
		}
		n := current.n
		if n.Type == html.ElementNode {
			if len(n.Attr) > maxAttributes {
				return domFacts{}, refusal(ErrorUntrusted, "dom.attributes")
			}
			seenAttrs := make(map[string]bool, len(n.Attr))
			for _, a := range n.Attr {
				key := a.Namespace + "\x00" + a.Key
				if seenAttrs[key] {
					return domFacts{}, refusal(ErrorDuplicate, "dom.attributes")
				}
				seenAttrs[key] = true
				lowerKey := strings.ToLower(a.Key)
				if a.Namespace == "" && strings.HasPrefix(lowerKey, "on") {
					return domFacts{}, refusal(ErrorUntrusted, "dom.event_handler")
				}
				if a.Namespace == "" && lowerKey == "srcdoc" {
					return domFacts{}, refusal(ErrorUntrusted, "dom.active_content")
				}
				if isURLAttribute(lowerKey) && dangerousURL(a.Val) {
					return domFacts{}, refusal(ErrorUntrusted, "dom.active_content")
				}
			}
			if id, present := attribute(n, "id"); present {
				if !validIdentifier(id) {
					return domFacts{}, refusal(ErrorUntrusted, "dom.id")
				}
				if facts.ids[id] != nil {
					return domFacts{}, refusal(ErrorDuplicate, "dom.id")
				}
				facts.ids[id] = n
			}
			if dangerousElement(n, ssr) {
				return domFacts{}, refusal(ErrorUntrusted, "dom.active_content")
			}
		}
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, item{n: child, depth: current.depth + 1})
		}
	}
	return facts, nil
}

func dangerousElement(n *html.Node, ssr bool) bool {
	switch n.Data {
	case "script":
		// JSON data islands are inert. Their identity, placement, uniqueness,
		// schema, and exact values are still checked separately.
		return !(ssr && attr(n, "type") == "application/json")
	case "iframe", "object", "embed", "applet", "base", "meta", "link", "template", "style":
		return true
	default:
		return false
	}
}

func isURLAttribute(key string) bool {
	switch key {
	case "href", "src", "action", "formaction", "poster", "xlink:href":
		return true
	default:
		return false
	}
}

func dangerousURL(value string) bool {
	var compact strings.Builder
	for _, r := range strings.ToLower(value) {
		if r <= ' ' || r == '\u007f' {
			continue
		}
		compact.WriteRune(r)
	}
	v := compact.String()
	return strings.HasPrefix(v, "javascript:") || strings.HasPrefix(v, "vbscript:") || strings.HasPrefix(v, "data:text/html")
}

func identityFrom(scope *html.Node, facts domFacts, c Contract, ssr bool) (PageIdentity, error) {
	if ssr {
		n := facts.ids[ssrshell.PageIslandElementID]
		if n == nil {
			return PageIdentity{}, refusal(ErrorMissing, "identity.page_island")
		}
		if n.Parent != scope || n.Data != "script" {
			return PageIdentity{}, refusal(ErrorUntrusted, "identity.page_island")
		}
		typ, ok := attribute(n, "type")
		if !ok {
			return PageIdentity{}, refusal(ErrorMissing, "identity.page_island.type")
		}
		if typ != "application/json" {
			return PageIdentity{}, refusal(ErrorUntrusted, "identity.page_island.type")
		}
		island, err := decodePageIsland(textContent(n))
		if err != nil {
			return PageIdentity{}, err
		}
		got := PageIdentity{PageID: island.PageID, Version: island.Version, Digest: island.Digest}
		if got != c.Identity {
			return PageIdentity{}, refusal(ErrorMismatch, "identity")
		}
		return got, nil
	}

	versionText, ok := attribute(scope, "data-page-version")
	if !ok {
		return PageIdentity{}, refusal(ErrorMissing, "identity.version")
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 || strconv.Itoa(version) != versionText {
		return PageIdentity{}, refusal(ErrorUntrusted, "identity.version")
	}
	digest, ok := attribute(scope, "data-page-digest")
	if !ok {
		return PageIdentity{}, refusal(ErrorMissing, "identity.digest")
	}
	if !digestPattern.MatchString(digest) {
		return PageIdentity{}, refusal(ErrorUntrusted, "identity.digest")
	}
	got := PageIdentity{PageID: c.Identity.PageID, Version: version, Digest: digest}
	if got != c.Identity {
		return PageIdentity{}, refusal(ErrorMismatch, "identity")
	}
	return got, nil
}

func decodePageIsland(source string) (pageIsland, error) {
	if len(source) > maxEvidenceValue {
		return pageIsland{}, refusal(ErrorUntrusted, "identity.page_island")
	}
	dec := json.NewDecoder(strings.NewReader(source))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return pageIsland{}, refusal(ErrorUntrusted, "identity.page_island")
	}
	var out pageIsland
	seen := map[string]bool{}
	for dec.More() {
		token, err := dec.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return pageIsland{}, refusal(ErrorUntrusted, "identity.page_island")
		}
		if seen[key] {
			return pageIsland{}, refusal(ErrorDuplicate, "identity.page_island")
		}
		seen[key] = true
		switch key {
		case "page_id":
			if err := dec.Decode(&out.PageID); err != nil {
				return pageIsland{}, refusal(ErrorUntrusted, "identity.page_id")
			}
		case "version":
			if err := dec.Decode(&out.Version); err != nil {
				return pageIsland{}, refusal(ErrorUntrusted, "identity.version")
			}
		case "digest":
			if err := dec.Decode(&out.Digest); err != nil {
				return pageIsland{}, refusal(ErrorUntrusted, "identity.digest")
			}
		default:
			return pageIsland{}, refusal(ErrorUntrusted, "identity.page_island")
		}
	}
	if _, err := dec.Token(); err != nil || ensureEOF(dec) != nil {
		return pageIsland{}, refusal(ErrorUntrusted, "identity.page_island")
	}
	for _, field := range []struct {
		name string
		set  bool
	}{{"page_id", seen["page_id"]}, {"version", seen["version"]}, {"digest", seen["digest"]}} {
		if !field.set {
			return pageIsland{}, refusal(ErrorMissing, "identity."+field.name)
		}
	}
	if !validIdentifier(out.PageID) || out.Version < 1 || !digestPattern.MatchString(out.Digest) {
		return pageIsland{}, refusal(ErrorUntrusted, "identity.page_island")
	}
	return out, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("trailing")
}

func validateScopeChildren(scope *html.Node, facts domFacts, c Contract, ssr bool) error {
	children, err := directElementChildren(scope)
	if err != nil {
		return err
	}
	position := 0
	expect := func(n *html.Node, field string) error {
		if n == nil {
			return refusal(ErrorMissing, field)
		}
		if position >= len(children) {
			return refusal(ErrorMissing, field)
		}
		if children[position] != n {
			return refusal(ErrorMismatch, field)
		}
		position++
		return nil
	}
	if ssr {
		if position >= len(children) {
			return refusal(ErrorMissing, "document.skip_link")
		}
		skip := children[position]
		if skip.Data != "a" || attr(skip, "href") != "#main-content" || textContent(skip) == "" {
			return refusal(ErrorMismatch, "document.skip_link")
		}
		position++
	}
	if c.LiveRegion.Present {
		if err := expect(facts.ids[ssrshell.LiveRegionElementID], "live_region.order"); err != nil {
			return err
		}
	}
	for _, region := range c.Regions {
		id := regionDOMID(region)
		n := facts.ids[id]
		if n == nil {
			return refusal(ErrorMissing, "regions.id")
		}
		if err := expect(n, "regions.order"); err != nil {
			return err
		}
	}
	if ssr {
		island := facts.ids[ssrshell.PageIslandElementID]
		if err := expect(island, "identity.page_island.order"); err != nil {
			return err
		}
	}
	if position != len(children) {
		return refusal(ErrorMismatch, "document.children")
	}
	return nil
}

func regionsFrom(facts domFacts, c Contract) ([]Region, []*html.Node, error) {
	out := make([]Region, 0, len(c.Regions))
	slots := make([]*html.Node, 0)
	validSlots := make(map[*html.Node]bool)
	for _, want := range c.Regions {
		n := facts.ids[regionDOMID(want)]
		if n == nil {
			return nil, nil, refusal(ErrorMissing, "regions.id")
		}
		got, regionSlots, err := regionFrom(n, want)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, got)
		for _, slot := range regionSlots {
			slots = append(slots, slot)
			validSlots[slot] = true
		}
	}
	var reservedErr error
	walk(facts.scope, func(n *html.Node) {
		if reservedErr != nil || n.Type != html.ElementNode {
			return
		}
		_, hasID := attribute(n, "data-slot-id")
		_, hasRef := attribute(n, "data-widget-ref")
		if !hasID && !hasRef {
			return
		}
		if !hasID || !hasRef {
			reservedErr = refusal(ErrorMissing, "widget_slots.attributes")
			return
		}
		if !validSlots[n] {
			reservedErr = refusal(ErrorUntrusted, "widget_slots.scope")
		}
	})
	if reservedErr != nil {
		return nil, nil, reservedErr
	}
	return out, slots, nil
}

func regionFrom(n *html.Node, want Region) (Region, []*html.Node, error) {
	if n.Data != want.Landmark {
		return Region{}, nil, refusal(ErrorMismatch, "regions.landmark")
	}
	label, ok := attribute(n, "aria-label")
	if !ok {
		return Region{}, nil, refusal(ErrorMissing, "regions.aria_label")
	}
	if label != want.AriaLabel {
		return Region{}, nil, refusal(ErrorMismatch, "regions.aria_label")
	}
	children, err := directElementChildren(n)
	if err != nil {
		return Region{}, nil, err
	}
	position := 0
	got := Region{ID: want.ID, Landmark: n.Data, AriaLabel: label}
	if want.Heading != nil {
		if position >= len(children) {
			return Region{}, nil, refusal(ErrorMissing, "regions.heading")
		}
		h := children[position]
		position++
		if h.Data != "h"+strconv.Itoa(want.Heading.Level) {
			return Region{}, nil, refusal(ErrorMismatch, "regions.heading.level")
		}
		id, ok := attribute(h, "id")
		if !ok {
			return Region{}, nil, refusal(ErrorMissing, "regions.heading.id")
		}
		if id != want.Heading.ID {
			return Region{}, nil, refusal(ErrorMismatch, "regions.heading.id")
		}
		for child := h.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode {
				return Region{}, nil, refusal(ErrorUntrusted, "regions.heading.content")
			}
		}
		got.Heading = &Heading{Level: want.Heading.Level, ID: id, Text: textContent(h)}
		if *got.Heading != *want.Heading {
			return Region{}, nil, refusal(ErrorMismatch, "regions.heading")
		}
	}
	if len(children)-position < len(want.WidgetSlots) {
		return Region{}, nil, refusal(ErrorMissing, "widget_slots")
	}
	if len(children)-position > len(want.WidgetSlots) {
		return Region{}, nil, refusal(ErrorMismatch, "regions.children")
	}
	slotNodes := make([]*html.Node, 0, len(want.WidgetSlots))
	for _, expected := range want.WidgetSlots {
		slotNode := children[position]
		position++
		if slotNode.Data != "div" {
			return Region{}, nil, refusal(ErrorMismatch, "widget_slots.element")
		}
		id, idOK := attribute(slotNode, "data-slot-id")
		ref, refOK := attribute(slotNode, "data-widget-ref")
		role, roleOK := attribute(slotNode, "role")
		if !idOK || !refOK || !roleOK {
			return Region{}, nil, refusal(ErrorMissing, "widget_slots.attributes")
		}
		gotSlot := WidgetSlot{ID: id, Ref: ref, Role: role}
		if gotSlot != expected {
			return Region{}, nil, refusal(ErrorMismatch, "widget_slots")
		}
		got.WidgetSlots = append(got.WidgetSlots, gotSlot)
		slotNodes = append(slotNodes, slotNode)
	}
	return got, slotNodes, nil
}

func liveFrom(facts domFacts, c Contract) (LiveRegion, error) {
	n := facts.ids[ssrshell.LiveRegionElementID]
	if !c.LiveRegion.Present {
		if n != nil {
			return LiveRegion{}, refusal(ErrorMismatch, "live_region")
		}
		return LiveRegion{}, nil
	}
	if n == nil {
		return LiveRegion{}, refusal(ErrorMissing, "live_region")
	}
	if n.Data != "div" {
		return LiveRegion{}, refusal(ErrorMismatch, "live_region.element")
	}
	role, roleOK := attribute(n, "role")
	polite, politeOK := attribute(n, "aria-live")
	atomic, atomicOK := attribute(n, "aria-atomic")
	if !roleOK || !politeOK || !atomicOK {
		return LiveRegion{}, refusal(ErrorMissing, "live_region.attributes")
	}
	got := LiveRegion{Present: true, Role: role, Polite: polite, Atomic: atomic}
	if got != c.LiveRegion {
		return LiveRegion{}, refusal(ErrorMismatch, "live_region")
	}
	return got, nil
}

func authFrom(scope *html.Node, slots []*html.Node, want []AuthorizationDisposition) ([]AuthorizationDisposition, error) {
	slotSet := pointerSet(slots)
	out := make([]AuthorizationDisposition, 0, len(want))
	seen := make(map[string]bool, len(want))
	var foundErr error
	walk(scope, func(n *html.Node) {
		if foundErr != nil || n.Type != html.ElementNode {
			return
		}
		id, hasID := attribute(n, "data-authz-id")
		disposition, hasDisposition := attribute(n, "data-authz-disposition")
		if !hasID && !hasDisposition {
			return
		}
		if !hasID || !hasDisposition {
			foundErr = refusal(ErrorMissing, "authorizations.attributes")
			return
		}
		if !insideSlot(n, slotSet) {
			foundErr = refusal(ErrorUntrusted, "authorizations.scope")
			return
		}
		if !validIdentifier(id) {
			foundErr = refusal(ErrorUntrusted, "authorizations.id")
			return
		}
		if seen[id] {
			foundErr = refusal(ErrorDuplicate, "authorizations.id")
			return
		}
		seen[id] = true
		if !validDisposition(disposition) {
			foundErr = refusal(ErrorUntrusted, "authorizations.disposition")
			return
		}
		out = append(out, AuthorizationDisposition{ID: id, Disposition: disposition})
	})
	if foundErr != nil {
		return nil, foundErr
	}
	if len(out) < len(want) {
		return nil, refusal(ErrorMissing, "authorizations")
	}
	if !equalAuth(out, want) {
		return nil, refusal(ErrorMismatch, "authorizations")
	}
	return out, nil
}

func preservationFrom(scope *html.Node, slots []*html.Node, want Preservation) (Preservation, error) {
	got := Preservation{
		FormValues:      make(map[string]string),
		FieldErrors:     make(map[string]string),
		IdempotencyKeys: make(map[string]string),
	}
	controls := make(map[string]*html.Node)
	errorIDs := make(map[string]string)
	slotSet := pointerSet(slots)
	var foundErr error
	walk(scope, func(n *html.Node) {
		if foundErr != nil || n.Type != html.ElementNode {
			return
		}

		if focusID, marked := attribute(n, "data-hydration-focus"); marked {
			id, hasID := attribute(n, "id")
			switch {
			case !insideSlot(n, slotSet):
				foundErr = refusal(ErrorUntrusted, "preservation.focus_id.scope")
			case got.FocusID != "":
				foundErr = refusal(ErrorDuplicate, "preservation.focus_id")
			case !hasID || !validIdentifier(focusID):
				foundErr = refusal(ErrorUntrusted, "preservation.focus_id")
			case id != focusID:
				foundErr = refusal(ErrorMismatch, "preservation.focus_id")
			case !focusable(n):
				foundErr = refusal(ErrorUntrusted, "preservation.focus_target")
			default:
				got.FocusID = focusID
			}
			if foundErr != nil {
				return
			}
		}

		if isValueControl(n) {
			typ := strings.ToLower(attr(n, "type"))
			if n.Data == "input" && typ == "hidden" {
				if _, marked := attribute(n, "data-hydration-field"); marked {
					foundErr = refusal(ErrorUntrusted, "preservation.form_values.element")
					return
				}
				if _, marked := attribute(n, "data-hydration-value"); marked {
					foundErr = refusal(ErrorUntrusted, "preservation.form_values.self_asserted")
					return
				}
			} else {
				field, marked := attribute(n, "data-hydration-field")
				id, hasID := attribute(n, "id")
				if marked {
					if !hasID || !validIdentifier(field) {
						foundErr = refusal(ErrorUntrusted, "preservation.form_values.field")
						return
					}
					if id != field {
						foundErr = refusal(ErrorMismatch, "preservation.form_values.field")
						return
					}
				} else if hasID {
					field = id
				} else if name, ok := attribute(n, "name"); ok {
					field = name
				}
				if _, selfAsserted := attribute(n, "data-hydration-value"); selfAsserted {
					foundErr = refusal(ErrorUntrusted, "preservation.form_values.self_asserted")
					return
				}
				if field == "" || !validIdentifier(field) || !insideSlot(n, slotSet) {
					foundErr = refusal(ErrorUntrusted, "preservation.form_values.scope")
					return
				}
				if hasKey(got.FormValues, field) {
					foundErr = refusal(ErrorDuplicate, "preservation.form_values")
					return
				}
				value, err := controlValue(n)
				if err != nil || !validEvidenceValue(value) {
					foundErr = refusal(ErrorUntrusted, "preservation.form_values.value")
					return
				}
				got.FormValues[field] = value
				controls[field] = n
			}
		} else if _, marked := attribute(n, "data-hydration-field"); marked {
			foundErr = refusal(ErrorUntrusted, "preservation.form_values.element")
			return
		} else if _, marked := attribute(n, "data-hydration-value"); marked {
			foundErr = refusal(ErrorUntrusted, "preservation.form_values.self_asserted")
			return
		}

		errorField, markedError := attribute(n, "data-hydration-error-field")
		_, selfAssertedError := attribute(n, "data-hydration-error")
		if selfAssertedError {
			foundErr = refusal(ErrorUntrusted, "preservation.field_errors.self_asserted")
			return
		}
		id := attr(n, "id")
		role := attr(n, "role")
		if !markedError && id != "" && strings.HasSuffix(id, "-error") && (role == "alert" || role == "status") {
			errorField = strings.TrimSuffix(id, "-error")
			markedError = true
		}
		if markedError {
			if !insideSlot(n, slotSet) || !validIdentifier(errorField) || id == "" || (role != "alert" && role != "status") {
				foundErr = refusal(ErrorUntrusted, "preservation.field_errors.element")
				return
			}
			if hasKey(got.FieldErrors, errorField) {
				foundErr = refusal(ErrorDuplicate, "preservation.field_errors")
				return
			}
			value := textContent(n)
			if !validEvidenceValue(value) {
				foundErr = refusal(ErrorUntrusted, "preservation.field_errors.value")
				return
			}
			got.FieldErrors[errorField] = value
			errorIDs[errorField] = id
		}

		key, markedKey := attribute(n, "data-idempotency-key")
		standardKey := n.Data == "input" && attr(n, "name") == "idempotency_key"
		if markedKey || standardKey {
			id, hasID := attribute(n, "id")
			if standardKey {
				if !hasID {
					foundErr = refusal(ErrorMissing, "preservation.idempotency_keys.id")
					return
				}
				if markedKey && key != id {
					foundErr = refusal(ErrorMismatch, "preservation.idempotency_keys.id")
					return
				}
				key = id
			}
			value, hasValue := attribute(n, "value")
			if n.Data != "input" || strings.ToLower(attr(n, "type")) != "hidden" || !insideSlot(n, slotSet) || !validIdentifier(key) {
				foundErr = refusal(ErrorUntrusted, "preservation.idempotency_keys.element")
				return
			}
			if !hasValue {
				foundErr = refusal(ErrorMissing, "preservation.idempotency_keys.value")
				return
			}
			if value == "" || !validEvidenceValue(value) {
				foundErr = refusal(ErrorUntrusted, "preservation.idempotency_keys.value")
				return
			}
			if hasKey(got.IdempotencyKeys, key) {
				foundErr = refusal(ErrorDuplicate, "preservation.idempotency_keys")
				return
			}
			got.IdempotencyKeys[key] = value
		}
	})
	if foundErr != nil {
		return Preservation{}, foundErr
	}

	for field, errorID := range errorIDs {
		control := controls[field]
		if control == nil {
			return Preservation{}, refusal(ErrorMissing, "preservation.field_errors.control")
		}
		if attr(control, "aria-invalid") != "true" || (!tokenAttributeContains(control, "aria-describedby", errorID) && !tokenAttributeContains(control, "aria-errormessage", errorID)) {
			return Preservation{}, refusal(ErrorMismatch, "preservation.field_errors.relationship")
		}
	}
	if err := comparePreservation(got, want); err != nil {
		return Preservation{}, err
	}
	if len(got.FormValues) == 0 {
		got.FormValues = nil
	}
	if len(got.FieldErrors) == 0 {
		got.FieldErrors = nil
	}
	if len(got.IdempotencyKeys) == 0 {
		got.IdempotencyKeys = nil
	}
	return got, nil
}

func comparePreservation(got, want Preservation) error {
	if want.FocusID != "" && got.FocusID == "" {
		return refusal(ErrorMissing, "preservation.focus_id")
	}
	if got.FocusID != want.FocusID {
		return refusal(ErrorMismatch, "preservation.focus_id")
	}
	fields := []struct {
		name string
		got  map[string]string
		want map[string]string
	}{
		{name: "form_values", got: got.FormValues, want: want.FormValues},
		{name: "field_errors", got: got.FieldErrors, want: want.FieldErrors},
		{name: "idempotency_keys", got: got.IdempotencyKeys, want: want.IdempotencyKeys},
	}
	for _, field := range fields {
		if missingMapKey(field.got, field.want) {
			return refusal(ErrorMissing, "preservation."+field.name)
		}
		if !equalStringMap(field.got, field.want) {
			return refusal(ErrorMismatch, "preservation."+field.name)
		}
	}
	return nil
}

func controlValue(n *html.Node) (string, error) {
	switch n.Data {
	case "textarea":
		return textContent(n), nil
	case "select":
		var selected []string
		var first string
		haveFirst := false
		walk(n, func(option *html.Node) {
			if option.Type != html.ElementNode || option.Data != "option" {
				return
			}
			value, ok := attribute(option, "value")
			if !ok {
				value = textContent(option)
			}
			if !haveFirst {
				first, haveFirst = value, true
			}
			if _, ok := attribute(option, "selected"); ok {
				selected = append(selected, value)
			}
		})
		if _, multiple := attribute(n, "multiple"); multiple {
			encoded, err := json.Marshal(selected)
			return string(encoded), err
		}
		if len(selected) > 0 {
			return selected[0], nil
		}
		return first, nil
	case "input":
		typ := strings.ToLower(attr(n, "type"))
		if typ == "file" {
			return "", errors.New("file input")
		}
		value := attr(n, "value")
		if typ == "checkbox" || typ == "radio" {
			if _, checked := attribute(n, "checked"); !checked {
				return "", nil
			}
			if _, present := attribute(n, "value"); !present {
				return "on", nil
			}
		}
		return value, nil
	default:
		return "", errors.New("not a value control")
	}
}

func focusable(n *html.Node) bool {
	if _, disabled := attribute(n, "disabled"); disabled {
		return false
	}
	switch n.Data {
	case "input":
		return strings.ToLower(attr(n, "type")) != "hidden"
	case "textarea", "select", "button":
		return true
	case "a", "area":
		_, ok := attribute(n, "href")
		return ok
	}
	if value, ok := attribute(n, "tabindex"); ok {
		_, err := strconv.Atoi(value)
		return err == nil
	}
	if value, ok := attribute(n, "contenteditable"); ok {
		return value == "" || strings.EqualFold(value, "true")
	}
	return false
}

func isValueControl(n *html.Node) bool {
	return n.Data == "input" || n.Data == "textarea" || n.Data == "select"
}

func tokenAttributeContains(n *html.Node, name, token string) bool {
	for _, candidate := range strings.Fields(attr(n, name)) {
		if candidate == token {
			return true
		}
	}
	return false
}

func pointerSet(nodes []*html.Node) map[*html.Node]bool {
	out := make(map[*html.Node]bool, len(nodes))
	for _, n := range nodes {
		out[n] = true
	}
	return out
}

func insideSlot(n *html.Node, slots map[*html.Node]bool) bool {
	for current := n; current != nil; current = current.Parent {
		if slots[current] {
			return true
		}
	}
	return false
}

func regionDOMID(region Region) string {
	if region.Landmark == "main" {
		return "main-content"
	}
	return "region-" + region.ID
}

func directElementChildren(n *html.Node) ([]*html.Node, error) {
	var out []*html.Node
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.ElementNode:
			out = append(out, child)
		case html.TextNode:
			if strings.TrimSpace(child.Data) != "" {
				return nil, refusal(ErrorMismatch, "document.text")
			}
		}
	}
	return out, nil
}

func hasKey(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func missingMapKey(got, want map[string]string) bool {
	for key := range want {
		if _, ok := got[key]; !ok {
			return true
		}
	}
	return false
}

func equalAuth(a, b []AuthorizationDisposition) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func semanticEqual(a, b Projection) bool {
	if a.Identity != b.Identity || a.LiveRegion != b.LiveRegion || !equalAuth(a.Authorizations, b.Authorizations) || a.Preservation.FocusID != b.Preservation.FocusID || !equalStringMap(a.Preservation.FormValues, b.Preservation.FormValues) || !equalStringMap(a.Preservation.FieldErrors, b.Preservation.FieldErrors) || !equalStringMap(a.Preservation.IdempotencyKeys, b.Preservation.IdempotencyKeys) || len(a.Regions) != len(b.Regions) {
		return false
	}
	for i := range a.Regions {
		if a.Regions[i].ID != b.Regions[i].ID || a.Regions[i].Landmark != b.Regions[i].Landmark || a.Regions[i].AriaLabel != b.Regions[i].AriaLabel || !headingEqual(a.Regions[i].Heading, b.Regions[i].Heading) || len(a.Regions[i].WidgetSlots) != len(b.Regions[i].WidgetSlots) {
			return false
		}
		for j := range a.Regions[i].WidgetSlots {
			if a.Regions[i].WidgetSlots[j] != b.Regions[i].WidgetSlots[j] {
				return false
			}
		}
	}
	return true
}

func headingEqual(a, b *Heading) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func semanticDigest(p Projection) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func attribute(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func attr(n *html.Node, key string) string {
	value, _ := attribute(n, key)
	return value
}

func textContent(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
	})
	return b.String()
}

func findNodes(root *html.Node, predicate func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	walk(root, func(n *html.Node) {
		if predicate(n) {
			out = append(out, n)
		}
	})
	return out
}

// walk is iterative so an adversarially deep document cannot exhaust the Go
// stack before inspectDOM applies its explicit depth and node budgets.
func walk(n *html.Node, visit func(*html.Node)) {
	if n == nil {
		return
	}
	stack := []*html.Node{n}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		visit(current)
		for child := current.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, child)
		}
	}
}
