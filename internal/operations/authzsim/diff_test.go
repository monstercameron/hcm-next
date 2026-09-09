package authzsim

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestDiff_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestDiff_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	got := DiffDecisions(authz.Decision{SubjectDisclosable: false}, authz.Decision{SubjectDisclosable: true})
	if got.Explanation != "subject_disclosable: false -> true" || len(got.FieldDeltas) != 0 || got.Digest == "" {
		t.Fatalf("coarse diff = %#v", got)
	}
}

func TestDiffDecisions_DisclosureAndFieldBoundaries(t *testing.T) {
	before := authz.Decision{
		SubjectDisclosable: true,
		Scope:              authz.AuthorizationScope{Relationship: authz.RelationshipManagerChain},
		Fields: map[authz.FieldID]authz.FieldRuling{
			authz.FieldBaseSalary:   {Effect: authz.EffectDenied},
			authz.FieldWorkerNumber: {Effect: authz.EffectAllow},
		},
	}
	after := authz.Decision{
		SubjectDisclosable: true,
		Scope:              authz.AuthorizationScope{Relationship: authz.RelationshipAdministrative},
		Fields: map[authz.FieldID]authz.FieldRuling{
			authz.FieldBaseSalary: {Effect: authz.EffectAllow},
			authz.FieldCaseNotes:  {Effect: authz.EffectRedacted},
		},
	}
	got := DiffDecisions(before, after)
	if got.ScopeRelationshipBefore != "MANAGER_CHAIN" || got.ScopeRelationshipAfter != "ADMINISTRATIVE" {
		t.Fatalf("scope relationship = %q -> %q", got.ScopeRelationshipBefore, got.ScopeRelationshipAfter)
	}
	wantFields := []authz.FieldID{authz.FieldBaseSalary, authz.FieldCaseNotes, authz.FieldWorkerNumber}
	if len(got.FieldDeltas) != len(wantFields) {
		t.Fatalf("field delta count = %d, want %d", len(got.FieldDeltas), len(wantFields))
	}
	for i, field := range wantFields {
		if got.FieldDeltas[i].Field != field {
			t.Fatalf("delta[%d].Field = %q, want %q", i, got.FieldDeltas[i].Field, field)
		}
	}
	if !got.FieldDeltas[0].Changed || got.FieldDeltas[0].Before != authz.EffectDenied || got.FieldDeltas[0].After != authz.EffectAllow {
		t.Fatalf("salary delta = %#v", got.FieldDeltas[0])
	}
	if !got.FieldDeltas[1].Changed || got.FieldDeltas[1].Before != authz.EffectUnspecified || got.FieldDeltas[1].After != authz.EffectRedacted {
		t.Fatalf("case notes delta = %#v", got.FieldDeltas[1])
	}
	if !got.FieldDeltas[2].Changed || got.FieldDeltas[2].Before != authz.EffectAllow || got.FieldDeltas[2].After != authz.EffectUnspecified {
		t.Fatalf("worker number delta = %#v", got.FieldDeltas[2])
	}
	if got.Explanation != "scope MANAGER_CHAIN -> ADMINISTRATIVE; 3/3 fields changed" || got.Digest == "" {
		t.Fatalf("field diff summary = %#v", got)
	}
	if !reflect.DeepEqual(got, DiffDecisions(before, after)) {
		t.Fatal("same inputs produced different diff")
	}
}

func TestDiffDecisions_CollapsesEitherWithheldSide(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before bool
		after  bool
	}{
		{"before withheld", false, true},
		{"after withheld", true, false},
		{"both withheld", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DiffDecisions(
				authz.Decision{SubjectDisclosable: tc.before, Scope: authz.AuthorizationScope{Relationship: authz.RelationshipManagerChain}, Fields: map[authz.FieldID]authz.FieldRuling{authz.FieldBaseSalary: {Effect: authz.EffectAllow}}},
				authz.Decision{SubjectDisclosable: tc.after, Scope: authz.AuthorizationScope{Relationship: authz.RelationshipAdministrative}, Fields: map[authz.FieldID]authz.FieldRuling{authz.FieldCaseNotes: {Effect: authz.EffectDenied}}},
			)
			if len(got.FieldDeltas) != 0 || got.ScopeRelationshipBefore != "" || got.ScopeRelationshipAfter != "" {
				t.Fatalf("withheld diff leaked detail: %#v", got)
			}
			want := "subject_disclosable: " + boolText(tc.before) + " -> " + boolText(tc.after)
			if got.Explanation != want || got.Digest == "" {
				t.Fatalf("explanation = %q, want %q", got.Explanation, want)
			}
		})
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
