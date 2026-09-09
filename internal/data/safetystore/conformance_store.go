package safetystore

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/safety"
	"time"
)

func (s Store) insertConformance(table string, args ...any) error {
	a, err := s.Executor.Exec(context.Background(), fmt.Sprintf("INSERT INTO %s VALUES (%s) ON CONFLICT DO NOTHING", table, placeholders(len(args))), args...)
	if err != nil {
		return fmt.Errorf("safetystore: insert %s: %w", table, err)
	}
	if a == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
	}
	return nil
}
func placeholders(n int) string {
	out := ""
	for i := 1; i <= n; i++ {
		if i > 1 {
			out += ","
		}
		out += fmt.Sprintf("$%d", i)
	}
	return out
}
func (s Store) conformanceReady() error                               { return s.ready() }
func conformanceIDs(caseRef, id string) (uuid.UUID, uuid.UUID, error) { return ids(caseRef, id) }
func (s Store) checkConformance(table, idcol string, caseRef, id uuid.UUID, rev, parent uint64, pdigest, digest string) error {
	return s.checkRevisionHead(table, idcol, caseRef, id, rev, parent, pdigest, digest)
}
func tval(i interface {
	IsSet() bool
	Time() time.Time
}) any {
	if !i.IsSet() {
		return nil
	}
	return i.Time().Truncate(time.Microsecond)
}

func nanosResidue(i interface {
	IsSet() bool
	Time() time.Time
}) any {
	if !i.IsSet() {
		return nil
	}
	return int16(i.Time().Nanosecond() % 1000)
}

func int16Value(v *int16) int16 {
	if v == nil {
		return 0
	}
	return *v
}

func (s Store) SaveFiling(r safety.FilingRevision) error {
	if err := s.conformanceReady(); err != nil {
		return err
	}
	n, err := safety.NewFilingRevision(r)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	c, id, err := conformanceIDs(n.CaseRef, n.ID)
	if err != nil {
		return err
	}
	comp, err := parseID("compartment_ref", n.CompartmentRef)
	if err != nil {
		return err
	}
	inc, err := parseID("incident_ref", n.IncidentRef)
	if err != nil {
		return err
	}
	auth, err := parseID("authority_ref", n.AuthorityRef)
	if err != nil {
		return err
	}
	prov, err := parseID("provider_ref", n.ProviderRef)
	if err != nil {
		return err
	}
	signer, err := parseID("signer_ref", n.SignerRef)
	if err != nil {
		return err
	}
	if err = s.checkConformance("safety_filing_revision", "filing_id", c, id, n.Revision, n.ParentRevision, n.ParentDigest, n.CanonicalDigest); err != nil {
		return err
	}
	return s.insertConformance("safety_filing_revision", s.TenantID, uuid.New(), c, comp, id, n.Revision, nullableRevision(n.ParentRevision), nullableDigest(n.ParentDigest), storageDigest(n.CanonicalDigest), inc, auth, prov, n.SubmissionRef, signer, n.SignatureRef, string(n.Status), nullString(n.ObservationRef), tval(n.ObservedAt), nanosResidue(n.ObservedAt), n.Obligations)
}
func (s Store) GetFiling(id string, rev uint64) (safety.FilingRevision, bool) {
	var r safety.FilingRevision
	var c, comp, f, inc, auth, prov, signer uuid.UUID
	var n int64
	var pr *int64
	var pd *string
	var d string
	var status, obs, sig, sub *string
	var at *time.Time
	var atNanos *int16
	var obligations []string
	x, err := uuid.Parse(id)
	if err != nil || s.conformanceReady() != nil {
		return r, false
	}
	err = s.Executor.QueryRow(context.Background(), `SELECT case_ref,compartment_ref,filing_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,authority_ref,provider_ref,submission_ref,signer_ref,signature_ref,status,observation_ref,observed_at,observed_at_nanos,obligations FROM safety_filing_revision WHERE tenant_id=$1 AND filing_id=$2 AND revision=$3`, s.TenantID, x, rev).Scan(&c, &comp, &f, &n, &pr, &pd, &d, &inc, &auth, &prov, &sub, &signer, &sig, &status, &obs, &at, &atNanos, &obligations)
	if err != nil {
		return r, false
	}
	filing := safety.FilingRevision{ID: f.String(), CaseRef: c.String(), CompartmentRef: comp.String(), Revision: uint64(n), ParentRevision: uint64(int64Value(pr)), ParentDigest: domainDigest(stringValue(pd)), IncidentRef: inc.String(), AuthorityRef: auth.String(), ProviderRef: prov.String(), SubmissionRef: stringValue(sub), SignerRef: signer.String(), SignatureRef: stringValue(sig), Status: safety.FilingStatus(stringValue(status)), ObservationRef: stringValue(obs), Obligations: obligations, CanonicalDigest: domainDigest(d)}
	if at != nil {
		filing.ObservedAt = newInstant(at.Add(time.Duration(int16Value(atNanos)) * time.Nanosecond))
	}
	r, err = safety.NewFilingRevision(filing)
	return r, err == nil && r.CanonicalDigest == domainDigest(d)
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s Store) SaveWorkersCompPayment(r safety.WorkersCompPaymentRevision) error {
	return s.savePayment(r)
}
func (s Store) savePayment(r safety.WorkersCompPaymentRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	n, err := safety.NewWorkersCompPaymentRevision(r)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	c, id, err := ids(n.CaseRef, n.ID)
	if err != nil {
		return err
	}
	comp, err := parseID("compartment_ref", n.CompartmentRef)
	if err != nil {
		return err
	}
	w, err := parseID("worker_ref", n.WorkerRef)
	if err != nil {
		return err
	}
	if err = s.checkRevisionHead("safety_workers_comp_payment_revision", "payment_id", c, id, n.Revision, n.ParentRevision, n.ParentDigest, n.CanonicalDigest); err != nil {
		return err
	}
	if n.Status == safety.PaymentReversed {
		var st string
		var amt int64
		var cur, claim string
		var worker uuid.UUID
		err = s.Executor.QueryRow(context.Background(), `SELECT status,amount_minor,currency,claim_ref,worker_ref FROM safety_workers_comp_payment_revision WHERE tenant_id=$1 AND case_ref=$2 AND payment_id=$3 AND revision=$4`, s.TenantID, c, id, n.ParentRevision).Scan(&st, &amt, &cur, &claim, &worker)
		if err != nil || st != string(safety.PaymentSettled) || amt != n.AmountMinor || cur != n.Currency || claim != n.ClaimRef || worker != w {
			return refusal(CodeIntegrityViolation, ErrIntegrity, "reversal must exactly offset stored settled parent")
		}
	}
	return s.insertConformance("safety_workers_comp_payment_revision", s.TenantID, uuid.New(), c, comp, id, n.Revision, nullableRevision(n.ParentRevision), nullableDigest(n.ParentDigest), storageDigest(n.CanonicalDigest), n.ClaimRef, w, n.AmountMinor, n.Currency, string(n.Status), n.ObservationRef, nullString(n.ReversalRef))
}
func (s Store) GetWorkersCompPayment(id string, rev uint64) (safety.WorkersCompPaymentRevision, bool) {
	var x safety.WorkersCompPaymentRevision
	var c, comp, p, w uuid.UUID
	var n int64
	var pr *int64
	var pd *string
	var d string
	var claim, cur, status, obs, rr string
	u, e := uuid.Parse(id)
	if e != nil || s.ready() != nil {
		return x, false
	}
	e = s.Executor.QueryRow(context.Background(), `SELECT case_ref,compartment_ref,payment_id,revision,parent_revision,parent_digest,canonical_digest,claim_ref,worker_ref,amount_minor,currency,status,observation_ref,coalesce(reversal_ref,'') FROM safety_workers_comp_payment_revision WHERE tenant_id=$1 AND payment_id=$2 AND revision=$3`, s.TenantID, u, rev).Scan(&c, &comp, &p, &n, &pr, &pd, &d, &claim, &w, &x.AmountMinor, &cur, &status, &obs, &rr)
	if e != nil {
		return x, false
	}
	x.ID = p.String()
	x.CaseRef = c.String()
	x.CompartmentRef = comp.String()
	x.Revision = uint64(n)
	x.ParentRevision = uint64(int64Value(pr))
	x.ParentDigest = domainDigest(stringValue(pd))
	x.ClaimRef = claim
	x.WorkerRef = w.String()
	x.Currency = cur
	x.Status = safety.PaymentStatus(status)
	x.ObservationRef = obs
	x.ReversalRef = rr
	x.CanonicalDigest = domainDigest(d)
	y, e := safety.NewWorkersCompPaymentRevision(x)
	return y, e == nil && y.CanonicalDigest == domainDigest(d)
}

func (s Store) SaveRestrictionClearance(r safety.RestrictionClearanceRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	n, e := safety.NewRestrictionClearanceRevision(r)
	if e != nil {
		return refusal(CodeInvalid, ErrInvalid, e.Error())
	}
	c, id, e := ids(n.CaseRef, n.ID)
	if e != nil {
		return e
	}
	comp, e := parseID("compartment_ref", n.CompartmentRef)
	if e != nil {
		return e
	}
	rr, e := parseID("restriction_ref", n.RestrictionRef)
	if e != nil {
		return e
	}
	w, e := parseID("worker_ref", n.WorkerRef)
	if e != nil {
		return e
	}
	if e = s.checkRevisionHead("safety_restriction_clearance_revision", "clearance_id", c, id, n.Revision, n.ParentRevision, n.ParentDigest, n.CanonicalDigest); e != nil {
		return e
	}
	return s.insertConformance("safety_restriction_clearance_revision", s.TenantID, uuid.New(), c, comp, id, n.Revision, nullableRevision(n.ParentRevision), nullableDigest(n.ParentDigest), storageDigest(n.CanonicalDigest), rr, w, n.EvidenceRef, n.ObservationRef, n.AuthorityRef, n.EvidenceDigest)
}
func (s Store) GetRestrictionClearance(id string, rev uint64) (safety.RestrictionClearanceRevision, bool) {
	var r safety.RestrictionClearanceRevision
	u, e := uuid.Parse(id)
	if e != nil || s.ready() != nil {
		return r, false
	}
	var c, comp, x, rr, w uuid.UUID
	var n int64
	var pr *int64
	var pd *string
	var d string
	e = s.Executor.QueryRow(context.Background(), `SELECT case_ref,compartment_ref,clearance_id,revision,parent_revision,parent_digest,canonical_digest,restriction_ref,worker_ref,evidence_ref,observation_ref,authority_ref,evidence_digest FROM safety_restriction_clearance_revision WHERE tenant_id=$1 AND clearance_id=$2 AND revision=$3`, s.TenantID, u, rev).Scan(&c, &comp, &x, &n, &pr, &pd, &d, &rr, &w, &r.EvidenceRef, &r.ObservationRef, &r.AuthorityRef, &r.EvidenceDigest)
	if e != nil {
		return r, false
	}
	r.ID = x.String()
	r.CaseRef = c.String()
	r.CompartmentRef = comp.String()
	r.Revision = uint64(n)
	r.ParentRevision = uint64(int64Value(pr))
	r.ParentDigest = domainDigest(stringValue(pd))
	r.RestrictionRef = rr.String()
	r.WorkerRef = w.String()
	r.CanonicalDigest = domainDigest(d)
	z, e := safety.NewRestrictionClearanceRevision(r)
	return z, e == nil && z.CanonicalDigest == domainDigest(d)
}
func (s Store) SaveSafetyReconciliation(r safety.SafetyReconciliationRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	n, e := safety.NewSafetyReconciliationRevision(r)
	if e != nil {
		return refusal(CodeInvalid, ErrInvalid, e.Error())
	}
	c, id, e := ids(n.CaseRef, n.ID)
	if e != nil {
		return e
	}
	comp, e := parseID("compartment_ref", n.CompartmentRef)
	if e != nil {
		return e
	}
	inc, e := parseID("incident_ref", n.IncidentRef)
	if e != nil {
		return e
	}
	if e = s.checkRevisionHead("safety_reconciliation_revision", "reconciliation_id", c, id, n.Revision, n.ParentRevision, n.ParentDigest, n.CanonicalDigest); e != nil {
		return e
	}
	return s.insertConformance("safety_reconciliation_revision", s.TenantID, uuid.New(), c, comp, id, n.Revision, nullableRevision(n.ParentRevision), nullableDigest(n.ParentDigest), storageDigest(n.CanonicalDigest), inc, storageDigest(n.SourceRevisionDigest), n.ObservationRef, nullString(n.AmendedFilingRef), nullString(n.RepairRef), n.Obligations, tval(n.ObservedAt), nanosResidue(n.ObservedAt), tval(n.PriorObservedAt), nanosResidue(n.PriorObservedAt), string(n.Status))
}
func (s Store) GetSafetyReconciliation(id string, rev uint64) (safety.SafetyReconciliationRevision, bool) {
	var r safety.SafetyReconciliationRevision
	u, e := uuid.Parse(id)
	if e != nil || s.ready() != nil {
		return r, false
	}
	var c, comp, x, inc uuid.UUID
	var n int64
	var pr *int64
	var pd *string
	var d, src, obs, status string
	var af, rr *string
	var obligations []string
	var at time.Time
	var prior *time.Time
	var atNanos int16
	var priorNanos *int16
	e = s.Executor.QueryRow(context.Background(), `SELECT case_ref,compartment_ref,reconciliation_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,source_revision_digest,observation_ref,amended_filing_ref,repair_ref,obligations,observed_at,observed_at_nanos,prior_observed_at,prior_observed_at_nanos,status FROM safety_reconciliation_revision WHERE tenant_id=$1 AND reconciliation_id=$2 AND revision=$3`, s.TenantID, u, rev).Scan(&c, &comp, &x, &n, &pr, &pd, &d, &inc, &src, &obs, &af, &rr, &obligations, &at, &atNanos, &prior, &priorNanos, &status)
	if e != nil {
		return r, false
	}
	r.ID = x.String()
	r.CaseRef = c.String()
	r.CompartmentRef = comp.String()
	r.Revision = uint64(n)
	r.ParentRevision = uint64(int64Value(pr))
	r.ParentDigest = domainDigest(stringValue(pd))
	r.IncidentRef = inc.String()
	r.SourceRevisionDigest = domainDigest(src)
	r.ObservationRef = obs
	r.AmendedFilingRef = stringValue(af)
	r.RepairRef = stringValue(rr)
	r.Obligations = obligations
	r.ObservedAt = newInstant(at.Add(time.Duration(atNanos) * time.Nanosecond))
	if prior != nil {
		r.PriorObservedAt = newInstant(prior.Add(time.Duration(int16Value(priorNanos)) * time.Nanosecond))
	}
	r.Status = safety.ReconciliationStatus(status)
	r.CanonicalDigest = domainDigest(d)
	z, e := safety.NewSafetyReconciliationRevision(r)
	return z, e == nil && z.CanonicalDigest == domainDigest(d)
}
func (s Store) SaveSafetyCorrection(r safety.SafetyCorrectionRevision) error {
	if err := s.ready(); err != nil {
		return err
	}
	n, e := safety.NewSafetyCorrectionRevision(r)
	if e != nil {
		return refusal(CodeInvalid, ErrInvalid, e.Error())
	}
	c, id, e := ids(n.CaseRef, n.ID)
	if e != nil {
		return e
	}
	comp, e := parseID("compartment_ref", n.CompartmentRef)
	if e != nil {
		return e
	}
	inc, e := parseID("incident_ref", n.IncidentRef)
	if e != nil {
		return e
	}
	if e = s.checkRevisionHead("safety_correction_revision", "correction_id", c, id, n.Revision, n.ParentRevision, n.ParentDigest, n.CanonicalDigest); e != nil {
		return e
	}
	return s.insertConformance("safety_correction_revision", s.TenantID, uuid.New(), c, comp, id, n.Revision, nullableRevision(n.ParentRevision), nullableDigest(n.ParentDigest), storageDigest(n.CanonicalDigest), inc, storageDigest(n.SourceRevisionDigest), n.Reason, n.EvidenceRef, nullString(n.AmendedFilingRef), nullString(n.RepairRef))
}
func (s Store) GetSafetyCorrection(id string, rev uint64) (safety.SafetyCorrectionRevision, bool) {
	var r safety.SafetyCorrectionRevision
	u, e := uuid.Parse(id)
	if e != nil || s.ready() != nil {
		return r, false
	}
	var c, comp, x, inc uuid.UUID
	var n int64
	var pr *int64
	var pd *string
	var d, src string
	var af, rr *string
	e = s.Executor.QueryRow(context.Background(), `SELECT case_ref,compartment_ref,correction_id,revision,parent_revision,parent_digest,canonical_digest,incident_ref,source_revision_digest,reason,evidence_ref,amended_filing_ref,repair_ref FROM safety_correction_revision WHERE tenant_id=$1 AND correction_id=$2 AND revision=$3`, s.TenantID, u, rev).Scan(&c, &comp, &x, &n, &pr, &pd, &d, &inc, &src, &r.Reason, &r.EvidenceRef, &af, &rr)
	if e != nil {
		return r, false
	}
	r.ID = x.String()
	r.CaseRef = c.String()
	r.CompartmentRef = comp.String()
	r.Revision = uint64(n)
	r.ParentRevision = uint64(int64Value(pr))
	r.ParentDigest = domainDigest(stringValue(pd))
	r.IncidentRef = inc.String()
	r.SourceRevisionDigest = domainDigest(src)
	r.AmendedFilingRef = stringValue(af)
	r.RepairRef = stringValue(rr)
	r.CanonicalDigest = domainDigest(d)
	z, e := safety.NewSafetyCorrectionRevision(r)
	return z, e == nil && z.CanonicalDigest == domainDigest(d)
}
