package evolution

import (
	"errors"
	"testing"
)

func TestDefinitionDigest_Deterministic(t *testing.T) {
	a, err := DefinitionDigest(promotionV1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := DefinitionDigest(promotionV1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != b {
		t.Fatalf("digest is not reproducible: %s != %s", a, b)
	}
	if a == "" {
		t.Fatal("digest is empty")
	}
}

func TestDefinitionDigest_DiffersOnContentChange(t *testing.T) {
	a, err := DefinitionDigest(promotionV1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	changed := promotionV1()
	changed.Description = changed.Description + " (edited)"
	b, err := DefinitionDigest(changed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == b {
		t.Fatal("digest did not change with content")
	}
}

func TestDefinitionDigest_InvalidDefinition_Errors(t *testing.T) {
	broken := promotionV1()
	broken.DisplayName = ""
	if _, err := DefinitionDigest(broken); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("want ErrInvalidDefinition, got %v", err)
	}
}

func TestRefuseInPlaceEdit_SameContent_IsAllowed(t *testing.T) {
	if err := RefuseInPlaceEdit(promotionV1(), promotionV1()); err != nil {
		t.Fatalf("republishing identical content must not be refused: %v", err)
	}
}

func TestRefuseInPlaceEdit_SameRefDifferentContent_IsRefused(t *testing.T) {
	edited := promotionV1()
	edited.ApprovalRequired = false // same Ref, different content: an in-place edit
	err := RefuseInPlaceEdit(promotionV1(), edited)
	if !errors.Is(err, ErrInPlaceEdit) {
		t.Fatalf("want ErrInPlaceEdit, got %v", err)
	}
}

func TestRefuseInPlaceEdit_DifferentRef_IsNotJudgedHere(t *testing.T) {
	// A different (advancing) version is a new-version question for
	// CompatibilityCheck, not an in-place edit; RefuseInPlaceEdit must stay
	// silent about it even when the content is wildly different.
	if err := RefuseInPlaceEdit(promotionV1(), promotionV2ApprovalRemoved()); err != nil {
		t.Fatalf("a different ref must never be judged as an in-place edit: %v", err)
	}
}
