package defaultactivation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/defaultactivation"
)

func feature(disposition defaultactivation.Disposition) defaultactivation.Feature {
	return defaultactivation.Feature{
		ID: "people.home", Version: "v1", Owner: "platform-experience", BusinessOwner: "people",
		DefaultDisposition: disposition, CapabilityRefs: []string{"people.read"},
		QueryContracts: []string{"people.home.query"}, Tables: []string{"worker_projection"},
	}
}

func manifest(ids ...string) defaultactivation.Manifest {
	return defaultactivation.Manifest{ID: "release-2026-09", Version: "v1", Digest: "sha256:manifest", AdmittedFeatureIDs: ids}
}

func TestTodo_ALIGN_048(t *testing.T) {
	got, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: manifest("people.home")})
	if err != nil || !got.Allowed || got.Reason != "ADMITTED_DEFAULT_FEATURE" || got.EvidenceDigest == "" {
		t.Fatalf("admitted feature = %+v, err=%v", got, err)
	}
	for _, disposition := range []defaultactivation.Disposition{defaultactivation.AvailableNotEnabled, defaultactivation.CustomerDefined, defaultactivation.Deferred, defaultactivation.Prohibited} {
		_, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(disposition), Manifest: manifest("people.home")})
		if !errors.Is(err, defaultactivation.ErrUnadmitted) {
			t.Fatalf("disposition %s error=%v, want ErrUnadmitted", disposition, err)
		}
	}
}

func TestTodo_ALIGN_048_Property(t *testing.T) {
	for _, disposition := range []defaultactivation.Disposition{defaultactivation.CoreRequired, defaultactivation.DomainPackDefault} {
		for _, ids := range [][]string{nil, {"other.feature"}, {"people.home", "other.feature"}} {
			decision, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(disposition), Manifest: manifest(ids...)})
			if len(ids) > 0 && ids[0] == "people.home" {
				if err != nil || !decision.Allowed {
					t.Fatalf("admitted ids=%v disposition=%s: %+v err=%v", ids, disposition, decision, err)
				}
				continue
			}
			if !errors.Is(err, defaultactivation.ErrUnadmitted) {
				t.Fatalf("ids=%v disposition=%s error=%v, want ErrUnadmitted", ids, disposition, err)
			}
		}
	}
}

func TestTodo_ALIGN_048_Golden(t *testing.T) {
	decision, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.DomainPackDefault), Manifest: manifest("people.home")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(defaultactivation.Explain(decision), "allowed=true") || !strings.Contains(decision.EvidenceDigest, "sha256:") {
		t.Fatalf("decision explanation=%q", defaultactivation.Explain(decision))
	}
}

func TestTodo_ALIGN_048_Security(t *testing.T) {
	bad := feature(defaultactivation.DomainPackDefault)
	bad.CapabilityRefs = nil
	if _, err := defaultactivation.Admit(defaultactivation.Request{Feature: bad, Manifest: manifest("people.home")}); !errors.Is(err, defaultactivation.ErrInvalidInput) {
		t.Fatalf("missing capability refs error=%v", err)
	}
	if _, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.DomainPackDefault), Manifest: manifest("other.feature")}); !errors.Is(err, defaultactivation.ErrUnadmitted) {
		t.Fatalf("missing manifest admission error=%v", err)
	}
}

func TestTodo_ALIGN_048_Integration(t *testing.T) {
	decision, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.DomainPackDefault), Manifest: manifest("people.home")})
	if err != nil || !decision.Allowed || decision.ManifestID != "release-2026-09" || decision.FeatureVersion != "v1" {
		t.Fatalf("integration admission = %+v, err=%v", decision, err)
	}
}
func TestTodo_ALIGN_048_Fault(t *testing.T) {
	if _, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: defaultactivation.Manifest{}}); !errors.Is(err, defaultactivation.ErrInvalidInput) {
		t.Fatalf("invalid manifest error=%v", err)
	}
}
func TestTodo_ALIGN_048_Conformance(t *testing.T) {
	if defaultactivation.Version() != 1 || defaultactivation.ContractExplain() == "" {
		t.Fatal("default activation policy contract is incomplete")
	}
}
