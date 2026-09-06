package binding

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/capability"
)

// WireMethod is one method of one generated service descriptor: the exact
// wire identity a capability must bind to. Streaming distinguishes a
// grpc.StreamDesc entry from a grpc.MethodDesc entry, which matters because
// a streaming method can never carry a unary request/result capability.
type WireMethod struct {
	ServiceFullName string
	MethodName      string
	Streaming       bool
}

// Ref renders the canonical "<ServiceFullName>/<MethodName>" identity used
// everywhere else in this repository (internal/transport/manifest's
// EndpointID, tools/uxqual/pagedef's RPCRef).
func (w WireMethod) Ref() string { return w.ServiceFullName + "/" + w.MethodName }

// Valid reports whether both halves of the wire identity are present.
func (w WireMethod) Valid() bool { return w.ServiceFullName != "" && w.MethodName != "" }

// HandlerSymbol names one typed Go function or method by the three things
// needed to find it in the tree and to prove it exists: the module-relative
// package path, the receiver type as written in the source (empty for a
// plain function, "*server" or "server" for a method), and the symbol name.
type HandlerSymbol struct {
	// PackagePath is module-relative, e.g. "internal/intent/app". It is
	// never an import path with the module prefix, so a symbol reference
	// reads the same in a report as it does in a directory listing.
	PackagePath string
	// Receiver is the receiver type expression exactly as the source spells
	// it, including the pointer star: "*domainHandlers". Empty means a
	// package-level function.
	Receiver string
	Name     string
}

// Ref renders the canonical symbol identity: "<pkg>.(<recv>).<Name>" for a
// method, "<pkg>.<Name>" for a function.
func (h HandlerSymbol) Ref() string {
	if h.Receiver == "" {
		return h.PackagePath + "." + h.Name
	}
	return h.PackagePath + ".(" + h.Receiver + ")." + h.Name
}

// Valid reports whether the symbol names both a package and a name.
func (h HandlerSymbol) Valid() bool { return h.PackagePath != "" && h.Name != "" }

// HandlerIndex is the set of typed Go handler symbols found in the live
// source tree, keyed by [HandlerSymbol.Ref]. [ScanHandlerSymbols] builds
// one from already-read sources; the tests in this package supply the file
// reading, keeping the package itself pure.
//
// A nil HandlerIndex means "symbol existence was not checked" and is
// reported as such rather than treated as "every symbol exists": see
// [BuildFrom].
type HandlerIndex map[string]bool

// Has reports whether the index contains sym.
func (h HandlerIndex) Has(sym HandlerSymbol) bool { return h[sym.Ref()] }

// Refs returns every indexed symbol reference, sorted.
func (h HandlerIndex) Refs() []string {
	out := make([]string, 0, len(h))
	for ref := range h {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

// Claim is one reviewed statement about how a published capability reaches
// the wire and the Go type system today. A Claim is evidence, not a wish:
// every WireMethod must exist in the compiled descriptors and every Handler
// must exist in the scanned tree, and a Claim that names two of either is a
// [GapAmbiguousWireMethod] or [GapAmbiguousHandler], never a silent
// first-wins pick.
type Claim struct {
	CapabilityID string
	// DefinitionRef is the intent definition ("<intent_type_id>/v<version>")
	// whose model binding supplies this capability's entities and
	// properties. Empty means the capability has no drafted definition, and
	// therefore no resolvable model binding.
	DefinitionRef string
	// WireMethods are the candidate wire identities ([WireMethod.Ref]) this
	// capability is reachable through today.
	WireMethods []string
	// Handlers are the candidate typed Go symbols that implement it.
	Handlers []HandlerSymbol
	// Rationale cites the source of the claim so a reader can check it
	// against the tree rather than trusting this file.
	Rationale string
}

// GapKind is one exhaustive reason a capability, handler or wire method
// failed to bind. There is no "other".
type GapKind string

// The twelve declared gap kinds.
const (
	// GapUnclaimedCapability: the registry publishes a capability that no
	// Claim describes at all.
	GapUnclaimedCapability GapKind = "UNCLAIMED_CAPABILITY"
	// GapNoWireMethod: the claim names zero wire methods.
	GapNoWireMethod GapKind = "NO_WIRE_METHOD"
	// GapAmbiguousWireMethod: the claim names more than one wire method, so
	// "the" descriptor for this capability does not exist.
	GapAmbiguousWireMethod GapKind = "AMBIGUOUS_WIRE_METHOD"
	// GapUnknownWireMethod: the claim names a wire method absent from the
	// compiled service descriptors (a stale or misspelled claim).
	GapUnknownWireMethod GapKind = "UNKNOWN_WIRE_METHOD"
	// GapStreamingWireMethod: the claim binds a unary request/result
	// capability to a server-streaming method.
	GapStreamingWireMethod GapKind = "STREAMING_WIRE_METHOD"
	// GapNoHandler: the claim names zero typed Go handler symbols.
	GapNoHandler GapKind = "NO_HANDLER"
	// GapAmbiguousHandler: the claim names more than one typed Go symbol.
	GapAmbiguousHandler GapKind = "AMBIGUOUS_HANDLER"
	// GapMissingHandlerSymbol: the claimed symbol does not exist in the
	// scanned source tree (a dangling binding).
	GapMissingHandlerSymbol GapKind = "MISSING_HANDLER_SYMBOL"
	// GapHandlerBoundTwice: one typed Go symbol is claimed by two or more
	// capabilities, so invoking it cannot identify which capability ran.
	GapHandlerBoundTwice GapKind = "HANDLER_BOUND_TWICE"
	// GapNoModelBinding: the capability resolves to no model binding —
	// either it names no intent definition, or internal/intent/modelbinding
	// could not resolve the one it names.
	GapNoModelBinding GapKind = "NO_MODEL_BINDING"
	// GapWireMethodUnbound: a method exists in a registered service
	// descriptor that no capability claims.
	GapWireMethodUnbound GapKind = "WIRE_METHOD_UNBOUND"
	// GapClaimWithoutCapability: a claim names a capability id and version
	// the registry does not publish — a stale claim that would otherwise
	// let an unregistered capability version look bound.
	GapClaimWithoutCapability GapKind = "CLAIM_WITHOUT_CAPABILITY"
)

// Valid reports whether k is one of the twelve declared kinds.
func (k GapKind) Valid() bool {
	switch k {
	case GapUnclaimedCapability, GapNoWireMethod, GapAmbiguousWireMethod,
		GapUnknownWireMethod, GapStreamingWireMethod, GapNoHandler,
		GapAmbiguousHandler, GapMissingHandlerSymbol, GapHandlerBoundTwice,
		GapNoModelBinding, GapWireMethodUnbound, GapClaimWithoutCapability:
		return true
	default:
		return false
	}
}

// Gap is one binding this table refuses to assert.
type Gap struct {
	Kind GapKind
	// Capability is the capability id the gap is about, empty for a
	// [GapWireMethodUnbound] (which is about a method, not a capability).
	Capability string
	// Subject is the wire ref, handler ref or definition ref the gap names,
	// so two gaps of the same kind on the same capability stay distinct.
	Subject string
	Detail  string
}

// ID is the gap's stable identity: "<KIND>|<capability>|<subject>". The
// allowlist and the conformance check are keyed by it, so re-wording Detail
// never silently re-opens or re-closes a gap.
func (g Gap) ID() string {
	return string(g.Kind) + "|" + g.Capability + "|" + g.Subject
}

func (g Gap) String() string {
	return fmt.Sprintf("%s: %s", g.ID(), g.Detail)
}

// Entry is one fully bound capability: all three facts present, unambiguous
// and verified. A capability with even one [Gap] produces no Entry.
type Entry struct {
	Capability  capability.Key
	OwnerDomain string
	EffectClass capability.EffectClass
	// DefinitionRef is the intent definition supplying the model binding.
	DefinitionRef string
	Wire          WireMethod
	Handler       HandlerSymbol
	// Entities are the generated model entity refs ("Name/vN") the
	// capability's definition names as aggregate roots.
	Entities []string
	// ReadProperties and WriteProperties are generated property refs
	// ("entity_key.path"), sorted as the model binding produced them.
	ReadProperties  []string
	WriteProperties []string
}

// Table is the whole answer: every fully bound capability, and every gap
// found across capabilities, handlers and wire methods.
type Table struct {
	// SymbolsChecked records whether a non-nil [HandlerIndex] was supplied.
	// A table built without one cannot have found a
	// [GapMissingHandlerSymbol], and says so rather than implying the tree
	// was clean.
	SymbolsChecked bool
	Entries        []Entry
	Gaps           []Gap
}

// GapIDs returns every gap's [Gap.ID], sorted and de-duplicated.
func (t Table) GapIDs() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(t.Gaps))
	for _, g := range t.Gaps {
		if seen[g.ID()] {
			continue
		}
		seen[g.ID()] = true
		out = append(out, g.ID())
	}
	sort.Strings(out)
	return out
}

// GapsOfKind returns every gap of one kind, in table order.
func (t Table) GapsOfKind(kind GapKind) []Gap {
	out := make([]Gap, 0, len(t.Gaps))
	for _, g := range t.Gaps {
		if g.Kind == kind {
			out = append(out, g)
		}
	}
	return out
}

// FullyBound reports whether every published capability bound cleanly and
// no wire method was left unbound.
func (t Table) FullyBound() bool { return len(t.Gaps) == 0 }

// Explain renders the table as a deterministic, human-readable account: one
// line per bound capability naming its wire method, handler symbol and
// model footprint, then one line per gap. It is the report a reviewer reads
// instead of re-deriving the join by hand.
func (t Table) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "capability binding table: %d bound, %d gaps (handler symbols %s)\n",
		len(t.Entries), len(t.Gaps), checkedWord(t.SymbolsChecked))
	for _, e := range t.Entries {
		fmt.Fprintf(&b, "BOUND %s -> wire %s -> handler %s\n",
			e.Capability.String(), e.Wire.Ref(), e.Handler.Ref())
		fmt.Fprintf(&b, "      definition %s; entities [%s]; reads [%s]; writes [%s]\n",
			refOrNone(e.DefinitionRef),
			strings.Join(e.Entities, " "),
			strings.Join(e.ReadProperties, " "),
			strings.Join(e.WriteProperties, " "))
	}
	for _, g := range t.Gaps {
		fmt.Fprintf(&b, "GAP   %s: %s\n", g.ID(), g.Detail)
	}
	return b.String()
}

func checkedWord(checked bool) string {
	if checked {
		return "verified against the source tree"
	}
	return "NOT verified: no handler index supplied"
}

func refOrNone(ref string) string {
	if ref == "" {
		return "(none)"
	}
	return ref
}
