package adapters_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/mapping"
	"github.com/monstercameron/hcm-next/internal/connectivity/mapping/execute"
	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/adapters"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/exec"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
)

// The connectivity migration proof.
//
// internal/connectivity/mapping is imported HERE ONLY, in a test, for the
// same reason internal/workflow is: the comparison must run the site's own
// current code, and the adapters package must not depend on the site.

// intg006Profile restates the INTG-006 fixture profile exactly as
// internal/connectivity/mapping/profile_test.go's own validProfile() declares
// it (that helper is unexported, so it is restated rather than called). The
// golden assertion below proves the restatement is the same fixture: it must
// compile to the digest INTG-006's own golden pins.
func intg006Profile() mapping.MappingProfile {
	return mapping.MappingProfile{
		MappingID: "promotion-worker", Version: 1, SourceSystemRef: "workday.worker@v1", TargetEntity: "worker",
		TargetFields: []mapping.TargetField{
			{Name: "worker.external_id", Classification: mapping.ClassificationInternal},
			{Name: "person.name.given", Classification: mapping.ClassificationPII},
			{Name: "person.name.family", Classification: mapping.ClassificationPII},
		},
		FieldMappings: []mapping.FieldMapping{
			{SourceField: "Worker_ID", TargetField: "worker.external_id", TransformationIRDigest: mapping.IdentityTransformation, Required: true, Classification: mapping.ClassificationInternal},
			{SourceField: "Legal_First_Name", TargetField: "person.name.given", TransformationIRDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Required: true, Classification: mapping.ClassificationPII, SourceClassification: mapping.ClassificationPII},
		},
	}
}

// profileFromCompiled translates a compiled MappingProfileVersion into the
// adapters mirror, reading only the accessors the site exports.
func profileFromCompiled(t *testing.T, compiled mapping.MappingProfileVersion) adapters.ConnectivityProfile {
	t.Helper()
	site := compiled.Profile()
	out := adapters.ConnectivityProfile{
		MappingID:       site.MappingID,
		SourceSystemRef: site.SourceSystemRef,
		TargetEntity:    site.TargetEntity,
	}
	version, ok := site.Version.(int)
	if !ok {
		t.Fatalf("compiled profile version is %T, want int", site.Version)
	}
	out.Version = version
	fields, ok := site.TargetFields.([]mapping.TargetField)
	if !ok {
		t.Fatalf("compiled profile target fields are %T, want []mapping.TargetField", site.TargetFields)
	}
	for _, f := range fields {
		out.TargetFields = append(out.TargetFields, adapters.ConnectivityTargetField{
			Name: f.Name, Classification: string(f.Classification),
		})
	}
	for _, m := range compiled.Mappings() {
		out.Mappings = append(out.Mappings, adapters.ConnectivityFieldMapping{
			SourceField:            m.SourceField,
			TargetField:            m.TargetField,
			TransformationIRDigest: m.TransformationIRDigest,
			Identity:               m.Identity,
			Required:               m.Required,
			Classification:         string(m.Classification),
		})
	}
	return out
}

func TestTodo_XFORM_008_MigrationProof_ConnectivityProfile(t *testing.T) {
	compiled, err := mapping.Compile(intg006Profile())
	if err != nil {
		t.Fatalf("mapping.Compile: %v", err)
	}
	// The fixture is the INTG-006 fixture: same profile, same pinned digest.
	const intg006Digest = "sha256:431fae998e4f6bf24383abe0ac73842b9e06a7936e94a9398aa2239b24d6466b"
	if compiled.Digest() != intg006Digest {
		t.Fatalf("restated fixture compiled to %s, want INTG-006's pinned %s", compiled.Digest(), intg006Digest)
	}

	lowered, err := adapters.LowerConnectivityProfile(profileFromCompiled(t, compiled))
	if err != nil {
		t.Fatalf("LowerConnectivityProfile: %v", err)
	}
	t.Logf("%s", lowered.Explain())

	// One identity mapping lowers to one instruction; the digest-referencing
	// mapping is recorded as a delegation, never dropped.
	if len(lowered.Bindings) != 1 || lowered.Bindings[0].Target != "worker.external_id" {
		t.Fatalf("bindings = %+v, want exactly worker.external_id", lowered.Bindings)
	}
	if len(lowered.Delegations) != 1 {
		t.Fatalf("delegations = %+v, want exactly one", lowered.Delegations)
	}
	d := lowered.Delegations[0]
	if d.Target != "person.name.given" || d.Source != "Legal_First_Name" ||
		d.ProgramDigest != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("delegation = %+v", d)
	}

	// Byte-identity for the part the profile itself computes, against the
	// connectivity site's LEGACY executable path -- mapping.Execute over
	// mapping.IR, the transform runtime this migration retires. (The site's
	// shared-engine path, internal/connectivity/mapping/execute, is compared
	// separately in TestTodo_XFORM_008_Integration_ConnectivityExecute.)
	siteIR := mapping.IR{
		Version: "profile:promotion-worker/v1",
		Rules: []mapping.Rule{
			{Source: "Worker_ID", Target: "worker.external_id", Op: mapping.OpIdentity, Null: mapping.NullError},
		},
	}
	input := map[string]string{"Worker_ID": "WD-000123", "Legal_First_Name": "Jane"}
	siteResult, err := mapping.Execute(siteIR, input)
	if err != nil {
		t.Fatalf("mapping.Execute: %v", err)
	}
	rows, err := lowered.Run([]map[string]string{{
		"connectivity.mapping_profile.promotion-worker.v1.source.Worker_ID": input["Worker_ID"],
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := lowered.Texts(rows[0])
	if err != nil {
		t.Fatalf("Texts: %v", err)
	}
	assertByteIdentical(t, "connectivity profile", fieldsToMap(t, siteResult), got)
}

func fieldsToMap(t *testing.T, r mapping.Result) map[string]string {
	t.Helper()
	out := make(map[string]string, len(r.Fields))
	for _, f := range r.Fields {
		if f.Deleted {
			t.Fatalf("field %q is a delete marker, which the lowering refuses rather than represents", f.Target)
		}
		out[f.Target] = f.Value
	}
	return out
}

// connectivityRulesFixture exercises every connectivity rule operation that
// has an IR equivalent.
func connectivityRulesFixture() mapping.IR {
	return mapping.IR{
		Version: "workday.worker/v1",
		Rules: []mapping.Rule{
			{Source: "Worker_ID", Target: "worker.external_id", Op: mapping.OpIdentity, Null: mapping.NullError},
			{Source: "ignored", Target: "worker.source_system", Op: mapping.OpConstant, Argument: "workday", Null: mapping.NullError},
			{Source: "Hire_Date", Target: "worker.hired_at", Op: mapping.OpDate, Argument: time.RFC3339, Null: mapping.NullError},
		},
	}
}

func rulesFromSite(site mapping.IR) adapters.ConnectivityRules {
	out := adapters.ConnectivityRules{Version: site.Version}
	for _, r := range site.Rules {
		out.Rules = append(out.Rules, adapters.ConnectivityRule{
			Source: r.Source, Target: r.Target, Op: adapters.ConnectivityOp(r.Op),
			Argument: r.Argument, Lookup: r.Lookup, Null: adapters.ConnectivityNullPolicy(r.Null),
		})
	}
	return out
}

func TestTodo_XFORM_008_MigrationProof_ConnectivityRules(t *testing.T) {
	siteIR := connectivityRulesFixture()
	lowered, err := adapters.LowerConnectivityRules(rulesFromSite(siteIR))
	if err != nil {
		t.Fatalf("LowerConnectivityRules: %v", err)
	}
	t.Logf("%s", lowered.Explain())

	inputs := []map[string]string{
		{"Worker_ID": "WD-000123", "ignored": "", "Hire_Date": "2026-03-01T09:00:00Z"},
		{"Worker_ID": "WD-000124", "ignored": "", "Hire_Date": "2026-03-01T09:00:00+02:00"},
		{"Worker_ID": "WD-000125", "ignored": "", "Hire_Date": "2026-03-01T09:00:00.123456789Z"},
	}
	const prefix = "connectivity.mapping_ir.workday.worker/v1.source."
	for _, input := range inputs {
		t.Run(input["Hire_Date"], func(t *testing.T) {
			siteResult, err := mapping.Execute(siteIR, input)
			if err != nil {
				t.Fatalf("mapping.Execute: %v", err)
			}
			rows, err := lowered.Run([]map[string]string{{
				prefix + "Worker_ID": input["Worker_ID"],
				prefix + "Hire_Date": input["Hire_Date"],
			}})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, err := lowered.Texts(rows[0])
			if err != nil {
				t.Fatalf("Texts: %v", err)
			}
			assertByteIdentical(t, "connectivity rules", fieldsToMap(t, siteResult), got)
		})
	}
}

func TestLowerConnectivityRulesRefusals(t *testing.T) {
	base := func() adapters.ConnectivityRules {
		return adapters.ConnectivityRules{
			Version: "workday.worker/v1",
			Rules: []adapters.ConnectivityRule{
				{Source: "Worker_ID", Target: "worker.external_id", Op: adapters.ConnectivityOpIdentity, Null: adapters.ConnectivityNullError},
			},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*adapters.ConnectivityRules)
		feature string
	}{
		{"trim", func(r *adapters.ConnectivityRules) { r.Rules[0].Op = adapters.ConnectivityOpTrim }, adapters.FeatureStringNormalization},
		{"upper", func(r *adapters.ConnectivityRules) { r.Rules[0].Op = adapters.ConnectivityOpUpper }, adapters.FeatureStringNormalization},
		{"lower", func(r *adapters.ConnectivityRules) { r.Rules[0].Op = adapters.ConnectivityOpLower }, adapters.FeatureStringNormalization},
		{"lookup", func(r *adapters.ConnectivityRules) {
			r.Rules[0].Op = adapters.ConnectivityOpLookup
			r.Rules[0].Lookup = map[string]string{"east": "BAND_E"}
		}, adapters.FeatureCrosswalkLookup},
		{"money", func(r *adapters.ConnectivityRules) {
			r.Rules[0].Op = adapters.ConnectivityOpMoney
			r.Rules[0].Argument = "USD"
		}, adapters.FeatureMoneyParse},
		{"compose", func(r *adapters.ConnectivityRules) {
			r.Rules[0].Op = adapters.ConnectivityOpCompose
			r.Rules[0].Argument = "urn:worker:${value}"
		}, adapters.FeatureTemplateCompose},
		{"non-RFC3339 date layout", func(r *adapters.ConnectivityRules) {
			r.Rules[0].Op = adapters.ConnectivityOpDate
			r.Rules[0].Argument = "2006-01-02"
		}, adapters.FeatureLayoutDateParse},
		{"omit null policy", func(r *adapters.ConnectivityRules) { r.Rules[0].Null = adapters.ConnectivityNullOmit }, adapters.FeatureNullPolicy},
		{"delete null policy", func(r *adapters.ConnectivityRules) { r.Rules[0].Null = adapters.ConnectivityNullDelete }, adapters.FeatureNullPolicy},
		{"empty constant", func(r *adapters.ConnectivityRules) {
			r.Rules[0].Op = adapters.ConnectivityOpConstant
			r.Rules[0].Argument = ""
		}, adapters.FeatureEmptyLiteral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := base()
			tc.mutate(&rules)
			_, err := adapters.LowerConnectivityRules(rules)
			var refusal adapters.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error = %v, want an adapters.Refusal", err)
			}
			if refusal.Feature != tc.feature {
				t.Fatalf("refusal feature = %q, want %q (%v)", refusal.Feature, tc.feature, err)
			}
			if refusal.Site != adapters.SiteConnectivityRules {
				t.Fatalf("refusal site = %q", refusal.Site)
			}
		})
	}
}

// TestConnectivityDivergences pins each declared difference against the
// site's real behaviour. Each is a finding reported by XFORM-008, not a
// change: internal/connectivity/mapping is not edited by this lane.
func TestConnectivityDivergences(t *testing.T) {
	t.Run("missing source: the site refuses the whole row, the lowering yields ABSENT", func(t *testing.T) {
		siteIR := mapping.IR{Version: "v1", Rules: []mapping.Rule{
			{Source: "Worker_ID", Target: "worker.external_id", Op: mapping.OpIdentity, Null: mapping.NullError},
		}}
		if _, err := mapping.Execute(siteIR, map[string]string{}); !errors.Is(err, mapping.ErrMissingSource) {
			t.Fatalf("site error = %v, want ErrMissingSource", err)
		}
		lowered, err := adapters.LowerConnectivityRules(rulesFromSite(siteIR))
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		rows, err := lowered.Run([]map[string]string{{}})
		if err != nil {
			t.Fatalf("lowered Run: %v", err)
		}
		got, err := lowered.Texts(rows[0])
		if err != nil {
			t.Fatalf("Texts: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("lowered produced %+v, want no present target", got)
		}
		assertDivergence(t, lowered.Divergences, "missing_source")
	})

	t.Run("empty output: the site refuses, the lowering keeps a present empty string", func(t *testing.T) {
		siteIR := mapping.IR{Version: "v1", Rules: []mapping.Rule{
			{Source: "Worker_ID", Target: "worker.external_id", Op: mapping.OpIdentity, Null: mapping.NullError},
		}}
		if _, err := mapping.Execute(siteIR, map[string]string{"Worker_ID": ""}); !errors.Is(err, mapping.ErrTransform) {
			t.Fatalf("site error = %v, want ErrTransform", err)
		}
		lowered, err := adapters.LowerConnectivityRules(rulesFromSite(siteIR))
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		rows, err := lowered.Run([]map[string]string{{
			"connectivity.mapping_ir.v1.source.Worker_ID": "",
		}})
		if err != nil {
			t.Fatalf("lowered Run: %v", err)
		}
		got, err := lowered.Texts(rows[0])
		if err != nil {
			t.Fatalf("Texts: %v", err)
		}
		if v, ok := got["worker.external_id"]; !ok || v != "" {
			t.Fatalf("lowered produced %+v, want a present empty value", got)
		}
		assertDivergence(t, lowered.Divergences, "empty_output")
	})

	t.Run("undeclared null policy validates as ERROR but executes as OMIT", func(t *testing.T) {
		siteIR := mapping.IR{Version: "v1", Rules: []mapping.Rule{
			{Source: "Worker_ID", Target: "worker.external_id", Op: mapping.OpIdentity},
		}}
		if err := siteIR.Validate(); err != nil {
			t.Fatalf("the site validates a rule with no null policy: %v", err)
		}
		result, err := mapping.Execute(siteIR, map[string]string{})
		if err != nil {
			t.Fatalf("the site executed a missing source without the ERROR its own validation implied: %v", err)
		}
		if len(result.Fields) != 0 {
			t.Fatalf("site fields = %+v, want none (the rule executed as if OMIT)", result.Fields)
		}
		lowered, err := adapters.LowerConnectivityRules(rulesFromSite(siteIR))
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		assertDivergence(t, lowered.Divergences, "undeclared_null_policy")
	})

	t.Run("whitespace: the site trims before parsing, the lowering does not", func(t *testing.T) {
		siteIR := mapping.IR{Version: "v1", Rules: []mapping.Rule{
			{Source: "Hire_Date", Target: "worker.hired_at", Op: mapping.OpDate, Argument: time.RFC3339, Null: mapping.NullError},
		}}
		if _, err := mapping.Execute(siteIR, map[string]string{"Hire_Date": " 2026-03-01T09:00:00Z "}); err != nil {
			t.Fatalf("the site trims and parses a padded cell: %v", err)
		}
		lowered, err := adapters.LowerConnectivityRules(rulesFromSite(siteIR))
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if _, err := lowered.Run([]map[string]string{{
			"connectivity.mapping_ir.v1.source.Hire_Date": " 2026-03-01T09:00:00Z ",
		}}); err == nil {
			t.Fatal("the lowered program accepted a padded timestamp; the declared divergence says it refuses")
		}
		assertDivergence(t, lowered.Divergences, "whitespace_trim")
	})
}

func assertDivergence(t *testing.T, declared []adapters.Divergence, feature string) {
	t.Helper()
	for _, d := range declared {
		if d.Feature == feature {
			return
		}
	}
	t.Fatalf("divergence %q was not declared: %+v", feature, declared)
}

func TestLowerConnectivityProfileRefusals(t *testing.T) {
	base := func() adapters.ConnectivityProfile {
		return adapters.ConnectivityProfile{
			MappingID: "promotion-worker", Version: 1,
			SourceSystemRef: "workday.worker@v1", TargetEntity: "worker",
			TargetFields: []adapters.ConnectivityTargetField{{Name: "worker.external_id", Classification: "INTERNAL"}},
			Mappings: []adapters.ConnectivityFieldMapping{
				{SourceField: "Worker_ID", TargetField: "worker.external_id", TransformationIRDigest: adapters.ConnectivityIdentityTransformation, Identity: true, Required: true, Classification: "INTERNAL"},
			},
		}
	}
	if _, err := adapters.LowerConnectivityProfile(base()); err != nil {
		t.Fatalf("the unmutated profile must lower: %v", err)
	}

	t.Run("a profile whose every field delegates compiles no instruction", func(t *testing.T) {
		p := base()
		p.Mappings[0].Identity = false
		p.Mappings[0].TransformationIRDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		_, err := adapters.LowerConnectivityProfile(p)
		var refusal adapters.Refusal
		if !errors.As(err, &refusal) || refusal.Feature != adapters.FeatureInvalidMapping {
			t.Fatalf("error = %v, want an invalid_mapping refusal", err)
		}
	})

	t.Run("a mapping writing an undeclared target field", func(t *testing.T) {
		p := base()
		p.Mappings[0].TargetField = "worker.unknown"
		_, err := adapters.LowerConnectivityProfile(p)
		var refusal adapters.Refusal
		if !errors.As(err, &refusal) || refusal.Feature != adapters.FeatureInvalidMapping {
			t.Fatalf("error = %v, want an invalid_mapping refusal", err)
		}
	})
}

// delegatedGivenNameProgram compiles, through the shared engine's own
// XFORM-001 contract and XFORM-002 compiler, the transformation program the
// INTG-006 fixture's second field mapping delegates to. The fixture pins a
// placeholder digest that names no compiled program anywhere in the
// repository (a finding, not a defect this lane fixes), so the integration
// below pins a real one in its place and leaves the fixture untouched.
func delegatedGivenNameProgram(t *testing.T) ir.Program {
	t.Helper()
	source := transformation.Path{Schema: "source", Field: "Legal_First_Name", Type: transformation.TypeString}
	program, err := ir.Compile(transformation.TransformationDefinition{
		Version: transformation.ContractVersion,
		Name:    "connectivity.delegated.person_name_given",
		Owner:   "connectivity.mapping_profile",
		Phase:   "MAP",
		Source: transformation.Schema{Name: "source", Version: 1,
			Fields: []transformation.Field{{Name: "Legal_First_Name", Type: transformation.TypeString}}},
		Destination: transformation.Schema{Name: "person.name", Version: 1,
			Fields: []transformation.Field{{Name: "given", Type: transformation.TypeString}}},
		Operations: []transformation.Operation{{
			Kind: transformation.OpCopy, Source: &source,
			Destination: transformation.Path{Schema: "person.name", Field: "given", Type: transformation.TypeString},
		}},
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits: transformation.ResourceLimits{
			MaxOperations: 1, MaxInputBytes: 4096, MaxOutputBytes: 4096, MaxExpansion: 1,
		},
		Failure:     transformation.FailureReject,
		SideEffects: transformation.SideEffectsNone,
	})
	if err != nil {
		t.Fatalf("ir.Compile: %v", err)
	}
	return program
}

// TestTodo_XFORM_008_Integration_ConnectivityExecute proves the lowering
// agrees, field for field and byte for byte, with the connectivity site's own
// shared-engine profile executor (internal/connectivity/mapping/execute): the
// identity mapping the lowering compiles, and the delegated mapping it
// records rather than compiling, together reproduce that executor's whole
// output.
func TestTodo_XFORM_008_Integration_ConnectivityExecute(t *testing.T) {
	program := delegatedGivenNameProgram(t)
	digest, err := program.Digest()
	if err != nil {
		t.Fatalf("program digest: %v", err)
	}

	profileInput := intg006Profile()
	profileInput.FieldMappings[1].TransformationIRDigest = digest
	compiled, err := mapping.Compile(profileInput)
	if err != nil {
		t.Fatalf("mapping.Compile: %v", err)
	}

	source := exec.Record{
		"Worker_ID":        exec.Present(transformation.TypeString, "WD-000123"),
		"Legal_First_Name": exec.Present(transformation.TypeString, "Jane"),
	}
	siteResult, err := execute.Execute(compiled, source, map[string]ir.Program{digest: program})
	if err != nil {
		t.Fatalf("execute.Execute: %v", err)
	}
	siteOutput := map[string]string{}
	for target, value := range siteResult.Output {
		text, ok := value.Data.(string)
		if !ok {
			t.Fatalf("site produced %T for %s, want string", value.Data, target)
		}
		siteOutput[target] = text
	}

	lowered, err := adapters.LowerConnectivityProfile(profileFromCompiled(t, compiled))
	if err != nil {
		t.Fatalf("LowerConnectivityProfile: %v", err)
	}
	if len(lowered.Delegations) != 1 || lowered.Delegations[0].ProgramDigest != digest {
		t.Fatalf("delegations = %+v, want the pinned program digest %s", lowered.Delegations, digest)
	}

	rows, err := lowered.Run([]map[string]string{{
		"connectivity.mapping_profile.promotion-worker.v1.source.Worker_ID": "WD-000123",
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	loweredOutput, err := lowered.Texts(rows[0])
	if err != nil {
		t.Fatalf("Texts: %v", err)
	}
	// The delegated field is run through the very program the profile pins,
	// which is what "delegation" means: the lowering does not recompile it.
	delegatedRows, err := exec.Execute(program, []exec.Record{{
		"source.Legal_First_Name": exec.Present(transformation.TypeString, "Jane"),
	}}, exec.Limits{MaxRows: 1, MaxSteps: 4})
	if err != nil {
		t.Fatalf("delegated exec.Execute: %v", err)
	}
	delegated, ok := delegatedRows[0]["person.name.given"]
	if !ok {
		t.Fatalf("delegated program produced %+v", delegatedRows[0])
	}
	loweredOutput[lowered.Delegations[0].Target] = delegated.Data.(string)

	assertByteIdentical(t, "connectivity profile via mapping/execute", siteOutput, loweredOutput)
}
