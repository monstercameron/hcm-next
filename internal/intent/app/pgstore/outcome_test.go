package pgstore

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/app"
)

func TestOutcomeBinderContract(t *testing.T) {
	var _ app.OutcomeBinder = (*Store)(nil)
	if StreamKey("intent-1") != "intent:intent-1" {
		t.Fatalf("StreamKey contract changed")
	}
}

func TestTodo_LEGAL014_ReplayBindingRequiresExactEvidence(t *testing.T) {
	obligation := intent.LegalBoundObligation{Type: "NOTICE", ID: "notice", BodyDigest: "body"}
	e := &intent.LegalObligationEvidence{ReceiptRef: "receipt", ReceiptDigest: "rd", BindingDigest: "bd", ProposalRevisionID: "proposal", MaterialDigest: "material", AppliedObligations: []intent.LegalBoundObligation{obligation}, Discharges: []intent.LegalObligationDischarge{{Obligation: obligation, EvidenceRefs: []string{"discharge"}}}}
	applied, err := json.Marshal(e.AppliedObligations)
	if err != nil {
		t.Fatal(err)
	}
	discharges, err := json.Marshal(e.Discharges)
	if err != nil {
		t.Fatal(err)
	}
	p := func(v string) *string { return &v }
	if !sameLegalEvidence(e, p("receipt"), p("rd"), p("bd"), p("proposal"), p("material"), applied, discharges) {
		t.Fatal("exact binding was not accepted")
	}
	if sameLegalEvidence(e, p("receipt"), p("rd"), p("bd"), p("other-proposal"), p("material"), applied, discharges) {
		t.Fatal("cross-proposal replay accepted")
	}
	if sameLegalEvidence(e, p("receipt"), p("rd"), p("bd"), p("proposal"), p("changed-material"), applied, discharges) {
		t.Fatal("changed-material replay accepted")
	}
	if sameLegalEvidence(e, p("receipt"), p("rd"), p("bd"), p("proposal"), p("material"), applied, []byte(`[{"EvidenceRefs":["forged"]}]`)) {
		t.Fatal("forged discharge evidence accepted")
	}
}
