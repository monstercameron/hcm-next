package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ModelGap is one unresolved check for one catalog item.
type ModelGap struct {
	Item    string
	Element string
	Detail  string
}

func (g ModelGap) String() string {
	return fmt.Sprintf("%s: %s (%s)", g.Item, g.Element, g.Detail)
}

// ModelCoverageReport is the MODEL-030 answer to "is the compiled model
// registry a closed, checkable set?"
//
// The grouped counts reuse the vocabulary
// planning/data/models/registry-and-coverage-contracts.md's "Minimum
// conformance proof per intent" section applies to the intent catalog,
// scoped down to what this registry actually tracks: ByDomain is
// EntityDefinition.OwnerDomain (the catalog's "domain" axis), ByStatus is
// EntityDefinition.Status (the "maturity" axis: DRAFT/ACTIVE/DEPRECATED/
// RETIRED), ByClass is EntityDefinition.Class (the "phase depth" axis: how
// deep the entity sits in the aggregate/child/evidence hierarchy), and
// ConformancePairs counts aggregate roots that declare at least one
// invariant reference — the closest analogue this registry has to per-intent
// conformance-scenario coverage, since MODEL-030 depends on MODEL-016's
// per-intent binding checker rather than reimplementing it.
type ModelCoverageReport struct {
	Total    int
	Verified int
	Gaps     []ModelGap

	ByDomain map[string]int
	ByStatus map[string]int
	ByClass  map[string]int

	ConformancePairs int

	SourceDigest string
}

// Summary renders "N/N VERIFIED" or the exact gap list.
func (r ModelCoverageReport) Summary() string {
	if len(r.Gaps) == 0 {
		return fmt.Sprintf("%d/%d VERIFIED", r.Verified, r.Total)
	}
	lines := make([]string, 0, len(r.Gaps)+1)
	lines = append(lines, fmt.Sprintf("%d/%d VERIFIED", r.Verified, r.Total))
	for _, g := range r.Gaps {
		lines = append(lines, "  gap: "+g.String())
	}
	return strings.Join(lines, "\n")
}

// FullyVerified reports whether every registered entity resolved cleanly.
func (r ModelCoverageReport) FullyVerified() bool {
	return len(r.Gaps) == 0 && r.Verified == r.Total
}

// CheckModelCoverage reports whether every published entity resolves: it has
// at least one property, an aggregate registration if it is a root or a
// resolvable owner if it is a child, and every one of its properties fully
// resolves through [Registry.ResolveProperty] (MODEL-030).
//
// A report can never claim VERIFIED while any check is absent: the first
// unresolved item/element pair is always present in Gaps, sorted so the
// same input always yields the same first gap.
func CheckModelCoverage(r *Registry) ModelCoverageReport {
	report := ModelCoverageReport{
		Total:    len(r.entityOrder),
		ByDomain: map[string]int{},
		ByStatus: map[string]int{},
		ByClass:  map[string]int{},
	}

	propertiesByEntity := map[EntityRef][]PropertyDefinition{}
	for _, p := range r.Properties() {
		propertiesByEntity[p.Entity] = append(propertiesByEntity[p.Entity], p)
	}

	for _, e := range r.Entities() {
		report.ByDomain[e.OwnerDomain]++
		report.ByStatus[string(e.Status)]++
		report.ByClass[string(e.Class)]++

		var gaps []ModelGap
		add := func(element, detail string) {
			gaps = append(gaps, ModelGap{Item: e.Ref.String(), Element: element, Detail: detail})
		}

		props := propertiesByEntity[e.Ref]
		if len(props) == 0 {
			add("properties", "entity registers no property")
		}

		switch e.Class {
		case ClassAggregateRoot:
			agg, err := r.Aggregate(e.Ref)
			if err != nil {
				add("aggregate", err.Error())
			} else if len(agg.InvariantRefs) > 0 {
				report.ConformancePairs++
			}
		case ClassChild:
			if _, err := r.OwnerRoot(e.Ref); err != nil {
				add("owner_root", err.Error())
			}
		}

		for _, p := range props {
			if _, err := r.ResolveProperty(p.Ref); err != nil {
				add("property_resolution", fmt.Sprintf("%s: %v", p.Ref, err))
			}
		}

		if len(gaps) == 0 {
			report.Verified++
			continue
		}
		report.Gaps = append(report.Gaps, gaps...)
	}

	sort.Slice(report.Gaps, func(i, j int) bool {
		if report.Gaps[i].Item != report.Gaps[j].Item {
			return report.Gaps[i].Item < report.Gaps[j].Item
		}
		return report.Gaps[i].Element < report.Gaps[j].Element
	})

	report.SourceDigest = r.Digest()
	return report
}

// Digest computes a stable sha256 digest over the registry's compiled
// content: sorted entity, property, aggregate, relationship, authority and
// retention-class rows. Two registries compiled from the same source data
// always produce the same digest, and the digest changes if any of that data
// changes — this is what backs the "generation is deterministic" proof both
// here and in tools/gen/storagemanifest.
func (r *Registry) Digest() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	for _, e := range r.Entities() {
		w("ENTITY", e.Ref.String(), e.Key, e.OwnerDomain, string(e.Class), e.LifecycleAssignment,
			fmt.Sprint(e.TenantScoped), string(e.Status))
	}
	for _, p := range r.Properties() {
		w("PROPERTY", string(p.Ref), p.Entity.String(), p.GoType, p.SchemaPath, string(p.Presence),
			string(p.Classification), string(p.Temporal), p.AuthorityRef, string(p.Correction),
			p.RetentionClassRef, string(p.Status))
	}
	for _, a := range r.Aggregates() {
		w("AGGREGATE", a.Root.String(), a.LifecycleAssignment, string(a.CommandBoundary), a.StreamKind)
		for _, c := range a.ChildRefs {
			w("CHILD", c.String())
		}
		for _, inv := range a.InvariantRefs {
			w("INVARIANT", inv)
		}
	}
	for _, rel := range r.Relationships() {
		w("RELATIONSHIP", rel.Ref.String(), rel.SourceEntity.String(), rel.TargetEntity.String(),
			string(rel.Cardinality), fmt.Sprint(rel.Exclusive), fmt.Sprint(rel.AllowCycles),
			fmt.Sprint(rel.TenantScoped))
	}
	for _, a := range r.Authorities() {
		w("AUTHORITY", a.AssignmentRef, string(a.Kind), a.DomainScope, string(a.Merge), a.EvidenceRef)
	}
	for _, c := range r.RetentionClasses() {
		w("RETENTION", c.ClassRef, fmt.Sprint(c.DefaultPeriodDays), c.TriggerEvent, c.DispositionOwner, c.AuthorityRef)
		keys := make([]string, 0, len(c.JurisdictionOverrides))
		for k := range c.JurisdictionOverrides {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w("OVERRIDE", k, fmt.Sprint(c.JurisdictionOverrides[k]))
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
