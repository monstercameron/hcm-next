package canonical_test

import (
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
)

// composedE and decomposedE are the same grapheme in the two Unicode forms
// NFC is supposed to collapse: U+00E9, and U+0065 followed by U+0301.
const (
	composedE   = "André"
	decomposedE = "André"
)

func mustTime(s string) time.Time {
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return v
}

func fieldString(num protowire.Number, s string) []byte {
	return protowire.AppendString(protowire.AppendTag(nil, num, protowire.BytesType), s)
}

func fieldVarint(num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), v)
}

func fieldMessage(num protowire.Number, body []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), body)
}

// wireProposal serializes the same ProposalRevision with its fields emitted in
// ascending or descending tag order. Protobuf permits both; the canonical
// encoder must not care.
func wireProposal(t *testing.T, reverse bool) []byte {
	t.Helper()
	src := baseProposal()
	chunks := [][]byte{
		fieldString(1, src.GetProposalRevisionId()),
		fieldString(2, src.GetIntentId()),
		fieldVarint(3, src.GetRevision()),
		fieldMessage(5, marshalSub(t, src.GetProposal())),
		fieldMessage(6, marshalSub(t, src.GetControlSnapshots())),
		fieldMessage(7, marshalSub(t, src.GetCreatedBy())),
		fieldMessage(8, marshalSub(t, src.GetCreatedAt())),
	}
	if reverse {
		slices.Reverse(chunks)
	}
	var out []byte
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}

func marshalSub(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal submessage: %v", err)
	}
	return b
}

func decodeProposal(t *testing.T, raw []byte) *intentsv1.ProposalRevision {
	t.Helper()
	var msg intentsv1.ProposalRevision
	if err := proto.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &msg
}

// mustStruct builds a google.protobuf.Struct whose contents depend only on the
// key set, never on the order the keys were inserted.
func mustStruct(t *testing.T, keys []string) *structpb.Struct {
	t.Helper()
	s := &structpb.Struct{Fields: map[string]*structpb.Value{}}
	for _, k := range keys {
		s.Fields[k] = structpb.NewStringValue("value-" + k)
	}
	s.Fields["nested"] = structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
		"number":  structpb.NewNumberValue(1.5),
		"flag":    structpb.NewBoolValue(true),
		"nothing": structpb.NewNullValue(),
	}})
	s.Fields["ordered"] = structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{
		structpb.NewStringValue("first"),
		structpb.NewStringValue("second"),
	}})
	return s
}
