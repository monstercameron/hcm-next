package intentcontrol_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/intentcontrol"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/protomap"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func fullProposalFixture(t *testing.T) intent.ProposalRevision {
	t.Helper()
	key, e := values.NewResourceKey("acme", "worker", "w-1")
	if e != nil {
		t.Fatal(e)
	}
	s, e := values.ParseLocalDate("2026-01-01")
	if e != nil {
		t.Fatal(e)
	}
	en, e := values.ParseLocalDate("2026-02-01")
	if e != nil {
		t.Fatal(e)
	}
	iv, e := values.NewLocalDateInterval(s, en, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	created := values.Instant{}
	if e := created.UnmarshalText([]byte("2026-01-01T00:00:00.123456789Z")); e != nil {
		t.Fatal(e)
	}
	iv, e = iv.WithZone(values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026b"}, values.DisambiguationEarlier)
	if e != nil {
		t.Fatal(e)
	}
	amt, e := values.NewMoney("12.3400", "USD", 4, values.RoundingHalfEven)
	if e != nil {
		t.Fatal(e)
	}
	tok, e := values.NewSequenceRevision("worker.w-1", 42)
	if e != nil {
		t.Fatal(e)
	}
	opaque, e := values.NewOpaqueRevision("benefits.w-1", []byte{0, 1, 2, 255})
	if e != nil {
		t.Fatal(e)
	}
	ref := digest.Reference{ProfileID: "hcmnext.proposal", ProfileVersion: 1, SchemaID: "proposal", SchemaVersion: 1, AlgorithmID: "sha256", Digest: strings.Repeat("a", 64)}
	sub := intent.SubjectReference{Kind: "WORKER", SubjectID: "w-1", AuthorityDomain: "PEOPLE"}
	return intent.ProposalRevision{ProposalRevisionID: "rev-1", IntentID: "intent-1", Revision: 1, Tenant: "acme", OrganizationScopeID: "org-1", LegalEntityID: "legal-1", Subjects: []intent.SubjectReference{sub}, EffectiveTime: iv, CurrentState: []intent.StateAssertion{{Subject: sub, ResourceKey: key, FieldPath: "name", CanonicalText: "old"}}, ProposedState: []intent.StateAssertion{{Subject: sub, ResourceKey: key, FieldPath: "name", CanonicalText: "new"}}, Writes: []intent.PlannedWrite{{Subject: sub, ResourceKey: key, FieldPath: "name", CurrentCanonicalText: "old", ProposedCanonicalText: "new", SourceAuthorityDecision: "authority/v1", ExpectedRevision: tok, Operation: intent.WriteOperationUpdate, EffectiveInterval: iv}}, Effects: []intent.PlannedEffect{{EffectID: "e1", Kind: "EMAIL", DestinationRef: "dest", Reversibility: "REVERSIBLE", CompensationRef: "c1", ObservationRef: "obs"}}, Children: []intent.ChildIntentBinding{{Definition: intent.Ref{TypeID: "hcmnext.people.child_intent", Version: 1}, ChildIntentID: "child-1", Ordinal: 1, MaterialInputDigest: "child-digest"}}, Reservations: []intent.Reservation{{ReservationID: "r1", Kind: "BUDGET", Expiry: created}}, RequiredApprovals: []intent.RequiredApproval{{RequirementID: "a1", SeparationConstraint: "manager"}}, Obligations: []intent.Obligation{{ObligationID: "o1", Kind: "notify"}}, Compensations: []intent.CompensationDeclaration{{EffectID: "e1", Strategy: "rollback", RepairPlanID: "rp1"}}, SourceBaselines: []intent.SourceBaseline{{StreamID: "worker.w-1", ExpectedRevision: tok}, {StreamID: "benefits.w-1", ExpectedRevision: opaque}}, Attachments: []intent.AttachmentRef{{ArtifactID: "a", AlgorithmID: "sha256", Digest: "abcd"}}, Purpose: intent.PurposeDecision{Purpose: "employment", RecipientRef: "hr", DestinationRef: "internal", ResidencyRef: "us"}, Cost: &amt, Revalidation: intent.RevalidationPlan{Rules: []string{"rule/v1"}}, SupersedesRevisionID: func() *string { x := "rev-0"; return &x }(), ControlSnapshots: intent.ControlSnapshots{CapabilityRegistryDigest: "cap", PolicyBundleDigest: "pol", LegalContextDigest: "legal", EntitlementDigest: "ent", ReferenceDataDigest: "ref", ClassificationTaxonomyDigest: "tax", DLPDecisionDigest: "dlp"}, CreatedBy: intent.PrincipalReference{PrincipalID: "p1", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "aal2"}, CreatedAt: created, InvalidatorRefs: []string{"reason"}, MaterialDigest: ref}
}

func TestFullProposalRoundTripPreservesCompleteMetadata(t *testing.T) {
	p := fullProposalFixture(t)
	p.CurrentState[0].CanonicalText = ""
	d, e := protomap.NewDefaultDigester()
	if e != nil {
		t.Fatal(e)
	}
	p.MaterialDigest, e = d.ProposalDigest(p)
	if e != nil {
		t.Fatal(e)
	}
	b, e := intentcontrol.EncodeFullProposal(p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := intentcontrol.DecodeFullProposal(b, d)
	if e != nil {
		t.Fatal(e)
	}
	if got.ProposalRevisionID != p.ProposalRevisionID || got.Writes[0].Operation != intent.WriteOperationUpdate || got.Writes[0].EffectiveInterval.Zone() != p.Writes[0].EffectiveInterval.Zone() || got.Cost.Amount().String() != p.Cost.Amount().String() {
		t.Fatalf("round trip lost metadata: %#v", got)
	}
	if !bytes.Equal(got.MaterialPayload().WireBytes, p.MaterialPayload().WireBytes) {
		t.Fatal("round trip changed complete material payload")
	}
	if got.CreatedAt != p.CreatedAt || got.ControlSnapshots != p.ControlSnapshots || !reflect.DeepEqual(got.InvalidatorRefs, p.InvalidatorRefs) {
		t.Fatalf("round trip changed non-material provenance: %#v", got)
	}
	if got.EffectiveTime.Calendar() != p.EffectiveTime.Calendar() || got.EffectiveTime.Zone() != p.EffectiveTime.Zone() || got.EffectiveTime.Disambiguation() != values.DisambiguationEarlier {
		t.Fatalf("round trip changed calendar/zone metadata: %#v", got.EffectiveTime)
	}
	if got.CurrentState[0].CanonicalText != "" {
		t.Fatalf("empty current canonical value changed to %q", got.CurrentState[0].CanonicalText)
	}
	missingValue := bytes.Replace(b, []byte(`,"CanonicalText":""`), nil, 1)
	if bytes.Equal(missingValue, b) {
		t.Fatal("fixture did not contain the empty canonical value")
	}
	if _, err := intentcontrol.DecodeFullProposal(missingValue, d); err == nil {
		t.Fatal("missing canonical value field was accepted")
	}
}
func TestFullProposalRejectsTamperDuplicateUnknownAndTrailing(t *testing.T) {
	p := fullProposalFixture(t)
	d, e := protomap.NewDefaultDigester()
	if e != nil {
		t.Fatal(e)
	}
	p.MaterialDigest, e = d.ProposalDigest(p)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := intentcontrol.EncodeFullProposal(p)
	tampered := bytes.Replace(b, []byte(`"IntentID":"intent-1"`), []byte(`"IntentID":"intent-2"`), 1)
	if _, e := intentcontrol.DecodeFullProposal(tampered, d); e == nil {
		t.Fatal("tamper accepted")
	}
	for _, x := range [][]byte{append(append([]byte{}, b...), []byte(` {}`)...), append([]byte(`{"SchemaVersion":1,"SchemaVersion":1}`), b[bytes.IndexByte(b, '}')+1:]...), append([]byte(`{"SchemaVersion":1,"Nope":1}`), b[bytes.IndexByte(b, '}')+1:]...)} {
		if _, e := intentcontrol.DecodeFullProposal(x, d); e == nil {
			t.Fatalf("invalid envelope accepted: %s", x)
		}
	}
	nestedUnknown := bytes.Replace(b, []byte(`"PrincipalID":"p1"`), []byte(`"PrincipalID":"p1","Nope":true`), 1)
	if _, e := intentcontrol.DecodeFullProposal(nestedUnknown, d); e == nil {
		t.Fatal("unknown nested field accepted")
	}
	nestedDuplicate := bytes.Replace(b, []byte(`"PrincipalID":"p1"`), []byte(`"PrincipalID":"p1","PrincipalID":"p2"`), 1)
	if _, e := intentcontrol.DecodeFullProposal(nestedDuplicate, d); e == nil {
		t.Fatal("duplicate nested field accepted")
	}
}

type nilMapVerifier map[string]string

func (nilMapVerifier) VerifyProposalDigest(intent.ProposalRevision) error {
	panic("typed nil verifier called")
}

type nilSliceVerifier []string

func (nilSliceVerifier) VerifyProposalDigest(intent.ProposalRevision) error {
	panic("typed nil verifier called")
}

type nilFuncVerifier func()

func (nilFuncVerifier) VerifyProposalDigest(intent.ProposalRevision) error {
	panic("typed nil verifier called")
}

type nilChanVerifier chan struct{}

func (nilChanVerifier) VerifyProposalDigest(intent.ProposalRevision) error {
	panic("typed nil verifier called")
}

type nilPointerVerifier struct{}

func (*nilPointerVerifier) VerifyProposalDigest(intent.ProposalRevision) error {
	panic("typed nil verifier called")
}

type rejectingVerifier struct{}

func (rejectingVerifier) VerifyProposalDigest(intent.ProposalRevision) error {
	return errors.New("reference mismatch")
}

func TestFullProposalDecoderBoundsAndVerifierForms(t *testing.T) {
	p := fullProposalFixture(t)
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatal(err)
	}
	p.MaterialDigest, err = d.ProposalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := intentcontrol.EncodeFullProposal(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, verifier := range map[string]intentcontrol.FullProposalVerifier{
		"nil interface": nil, "nil map": nilMapVerifier(nil), "nil slice": nilSliceVerifier(nil),
		"nil function": nilFuncVerifier(nil), "nil channel": nilChanVerifier(nil), "nil pointer": (*nilPointerVerifier)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := intentcontrol.DecodeFullProposal(b, verifier); err == nil || !strings.Contains(err.Error(), "verifier required") {
				t.Fatalf("typed nil verifier error = %v", err)
			}
		})
	}
	if _, err := intentcontrol.DecodeFullProposal(b, rejectingVerifier{}); err == nil || !strings.Contains(err.Error(), "reference mismatch") {
		t.Fatalf("full-reference refusal = %v", err)
	}
	tooLarge := bytes.Repeat([]byte{' '}, 4<<20+1)
	if _, err := intentcontrol.DecodeFullProposal(tooLarge, d); err == nil {
		t.Fatal("oversized envelope accepted")
	}
	deep := []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))
	if _, err := intentcontrol.DecodeFullProposal(deep, d); err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("deep envelope error = %v", err)
	}
}
