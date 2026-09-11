package trust

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type verifierTestMarkerKey struct{}

func TestVerifierFunc_VerifyForwardsContextAndCredential(t *testing.T) {
	wantCtx := context.WithValue(context.Background(), verifierTestMarkerKey{}, "value")
	want := Credential{Scheme: "Bearer", Token: "proof", Audience: "api"}
	called := false
	f := VerifierFunc(func(ctx context.Context, got Credential) (*Principal, error) {
		called = true
		if ctx != wantCtx || !reflect.DeepEqual(got, want) {
			t.Fatalf("forwarded (%v, %+v), want (%v, %+v)", ctx, got, wantCtx, want)
		}
		return nil, ErrInvalidCredential
	})
	if _, err := f.Verify(wantCtx, want); !errors.Is(err, ErrInvalidCredential) || !called {
		t.Fatalf("Verify err=%v called=%v", err, called)
	}
}

func TestReservedMetadataKeys_ReturnsSortedCopy(t *testing.T) {
	keys := ReservedMetadataKeys()
	if len(keys) == 0 || !reflect.DeepEqual(keys, append([]string(nil), keys...)) {
		t.Fatalf("unexpected reserved key list: %v", keys)
	}
	original := keys[0]
	keys[0] = "mutated"
	keysAgain := ReservedMetadataKeys()
	if keysAgain[0] != original {
		t.Fatal("ReservedMetadataKeys exposed mutable package state")
	}
	for i := 1; i < len(keysAgain); i++ {
		if keysAgain[i-1] > keysAgain[i] {
			t.Fatalf("reserved keys are not sorted: %v", keysAgain)
		}
	}
}

func TestReservedMetadataKeyAndRejectCallerSelectedAuthority_NormalizeAndDeny(t *testing.T) {
	if !IsReservedMetadataKey("  X-HCM-TENANT  ") || IsReservedMetadataKey("x-client-request-id") {
		t.Fatal("reserved metadata matching boundary is wrong")
	}
	if got := RejectCallerSelectedAuthority([]string{"x-hcm-tenant", " X-ROLES ", "x-hcm-tenant", "safe"}); !reflect.DeepEqual(got, []string{"x-hcm-tenant", "x-roles"}) {
		t.Fatalf("rejected keys = %v", got)
	}
	if got := RejectCallerSelectedAuthority([]string{"safe", " ", "x-client"}); got != nil {
		t.Fatalf("non-reserved keys returned %v", got)
	}
}
