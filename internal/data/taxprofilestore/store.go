// Package taxprofilestore persists the immutable tax-profile revisions from
// internal/domains/taxprofile over migration 00124.
package taxprofilestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/taxprofile"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the transaction-opening capability required by Store.
type DB interface{ dbport.Beginner }

// Store implements [taxprofile.Store]. Each public operation scopes its
// transaction with tenancy.WithTenant before it reads or writes a row.
type Store struct{ db DB }

var _ taxprofile.Store = (*Store)(nil)

// New returns a PostgreSQL-backed tax-profile store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenant string, fn func(dbport.Tx, uuid.UUID) error) error {
	if ctx == nil {
		return storeError(taxprofile.StoreCodeInvalid, "context is required")
	}
	tenantID, err := uuid.Parse(tenant)
	if err != nil || tenantID == uuid.Nil {
		return storeError(taxprofile.StoreCodeInvalid, "tenant must be a non-nil UUID")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("taxprofilestore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("taxprofilestore: commit transaction: %w", err)
	}
	return nil
}

func storeError(code taxprofile.StoreCode, detail string) error {
	return &taxprofile.StoreError{Code: code, Detail: detail}
}

func invalid(detail string) error   { return storeError(taxprofile.StoreCodeInvalid, detail) }
func notFound(detail string) error  { return storeError(taxprofile.StoreCodeNotFound, detail) }
func duplicate(detail string) error { return storeError(taxprofile.StoreCodeDuplicate, detail) }

func stale(detail string, expected, actual uint64) error {
	return &taxprofile.StoreError{Code: taxprofile.StoreCodeStaleCAS, Detail: detail, Expected: expected, Actual: actual}
}

func mapInsertError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return duplicate(operation)
	}
	return fmt.Errorf("taxprofilestore: %s: %w", operation, err)
}

func workerID(ref string) (uuid.UUID, error) {
	id, err := uuid.Parse(ref)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid("worker_ref must be a non-nil UUID")
	}
	return id, nil
}

func storedDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func instantBounds(interval values.EffectiveInterval) (time.Time, error) {
	if err := interval.Validate(); err != nil {
		return time.Time{}, invalid("effective interval: " + err.Error())
	}
	start, ok := interval.StartInstant()
	if !ok {
		return time.Time{}, invalid("effective interval must use an instant")
	}
	if _, ok := interval.EndInstant(); ok {
		return time.Time{}, invalid("effective interval end is not representable by migration 00124")
	}
	return start.Time(), nil
}

func openInterval(at time.Time) (values.EffectiveInterval, error) {
	return values.NewOpenInstantInterval(values.NewInstant(at.UTC()))
}

func nullableUint64(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func scanUint64(value *int64) (uint64, error) {
	if *value < 1 {
		return 0, fmt.Errorf("revision must be positive")
	}
	return uint64(*value), nil
}

// SaveProfile stores one profile revision and its nested immutable values in
// one transaction. Existing identical child identities are left untouched so
// callers may ingest the children before their containing profile.
func (s *Store) SaveProfile(ctx context.Context, tenant string, profile taxprofile.WorkerTaxProfileRevision) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.saveProfileTx(ctx, tx, tenantID, profile)
	})
}

func (s *Store) saveProfileTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, profile taxprofile.WorkerTaxProfileRevision) error {
	if profile.CanonicalDigest == "" {
		var err error
		profile, err = taxprofile.NewWorkerTaxProfileRevision(profile)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := profile.Validate(); err != nil {
		return invalid(err.Error())
	}
	worker, err := workerID(profile.WorkerRef)
	if err != nil {
		return err
	}
	effectiveFrom, err := instantBounds(profile.Effective)
	if err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM worker_tax_profile_revision
		WHERE tenant_id=$1 AND worker_ref=$2 AND revision=$3)`, tenantID, worker, int64(profile.Revision)).Scan(&exists); err != nil {
		return fmt.Errorf("taxprofilestore: check profile identity: %w", err)
	}
	if exists {
		return duplicate("worker tax profile revision")
	}
	var latest int64
	err = tx.QueryRow(ctx, `SELECT revision FROM worker_tax_profile_revision
		WHERE tenant_id=$1 AND worker_ref=$2 ORDER BY revision DESC LIMIT 1`, tenantID, worker).Scan(&latest)
	if errors.Is(err, dbport.ErrNoRows) {
		if profile.Revision != 1 || profile.ParentRevision != 0 || profile.ParentDigest != "" {
			return stale("initial profile must be revision 1 without a parent", 0, profile.Revision)
		}
	} else if err != nil {
		return fmt.Errorf("taxprofilestore: inspect profile chain: %w", err)
	} else if profile.Revision != uint64(latest)+1 || profile.ParentRevision != uint64(latest) {
		return stale("profile does not extend the current revision", uint64(latest), profile.Revision)
	} else {
		var parentDigest string
		if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM worker_tax_profile_revision
			WHERE tenant_id=$1 AND worker_ref=$2 AND revision=$3`, tenantID, worker, latest).Scan(&parentDigest); err != nil {
			return fmt.Errorf("taxprofilestore: read profile predecessor: %w", err)
		}
		if storedDigest(profile.ParentDigest) != parentDigest {
			return stale("profile parent digest is stale", uint64(latest), profile.Revision)
		}
	}
	residence, _ := json.Marshal(profile.ResidenceJurisdictions)
	work, _ := json.Marshal(profile.WorkJurisdictions)
	_, err = tx.Exec(ctx, `INSERT INTO worker_tax_profile_revision
		(tenant_id,row_id,worker_ref,revision,parent_revision,parent_digest,
		 residence_jurisdictions,work_jurisdictions,filing_status,classification,
		 classification_evidence_ref,effective_from,known_at,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13,$14)`,
		tenantID, uuid.New(), worker, int64(profile.Revision), nullableUint64(profile.ParentRevision),
		nilIfEmpty(storedDigest(profile.ParentDigest)), residence, work, string(profile.FilingStatus),
		string(profile.Classification), profile.ClassificationEvidenceRef, effectiveFrom, profile.KnownAt.Time(),
		storedDigest(profile.CanonicalDigest))
	if err != nil {
		return mapInsertError("insert worker tax profile revision", err)
	}
	for _, registration := range profile.Registrations {
		if err := s.saveRegistrationTx(ctx, tx, tenantID, registration, true); err != nil {
			return err
		}
	}
	for _, election := range profile.Elections {
		if err := s.saveElectionTx(ctx, tx, tenantID, election, true); err != nil {
			return err
		}
	}
	for _, exemption := range profile.Exemptions {
		if err := s.saveExemptionTx(ctx, tx, tenantID, exemption, true); err != nil {
			return err
		}
	}
	return nil
}

// LoadProfile loads one profile revision and the current matching child
// revisions that make its domain value complete.
func (s *Store) LoadProfile(ctx context.Context, tenant, workerRef string, revision uint64) (taxprofile.WorkerTaxProfileRevision, error) {
	var out taxprofile.WorkerTaxProfileRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		worker, err := workerID(workerRef)
		if err != nil {
			return err
		}
		var (
			storedRevision                   int64
			parentRevision                   *int64
			parentDigest                     *string
			residenceJSON, workJSON          []byte
			filing, classification, evidence string
			effectiveFrom, knownAt           time.Time
		)
		err = tx.QueryRow(ctx, `SELECT revision,parent_revision,parent_digest,
			residence_jurisdictions,work_jurisdictions,filing_status,classification,
			classification_evidence_ref,effective_from,known_at,canonical_digest
			FROM worker_tax_profile_revision
			WHERE tenant_id=$1 AND worker_ref=$2 AND revision=$3`, tenantID, worker, int64(revision)).Scan(
			&storedRevision, &parentRevision, &parentDigest, &residenceJSON, &workJSON, &filing,
			&classification, &evidence, &effectiveFrom, &knownAt, &out.CanonicalDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("worker tax profile revision")
		}
		if err != nil {
			return fmt.Errorf("taxprofilestore: load worker tax profile revision: %w", err)
		}
		if err := json.Unmarshal(residenceJSON, &out.ResidenceJurisdictions); err != nil {
			return fmt.Errorf("taxprofilestore: decode residence jurisdictions: %w", err)
		}
		if err := json.Unmarshal(workJSON, &out.WorkJurisdictions); err != nil {
			return fmt.Errorf("taxprofilestore: decode work jurisdictions: %w", err)
		}
		out.WorkerRef, out.Revision, out.FilingStatus = workerRef, uint64(storedRevision), taxprofile.FilingStatus(filing)
		out.Classification, out.ClassificationEvidenceRef = taxprofile.TaxClassification(classification), evidence
		out.ParentRevision = uint64Ptr(parentRevision)
		out.ParentDigest = domainDigest(derefString(parentDigest))
		out.CanonicalDigest = domainDigest(out.CanonicalDigest)
		out.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		out.KnownAt = values.NewInstant(knownAt.UTC())
		if err := s.loadProfileChildren(ctx, tx, tenantID, worker, &out); err != nil {
			return err
		}
		if err := out.Validate(); err != nil {
			return fmt.Errorf("taxprofilestore: stored profile is invalid: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) loadProfileChildren(ctx context.Context, tx dbport.Tx, tenantID, worker uuid.UUID, out *taxprofile.WorkerTaxProfileRevision) error {
	jurisdictions := append(append([]string(nil), out.ResidenceJurisdictions...), out.WorkJurisdictions...)
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (registration_id) registration_id,jurisdiction,authority_ref,revision,
		parent_revision,parent_digest,effective_from,known_at,raw_registration_id,canonical_digest
		FROM tax_registration_revision WHERE tenant_id=$1 AND jurisdiction = ANY($2::text[])
		ORDER BY registration_id,revision DESC`, tenantID, jurisdictions)
	if err != nil {
		return fmt.Errorf("taxprofilestore: list profile registrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, jurisdiction                     string
			authority, raw, parentDigest, digest *string
			revision                             int64
			parentRevision                       *int64
			effectiveFrom, knownAt               time.Time
		)
		if err := rows.Scan(&id, &jurisdiction, &authority, &revision, &parentRevision, &parentDigest, &effectiveFrom, &knownAt, &raw, &digest); err != nil {
			return fmt.Errorf("taxprofilestore: scan profile registration: %w", err)
		}
		if raw != nil && *raw != "" {
			return invalid("stored raw registration id is prohibited")
		}
		registration := taxprofile.TaxRegistrationRevision{RegistrationIDRef: id, Jurisdiction: jurisdiction, AuthorityRef: derefString(authority), Revision: uint64(revision), ParentRevision: uint64Ptr(parentRevision), ParentDigest: domainDigest(derefString(parentDigest)), CanonicalDigest: domainDigest(derefString(digest)), KnownAt: values.NewInstant(knownAt.UTC())}
		registration.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		out.Registrations = append(out.Registrations, registration)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("taxprofilestore: list profile registrations: %w", err)
	}
	if err := loadElections(ctx, tx, tenantID, worker, out); err != nil {
		return err
	}
	return loadExemptions(ctx, tx, tenantID, worker, out)
}

func loadElections(ctx context.Context, tx dbport.Tx, tenantID, worker uuid.UUID, out *taxprofile.WorkerTaxProfileRevision) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (election_id) election_id,jurisdiction,kind,
		form_revision_ref,evidence_ref,amount,effective_from,known_at,canonical_digest
		FROM withholding_election_revision WHERE tenant_id=$1 AND worker_ref=$2
		ORDER BY election_id,effective_from DESC`, tenantID, worker)
	if err != nil {
		return fmt.Errorf("taxprofilestore: list profile elections: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var election taxprofile.WithholdingElectionRevision
		var amount *string
		var effectiveFrom, knownAt time.Time
		if err := rows.Scan(&election.ElectionID, &election.Jurisdiction, &election.Kind, &election.FormRevisionRef, &election.EvidenceRef, &amount, &effectiveFrom, &knownAt, &election.CanonicalDigest); err != nil {
			return fmt.Errorf("taxprofilestore: scan profile election: %w", err)
		}
		election.WorkerRef = worker.String()
		election.CanonicalDigest = domainDigest(election.CanonicalDigest)
		var err error
		election.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		election.KnownAt = values.NewInstant(knownAt.UTC())
		election.Amount, err = decimalFromStored(amount)
		if err != nil {
			return err
		}
		out.Elections = append(out.Elections, election)
	}
	return rows.Err()
}

func loadExemptions(ctx context.Context, tx dbport.Tx, tenantID, worker uuid.UUID, out *taxprofile.WorkerTaxProfileRevision) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (exemption_id) exemption_id,jurisdiction,kind,
		evidence_refs,expires_at,effective_from,known_at,canonical_digest
		FROM tax_exemption_revision WHERE tenant_id=$1 AND worker_ref=$2
		ORDER BY exemption_id,effective_from DESC`, tenantID, worker)
	if err != nil {
		return fmt.Errorf("taxprofilestore: list profile exemptions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var exemption taxprofile.TaxExemptionRevision
		var evidenceJSON []byte
		var expiresAt, effectiveFrom, knownAt time.Time
		if err := rows.Scan(&exemption.ExemptionID, &exemption.Jurisdiction, &exemption.Kind, &evidenceJSON, &expiresAt, &effectiveFrom, &knownAt, &exemption.CanonicalDigest); err != nil {
			return fmt.Errorf("taxprofilestore: scan profile exemption: %w", err)
		}
		if err := json.Unmarshal(evidenceJSON, &exemption.EvidenceRefs); err != nil {
			return fmt.Errorf("taxprofilestore: decode exemption evidence: %w", err)
		}
		var err error
		exemption.WorkerRef = worker.String()
		exemption.CanonicalDigest = domainDigest(exemption.CanonicalDigest)
		exemption.ExpiresAt = values.NewInstant(expiresAt.UTC())
		exemption.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		exemption.KnownAt = values.NewInstant(knownAt.UTC())
		out.Exemptions = append(out.Exemptions, exemption)
	}
	return rows.Err()
}

// SaveRegistration stores one chained registration revision.
func (s *Store) SaveRegistration(ctx context.Context, tenant string, registration taxprofile.TaxRegistrationRevision) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.saveRegistrationTx(ctx, tx, tenantID, registration, false)
	})
}

func (s *Store) saveRegistrationTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, registration taxprofile.TaxRegistrationRevision, ignoreDuplicate bool) error {
	if registration.CanonicalDigest == "" {
		var err error
		registration, err = taxprofile.NewTaxRegistrationRevision(registration)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := registration.Validate(); err != nil {
		return invalid(err.Error())
	}
	effectiveFrom, err := instantBounds(registration.Effective)
	if err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tax_registration_revision
		WHERE tenant_id=$1 AND registration_id=$2 AND revision=$3)`, tenantID, registration.RegistrationIDRef, int64(registration.Revision)).Scan(&exists); err != nil {
		return fmt.Errorf("taxprofilestore: check registration identity: %w", err)
	}
	if exists {
		if ignoreDuplicate {
			return nil
		}
		return duplicate("tax registration revision")
	}
	var latest int64
	err = tx.QueryRow(ctx, `SELECT revision FROM tax_registration_revision
		WHERE tenant_id=$1 AND registration_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, registration.RegistrationIDRef).Scan(&latest)
	if errors.Is(err, dbport.ErrNoRows) {
		if registration.Revision != 1 || registration.ParentRevision != 0 || registration.ParentDigest != "" {
			return stale("initial registration must be revision 1 without a parent", 0, registration.Revision)
		}
	} else if err != nil {
		return fmt.Errorf("taxprofilestore: inspect registration chain: %w", err)
	} else if registration.Revision != uint64(latest)+1 || registration.ParentRevision != uint64(latest) {
		return stale("registration does not extend the current revision", uint64(latest), registration.Revision)
	} else {
		var parentDigest string
		if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM tax_registration_revision
			WHERE tenant_id=$1 AND registration_id=$2 AND revision=$3`, tenantID, registration.RegistrationIDRef, latest).Scan(&parentDigest); err != nil {
			return fmt.Errorf("taxprofilestore: read registration predecessor: %w", err)
		}
		if storedDigest(registration.ParentDigest) != parentDigest {
			return stale("registration parent digest is stale", uint64(latest), registration.Revision)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO tax_registration_revision
		(tenant_id,row_id,registration_id,jurisdiction,authority_ref,revision,parent_revision,
		 parent_digest,effective_from,known_at,raw_registration_id,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL,$11)`, tenantID, uuid.New(), registration.RegistrationIDRef,
		registration.Jurisdiction, nilIfEmpty(registration.AuthorityRef), int64(registration.Revision), nullableUint64(registration.ParentRevision),
		nilIfEmpty(storedDigest(registration.ParentDigest)), effectiveFrom, registration.KnownAt.Time(), storedDigest(registration.CanonicalDigest))
	if err != nil {
		return mapInsertError("insert tax registration revision", err)
	}
	return nil
}

// LoadRegistration loads one registration revision.
func (s *Store) LoadRegistration(ctx context.Context, tenant, registrationID string, revision uint64) (taxprofile.TaxRegistrationRevision, error) {
	var out taxprofile.TaxRegistrationRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var parentRevision *int64
		var parentDigest, authority, raw, digest *string
		var storedRevision int64
		var effectiveFrom, knownAt time.Time
		err := tx.QueryRow(ctx, `SELECT jurisdiction,authority_ref,revision,parent_revision,parent_digest,
			effective_from,known_at,raw_registration_id,canonical_digest FROM tax_registration_revision
			WHERE tenant_id=$1 AND registration_id=$2 AND revision=$3`, tenantID, registrationID, int64(revision)).Scan(
			&out.Jurisdiction, &authority, &storedRevision, &parentRevision, &parentDigest, &effectiveFrom, &knownAt, &raw, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("tax registration revision")
		}
		if err != nil {
			return fmt.Errorf("taxprofilestore: load tax registration revision: %w", err)
		}
		if raw != nil && *raw != "" {
			return invalid("stored raw registration id is prohibited")
		}
		out.RegistrationIDRef, out.AuthorityRef, out.Revision = registrationID, derefString(authority), uint64(storedRevision)
		out.ParentRevision, out.ParentDigest = uint64Ptr(parentRevision), domainDigest(derefString(parentDigest))
		out.CanonicalDigest, out.KnownAt = domainDigest(derefString(digest)), values.NewInstant(knownAt.UTC())
		out.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		return out.Validate()
	})
	return out, err
}

// SaveElection stores one immutable election revision.
func (s *Store) SaveElection(ctx context.Context, tenant string, election taxprofile.WithholdingElectionRevision) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.saveElectionTx(ctx, tx, tenantID, election, false)
	})
}

func (s *Store) saveElectionTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, election taxprofile.WithholdingElectionRevision, ignoreDuplicate bool) error {
	if election.CanonicalDigest == "" {
		var err error
		election, err = taxprofile.NewWithholdingElectionRevision(election)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := election.Validate(); err != nil {
		return invalid(err.Error())
	}
	worker, err := workerID(election.WorkerRef)
	if err != nil {
		return err
	}
	effectiveFrom, err := instantBounds(election.Effective)
	if err != nil {
		return err
	}
	amount := nilIfZero(election.Amount)
	affected, err := tx.Exec(ctx, `INSERT INTO withholding_election_revision
		(tenant_id,row_id,election_id,worker_ref,jurisdiction,kind,form_revision_ref,evidence_ref,
		 amount,effective_from,known_at,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (tenant_id,election_id,effective_from) DO NOTHING`, tenantID, uuid.New(), election.ElectionID, worker,
		election.Jurisdiction, string(election.Kind), nilIfEmpty(election.FormRevisionRef), nilIfEmpty(election.EvidenceRef), amount,
		effectiveFrom, election.KnownAt.Time(), storedDigest(election.CanonicalDigest))
	if err != nil {
		return mapInsertError("insert withholding election revision", err)
	}
	if affected == 0 && !ignoreDuplicate {
		return duplicate("withholding election revision")
	}
	return nil
}

// LoadElection loads the latest effective election revision for an identity.
func (s *Store) LoadElection(ctx context.Context, tenant, electionID string) (taxprofile.WithholdingElectionRevision, error) {
	var out taxprofile.WithholdingElectionRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var worker uuid.UUID
		var amount *string
		var effectiveFrom, knownAt time.Time
		err := tx.QueryRow(ctx, `SELECT worker_ref,jurisdiction,kind,form_revision_ref,evidence_ref,amount,
			effective_from,known_at,canonical_digest FROM withholding_election_revision
			WHERE tenant_id=$1 AND election_id=$2 ORDER BY effective_from DESC LIMIT 1`, tenantID, electionID).Scan(
			&worker, &out.Jurisdiction, &out.Kind, &out.FormRevisionRef, &out.EvidenceRef, &amount, &effectiveFrom, &knownAt, &out.CanonicalDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("withholding election revision")
		}
		if err != nil {
			return fmt.Errorf("taxprofilestore: load withholding election revision: %w", err)
		}
		out.ElectionID, out.WorkerRef = electionID, worker.String()
		out.CanonicalDigest = domainDigest(out.CanonicalDigest)
		out.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		out.KnownAt = values.NewInstant(knownAt.UTC())
		out.Amount, err = decimalFromStored(amount)
		if err != nil {
			return err
		}
		return out.Validate()
	})
	return out, err
}

// SaveExemption stores one immutable exemption revision.
func (s *Store) SaveExemption(ctx context.Context, tenant string, exemption taxprofile.TaxExemptionRevision) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.saveExemptionTx(ctx, tx, tenantID, exemption, false)
	})
}

func (s *Store) saveExemptionTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, exemption taxprofile.TaxExemptionRevision, ignoreDuplicate bool) error {
	if exemption.CanonicalDigest == "" {
		var err error
		exemption, err = taxprofile.NewTaxExemptionRevision(exemption)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := exemption.Validate(); err != nil {
		return invalid(err.Error())
	}
	worker, err := workerID(exemption.WorkerRef)
	if err != nil {
		return err
	}
	effectiveFrom, err := instantBounds(exemption.Effective)
	if err != nil {
		return err
	}
	evidence, _ := json.Marshal(exemption.EvidenceRefs)
	affected, err := tx.Exec(ctx, `INSERT INTO tax_exemption_revision
		(tenant_id,row_id,exemption_id,worker_ref,jurisdiction,kind,evidence_refs,expires_at,
		effective_from,known_at,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11)
		ON CONFLICT (tenant_id,exemption_id,effective_from) DO NOTHING`, tenantID, uuid.New(), exemption.ExemptionID, worker,
		exemption.Jurisdiction, string(exemption.Kind), evidence, exemption.ExpiresAt.Time(), effectiveFrom, exemption.KnownAt.Time(), storedDigest(exemption.CanonicalDigest))
	if err != nil {
		return mapInsertError("insert tax exemption revision", err)
	}
	if affected == 0 && !ignoreDuplicate {
		return duplicate("tax exemption revision")
	}
	return nil
}

// LoadExemption loads the latest effective exemption revision for an identity.
func (s *Store) LoadExemption(ctx context.Context, tenant, exemptionID string) (taxprofile.TaxExemptionRevision, error) {
	var out taxprofile.TaxExemptionRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var worker uuid.UUID
		var evidenceJSON []byte
		var expiresAt, effectiveFrom, knownAt time.Time
		err := tx.QueryRow(ctx, `SELECT worker_ref,jurisdiction,kind,evidence_refs,expires_at,effective_from,
			known_at,canonical_digest FROM tax_exemption_revision
			WHERE tenant_id=$1 AND exemption_id=$2 ORDER BY effective_from DESC LIMIT 1`, tenantID, exemptionID).Scan(
			&worker, &out.Jurisdiction, &out.Kind, &evidenceJSON, &expiresAt, &effectiveFrom, &knownAt, &out.CanonicalDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("tax exemption revision")
		}
		if err != nil {
			return fmt.Errorf("taxprofilestore: load tax exemption revision: %w", err)
		}
		if err := json.Unmarshal(evidenceJSON, &out.EvidenceRefs); err != nil {
			return fmt.Errorf("taxprofilestore: decode tax exemption evidence: %w", err)
		}
		out.ExemptionID, out.WorkerRef = exemptionID, worker.String()
		out.CanonicalDigest = domainDigest(out.CanonicalDigest)
		out.ExpiresAt = values.NewInstant(expiresAt.UTC())
		out.Effective, err = openInterval(effectiveFrom)
		if err != nil {
			return err
		}
		out.KnownAt = values.NewInstant(knownAt.UTC())
		return out.Validate()
	})
	return out, err
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nilIfZero(value values.Decimal) any {
	if value.IsZero() {
		return nil
	}
	return value.String()
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uint64Ptr(value *int64) uint64 {
	if value == nil || *value < 1 {
		return 0
	}
	return uint64(*value)
}

func decimalFromStored(value *string) (values.Decimal, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return values.Decimal{}, nil
	}
	text := strings.TrimSpace(*value)
	text = strings.TrimPrefix(text, "+")
	scale := int32(0)
	if _, fraction, ok := strings.Cut(text, "."); ok {
		scale = int32(len(fraction))
	}
	return values.NewDecimal(text, scale, values.RoundingExactRequired)
}
