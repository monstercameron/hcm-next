package legalevidencestore

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	legal "github.com/monstercameron/hcm-next/internal/governance/legal"
)

// EvaluationReceiptEntry is the durable envelope for an offline-verifiable
// receipt. ReceiptRef is deliberately outside the signed receipt: it is the
// stable storage reference bound by EvaluationBinding.
type EvaluationReceiptEntry struct {
	TenantID   string
	Tenant     string
	ReceiptRef string
	Receipt    legal.LegalEvaluationReceipt
}

// EvaluationBindingEntry is the durable envelope for a proposal binding.
type EvaluationBindingEntry struct {
	TenantID string
	Binding  legal.EvaluationBinding
}

// EvidenceRequest is the data-plane equivalent of app.LegalEvidenceRequest.
// Keeping this type here avoids a data-to-application import cycle; the
// application adapter can translate its request into this value.
type EvidenceRequest struct {
	Tenant, IntentID, ProposalRevisionID, MaterialDigest, LegalContextDigest string
}

// Verifier authenticates both artifacts against an explicitly configured set
// of issuer keys. An embedded public key alone is never an authorization
// decision.
type Verifier struct {
	store           *Store
	keys            [][]byte
	authorizeTenant TenantAuthority
}

// TenantAuthority validates tenant against trusted outer request context.
type TenantAuthority func(context.Context, string) (uuid.UUID, error)

func (s *Store) WithTrustedIssuerKeys(authorize TenantAuthority, keys ...[]byte) *Verifier {
	trusted := make([][]byte, 0, len(keys))
	for _, key := range keys {
		trusted = append(trusted, slices.Clone(key))
	}
	return &Verifier{store: s, keys: trusted, authorizeTenant: authorize}
}

// NewVerifier constructs the application adapter seam with explicit issuer
// allowlisting. Passing no keys intentionally makes every verification fail.
func NewVerifier(s *Store, authorize TenantAuthority, keys ...[]byte) *Verifier {
	return s.WithTrustedIssuerKeys(authorize, keys...)
}

func trustedKey(sig legal.Signature, keys [][]byte) []byte {
	for _, key := range keys {
		if slices.Equal(key, sig.PublicKey) {
			return key
		}
	}
	return nil
}

func (v *Verifier) AppendEvaluationReceipt(ctx context.Context, in EvaluationReceiptEntry) error {
	tenant, err := parseUUID("tenant_id", in.TenantID)
	if err != nil || in.ReceiptRef == "" {
		if err == nil {
			err = fmt.Errorf("%w: receipt reference is required", ErrInvalid)
		}
		return err
	}
	if in.Tenant == "" {
		return fmt.Errorf("%w: signed tenant reference is required", ErrInvalid)
	}
	resolved, err := v.authorize(ctx, in.Tenant)
	if err != nil || resolved != tenant {
		return fmt.Errorf("%w: tenant authority", ErrInvalid)
	}
	key := trustedKey(in.Receipt.Signature, v.keys)
	if key == nil || in.Receipt.VerifyWithKey(key) != nil {
		return fmt.Errorf("%w: receipt authentication", ErrInvalid)
	}
	return v.store.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		blob, e := jsonValue(in.Receipt)
		if e != nil {
			return e
		}
		var existingDigest string
		var existing []byte
		if e = tx.QueryRow(ctx, `SELECT receipt_digest, receipt FROM legal_evaluation_receipt WHERE tenant_id=$1 AND receipt_ref=$2`, tenant, in.ReceiptRef).Scan(&existingDigest, &existing); e == nil {
			if existingDigest == in.Receipt.Digest {
				return nil
			}
			return fmt.Errorf("%w: legal_evaluation_receipt", ErrDuplicate)
		} else if !errors.Is(e, pgx.ErrNoRows) && !errors.Is(e, dbport.ErrNoRows) {
			return e
		}
		tag, e := tx.Exec(ctx, `INSERT INTO legal_evaluation_receipt
			(tenant_id, receipt_ref, receipt_digest, receipt, issuer_key)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, tenant, in.ReceiptRef, in.Receipt.Digest, blob, in.Receipt.Signature.PublicKey)
		if e != nil {
			return classifyWriteError("legal_evaluation_receipt", e)
		}
		if tag == 0 {
			var digest string
			var existing []byte
			if e = tx.QueryRow(ctx, `SELECT receipt_digest, receipt FROM legal_evaluation_receipt WHERE tenant_id=$1 AND receipt_ref=$2`, tenant, in.ReceiptRef).Scan(&digest, &existing); e != nil {
				if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, dbport.ErrNoRows) {
					return fmt.Errorf("%w: legal_evaluation_receipt", ErrDuplicate)
				}
				return e
			}
			if digest != in.Receipt.Digest {
				return fmt.Errorf("%w: legal_evaluation_receipt", ErrDuplicate)
			}
		}
		return nil
	})
}

func (v *Verifier) AppendEvaluationBinding(ctx context.Context, in EvaluationBindingEntry) error {
	tenant, err := parseUUID("tenant_id", in.TenantID)
	if err != nil {
		return err
	}
	resolved, err := v.authorize(ctx, in.Binding.Tenant)
	if err != nil || resolved != tenant {
		return fmt.Errorf("%w: tenant authority", ErrInvalid)
	}
	key := trustedKey(in.Binding.Signature, v.keys)
	if key == nil || in.Binding.VerifyWithKey(key) != nil {
		return fmt.Errorf("%w: binding authentication", ErrInvalid)
	}
	return v.store.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		blob, e := jsonValue(in.Binding)
		if e != nil {
			return e
		}
		var existing []byte
		if e = tx.QueryRow(ctx, `SELECT binding FROM legal_evaluation_binding WHERE tenant_id=$1 AND binding_digest=$2`, tenant, in.Binding.Digest).Scan(&existing); e == nil {
			if len(existing) > 0 {
				return nil
			}
			return fmt.Errorf("%w: legal_evaluation_binding", ErrDuplicate)
		} else if !errors.Is(e, pgx.ErrNoRows) && !errors.Is(e, dbport.ErrNoRows) {
			return e
		}
		tag, e := tx.Exec(ctx, `INSERT INTO legal_evaluation_binding
			(tenant_id, binding_digest, receipt_ref, receipt_digest, intent_id,
			 proposal_revision_id, material_digest, legal_context_digest, binding, issuer_key)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, tenant, in.Binding.Digest,
			in.Binding.ReceiptRef, in.Binding.ReceiptDigest, in.Binding.IntentID,
			in.Binding.ProposalRevisionID, in.Binding.MaterialDigest, in.Binding.LegalContextDigest,
			blob, in.Binding.Signature.PublicKey)
		if e != nil {
			return classifyWriteError("legal_evaluation_binding", e)
		}
		if tag == 0 {
			var existing []byte
			if e = tx.QueryRow(ctx, `SELECT binding FROM legal_evaluation_binding WHERE tenant_id=$1 AND binding_digest=$2`, tenant, in.Binding.Digest).Scan(&existing); e != nil {
				if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, dbport.ErrNoRows) {
					return fmt.Errorf("%w: legal_evaluation_binding", ErrDuplicate)
				}
				return e
			}
			if string(existing) != string(blob) {
				return fmt.Errorf("%w: legal_evaluation_binding", ErrDuplicate)
			}
		}
		return nil
	})
}

func (v *Verifier) VerifyLegalEvidence(ctx context.Context, req EvidenceRequest) (legal.EvaluationBinding, error) {
	if v == nil || v.store == nil || v.authorizeTenant == nil || req.Tenant == "" || req.IntentID == "" || req.ProposalRevisionID == "" || req.MaterialDigest == "" || req.LegalContextDigest == "" || len(v.keys) == 0 {
		return legal.EvaluationBinding{}, fmt.Errorf("%w: incomplete trusted evidence request", ErrInvalid)
	}
	tenant, err := v.authorizeTenant(ctx, req.Tenant)
	if err != nil || tenant == uuid.Nil {
		return legal.EvaluationBinding{}, fmt.Errorf("%w: tenant authority: %v", ErrInvalid, err)
	}
	var out legal.EvaluationBinding
	err = v.store.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var bindingBlob []byte
		err := tx.QueryRow(ctx, `SELECT binding FROM legal_evaluation_binding
			WHERE tenant_id=$1 AND intent_id=$2 AND proposal_revision_id=$3
			AND material_digest=$4 AND legal_context_digest=$5`, tenant, req.IntentID,
			req.ProposalRevisionID, req.MaterialDigest, req.LegalContextDigest).Scan(&bindingBlob)
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := unmarshalJSON(bindingBlob, &out); err != nil {
			return fmt.Errorf("%w: decode binding: %v", ErrInvalid, err)
		}
		key := trustedKey(out.Signature, v.keys)
		if key == nil || out.VerifyWithKey(key) != nil || out.Tenant != req.Tenant || out.IntentID != req.IntentID || out.ProposalRevisionID != req.ProposalRevisionID || out.MaterialDigest != req.MaterialDigest || out.LegalContextDigest != req.LegalContextDigest {
			return fmt.Errorf("%w: binding mismatch or untrusted issuer", ErrInvalid)
		}
		var receiptBlob []byte
		if err := tx.QueryRow(ctx, `SELECT receipt FROM legal_evaluation_receipt WHERE tenant_id=$1 AND receipt_ref=$2`, tenant, out.ReceiptRef).Scan(&receiptBlob); err != nil {
			if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var receipt legal.LegalEvaluationReceipt
		if err := unmarshalJSON(receiptBlob, &receipt); err != nil {
			return fmt.Errorf("%w: decode receipt: %v", ErrInvalid, err)
		}
		key = trustedKey(receipt.Signature, v.keys)
		if key == nil || receipt.VerifyWithKey(key) != nil || receipt.Digest != out.ReceiptDigest || receipt.LegalContextDigest != req.LegalContextDigest {
			return fmt.Errorf("%w: receipt mismatch or untrusted issuer", ErrInvalid)
		}
		if len(out.AppliedObligations) != len(receipt.ObligationsApplied) {
			return fmt.Errorf("%w: binding obligation set differs from receipt", ErrInvalid)
		}
		for i, obligation := range receipt.ObligationsApplied {
			if out.AppliedObligations[i] != (legal.BoundObligation{Type: obligation.Type, ID: obligation.ID, BodyDigest: obligation.BodyDigest}) {
				return fmt.Errorf("%w: binding obligation set differs from receipt", ErrInvalid)
			}
		}
		return nil
	})
	if err != nil {
		return legal.EvaluationBinding{}, err
	}
	return out, nil
}

func (v *Verifier) authorize(ctx context.Context, tenant string) (uuid.UUID, error) {
	if v == nil || v.store == nil || v.authorizeTenant == nil || len(v.keys) == 0 {
		return uuid.Nil, fmt.Errorf("%w: verifier configuration", ErrInvalid)
	}
	resolved, err := v.authorizeTenant(ctx, tenant)
	if err != nil || resolved == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: tenant authority: %v", ErrInvalid, err)
	}
	return resolved, nil
}
