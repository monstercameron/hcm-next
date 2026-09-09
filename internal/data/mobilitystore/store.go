// Package mobilitystore persists the immutable mobility-plan revisions and
// append-only immigration milestone log declared by migration 00102.
package mobilitystore

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/mobility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction capability required by the store.
type DB interface{ dbport.Beginner }
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// ErrorCode identifies a stable store refusal without requiring callers to
// parse database-driver text.
type ErrorCode string

const (
	CodeInvalid            ErrorCode = "MOBILITY_INVALID"
	CodeDuplicateRevision  ErrorCode = "MOBILITY_DUPLICATE_REVISION"
	CodeVersionConflict    ErrorCode = "MOBILITY_VERSION_CONFLICT"
	CodeNotFound           ErrorCode = "MOBILITY_NOT_FOUND"
	CodeDuplicateMilestone ErrorCode = "MOBILITY_DUPLICATE_MILESTONE"
	CodeIntegrityViolation ErrorCode = "MOBILITY_INTEGRITY_VIOLATION"
)

var (
	ErrInvalid            = errors.New("mobilitystore: invalid row")
	ErrDuplicateRevision  = errors.New("mobilitystore: duplicate plan revision")
	ErrVersionConflict    = errors.New("mobilitystore: plan revision conflict")
	ErrNotFound           = errors.New("mobilitystore: row not found")
	ErrDuplicateMilestone = errors.New("mobilitystore: duplicate milestone sequence")
	ErrIntegrity          = errors.New("mobilitystore: integrity violation")
)

// Error is a classified store refusal. Cause remains available through
// errors.Is for callers that use sentinel errors.
type Error struct {
	Code   ErrorCode
	Cause  error
	Detail string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s: %s", e.Code, e.Cause, e.Detail) }
func (e *Error) Unwrap() error { return e.Cause }

func refusal(code ErrorCode, cause error, detail string) error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

// Store implements mobility.TenantPlanStore over PostgreSQL.
type Store struct{ db DB }

var _ mobility.TenantPlanStore = (*Store)(nil)

// New returns a PostgreSQL-backed mobility store.
func New(db DB) *Store { return &Store{db: db} }

// Put appends one immutable mobility-plan revision.
func (s *Store) Put(ctx context.Context, tenant values.TenantId, p mobility.MobilityPlan) error {
	if err := p.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	workerRef, err := uuid.Parse(p.WorkerRef)
	if err != nil || workerRef == uuid.Nil {
		return refusal(CodeInvalid, ErrInvalid, "worker_ref must be a non-nil UUID")
	}
	payload, err := marshalPlan(p)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if err := checkPlanHead(ctx, tx, tenantID, planID(p), p.Revision, p.ParentRevision, p.ParentDigest); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO mobility_plan
				(tenant_id, row_id, mobility_id, revision, parent_revision, parent_digest,
				 worker_ref, home_assignment, host_assignment, legs, relocation,
				 immigration, obligations, status, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb,
				$12::jsonb,$13::jsonb,$14,$15)
			ON CONFLICT DO NOTHING`,
			tenantID, uuid.New(), planID(p), int64(p.Revision), nullableRevision(p.ParentRevision),
			nullableDigest(p.ParentDigest), workerRef, string(payload.Home), string(payload.Host),
			string(payload.Legs), string(payload.Relocation), string(payload.Immigration),
			string(payload.Obligations), string(p.Status), storageDigest(p.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("mobilitystore: insert plan %s/%d: %w", planID(p), p.Revision, err)
		}
		if affected != 1 {
			return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "plan revision already exists")
		}
		return nil
	})
}

// PutForTenant implements mobility.TenantPlanStore.
func (s *Store) PutForTenant(ctx context.Context, tenant values.TenantId, p mobility.MobilityPlan) error {
	return s.Put(ctx, tenant, p)
}

// Get loads one immutable plan revision.
func (s *Store) Get(ctx context.Context, tenant values.TenantId, id string, revision uint64) (mobility.MobilityPlan, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return mobility.MobilityPlan{}, err
	}
	var out mobility.MobilityPlan
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		out, err = loadPlan(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

// GetForTenant implements mobility.TenantPlanStore.
func (s *Store) GetForTenant(ctx context.Context, tenant values.TenantId, id string, revision uint64) (mobility.MobilityPlan, error) {
	return s.Get(ctx, tenant, id, revision)
}

// Load is a descriptive alias for Get.
func (s *Store) Load(ctx context.Context, tenant values.TenantId, id string, revision uint64) (mobility.MobilityPlan, error) {
	return s.Get(ctx, tenant, id, revision)
}

// List returns all plan revisions in ascending order.
func (s *Store) List(ctx context.Context, tenant values.TenantId, id string) ([]mobility.MobilityPlan, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return nil, err
	}
	var out []mobility.MobilityPlan
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM mobility_plan WHERE tenant_id=$1 AND mobility_id=$2 ORDER BY revision`, tenantID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			plan, err := loadPlan(ctx, tx, tenantID, id, uint64(revision))
			if err != nil {
				return err
			}
			out = append(out, plan)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, refusal(CodeNotFound, ErrNotFound, "mobility plan is absent")
	}
	return out, nil
}

// AppendImmigrationMilestone records one append-only milestone event.
func (s *Store) AppendImmigrationMilestone(ctx context.Context, tenant values.TenantId, m mobility.ImmigrationMilestone, sequence uint64) error {
	if err := m.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	if sequence == 0 {
		return refusal(CodeVersionConflict, ErrVersionConflict, "event sequence must be positive")
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	at := m.At
	if !at.IsSet() {
		at = m.EffectiveOn
	}
	expires := m.ExpiresOn
	if !expires.IsSet() {
		expires = m.ExpiryDate
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		lockKey := tenantID.String() + ":mobility-immigration:" + m.ProcessRef
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return err
		}
		var latest int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(event_sequence),0) FROM mobility_immigration_milestone WHERE tenant_id=$1 AND process_ref=$2`, tenantID, m.ProcessRef).Scan(&latest); err != nil {
			return err
		}
		if int64(sequence) <= latest {
			if int64(sequence) == latest {
				return refusal(CodeDuplicateMilestone, ErrDuplicateMilestone, "milestone sequence already exists")
			}
			return refusal(CodeVersionConflict, ErrVersionConflict, "milestone sequence is stale")
		}
		if int64(sequence) != latest+1 {
			return refusal(CodeVersionConflict, ErrVersionConflict, "milestone sequence is not the next sequence")
		}
		expiresArg := any(nil)
		if expires.IsSet() {
			expiresArg = expires.String()
		}
		var sourceArg, evidenceArg any
		if m.SourceRef != "" {
			sourceArg = m.SourceRef
		}
		if m.EvidenceRef != "" {
			evidenceArg = m.EvidenceRef
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO mobility_immigration_milestone
				(tenant_id,row_id,milestone_id,process_ref,kind,jurisdiction,at,source_ref,
				 evidence_ref,expires_on,canonical_digest,event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT DO NOTHING`, tenantID, uuid.New(), m.MilestoneID, m.ProcessRef,
			string(m.Kind), nullableString(m.Jurisdiction), at.String(), sourceArg, evidenceArg,
			expiresArg, storageDigest(m.CanonicalDigest), int64(sequence))
		if err != nil {
			return err
		}
		if affected != 1 {
			return refusal(CodeDuplicateMilestone, ErrDuplicateMilestone, "milestone identity already exists")
		}
		return nil
	})
}

// AppendImmigrationMilestoneForTenant implements mobility.TenantPlanStore.
func (s *Store) AppendImmigrationMilestoneForTenant(ctx context.Context, tenant values.TenantId, m mobility.ImmigrationMilestone, sequence uint64) error {
	return s.AppendImmigrationMilestone(ctx, tenant, m, sequence)
}

// ListImmigrationMilestones returns a process's append-only history.
func (s *Store) ListImmigrationMilestones(ctx context.Context, tenant values.TenantId, processRef string) ([]mobility.ImmigrationMilestone, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return nil, err
	}
	var out []mobility.ImmigrationMilestone
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT milestone_id,process_ref,kind,jurisdiction,at,source_ref,evidence_ref,
				expires_on,canonical_digest,event_sequence
			FROM mobility_immigration_milestone
			WHERE tenant_id=$1 AND process_ref=$2 ORDER BY event_sequence`, tenantID, processRef)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				m                  mobility.ImmigrationMilestone
				kind, jurisdiction string
				at                 time.Time
				source, evidence   *string
				expires            *time.Time
				digest             *string
				sequence           int64
			)
			if err := rows.Scan(&m.MilestoneID, &m.ProcessRef, &kind, &jurisdiction, &at, &source, &evidence, &expires, &digest, &sequence); err != nil {
				return err
			}
			date, err := values.ParseLocalDate(at.Format("2006-01-02"))
			if err != nil {
				return refusal(CodeIntegrityViolation, ErrIntegrity, "stored milestone date is invalid")
			}
			m.Kind, m.Jurisdiction, m.At = mobility.ImmigrationMilestoneKind(kind), jurisdiction, date
			m.Disclosure = mobility.DisclosureScope{Jurisdiction: jurisdiction, Allowed: true}
			m.SourceRef, m.EvidenceRef = stringValue(source), stringValue(evidence)
			if expires != nil {
				m.ExpiresOn, err = values.ParseLocalDate(expires.Format("2006-01-02"))
				if err != nil {
					return refusal(CodeIntegrityViolation, ErrIntegrity, "stored milestone expiry is invalid")
				}
			}
			m.CanonicalDigest = domainDigest(stringValue(digest))
			if err := m.Validate(); err != nil {
				return refusal(CodeIntegrityViolation, ErrIntegrity, "stored milestone does not revalidate")
			}
			_ = sequence
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, refusal(CodeNotFound, ErrNotFound, "immigration process is absent")
	}
	return out, nil
}

// ListImmigrationMilestonesForTenant implements mobility.TenantPlanStore.
func (s *Store) ListImmigrationMilestonesForTenant(ctx context.Context, tenant values.TenantId, processRef string) ([]mobility.ImmigrationMilestone, error) {
	return s.ListImmigrationMilestones(ctx, tenant, processRef)
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return refusal(CodeInvalid, ErrInvalid, "database is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mobilitystore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("mobilitystore: commit: %w", err)
	}
	return nil
}

func parseTenant(tenant values.TenantId) (uuid.UUID, error) {
	if err := tenant.Validate(); err != nil {
		return uuid.Nil, refusal(CodeInvalid, ErrInvalid, "tenant is required")
	}
	id, err := uuid.Parse(tenant.String())
	if err != nil || id == uuid.Nil {
		return uuid.Nil, refusal(CodeInvalid, ErrInvalid, "tenant must be a non-nil UUID")
	}
	return id, nil
}

func checkPlanHead(ctx context.Context, q Executor, tenantID uuid.UUID, id string, revision, parentRevision uint64, parentDigest string) error {
	if revision == 0 {
		return refusal(CodeInvalid, ErrInvalid, "revision must be positive")
	}
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":mobility-plan:"+id); err != nil {
		return err
	}
	var latest *int64
	var digest *string
	if err := q.QueryRow(ctx, `SELECT max(revision), (array_agg(canonical_digest ORDER BY revision DESC))[1] FROM mobility_plan WHERE tenant_id=$1 AND mobility_id=$2`, tenantID, id).Scan(&latest, &digest); err != nil {
		return err
	}
	if revision == 1 {
		if latest != nil {
			return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "revision identity already exists")
		}
		return nil
	}
	if latest == nil {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor has no current parent")
	}
	if int64(revision) == *latest {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "revision identity already exists")
	}
	if int64(revision) != *latest+1 || parentRevision != uint64(*latest) || digest == nil || *digest != storageDigest(parentDigest) {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor parent is stale")
	}
	return nil
}

func loadPlan(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, id string, revision uint64) (mobility.MobilityPlan, error) {
	if id == "" || revision == 0 {
		return mobility.MobilityPlan{}, refusal(CodeInvalid, ErrInvalid, "mobility id and positive revision are required")
	}
	var (
		rowID, worker                                          uuid.UUID
		storedID, status, digest                               string
		rev                                                    int64
		parentRevision                                         *int64
		parentDigest                                           *string
		home, host, legs, relocation, immigration, obligations []byte
	)
	err := q.QueryRow(ctx, `
		SELECT row_id,mobility_id,revision,parent_revision,parent_digest,worker_ref,
			home_assignment,host_assignment,legs,relocation,immigration,obligations,
			status,canonical_digest
		FROM mobility_plan WHERE tenant_id=$1 AND mobility_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&rowID, &storedID, &rev, &parentRevision, &parentDigest, &worker, &home, &host, &legs,
		&relocation, &immigration, &obligations, &status, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return mobility.MobilityPlan{}, refusal(CodeNotFound, ErrNotFound, "mobility plan is absent")
		}
		return mobility.MobilityPlan{}, err
	}
	_ = rowID
	p, err := unmarshalPlan(home, host, legs, relocation, immigration, obligations)
	if err != nil {
		return mobility.MobilityPlan{}, refusal(CodeIntegrityViolation, ErrIntegrity, err.Error())
	}
	p.MobilityID, p.PlanID, p.Revision, p.WorkerRef, p.Status = storedID, storedID, uint64(rev), worker.String(), mobility.MobilityStatus(status)
	if parentRevision != nil {
		p.ParentRevision = uint64(*parentRevision)
	}
	if parentDigest != nil {
		p.ParentDigest = domainDigest(*parentDigest)
	}
	stored, err := mobility.NewMobilityPlan(p)
	if err != nil || stored.CanonicalDigest != domainDigest(digest) || string(stored.Status) != status {
		return mobility.MobilityPlan{}, refusal(CodeIntegrityViolation, ErrIntegrity, "stored mobility plan does not match its canonical digest")
	}
	return stored, nil
}

type planPayload struct {
	Home        json.RawMessage
	Host        json.RawMessage
	Legs        json.RawMessage
	Relocation  json.RawMessage
	Immigration json.RawMessage
	Obligations json.RawMessage
}

func marshalPlan(p mobility.MobilityPlan) (planPayload, error) {
	home := p.HomeAssignment
	if home.AssignmentID == "" {
		home = p.Home
	}
	hosts := append([]mobility.AssignmentRevision(nil), p.HostAssignments...)
	if p.HostAssignment.AssignmentID != "" || p.HostAssignment.ID != "" {
		hosts = append(hosts, p.HostAssignment)
	}
	if len(hosts) == 0 {
		hosts = []mobility.AssignmentRevision{p.Host}
	}
	homeJSON, err := json.Marshal(assignmentToPayload(home))
	if err != nil {
		return planPayload{}, err
	}
	hostJSON, err := json.Marshal(assignmentsToPayload(hosts))
	if err != nil {
		return planPayload{}, err
	}
	legsJSON, err := json.Marshal(legsToPayload(p.Legs))
	if err != nil {
		return planPayload{}, err
	}
	relocationJSON, err := json.Marshal(relocationToPayload(p.Relocation))
	if err != nil {
		return planPayload{}, err
	}
	immigrationJSON, err := json.Marshal(immigrationToPayload(p.Immigration))
	if err != nil {
		return planPayload{}, err
	}
	obligationsJSON, err := json.Marshal(p.Obligations)
	if err != nil {
		return planPayload{}, err
	}
	return planPayload{Home: homeJSON, Host: hostJSON, Legs: legsJSON, Relocation: relocationJSON, Immigration: immigrationJSON, Obligations: obligationsJSON}, nil
}

func unmarshalPlan(home, host, legs, relocation, immigration, obligations []byte) (mobility.MobilityPlan, error) {
	var hp assignmentPayload
	if err := json.Unmarshal(home, &hp); err != nil {
		return mobility.MobilityPlan{}, err
	}
	homeValue, err := payloadToAssignment(hp)
	if err != nil {
		return mobility.MobilityPlan{}, err
	}
	var hosts []assignmentPayload
	if err := json.Unmarshal(host, &hosts); err != nil {
		return mobility.MobilityPlan{}, err
	}
	hostValues := make([]mobility.AssignmentRevision, len(hosts))
	for i, item := range hosts {
		hostValues[i], err = payloadToAssignment(item)
		if err != nil {
			return mobility.MobilityPlan{}, err
		}
	}
	var lp []legPayload
	if err := json.Unmarshal(legs, &lp); err != nil {
		return mobility.MobilityPlan{}, err
	}
	legValues := make([]mobility.MobilityLeg, len(lp))
	for i, item := range lp {
		legValues[i], err = payloadToLeg(item)
		if err != nil {
			return mobility.MobilityPlan{}, err
		}
	}
	var rp relocationPayload
	if err := json.Unmarshal(relocation, &rp); err != nil {
		return mobility.MobilityPlan{}, err
	}
	relocationValue, err := payloadToRelocation(rp)
	if err != nil {
		return mobility.MobilityPlan{}, err
	}
	var ip []immigrationPayload
	if err := json.Unmarshal(immigration, &ip); err != nil {
		return mobility.MobilityPlan{}, err
	}
	immigrationValues := make([]mobility.ImmigrationMilestone, len(ip))
	for i, item := range ip {
		immigrationValues[i], err = payloadToImmigration(item)
		if err != nil {
			return mobility.MobilityPlan{}, err
		}
	}
	var obligationValues []mobility.MobilityObligation
	if err := json.Unmarshal(obligations, &obligationValues); err != nil {
		return mobility.MobilityPlan{}, err
	}
	p := mobility.MobilityPlan{HomeAssignment: homeValue, HostAssignments: hostValues, Legs: legValues, Relocation: relocationValue, Immigration: immigrationValues, Obligations: obligationValues}
	if len(hostValues) > 0 {
		p.HostAssignment = hostValues[0]
		p.HostAssignments = hostValues[1:]
	}
	return p, nil
}

type assignmentPayload struct {
	AssignmentID       string `json:"assignment_id"`
	Revision           uint64 `json:"revision"`
	SupersedesRevision uint64 `json:"supersedes_revision"`
	SupersedesDigest   string `json:"supersedes_digest"`
	Role               string `json:"role"`
	Type               string `json:"type"`
	EntityRef          string `json:"entity_ref"`
	EmploymentRef      string `json:"employment_ref"`
	LocationRef        string `json:"location_ref"`
	Jurisdiction       string `json:"jurisdiction"`
	PayrollRef         string `json:"payroll_ref"`
	PayrollModel       string `json:"payroll_model"`
	Effective          string `json:"effective"`
	Split              string `json:"cost_allocation_split"`
	SourceAuthority    string `json:"source_authority"`
	KnownAt            string `json:"known_at,omitempty"`
}

func assignmentToPayload(a mobility.AssignmentRevision) assignmentPayload {
	effective := a.Effective
	if effective.Validate() != nil {
		effective = a.EffectiveWindow
	}
	entity := a.EntityRef
	if entity == "" {
		entity = a.Entity
	}
	split := a.CostAllocationSplit
	if split.Validate() != nil {
		split = a.AllocationSplit
	}
	if split.Validate() != nil {
		split = a.CostAllocation
	}
	splitText := ""
	if split.Validate() == nil {
		encoded, _ := split.MarshalText()
		splitText = string(encoded)
	}
	id := a.AssignmentID
	if id == "" {
		id = a.ID
	}
	out := assignmentPayload{AssignmentID: id, Revision: a.Revision, SupersedesRevision: a.SupersedesRevision, SupersedesDigest: a.SupersedesDigest, Role: string(a.Role), Type: string(a.Type), EntityRef: entity, EmploymentRef: a.EmploymentRef, LocationRef: a.LocationRef, Jurisdiction: a.Jurisdiction, PayrollRef: a.PayrollRef, PayrollModel: string(a.PayrollModel), Effective: encodeInterval(effective), Split: splitText, SourceAuthority: a.SourceAuthority}
	if a.KnownAt.Canonical() != nil {
		out.KnownAt = a.KnownAt.String()
	}
	return out
}

func assignmentsToPayload(in []mobility.AssignmentRevision) []assignmentPayload {
	out := make([]assignmentPayload, len(in))
	for i, item := range in {
		out[i] = assignmentToPayload(item)
	}
	return out
}

func payloadToAssignment(a assignmentPayload) (mobility.AssignmentRevision, error) {
	effective, err := decodeInterval(a.Effective)
	if err != nil {
		return mobility.AssignmentRevision{}, err
	}
	var split values.Decimal
	if err := split.UnmarshalText([]byte(a.Split)); err != nil {
		return mobility.AssignmentRevision{}, err
	}
	var known values.KnownAt
	if a.KnownAt != "" {
		instant, err := time.Parse(time.RFC3339Nano, a.KnownAt)
		if err != nil {
			return mobility.AssignmentRevision{}, err
		}
		known, err = values.NewKnownAt(values.NewInstant(instant))
		if err != nil {
			return mobility.AssignmentRevision{}, err
		}
	}
	return mobility.NewAssignmentRevision(mobility.AssignmentRevision{AssignmentID: a.AssignmentID, Revision: a.Revision, SupersedesRevision: a.SupersedesRevision, SupersedesDigest: domainDigest(a.SupersedesDigest), Role: mobility.AssignmentRole(a.Role), Type: mobility.AssignmentType(a.Type), EntityRef: a.EntityRef, EmploymentRef: a.EmploymentRef, LocationRef: a.LocationRef, Jurisdiction: a.Jurisdiction, PayrollRef: a.PayrollRef, PayrollModel: mobility.PayrollModel(a.PayrollModel), Effective: effective, CostAllocationSplit: split, SourceAuthority: a.SourceAuthority, KnownAt: known})
}

type legPayload struct {
	LegID        string `json:"leg_id"`
	Origin       string `json:"origin"`
	Destination  string `json:"destination"`
	Effective    string `json:"effective"`
	Purpose      string `json:"purpose"`
	WorkPresence bool   `json:"work_presence"`
	SourceRef    string `json:"source_ref"`
}

func legsToPayload(in []mobility.MobilityLeg) []legPayload {
	out := make([]legPayload, len(in))
	for i, l := range in {
		out[i] = legPayload{l.LegID, l.OriginJurisdiction, l.DestinationJurisdiction, encodeInterval(l.Effective), l.Purpose, l.WorkPresence, l.SourceRef}
	}
	return out
}
func payloadToLeg(l legPayload) (mobility.MobilityLeg, error) {
	iv, err := decodeInterval(l.Effective)
	if err != nil {
		return mobility.MobilityLeg{}, err
	}
	return mobility.MobilityLeg{LegID: l.LegID, OriginJurisdiction: l.Origin, DestinationJurisdiction: l.Destination, Effective: iv, Purpose: l.Purpose, WorkPresence: l.WorkPresence, SourceRef: l.SourceRef}, nil
}

type relocationPayload struct {
	PackageID  string                       `json:"package_id"`
	MobilityID string                       `json:"mobility_id"`
	WorkerRef  string                       `json:"worker_ref"`
	OwnerRef   string                       `json:"owner_ref"`
	Milestones []relocationMilestonePayload `json:"milestones"`
}
type relocationMilestonePayload struct {
	MilestoneID string `json:"milestone_id"`
	Kind        string `json:"kind"`
	Effective   string `json:"effective"`
	OwnerRef    string `json:"owner_ref"`
	EvidenceRef string `json:"evidence_ref"`
}

func relocationToPayload(p mobility.RelocationPackage) relocationPayload {
	out := relocationPayload{PackageID: p.PackageID, MobilityID: p.MobilityID, WorkerRef: p.WorkerRef, OwnerRef: p.OwnerRef, Milestones: make([]relocationMilestonePayload, len(p.Milestones))}
	for i, m := range p.Milestones {
		out.Milestones[i] = relocationMilestonePayload{m.MilestoneID, string(m.Kind), encodeInterval(m.Effective), m.OwnerRef, m.EvidenceRef}
	}
	return out
}
func payloadToRelocation(p relocationPayload) (mobility.RelocationPackage, error) {
	out := mobility.RelocationPackage{PackageID: p.PackageID, MobilityID: p.MobilityID, WorkerRef: p.WorkerRef, OwnerRef: p.OwnerRef, Milestones: make([]mobility.RelocationMilestone, len(p.Milestones))}
	for i, m := range p.Milestones {
		iv, err := decodeInterval(m.Effective)
		if err != nil {
			return mobility.RelocationPackage{}, err
		}
		out.Milestones[i] = mobility.RelocationMilestone{MilestoneID: m.MilestoneID, Kind: mobility.RelocationMilestoneKind(m.Kind), Effective: iv, OwnerRef: m.OwnerRef, EvidenceRef: m.EvidenceRef}
	}
	return mobility.NewRelocationPackage(out)
}

type immigrationPayload struct {
	MilestoneID  string                   `json:"milestone_id"`
	ProcessRef   string                   `json:"process_ref"`
	Kind         string                   `json:"kind"`
	Jurisdiction string                   `json:"jurisdiction"`
	At           string                   `json:"at"`
	SourceRef    string                   `json:"source_ref"`
	EvidenceRef  string                   `json:"evidence_ref"`
	ExpiresOn    string                   `json:"expires_on,omitempty"`
	Disclosure   mobility.DisclosureScope `json:"disclosure"`
}

func immigrationToPayload(in []mobility.ImmigrationMilestone) []immigrationPayload {
	out := make([]immigrationPayload, len(in))
	for i, m := range in {
		out[i] = immigrationPayload{m.MilestoneID, m.ProcessRef, string(m.Kind), m.Jurisdiction, m.At.String(), m.SourceRef, m.EvidenceRef, m.ExpiresOn.String(), m.Disclosure}
	}
	return out
}
func payloadToImmigration(m immigrationPayload) (mobility.ImmigrationMilestone, error) {
	at, err := values.ParseLocalDate(m.At)
	if err != nil {
		return mobility.ImmigrationMilestone{}, err
	}
	out := mobility.ImmigrationMilestone{MilestoneID: m.MilestoneID, ProcessRef: m.ProcessRef, Kind: mobility.ImmigrationMilestoneKind(m.Kind), Jurisdiction: m.Jurisdiction, At: at, SourceRef: m.SourceRef, EvidenceRef: m.EvidenceRef, Disclosure: m.Disclosure}
	if m.ExpiresOn != "" {
		out.ExpiresOn, err = values.ParseLocalDate(m.ExpiresOn)
		if err != nil {
			return mobility.ImmigrationMilestone{}, err
		}
	}
	return mobility.NewImmigrationMilestone(out)
}

func encodeInterval(iv values.EffectiveInterval) string {
	return base64.StdEncoding.EncodeToString(iv.Canonical())
}

func decodeInterval(encoded string) (values.EffectiveInterval, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	if len(raw) < 3 || raw[0] != 0x05 {
		return values.EffectiveInterval{}, errors.New("invalid effective interval encoding")
	}
	kind, hasEnd, pos := values.IntervalKind(raw[1]), raw[2] == 1, 3
	readDate := func() (values.LocalDate, error) {
		if len(raw)-pos < 7 || raw[pos] != 0x02 {
			return values.LocalDate{}, errors.New("invalid local date encoding")
		}
		year := int32(binary.BigEndian.Uint32(raw[pos+1 : pos+5]))
		month, day := time.Month(raw[pos+5]), int(raw[pos+6])
		pos += 7
		return values.NewLocalDate(int(year), month, day)
	}
	readInstant := func() (values.Instant, error) {
		if len(raw)-pos < 13 || raw[pos] != 0x01 {
			return values.Instant{}, errors.New("invalid instant encoding")
		}
		sec := int64(binary.BigEndian.Uint64(raw[pos+1 : pos+9]))
		nsec := int32(binary.BigEndian.Uint32(raw[pos+9 : pos+13]))
		pos += 13
		return values.NewInstantFromUnix(sec, nsec)
	}
	readString := func() (string, error) {
		if len(raw)-pos < 4 {
			return "", errors.New("invalid interval string length")
		}
		n := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
		pos += 4
		if n < 0 || len(raw)-pos < n {
			return "", errors.New("invalid interval string")
		}
		out := string(raw[pos : pos+n])
		pos += n
		return out, nil
	}
	var localStart, localEnd values.LocalDate
	var instantStart, instantEnd values.Instant
	switch kind {
	case values.IntervalKindLocalDate:
		localStart, err = readDate()
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		if hasEnd {
			localEnd, err = readDate()
			if err != nil {
				return values.EffectiveInterval{}, err
			}
		}
	case values.IntervalKindInstant:
		instantStart, err = readInstant()
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		if hasEnd {
			instantEnd, err = readInstant()
			if err != nil {
				return values.EffectiveInterval{}, err
			}
		}
	default:
		return values.EffectiveInterval{}, errors.New("invalid interval kind")
	}
	calendarRef, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendarVersion, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	zoneID, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	zoneVersion, err := readString()
	if err != nil || pos >= len(raw) {
		return values.EffectiveInterval{}, errors.New("invalid interval zone")
	}
	disambiguation := values.Disambiguation(raw[pos])
	var iv values.EffectiveInterval
	if kind == values.IntervalKindLocalDate {
		if hasEnd {
			iv, err = values.NewLocalDateInterval(localStart, localEnd, values.CalendarRef{Ref: calendarRef, Version: calendarVersion})
		} else {
			iv, err = values.NewOpenLocalDateInterval(localStart, values.CalendarRef{Ref: calendarRef, Version: calendarVersion})
		}
	} else if hasEnd {
		iv, err = values.NewInstantInterval(instantStart, instantEnd)
	} else {
		iv, err = values.NewOpenInstantInterval(instantStart)
	}
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	if zoneID != "" || zoneVersion != "" {
		iv, err = iv.WithZone(values.ZoneRef{ID: zoneID, TzdbVersion: zoneVersion}, disambiguation)
	}
	return iv, err
}

func planID(p mobility.MobilityPlan) string {
	if p.MobilityID != "" {
		return p.MobilityID
	}
	return p.PlanID
}
func nullableRevision(v uint64) any {
	if v == 0 {
		return nil
	}
	return int64(v)
}
func nullableDigest(v string) any {
	if v == "" {
		return nil
	}
	return storageDigest(v)
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func storageDigest(v string) string { return strings.TrimPrefix(v, "sha256:") }
func domainDigest(v string) string {
	if v == "" || strings.HasPrefix(v, "sha256:") {
		return v
	}
	return "sha256:" + v
}
