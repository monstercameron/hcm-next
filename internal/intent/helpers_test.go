package intent_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

// goldenText compares got against testdata/name, or rewrites it under -update.
func goldenText(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", name, err)
	}
	goldenText(t, name, append(b, '\n'))
}

// fixedClock returns the same instant on every call, so envelopes and goldens
// are byte-identical across runs.
func fixedClock() intent.Clock {
	at := values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	return func() values.Instant { return at }
}

// countingIDs mints deterministic identifiers. Real instances use UUIDv7; the
// tests need reproducible goldens, which is exactly why the id source is a
// parameter rather than a package-level call.
func countingIDs(prefix string) intent.IDSource {
	n := 0
	return func() (string, error) {
		n++
		return fmt.Sprintf("%s-%08d-0000-7000-8000-000000000000", prefix, n), nil
	}
}

// registryOnce and digesterOnce are the fuzz-target constructors. A fuzz body
// runs hundreds of thousands of times, so it builds the registry and digester
// once rather than per execution.
var (
	sharedRegistryOnce sync.Once
	sharedRegistry     *intent.Registry
	sharedRegistryErr  error

	sharedDigesterOnce sync.Once
	sharedDigester     *protomap.Digester
	sharedDigesterErr  error
)

func registryOnce() (*intent.Registry, error) {
	sharedRegistryOnce.Do(func() {
		sharedRegistry, sharedRegistryErr = definitions.NewRegistry()
	})
	return sharedRegistry, sharedRegistryErr
}

func digesterOnce() (*protomap.Digester, error) {
	sharedDigesterOnce.Do(func() {
		sharedDigester, sharedDigesterErr = protomap.NewDefaultDigester()
	})
	return sharedDigester, sharedDigesterErr
}

func mustRegistry(t *testing.T) *intent.Registry {
	t.Helper()
	reg, err := definitions.NewRegistry()
	if err != nil {
		t.Fatalf("compile catalog registry: %v", err)
	}
	return reg
}

func mustDigester(t *testing.T) *protomap.Digester {
	t.Helper()
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("build digester: %v", err)
	}
	return d
}

func mustResolve(t *testing.T, reg *intent.Registry, text string) intent.Definition {
	t.Helper()
	def, err := reg.ResolveText(text)
	if err != nil {
		t.Fatalf("resolve %s: %v", text, err)
	}
	return def
}

// controlSnapshots returns a fully pinned control context.
func controlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{
		CapabilityRegistryDigest:     "cap-registry-1",
		PolicyBundleDigest:           "policy-bundle-1",
		LegalContextDigest:           "legal-1",
		EntitlementDigest:            "entitlement-1",
		ReferenceDataDigest:          "reference-1",
		ClassificationTaxonomyDigest: "taxonomy-1",
		ClassificationLabelSetDigest: "labels-1",
		DLPDecisionDigest:            "dlp-1",
	}
}

func principal() intent.PrincipalReference {
	return intent.PrincipalReference{
		PrincipalID:          "principal:hr-partner-7",
		Kind:                 intent.InitiatorHuman,
		IdentityAssuranceRef: "assurance.mfa_session/v1",
	}
}

// promoteSpec builds a valid PromoteWorker creation request.
func promoteSpec() intent.InstanceSpec {
	at := values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	return intent.InstanceSpec{
		Tenant:              values.TenantId("acme-eu"),
		OrganizationScopeID: "org:acme-eu:engineering",
		Initiator:           principal(),
		Purpose:             "promotion.annual_cycle",
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			{Kind: "POSITION", SubjectID: "position:staff-engineer", AuthorityDomain: "POSITION"},
		},
		RequestedEffectiveAt: &at,
		Request: intent.TypedPayload{
			Schema:    schemaOf("hcmnext.people.v1.PromoteWorkerRequest"),
			WireBytes: []byte{0x0a, 0x04, 't', 'e', 's', 't'},
		},
		IdempotencyKey:                "idem:promote:9001:2026-10-01",
		CorrelationID:                 "corr:1",
		TraceID:                       "trace:1",
		Classification:                "CONFIDENTIAL_HR",
		RetentionClass:                "WORKER_TRANSACTION",
		ControlSnapshots:              controlSnapshots(),
		ExecutionMode:                 intent.ModeSimulate,
		SourceAuthoritySnapshotDigest: "authority-1",
		RiskContextDigest:             "risk-1",
	}
}

func schemaOf(name string) intent.SchemaRef {
	return intent.SchemaRef{SchemaID: name, Version: 1, ProtobufFullName: name}
}

// promoteBaseline returns a baseline snapshot in which the promote request is
// fully resolvable.
func promoteBaseline(t *testing.T) intent.BaselineSnapshot {
	t.Helper()
	rev, err := values.NewSequenceRevision("people.employment.9001", 42)
	if err != nil {
		t.Fatalf("build baseline revision: %v", err)
	}
	return intent.BaselineSnapshot{
		SnapshotID: "snapshot:1",
		ObservedAt: values.NewInstant(time.Date(2026, 9, 3, 11, 59, 0, 0, time.UTC)),
		Revisions:  map[string]values.RevisionToken{"people.employment.9001": rev},
		KnownSubjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			{Kind: "POSITION", SubjectID: "position:staff-engineer", AuthorityDomain: "POSITION"},
		},
		PresentInputs: []string{
			"employment_ref", "target_position_ref", "effective_time", "reason_ref",
		},
	}
}

func mustResourceKey(t *testing.T, segments ...string) values.ResourceKey {
	t.Helper()
	k, err := values.NewResourceKey(values.TenantId("acme-eu"), values.Kind("assignment"), segments...)
	if err != nil {
		t.Fatalf("build resource key: %v", err)
	}
	return k
}

func mustSequenceRevision(t *testing.T, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("build revision token: %v", err)
	}
	return rev
}

func mustInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	iv, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatalf("build effective interval: %v", err)
	}
	return iv
}
