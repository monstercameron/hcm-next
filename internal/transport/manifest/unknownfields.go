package manifest

import (
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
)

// UnknownFieldPolicy is the PROTO-009 boundary policy for one message: what
// happens when a decoded instance carries a field number the compiled
// descriptor does not declare.
type UnknownFieldPolicy string

const (
	// PolicyRejectMaterialUnknown means the wire bytes are preserved by
	// Protobuf's default decode behavior (proto.Unmarshal never drops
	// unknown bytes on its own), but a message carrying any survives
	// unknown field blocks semantic execution with a typed compatibility
	// error rather than proceeding as if the field had never been sent.
	// This is every message's policy today: internal/transport/validate.go
	// walks every nested message of every request unconditionally, and this
	// table generalizes that same rule to every message reachable from
	// either governed service, request or response.
	PolicyRejectMaterialUnknown UnknownFieldPolicy = "REJECT_MATERIAL_UNKNOWN"
)

// DynamicTypePolicy is the PROTO-009 policy for a message that carries a
// schema-tagged, dynamically-typed payload rather than a plain nested
// message. hcmnext has no google.protobuf.Any anywhere in schema/proto; the
// equivalent risk is hcmnext.intents.v1.TypedPayload, which carries
// protobuf_wire_bytes tagged by a SchemaReference the sender chooses. A
// bytes field is never walked by field-presence or unknown-field reflection
// (there is nothing to reflect into), so an unrestricted TypedPayload would
// be exactly the "unregistered Any type URL resolves" gap PROTO-009 names.
type DynamicTypePolicy string

const (
	// DynamicTypeNone means the message carries no dynamically-typed
	// payload; every field is a plain, statically-typed Protobuf field.
	DynamicTypeNone DynamicTypePolicy = "NONE"
	// DynamicTypeRestrictedAllowlist means the message carries an opaque,
	// schema-tagged payload whose declared type may only be decoded when
	// its protobuf_full_name appears in the signed allowlist
	// (AllowlistedSchemas): the union of every catalog intent definition's
	// input/result schema and every BOOTSTRAP capability's
	// request/response/error schema. An allowlist miss is a typed
	// compatibility error, never a dynamic lookup against the global type
	// registry.
	DynamicTypeRestrictedAllowlist DynamicTypePolicy = "RESTRICTED_TO_SIGNED_ALLOWLIST"
)

// MessagePolicy is one message's PROTO-009 boundary policy.
type MessagePolicy struct {
	FullName      string             `json:"full_name"`
	UnknownFields UnknownFieldPolicy `json:"unknown_fields"`
	DynamicType   DynamicTypePolicy  `json:"dynamic_type"`
	// AllowlistedSchemas is non-empty only when DynamicType is
	// DynamicTypeRestrictedAllowlist: the sorted, de-duplicated set of
	// protobuf_full_name values protobuf_wire_bytes may be decoded against.
	AllowlistedSchemas []string `json:"allowlisted_schemas,omitempty"`
}

// MessagePolicySet is the total, sorted table PROTO-009 requires: one row
// per message reachable from a governed service's request or response.
type MessagePolicySet struct {
	Messages []MessagePolicy `json:"messages"`
}

// Lookup returns the policy for fullName, if this set covers it.
func (s *MessagePolicySet) Lookup(fullName string) (MessagePolicy, bool) {
	i := sort.Search(len(s.Messages), func(i int) bool { return s.Messages[i].FullName >= fullName })
	if i < len(s.Messages) && s.Messages[i].FullName == fullName {
		return s.Messages[i], true
	}
	return MessagePolicy{}, false
}

// typedPayloadFullName is the one message in schema/proto/** whose payload
// is dynamically typed.
const typedPayloadFullName = "hcmnext.intents.v1.TypedPayload"

// BuildMessagePolicies walks every message reachable (through message-typed
// fields, including map values and list elements) from any RPC in rpcs'
// input or output, and returns one policy per distinct message full name.
// allowlistedSchemas is the signed set of protobuf_full_name values a
// TypedPayload may declare; [BuildDefaultMessagePolicies] supplies the
// production value.
func BuildMessagePolicies(rpcs []RPCDescriptor, allowlistedSchemas []string) *MessagePolicySet {
	visited := map[string]protoreflect.MessageDescriptor{}
	for _, d := range rpcs {
		collectMessages(d.Input, visited)
		collectMessages(d.Output, visited)
	}

	names := make([]string, 0, len(visited))
	for name := range visited {
		names = append(names, name)
	}
	sort.Strings(names)

	allow := sortUniqueStrings(allowlistedSchemas)

	out := make([]MessagePolicy, 0, len(names))
	for _, name := range names {
		p := MessagePolicy{FullName: name, UnknownFields: PolicyRejectMaterialUnknown, DynamicType: DynamicTypeNone}
		if name == typedPayloadFullName {
			p.DynamicType = DynamicTypeRestrictedAllowlist
			p.AllowlistedSchemas = allow
		}
		out = append(out, p)
	}
	return &MessagePolicySet{Messages: out}
}

// collectMessages records md and every message-typed field reachable from
// it (recursively, through singular fields, list elements and map values)
// into visited, keyed by full name. A message already present is not
// re-walked, which both memoizes the walk and makes an accidental cycle
// safe (none exists in schema/proto/** today).
func collectMessages(md protoreflect.MessageDescriptor, visited map[string]protoreflect.MessageDescriptor) {
	if md == nil {
		return
	}
	name := string(md.FullName())
	if _, ok := visited[name]; ok {
		return
	}
	visited[name] = md

	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		switch {
		case fd.IsMap():
			if fd.MapValue().Kind() == protoreflect.MessageKind || fd.MapValue().Kind() == protoreflect.GroupKind {
				collectMessages(fd.MapValue().Message(), visited)
			}
		case fd.Kind() == protoreflect.MessageKind, fd.Kind() == protoreflect.GroupKind:
			collectMessages(fd.Message(), visited)
		}
	}
}

// schemaAllowlist returns the sorted, de-duplicated set of protobuf full
// names a TypedPayload may declare: every catalog intent definition's
// input/result schema, plus every BOOTSTRAP capability's
// request/response/error schema.
func schemaAllowlist(defs []intent.Definition, records []capability.Record) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.InputSchema.ProtobufFullName, d.ResultSchema.ProtobufFullName)
	}
	for _, rec := range records {
		out = append(out,
			rec.Definition.RequestSchema.ProtobufFullName,
			rec.Definition.ResponseSchema.ProtobufFullName,
			rec.Definition.ErrorSchema.ProtobufFullName,
		)
	}
	return sortUniqueStrings(out)
}

// BuildDefaultMessagePolicies is [BuildMessagePolicies] wired to the
// production descriptor set and the production schema allowlist
// (internal/intent/definitions and internal/capability's BOOTSTRAP
// registry).
func BuildDefaultMessagePolicies() (*MessagePolicySet, error) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		return nil, err
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, err
	}
	allow := schemaAllowlist(definitions.All(), registry.List())
	return BuildMessagePolicies(rpcs, allow), nil
}
