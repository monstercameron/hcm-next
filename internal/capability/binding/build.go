package binding

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	model "github.com/monstercameron/hcm-next/gen/go/hcmnext/model"
	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/intent/modelbinding"
)

// Build computes the live binding table: the BOOTSTRAP capability table
// (internal/capability), the compiled generated service descriptors of the
// four registered services, the reviewed [Claims] table, and the real model
// binding from internal/intent/modelbinding against the generated
// gen/go/hcmnext/model registry.
//
// index may be nil, in which case claimed handler symbols are not proved to
// exist and [Table.SymbolsChecked] is false — the table says so rather than
// implying a clean tree. This package's tests supply a real index built by
// walking the source tree, which is what makes a renamed or deleted handler
// fail the conformance check.
func Build(index HandlerIndex) (Table, error) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return Table{}, fmt.Errorf("binding: building the BOOTSTRAP capability registry: %w", err)
	}
	models, err := modelbinding.BindCatalog()
	if err != nil {
		return Table{}, fmt.Errorf("binding: binding the intent catalog to the generated model registry: %w", err)
	}
	return BuildFrom(registry.List(), WireMethods(), Claims(), models, index), nil
}

// BuildFrom is [Build]'s pure core: every input is explicit, so the binding
// rules can be proved against a controlled fixture without depending on the
// compiled BOOTSTRAP tables or the generated descriptors. It never fails —
// every problem is a typed [Gap], because "the join could not be computed"
// and "the join found a hole" must not look the same to a caller.
func BuildFrom(
	records []capability.Record,
	wire []WireMethod,
	claims []Claim,
	models modelbinding.Table,
	index HandlerIndex,
) Table {
	table := Table{SymbolsChecked: index != nil}

	wireByRef := make(map[string]WireMethod, len(wire))
	for _, m := range wire {
		wireByRef[m.Ref()] = m
	}
	claimByCapability := make(map[string]Claim, len(claims))
	for _, c := range claims {
		claimByCapability[c.CapabilityID] = c
	}
	modelByDefinition := make(map[string]modelbinding.ModelBinding, len(models.Bindings))
	for _, mb := range models.Bindings {
		modelByDefinition[mb.Definition.String()] = mb
	}

	claimedWire := map[string]bool{}
	handlerOwners := map[string]map[string]bool{}

	sorted := append([]capability.Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Definition.ID != sorted[j].Definition.ID {
			return sorted[i].Definition.ID < sorted[j].Definition.ID
		}
		return sorted[i].Definition.Version < sorted[j].Definition.Version
	})

	for _, rec := range sorted {
		def := rec.Definition
		claim, claimed := claimByCapability[def.ID]
		if !claimed {
			table.Gaps = append(table.Gaps, Gap{
				Kind: GapUnclaimedCapability, Capability: def.ID,
				Detail: "the registry publishes this capability but no reviewed claim names a wire method, a handler or a definition for it",
			})
			continue
		}
		for _, ref := range claim.WireMethods {
			if _, known := wireByRef[ref]; known {
				claimedWire[ref] = true
			}
		}
		for _, h := range claim.Handlers {
			if handlerOwners[h.Ref()] == nil {
				handlerOwners[h.Ref()] = map[string]bool{}
			}
			handlerOwners[h.Ref()][def.ID] = true
		}

		gaps := make([]Gap, 0, 4)
		method, wireGaps := resolveWire(def.ID, claim, wireByRef)
		gaps = append(gaps, wireGaps...)
		handler, handlerGaps := resolveHandler(def.ID, claim, index)
		gaps = append(gaps, handlerGaps...)
		mb, modelGaps := resolveModel(def.ID, claim, modelByDefinition)
		gaps = append(gaps, modelGaps...)

		if len(gaps) > 0 {
			table.Gaps = append(table.Gaps, gaps...)
			continue
		}
		table.Entries = append(table.Entries, Entry{
			Capability:      def.Key(),
			OwnerDomain:     def.OwnerDomain,
			EffectClass:     def.EffectClass,
			DefinitionRef:   claim.DefinitionRef,
			Wire:            method,
			Handler:         handler,
			Entities:        entityRefs(mb),
			ReadProperties:  propertyRefs(mb.ReadProperties),
			WriteProperties: propertyRefs(mb.WriteProperties),
		})
	}

	table.Gaps = append(table.Gaps, staleClaimGaps(claims, sorted)...)
	table.Gaps = append(table.Gaps, sharedHandlerGaps(handlerOwners)...)
	table.Gaps = append(table.Gaps, unboundWireGaps(wire, claimedWire)...)

	sort.Slice(table.Gaps, func(i, j int) bool { return table.Gaps[i].ID() < table.Gaps[j].ID() })
	return table
}

// resolveWire requires the claim to name exactly one wire method that
// exists in the compiled descriptors and is not server-streaming.
func resolveWire(capabilityID string, claim Claim, wireByRef map[string]WireMethod) (WireMethod, []Gap) {
	var gaps []Gap
	known := make([]WireMethod, 0, len(claim.WireMethods))
	for _, ref := range claim.WireMethods {
		m, ok := wireByRef[ref]
		if !ok {
			gaps = append(gaps, Gap{
				Kind: GapUnknownWireMethod, Capability: capabilityID, Subject: ref,
				Detail: "no method of the four registered generated service descriptors is named " + ref,
			})
			continue
		}
		known = append(known, m)
	}
	sortWireMethods(known)

	switch {
	case len(known) == 0:
		gaps = append(gaps, Gap{
			Kind: GapNoWireMethod, Capability: capabilityID,
			Detail: "no method of the intents, registry, admin or journey service carries this capability, so nothing on the wire can reach it",
		})
		return WireMethod{}, gaps
	case len(known) > 1:
		refs := make([]string, 0, len(known))
		for _, m := range known {
			refs = append(refs, m.Ref())
		}
		gaps = append(gaps, Gap{
			Kind: GapAmbiguousWireMethod, Capability: capabilityID, Subject: strings.Join(refs, ","),
			Detail: fmt.Sprintf("%d wire methods can carry this capability; BIND-001 requires exactly one descriptor per capability", len(known)),
		})
		return WireMethod{}, gaps
	}

	if known[0].Streaming {
		gaps = append(gaps, Gap{
			Kind: GapStreamingWireMethod, Capability: capabilityID, Subject: known[0].Ref(),
			Detail: "the one bound method is server-streaming; a unary request/result capability cannot be carried by a stream",
		})
		return WireMethod{}, gaps
	}
	return known[0], gaps
}

// resolveHandler requires the claim to name exactly one typed Go symbol,
// and — when a handler index was supplied — that the symbol exists in the
// scanned source tree.
func resolveHandler(capabilityID string, claim Claim, index HandlerIndex) (HandlerSymbol, []Gap) {
	var gaps []Gap
	present := make([]HandlerSymbol, 0, len(claim.Handlers))
	for _, h := range claim.Handlers {
		if index != nil && !index.Has(h) {
			gaps = append(gaps, Gap{
				Kind: GapMissingHandlerSymbol, Capability: capabilityID, Subject: h.Ref(),
				Detail: "the claimed handler symbol does not exist in the scanned source tree (dangling binding)",
			})
			continue
		}
		present = append(present, h)
	}
	sort.Slice(present, func(i, j int) bool { return present[i].Ref() < present[j].Ref() })

	switch {
	case len(present) == 0:
		gaps = append(gaps, Gap{
			Kind: GapNoHandler, Capability: capabilityID,
			Detail: "no typed Go symbol implements this capability; the registry publishes it with nothing compile-time checked behind it",
		})
		return HandlerSymbol{}, gaps
	case len(present) > 1:
		refs := make([]string, 0, len(present))
		for _, h := range present {
			refs = append(refs, h.Ref())
		}
		gaps = append(gaps, Gap{
			Kind: GapAmbiguousHandler, Capability: capabilityID, Subject: strings.Join(refs, ","),
			Detail: fmt.Sprintf("%d typed Go symbols answer this capability; BIND-001 requires exactly one", len(present)),
		})
		return HandlerSymbol{}, gaps
	}
	return present[0], gaps
}

// resolveModel requires the claim to name a definition whose model binding
// resolved cleanly against the generated registry.
func resolveModel(capabilityID string, claim Claim, byDefinition map[string]modelbinding.ModelBinding) (modelbinding.ModelBinding, []Gap) {
	if claim.DefinitionRef == "" {
		return modelbinding.ModelBinding{}, []Gap{{
			Kind: GapNoModelBinding, Capability: capabilityID,
			Detail: "the capability names no drafted intent definition, so no generated model entity or property is bound to it",
		}}
	}
	mb, ok := byDefinition[claim.DefinitionRef]
	if !ok {
		return modelbinding.ModelBinding{}, []Gap{{
			Kind: GapNoModelBinding, Capability: capabilityID, Subject: claim.DefinitionRef,
			Detail: "internal/intent/modelbinding produced no binding for this definition against the generated model registry",
		}}
	}
	return mb, nil
}

// staleClaimGaps reports every claim naming a capability id the registry
// does not publish. Without it a claim could survive the capability being
// renamed or reversioned out from under it, and an unregistered capability
// version would look bound — the "unregistered capability version" half of
// BIND-001's RED clause.
func staleClaimGaps(claims []Claim, records []capability.Record) []Gap {
	published := make(map[string]bool, len(records))
	for _, rec := range records {
		published[rec.Definition.ID] = true
	}
	sorted := append([]Claim(nil), claims...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CapabilityID < sorted[j].CapabilityID })

	var gaps []Gap
	for _, c := range sorted {
		if published[c.CapabilityID] {
			continue
		}
		gaps = append(gaps, Gap{
			Kind: GapClaimWithoutCapability, Capability: c.CapabilityID,
			Detail: "a reviewed claim names this capability but the registry publishes no version of it; the claim is stale or the capability was never registered",
		})
	}
	return gaps
}

// sharedHandlerGaps reports every typed Go symbol claimed by more than one
// capability: invoking it cannot identify which capability ran, which is
// the "duplicate Go symbol" half of BIND-001's RED clause.
func sharedHandlerGaps(owners map[string]map[string]bool) []Gap {
	refs := make([]string, 0, len(owners))
	for ref := range owners {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	var gaps []Gap
	for _, ref := range refs {
		if len(owners[ref]) < 2 {
			continue
		}
		capabilityIDs := make([]string, 0, len(owners[ref]))
		for id := range owners[ref] {
			capabilityIDs = append(capabilityIDs, id)
		}
		sort.Strings(capabilityIDs)
		gaps = append(gaps, Gap{
			Kind: GapHandlerBoundTwice, Subject: ref,
			Detail: "claimed by " + strings.Join(capabilityIDs, ", ") + "; one symbol cannot be the implementation of two capabilities",
		})
	}
	return gaps
}

// unboundWireGaps reports every method of a registered service that no
// capability claims: a route on the wire that no published capability
// stands behind.
func unboundWireGaps(wire []WireMethod, claimed map[string]bool) []Gap {
	sorted := append([]WireMethod(nil), wire...)
	sortWireMethods(sorted)

	var gaps []Gap
	for _, m := range sorted {
		if claimed[m.Ref()] {
			continue
		}
		gaps = append(gaps, Gap{
			Kind: GapWireMethodUnbound, Subject: m.Ref(),
			Detail: "this method is declared in a registered service descriptor but no published capability claims it",
		})
	}
	return gaps
}

func entityRefs(mb modelbinding.ModelBinding) []string {
	out := make([]string, 0, len(mb.Entities))
	for _, e := range mb.Entities {
		out = append(out, e.Ref())
	}
	return out
}

func propertyRefs(props []model.PropertyMeta) []string {
	out := make([]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.Ref)
	}
	return out
}

// Digest is the table's canonical content digest: a sha256 over
// length-tagged, NUL-separated parts, the same convention
// internal/intent/modelbinding.Table.Digest and tools/gen/storagemanifest
// use. Two tables built from the same registry, descriptors, claims, model
// binding and handler index always agree; any change to a bound wire
// method, handler symbol, model footprint or gap changes it.
func (t Table) Digest() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			fmt.Fprintf(h, "%d:", len(p))
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	if t.SymbolsChecked {
		w("SYMBOLS_CHECKED", "true")
	} else {
		w("SYMBOLS_CHECKED", "false")
	}
	for _, e := range t.Entries {
		w("ENTRY", e.Capability.String(), e.OwnerDomain, string(e.EffectClass),
			e.DefinitionRef, e.Wire.Ref(), e.Handler.Ref())
		for _, ref := range e.Entities {
			w("ENTITY", ref)
		}
		for _, ref := range e.ReadProperties {
			w("READ", ref)
		}
		for _, ref := range e.WriteProperties {
			w("WRITE", ref)
		}
	}
	for _, g := range t.Gaps {
		w("GAP", string(g.Kind), g.Capability, g.Subject, g.Detail)
	}
	return hex.EncodeToString(h.Sum(nil))
}
