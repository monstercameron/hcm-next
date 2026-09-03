package gen

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/timestamppb"

	capabilitiesv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/capabilities/v1"
	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
)

// roundTrip marshals m, unmarshals into a fresh instance of the same
// concrete type, and returns the fresh instance for comparison.
func roundTrip(t *testing.T, m proto.Message) proto.Message {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatalf("marshal %T: %v", m, err)
	}
	out := m.ProtoReflect().New().Interface()
	if err := proto.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal %T: %v", m, err)
	}
	return out
}

// TestTodo_PROTO_001 golden-round-trips every hcmnext/common/v1 wire
// primitive and asserts, by walking every registered hcmnext descriptor,
// that no message anywhere in the schema declares a float or double field.
// Money/decimal arithmetic must use the fixed-point Decimal encoding, never
// float64, per planning/data/models/wire-contract-primitives.md.
func TestTodo_PROTO_001(t *testing.T) {
	golden := []proto.Message{
		&commonv1.EntityRef{TenantId: "tenant-1", Kind: "worker", Id: "w-1"},
		&commonv1.ResourceKey{
			ResourceType:        "worker",
			TenantId:            "tenant-1",
			OrganizationScopeId: "org-1",
			KeyBytes:            []byte{0x01, 0x02, 0x03},
			OrderingClass:       "lexical",
			OrderingVersion:     1,
		},
		&commonv1.RevisionToken{
			Entity:            &commonv1.EntityRef{TenantId: "tenant-1", Kind: "worker", Id: "w-1"},
			Revision:          42,
			OpaqueCasBytes:    []byte{0xAA, 0xBB},
			SourceAuthorityId: "authority-1",
			Epoch:             7,
			IssuedAt:          timestamppb.New(fixedTestTime()),
		},
		// PresenceValue-shaped uses of the Presence enum itself (a scalar
		// enum value round trip, since Presence has no wrapping message).
		presenceHolder(commonv1.Presence_PRESENCE_REDACTED),
		&commonv1.Decimal{
			Sign:              commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE,
			UnscaledMagnitude: []byte{0x01, 0x86, 0xA0}, // 100000
			Scale:             2,                        // -1000.00
		},
		&commonv1.Money{
			Amount: &commonv1.Decimal{
				Sign:              commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE,
				UnscaledMagnitude: []byte{0x30, 0x39}, // 12345
				Scale:             2,
			},
			CurrencyCode: "USD",
		},
		&commonv1.LocalDate{Year: 2026, Month: 9, Day: 2, CalendarRef: "ISO_8601"},
		&commonv1.EffectiveTimeRange{
			Kind:           commonv1.EffectiveTimeRangeKind_EFFECTIVE_TIME_RANGE_KIND_LOCAL_DATE,
			StartInclusive: &commonv1.TimePoint{Point: &commonv1.TimePoint_LocalDate{LocalDate: &commonv1.LocalDate{Year: 2026, Month: 1, Day: 1, CalendarRef: "ISO_8601"}}},
			EndExclusive:   &commonv1.TimePoint{Point: &commonv1.TimePoint_LocalDate{LocalDate: &commonv1.LocalDate{Year: 2027, Month: 1, Day: 1, CalendarRef: "ISO_8601"}}},
			Timezone:       "America/New_York",
			TzdbVersion:    "2026a",
			CalendarRef:    "ISO_8601",
			Disambiguation: commonv1.DisambiguationRule_DISAMBIGUATION_RULE_EARLIER,
		},
		&commonv1.EffectiveTimeRange{
			Kind:           commonv1.EffectiveTimeRangeKind_EFFECTIVE_TIME_RANGE_KIND_INSTANT,
			StartInclusive: &commonv1.TimePoint{Point: &commonv1.TimePoint_Instant{Instant: timestamppb.New(fixedTestTime())}},
		},
		&commonv1.ScopeContext{TenantId: "tenant-1", OrganizationScopeId: "org-1", Purpose: "payroll"},
		&commonv1.PageRequest{PageSize: 25, Cursor: opaqueCursor("golden")},
		&commonv1.PageResponse{NextCursor: opaqueCursor("golden-next")},
		&commonv1.VersionRef{SchemaId: "worker.v1", Version: 3},
		&commonv1.EvidenceRef{EvidenceId: "ev-1", EvidenceKind: "simulation", Digest: "deadbeef"},
		&commonv1.ErrorDetail{
			Code: commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION,
			FieldViolations: []*commonv1.FieldViolation{
				{FieldPath: "effective_date", Description: "must not be in the past", RuleRef: "rule.effective-date.future"},
			},
			Retryable:     false,
			CorrelationId: "corr-1",
			EvidenceRef:   &commonv1.EvidenceRef{EvidenceId: "ev-1", EvidenceKind: "policy-decision"},
		},
	}

	for _, want := range golden {
		got := roundTrip(t, want)
		if !proto.Equal(want, got) {
			t.Errorf("round trip mismatch for %T:\nwant %v\ngot  %v", want, want, got)
		}
	}

	t.Run("NoFloatOrDoubleFieldsAnywhereInHcmnext", func(t *testing.T) {
		assertNoFloatFields(t)
	})

	// ErrorCode enumerates exactly the owned conditions from the canonical
	// error projection table in
	// planning/specs/http-grpc-endpoint-contract.md#canonical-error-projection.
	t.Run("ErrorCodeCoversCanonicalProjectionTable", func(t *testing.T) {
		wantCodes := []commonv1.ErrorCode{
			commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT,
			commonv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED,
			commonv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED,
			commonv1.ErrorCode_ERROR_CODE_NOT_FOUND,
			commonv1.ErrorCode_ERROR_CODE_ALREADY_EXISTS,
			commonv1.ErrorCode_ERROR_CODE_ABORTED,
			commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION,
			commonv1.ErrorCode_ERROR_CODE_RESOURCE_EXHAUSTED,
			commonv1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED,
			commonv1.ErrorCode_ERROR_CODE_UNAVAILABLE,
		}
		enumDesc := commonv1.ErrorCode(0).Descriptor()
		values := enumDesc.Values()
		if values.Len() != len(wantCodes)+1 { // +1 for UNSPECIFIED = 0
			t.Fatalf("expected %d ErrorCode values (including UNSPECIFIED), got %d", len(wantCodes)+1, values.Len())
		}
		for _, wc := range wantCodes {
			if values.ByNumber(protoreflect.EnumNumber(wc)) == nil {
				t.Errorf("ErrorCode missing expected value %v", wc)
			}
		}
	})
}

// presenceHolder wraps a Presence value in a message so it can appear in the
// generic golden-vector round-trip loop (a bare enum is not a proto.Message).
func presenceHolder(p commonv1.Presence) proto.Message {
	return &commonv1.ErrorDetail{
		FieldViolations: []*commonv1.FieldViolation{{Description: p.String()}},
	}
}

func fixedTestTime() time.Time {
	return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
}

// assertNoFloatFields walks every file descriptor registered under the
// hcmnext.* proto package namespace and fails if any message field anywhere
// declares FloatKind or DoubleKind. Importing the generated packages below
// is what causes their descriptors to register in the global registry.
func assertNoFloatFields(t *testing.T) {
	t.Helper()

	// Force registration of every hcmnext package this generator owns.
	_ = (&commonv1.EntityRef{}).ProtoReflect().Descriptor()
	_ = (&capabilitiesv1.CapabilityDefinition{}).ProtoReflect().Descriptor()
	_ = (&intentsv1.IntentInstance{}).ProtoReflect().Descriptor()
	_ = (&registryv1.ListIntentDefinitionsRequest{}).ProtoReflect().Descriptor()

	checked := 0
	var walkMessage func(md protoreflect.MessageDescriptor)
	walkMessage = func(md protoreflect.MessageDescriptor) {
		checked++
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.Kind() == protoreflect.FloatKind || f.Kind() == protoreflect.DoubleKind {
				t.Errorf("field %s in message %s uses a float/double kind; money and decimal values must use Decimal", f.FullName(), md.FullName())
			}
		}
		nested := md.Messages()
		for i := 0; i < nested.Len(); i++ {
			walkMessage(nested.Get(i))
		}
	}

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !isHcmnextPackage(fd.Package()) {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			walkMessage(msgs.Get(i))
		}
		return true
	})

	if checked == 0 {
		t.Fatal("no hcmnext messages were found in the global registry; descriptor registration did not run")
	}
}

func isHcmnextPackage(pkg protoreflect.FullName) bool {
	const prefix = "hcmnext."
	s := string(pkg)
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
