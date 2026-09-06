package canonical

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"

	integrationv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/integration/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestEncode_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestEncode_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestEncode_PlanAndDurationBoundaries(t *testing.T) {
	profile := Profile{ID: "test.bounds", Version: 1, SchemaID: "integration", SchemaVersion: 3, MessageName: "hcmnext.integration.v1.Bounds", Material: []string{"min_request_interval"}}
	msg := &integrationv1.Bounds{MinRequestInterval: durationpb.New(2*time.Second + 3*time.Nanosecond)}
	first, err := Encode(msg, profile)
	if err != nil || len(first) == 0 {
		t.Fatalf("Encode = %x, %v", first, err)
	}
	plan, err := Compile(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := plan.Encode(msg)
	if err != nil || !bytes.Equal(first, second) || plan.Profile().ID != profile.ID {
		t.Fatalf("Plan encode/profile = %x, %v, %+v", second, err, plan.Profile())
	}
	if _, err := Encode(nil, profile); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("nil message error = %v", err)
	}
	for _, mutate := range []func(*durationpb.Duration){
		func(d *durationpb.Duration) { d.Nanos = 1_000_000_000 },
		func(d *durationpb.Duration) { d.Seconds = 1; d.Nanos = -1 },
		func(d *durationpb.Duration) { d.Seconds = -1; d.Nanos = 1 },
		func(d *durationpb.Duration) { d.Seconds = 315576000001 },
		func(d *durationpb.Duration) { d.Seconds = -315576000001 },
	} {
		bad := durationpb.New(0)
		mutate(bad)
		if _, err := Encode(&integrationv1.Bounds{MinRequestInterval: bad}, profile); !errors.Is(err, ErrUnrepresentable) {
			t.Fatalf("duration %+v error = %v", bad, err)
		}
	}
}

func TestEncode_RejectsNonFiniteFloatsAndCanonicalMapKeyCollisions(t *testing.T) {
	profile := Profile{ID: "test.struct", Version: 1, SchemaID: "google.protobuf", SchemaVersion: 1, MessageName: "google.protobuf.Struct", Material: []string{"fields"}}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		msg := &structpb.Struct{Fields: map[string]*structpb.Value{"number": structpb.NewNumberValue(value)}}
		if _, err := Encode(msg, profile); !errors.Is(err, ErrUnrepresentable) {
			t.Fatalf("float %v error = %v", value, err)
		}
	}
	msg := &structpb.Struct{Fields: map[string]*structpb.Value{"André": structpb.NewStringValue("one"), "André": structpb.NewStringValue("two")}}
	if _, err := Encode(msg, profile); !errors.Is(err, ErrDuplicateSetMember) {
		t.Fatalf("normalized map key collision error = %v", err)
	}
	negZero := &structpb.Struct{Fields: map[string]*structpb.Value{"number": structpb.NewNumberValue(math.Copysign(0, -1))}}
	posZero := &structpb.Struct{Fields: map[string]*structpb.Value{"number": structpb.NewNumberValue(0)}}
	left, err := Encode(negZero, profile)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Encode(posZero, profile)
	if err != nil || !bytes.Equal(left, right) {
		t.Fatalf("signed zero bytes differ: %v", err)
	}
}
