// Package designownership validates every workflow design against
// model, engine, capability and authority ownership (WF-DISC-009): each
// engine, entity and property reference resolves to one versioned
// semantic owner, direct adapter calls and physical owners fail, and
// every missing contract emits an atomic todo candidate blocking
// CONTRACTED maturity. Capability invocation binding rides BIND-001's
// BindingTable and is not re-derived here; embedded calculations are
// unobservable from design records and stay out of scope by decision,
// both documented in Compile. It is kernel-pure: pure snapshot
// compilation plus text emission, no database, network or mutable
// global state.
package designownership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Ownership finding codes.
const (
	UnownedEngine     = "UNOWNED_ENGINE"
	EngineContract    = "ENGINE_CONTRACT"
	DirectAdapterCall = "DIRECT_ADAPTER_CALL"
	UnownedEntity     = "UNOWNED_ENTITY"
	UnownedProperty   = "UNOWNED_PROPERTY"
	PhysicalOwner     = "PHYSICAL_OWNER"
	UnknownDefinition = "UNKNOWN_DEFINITION"
)

// adapterRe marks engine names that call persistence or provider
// adapters directly instead of a versioned semantic owner.
var adapterRe = regexp.MustCompile(`(?i)\b(postgres|mysql|dynamo|sqlite|s3|bucket|queue|topic|provider|adapter|sdk|http|grpc|client|driver)\b`)

// physicalRe marks authority values that assume a physical system owner
// instead of a semantic authority rule.
var physicalRe = regexp.MustCompile(`(?i)\b(postgres|mysql|dynamo|provider|adapter|sdk|http|grpc|s3|bucket)\b`)

// DesignRef is one workflow design's ownership-relevant references.
type DesignRef struct {
	Definition string
	Intent     string
	Engines    []string
	Phase      string
}

// EngineOwner is one versioned semantic engine owner.
type EngineOwner struct {
	Engine     string
	Package    string
	HasVersion bool
	HasExplain bool
}

// EntityOwner is one model owner from the model sources.
type EntityOwner struct {
	Entity string
	Owner  string
	Source string
}

// Snapshot is the complete typed input the ownership register compiles.
type Snapshot struct {
	Designs               []DesignRef
	EngineOwners          map[string]EngineOwner
	EntityOwners          map[string]EntityOwner
	DescriptorEntities    map[string][]string
	DescriptorProperties  map[string][]string
	DescriptorAuthorities map[string][]string
	DefinitionPhase       map[string]string
}

// Resolution binds one reference to its single semantic owner.
type Resolution struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	Source string `json:"source"`
}

// Candidate is one atomic todo candidate for a missing contract. It
// always blocks CONTRACTED maturity: no design reaches contracted
// state while its references stay unowned.
type Candidate struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Refs   []string `json:"refs"`
	Owner  string   `json:"owner"`
	Phase  string   `json:"phase"`
	Title  string   `json:"title"`
	Red    string   `json:"red"`
	Green  string   `json:"green"`
	Blocks string   `json:"blocks"`
}

// Ownership is the compiled register.
type Ownership struct {
	Resolutions []Resolution `json:"resolutions"`
	Candidates  []Candidate  `json:"candidates"`
	Digest      string       `json:"digest"`
}

// Finding is one source-exact ownership diagnostic.
type Finding struct {
	Definition string `json:"definition"`
	Name       string `json:"name,omitempty"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
}

// MarshalOwnership renders a register with its findings as canonical JSON.
func MarshalOwnership(ownership Ownership, findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Definition != ordered[j].Definition {
			return ordered[i].Definition < ordered[j].Definition
		}
		if ordered[i].Code != ordered[j].Code {
			return ordered[i].Code < ordered[j].Code
		}
		return ordered[i].Detail < ordered[j].Detail
	})
	rendered, err := json.MarshalIndent(struct {
		Ownership Ownership `json:"ownership"`
		Findings  []Finding `json:"findings,omitempty"`
	}{Ownership: ownership, Findings: ordered}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

// Compile validates every design's references and emits the register.
// Findings never fail compilation: missing contracts become candidates,
// and unknown definitions stay visible without joining.
func Compile(accepted []string, snap Snapshot) (Ownership, []Finding) {
	var findings []Finding
	acceptedSet := make(map[string]bool, len(accepted))
	for _, id := range accepted {
		acceptedSet[id] = true
	}
	compiler := &compiler{
		snap:       snap,
		resolved:   make(map[string]Resolution),
		candidates: make(map[string]*Candidate),
		consumerOf: make(map[string][]string),
	}
	for _, design := range snap.Designs {
		if !acceptedSet[design.Definition] {
			findings = append(findings, Finding{Definition: design.Definition, Name: design.Intent, Code: UnknownDefinition, Detail: "design binds outside the accepted definitions; it resolves nothing"})
			continue
		}
		compiler.checkDesign(design, &findings)
	}
	ownership := Ownership{
		Candidates: compiler.candidatesList(),
	}
	for _, resolution := range compiler.resolved {
		ownership.Resolutions = append(ownership.Resolutions, resolution)
	}
	sort.Slice(ownership.Resolutions, func(i, j int) bool {
		if ownership.Resolutions[i].Kind != ownership.Resolutions[j].Kind {
			return ownership.Resolutions[i].Kind < ownership.Resolutions[j].Kind
		}
		return ownership.Resolutions[i].Name < ownership.Resolutions[j].Name
	})
	ownership.Digest = digestOwnership(ownership)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Definition != findings[j].Definition {
			return findings[i].Definition < findings[j].Definition
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
	return ownership, findings
}

type compiler struct {
	snap       Snapshot
	resolved   map[string]Resolution
	candidates map[string]*Candidate
	consumerOf map[string][]string
}

func (c *compiler) checkDesign(design DesignRef, findings *[]Finding) {
	add := func(code, name, detail string) {
		*findings = append(*findings, Finding{Definition: design.Definition, Name: name, Code: code, Detail: detail})
	}
	seen := make(map[string]bool)
	for _, engine := range design.Engines {
		engine = strings.TrimSpace(engine)
		if engine == "" || seen["engine\x00"+engine] {
			continue
		}
		seen["engine\x00"+engine] = true
		if adapterRe.MatchString(engine) {
			add(DirectAdapterCall, engine, "design calls persistence or provider adapter "+engine+" directly; route through a versioned semantic engine")
			c.emit(design, "engine-adapter", engine, "Remove direct "+engine+" call from "+design.Definition)
			continue
		}
		owner, ok := c.snap.EngineOwners[engine]
		if !ok {
			add(UnownedEngine, engine, "design engine "+engine+" resolves to no versioned semantic owner")
			c.emit(design, "engine", engine, "Own engine "+engine)
			continue
		}
		if !owner.HasVersion || !owner.HasExplain {
			add(EngineContract, engine, "engine owner "+owner.Package+" violates the Version and Explain contract")
			c.emit(design, "engine-contract", engine, "Restore the Version and Explain contract on "+owner.Package)
			continue
		}
		key := "engine\x00" + engine
		if _, dup := c.resolved[key]; !dup {
			c.resolved[key] = Resolution{Kind: "engine", Name: engine, Owner: owner.Package, Source: owner.Package + " exports Version and Explain"}
		}
	}
	for _, entity := range c.snap.DescriptorEntities[design.Definition] {
		entity = strings.TrimSpace(entity)
		if entity == "" {
			continue
		}
		if owner, ok := c.snap.EntityOwners[entity]; ok {
			key := "entity\x00" + entity
			if _, dup := c.resolved[key]; !dup {
				c.resolved[key] = Resolution{Kind: "entity", Name: entity, Owner: owner.Owner, Source: owner.Source}
			}
			continue
		}
		add(UnownedEntity, entity, "descriptor entity "+entity+" resolves to no model owner")
		c.emit(design, "entity", entity, "Bind entity "+entity+" to a model owner")
	}
	for _, property := range c.snap.DescriptorProperties[design.Definition] {
		property = strings.TrimSpace(property)
		if property == "" {
			continue
		}
		if owner, ok := c.snap.EntityOwners[property]; ok {
			key := "property\x00" + property
			if _, dup := c.resolved[key]; !dup {
				c.resolved[key] = Resolution{Kind: "property", Name: property, Owner: owner.Owner, Source: owner.Source}
			}
			continue
		}
		add(UnownedProperty, property, "descriptor property "+property+" resolves to no model owner")
		c.emit(design, "property", property, "Bind property "+property+" to a model owner")
	}
	for _, authority := range c.snap.DescriptorAuthorities[design.Definition] {
		authority = strings.TrimSpace(authority)
		if authority == "" {
			continue
		}
		if physicalRe.MatchString(authority) {
			add(PhysicalOwner, authority, "authority "+authority+" assumes a physical system owner")
			c.emit(design, "authority", authority, "Replace physical authority "+authority+" with a semantic rule")
		}
	}
}

// emit records one atomic candidate per reference kind and name,
// accumulating every consuming definition in its refs.
func (c *compiler) emit(design DesignRef, kind, name, title string) {
	key := kind + "\x00" + name
	candidate, ok := c.candidates[key]
	if !ok {
		phase := design.Phase
		if phase == "" {
			phase = c.snap.DefinitionPhase[design.Definition]
		}
		candidate = &Candidate{
			ID:     candidateID(kind, name),
			Kind:   kind,
			Name:   name,
			Owner:  "UNASSIGNED",
			Phase:  phase,
			Title:  title,
			Blocks: "CONTRACTED",
		}
		switch kind {
		case "engine", "entity", "property":
			candidate.Red = "reference " + name + " has no versioned semantic owner"
			candidate.Green = "reference " + name + " resolves to one versioned owner, or the design stops referencing it"
		default:
			candidate.Red = "reference " + name + " violates ownership"
			candidate.Green = "reference " + name + " resolves to one versioned semantic owner"
		}
		c.candidates[key] = candidate
	}
	for _, ref := range c.consumerOf[key] {
		if ref == design.Definition {
			return
		}
	}
	c.consumerOf[key] = append(c.consumerOf[key], design.Definition)
	candidate.Refs = append(candidate.Refs, design.Definition)
	sort.Strings(candidate.Refs)
}

func (c *compiler) candidatesList() []Candidate {
	var out []Candidate
	for _, candidate := range c.candidates {
		out = append(out, *candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func candidateID(kind, name string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + name))
	return "WF-DISC-009-" + hex.EncodeToString(sum[:])[:8]
}

func digestOwnership(ownership Ownership) string {
	var lines []string
	for _, resolution := range ownership.Resolutions {
		lines = append(lines, strings.Join([]string{resolution.Kind, resolution.Name, resolution.Owner, resolution.Source}, "\x00"))
	}
	for _, candidate := range ownership.Candidates {
		lines = append(lines, strings.Join([]string{candidate.ID, candidate.Kind, candidate.Name, strings.Join(candidate.Refs, ","), candidate.Owner, candidate.Phase, candidate.Title, candidate.Blocks}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
