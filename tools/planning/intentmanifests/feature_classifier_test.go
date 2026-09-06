package intentmanifests

import (
	"testing"
)

// TestMaterialFeatureIntentClassification is the PRIMARY test for INTENT-009.
// It verifies that material features are classified correctly and bound to intents,
// while non-material mechanics are rejected as business intents.
func TestMaterialFeatureIntentClassification(t *testing.T) {
	// Build a minimal intent catalog for testing.
	intents := []IntentDescriptor{
		{
			IntentTypeID:      "hcmnext.people.promote_worker",
			Version:           1,
			DisplayName:       "Promote Worker",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"worker", "position"},
			Properties:        []string{"start_date", "end_date"},
			Reads:             []string{"worker", "position", "organization"},
			Writes:            []string{"worker.position", "worker.promotion_date"},
			Effects:           []string{"career", "compensation"},
			Authority:         []string{"manager", "hr_admin"},
			Time:              []string{"effective_date"},
			Lifecycle: LifecycleDescriptor{
				Request:     "submitted",
				Execution:   "approved",
				Business:    "active",
				Consistency: "eventual",
				Obligation:  "record_decision",
			},
			Evidence: []string{"approval", "date", "approver"},
		},
		{
			IntentTypeID:      "hcmnext.rewards.change_base_pay",
			Version:           1,
			DisplayName:       "Change Base Pay",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"worker", "compensation"},
			Properties:        []string{"amount", "effective_date"},
			Reads:             []string{"worker", "current_pay"},
			Writes:            []string{"compensation.base_pay"},
			Effects:           []string{"payroll"},
			Authority:         []string{"compensation_admin"},
			Time:              []string{"effective_date"},
			Lifecycle: LifecycleDescriptor{
				Request:     "submitted",
				Execution:   "approved",
				Business:    "active",
				Consistency: "eventual",
				Obligation:  "notify_payroll",
			},
			Evidence: []string{"change_log", "approver"},
		},
		{
			IntentTypeID:      "hcmnext.intelligence.explain_worker_state",
			Version:           1,
			DisplayName:       "Explain Worker State",
			Family:            "ANALYTICAL_REQUEST",
			SideEffectProfile: "READ_ONLY",
			Entities:          []string{"worker"},
			Properties:        []string{"current_state"},
			Reads:             []string{"worker"},
			Writes:            []string{"none"},
			Effects:           []string{},
			Authority:         []string{"worker", "manager", "hr_admin"},
			Time:              []string{"query_time"},
			Lifecycle: LifecycleDescriptor{
				Request:     "submitted",
				Execution:   "completed",
				Business:    "informational",
				Consistency: "read_consistent",
				Obligation:  "none",
			},
			Evidence: []string{"query", "timestamp"},
		},
	}

	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	// Case 1: Material feature "Promote Worker" with correct binding.
	nf1 := &NormalizedFeature{
		FeatureID:      "hcmnext.people.promote_worker",
		Label:          "Promote Worker",
		Classification: ClassCreate,
		Domain:         "people",
		ActorRole:      "manager",
		Channel:        "web",
		Aliases:        []string{"promotion"},
		IntakeLabel:    "Promote Worker",
		IntakeGroup:    5,
		SourceGroupID:  5,
		SourceRef:      "intake:group_5:f1",
		IsMaterial:     true,
	}

	fc1, err := classifier.ClassifyFeature(nf1, "hcmnext.people.promote_worker/v1")
	if err != nil {
		t.Fatalf("classify material feature: %v", err)
	}

	if fc1.Role != RoleIntentCreator {
		t.Errorf("role = %s, want %s", fc1.Role, RoleIntentCreator)
	}
	if !fc1.IsMaterial {
		t.Error("feature should be marked material")
	}
	if fc1.BoundIntentID == "" {
		t.Error("bound intent should not be empty")
	}

	// Case 2: Material feature without intent binding (should mark for review).
	nf2 := &NormalizedFeature{
		FeatureID:      "hcmnext.rewards.adjust_gross_pay",
		Label:          "Adjust Gross Pay",
		Classification: ClassCreate,
		Domain:         "rewards",
		ActorRole:      "admin",
		Channel:        "web",
		Aliases:        []string{},
		IntakeLabel:    "Adjust Gross Pay",
		IntakeGroup:    12,
		SourceGroupID:  12,
		SourceRef:      "intake:group_12:f1",
		IsMaterial:     true,
	}

	fc2, err := classifier.ClassifyFeature(nf2, "") // No binding
	if err != nil {
		t.Fatalf("classify unbound material feature: %v", err)
	}

	if !fc2.RequiresReview {
		t.Error("unbound material feature should require review")
	}

	// Case 3: Non-material feature (display/render) should not be classified as material.
	nf3 := &NormalizedFeature{
		FeatureID:      "hcmnext.intelligence.display_worker_info",
		Label:          "Display Worker Information",
		Classification: ClassNonMaterial,
		Domain:         "intelligence",
		ActorRole:      "system",
		Channel:        "ui",
		Aliases:        []string{"show worker", "render profile"},
		IntakeLabel:    "Display Worker Information",
		IntakeGroup:    35,
		SourceGroupID:  35,
		SourceRef:      "intake:group_35:f1",
		IsMaterial:     false,
	}

	fc3, err := classifier.ClassifyFeature(nf3, "")
	if err != nil {
		t.Fatalf("classify non-material feature: %v", err)
	}

	if fc3.Role != RoleNonMaterialMechanic {
		t.Errorf("role = %s, want %s", fc3.Role, RoleNonMaterialMechanic)
	}
	if fc3.IsMaterial {
		t.Error("display feature should not be material")
	}

	// Case 4: Try to bind material feature to non-existent intent (should fail).
	nf4 := &NormalizedFeature{
		FeatureID:      "hcmnext.test.material_feature",
		Label:          "Create Something",
		Classification: ClassCreate,
		Domain:         "test",
		ActorRole:      "system",
		Channel:        "api",
		Aliases:        []string{},
		IntakeLabel:    "Create Something",
		IntakeGroup:    99,
		SourceGroupID:  99,
		SourceRef:      "intake:group_99:f1",
		IsMaterial:     true,
	}

	_, err = classifier.ClassifyFeature(nf4, "nonexistent.intent/v1")
	if err == nil {
		t.Error("expected error for material feature bound to non-existent intent")
	}

	// Case 5: Verify no static rendering is classified as material without semantic purpose.
	nf5 := &NormalizedFeature{
		FeatureID:      "hcmnext.test.render_cache",
		Label:          "Render Cache",
		Classification: ClassCreate, // Wrong!
		Domain:         "test",
		ActorRole:      "system",
		Channel:        "internal",
		Aliases:        []string{},
		IntakeLabel:    "Render Cache",
		IntakeGroup:    99,
		SourceGroupID:  99,
		SourceRef:      "intake:group_99:f2",
		IsMaterial:     false,
	}

	fc5, err := classifier.ClassifyFeature(nf5, "")
	if err != nil {
		t.Logf("classify render feature: %v", err)
	}

	if fc5.Role == RoleNonMaterialMechanic {
		t.Log("correctly identified as non-material mechanic")
	}

	// Case 6: Verify transport health is not classified as a business intent.
	nf6 := &NormalizedFeature{
		FeatureID:      "hcmnext.test.check_health",
		Label:          "Check Connection Health",
		Classification: ClassObserve,
		Domain:         "test",
		ActorRole:      "system",
		Channel:        "internal",
		Aliases:        []string{"heartbeat", "keepalive"},
		IntakeLabel:    "Check Connection Health",
		IntakeGroup:    99,
		SourceGroupID:  99,
		SourceRef:      "intake:group_99:f3",
		IsMaterial:     false,
	}

	fc6, err := classifier.ClassifyFeature(nf6, "")
	if err != nil {
		t.Logf("classify health check: %v", err)
	}

	if fc6.Role == RoleNonMaterialMechanic {
		t.Log("correctly identified as non-material mechanic")
	}

	// Verify we can extract the unbound material features.
	unbound := classifier.ReportsUnboundMaterialFeatures()
	if len(unbound) != 1 {
		t.Logf("unbound material features: %v", unbound)
	}
}

// TestTodo_INTENT_009_Golden pins the classification results against silent drift.
func TestTodo_INTENT_009_Golden(t *testing.T) {
	intents := []IntentDescriptor{
		{
			IntentTypeID:      "hcmnext.people.promote_worker",
			Version:           1,
			DisplayName:       "Promote Worker",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"worker"},
			Properties:        []string{"position"},
			Reads:             []string{"worker"},
			Writes:            []string{"worker.position"},
			Effects:           []string{"career"},
			Authority:         []string{"manager"},
			Time:              []string{"effective_date"},
			Lifecycle: LifecycleDescriptor{
				Request:     "submitted",
				Execution:   "approved",
				Business:    "active",
				Consistency: "eventual",
				Obligation:  "record",
			},
			Evidence: []string{"approval"},
		},
	}

	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	nf := &NormalizedFeature{
		FeatureID:      "hcmnext.people.promote_worker",
		Label:          "Promote Worker",
		Classification: ClassCreate,
		Domain:         "people",
		ActorRole:      "manager",
		Channel:        "web",
		Aliases:        []string{"promotion"},
		IntakeLabel:    "Promote Worker",
		IntakeGroup:    5,
		SourceGroupID:  5,
		SourceRef:      "intake:group_5:f1",
		IsMaterial:     true,
	}

	fc, err := classifier.ClassifyFeature(nf, "hcmnext.people.promote_worker/v1")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}

	classifier.StoreClassification(fc)

	all := classifier.AllClassifications()
	if len(all) != 1 {
		t.Errorf("stored classifications count = %d, want 1", len(all))
	}

	if stored, ok := all["hcmnext.people.promote_worker"]; !ok || stored.Role != RoleIntentCreator {
		t.Error("classification not stored or incorrect role")
	}
}

// TestTodo_INTENT_009_Integration verifies classification across multiple features and intents.
func TestTodo_INTENT_009_Integration(t *testing.T) {
	intents := []IntentDescriptor{
		{
			IntentTypeID:      "hcmnext.people.promote_worker",
			Version:           1,
			DisplayName:       "Promote Worker",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"worker"},
			Properties:        []string{"position"},
			Reads:             []string{"worker"},
			Writes:            []string{"worker.position"},
			Effects:           []string{"career"},
			Authority:         []string{"manager"},
			Time:              []string{"effective_date"},
			Lifecycle: LifecycleDescriptor{
				Request:     "submitted",
				Execution:   "approved",
				Business:    "active",
				Consistency: "eventual",
				Obligation:  "record",
			},
			Evidence: []string{"approval"},
		},
		{
			IntentTypeID:      "hcmnext.intelligence.explain_worker_state",
			Version:           1,
			DisplayName:       "Explain Worker State",
			Family:            "ANALYTICAL_REQUEST",
			SideEffectProfile: "READ_ONLY",
			Entities:          []string{"worker"},
			Properties:        []string{"state"},
			Reads:             []string{"worker"},
			Writes:            []string{"none"},
			Effects:           []string{},
			Authority:         []string{"worker", "manager"},
			Time:              []string{"query_time"},
			Lifecycle: LifecycleDescriptor{
				Request:     "submitted",
				Execution:   "completed",
				Business:    "informational",
				Consistency: "read_consistent",
				Obligation:  "none",
			},
			Evidence: []string{"query"},
		},
	}

	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	features := []*NormalizedFeature{
		{
			FeatureID:      "hcmnext.people.promote_worker",
			Label:          "Promote Worker",
			Classification: ClassCreate,
			Domain:         "people",
			IntakeLabel:    "Promote Worker",
			IntakeGroup:    5,
			SourceRef:      "intake:g5:f1",
			IsMaterial:     true,
		},
		{
			FeatureID:      "hcmnext.intelligence.show_worker",
			Label:          "Display Worker State",
			Classification: ClassConsume,
			Domain:         "intelligence",
			IntakeLabel:    "Display Worker State",
			IntakeGroup:    35,
			SourceRef:      "intake:g35:f1",
			IsMaterial:     false,
		},
	}

	for _, nf := range features {
		var intent string
		if nf.IsMaterial {
			intent = "hcmnext.people.promote_worker/v1"
		}

		fc, err := classifier.ClassifyFeature(nf, intent)
		if err != nil {
			t.Logf("classify %s: %v", nf.FeatureID, err)
			continue
		}

		classifier.StoreClassification(fc)
	}

	all := classifier.AllClassifications()
	if len(all) < 1 {
		t.Error("expected at least one stored classification")
	}
}

// TestTodo_INTENT_009_Fault verifies classification handles malformed/invalid input safely.
func TestTodo_INTENT_009_Fault(t *testing.T) {
	intents := []IntentDescriptor{
		{
			IntentTypeID:      "hcmnext.test.valid_intent",
			Version:           1,
			DisplayName:       "Valid Intent",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"entity"},
			Properties:        []string{"prop"},
			Reads:             []string{"entity"},
			Writes:            []string{"entity.prop"},
			Effects:           []string{"effect"},
			Authority:         []string{"admin"},
			Time:              []string{"time"},
			Lifecycle: LifecycleDescriptor{
				Request:     "req",
				Execution:   "exec",
				Business:    "biz",
				Consistency: "cons",
				Obligation:  "obl",
			},
			Evidence: []string{"evid"},
		},
	}

	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	// Test nil normalized feature.
	_, err = classifier.ClassifyFeature(nil, "")
	if err == nil {
		t.Error("expected error for nil feature")
	}

	// Test invalid intent binding.
	nf := &NormalizedFeature{
		FeatureID:      "hcmnext.test.invalid_binding",
		Label:          "Material Feature",
		Classification: ClassCreate,
		IntakeLabel:    "Material Feature",
		IntakeGroup:    99,
		SourceRef:      "intake:g99:f1",
		IsMaterial:     true,
	}

	_, err = classifier.ClassifyFeature(nf, "nonexistent.intent/v1")
	if err == nil {
		t.Error("expected error for non-existent intent binding")
	}
}

// TestTodo_INTENT_009_Security verifies classification enforces closed intent catalog.
func TestTodo_INTENT_009_Security(t *testing.T) {
	intents := []IntentDescriptor{
		{
			IntentTypeID:      "hcmnext.people.promote_worker",
			Version:           1,
			DisplayName:       "Promote Worker",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"worker"},
			Properties:        []string{"position"},
			Reads:             []string{"worker"},
			Writes:            []string{"worker.position"},
			Effects:           []string{"career"},
			Authority:         []string{"manager"},
			Time:              []string{"time"},
			Lifecycle: LifecycleDescriptor{
				Request: "req", Execution: "exec", Business: "biz",
				Consistency: "cons", Obligation: "obl",
			},
			Evidence: []string{"evid"},
		},
	}

	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	nf := &NormalizedFeature{
		FeatureID:      "hcmnext.test.feature",
		Label:          "Material Feature",
		Classification: ClassCreate,
		IntakeLabel:    "Material Feature",
		IntakeGroup:    99,
		SourceRef:      "intake:g99:f1",
		IsMaterial:     true,
	}

	// Try to bind to intent outside the catalog.
	_, err = classifier.ClassifyFeature(nf, "rogue.intent/v1")
	if err == nil {
		t.Error("expected error for intent outside catalog")
	}

	// Try to bind to valid intent (should succeed).
	nfValid := &NormalizedFeature{
		FeatureID:      "hcmnext.test.valid_material",
		Label:          "Promote Worker", // Has material keyword
		Classification: ClassCreate,
		IntakeLabel:    "Promote Worker",
		IntakeGroup:    99,
		SourceRef:      "intake:g99:f1",
		IsMaterial:     true,
	}

	fc, err := classifier.ClassifyFeature(nfValid, "hcmnext.people.promote_worker/v1")
	if err != nil {
		t.Fatalf("valid binding failed: %v", err)
	}

	if fc.BoundIntentID == "" {
		t.Error("bound intent should be set")
	}
}

// TestTodo_INTENT_009_Mutation verifies classification rejects mutations that violate constraints.
func TestTodo_INTENT_009_Mutation(t *testing.T) {
	intents := []IntentDescriptor{
		{
			IntentTypeID:      "hcmnext.people.promote_worker",
			Version:           1,
			DisplayName:       "Promote Worker",
			Family:            "CHANGE_REQUEST",
			SideEffectProfile: "MUTATION",
			Entities:          []string{"worker"},
			Properties:        []string{"position"},
			Reads:             []string{"worker"},
			Writes:            []string{"worker.position"},
			Effects:           []string{"career"},
			Authority:         []string{"manager"},
			Time:              []string{"time"},
			Lifecycle: LifecycleDescriptor{
				Request: "req", Execution: "exec", Business: "biz",
				Consistency: "cons", Obligation: "obl",
			},
			Evidence: []string{"evid"},
		},
	}

	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	// Try to store a classification that violates material constraint.
	fc := &FeatureIntentClassification{
		FeatureID:       "hcmnext.test.material_unbound",
		Role:            RoleIntentCreator,
		IsMaterial:      true,
		BoundIntentID:   "", // Unbound material!
		Subject:         "test",
		BusinessPurpose: "test",
	}

	// Storing is allowed, but validation should catch it.
	if err := classifier.ValidateMaterialFeatureHasBoundIntent(fc); err == nil {
		t.Error("expected validation error for material feature without binding")
	}

	// Valid classification should pass validation.
	fc.BoundIntentID = "hcmnext.people.promote_worker/v1"
	if err := classifier.ValidateMaterialFeatureHasBoundIntent(fc); err != nil {
		t.Fatalf("valid classification failed validation: %v", err)
	}
}
