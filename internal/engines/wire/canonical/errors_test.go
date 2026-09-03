package canonical

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrInvalidUTF8 == nil {
		t.Fatalf("ErrInvalidUTF8 is nil")
	}
	if ErrDuplicateSetMember == nil {
		t.Fatalf("ErrDuplicateSetMember is nil")
	}
	if ErrUnknownField == nil {
		t.Fatalf("ErrUnknownField is nil")
	}
	if ErrUnrepresentable == nil {
		t.Fatalf("ErrUnrepresentable is nil")
	}
	if ErrInvalidProfile == nil {
		t.Fatalf("ErrInvalidProfile is nil")
	}
	if ErrSchemaMismatch == nil {
		t.Fatalf("ErrSchemaMismatch is nil")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrInvalidUTF8, ErrInvalidUTF8) {
		t.Fatalf("errors.Is failed for ErrInvalidUTF8")
	}
	if ErrInvalidUTF8.Error() == "" {
		t.Fatalf("ErrInvalidUTF8 Error empty")
	}
	if !errors.Is(ErrDuplicateSetMember, ErrDuplicateSetMember) {
		t.Fatalf("errors.Is failed for ErrDuplicateSetMember")
	}
	if ErrDuplicateSetMember.Error() == "" {
		t.Fatalf("ErrDuplicateSetMember Error empty")
	}
	if !errors.Is(ErrUnknownField, ErrUnknownField) {
		t.Fatalf("errors.Is failed for ErrUnknownField")
	}
	if ErrUnknownField.Error() == "" {
		t.Fatalf("ErrUnknownField Error empty")
	}
	if !errors.Is(ErrUnrepresentable, ErrUnrepresentable) {
		t.Fatalf("errors.Is failed for ErrUnrepresentable")
	}
	if ErrUnrepresentable.Error() == "" {
		t.Fatalf("ErrUnrepresentable Error empty")
	}
	if !errors.Is(ErrInvalidProfile, ErrInvalidProfile) {
		t.Fatalf("errors.Is failed for ErrInvalidProfile")
	}
	if ErrInvalidProfile.Error() == "" {
		t.Fatalf("ErrInvalidProfile Error empty")
	}
	if !errors.Is(ErrSchemaMismatch, ErrSchemaMismatch) {
		t.Fatalf("errors.Is failed for ErrSchemaMismatch")
	}
	if ErrSchemaMismatch.Error() == "" {
		t.Fatalf("ErrSchemaMismatch Error empty")
	}
}
