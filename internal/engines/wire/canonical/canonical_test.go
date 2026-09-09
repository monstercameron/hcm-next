package canonical_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/canonical"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden canonical vectors")

// golden compares got against the checked-in vector at testdata/name. The
// checked-in bytes are the contract: a change to them is a change to every
// digest ever minted under the profile that produced them.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical bytes drifted from %s\n got %d bytes: %x\nwant %d bytes: %x",
			path, len(got), got, len(want), want)
	}
}

// proposalProfile is the profile under test for the ProposalRevision vectors.
// It deliberately excludes control_snapshots, created_by, created_at and
// invalidator_refs so the nonmaterial-change assertions have something to move.
func proposalProfile() canonical.Profile {
	return canonical.Profile{
		ID:            "test.proposal",
		Version:       1,
		SchemaID:      "hcmnext.intents.v1",
		SchemaVersion: 1,
		MessageName:   "hcmnext.intents.v1.ProposalRevision",
		Material: []string{
			"proposal_revision_id",
			"intent_id",
			"revision",
			"proposal.schema.schema_id",
			"proposal.schema.version",
			"proposal.schema.protobuf_full_name",
			"proposal.protobuf_wire_bytes",
			"supersedes_proposal_revision_id",
		},
		RejectUnknownFields: true,
	}
}

// setProfile treats invalidator_refs as a set: their order carries no meaning.
func setProfile() canonical.Profile {
	p := proposalProfile()
	p.ID = "test.proposal.sets"
	p.Material = append(p.Material, "invalidator_refs")
	p.Sets = []string{"invalidator_refs"}
	return p
}

// listProfile treats the same field as an ordered list.
func listProfile() canonical.Profile {
	p := setProfile()
	p.ID = "test.proposal.lists"
	p.Sets = nil
	return p
}

// subjectProfile exercises the declared string disciplines: NFC text, verbatim
// bytes, a fixed decimal, and an ISO currency code.
func subjectProfile() canonical.Profile {
	return canonical.Profile{
		ID:            "test.subject",
		Version:       1,
		SchemaID:      "hcmnext.intents.v1",
		SchemaVersion: 1,
		MessageName:   "hcmnext.intents.v1.SubjectReference",
		Material:      []string{"subject_kind", "subject_id", "authority_domain"},
		Decimals:      []canonical.Decimal{{Path: "subject_id"}},
		Currency:      []string{"authority_domain"},
	}
}

// structProfile exercises maps, enums, doubles and nested oneofs via a
// well-known message that actually has a map field.
func structProfile() canonical.Profile {
	return canonical.Profile{
		ID:                  "test.struct",
		Version:             1,
		SchemaID:            "google.protobuf",
		SchemaVersion:       1,
		MessageName:         "google.protobuf.Struct",
		Material:            []string{"fields"},
		RejectUnknownFields: true,
	}
}

func baseProposal() *intentsv1.ProposalRevision {
	return &intentsv1.ProposalRevision{
		ProposalRevisionId: "pr-0001",
		IntentId:           "int-0001",
		Revision:           7,
		Proposal: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId:         "hcmnext.people.v1.HireRequest",
				Version:          3,
				ProtobufFullName: "hcmnext.people.v1.HireRequest",
				DescriptorDigest: "sha256:aa",
			},
			ProtobufWireBytes: []byte{0x0a, 0x03, 'a', 'b', 'c'},
		},
		ControlSnapshots: &intentsv1.ControlSnapshotReferences{
			PolicyBundleDigest: "sha256:policy-v1",
		},
		CreatedBy: &intentsv1.PrincipalReference{PrincipalId: "user-1"},
		CreatedAt: timestamppb.New(mustTime("2026-01-02T03:04:05.000000006Z")),
	}
}

func mustEncode(t *testing.T, msg proto.Message, p canonical.Profile) []byte {
	t.Helper()
	b, err := canonical.Encode(msg, p)
	if err != nil {
		t.Fatalf("encode under %s: %v", p.ID, err)
	}
	return b
}

func TestTodo_MODEL_006(t *testing.T) {
	t.Run("field order on the wire is immaterial", func(t *testing.T) {
		// The same material meaning, serialized with fields emitted in
		// ascending and descending tag order, must canonicalize identically.
		ascending := wireProposal(t, false)
		descending := wireProposal(t, true)
		if bytes.Equal(ascending, descending) {
			t.Fatal("the two wire encodings are identical; the test proves nothing")
		}
		a := decodeProposal(t, ascending)
		b := decodeProposal(t, descending)
		if got, want := mustEncode(t, a, proposalProfile()), mustEncode(t, b, proposalProfile()); !bytes.Equal(got, want) {
			t.Fatalf("wire field order changed canonical bytes:\n%x\n%x", got, want)
		}
	})

	t.Run("map iteration order is immaterial", func(t *testing.T) {
		forward := mustStruct(t, []string{"zulu", "alpha", "mike"})
		reverse := mustStruct(t, []string{"mike", "alpha", "zulu"})
		first := mustEncode(t, forward, structProfile())
		second := mustEncode(t, reverse, structProfile())
		if !bytes.Equal(first, second) {
			t.Fatalf("map insertion order changed canonical bytes:\n%x\n%x", first, second)
		}
		// Go randomizes map iteration per range; repeat to catch an encoder
		// that accidentally depends on it.
		for i := range 32 {
			if again := mustEncode(t, forward, structProfile()); !bytes.Equal(first, again) {
				t.Fatalf("repeat %d produced different bytes:\n%x\n%x", i, first, again)
			}
		}
	})

	t.Run("strings normalize to NFC", func(t *testing.T) {
		composed := &intentsv1.SubjectReference{SubjectKind: composedE, SubjectId: "1", AuthorityDomain: "usd"}
		decomposed := &intentsv1.SubjectReference{SubjectKind: decomposedE, SubjectId: "1", AuthorityDomain: "usd"}
		if composed.GetSubjectKind() == decomposed.GetSubjectKind() {
			t.Fatal("the two spellings are identical; the test proves nothing")
		}
		a := mustEncode(t, composed, subjectProfile())
		b := mustEncode(t, decomposed, subjectProfile())
		if !bytes.Equal(a, b) {
			t.Fatalf("NFC normalization did not converge:\n%x\n%x", a, b)
		}

		// A verbatim field is exempt: the two spellings stay distinct.
		verbatim := subjectProfile()
		verbatim.ID = "test.subject.verbatim"
		verbatim.Verbatim = []string{"subject_kind"}
		if av, bv := mustEncode(t, composed, verbatim), mustEncode(t, decomposed, verbatim); bytes.Equal(av, bv) {
			t.Fatal("a verbatim field was normalized")
		}
	})

	t.Run("invalid UTF-8 is rejected", func(t *testing.T) {
		// A proto3 string field can still carry invalid UTF-8 when it arrives
		// through the wire without validation, so reject it at the encoder.
		msg := &intentsv1.SubjectReference{
			SubjectKind:     string([]byte{0xff, 0xfe}),
			SubjectId:       "1",
			AuthorityDomain: "usd",
		}
		_, err := canonical.Encode(msg, subjectProfile())
		if !errors.Is(err, canonical.ErrInvalidUTF8) {
			t.Fatalf("want ErrInvalidUTF8, got %v", err)
		}
	})

	t.Run("unknown fields are rejected on a material profile", func(t *testing.T) {
		msg := decodeProposal(t, wireProposal(t, false))
		msg.ProtoReflect().SetUnknown(fieldVarint(4242, 1))
		_, err := canonical.Encode(msg, proposalProfile())
		if !errors.Is(err, canonical.ErrUnknownField) {
			t.Fatalf("want ErrUnknownField, got %v", err)
		}
		// The same message canonicalizes fine under a profile that does not
		// claim to be material.
		lenient := proposalProfile()
		lenient.ID = "test.proposal.lenient"
		lenient.RejectUnknownFields = false
		if _, err := canonical.Encode(msg, lenient); err != nil {
			t.Fatalf("lenient profile rejected unknown fields: %v", err)
		}
	})

	t.Run("sets sort and reject duplicates", func(t *testing.T) {
		a := baseProposal()
		a.InvalidatorRefs = []string{"inv-c", "inv-a", "inv-b"}
		b := baseProposal()
		b.InvalidatorRefs = []string{"inv-a", "inv-b", "inv-c"}

		if x, y := mustEncode(t, a, setProfile()), mustEncode(t, b, setProfile()); !bytes.Equal(x, y) {
			t.Fatalf("set member order changed canonical bytes:\n%x\n%x", x, y)
		}
		// The same two values under an ordered list profile must differ:
		// otherwise the set behavior is vacuous.
		if x, y := mustEncode(t, a, listProfile()), mustEncode(t, b, listProfile()); bytes.Equal(x, y) {
			t.Fatal("an ordered list ignored element order")
		}

		dup := baseProposal()
		dup.InvalidatorRefs = []string{"inv-a", "inv-a"}
		_, err := canonical.Encode(dup, setProfile())
		if !errors.Is(err, canonical.ErrDuplicateSetMember) {
			t.Fatalf("want ErrDuplicateSetMember, got %v", err)
		}

		// Duplicates that differ only by Unicode form are still duplicates:
		// normalization happens before the set comparison.
		nfcDup := baseProposal()
		nfcDup.InvalidatorRefs = []string{composedE, decomposedE}
		if _, err := canonical.Encode(nfcDup, setProfile()); !errors.Is(err, canonical.ErrDuplicateSetMember) {
			t.Fatalf("want ErrDuplicateSetMember for NFC-equal members, got %v", err)
		}
	})

	t.Run("every material change moves the bytes", func(t *testing.T) {
		base := mustEncode(t, baseProposal(), proposalProfile())
		for name, mutate := range map[string]func(*intentsv1.ProposalRevision){
			"revision id":   func(p *intentsv1.ProposalRevision) { p.ProposalRevisionId = "pr-0002" },
			"intent id":     func(p *intentsv1.ProposalRevision) { p.IntentId = "int-0002" },
			"revision":      func(p *intentsv1.ProposalRevision) { p.Revision = 8 },
			"payload bytes": func(p *intentsv1.ProposalRevision) { p.Proposal.ProtobufWireBytes = []byte{0x01} },
			"payload schema id": func(p *intentsv1.ProposalRevision) {
				p.Proposal.Schema.SchemaId = "hcmnext.people.v1.TerminateRequest"
			},
			"payload schema version": func(p *intentsv1.ProposalRevision) { p.Proposal.Schema.Version = 4 },
			"supersession": func(p *intentsv1.ProposalRevision) {
				p.SupersedesProposalRevisionId = proto.String("pr-0000")
			},
			"payload cleared": func(p *intentsv1.ProposalRevision) { p.Proposal = nil },
		} {
			p := baseProposal()
			mutate(p)
			if got := mustEncode(t, p, proposalProfile()); bytes.Equal(got, base) {
				t.Errorf("material change %q did not change the canonical bytes", name)
			}
		}
	})

	t.Run("a nonmaterial change leaves the bytes identical", func(t *testing.T) {
		base := mustEncode(t, baseProposal(), proposalProfile())
		for name, mutate := range map[string]func(*intentsv1.ProposalRevision){
			// Republishing a policy bundle is exactly the case the canonical
			// envelope contract exists to protect: it must not invalidate a
			// pending approval.
			"policy bundle republished": func(p *intentsv1.ProposalRevision) {
				p.ControlSnapshots.PolicyBundleDigest = "sha256:policy-v2"
			},
			"classification taxonomy republished": func(p *intentsv1.ProposalRevision) {
				p.ControlSnapshots.ClassificationTaxonomyDigest = "sha256:taxonomy-v9"
			},
			"control snapshots cleared": func(p *intentsv1.ProposalRevision) { p.ControlSnapshots = nil },
			"created at":                func(p *intentsv1.ProposalRevision) { p.CreatedAt = timestamppb.New(mustTime("2030-06-01T00:00:00Z")) },
			"created by":                func(p *intentsv1.ProposalRevision) { p.CreatedBy = nil },
			"invalidator refs":          func(p *intentsv1.ProposalRevision) { p.InvalidatorRefs = []string{"inv-z"} },
			"payload descriptor digest": func(p *intentsv1.ProposalRevision) { p.Proposal.Schema.DescriptorDigest = "sha256:zz" },
			"payload digest ref": func(p *intentsv1.ProposalRevision) {
				p.Proposal.CanonicalDigest = &intentsv1.CanonicalDigestReference{Digest: "sha256:whatever"}
			},
		} {
			p := baseProposal()
			mutate(p)
			if got := mustEncode(t, p, proposalProfile()); !bytes.Equal(got, base) {
				t.Errorf("nonmaterial change %q changed the canonical bytes", name)
			}
		}
	})

	t.Run("absent and explicit default stay distinct where the schema says so", func(t *testing.T) {
		absent := baseProposal()
		explicit := baseProposal()
		explicit.SupersedesProposalRevisionId = proto.String("")
		if x, y := mustEncode(t, absent, proposalProfile()), mustEncode(t, explicit, proposalProfile()); bytes.Equal(x, y) {
			t.Fatal("an explicitly empty optional field canonicalized the same as an absent one")
		}
	})

	t.Run("decimal scale is normalized unless it is material", func(t *testing.T) {
		wide := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "1.50", AuthorityDomain: "USD"}
		narrow := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "1.5", AuthorityDomain: "USD"}
		if x, y := mustEncode(t, wide, subjectProfile()), mustEncode(t, narrow, subjectProfile()); !bytes.Equal(x, y) {
			t.Fatalf("1.50 and 1.5 canonicalized differently under an immaterial scale:\n%x\n%x", x, y)
		}
		scaled := subjectProfile()
		scaled.ID = "test.subject.scaled"
		scaled.Decimals = []canonical.Decimal{{Path: "subject_id", ScaleMaterial: true}}
		if x, y := mustEncode(t, wide, scaled), mustEncode(t, narrow, scaled); bytes.Equal(x, y) {
			t.Fatal("1.50 and 1.5 collapsed under a material scale")
		}
		// Signed zero has one representation.
		negZero := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "-0.00", AuthorityDomain: "USD"}
		posZero := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "0", AuthorityDomain: "USD"}
		if x, y := mustEncode(t, negZero, subjectProfile()), mustEncode(t, posZero, subjectProfile()); !bytes.Equal(x, y) {
			t.Fatal("-0.00 and 0 canonicalized differently")
		}
		bad := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "1.2.3", AuthorityDomain: "USD"}
		if _, err := canonical.Encode(bad, subjectProfile()); !errors.Is(err, canonical.ErrUnrepresentable) {
			t.Fatalf("want ErrUnrepresentable for a malformed decimal, got %v", err)
		}
	})

	t.Run("currency codes fold to uppercase and reject non-ISO input", func(t *testing.T) {
		lower := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "1", AuthorityDomain: "usd"}
		upper := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "1", AuthorityDomain: "USD"}
		if x, y := mustEncode(t, lower, subjectProfile()), mustEncode(t, upper, subjectProfile()); !bytes.Equal(x, y) {
			t.Fatal("currency case was material")
		}
		bad := &intentsv1.SubjectReference{SubjectKind: "worker", SubjectId: "1", AuthorityDomain: "dollars"}
		if _, err := canonical.Encode(bad, subjectProfile()); !errors.Is(err, canonical.ErrUnrepresentable) {
			t.Fatalf("want ErrUnrepresentable for a non-ISO currency, got %v", err)
		}
	})

	t.Run("instants outside the normalized range are unrepresentable", func(t *testing.T) {
		p := instantProfile()
		ok := baseProposal()
		if _, err := canonical.Encode(ok, p); err != nil {
			t.Fatalf("a normal instant failed: %v", err)
		}
		bad := baseProposal()
		bad.CreatedAt.Nanos = 1_000_000_000
		if _, err := canonical.Encode(bad, p); !errors.Is(err, canonical.ErrUnrepresentable) {
			t.Fatalf("want ErrUnrepresentable for out-of-range nanos, got %v", err)
		}
		bad2 := baseProposal()
		bad2.CreatedAt.Seconds = -62135596801
		if _, err := canonical.Encode(bad2, p); !errors.Is(err, canonical.ErrUnrepresentable) {
			t.Fatalf("want ErrUnrepresentable for an out-of-range instant, got %v", err)
		}
	})

	t.Run("the host timezone is immaterial", func(t *testing.T) {
		p := instantProfile()
		t.Setenv("TZ", "UTC")
		utc := mustEncode(t, baseProposal(), p)
		t.Setenv("TZ", "Pacific/Kiritimati")
		shifted := mustEncode(t, baseProposal(), p)
		if !bytes.Equal(utc, shifted) {
			t.Fatalf("the host timezone changed canonical bytes:\n%x\n%x", utc, shifted)
		}
	})

	t.Run("profile and schema identity are enforced", func(t *testing.T) {
		if _, err := canonical.Encode(&intentsv1.SubjectReference{}, proposalProfile()); !errors.Is(err, canonical.ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		bad := proposalProfile()
		bad.Material = append(bad.Material, "not_a_field")
		if _, err := canonical.Encode(baseProposal(), bad); !errors.Is(err, canonical.ErrInvalidProfile) {
			t.Fatalf("want ErrInvalidProfile for an unresolvable path, got %v", err)
		}
		notASet := proposalProfile()
		notASet.Sets = []string{"intent_id"}
		if _, err := canonical.Encode(baseProposal(), notASet); !errors.Is(err, canonical.ErrInvalidProfile) {
			t.Fatalf("want ErrInvalidProfile for a set on a singular field, got %v", err)
		}
		strayDecl := proposalProfile()
		strayDecl.Verbatim = []string{"control_snapshots.policy_bundle_digest"}
		if _, err := canonical.Encode(baseProposal(), strayDecl); !errors.Is(err, canonical.ErrInvalidProfile) {
			t.Fatalf("want ErrInvalidProfile for a declaration outside the material list, got %v", err)
		}
	})

	t.Run("profile identity is bound into the bytes", func(t *testing.T) {
		base := mustEncode(t, baseProposal(), proposalProfile())
		bumped := proposalProfile()
		bumped.Version = 2
		if got := mustEncode(t, baseProposal(), bumped); bytes.Equal(got, base) {
			t.Fatal("a profile version bump left the canonical bytes unchanged")
		}
		reschema := proposalProfile()
		reschema.SchemaVersion = 2
		if got := mustEncode(t, baseProposal(), reschema); bytes.Equal(got, base) {
			t.Fatal("a schema version bump left the canonical bytes unchanged")
		}
	})

	t.Run("Explain names the paths that contributed", func(t *testing.T) {
		x, b, err := canonical.Explain(baseProposal(), proposalProfile())
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		if x.CanonicalLength != len(b) {
			t.Fatalf("explain reported %d bytes, encoded %d", x.CanonicalLength, len(b))
		}
		got := x.ContributingPaths()
		want := []string{
			"intent_id", "proposal", "proposal.protobuf_wire_bytes", "proposal.schema",
			"proposal.schema.protobuf_full_name", "proposal.schema.schema_id",
			"proposal.schema.version", "proposal_revision_id", "revision",
		}
		if len(got) != len(want) {
			t.Fatalf("contributing paths\n got %v\nwant %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("contributing paths\n got %v\nwant %v", got, want)
			}
		}
		// control_snapshots is populated on the message and must not appear.
		for _, p := range got {
			if p == "control_snapshots" {
				t.Fatal("revalidated context contributed to the digest")
			}
		}
	})
}

// instantProfile materializes the created_at instant so the timestamp rules can
// be exercised. The production proposal profile deliberately excludes it.
func instantProfile() canonical.Profile {
	p := proposalProfile()
	p.ID = "test.proposal.instant"
	p.Material = append(p.Material, "created_at")
	return p
}

func TestTodo_MODEL_006_Property(t *testing.T) {
	t.Run("encoding is idempotent and allocation-order independent", func(t *testing.T) {
		msg := baseProposal()
		first := mustEncode(t, msg, proposalProfile())
		for i := range 64 {
			// A fresh message built the same way, plus a re-encode of the same
			// message, must both reproduce the bytes exactly.
			if got := mustEncode(t, baseProposal(), proposalProfile()); !bytes.Equal(got, first) {
				t.Fatalf("rebuild %d diverged", i)
			}
			if got := mustEncode(t, msg, proposalProfile()); !bytes.Equal(got, first) {
				t.Fatalf("re-encode %d diverged", i)
			}
		}
	})

	t.Run("a round trip through the wire preserves canonical bytes", func(t *testing.T) {
		msg := baseProposal()
		msg.InvalidatorRefs = []string{"inv-b", "inv-a"}
		want := mustEncode(t, msg, setProfile())
		for i := range 32 {
			raw, err := proto.MarshalOptions{Deterministic: false}.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			back := decodeProposal(t, raw)
			if got := mustEncode(t, back, setProfile()); !bytes.Equal(got, want) {
				t.Fatalf("wire round trip %d changed canonical bytes", i)
			}
		}
	})

	t.Run("material and nonmaterial partition the message", func(t *testing.T) {
		// Mutating only excluded fields never moves the bytes; mutating any
		// included leaf always does. Together those are the whole contract.
		base := mustEncode(t, baseProposal(), proposalProfile())
		for i := range 100 {
			p := baseProposal()
			p.ControlSnapshots = &intentsv1.ControlSnapshotReferences{
				PolicyBundleDigest: string(rune('a' + i%26)),
				DlpDecisionDigest:  string(rune('A' + i%26)),
			}
			p.InvalidatorRefs = []string{string(rune('0' + i%10))}
			if got := mustEncode(t, p, proposalProfile()); !bytes.Equal(got, base) {
				t.Fatalf("iteration %d: excluded fields moved the bytes", i)
			}
			q := baseProposal()
			q.Revision = uint64(i) + 100
			if got := mustEncode(t, q, proposalProfile()); bytes.Equal(got, base) {
				t.Fatalf("iteration %d: an included field did not move the bytes", i)
			}
		}
	})
}

func TestTodo_MODEL_006_Golden(t *testing.T) {
	golden(t, "proposal_v1.canonical.bin", mustEncode(t, baseProposal(), proposalProfile()))

	withSets := baseProposal()
	withSets.InvalidatorRefs = []string{"inv-c", "inv-a", "inv-b"}
	withSets.SupersedesProposalRevisionId = proto.String("pr-0000")
	golden(t, "proposal_sets_v1.canonical.bin", mustEncode(t, withSets, setProfile()))

	golden(t, "subject_decimal_currency_v1.canonical.bin", mustEncode(t,
		&intentsv1.SubjectReference{SubjectKind: composedE, SubjectId: "1.500", AuthorityDomain: "usd"},
		subjectProfile()))

	golden(t, "struct_map_v1.canonical.bin", mustEncode(t,
		mustStruct(t, []string{"zulu", "alpha", "mike"}), structProfile()))

	golden(t, "proposal_instant_v1.canonical.bin", mustEncode(t, baseProposal(), instantProfile()))
}

// FuzzTodo_MODEL_006 drives arbitrary wire bytes through the encoder. No input
// may panic, and every input that encodes must encode identically twice.
func FuzzTodo_MODEL_006(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		mustMarshal(f, baseProposal()),
		// Invalid UTF-8 in a string field, which proto.Marshal refuses to
		// produce but an untrusted peer can still put on the wire.
		fieldString(1, string([]byte{0xff, 0xfe})),
		fieldVarint(4242, 1),
		mustMarshal(f, &intentsv1.ProposalRevision{InvalidatorRefs: []string{"a", "a"}}),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	profiles := []canonical.Profile{proposalProfile(), setProfile(), listProfile(), instantProfile()}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192 {
			t.Skip("oversized input")
		}
		var msg intentsv1.ProposalRevision
		if err := proto.Unmarshal(data, &msg); err != nil {
			return
		}
		for _, p := range profiles {
			first, err := canonical.Encode(&msg, p)
			if err != nil {
				continue
			}
			second, err := canonical.Encode(&msg, p)
			if err != nil {
				t.Fatalf("profile %s encoded once and then failed: %v", p.ID, err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("profile %s is not deterministic", p.ID)
			}
			if _, _, err := canonical.Explain(&msg, p); err != nil {
				t.Fatalf("profile %s explained a message it encoded: %v", p.ID, err)
			}
		}
	})
}

func mustMarshal(f *testing.F, m proto.Message) []byte {
	f.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		f.Fatalf("marshal seed: %v", err)
	}
	return b
}
