package application

// The composed dependency graph, recorded while the composition happens.
//
// A composition root's real output is not just a running process; it is a
// claim about what that process is made of. Writing that claim down as a
// value - and digesting it - is what makes "we swapped an adapter" a visible
// diff rather than a thing somebody has to re-read main.go to notice. The
// graph is also what makes the ARCH-GO-020 Golden test possible at all: the
// alternative, asserting on a running listener, proves the process starts,
// not that it is wired the way the architecture says.
//
// The graph deliberately records no addresses, no keys, no tenant and no
// timestamps. It is the shape of the composition, not this run of it, so two
// processes started on different ports with different credentials produce the
// same digest and a process that grew a hidden dependency does not.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Component kinds. They are a closed vocabulary so the digest stays readable:
// a new kind is a deliberate edit here, not a free-text string somebody typed
// at a call site.
const (
	KindConfig     = "config"
	KindPort       = "port"
	KindAdapter    = "adapter"
	KindRegistry   = "registry"
	KindGovernance = "governance"
	KindEngine     = "engine"
	KindWorkflow   = "workflow"
	KindTransport  = "transport"
	KindWorkload   = "workload"
	KindShutdown   = "shutdown"
)

// Component is one named node of a composed application: what it is, which
// concrete Go type answers for it, and which other nodes it was built from.
type Component struct {
	// Name is the composition-local name, unique within one Graph.
	Name string
	// Kind is one of the Kind* constants above.
	Kind string
	// Impl is the concrete Go type that answers for this node, or the empty
	// string when the node is a value rather than an implementation (a
	// configuration, say). A nil interface records "<nil>", which is a real
	// and interesting composition fact: it is how "telemetry is off" and
	// "this cell cannot execute" are stated.
	Impl string
	// DependsOn names the components this one was constructed from, sorted.
	DependsOn []string
}

// Graph is the whole composed dependency graph for one role.
type Graph struct {
	Role       Role
	Components []Component
}

// graphBuilder accumulates components during composition. It is a local
// value carried through one ComposeServe call, never package state: a
// package-level graph would be exactly the global mutable registry this todo
// exists to forbid.
type graphBuilder struct {
	role       Role
	components []Component
}

func newGraphBuilder(role Role) *graphBuilder {
	return &graphBuilder{role: role}
}

// add records one component. impl may be any value; its dynamic type is what
// is recorded.
func (b *graphBuilder) add(name, kind string, impl any, dependsOn ...string) {
	deps := append([]string(nil), dependsOn...)
	sort.Strings(deps)
	b.components = append(b.components, Component{
		Name:      name,
		Kind:      kind,
		Impl:      implName(impl),
		DependsOn: deps,
	})
}

func (b *graphBuilder) graph() Graph {
	components := append([]Component(nil), b.components...)
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	return Graph{Role: b.role, Components: components}
}

// implName renders a value's dynamic type. A nil interface or nil pointer
// renders "<nil>": telemetry that is off and an executor that is absent are
// composition facts worth digesting, not blanks.
func implName(v any) string {
	if v == nil {
		return "<nil>"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if rv.IsNil() {
			return "<nil>"
		}
	}
	if rv.Kind() == reflect.Func {
		// A function value's identity is its address, which differs run to
		// run. Only its signature is a stable composition fact.
		return rv.Type().String()
	}
	return reflect.TypeOf(v).String()
}

// Canonical renders the graph as deterministic, diffable text: one line per
// component, sorted by name, fields separated by "|" and dependencies by ",".
func (g Graph) Canonical() string {
	var b strings.Builder
	fmt.Fprintf(&b, "role=%s\n", g.Role)
	components := append([]Component(nil), g.Components...)
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	for _, c := range components {
		deps := append([]string(nil), c.DependsOn...)
		sort.Strings(deps)
		fmt.Fprintf(&b, "%s|%s|%s|%s\n", c.Name, c.Kind, c.Impl, strings.Join(deps, ","))
	}
	return b.String()
}

// Digest is the SHA-256 of Canonical, hex-encoded. A change to what this
// process is made of changes this string; a change to which port it listens
// on does not.
func (g Graph) Digest() string {
	sum := sha256.Sum256([]byte(g.Canonical()))
	return hex.EncodeToString(sum[:])
}

// Component returns the named component and whether it was composed.
func (g Graph) Component(name string) (Component, bool) {
	for _, c := range g.Components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}

// Names returns every composed component name, sorted.
func (g Graph) Names() []string {
	out := make([]string, 0, len(g.Components))
	for _, c := range g.Components {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

// Validate reports a graph that names a dependency it does not contain, or a
// component with no name or an unknown kind. A composition root that can
// describe itself incoherently cannot be trusted to describe itself at all.
func (g Graph) Validate() error {
	known := make(map[string]bool, len(g.Components))
	for _, c := range g.Components {
		if c.Name == "" {
			return fmt.Errorf("application: composed graph has an unnamed component")
		}
		if known[c.Name] {
			return fmt.Errorf("application: composed graph names %q twice", c.Name)
		}
		if !knownKind(c.Kind) {
			return fmt.Errorf("application: composed component %q has unknown kind %q", c.Name, c.Kind)
		}
		known[c.Name] = true
	}
	for _, c := range g.Components {
		for _, dep := range c.DependsOn {
			if !known[dep] {
				return fmt.Errorf("application: composed component %q depends on absent %q", c.Name, dep)
			}
		}
	}
	return nil
}

func knownKind(kind string) bool {
	switch kind {
	case KindConfig, KindPort, KindAdapter, KindRegistry, KindGovernance,
		KindEngine, KindWorkflow, KindTransport, KindWorkload, KindShutdown:
		return true
	default:
		return false
	}
}
