package attestationstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	trustattest "github.com/monstercameron/hcm-next/internal/trust/attest"
)

var _ trustattest.ResponseStore = (*Store)(nil)

// AppendResponse stores one immutable response or correction. Revision and
// digest are allocated here, so callers cannot forge chronology or content
// identity. Exact idempotency replays return the existing row.
func (s *Store) AppendResponse(ctx context.Context, in trustattest.Response) (trustattest.Response, error) {
	if in.Kind == "" {
		in.Kind = trustattest.AssertionResponse
	}
	if err := trustattest.ValidateResponseDraft(in); err != nil {
		return trustattest.Response{}, failure(CodeInvalid, "attestation_response", in.ResponseID, err)
	}
	var out trustattest.Response
	err := s.withTenant(ctx, in.Tenant, func(tx dbport.Tx) error {
		var existing trustattest.Response
		err := scanResponse(tx.QueryRow(ctx, `SELECT tenant_id,response_id,revision,statement_id,statement_version,statement_digest,binding_digest,status,kind,reason,evidence_receipt,idempotency_key,corrects_response_id,authority_ref,affected_obligations,transaction_id,trusted_at,trusted_time,digest,request_digest FROM attestation_response WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID(in.Tenant), in.IdempotencyKey), in.Tenant, &existing)
		if err == nil {
			if existing.RequestDigest != in.RequestDigest {
				return failure(CodeIdempotencyConflict, "attestation_response", in.IdempotencyKey, trustattest.ErrIdempotencyConflict)
			}
			existing.Replayed = true
			out = existing
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if in.RequestDigest == "" {
			return failure(CodeInvalid, "attestation_response", in.ResponseID, fmt.Errorf("request digest is required"))
		}
		latest, err := latestVersion(ctx, tx, tenantID(in.Tenant), "attestation_response", "response_id", "revision", in.ResponseID)
		if err != nil {
			return err
		}
		if latest > 0 && in.Kind == trustattest.AssertionResponse {
			return failure(CodeDuplicateRevision, "attestation_response", in.ResponseID, trustattest.ErrResponseAlreadyExists)
		}
		in.Revision = latest + 1
		// content_digest is stored as bare hex. Normalize the digest-bearing
		// response fields to their domain representation before deriving the
		// response digest so the value is stable after a storage round-trip.
		in.StatementDigest = domainDigest(in.StatementDigest)
		in.BindingDigest = domainDigest(in.BindingDigest)
		in.EvidenceReceipt = domainDigest(in.EvidenceReceipt)
		in.Digest = trustattest.DigestResponse(in)
		timeEvidence, err := json.Marshal(in.RecordedAt)
		if err != nil {
			return failure(CodeInvalid, "attestation_response", in.ResponseID, err)
		}
		obligations, err := json.Marshal(in.AffectedObligations)
		if err != nil {
			return failure(CodeInvalid, "attestation_response", in.ResponseID, err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO attestation_response (tenant_id,row_id,response_id,revision,statement_id,statement_version,statement_digest,binding_digest,status,kind,reason,evidence_receipt,idempotency_key,corrects_response_id,authority_ref,affected_obligations,transaction_id,trusted_at,trusted_time,request_digest,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, tenantID(in.Tenant), uuid.New(), in.ResponseID, int64(in.Revision), in.StatementID, int64(in.StatementVersion), storageDigest(in.StatementDigest), storageDigest(in.BindingDigest), string(in.Status), string(in.Kind), in.Reason, storageDigest(in.EvidenceReceipt), in.IdempotencyKey, nullableString(in.CorrectsResponseID), nullableString(in.Authority), obligations, nullableString(in.TransactionID), in.RecordedAt.At, timeEvidence, storageDigest(in.RequestDigest), storageDigest(in.Digest))
		if err != nil {
			return mapWriteError("attestation_response", in.ResponseID, err, trustattest.ErrIdempotencyConflict, CodeIdempotencyConflict)
		}
		out = in
		return nil
	})
	return out, err
}

// SaveResponse is a descriptive alias for AppendResponse.
func (s *Store) SaveResponse(ctx context.Context, in trustattest.Response) (trustattest.Response, error) {
	return s.AppendResponse(ctx, in)
}

// GetResponse loads one immutable response revision.
func (s *Store) GetResponse(ctx context.Context, tenant values.TenantId, id string, revision uint64) (trustattest.Response, error) {
	var out trustattest.Response
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		return scanResponse(tx.QueryRow(ctx, `SELECT tenant_id,response_id,revision,statement_id,statement_version,statement_digest,binding_digest,status,kind,reason,evidence_receipt,idempotency_key,corrects_response_id,authority_ref,affected_obligations,transaction_id,trusted_at,trusted_time,digest,request_digest FROM attestation_response WHERE tenant_id=$1 AND response_id=$2 AND revision=$3`, tenantID(tenant), id, int64(revision)), tenant, &out)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return trustattest.Response{}, failure(CodeNotFound, "attestation_response", id, trustattest.ErrResponseNotFound)
	}
	return out, err
}

// LoadResponse is a descriptive alias for GetResponse.
func (s *Store) LoadResponse(ctx context.Context, tenant values.TenantId, id string, revision uint64) (trustattest.Response, error) {
	return s.GetResponse(ctx, tenant, id, revision)
}

// GetResponseByIdempotency returns the immutable receipt for an exact replay.
func (s *Store) GetResponseByIdempotency(ctx context.Context, tenant values.TenantId, key string) (trustattest.Response, error) {
	var out trustattest.Response
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		return scanResponse(tx.QueryRow(ctx, `SELECT tenant_id,response_id,revision,statement_id,statement_version,statement_digest,binding_digest,status,kind,reason,evidence_receipt,idempotency_key,corrects_response_id,authority_ref,affected_obligations,transaction_id,trusted_at,trusted_time,digest,request_digest FROM attestation_response WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID(tenant), key), tenant, &out)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return trustattest.Response{}, trustattest.ErrResponseNotFound
	}
	return out, err
}

// ListResponseHistory returns all revisions for a logical response in order.
func (s *Store) ListResponseHistory(ctx context.Context, tenant values.TenantId, id string) ([]trustattest.Response, error) {
	var out []trustattest.Response
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,response_id,revision,statement_id,statement_version,statement_digest,binding_digest,status,kind,reason,evidence_receipt,idempotency_key,corrects_response_id,authority_ref,affected_obligations,transaction_id,trusted_at,trusted_time,digest,request_digest FROM attestation_response WHERE tenant_id=$1 AND response_id=$2 ORDER BY revision`, tenantID(tenant), id)
		if err != nil {
			return failure(CodeDatabase, "attestation_response", id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var row trustattest.Response
			if err := scanResponse(rows, tenant, &row); err != nil {
				return failure(CodeDatabase, "attestation_response", id, err)
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, failure(CodeNotFound, "attestation_response", id, trustattest.ErrResponseNotFound)
	}
	return out, nil
}

type responseScanner interface{ Scan(...any) error }

func scanResponse(row responseScanner, tenant values.TenantId, out *trustattest.Response) error {
	var (
		storedTenant                                 uuid.UUID
		version, statementVersion                    int64
		statementDigest, bindingDigest, status, kind string
		corrects, authority, transactionID           *string
		obligations, timeEvidence                    []byte
	)
	err := row.Scan(&storedTenant, &out.ResponseID, &version, &out.StatementID, &statementVersion, &statementDigest, &bindingDigest, &status, &kind, &out.Reason, &out.EvidenceReceipt, &out.IdempotencyKey, &corrects, &authority, &obligations, &transactionID, &out.RecordedAt.At, &timeEvidence, &out.Digest, &out.RequestDigest)
	if err != nil {
		return err
	}
	if storedTenant != tenantID(tenant) {
		return fmt.Errorf("response tenant does not match request")
	}
	if err := json.Unmarshal(timeEvidence, &out.RecordedAt); err != nil {
		return err
	}
	if err := json.Unmarshal(obligations, &out.AffectedObligations); err != nil {
		return err
	}
	out.Tenant = tenant
	out.Revision = uint64(version)
	out.StatementVersion = uint64(statementVersion)
	out.StatementDigest = domainDigest(statementDigest)
	out.BindingDigest = domainDigest(bindingDigest)
	out.EvidenceReceipt = domainDigest(out.EvidenceReceipt)
	out.Status = trustattest.ResponseStatus(status)
	out.Kind = trustattest.AssertionKind(kind)
	if corrects != nil {
		out.CorrectsResponseID = *corrects
	}
	if authority != nil {
		out.Authority = *authority
	}
	if transactionID != nil {
		out.TransactionID = *transactionID
	}
	if want := trustattest.DigestResponse(*out); storageDigest(want) != storageDigest(out.Digest) {
		return fmt.Errorf("stored response digest does not match rehydrated response")
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
