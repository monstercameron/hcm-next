package privacymeta

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidationHelpers_Boundaries(t *testing.T) {
	if got := object(nil); string(got) != "{}" {
		t.Fatalf("object(nil) = %s", got)
	}
	if got := object([]byte(`null`)); string(got) != "null" {
		t.Fatalf("object(nonempty) = %s", got)
	}
	if !oneOf("B", "A", "B") || oneOf("C", "A", "B") {
		t.Fatal("oneOf boundary result is incorrect")
	}
	if !errors.Is(detail(ErrMissingScope, "field %s", "x"), ErrMissingScope) {
		t.Fatal("ErrDetail does not unwrap its sentinel")
	}
	if err := ensureTenant(context.Background(), nil, uuid.Nil); !errors.Is(err, ErrNilTenant) {
		t.Fatalf("ensureTenant(nil tenant) = %v, want ErrNilTenant", err)
	}
}

func TestProcessingPurposeDeclarationValidate_Errors(t *testing.T) {
	good := ProcessingPurposeDeclaration{TenantID: uuid.New(), DeclarationID: uuid.New(), DeclarationVersion: 1,
		PurposeKey: "purpose", LawfulBasis: "CONSENT", RetentionScheduleKey: "retention", ContentDigest: "digest", Status: "DRAFT",
		EffectiveFrom: time.Now()}
	cases := []struct {
		name   string
		mutate func(*ProcessingPurposeDeclaration)
		want   error
	}{
		{"nil tenant", func(d *ProcessingPurposeDeclaration) { d.TenantID = uuid.Nil }, ErrNilTenant},
		{"nil declaration", func(d *ProcessingPurposeDeclaration) { d.DeclarationID = uuid.Nil }, ErrNilTenant},
		{"missing version", func(d *ProcessingPurposeDeclaration) { d.DeclarationVersion = 0 }, ErrMissingVersion},
		{"missing digest", func(d *ProcessingPurposeDeclaration) { d.ContentDigest = "" }, ErrMissingDigest},
		{"missing scope", func(d *ProcessingPurposeDeclaration) { d.PurposeKey = "" }, ErrMissingScope},
		{"invalid basis", func(d *ProcessingPurposeDeclaration) { d.LawfulBasis = "NO" }, ErrInvalidEnum},
		{"invalid status", func(d *ProcessingPurposeDeclaration) { d.Status = "NO" }, ErrInvalidEnum},
		{"reversed interval", func(d *ProcessingPurposeDeclaration) { end := d.EffectiveFrom; d.EffectiveTo = &end }, ErrInvalidInterval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDataCopyInventoryValidate_Errors(t *testing.T) {
	good := DataCopyInventory{TenantID: uuid.New(), InventoryID: uuid.New(), CanonicalAssetKey: "asset", SourceWatermark: "wm", ContentDigest: "digest", Completeness: "COMPLETE"}
	cases := []struct {
		name   string
		mutate func(*DataCopyInventory)
		want   error
	}{
		{"nil id", func(i *DataCopyInventory) { i.InventoryID = uuid.Nil }, ErrNilTenant},
		{"missing asset", func(i *DataCopyInventory) { i.CanonicalAssetKey = "" }, ErrMissingScope},
		{"missing watermark", func(i *DataCopyInventory) { i.SourceWatermark = "" }, ErrMissingScope},
		{"missing digest", func(i *DataCopyInventory) { i.ContentDigest = "" }, ErrMissingDigest},
		{"bad completeness", func(i *DataCopyInventory) { i.Completeness = "NO" }, ErrInvalidEnum},
		{"negative unknowns", func(i *DataCopyInventory) { i.UnknownCount = -1 }, ErrInvalidEnum},
		{"complete with unknowns", func(i *DataCopyInventory) { i.UnknownCount = 1 }, ErrIncompleteCensus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDataCopyValidate_Errors(t *testing.T) {
	good := DataCopy{TenantID: uuid.New(), CopyID: uuid.New(), InventoryID: uuid.New(), CanonicalAssetKey: "asset", StoreRef: "store", CopyType: "CACHE", RetentionScheduleKey: "retention", HoldState: "NONE", DeletionCapability: "DELETE"}
	cases := []struct {
		name   string
		mutate func(*DataCopy)
		want   error
	}{
		{"nil id", func(c *DataCopy) { c.CopyID = uuid.Nil }, ErrNilTenant},
		{"missing scope", func(c *DataCopy) { c.StoreRef = "" }, ErrMissingScope},
		{"bad type", func(c *DataCopy) { c.CopyType = "NO" }, ErrInvalidEnum},
		{"bad hold", func(c *DataCopy) { c.HoldState = "NO" }, ErrInvalidEnum},
		{"bad deletion", func(c *DataCopy) { c.DeletionCapability = "NO" }, ErrInvalidEnum},
		{"provider without processor", func(c *DataCopy) { c.CopyType = "PROVIDER" }, ErrMissingScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}
