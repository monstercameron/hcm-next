package readiness

import (
	"crypto/ed25519"
	"reflect"
	"strings"
	"testing"
)

// placeholderRehearsal contains labelled refs and digests only; it is not a
// real customer or provider selection.
func placeholderRehearsal() Rehearsal {
	field := SourceField{Ref: "placeholder:worker-id", CanonicalPath: "person.worker_id", Present: true, AuthorityRef: "placeholder:source-authority", Classification: "INTERNAL", Purpose: "promotion eligibility", EffectiveAt: "2026-01-01T00:00:00Z"}
	return Rehearsal{SchemaVersion: 1, RehearsalID: "rehearsal:placeholder-partner", TenantRef: "placeholder:design-partner", SourceSystem: "placeholder:hris", Fields: []SourceField{field}, Records: []SourceRecord{{Ref: "placeholder:worker-row-1", Fields: []SourceField{field}}}, Configurations: []Configuration{{Ref: "placeholder:promotion-config", Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64), Compatible: true, AuthorityRef: "placeholder:config-authority"}}, Identities: []IdentityCrosswalk{{SourceRef: "placeholder:worker-id", CanonicalRef: "placeholder:person-id", AuthorityRef: "placeholder:identity-authority", MatchMethod: "reviewed-crosswalk", Confidence: 1}}}
}

func TestPilotReadinessRehearsalClassifiesEverySourceFieldRecordConfigurationAndIdentity(t *testing.T) {
	result, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ready || !result.ZeroMutation || result.Digest == "" {
		t.Fatalf("readiness result = %+v", result)
	}
	if result.Reconciliation.InputFields != result.Reconciliation.ClassifiedFields || result.Reconciliation.InputRecords != result.Reconciliation.ClassifiedRecords || result.Reconciliation.InputConfigurations != result.Reconciliation.ClassifiedConfigurations || result.Reconciliation.InputIdentities != result.Reconciliation.ClassifiedIdentities {
		t.Fatalf("unreconciled result = %+v", result.Reconciliation)
	}
}

func TestTodo_CUSTOMER_002_Property(t *testing.T) {
	result, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	if result.Reconciliation.EffectiveDates != result.Reconciliation.ClassifiedEffectiveDates {
		t.Fatalf("effective-date drift = %+v", result.Reconciliation)
	}
}

func TestTodo_CUSTOMER_002_Golden(t *testing.T) {
	one, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	two, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest || !reflect.DeepEqual(one.Items, two.Items) {
		t.Fatalf("non-deterministic rehearsal: %+v %+v", one, two)
	}
}

func FuzzTodo_CUSTOMER_002(f *testing.F) {
	f.Add("placeholder:field")
	f.Fuzz(func(t *testing.T, ref string) {
		r := placeholderRehearsal()
		r.Fields[0].Ref = ref
		if _, err := Evaluate(r); err != nil {
			t.Fatalf("field ref %q caused structural failure: %v", ref, err)
		}
	})
}

func TestTodo_CUSTOMER_002_Integration(t *testing.T) {
	result, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(result, "placeholder:evidence-signer", private)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(signed, public); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CUSTOMER_002_Fault(t *testing.T) {
	r := placeholderRehearsal()
	r.Configurations[0].Compatible = false
	result, err := Evaluate(r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready || !containsBlocker(result.Blockers, "CONFIGURATION:placeholder:promotion-config:QUARANTINE") {
		t.Fatalf("incompatible configuration not blocked: %+v", result)
	}
}

func TestTodo_CUSTOMER_002_Security(t *testing.T) {
	result, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(nil)
	signed, _ := Sign(result, "placeholder:signer", private)
	signed.Result.Items[0].Reason = "tampered"
	if Verify(signed, public) == nil {
		t.Fatal("tampered readiness evidence verified")
	}
}

func TestTodo_CUSTOMER_002_Conformance(t *testing.T) {
	if Version() != 1 || !strings.Contains(Explain(), "no-mutation") {
		t.Fatalf("contract metadata missing: version=%d explain=%q", Version(), Explain())
	}
	if err := Check(placeholderRehearsal()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CUSTOMER_002_Recovery(t *testing.T) {
	r := placeholderRehearsal()
	before := reflect.ValueOf(r)
	if _, err := Evaluate(r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Interface(), reflect.ValueOf(r).Interface()) {
		t.Fatal("evaluation mutated rehearsal input")
	}
}

func TestTodo_CUSTOMER_002_Mutation(t *testing.T) {
	result, err := Evaluate(placeholderRehearsal())
	if err != nil {
		t.Fatal(err)
	}
	result.Items[0].Ref = "changed"
	mutatedDigest, err := resultDigest(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest == mutatedDigest {
		t.Fatal("evidence digest ignored a material item mutation")
	}
}

func containsBlocker(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
