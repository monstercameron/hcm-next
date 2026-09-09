package protomap_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", name, err)
	}
	b = append(b, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(b, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, b, want)
	}
}

const uuidA = "018f3f52-4a7b-7c31-9c6a-0a1b2c3d4e5f"

func mustKey(t *testing.T, segments ...string) values.ResourceKey {
	t.Helper()
	k, err := values.NewResourceKey(values.TenantId("acme-eu"), values.Kind("assignment"), segments...)
	if err != nil {
		t.Fatalf("resource key: %v", err)
	}
	return k
}

// TestTodo_MODEL_001 is the PRIMARY test for the remaining canonical-identifier
// clause: round-tripping through Protobuf.
//
// RED: canonical vectors reject empty or wrong-kind ids, tenantless resource
// keys and ambiguous revision selectors.
//
// GREEN: EntityId, EntityRef, ResourceKey and RevisionToken round-trip through
// Protobuf and Go without losing kind, tenant or revision intent.
func TestTodo_MODEL_001(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		t.Run("empty or wrong-kind entity id", func(t *testing.T) {
			for _, tc := range []struct {
				name string
				msg  *commonv1.EntityRef
			}{
				{"no kind", &commonv1.EntityRef{Id: uuidA}},
				{"no id", &commonv1.EntityRef{Kind: "worker"}},
				{"kind is a display label", &commonv1.EntityRef{Kind: "Worker", Id: uuidA}},
				{"id is not a UUID or ULID", &commonv1.EntityRef{Kind: "worker", Id: "worker-1"}},
				{"uppercase UUID spelling", &commonv1.EntityRef{
					Kind: "worker", Id: "018F3F52-4A7B-7C31-9C6A-0A1B2C3D4E5F",
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if _, err := protomap.EntityIDFromProto(tc.msg); err == nil {
						t.Fatalf("decoded an invalid entity id")
					}
				})
			}
		})

		t.Run("a tenantless reference is not an EntityRef", func(t *testing.T) {
			msg := &commonv1.EntityRef{Kind: "worker", Id: uuidA}
			if _, err := protomap.EntityRefFromProto(msg); !errors.Is(err, protomap.ErrLossyMapping) {
				t.Fatalf("a tenantless EntityRef decoded: %v", err)
			}
			// The same bytes are a perfectly good tenant-agnostic identity.
			if _, err := protomap.EntityIDFromProto(msg); err != nil {
				t.Fatalf("a tenant-agnostic identity failed to decode: %v", err)
			}
		})

		t.Run("a tenant-scoped reference is not an EntityId", func(t *testing.T) {
			msg := &commonv1.EntityRef{TenantId: "acme-eu", Kind: "worker", Id: uuidA}
			if _, err := protomap.EntityIDFromProto(msg); !errors.Is(err, protomap.ErrLossyMapping) {
				t.Fatalf("decoding a tenant-scoped reference as a bare id silently dropped the tenant: %v", err)
			}
		})

		t.Run("tenantless and inconsistent resource keys", func(t *testing.T) {
			key := mustKey(t, "employment", "9001")
			good, err := protomap.ResourceKeyToProto(key, "org:1", "employment", 1)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			for _, tc := range []struct {
				name string
				msg  *commonv1.ResourceKey
			}{
				{"no tenant", &commonv1.ResourceKey{
					ResourceType: "assignment", KeyBytes: good.GetKeyBytes(),
				}},
				{"no canonical bytes", &commonv1.ResourceKey{
					TenantId: "acme-eu", ResourceType: "assignment",
				}},
				{"tenant field disagrees with the canonical bytes", &commonv1.ResourceKey{
					TenantId: "other-tenant", ResourceType: "assignment",
					KeyBytes: good.GetKeyBytes(),
				}},
				{"type field disagrees with the canonical bytes", &commonv1.ResourceKey{
					TenantId: "acme-eu", ResourceType: "employment",
					KeyBytes: good.GetKeyBytes(),
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if _, err := protomap.ResourceKeyFromProto(tc.msg); err == nil {
						t.Fatalf("decoded an invalid resource key")
					}
				})
			}
		})

		t.Run("ambiguous revision selectors", func(t *testing.T) {
			for _, tc := range []struct {
				name string
				msg  *commonv1.RevisionToken
			}{
				{"a revision with no source authority", &commonv1.RevisionToken{Revision: 7}},
				{"CAS bytes with no source authority", &commonv1.RevisionToken{
					OpaqueCasBytes: []byte{1, 2, 3},
				}},
				{"both a sequence and CAS bytes", &commonv1.RevisionToken{
					SourceAuthorityId: "stream", Revision: 7, OpaqueCasBytes: []byte{1, 2, 3},
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if _, err := protomap.RevisionTokenFromProto(tc.msg); err == nil {
						t.Fatalf("decoded an ambiguous revision selector")
					}
				})
			}
		})

		t.Run("a nil message is never a zero value", func(t *testing.T) {
			if _, err := protomap.EntityRefFromProto(nil); !errors.Is(err, protomap.ErrNilMessage) {
				t.Fatalf("nil EntityRef decoded to a value")
			}
			if _, err := protomap.ResourceKeyFromProto(nil); !errors.Is(err, protomap.ErrNilMessage) {
				t.Fatalf("nil ResourceKey decoded to a value")
			}
			if _, err := protomap.RevisionTokenFromProto(nil); !errors.Is(err, protomap.ErrNilMessage) {
				t.Fatalf("nil RevisionToken decoded to a value")
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		t.Run("EntityId keeps its kind", func(t *testing.T) {
			id := values.EntityId{Kind: values.Kind("worker"), Id: uuidA}
			msg, err := protomap.EntityIDToProto(id)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			// Survive an actual wire round trip, not just a struct copy.
			back, err := protomap.EntityIDFromProto(roundTrip(t, msg).(*commonv1.EntityRef))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if back != id {
				t.Fatalf("round trip changed the id: %+v -> %+v", id, back)
			}
			if !bytes.Equal(back.Canonical(), id.Canonical()) {
				t.Fatalf("canonical encoding drifted")
			}
		})

		t.Run("EntityRef keeps its tenant", func(t *testing.T) {
			ref := values.EntityRef{
				Tenant: values.TenantId("acme-eu"), Kind: values.Kind("worker"), Id: uuidA,
			}
			msg, err := protomap.EntityRefToProto(ref)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			back, err := protomap.EntityRefFromProto(roundTrip(t, msg).(*commonv1.EntityRef))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if back != ref {
				t.Fatalf("round trip changed the reference: %+v -> %+v", ref, back)
			}
		})

		t.Run("ResourceKey keeps its ordered segments", func(t *testing.T) {
			// Segments containing the canonical separators are the interesting
			// case: a joined display string would let them inject a path
			// element.
			key := mustKey(t, "employment/9001", "primary:assignment", "café")
			msg, err := protomap.ResourceKeyToProto(key, "org:1", "employment", 3)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			back, err := protomap.ResourceKeyFromProto(roundTrip(t, msg).(*commonv1.ResourceKey))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !back.Equal(key) {
				t.Fatalf("round trip changed the key: %v -> %v", key.Segments, back.Segments)
			}
			if len(back.Segments) != 3 {
				t.Fatalf("segment count changed to %d", len(back.Segments))
			}
		})

		t.Run("RevisionToken keeps its revision intent", func(t *testing.T) {
			seq, err := values.NewSequenceRevision("people.employment.9001", 42)
			if err != nil {
				t.Fatalf("sequence: %v", err)
			}
			zero, err := values.NewSequenceRevision("people.employment.9001", 0)
			if err != nil {
				t.Fatalf("zero sequence: %v", err)
			}
			opaque, err := values.NewOpaqueRevision("people.employment.9001", []byte{0xde, 0xad})
			if err != nil {
				t.Fatalf("opaque: %v", err)
			}
			for _, tc := range []struct {
				name  string
				token values.RevisionToken
			}{
				{"unspecified", values.UnspecifiedRevision()},
				{"sequence", seq},
				{"sequence zero", zero},
				{"opaque", opaque},
			} {
				t.Run(tc.name, func(t *testing.T) {
					msg, err := protomap.RevisionTokenToProto(tc.token, nil, values.Instant{})
					if err != nil {
						t.Fatalf("encode: %v", err)
					}
					back, err := protomap.RevisionTokenFromProto(
						roundTrip(t, msg).(*commonv1.RevisionToken))
					if err != nil {
						t.Fatalf("decode: %v", err)
					}
					if !back.Equal(tc.token) {
						t.Fatalf("round trip changed the token: %v -> %v", tc.token, back)
					}
					if back.IsSpecified() != tc.token.IsSpecified() {
						t.Fatalf("round trip changed whether the token is specified")
					}
				})
			}

			// The distinction that a naive mapping loses: sequence zero is an
			// explicit revision expectation, "unspecified" is none.
			unspecified := values.UnspecifiedRevision()
			if zero.Equal(unspecified) {
				t.Fatalf("sequence zero and unspecified are the same value in Go")
			}
			zeroMsg, err := protomap.RevisionTokenToProto(zero, nil, values.Instant{})
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			unspecifiedMsg, err := protomap.RevisionTokenToProto(unspecified, nil, values.Instant{})
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if proto.Equal(zeroMsg, unspecifiedMsg) {
				t.Fatalf("sequence zero and unspecified encode identically")
			}
		})
	})
}

// roundTrip marshals and unmarshals a message so the test exercises the wire
// format rather than a struct copy.
func roundTrip(t *testing.T, msg proto.Message) proto.Message {
	t.Helper()
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := msg.ProtoReflect().New().Interface()
	if err := proto.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// TestTodo_MODEL_001_Property round-trips a table of values and asserts that
// every one comes back canonically identical.
func TestTodo_MODEL_001_Property(t *testing.T) {
	t.Run("entity references", func(t *testing.T) {
		for _, kind := range []string{"worker", "employment", "position_occupancy", "org_unit"} {
			for _, id := range []string{uuidA, "01HZ8V5Q7XABCDEFGHJKMNPQRS"} {
				ref := values.EntityRef{
					Tenant: values.TenantId("acme-eu"), Kind: values.Kind(kind), Id: id,
				}
				if ref.Validate() != nil {
					continue
				}
				msg, err := protomap.EntityRefToProto(ref)
				if err != nil {
					t.Fatalf("%v: %v", ref, err)
				}
				back, err := protomap.EntityRefFromProto(msg)
				if err != nil {
					t.Fatalf("%v: %v", ref, err)
				}
				if !bytes.Equal(back.Canonical(), ref.Canonical()) {
					t.Fatalf("%v did not round-trip canonically", ref)
				}
			}
		}
	})

	t.Run("decimals and money", func(t *testing.T) {
		for _, tc := range []struct {
			text  string
			scale int32
		}{
			{"0", 2}, {"0.00", 2}, {"1.50", 2}, {"-1.50", 2},
			{"123456789012345678", 0}, {"-0.000000001", 9},
		} {
			d, err := values.NewDecimal(tc.text, tc.scale, values.RoundingHalfEven)
			if err != nil {
				t.Fatalf("decimal %q: %v", tc.text, err)
			}
			msg, err := protomap.DecimalToProto(d)
			if err != nil {
				t.Fatalf("encode %q: %v", tc.text, err)
			}
			back, err := protomap.DecimalFromProto(msg, values.RoundingHalfEven)
			if err != nil {
				t.Fatalf("decode %q: %v", tc.text, err)
			}
			if !bytes.Equal(back.Canonical(), d.Canonical()) {
				t.Fatalf("decimal %q round-tripped to %q", d.String(), back.String())
			}
		}

		m, err := values.NewMoney("1234.56", "EUR", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("money: %v", err)
		}
		msg, err := protomap.MoneyToProto(m)
		if err != nil {
			t.Fatalf("encode money: %v", err)
		}
		back, err := protomap.MoneyFromProto(msg, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("decode money: %v", err)
		}
		if !bytes.Equal(back.Canonical(), m.Canonical()) {
			t.Fatalf("money round-tripped to %q", back.String())
		}
	})

	t.Run("presence never collapses", func(t *testing.T) {
		seen := map[commonv1.Presence]bool{}
		for _, s := range values.AllPresenceStates() {
			p, err := protomap.PresenceToProto(s)
			if err != nil {
				// PRESENCE_UNSPECIFIED is never a valid wire assertion.
				if s == values.PresenceUnspecified {
					continue
				}
				t.Fatalf("encode %v: %v", s, err)
			}
			if seen[p] {
				t.Fatalf("two presence states encode to %v", p)
			}
			seen[p] = true
			back, err := protomap.PresenceFromProto(p)
			if err != nil {
				t.Fatalf("decode %v: %v", p, err)
			}
			if back != s {
				t.Fatalf("presence %v round-tripped to %v", s, back)
			}
		}
		if _, err := protomap.PresenceFromProto(commonv1.Presence_PRESENCE_UNSPECIFIED); err == nil {
			t.Fatalf("PRESENCE_UNSPECIFIED decoded to a state")
		}
	})

	t.Run("kernel enums", func(t *testing.T) {
		for _, f := range intent.Families() {
			p, err := protomap.FamilyToProto(f)
			if err != nil {
				t.Fatalf("encode %v: %v", f, err)
			}
			back, err := protomap.FamilyFromProto(p)
			if err != nil {
				t.Fatalf("decode %v: %v", p, err)
			}
			if back != f {
				t.Fatalf("family %v round-tripped to %v", f, back)
			}
		}
		// The reserved numbers name the retired family rather than decoding.
		for _, n := range []int32{2, 4, 5, 6} {
			_, err := protomap.FamilyFromProto(intentsv1.KernelFamily(n))
			if !errors.Is(err, protomap.ErrUnknownEnum) {
				t.Fatalf("reserved family number %d decoded: %v", n, err)
			}
		}
		// CATALOGUED is retired and its number must not decode.
		if _, err := protomap.MaturityFromProto(intentsv1.DefinitionMaturity(1)); !errors.Is(err, protomap.ErrUnknownEnum) {
			t.Fatalf("the retired CATALOGUED maturity decoded: %v", err)
		}
	})

	t.Run("lifecycle dimensions", func(t *testing.T) {
		d := lifecycle.Dimensions{
			Request: lifecycle.RequestSimulated, Execution: lifecycle.ExecutionNotPlanned,
			Business: lifecycle.BusinessInProgress, Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation: lifecycle.ObligationUnknown,
		}
		msg, err := protomap.DimensionsToProto(d)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		back, err := protomap.DimensionsFromProto(roundTrip(t, msg).(*intentsv1.LifecycleDimensions))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if back != d {
			t.Fatalf("dimensions round-tripped to %v", back)
		}
		// The reserved ObligationState number where DISPUTED used to sit must
		// not decode.
		bad := &intentsv1.LifecycleDimensions{
			Request:    intentsv1.RequestState_REQUEST_STATE_DRAFT,
			Obligation: intentsv1.ObligationState(6),
		}
		if _, err := protomap.DimensionsFromProto(bad); err == nil {
			t.Fatalf("the reserved ObligationState number decoded")
		}
	})
}

// TestTodo_MODEL_001_Golden pins the canonical text of a fixed set of
// identifiers after a full Protobuf round trip.
func TestTodo_MODEL_001_Golden(t *testing.T) {
	type row struct{ Name, Canonical string }
	var rows []row

	id := values.EntityId{Kind: values.Kind("worker"), Id: uuidA}
	idMsg, err := protomap.EntityIDToProto(id)
	if err != nil {
		t.Fatalf("encode entity id: %v", err)
	}
	backID, err := protomap.EntityIDFromProto(roundTrip(t, idMsg).(*commonv1.EntityRef))
	if err != nil {
		t.Fatalf("decode entity id: %v", err)
	}
	rows = append(rows, row{"entity_id", backID.String()})

	ref := values.EntityRef{Tenant: "acme-eu", Kind: values.Kind("employment"), Id: uuidA}
	refMsg, err := protomap.EntityRefToProto(ref)
	if err != nil {
		t.Fatalf("encode entity ref: %v", err)
	}
	backRef, err := protomap.EntityRefFromProto(roundTrip(t, refMsg).(*commonv1.EntityRef))
	if err != nil {
		t.Fatalf("decode entity ref: %v", err)
	}
	rows = append(rows, row{"entity_ref", backRef.String()})

	key := mustKey(t, "employment/9001", "primary:assignment", "café")
	keyMsg, err := protomap.ResourceKeyToProto(key, "org:acme-eu:engineering", "employment", 1)
	if err != nil {
		t.Fatalf("encode resource key: %v", err)
	}
	backKey, err := protomap.ResourceKeyFromProto(roundTrip(t, keyMsg).(*commonv1.ResourceKey))
	if err != nil {
		t.Fatalf("decode resource key: %v", err)
	}
	rows = append(rows, row{"resource_key", backKey.String()})

	seq, err := values.NewSequenceRevision("people.employment.9001", 42)
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	opaque, err := values.NewOpaqueRevision("people.employment.9001", []byte{0xde, 0xad, 0xbe, 0xef})
	if err != nil {
		t.Fatalf("opaque: %v", err)
	}
	for name, token := range map[string]values.RevisionToken{
		"revision_unspecified": values.UnspecifiedRevision(),
		"revision_sequence":    seq,
		"revision_opaque":      opaque,
	} {
		msg, err := protomap.RevisionTokenToProto(token, nil, values.Instant{})
		if err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		back, err := protomap.RevisionTokenFromProto(roundTrip(t, msg).(*commonv1.RevisionToken))
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		rows = append(rows, row{name, back.String()})
	}

	// Sort by name so map iteration cannot move the golden.
	for i := range rows {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].Name < rows[i].Name {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	goldenJSON(t, "model_001_identifiers.json", rows)
}

// TestTodo_MODEL_001_Fault proves the mapping fails closed rather than
// producing a plausible-looking value from a malformed message.
func TestTodo_MODEL_001_Fault(t *testing.T) {
	t.Run("a decimal whose sign disagrees with its magnitude", func(t *testing.T) {
		for _, msg := range []*commonv1.Decimal{
			{Sign: commonv1.DecimalSign_DECIMAL_SIGN_ZERO, UnscaledMagnitude: []byte{1}, Scale: 0},
			{Sign: commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE, Scale: 0},
			{Sign: commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE, Scale: 0},
			{Sign: commonv1.DecimalSign_DECIMAL_SIGN_UNSPECIFIED, UnscaledMagnitude: []byte{1}},
		} {
			if _, err := protomap.DecimalFromProto(msg, values.RoundingHalfEven); err == nil {
				t.Fatalf("decoded an inconsistent decimal %v", msg)
			}
		}
	})

	t.Run("an unset instant is not the epoch", func(t *testing.T) {
		if got := protomap.InstantToProto(values.Instant{}); got != nil {
			t.Fatalf("an unset instant encoded as %v rather than absence", got)
		}
		back, err := protomap.InstantFromProto(nil)
		if err != nil {
			t.Fatalf("decode nil instant: %v", err)
		}
		if back.IsSet() {
			t.Fatalf("a nil timestamp decoded to a set instant")
		}
	})

	t.Run("a schema reference with no version is refused", func(t *testing.T) {
		_, err := protomap.SchemaRefFromProto(&intentsv1.SchemaReference{SchemaId: "x"})
		if err == nil {
			t.Fatalf("decoded an unversioned schema reference")
		}
	})

	t.Run("a definition reference with a zero version is refused", func(t *testing.T) {
		_, err := protomap.DefinitionRefFromProto(&intentsv1.DefinitionReference{
			IntentTypeId: "hcmnext.people.promote_worker",
		})
		if !errors.Is(err, intent.ErrInvalidReference) {
			t.Fatalf("decoded a zero-version definition reference: %v", err)
		}
	})
}

// TestTodo_MODEL_001_Security proves an encoded identifier cannot be forged
// into a different tenant or kind by manipulating the redundant scalar fields
// beside the canonical bytes.
func TestTodo_MODEL_001_Security(t *testing.T) {
	key := mustKey(t, "employment", "9001")
	msg, err := protomap.ResourceKeyToProto(key, "org:1", "employment", 1)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	forged := proto.Clone(msg).(*commonv1.ResourceKey)
	forged.TenantId = "victim-tenant"
	if _, err := protomap.ResourceKeyFromProto(forged); !errors.Is(err, protomap.ErrLossyMapping) {
		t.Fatalf("a forged tenant field was accepted: %v", err)
	}

	forged = proto.Clone(msg).(*commonv1.ResourceKey)
	forged.ResourceType = "compensation"
	if _, err := protomap.ResourceKeyFromProto(forged); !errors.Is(err, protomap.ErrLossyMapping) {
		t.Fatalf("a forged resource type was accepted: %v", err)
	}

	// A truncated canonical byte string must not decode into a shorter key.
	forged = proto.Clone(msg).(*commonv1.ResourceKey)
	forged.KeyBytes = forged.KeyBytes[:len(forged.KeyBytes)-4]
	if _, err := protomap.ResourceKeyFromProto(forged); err == nil {
		t.Fatalf("a truncated canonical key decoded")
	}
}

// FuzzTodo_MODEL_001 fuzzes the identifier mappings. Anything that decodes must
// round-trip canonically; anything that does not must fail rather than produce
// a value.
func FuzzTodo_MODEL_001(f *testing.F) {
	f.Add("acme-eu", "worker", uuidA, "stream", uint64(0), []byte(nil))
	f.Add("", "worker", uuidA, "", uint64(7), []byte(nil))
	f.Add("acme-eu", "Worker", uuidA, "stream", uint64(0), []byte{1, 2})
	f.Add("acme-eu", "worker", "not-a-uuid", "stream", uint64(3), []byte(nil))
	f.Fuzz(func(t *testing.T, tenant, kind, id, stream string, revision uint64, cas []byte) {
		entity := &commonv1.EntityRef{TenantId: tenant, Kind: kind, Id: id}
		if ref, err := protomap.EntityRefFromProto(entity); err == nil {
			if ref.Tenant == "" {
				t.Fatalf("decoded an EntityRef with no tenant")
			}
			back, err := protomap.EntityRefToProto(ref)
			if err != nil {
				t.Fatalf("a decoded reference failed to re-encode: %v", err)
			}
			again, err := protomap.EntityRefFromProto(back)
			if err != nil || again != ref {
				t.Fatalf("reference did not round-trip: %v / %v", again, err)
			}
		}
		if bare, err := protomap.EntityIDFromProto(entity); err == nil {
			if tenant != "" {
				t.Fatalf("decoded a tenant-scoped message as a bare entity id")
			}
			if bare.Kind == "" || bare.Id == "" {
				t.Fatalf("decoded an entity id with no kind or id")
			}
		}

		token := &commonv1.RevisionToken{
			SourceAuthorityId: stream, Revision: revision, OpaqueCasBytes: cas,
		}
		rev, err := protomap.RevisionTokenFromProto(token)
		if err != nil {
			return
		}
		if !rev.IsSpecified() {
			if revision != 0 || len(cas) > 0 {
				t.Fatalf("a token naming a revision decoded as unspecified")
			}
			return
		}
		if rev.Stream() == "" {
			t.Fatalf("a specified token decoded with no stream")
		}
		back, err := protomap.RevisionTokenToProto(rev, nil, values.Instant{})
		if err != nil {
			t.Fatalf("a decoded token failed to re-encode: %v", err)
		}
		again, err := protomap.RevisionTokenFromProto(back)
		if err != nil {
			t.Fatalf("a re-encoded token failed to decode: %v", err)
		}
		if !again.Equal(rev) {
			t.Fatalf("token did not round-trip: %v -> %v", rev, again)
		}
	})
}

// TestTodo_MODEL_001_Integration round-trips whole kernel objects — a
// definition, an instance envelope and a proposal revision — through the
// generated contracts, since those are what actually cross a wire.
func TestTodo_MODEL_001_Integration(t *testing.T) {
	reg, err := definitions.NewRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("digester: %v", err)
	}
	def, err := reg.ResolveText("hcmnext.people.promote_worker/v1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	t.Run("definition", func(t *testing.T) {
		msg, err := protomap.DefinitionToProto(def)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		back, err := protomap.DefinitionFromProto(roundTrip(t, msg).(*intentsv1.IntentDefinition))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for name, ok := range map[string]bool{
			"reference":     back.Ref == def.Ref,
			"display name":  back.DisplayName == def.DisplayName,
			"family":        back.Family == def.Family,
			"maturity":      back.Maturity == def.Maturity,
			"side effect":   back.SideEffect == def.SideEffect,
			"input schema":  back.InputSchema == def.InputSchema,
			"result schema": back.ResultSchema == def.ResultSchema,
			"phase depth":   back.PhaseDepth == def.PhaseDepth,
			"risk class":    back.RiskClass == def.RiskClass,
		} {
			if !ok {
				t.Fatalf("definition round trip lost the %s", name)
			}
		}
		if len(back.AllowedModes) != len(def.AllowedModes) ||
			len(back.AllowedInitiators) != len(def.AllowedInitiators) ||
			len(back.RequiredCapabilities) != len(def.RequiredCapabilities) {
			t.Fatalf("definition round trip lost a repeated field")
		}
	})

	t.Run("instance envelope", func(t *testing.T) {
		at := values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
		clock := func() values.Instant {
			return values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
		}
		inst, _, err := intent.NewInstance(intent.InstanceSpec{
			Tenant:              values.TenantId("acme-eu"),
			OrganizationScopeID: "org:acme-eu:engineering",
			Initiator: intent.PrincipalReference{
				PrincipalID: "principal:1", Kind: intent.InitiatorHuman,
				IdentityAssuranceRef: "assurance.mfa_session/v1",
			},
			Purpose: "promotion.annual_cycle",
			Subjects: []intent.SubjectReference{
				{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			},
			RequestedEffectiveAt: &at,
			Request: intent.TypedPayload{
				Schema:    def.InputSchema,
				WireBytes: []byte{0x0a, 0x01, 'x'},
			},
			IdempotencyKey: "idem:1", CorrelationID: "corr:1", TraceID: "trace:1",
			Classification: "CONFIDENTIAL_HR", RetentionClass: "WORKER_TRANSACTION",
			ControlSnapshots: intent.ControlSnapshots{
				CapabilityRegistryDigest:     "cap-1",
				PolicyBundleDigest:           "policy-1",
				LegalContextDigest:           "legal-1",
				EntitlementDigest:            "ent-1",
				ReferenceDataDigest:          "ref-1",
				ClassificationTaxonomyDigest: "tax-1",
				DLPDecisionDigest:            "dlp-1",
			},
			ExecutionMode:                 intent.ModeSimulate,
			SourceAuthoritySnapshotDigest: "authority-1",
			RiskContextDigest:             "risk-1",
		}, def, d, nil, clock)
		if err != nil {
			t.Fatalf("create instance: %v", err)
		}

		msg, err := protomap.InstanceToProto(inst)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		back, err := protomap.InstanceFromProto(roundTrip(t, msg).(*intentsv1.IntentInstance))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for name, ok := range map[string]bool{
			"intent id":       back.IntentID == inst.IntentID,
			"definition":      back.Definition == inst.Definition,
			"tenant":          back.Tenant == inst.Tenant,
			"initiator":       back.Initiator == inst.Initiator,
			"idempotency key": back.IdempotencyKey == inst.IdempotencyKey,
			"lifecycle":       back.Lifecycle == inst.Lifecycle,
			"execution mode":  back.ExecutionMode == inst.ExecutionMode,
			"request digest":  back.CanonicalRequestDigest.Digest == inst.CanonicalRequestDigest.Digest,
			"created at":      back.CreatedAt == inst.CreatedAt,
			"control context": back.ControlSnapshots == inst.ControlSnapshots,
		} {
			if !ok {
				t.Fatalf("instance round trip lost the %s", name)
			}
		}
		if back.RequestedEffectiveAt == nil || *back.RequestedEffectiveAt != at {
			t.Fatalf("instance round trip lost the requested effective time")
		}
		if !bytes.Equal(back.Request.WireBytes, inst.Request.WireBytes) {
			t.Fatalf("instance round trip lost the typed payload bytes")
		}
		// The digest survives the round trip and still verifies, which is the
		// only test that matters for idempotency across a process boundary.
		if err := d.VerifyRequestDigest(back); err != nil {
			t.Fatalf("the round-tripped envelope no longer verifies: %v", err)
		}
	})
}
