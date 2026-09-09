package telemetry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

func testAllowlist(t *testing.T) *telemetry.Allowlist {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	return allow
}

func validResource() telemetry.Resource {
	return telemetry.NewResourceFromBuild(
		buildinfo.Info{Revision: "abc123"},
		"hcm-api", "instance-1", "production", "cell-p1a", "us-east-1",
		telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
	)
}

// TestTodo_OBS_001 proves the RED and GREEN clauses of planning/todos.md
// OBS-001: a resource missing a required field, an outcome/policy version
// that was never set, and an attribute that was never allow-listed all
// fail closed with a field-level RejectionError before anything reaches a
// handler or exporter; a well-formed resource, context and attribute set
// builds one typed, signal-neutral Envelope.
func TestTodo_OBS_001(t *testing.T) {
	allow := testAllowlist(t)

	t.Run("RED_missing_schema_version_never_validates", func(t *testing.T) {
		var r telemetry.Resource // zero value: SchemaVersion 0
		err := r.Validate()
		if err == nil {
			t.Fatal("zero-value resource validated; want rejection")
		}
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) {
			t.Fatalf("error is not a *RejectionError: %v", err)
		}
		if rej.Code != "OBS_001_REJECTED" || rej.Field != "resource.schema_version" {
			t.Fatalf("rejection = %+v, want code OBS_001_REJECTED field resource.schema_version", rej)
		}
	})

	t.Run("RED_missing_required_resource_field_never_validates", func(t *testing.T) {
		r := validResource()
		r.CellID = ""
		err := r.Validate()
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.Field != "resource.cell_id" || rej.State != "missing" {
			t.Fatalf("Validate() = %v, want a missing resource.cell_id rejection", err)
		}
	})

	t.Run("RED_missing_process_role_never_validates", func(t *testing.T) {
		r := validResource()
		r.ProcessRole = telemetry.ProcessRoleUnspecified
		if err := r.Validate(); err == nil {
			t.Fatal("resource with unspecified process role validated")
		}
	})

	t.Run("RED_envelope_without_owned_correlation_never_builds", func(t *testing.T) {
		ctx := context.Background() // no WithCorrelationID
		_, err := telemetry.BuildEnvelope(ctx, validResource(), telemetry.OutcomeSuccess, 1, nil, allow)
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.Field != "context.correlation_id" {
			t.Fatalf("BuildEnvelope() = %v, want a missing correlation id rejection", err)
		}
	})

	t.Run("RED_envelope_without_outcome_never_builds", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		_, err := telemetry.BuildEnvelope(ctx, validResource(), telemetry.OutcomeUnspecified, 1, nil, allow)
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.Field != "envelope.outcome" {
			t.Fatalf("BuildEnvelope() = %v, want a missing outcome rejection", err)
		}
	})

	t.Run("RED_envelope_without_policy_version_never_builds", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		_, err := telemetry.BuildEnvelope(ctx, validResource(), telemetry.OutcomeSuccess, 0, nil, allow)
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.Field != "envelope.policy_version" {
			t.Fatalf("BuildEnvelope() = %v, want a missing policy version rejection", err)
		}
	})

	t.Run("RED_prohibited_payload_data_never_reaches_envelope", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		for _, key := range []string{"salary", "medical_condition", "bank_account", "case_notes", "prompt", "worker_id", "request_body"} {
			attrs := map[string]string{key: "sensitive-value"}
			_, err := telemetry.BuildEnvelope(ctx, validResource(), telemetry.OutcomeSuccess, 1, attrs, allow)
			var rej *telemetry.RejectionError
			if !errors.As(err, &rej) {
				t.Fatalf("key %q: BuildEnvelope() = %v, want a rejection", key, err)
			}
			if rej.Field != "attributes."+key {
				t.Fatalf("key %q: rejection field = %q, want attributes.%s", key, rej.Field, key)
			}
		}
	})

	t.Run("GREEN_well_formed_envelope_builds_with_exact_fields", func(t *testing.T) {
		ctx := context.Background()
		ctx = telemetry.WithCorrelationID(ctx, "corr-42")
		ctx = telemetry.WithRequestID(ctx, "req-7")
		ctx = telemetry.WithEvidenceRef(ctx, "evidence-1")
		ctx = telemetry.WithPrincipalRef(ctx, "principal-ref-1")

		env, err := telemetry.BuildEnvelope(ctx, validResource(), telemetry.OutcomeSuccess, 1,
			map[string]string{"cell_id": "cell-p1a", "outcome": "SUCCESS"}, allow)
		if err != nil {
			t.Fatalf("BuildEnvelope() = %v, want success", err)
		}
		if env.SchemaVersion != telemetry.EnvelopeSchemaVersion {
			t.Errorf("SchemaVersion = %d, want %d", env.SchemaVersion, telemetry.EnvelopeSchemaVersion)
		}
		if env.CorrelationID != "corr-42" || env.RequestID != "req-7" || env.EvidenceRef != "evidence-1" || env.PrincipalRef != "principal-ref-1" {
			t.Errorf("context fields not carried through: %+v", env)
		}
		if env.Resource.CellID != "cell-p1a" {
			t.Errorf("Resource not carried through: %+v", env.Resource)
		}
		if env.Outcome != telemetry.OutcomeSuccess || env.PolicyVersion != 1 {
			t.Errorf("outcome/policy version = %v/%d, want SUCCESS/1", env.Outcome, env.PolicyVersion)
		}
		if len(env.Attributes) != 2 {
			t.Errorf("Attributes = %v, want exactly the 2 allow-listed keys", env.Attributes)
		}
	})
}

// TestTodo_OBS_001_Integration exercises Resource, context propagation,
// the Allowlist and BuildEnvelope together end to end, the way a real
// caller composes them.
func TestTodo_OBS_001_Integration(t *testing.T) {
	allow := testAllowlist(t)
	build := buildinfo.Info{Revision: "deadbeef", Modified: true}

	res := telemetry.NewResourceFromBuild(build, "hcm-worker", "instance-9", "staging", "cell-p1a", "", telemetry.ProcessRoleWorker, telemetry.TenantClassEnterprise)
	if err := res.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want success (region is optional)", err)
	}
	if !strings.HasSuffix(res.BuildDigest, "+modified") {
		t.Fatalf("BuildDigest = %q, want a +modified suffix for a dirty build", res.BuildDigest)
	}

	ctx := telemetry.WithCorrelationID(context.Background(), "corr-integration")
	ctx = telemetry.WithRequestID(ctx, "req-integration")

	env, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeDegraded, 1, map[string]string{
		"capability_id": "cap.workflow.start",
		"workflow_node": "node.approve",
		"outcome":       "DEGRADED",
		"tenant_class":  string(telemetry.TenantClassEnterprise),
	}, allow)
	if err != nil {
		t.Fatalf("BuildEnvelope() = %v, want success", err)
	}
	if env.Resource.ProcessRole != telemetry.ProcessRoleWorker {
		t.Errorf("Resource.ProcessRole = %v, want worker", env.Resource.ProcessRole)
	}
	if env.Attributes["capability_id"] != "cap.workflow.start" {
		t.Errorf("Attributes[capability_id] = %q", env.Attributes["capability_id"])
	}
}

// TestTodo_OBS_001_Security proves that untrusted callers cannot smuggle
// an oversized token through context propagation, cannot overwrite an
// envelope's own owned fields through the attribute map, and cannot use an
// unregistered key to carry restricted content merely because its name
// looks safe.
func TestTodo_OBS_001_Security(t *testing.T) {
	allow := testAllowlist(t)
	res := validResource()

	t.Run("oversized_correlation_id_rejected", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), strings.Repeat("a", telemetry.MaxCorrelationIDLen+1))
		_, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, nil, allow)
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.Field != "context.correlation_id" || rej.State != "too_long" {
			t.Fatalf("BuildEnvelope() = %v, want an oversized correlation id rejection", err)
		}
	})

	t.Run("oversized_principal_ref_rejected", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		ctx = telemetry.WithPrincipalRef(ctx, strings.Repeat("p", telemetry.MaxPrincipalRefLen+1))
		_, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, nil, allow)
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.Field != "context.principal_ref" {
			t.Fatalf("BuildEnvelope() = %v, want an oversized principal ref rejection", err)
		}
	})

	t.Run("attribute_cannot_overwrite_envelope_owned_correlation_id", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "real-correlation-id")
		env, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1,
			map[string]string{"correlation_id": "forged-by-attacker"}, allow)
		if err != nil {
			t.Fatalf("BuildEnvelope() = %v, want success (correlation_id is a registered log/span-only attribute)", err)
		}
		if env.CorrelationID != "real-correlation-id" {
			t.Fatalf("Envelope.CorrelationID = %q, an attribute forged the owned field", env.CorrelationID)
		}
		if env.Attributes["correlation_id"] != "forged-by-attacker" {
			t.Fatalf("the attribute itself should still be recorded as its own bounded-restricted value")
		}
	})

	t.Run("oversized_attribute_value_rejected", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		huge := strings.Repeat("x", 4097)
		_, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, map[string]string{"cell_id": huge}, allow)
		var rej *telemetry.RejectionError
		if !errors.As(err, &rej) || rej.State != "value_too_long" {
			t.Fatalf("BuildEnvelope() = %v, want an oversized attribute value rejection", err)
		}
	})

	t.Run("nil_allowlist_fails_closed_not_open", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		_, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, map[string]string{"cell_id": "x"}, nil)
		if err == nil {
			t.Fatal("BuildEnvelope() with a nil allow-list built successfully; want fail-closed rejection")
		}
	})
}

// TestTodo_OBS_001_Mutation flips each boundary condition by exactly one
// unit and asserts the outcome flips too, proving the checks are not
// accidentally off-by-one or vacuous.
func TestTodo_OBS_001_Mutation(t *testing.T) {
	allow := testAllowlist(t)
	res := validResource()

	t.Run("correlation_id_at_max_length_passes_one_over_fails", func(t *testing.T) {
		ok := telemetry.WithCorrelationID(context.Background(), strings.Repeat("a", telemetry.MaxCorrelationIDLen))
		if _, err := telemetry.BuildEnvelope(ok, res, telemetry.OutcomeSuccess, 1, nil, allow); err != nil {
			t.Fatalf("at exactly MaxCorrelationIDLen: %v, want success", err)
		}
		over := telemetry.WithCorrelationID(context.Background(), strings.Repeat("a", telemetry.MaxCorrelationIDLen+1))
		if _, err := telemetry.BuildEnvelope(over, res, telemetry.OutcomeSuccess, 1, nil, allow); err == nil {
			t.Fatal("one byte over MaxCorrelationIDLen: got success, want rejection")
		}
	})

	t.Run("schema_version_exact_passes_any_other_fails", func(t *testing.T) {
		good := res
		good.SchemaVersion = telemetry.ResourceSchemaVersion
		if err := good.Validate(); err != nil {
			t.Fatalf("exact schema version: %v, want success", err)
		}
		bad := res
		bad.SchemaVersion = telemetry.ResourceSchemaVersion + 1
		if err := bad.Validate(); err == nil {
			t.Fatal("schema version + 1: got success, want rejection")
		}
	})

	t.Run("policy_version_zero_fails_one_passes", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		if _, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 0, nil, allow); err == nil {
			t.Fatal("policy version 0: got success, want rejection")
		}
		if _, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, nil, allow); err != nil {
			t.Fatalf("policy version 1: %v, want success", err)
		}
	})

	t.Run("attribute_value_at_bound_passes_one_over_fails", func(t *testing.T) {
		ctx := telemetry.WithCorrelationID(context.Background(), "corr-1")
		atBound := strings.Repeat("x", 4096)
		if _, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, map[string]string{"cell_id": atBound}, allow); err != nil {
			t.Fatalf("attribute value at 4096 bytes: %v, want success", err)
		}
		overBound := strings.Repeat("x", 4097)
		if _, err := telemetry.BuildEnvelope(ctx, res, telemetry.OutcomeSuccess, 1, map[string]string{"cell_id": overBound}, allow); err == nil {
			t.Fatal("attribute value at 4097 bytes: got success, want rejection")
		}
	})
}
