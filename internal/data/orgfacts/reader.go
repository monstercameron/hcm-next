// Package orgfacts is PROMOUX-005's real, production-backed implementation of
// [org.WorkerFacts] -- the first one anywhere in the tree. Before this
// package, org.WorkerFacts had exactly one kind of implementation: an
// in-memory test fixture. internal/domains/promotion/snapshot's manager-chain
// input and org.DetectManagerCycle's traversal both take an org.WorkerFacts
// reader, but neither had a real store behind it, so a cycle-safety
// evaluation composed for the live server had nothing to read.
//
// Reader answers from journey_worker, the same created-population table
// internal/data/workforce.Facts already reads for people.WorkerFacts (and
// the same table internal/data/demoworkforce seeds the demo corpus into, per
// PROMOUX-001's evidence). journey_worker carries exactly one manager fact
// per worker -- manager_relationship_ref -- so this reader reports at most
// one DIRECT_MANAGER relationship and never a dotted line; there is no
// second relationship stream in this population to report one from.
package orgfacts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PolicyVersion is the org-graph policy version this reader's answers carry.
// It is a distinct token from workforce.AuthorityPolicy: the two ports answer
// different questions (worker field facts versus manager-relationship
// facts) even though they read the same row.
const PolicyVersion = "org.manager_relationship.journey_worker/2026.1"

// Reader answers [org.WorkerFacts] from journey_worker.
//
// It opens and rolls back its own read transaction, exactly like
// internal/data/workforce.Facts, because a manager-relationship read has no
// caller-visible transaction to join and journey_worker is row-level-security
// protected: the tenant has to be established on the connection before the
// SELECT runs.
type Reader struct {
	// DB opens the read transactions. Nil makes every read report "this
	// worker does not exist", which is the correct answer for a cell
	// composed with no execution database.
	DB dbport.Beginner
	// TenantUUID maps the query's tenant key onto the uuid journey_worker
	// rows carry. Nil is treated the same way as a nil DB.
	TenantUUID func(values.TenantId) uuid.UUID
}

var _ org.WorkerFacts = Reader{}

// NewReader builds the production org.WorkerFacts adapter.
func NewReader(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) Reader {
	return Reader{DB: db, TenantUUID: tenantUUID}
}

// WorkerFactsAt implements [org.WorkerFacts].
//
// A worker with no manager_relationship_ref, or one that does not resolve to
// another journey_worker row in this tenant (the seeded "board:harborcare"
// sentinel included), is reported as an existing worker with zero
// relationships -- the top of the chain -- never as a fabricated manager.
func (r Reader) WorkerFactsAt(ctx context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if err := q.Validate(); err != nil {
		return org.WorkerFactSet{}, err
	}
	if r.DB == nil || r.TenantUUID == nil {
		return org.WorkerFactSet{Worker: q.Worker, Exists: false}, nil
	}
	tenantID := r.TenantUUID(q.Tenant)
	if tenantID == uuid.Nil {
		return org.WorkerFactSet{Worker: q.Worker, Exists: false}, nil
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("%w: begin: %v", org.ErrReaderFailed, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("%w: scope tenant: %v", org.ErrReaderFailed, err)
	}

	var store workforce.Store
	row, found, err := store.Get(ctx, tx, tenantID, q.Worker.Id)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("%w: %v", org.ErrReaderFailed, err)
	}
	if !found {
		return org.WorkerFactSet{Worker: q.Worker, Exists: false}, nil
	}

	var relationships []org.ManagerRelationshipFact
	if managerRef := row.ManagerRelationshipRef; managerRef != "" {
		managerRow, managerFound, err := store.Get(ctx, tx, tenantID, managerRef)
		if err != nil {
			return org.WorkerFactSet{}, fmt.Errorf("%w: resolve manager: %v", org.ErrReaderFailed, err)
		}
		if managerFound {
			fact, err := r.factFrom(q.Tenant, q.Worker, row, managerRow)
			if err != nil {
				return org.WorkerFactSet{}, fmt.Errorf("%w: %v", org.ErrReaderFailed, err)
			}
			relationships = append(relationships, fact)
		}
		// A manager_relationship_ref that does not resolve to another
		// journey_worker row in this tenant -- a sentinel like
		// "board:harborcare", or a dangling reference -- is not a manager
		// this population can name, so it is reported as no relationship at
		// all: the correct answer for the top of the chain, never a
		// fabricated hop.
	}

	watermark, err := r.graphWatermark(ctx, tx, tenantID)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("%w: graph watermark: %v", org.ErrReaderFailed, err)
	}
	return org.WorkerFactSet{
		Worker:        q.Worker,
		Exists:        true,
		Relationships: relationships,
		Watermark:     watermark,
		PolicyVersion: PolicyVersion,
	}, nil
}

// graphWatermark is a tenant-wide consistency token, not a per-worker
// revision: [ResolveManagerRelationships] requires every hop of one walk to
// report the SAME watermark, and to declare DISAGREEING the moment two hops
// disagree, so a per-worker revision (which legitimately differs from row to
// row) cannot serve as this reader's watermark. MAX(known_at) across every
// journey_worker row in the tenant is stable across the hops of one short
// walk when nothing changes underneath it, and genuinely changes -- and is
// therefore correctly caught as DISAGREEING -- when a row in this tenant is
// created or updated between two reads of the same resolution.
func (r Reader) graphWatermark(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) (values.RevisionToken, error) {
	row := tx.QueryRow(ctx, `SELECT COALESCE(MAX(known_at), 'epoch'::timestamptz) FROM journey_worker WHERE tenant_id = $1`, tenantID)
	var maxKnownAt time.Time
	if err := row.Scan(&maxKnownAt); err != nil {
		return values.RevisionToken{}, err
	}
	return values.NewSequenceRevision("org.manager_relationship.tenant."+tenantID.String(), uint64(maxKnownAt.UTC().UnixNano()))
}

// factFrom builds the one DIRECT_MANAGER fact journey_worker can assert for
// worker. RelationshipID is deterministic from the worker's own assignment
// and the manager's row id, so re-reading the same unchanged row twice
// yields the identical relationship identity.
func (r Reader) factFrom(tenant values.TenantId, worker values.EntityRef, row, managerRow workforce.WorkerRow) (org.ManagerRelationshipFact, error) {
	managerRef := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: managerRow.WorkerID.String()}
	effectiveFrom, err := values.ParseLocalDate(row.EffectiveFrom)
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(dateAtMidnightUTC(effectiveFrom)))
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(row.KnownAt))
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(row.RecordedAt))
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	revision, err := values.NewSequenceRevision(row.RevisionStream, row.RevisionSequence)
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	return org.ManagerRelationshipFact{
		RelationshipID: "rel_" + row.WorkerID.String() + "_" + managerRow.WorkerID.String(),
		Type:           org.RelationshipDirectManager,
		Worker:         worker,
		Manager:        managerRef,
		AssignmentID:   row.AssignmentID,
		Effective:      effective,
		KnownAt:        knownAt,
		Revision:       revision,
		Authority:      evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: workforce.SourceSystem, PolicyRef: workforce.AuthorityPolicy},
		Provenance:     evidence.Provenance{Source: workforce.SourceSystem, EvidenceRef: workforce.EvidenceRef(row), RecordedAt: recordedAt},
	}, nil
}

func dateAtMidnightUTC(d values.LocalDate) time.Time {
	return time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC)
}
