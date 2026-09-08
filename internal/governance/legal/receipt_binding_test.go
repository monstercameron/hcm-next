package legal

import "testing"

func TestEvaluationBindingRejectsUntrustedKeyAndUnboundDischarge(t *testing.T) {
	signer := fixedSigner(t, 0x31)
	obligation := BoundObligation{Type: ObligationTypePayFrequency, ID: "pay-frequency", BodyDigest: "body"}
	binding, err := SignEvaluationBinding(EvaluationBinding{Tenant: "tenant", IntentID: "intent", ProposalRevisionID: "proposal", MaterialDigest: "material", ReceiptRef: "receipt", ReceiptDigest: "receipt-digest", LegalContextDigest: "context", AppliedObligations: []BoundObligation{obligation}, Discharges: []ObligationDischarge{{Obligation: obligation, EvidenceRefs: []string{"evidence"}}}}, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := binding.VerifyWithKey(fixedSigner(t, 0x32).PublicKey()); err == nil {
		t.Fatal("untrusted issuer key verified")
	}
	binding.Discharges[0].Obligation.ID = "another-duty"
	if _, err := SignEvaluationBinding(binding, signer); err == nil {
		t.Fatal("discharge for an unapplied obligation signed")
	}
}
