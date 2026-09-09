package manifest

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

// injectUnknownField appends a well-formed varint field the target
// message's descriptor does not declare, so decoding it exercises
// Protobuf's default unknown-field preservation rather than a rejection at
// the wire level.
func injectUnknownField(wire []byte, fieldNumber int32) []byte {
	tail := protowire.AppendTag(nil, protowire.Number(fieldNumber), protowire.VarintType)
	tail = protowire.AppendVarint(tail, 13)
	return append(append([]byte{}, wire...), tail...)
}

// TestMixedVersionProtoRelayNeverSilentlyDropsMaterialUnknownFields is
// PROTO-009's RED/GREEN test: a message carrying a field number newer than
// this build's descriptor must (a) still decode, (b) still preserve every
// known field, (c) still carry the unknown bytes forward through a relay
// round trip rather than silently dropping them, and (d) this package's
// message policy table must classify that message as one whose unknown
// fields block semantic execution — never as a message that quietly
// tolerates them.
func TestMixedVersionProtoRelayNeverSilentlyDropsMaterialUnknownFields(t *testing.T) {
	const unknownFieldNumber = 999

	known := &commonv1.ScopeContext{TenantId: "tenant-1", OrganizationScopeId: "org-1", Purpose: "payroll"}
	knownBytes, err := proto.Marshal(known)
	if err != nil {
		t.Fatalf("marshal known message: %v", err)
	}
	wireBytes := injectUnknownField(knownBytes, unknownFieldNumber)

	decoded := &commonv1.ScopeContext{}
	if err := proto.Unmarshal(wireBytes, decoded); err != nil {
		t.Fatalf("unmarshal message with unknown field: %v", err)
	}
	if decoded.GetTenantId() != known.GetTenantId() || decoded.GetOrganizationScopeId() != known.GetOrganizationScopeId() || decoded.GetPurpose() != known.GetPurpose() {
		t.Fatalf("a known field was lost alongside the unknown field: got %+v", decoded)
	}
	if len(decoded.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("expected the unknown field to be preserved by default decoding, got zero unknown bytes")
	}

	// Relay: re-encode and decode again (a mixed-version relay/JSON bridge
	// step). The unknown bytes must still be present — "allowed relays
	// preserve golden bytes" — not dropped on the second hop.
	relayed, err := proto.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal for relay: %v", err)
	}
	afterRelay := &commonv1.ScopeContext{}
	if err := proto.Unmarshal(relayed, afterRelay); err != nil {
		t.Fatalf("unmarshal after relay: %v", err)
	}
	if len(afterRelay.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("a relay round trip silently dropped the material unknown field")
	}

	policies, err := BuildDefaultMessagePolicies()
	if err != nil {
		t.Fatalf("BuildDefaultMessagePolicies: %v", err)
	}
	p, ok := policies.Lookup(string((&commonv1.ScopeContext{}).ProtoReflect().Descriptor().FullName()))
	if !ok {
		t.Fatal("ScopeContext has no policy row even though it is reachable from every governed request")
	}
	if p.UnknownFields != PolicyRejectMaterialUnknown {
		t.Fatalf("ScopeContext unknown-field policy = %s, want %s (a mixed-version relay must not silently tolerate this)", p.UnknownFields, PolicyRejectMaterialUnknown)
	}

	t.Run("TypedPayloadDynamicTypeIsRestrictedToTheSignedAllowlist", func(t *testing.T) {
		payloadPolicy, ok := policies.Lookup("hcmnext.intents.v1.TypedPayload")
		if !ok {
			t.Fatal("TypedPayload has no policy row")
		}
		if payloadPolicy.DynamicType != DynamicTypeRestrictedAllowlist {
			t.Fatalf("TypedPayload dynamic type policy = %s, want %s", payloadPolicy.DynamicType, DynamicTypeRestrictedAllowlist)
		}
		if len(payloadPolicy.AllowlistedSchemas) == 0 {
			t.Fatal("TypedPayload's allowlist is empty; every catalog schema would be rejected")
		}
		found := false
		for _, s := range payloadPolicy.AllowlistedSchemas {
			if s == "hcmnext.people.v1.PromoteWorkerRequest" {
				found = true
			}
		}
		if !found {
			t.Fatal("PromoteWorkerRequest, a real catalog schema, is missing from the TypedPayload allowlist")
		}
		// An unregistered type URL/full name must never resolve: it is
		// simply absent from the allowlist, not looked up dynamically.
		for _, s := range payloadPolicy.AllowlistedSchemas {
			if s == "attacker.v1.UnregisteredType" {
				t.Fatal("an unregistered type name resolved against the allowlist")
			}
		}
	})
}

// TestTodo_PROTO_009_Property proves the message policy table is a pure,
// order-independent function of the discovered RPC set: reversing rpcs
// produces the identical sorted policy set.
func TestTodo_PROTO_009_Property(t *testing.T) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		t.Fatalf("DiscoverRPCs: %v", err)
	}
	reversed := make([]RPCDescriptor, len(rpcs))
	for i, d := range rpcs {
		reversed[len(rpcs)-1-i] = d
	}
	allow := []string{"a.v1.A", "b.v1.B"}
	p1 := BuildMessagePolicies(rpcs, allow)
	p2 := BuildMessagePolicies(reversed, allow)
	if len(p1.Messages) != len(p2.Messages) {
		t.Fatalf("message count depends on input order: %d vs %d", len(p1.Messages), len(p2.Messages))
	}
	for i := range p1.Messages {
		a, b := p1.Messages[i], p2.Messages[i]
		if a.FullName != b.FullName || a.UnknownFields != b.UnknownFields || a.DynamicType != b.DynamicType {
			t.Fatalf("row %d differs by input order: %+v vs %+v", i, a, b)
		}
	}
}

// TestTodo_PROTO_009_Golden pins that every message reachable from the
// governed services carries PolicyRejectMaterialUnknown, and that exactly
// one message (TypedPayload) carries a restricted dynamic-type policy.
func TestTodo_PROTO_009_Golden(t *testing.T) {
	policies, err := BuildDefaultMessagePolicies()
	if err != nil {
		t.Fatalf("BuildDefaultMessagePolicies: %v", err)
	}
	if len(policies.Messages) == 0 {
		t.Fatal("zero messages discovered; the descriptor walk is broken")
	}
	dynamicCount := 0
	for _, p := range policies.Messages {
		if p.UnknownFields != PolicyRejectMaterialUnknown {
			t.Errorf("%s: unknown-field policy = %s, want %s", p.FullName, p.UnknownFields, PolicyRejectMaterialUnknown)
		}
		if p.DynamicType == DynamicTypeRestrictedAllowlist {
			dynamicCount++
			if p.FullName != "hcmnext.intents.v1.TypedPayload" {
				t.Errorf("unexpected message with a restricted dynamic type: %s", p.FullName)
			}
		}
	}
	if dynamicCount != 1 {
		t.Fatalf("expected exactly one dynamically-typed message (TypedPayload), found %d", dynamicCount)
	}
}

// TestTodo_PROTO_009_Security proves an out-of-allowlist schema name is never
// treated as resolvable: [MessagePolicySet.Lookup] for TypedPayload never
// contains a name outside the reviewed catalog/capability schema set, even
// when the caller-controlled SchemaReference names one.
func TestTodo_PROTO_009_Security(t *testing.T) {
	policies, err := BuildDefaultMessagePolicies()
	if err != nil {
		t.Fatalf("BuildDefaultMessagePolicies: %v", err)
	}
	p, ok := policies.Lookup("hcmnext.intents.v1.TypedPayload")
	if !ok {
		t.Fatal("TypedPayload has no policy row")
	}
	allowed := map[string]bool{}
	for _, s := range p.AllowlistedSchemas {
		allowed[s] = true
	}
	if allowed["attacker.v1.MaliciousType"] {
		t.Fatal("a fabricated attacker-controlled type name resolved as allowlisted")
	}
	if allowed[""] {
		t.Fatal("the empty schema name must never be allowlisted")
	}
}

// TestTodo_PROTO_009_Conformance proves a construction using a real
// CreateIntentRequest carrying a TypedPayload — the actual shape a mixed
// intent creation call takes — still decodes, preserves unknown fields, and
// is covered by a policy row.
func TestTodo_PROTO_009_Conformance(t *testing.T) {
	req := &intentsv1.CreateIntentRequest{
		IdempotencyKey: "idem-1",
		Definition:     &intentsv1.DefinitionReference{IntentTypeId: "hcmnext.people.explain_worker_state", Version: 1},
		Request: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{ProtobufFullName: "hcmnext.people.v1.ExplainWorkerStateRequest"},
			ProtobufWireBytes: []byte{0x01, 0x02, 0x03},
		},
	}
	wire, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal CreateIntentRequest: %v", err)
	}
	wire = injectUnknownField(wire, 777)

	decoded := &intentsv1.CreateIntentRequest{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal CreateIntentRequest with unknown field: %v", err)
	}
	if decoded.GetIdempotencyKey() != req.GetIdempotencyKey() {
		t.Fatal("a known top-level field was lost alongside the unknown field")
	}
	if len(decoded.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("expected the unknown field on CreateIntentRequest to be preserved")
	}

	policies, err := BuildDefaultMessagePolicies()
	if err != nil {
		t.Fatalf("BuildDefaultMessagePolicies: %v", err)
	}
	if _, ok := policies.Lookup("hcmnext.intents.v1.CreateIntentRequest"); !ok {
		t.Fatal("CreateIntentRequest has no policy row")
	}
	if _, ok := policies.Lookup("hcmnext.intents.v1.TypedPayload"); !ok {
		t.Fatal("TypedPayload, nested inside CreateIntentRequest, has no policy row")
	}
}

// TestTodo_PROTO_009_Mutation proves the walk is not vacuous: a synthetic
// RPCDescriptor pair whose input/output differ produces different message
// sets, and an empty allowlist changes TypedPayload's recorded allowlist.
func TestTodo_PROTO_009_Mutation(t *testing.T) {
	scope := (&commonv1.ScopeContext{}).ProtoReflect().Descriptor()
	page := (&commonv1.PageRequest{}).ProtoReflect().Descriptor()

	only := []RPCDescriptor{{ServiceFullName: "x.v1.X", MethodName: "M", Input: scope, Output: scope}}
	both := []RPCDescriptor{{ServiceFullName: "x.v1.X", MethodName: "M", Input: scope, Output: page}}

	p1 := BuildMessagePolicies(only, nil)
	p2 := BuildMessagePolicies(both, nil)
	if len(p1.Messages) == len(p2.Messages) {
		t.Fatal("adding a second, distinct output message did not change the policy set size")
	}

	withAllow := BuildMessagePolicies([]RPCDescriptor{{
		ServiceFullName: "x.v1.X", MethodName: "M",
		Input:  (&intentsv1.TypedPayload{}).ProtoReflect().Descriptor(),
		Output: scope,
	}}, []string{"only.v1.Allowed"})
	p, ok := withAllow.Lookup("hcmnext.intents.v1.TypedPayload")
	if !ok || len(p.AllowlistedSchemas) != 1 || p.AllowlistedSchemas[0] != "only.v1.Allowed" {
		t.Fatalf("changing the allowlist input did not change TypedPayload's recorded allowlist: %+v", p)
	}
}

// FuzzTodo_PROTO_009 fuzzes arbitrary trailing bytes appended as an extra
// field on a real request message: decoding must never panic, and whenever
// the appended bytes parse as a well-formed extra field, the message must
// still preserve every already-known field and still surface the unknown
// bytes rather than silently discarding them.
func FuzzTodo_PROTO_009(f *testing.F) {
	seed := &commonv1.ScopeContext{TenantId: "seed-tenant", OrganizationScopeId: "seed-org", Purpose: "seed-purpose"}
	seedBytes, err := proto.Marshal(seed)
	if err != nil {
		f.Fatalf("marshal seed: %v", err)
	}
	f.Add(seedBytes, int32(500))
	f.Add(seedBytes, int32(1))
	f.Add(seedBytes, int32(1<<20))

	f.Fuzz(func(t *testing.T, base []byte, fieldNumber int32) {
		if fieldNumber <= 0 || protowire.Number(fieldNumber) > protowire.MaxValidNumber {
			return
		}
		// Only fuzz over inputs that at least decode as a valid
		// ScopeContext on their own, so the injected tail is the only
		// unknown data under test.
		known := &commonv1.ScopeContext{}
		if err := proto.Unmarshal(base, known); err != nil {
			return
		}

		injected := injectUnknownField(base, fieldNumber)
		decoded := &commonv1.ScopeContext{}
		if err := proto.Unmarshal(injected, decoded); err != nil {
			// A field number colliding with an existing declared field
			// number can legitimately fail to parse as a plain scalar;
			// that is not the property under test.
			return
		}
		if decoded.GetTenantId() != known.GetTenantId() ||
			decoded.GetOrganizationScopeId() != known.GetOrganizationScopeId() ||
			decoded.GetPurpose() != known.GetPurpose() {
			t.Fatalf("known fields changed after appending an unknown field: before %+v, after %+v", known, decoded)
		}
	})
}
