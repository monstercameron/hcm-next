package canonical

import (
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Decimal declares that a string-typed material path carries a fixed decimal
// literal rather than free text.
//
// The value encodes as sign, unscaled magnitude, and scale. When ScaleMaterial
// is false, trailing fractional zeros are stripped first, so "1.50" and "1.5"
// canonicalize identically. When it is true the declared scale is preserved and
// those two literals are distinct — the schema, not the implementation, decides.
type Decimal struct {
	Path          string
	ScaleMaterial bool
}

// Profile is the explicitly materialized canonical model for one message type.
//
// A profile is an include list: a field contributes to the canonical bytes only
// when it is named by Material (or is a descendant of a Material path that has
// no listed descendants of its own). Nonmaterial display labels, UI layout,
// transient traces, and derived formatting are excluded by omission, never by
// an implicit rule the encoder invents.
//
// Paths are dotted Protobuf field names rooted at MessageName, for example
// "proposal.schema.schema_id". A path naming a repeated or map field selects
// that whole field; paths beneath a repeated message field apply element-wise,
// for example "delegation_chain.delegation_id".
//
// Listing both an ancestor and one of its descendants narrows: the descendants
// win and the unlisted siblings are excluded.
type Profile struct {
	// ID and Version identify the canonicalization profile itself, e.g.
	// "hcmnext.proposal" version 1. Both are bound into the canonical bytes.
	ID      string
	Version uint32

	// SchemaID and SchemaVersion identify the semantic schema the canonical
	// model belongs to. Both are bound into the canonical bytes, and
	// SchemaVersion is additionally bound into every encoded enum value so an
	// enum's numeric identity can never drift meaning across schema versions.
	SchemaID      string
	SchemaVersion uint32

	// MessageName is the Protobuf full name of the canonical model message.
	// Encoding a message of any other type fails with ErrSchemaMismatch.
	MessageName protoreflect.FullName

	// Material is the include list of material field paths.
	Material []string

	// Sets names repeated paths whose element order is immaterial. Their
	// elements sort by canonical bytes and duplicate canonical elements are
	// rejected. Repeated paths absent from Sets keep their declared order.
	Sets []string

	// Verbatim names string paths that carry verbatim bytes and are therefore
	// exempt from NFC normalization. They are still required to be valid UTF-8.
	Verbatim []string

	// Currency names string paths carrying ISO 4217 currency codes. They
	// canonicalize to three uppercase ASCII letters.
	Currency []string

	// Decimals declares fixed-decimal string paths.
	Decimals []Decimal

	// DistinctPresence names implicit-presence paths that must still emit an
	// explicit absent marker rather than being omitted from the stream.
	//
	// Protobuf implicit presence cannot itself distinguish an unset field from
	// one set to the schema default, so this does not recover a distinction the
	// wire format destroyed. What it does is pin one normalized representation
	// — "this field was considered and was at its default" — rather than
	// letting the field silently vanish. Fields that carry explicit presence
	// (proto3 optional, message fields, oneof members) are read from the
	// descriptor and need no declaration. An empty repeated or map field is
	// omitted, which is the one normalized representation for "no members".
	DistinctPresence []string

	// RejectUnknownFields must be true for material signed or hashed profiles.
	// When true, unknown Protobuf wire data retained anywhere within the
	// material subtree fails the encode.
	RejectUnknownFields bool
}

// pathNode is one node of the compiled material path trie.
type pathNode struct {
	children map[string]*pathNode
	terminal bool
}

func (n *pathNode) child(name string) *pathNode {
	if n.children == nil {
		n.children = map[string]*pathNode{}
	}
	c, ok := n.children[name]
	if !ok {
		c = &pathNode{}
		n.children[name] = c
	}
	return c
}

// subtree reports whether this node selects everything beneath it.
func (n *pathNode) subtree() bool { return n.terminal && len(n.children) == 0 }

// Plan is a validated profile ready to drive the encoder. Compiling is
// separated from encoding so that a registry can reject a malformed profile at
// publication time rather than at digest time.
type Plan struct {
	profile  Profile
	desc     protoreflect.MessageDescriptor
	root     *pathNode
	sets     map[string]bool
	verbatim map[string]bool
	currency map[string]bool
	decimals map[string]Decimal
	presence map[string]bool
}

func splitPath(p string) []string { return strings.Split(p, ".") }

func buildTrie(paths []string) (*pathNode, error) {
	root := &pathNode{}
	for _, p := range paths {
		if p == "" {
			return nil, newError("compile", "", ErrInvalidProfile, "empty material path")
		}
		n := root
		for _, seg := range splitPath(p) {
			if seg == "" {
				return nil, newError("compile", p, ErrInvalidProfile, "empty path segment")
			}
			n = n.child(seg)
		}
		n.terminal = true
	}
	return root, nil
}

// Compile validates a profile against the registered descriptor for its
// canonical model and returns a reusable encoder plan.
func Compile(p Profile) (*Plan, error) {
	if p.ID == "" || p.SchemaID == "" || p.MessageName == "" {
		return nil, newError("compile", "", ErrInvalidProfile,
			"profile id, schema id and message name are all required")
	}
	if len(p.Material) == 0 {
		return nil, newError("compile", "", ErrInvalidProfile, "material path list is empty")
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(p.MessageName)
	if err != nil {
		return nil, newError("compile", string(p.MessageName), ErrInvalidProfile,
			"canonical model is not registered: %v", err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, newError("compile", string(p.MessageName), ErrInvalidProfile,
			"canonical model is not a message")
	}
	root, err := buildTrie(p.Material)
	if err != nil {
		return nil, err
	}
	c := &Plan{
		profile:  p,
		desc:     md,
		root:     root,
		sets:     map[string]bool{},
		verbatim: map[string]bool{},
		currency: map[string]bool{},
		decimals: map[string]Decimal{},
		presence: map[string]bool{},
	}
	for _, s := range p.Sets {
		c.sets[s] = true
	}
	for _, s := range p.Verbatim {
		c.verbatim[s] = true
	}
	for _, s := range p.Currency {
		c.currency[s] = true
	}
	for _, dec := range p.Decimals {
		c.decimals[dec.Path] = dec
	}
	for _, s := range p.DistinctPresence {
		c.presence[s] = true
	}
	if err := c.validatePaths(); err != nil {
		return nil, err
	}
	return c, nil
}

// validatePaths resolves every declared path against the descriptor so that a
// typo, a renamed field, or a set declared on a singular field is caught before
// any digest is minted.
func (c *Plan) validatePaths() error {
	if err := c.walk(c.desc, c.root, ""); err != nil {
		return err
	}
	for _, decl := range [...]struct {
		kind  string
		paths []string
	}{
		{"set", c.profile.Sets},
		{"verbatim", c.profile.Verbatim},
		{"currency", c.profile.Currency},
		{"distinct presence", c.profile.DistinctPresence},
	} {
		for _, p := range decl.paths {
			if err := c.requireMaterial(p, decl.kind); err != nil {
				return err
			}
		}
	}
	for _, dec := range c.profile.Decimals {
		if err := c.requireMaterial(dec.Path, "decimal"); err != nil {
			return err
		}
	}
	return nil
}

// requireMaterial reports whether a declaration attaches to a path the profile
// actually materializes. A set or verbatim declaration on an excluded field is
// dead configuration and almost always a mistake.
func (c *Plan) requireMaterial(path, kind string) error {
	n := c.root
	for _, seg := range splitPath(path) {
		if n.subtree() {
			return nil
		}
		next, ok := n.children[seg]
		if !ok {
			return newError("compile", path, ErrInvalidProfile,
				"%s declaration names a path outside the material list", kind)
		}
		n = next
	}
	return nil
}

// walk resolves the trie against a descriptor. Nodes that select a whole
// subtree stop resolution; the encoder validates the subtree structurally as it
// descends.
func (c *Plan) walk(md protoreflect.MessageDescriptor, n *pathNode, prefix string) error {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := n.children[name]
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		fd := md.Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return newError("compile", path, ErrInvalidProfile,
				"field %q is not defined on %s", name, md.FullName())
		}
		if c.sets[path] && !fd.IsList() {
			return newError("compile", path, ErrInvalidProfile,
				"set declared on a field that is not a repeated list")
		}
		if len(child.children) > 0 {
			nested := fd.Message()
			if fd.IsMap() {
				nested = fd.MapValue().Message()
			}
			if nested == nil {
				return newError("compile", path, ErrInvalidProfile,
					"path descends into a field that is not a message")
			}
			if err := c.walk(nested, child, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// MaterialPaths returns the profile's material path list in sorted order. It is
// the authoritative answer to "what does this digest bind?".
func (p Profile) MaterialPaths() []string {
	out := append([]string(nil), p.Material...)
	sort.Strings(out)
	return out
}
