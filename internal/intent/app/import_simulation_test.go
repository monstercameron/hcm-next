package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/dataops/importing"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/intent"
	intentdefinitions "github.com/monstercameron/hcm-next/internal/intent/definitions"
	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type importProposalDigester struct{}

func (importProposalDigester) RequestDigest(intent.Instance) (digest.Reference, error) {
	return digest.Reference{}, nil
}

func (importProposalDigester) ProposalDigest(p intent.ProposalRevision) (digest.Reference, error) {
	b := p.MaterialPayload().WireBytes
	s := sha256.Sum256(b)
	return digest.Reference{
		ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "test", SchemaVersion: 1,
		AlgorithmID: "sha256", CanonicalLength: uint64(len(b)), Digest: hex.EncodeToString(s[:]),
		IntentID: digest.Ptr(p.IntentID), ProposalRevisionID: digest.Ptr(p.ProposalRevisionID),
		ScopeBindingDigest: "test", MaterialProfileRef: digest.Ptr("PROPOSAL@v1"),
	}, nil
}

type importProposalSigner struct{}

func (importProposalSigner) Authority() string { return "kms:test" }
func (importProposalSigner) Sign(b []byte) (string, error) {
	s := sha256.Sum256(append([]byte("key:"), b...))
	return hex.EncodeToString(s[:]), nil
}
func (s importProposalSigner) Verify(b []byte, sig string) error {
	want, _ := s.Sign(b)
	if sig != want {
		return importing.ErrSimulationSignature
	}
	return nil
}

func signedImportSimulation(t *testing.T, signer importProposalSigner, noOp bool) importing.ImportSimulation {
	t.Helper()
	// Start with a structurally complete result and sign it through the domain
	// entry point so this test cannot fabricate the canonical signed bytes.
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := importing.Compile(reg, importing.MappingSpecInput{Version: "v1", Fields: []importing.FieldMapping{{SourceColumn: "title", Target: "job.title", Transform: importing.TransformSpec{Kind: importing.TransformTrim}}}})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := importing.StageBatch(importing.SourceDescriptor{Kind: importing.SourceKindRecords, URI: "s3://test/import.csv", Tenant: values.TenantId("acme")}, values.NewInstant(time.Unix(10, 0)), []string{"title"}, [][]string{{"Engineer"}})
	if err != nil {
		t.Fatal(err)
	}
	validation, err := importing.ValidateBatch(batch, mapping, reg)
	if err != nil {
		t.Fatal(err)
	}
	rowID := batch.Rows()[0].ID()
	key, err := values.NewResourceKey(batch.Source().Tenant, values.Kind("employment"), rowID, "job.title")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("employment:"+rowID+":job.title", 7)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(time.Unix(20, 0)))
	if err != nil {
		t.Fatal(err)
	}
	var current map[string]map[string]string
	if noOp {
		current = map[string]map[string]string{rowID: {"job.title": "Engineer"}}
	}
	out, err := importing.SimulateImport(importing.SimulationInput{Batch: batch, Mapping: mapping, Validation: validation, Registry: reg, Current: current,
		IntentType: "hcmnext.people.promote_worker/v1", Subjects: map[string]importing.SimulationSubject{rowID: {Kind: "EMPLOYMENT", SubjectID: "employment-1", AuthorityDomain: "PEOPLE"}},
		Resources: map[string]map[string]values.ResourceKey{rowID: {"job.title": key}}, CurrentRevisions: map[string]map[string]values.RevisionToken{rowID: {"job.title": revision}},
		OrganizationScopeID: "org:acme", LegalEntityID: "legal:acme", EffectiveTime: effective, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImportSimulationAdapterMintsCanonicalProposalWithoutEffects(t *testing.T) {
	signer := importProposalSigner{}
	sim := signedImportSimulation(t, signer, false)
	row := sim.Drafts[0]
	tenant := values.TenantId("acme")
	key, err := values.NewResourceKey(tenant, values.Kind("employment"), row.RowID, "job.title")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("employment:"+row.RowID+":job.title", 7)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(time.Unix(20, 0)))
	if err != nil {
		t.Fatal(err)
	}
	intentRegistry, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	request := ImportProposalRequest{
		Simulation: sim, Verifier: signer, Registry: intentRegistry, Digester: importProposalDigester{},
		IDs:    func() (string, error) { return "proposal-1", nil },
		Clock:  func() values.Instant { return values.NewInstant(time.Unix(30, 0)) },
		Tenant: tenant, OrganizationScopeID: "org:acme", LegalEntityID: "legal:acme", EffectiveTime: effective,
		CreatedBy:        intent.PrincipalReference{PrincipalID: "operator-1", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "mfa:1"},
		ControlSnapshots: testControlSnapshots(),
		TrustedPins:      ImportSimulationPins{BatchDigest: sim.BatchDigest, MappingDigest: sim.MappingDigest, ValidationDigest: sim.ValidationDigest, SnapshotDigest: sim.SnapshotDigest, ModelDigest: sim.ModelDigest},
		Rows:             map[string]ImportRowAuthority{row.RowID: {IntentID: "intent-1", ProposalRevision: 1, Subject: intent.SubjectReference{Kind: row.SubjectKind, SubjectID: row.SubjectID, AuthorityDomain: row.AuthorityDomain}, Writes: map[string]ImportWriteAuthority{"job.title": {ResourceKey: key, ExpectedRevision: revision, SourceAuthorityDecision: row.Writes[0].AuthorityRef}}}},
		Revalidation:     intent.RevalidationPlan{Rules: []string{"revalidate:authority/v1"}},
	}
	revisions, err := NewImportProposalRevisions(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || len(revisions[0].Writes) != 1 || len(revisions[0].Effects) != 0 || revisions[0].Tenant != tenant || !revisions[0].Writes[0].ExpectedRevision.Equal(revision) {
		t.Fatalf("unexpected proposal: %+v", revisions)
	}

	tampered := request
	tampered.Simulation = signedImportSimulation(t, signer, false)
	tampered.Simulation.Drafts[0].Writes[0].Proposed = "Chief Financial Officer"
	if _, err := NewImportProposalRevisions(tampered); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("tampered simulation error = %v", err)
	}

	forged := request
	forged.Rows = map[string]ImportRowAuthority{row.RowID: {IntentID: "intent-1", ProposalRevision: 1, Subject: request.Rows[row.RowID].Subject, Writes: map[string]ImportWriteAuthority{"job.title": {ResourceKey: key, SourceAuthorityDecision: "csv-claimed-authority"}}}}
	if _, err := NewImportProposalRevisions(forged); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("missing trusted revision error = %v", err)
	}

	crossTenant := request
	crossTenant.Tenant = values.TenantId("tenant-b")
	if _, err := NewImportProposalRevisions(crossTenant); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("cross-tenant transplant error = %v", err)
	}

	staleSnapshot := request
	staleSnapshot.TrustedPins.SnapshotDigest = "sha256:fresh-snapshot"
	if _, err := NewImportProposalRevisions(staleSnapshot); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("cross-snapshot transplant error = %v", err)
	}

	staleRevision := request
	otherRevision, err := values.NewSequenceRevision("employment:"+row.RowID+":job.title", 8)
	if err != nil {
		t.Fatal(err)
	}
	rowBinding := staleRevision.Rows[row.RowID]
	writeBinding := rowBinding.Writes["job.title"]
	writeBinding.ExpectedRevision = otherRevision
	rowBinding.Writes = map[string]ImportWriteAuthority{"job.title": writeBinding}
	staleRevision.Rows = map[string]ImportRowAuthority{row.RowID: rowBinding}
	if _, err := NewImportProposalRevisions(staleRevision); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("cross-revision transplant error = %v", err)
	}

	missingRegistry := request
	missingRegistry.Registry = nil
	if _, err := NewImportProposalRevisions(missingRegistry); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("missing registry error = %v", err)
	}

	governanceMismatch := request
	governanceMismatch.RequiredApprovals = []intent.RequiredApproval{{RequirementID: "unexpected", SeparationConstraint: "not-requester"}}
	if _, err := NewImportProposalRevisions(governanceMismatch); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("governance mismatch error = %v", err)
	}

	missingBinding := request
	missingBinding.Rows = nil
	if _, err := NewImportProposalRevisions(missingBinding); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("missing row binding error = %v", err)
	}

	wrongSubject := request
	binding := wrongSubject.Rows[row.RowID]
	binding.Subject.SubjectID = "employment:other"
	wrongSubject.Rows = map[string]ImportRowAuthority{row.RowID: binding}
	if _, err := NewImportProposalRevisions(wrongSubject); !errors.Is(err, ErrImportProposal) {
		t.Fatalf("subject transplant error = %v", err)
	}
}

func TestImportSimulationAdapterDoesNotMintNoOpProposal(t *testing.T) {
	signer := importProposalSigner{}
	sim := signedImportSimulation(t, signer, true)
	registry, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(time.Unix(20, 0)))
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := NewImportProposalRevisions(ImportProposalRequest{
		Simulation: sim, Verifier: signer, Registry: registry, Digester: importProposalDigester{},
		IDs: func() (string, error) { return "unused", nil }, Clock: func() values.Instant { return values.NewInstant(time.Unix(30, 0)) },
		Tenant: values.TenantId("acme"), OrganizationScopeID: "org:acme", LegalEntityID: "legal:acme", EffectiveTime: effective,
		CreatedBy: intent.PrincipalReference{PrincipalID: "operator-1", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "mfa:1"}, ControlSnapshots: testControlSnapshots(),
		TrustedPins: ImportSimulationPins{BatchDigest: sim.BatchDigest, MappingDigest: sim.MappingDigest, ValidationDigest: sim.ValidationDigest, SnapshotDigest: sim.SnapshotDigest, ModelDigest: sim.ModelDigest},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 0 || sim.NoOps != 1 || len(sim.PlannedWrites) != 0 {
		t.Fatalf("no-op produced proposal work: revisions=%d", len(revisions))
	}
}

func testControlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{CapabilityRegistryDigest: "cap", PolicyBundleDigest: "policy", LegalContextDigest: "legal", EntitlementDigest: "entitlement", ReferenceDataDigest: "reference", ClassificationTaxonomyDigest: "classification", DLPDecisionDigest: "dlp"}
}
