package legal

import (
	"fmt"
)

// EvaluationBinding prevents a valid evaluation receipt from being replayed
// for a different intent or proposal. It is signed by the same custody seam
// used for LegalContext and LegalEvaluationReceipt.
type EvaluationBinding struct {
	Tenant             string
	IntentID           string
	ProposalRevisionID string
	MaterialDigest     string
	ReceiptRef         string
	ReceiptDigest      string
	LegalContextDigest string
	AppliedObligations []BoundObligation
	Discharges         []ObligationDischarge
	Digest             string
	Signature          Signature
}

// BoundObligation is the stable identity of one applied receipt obligation.
type BoundObligation struct {
	Type       ObligationType
	ID         string
	BodyDigest string
}

// ObligationDischarge names the exact obligation and the evidence proving its
// terminal state. A bag of evidence references cannot establish which duty it
// discharges.
type ObligationDischarge struct {
	Obligation   BoundObligation
	EvidenceRefs []string
}

func SignEvaluationBinding(b EvaluationBinding, signer *Signer) (EvaluationBinding, error) {
	if signer == nil || b.validate() != nil {
		return EvaluationBinding{}, fmt.Errorf("%w: incomplete proposal binding", ErrReceiptInvalid)
	}
	var err error
	b.Digest, b.Signature, err = signer.SignDigestChecked(b.canonicalBytes())
	if err != nil {
		return EvaluationBinding{}, fmt.Errorf("%w: signing binding: %w", ErrReceiptInvalid, err)
	}
	return b, nil
}

func (b EvaluationBinding) VerifyWithKey(key []byte) error {
	if b.validate() != nil || len(key) != len(b.Signature.PublicKey) || string(key) != string(b.Signature.PublicKey) {
		return ErrReceiptInvalid
	}
	return VerifySignature(b.canonicalBytes(), b.Digest, b.Signature)
}

func (b EvaluationBinding) validate() error {
	if b.Tenant == "" || b.IntentID == "" || b.ProposalRevisionID == "" || b.MaterialDigest == "" || b.ReceiptRef == "" || b.ReceiptDigest == "" || b.LegalContextDigest == "" {
		return ErrReceiptInvalid
	}
	seen := make(map[BoundObligation]struct{}, len(b.AppliedObligations))
	for _, obligation := range b.AppliedObligations {
		if obligation.Type == ObligationTypeUnspecified || obligation.ID == "" || obligation.BodyDigest == "" {
			return ErrReceiptInvalid
		}
		if _, duplicate := seen[obligation]; duplicate {
			return ErrReceiptInvalid
		}
		seen[obligation] = struct{}{}
	}
	discharged := make(map[BoundObligation]struct{}, len(b.Discharges))
	for _, discharge := range b.Discharges {
		if _, ok := seen[discharge.Obligation]; !ok || len(discharge.EvidenceRefs) == 0 {
			return ErrReceiptInvalid
		}
		if _, duplicate := discharged[discharge.Obligation]; duplicate {
			return ErrReceiptInvalid
		}
		for _, ref := range discharge.EvidenceRefs {
			if ref == "" {
				return ErrReceiptInvalid
			}
		}
		discharged[discharge.Obligation] = struct{}{}
	}
	return nil
}

func (b EvaluationBinding) canonicalBytes() []byte {
	var out []byte
	out = appendField(out, "tenant", b.Tenant)
	out = appendField(out, "intent_id", b.IntentID)
	out = appendField(out, "proposal_revision_id", b.ProposalRevisionID)
	out = appendField(out, "material_digest", b.MaterialDigest)
	out = appendField(out, "receipt_ref", b.ReceiptRef)
	out = appendField(out, "receipt_digest", b.ReceiptDigest)
	out = appendField(out, "legal_context_digest", b.LegalContextDigest)
	out = appendField(out, "applied_obligation_count", fmt.Sprint(len(b.AppliedObligations)))
	for _, obligation := range b.AppliedObligations {
		out = appendBoundObligation(out, "applied", obligation)
	}
	for _, discharge := range b.Discharges {
		out = appendBoundObligation(out, "discharged", discharge.Obligation)
		for _, ref := range discharge.EvidenceRefs {
			out = appendField(out, "discharge_evidence_ref", ref)
		}
	}
	return out
}

func appendBoundObligation(out []byte, prefix string, obligation BoundObligation) []byte {
	out = appendField(out, prefix+"_type", obligation.Type.String())
	out = appendField(out, prefix+"_id", obligation.ID)
	return appendField(out, prefix+"_body_digest", obligation.BodyDigest)
}
