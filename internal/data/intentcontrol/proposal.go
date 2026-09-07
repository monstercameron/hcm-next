package intentcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// Reversibility classes, matching the schema's closed vocabulary.
const (
	Reversible    = "REVERSIBLE"
	Compensatable = "COMPENSATABLE"
	Irreversible  = "IRREVERSIBLE"
)

// Materiality classes.
const (
	Material    = "MATERIAL"
	NonMaterial = "NON_MATERIAL"
)

// WriteItem is one proposal_write_item row: a single field a proposal revision
// would change, with the canonical text of both the current and the proposed
// value and the revision the write expects to find.
type WriteItem struct {
	SubjectKind string
	SubjectID   string
	ResourceKey string
	FieldPath   string

	CurrentCanonicalText  string
	ProposedCanonicalText string

	ExpectedRevision        string
	SourceAuthorityDecision string
}

// Validate rejects a write item that could not be stored.
func (w WriteItem) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"subject_kind", w.SubjectKind},
		{"subject_id", w.SubjectID},
		{"resource_key", w.ResourceKey},
		{"field_path", w.FieldPath},
		{"expected_revision", w.ExpectedRevision},
		{"source_authority_decision", w.SourceAuthorityDecision},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	// A write that proposes what is already there is not a write. The schema
	// says the same thing; saying it here names the field.
	if w.ProposedCanonicalText == w.CurrentCanonicalText {
		return invalid("proposed_canonical_text", "the proposed value equals the current one")
	}
	return nil
}

// EffectItem is one proposal_effect_item row. Both CompensationRef and
// ObservationRef are required: internal/intent.CompilePlan refuses to compile a
// plan whose effect has neither, and an effect that nothing compensates and
// nothing watches has no business existing in a proposal either.
type EffectItem struct {
	EffectID        string
	Kind            string
	DestinationRef  string
	Reversibility   string
	CompensationRef string
	ObservationRef  string
}

// Validate rejects an effect item that could not be stored.
func (e EffectItem) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"effect_id", e.EffectID},
		{"effect_kind", e.Kind},
		{"destination_ref", e.DestinationRef},
		{"compensation_ref", e.CompensationRef},
		{"observation_ref", e.ObservationRef},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	switch e.Reversibility {
	case Reversible, Compensatable, Irreversible:
		return nil
	default:
		return invalid("reversibility", "value is not REVERSIBLE, COMPENSATABLE or IRREVERSIBLE")
	}
}

// ApprovalRequirementItem is one proposal_approval_requirement row.
type ApprovalRequirementItem struct {
	RequirementID        string
	SeparationConstraint string
	MaterialityClass     string
}

// Validate rejects an approval requirement that could not be stored.
func (a ApprovalRequirementItem) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"requirement_id", a.RequirementID},
		{"separation_constraint", a.SeparationConstraint},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	switch a.MaterialityClass {
	case Material, NonMaterial:
		return nil
	default:
		return invalid("materiality_class", "value is not MATERIAL or NON_MATERIAL")
	}
}

// ObligationItem is one proposal_obligation row. DueAt is required because an
// obligation with no deadline can never become OVERDUE, which is one of the
// five lifecycle dimensions migration 00004 already declares.
type ObligationItem struct {
	ObligationID string
	Kind         string
	DueAt        time.Time
}

// Validate rejects an obligation that could not be stored.
func (o ObligationItem) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"obligation_id", o.ObligationID},
		{"obligation_kind", o.Kind},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	return requireInstant("due_at", o.DueAt)
}

// ProposalSets is the four sets a proposal revision carries. They are written
// together because they are one proposal: a revision whose writes landed and
// whose approval requirements did not would be a revision that looks
// unconditionally approvable.
type ProposalSets struct {
	Writes      []WriteItem
	Effects     []EffectItem
	Approvals   []ApprovalRequirementItem
	Obligations []ObligationItem
}

// Validate rejects any member that could not be stored, naming its position so
// a rejected set of twelve writes says which one is wrong.
func (p ProposalSets) Validate() error {
	for i, w := range p.Writes {
		if err := w.Validate(); err != nil {
			return fmt.Errorf("writes[%d]: %w", i, err)
		}
	}
	for i, e := range p.Effects {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("effects[%d]: %w", i, err)
		}
	}
	for i, a := range p.Approvals {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("approvals[%d]: %w", i, err)
		}
	}
	for i, o := range p.Obligations {
		if err := o.Validate(); err != nil {
			return fmt.Errorf("obligations[%d]: %w", i, err)
		}
	}
	return nil
}

// ProposalSetStore writes and reads a proposal revision's four sets.
type ProposalSetStore struct{}

// Record inserts every member of sets against one proposal revision.
//
// Ordinals are assigned from slice position starting at 1, and they are not
// cosmetic: the proposal's material digest is computed over the sets in a
// canonical order, so a set stored without its order is a set whose digest
// cannot be recomputed from what was stored. Because the four tables are
// append-only, a second Record for the same revision collides on the first
// member rather than appending a duplicate set -- the caller gets
// [ErrDuplicate], and the caller's transaction is what makes the partial insert
// disappear.
func (s ProposalSetStore) Record(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID,
	revision uint64, sets ProposalSets,
) error {
	if err := requireID("tenant_id", tenantID); err != nil {
		return err
	}
	if err := requireID("intent_id", intentID); err != nil {
		return err
	}
	if revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	if err := sets.Validate(); err != nil {
		return err
	}

	writeStatements := make([]dbport.Statement, 0, len(sets.Writes))
	for i, w := range sets.Writes {
		writeStatements = append(writeStatements, dbport.Statement{SQL: `
			INSERT INTO proposal_write_item (
				tenant_id, intent_id, revision, ordinal,
				subject_kind, subject_id, resource_key, field_path,
				current_canonical_text, proposed_canonical_text,
				expected_revision, source_authority_decision)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT DO NOTHING`, Args: []any{
			tenantID, intentID, int64(revision), i + 1,
			w.SubjectKind, w.SubjectID, w.ResourceKey, w.FieldPath,
			w.CurrentCanonicalText, w.ProposedCanonicalText,
			w.ExpectedRevision, w.SourceAuthorityDecision,
		}})
	}
	writeCounts, err := dbport.ExecAll(ctx, ex, writeStatements)
	if err != nil {
		index := dbport.FailedStatement(writeCounts, len(sets.Writes))
		if index >= 0 {
			return fmt.Errorf("intentcontrol: record write item %d: %w", index+1, err)
		}
		return fmt.Errorf("intentcontrol: record write items: %w", err)
	}
	for i, affected := range writeCounts {
		if affected == 0 {
			return fmt.Errorf("%w: proposal_write_item %s/%d/%d", ErrDuplicate, intentID, revision, i+1)
		}
	}

	effectStatements := make([]dbport.Statement, 0, len(sets.Effects))
	for i, e := range sets.Effects {
		effectStatements = append(effectStatements, dbport.Statement{SQL: `
			INSERT INTO proposal_effect_item (
				tenant_id, intent_id, revision, ordinal,
				effect_id, effect_kind, destination_ref, reversibility,
				compensation_ref, observation_ref)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT DO NOTHING`, Args: []any{
			tenantID, intentID, int64(revision), i + 1,
			e.EffectID, e.Kind, e.DestinationRef, e.Reversibility,
			e.CompensationRef, e.ObservationRef,
		}})
	}
	effectCounts, err := dbport.ExecAll(ctx, ex, effectStatements)
	if err != nil {
		index := dbport.FailedStatement(effectCounts, len(sets.Effects))
		if index >= 0 {
			return fmt.Errorf("intentcontrol: record effect item %d: %w", index+1, err)
		}
		return fmt.Errorf("intentcontrol: record effect items: %w", err)
	}
	for i, affected := range effectCounts {
		if affected == 0 {
			return fmt.Errorf("%w: proposal_effect_item %s/%d/%d", ErrDuplicate, intentID, revision, i+1)
		}
	}

	approvalStatements := make([]dbport.Statement, 0, len(sets.Approvals))
	for i, a := range sets.Approvals {
		approvalStatements = append(approvalStatements, dbport.Statement{SQL: `
			INSERT INTO proposal_approval_requirement (
				tenant_id, intent_id, revision, ordinal,
				requirement_id, separation_constraint, materiality_class)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING`, Args: []any{
			tenantID, intentID, int64(revision), i + 1,
			a.RequirementID, a.SeparationConstraint, a.MaterialityClass,
		}})
	}
	approvalCounts, err := dbport.ExecAll(ctx, ex, approvalStatements)
	if err != nil {
		index := dbport.FailedStatement(approvalCounts, len(sets.Approvals))
		if index >= 0 {
			return fmt.Errorf("intentcontrol: record approval requirement %d: %w", index+1, err)
		}
		return fmt.Errorf("intentcontrol: record approval requirements: %w", err)
	}
	for i, affected := range approvalCounts {
		if affected == 0 {
			return fmt.Errorf("%w: proposal_approval_requirement %s/%d/%d", ErrDuplicate, intentID, revision, i+1)
		}
	}

	obligationStatements := make([]dbport.Statement, 0, len(sets.Obligations))
	for i, o := range sets.Obligations {
		obligationStatements = append(obligationStatements, dbport.Statement{SQL: `
			INSERT INTO proposal_obligation (
				tenant_id, intent_id, revision, ordinal,
				obligation_id, obligation_kind, due_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING`, Args: []any{
			tenantID, intentID, int64(revision), i + 1,
			o.ObligationID, o.Kind, o.DueAt.UTC(),
		}})
	}
	obligationCounts, err := dbport.ExecAll(ctx, ex, obligationStatements)
	if err != nil {
		index := dbport.FailedStatement(obligationCounts, len(sets.Obligations))
		if index >= 0 {
			return fmt.Errorf("intentcontrol: record obligation %d: %w", index+1, err)
		}
		return fmt.Errorf("intentcontrol: record obligations: %w", err)
	}
	for i, affected := range obligationCounts {
		if affected == 0 {
			return fmt.Errorf("%w: proposal_obligation %s/%d/%d", ErrDuplicate, intentID, revision, i+1)
		}
	}
	return nil
}

// Load returns a revision's four sets in their stored ordinal order, which is
// the order the proposal digest was computed over.
func (s ProposalSetStore) Load(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64) (ProposalSets, error) {
	var out ProposalSets

	writes, err := ex.Query(ctx, `
		SELECT subject_kind, subject_id, resource_key, field_path,
			current_canonical_text, proposed_canonical_text,
			expected_revision, source_authority_decision
		FROM proposal_write_item
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3
		ORDER BY ordinal`, tenantID, intentID, int64(revision))
	if err != nil {
		return ProposalSets{}, fmt.Errorf("intentcontrol: load write items: %w", err)
	}
	for writes.Next() {
		var w WriteItem
		if err := writes.Scan(&w.SubjectKind, &w.SubjectID, &w.ResourceKey, &w.FieldPath,
			&w.CurrentCanonicalText, &w.ProposedCanonicalText,
			&w.ExpectedRevision, &w.SourceAuthorityDecision); err != nil {
			writes.Close()
			return ProposalSets{}, fmt.Errorf("intentcontrol: scan write item: %w", err)
		}
		out.Writes = append(out.Writes, w)
	}
	if err := writes.Err(); err != nil {
		writes.Close()
		return ProposalSets{}, fmt.Errorf("intentcontrol: load write items: %w", err)
	}
	writes.Close()

	effects, err := ex.Query(ctx, `
		SELECT effect_id, effect_kind, destination_ref, reversibility,
			compensation_ref, observation_ref
		FROM proposal_effect_item
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3
		ORDER BY ordinal`, tenantID, intentID, int64(revision))
	if err != nil {
		return ProposalSets{}, fmt.Errorf("intentcontrol: load effect items: %w", err)
	}
	for effects.Next() {
		var e EffectItem
		if err := effects.Scan(&e.EffectID, &e.Kind, &e.DestinationRef, &e.Reversibility,
			&e.CompensationRef, &e.ObservationRef); err != nil {
			effects.Close()
			return ProposalSets{}, fmt.Errorf("intentcontrol: scan effect item: %w", err)
		}
		out.Effects = append(out.Effects, e)
	}
	if err := effects.Err(); err != nil {
		effects.Close()
		return ProposalSets{}, fmt.Errorf("intentcontrol: load effect items: %w", err)
	}
	effects.Close()

	approvals, err := ex.Query(ctx, `
		SELECT requirement_id, separation_constraint, materiality_class
		FROM proposal_approval_requirement
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3
		ORDER BY ordinal`, tenantID, intentID, int64(revision))
	if err != nil {
		return ProposalSets{}, fmt.Errorf("intentcontrol: load approval requirements: %w", err)
	}
	for approvals.Next() {
		var a ApprovalRequirementItem
		if err := approvals.Scan(&a.RequirementID, &a.SeparationConstraint, &a.MaterialityClass); err != nil {
			approvals.Close()
			return ProposalSets{}, fmt.Errorf("intentcontrol: scan approval requirement: %w", err)
		}
		out.Approvals = append(out.Approvals, a)
	}
	if err := approvals.Err(); err != nil {
		approvals.Close()
		return ProposalSets{}, fmt.Errorf("intentcontrol: load approval requirements: %w", err)
	}
	approvals.Close()

	obligations, err := ex.Query(ctx, `
		SELECT obligation_id, obligation_kind, due_at
		FROM proposal_obligation
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3
		ORDER BY ordinal`, tenantID, intentID, int64(revision))
	if err != nil {
		return ProposalSets{}, fmt.Errorf("intentcontrol: load obligations: %w", err)
	}
	for obligations.Next() {
		var o ObligationItem
		if err := obligations.Scan(&o.ObligationID, &o.Kind, &o.DueAt); err != nil {
			obligations.Close()
			return ProposalSets{}, fmt.Errorf("intentcontrol: scan obligation: %w", err)
		}
		o.DueAt = o.DueAt.UTC()
		out.Obligations = append(out.Obligations, o)
	}
	if err := obligations.Err(); err != nil {
		obligations.Close()
		return ProposalSets{}, fmt.Errorf("intentcontrol: load obligations: %w", err)
	}
	obligations.Close()

	return out, nil
}

// Relationship types, matching the schema's closed vocabulary.
const (
	RelationChildOf    = "CHILD_OF"
	RelationSupersedes = "SUPERSEDES"
	RelationCorrects   = "CORRECTS"
	RelationRepairs    = "REPAIRS"
)

// Relationship is one intent_relationship row: a typed, immutable edge between
// two intents in the same tenant.
type Relationship struct {
	TenantID       uuid.UUID
	RelationshipID uuid.UUID

	Type   string
	Parent uuid.UUID
	Child  uuid.UUID

	Ordinal             uint32
	MaterialInputDigest string

	EstablishedAt time.Time
	RecordedAt    time.Time
}

// Validate rejects a relationship that could not be stored.
func (r Relationship) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", r.TenantID},
		{"relationship_id", r.RelationshipID},
		{"parent_intent_id", r.Parent},
		{"child_intent_id", r.Child},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	switch r.Type {
	case RelationChildOf, RelationSupersedes, RelationCorrects, RelationRepairs:
	default:
		return invalid("relationship_type", "type is not a declared relationship type")
	}
	if r.Parent == r.Child {
		return fmt.Errorf("%w: intent %s cannot be its own %s parent", ErrRelationshipCycle, r.Parent, r.Type)
	}
	if r.Ordinal == 0 {
		return invalid("ordinal", "ordinal starts at 1")
	}
	if err := requireDigest("material_input_digest", r.MaterialInputDigest); err != nil {
		return err
	}
	return requireInstant("established_at", r.EstablishedAt)
}

// RelationshipStore writes and reads intent_relationship.
type RelationshipStore struct{}

// Link inserts one relationship after proving it closes no cycle.
//
// The schema already refuses the one-hop cycle (parent = child) and a second
// parent for the same (type, child). What it cannot see is the longer cycle:
// with A -> B and B -> C already stored, inserting C -> A is three legal rows
// that together make the graph unwalkable. Link therefore walks up from the
// proposed parent through its own ancestors of the same type, inside the
// caller's transaction, and refuses ([ErrRelationshipCycle]) if it reaches the
// proposed child. Because each child has at most one parent per type, that walk
// is a chain, not a search, and it terminates at the root or at the cycle it is
// looking for.
func (s RelationshipStore) Link(ctx context.Context, ex Executor, in Relationship) error {
	if err := in.Validate(); err != nil {
		return err
	}
	// Walk ancestors of the proposed parent. The visited set is belt and braces:
	// a chain built only through this method cannot already contain a cycle, but
	// a walk that would loop forever on corrupted data is not a walk worth
	// shipping.
	visited := map[uuid.UUID]bool{in.Parent: true}
	cursor := in.Parent
	for {
		var next uuid.UUID
		err := ex.QueryRow(ctx, `
			SELECT parent_intent_id FROM intent_relationship
			WHERE tenant_id = $1 AND relationship_type = $2 AND child_intent_id = $3`,
			in.TenantID, in.Type, cursor).Scan(&next)
		if err != nil {
			if isNoRows(err) {
				break // cursor is a root: no cycle on this chain.
			}
			return fmt.Errorf("intentcontrol: walk %s ancestors of %s: %w", in.Type, cursor, err)
		}
		if next == in.Child {
			return fmt.Errorf("%w: %s -> %s would close a %s cycle through %s",
				ErrRelationshipCycle, in.Parent, in.Child, in.Type, cursor)
		}
		if visited[next] {
			return fmt.Errorf("%w: %s ancestry of %s already contains a cycle at %s",
				ErrRelationshipCycle, in.Type, in.Parent, next)
		}
		visited[next] = true
		cursor = next
	}

	affected, err := ex.Exec(ctx, `
		INSERT INTO intent_relationship (
			tenant_id, relationship_id, relationship_type,
			parent_intent_id, child_intent_id,
			ordinal, material_input_digest, established_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.RelationshipID, in.Type,
		in.Parent, in.Child, int32(in.Ordinal), in.MaterialInputDigest, in.EstablishedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: link %s -> %s: %w", in.Parent, in.Child, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: intent_relationship %s %s -> %s", ErrDuplicate, in.Type, in.Parent, in.Child)
	}
	return nil
}

// Children returns a parent's children of one relationship type, in ordinal
// order.
func (s RelationshipStore) Children(ctx context.Context, ex Executor, tenantID, parent uuid.UUID, relType string) ([]uuid.UUID, error) {
	rows, err := ex.Query(ctx, `
		SELECT child_intent_id FROM intent_relationship
		WHERE tenant_id = $1 AND parent_intent_id = $2 AND relationship_type = $3
		ORDER BY ordinal`, tenantID, parent, relType)
	if err != nil {
		return nil, fmt.Errorf("intentcontrol: load children of %s: %w", parent, err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var child uuid.UUID
		if err := rows.Scan(&child); err != nil {
			return nil, fmt.Errorf("intentcontrol: scan child: %w", err)
		}
		out = append(out, child)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intentcontrol: load children of %s: %w", parent, err)
	}
	return out, nil
}

// Result kinds, matching the schema's closed vocabulary.
const (
	ResultSimulated = "SIMULATED"
	ResultCommitted = "COMMITTED"
	ResultRejected  = "REJECTED"
	ResultCorrected = "CORRECTED"
	ResultAbandoned = "ABANDONED"
)

// Result is one intent_result row: what a revision produced. Its primary key is
// the revision, so a second result for the same revision collides rather than
// overwriting -- the RED clause's "result overwrites revision".
type Result struct {
	TenantID uuid.UUID
	IntentID uuid.UUID
	Revision uint64

	Kind           string
	ResultDigest   string
	ProposalDigest string
	ControlDigest  string

	SchemaRef string
	Body      json.RawMessage

	ProducedBy string
	ProducedAt time.Time
	RecordedAt time.Time
}

// Validate rejects a result that could not be stored.
func (r Result) Validate() error {
	if err := requireID("tenant_id", r.TenantID); err != nil {
		return err
	}
	if err := requireID("intent_id", r.IntentID); err != nil {
		return err
	}
	if r.Revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	switch r.Kind {
	case ResultSimulated, ResultCommitted, ResultRejected, ResultCorrected, ResultAbandoned:
	default:
		return invalid("result_kind", "kind is not a declared result kind")
	}
	for _, req := range []struct{ field, value string }{
		{"result_digest", r.ResultDigest},
		{"proposal_digest", r.ProposalDigest},
		{"control_digest", r.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireText("schema_ref", r.SchemaRef); err != nil {
		return err
	}
	if err := requireText("produced_by", r.ProducedBy); err != nil {
		return err
	}
	if err := requireInstant("produced_at", r.ProducedAt); err != nil {
		return err
	}
	return requireJSONObject("result_body", r.Body)
}

// ResultStore writes and reads intent_result.
type ResultStore struct{}

// Record inserts one immutable result for a revision.
func (s ResultStore) Record(ctx context.Context, ex Executor, in Result) error {
	if err := in.Validate(); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO intent_result (
			tenant_id, intent_id, revision,
			result_kind, result_digest, proposal_digest, control_digest,
			schema_ref, result_body, produced_by, produced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.IntentID, int64(in.Revision),
		in.Kind, in.ResultDigest, in.ProposalDigest, in.ControlDigest,
		in.SchemaRef, string(in.Body), in.ProducedBy, in.ProducedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: record result %s/%d: %w", in.IntentID, in.Revision, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: intent_result %s/%d", ErrDuplicate, in.IntentID, in.Revision)
	}
	return nil
}

// Load returns the result of one revision.
func (s ResultStore) Load(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64) (Result, error) {
	var (
		out     Result
		stored  int64
		bodyRaw string
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, intent_id, revision,
			result_kind, result_digest, proposal_digest, control_digest,
			schema_ref, result_body::text, produced_by, produced_at, recorded_at
		FROM intent_result
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3`,
		tenantID, intentID, int64(revision)).Scan(
		&out.TenantID, &out.IntentID, &stored,
		&out.Kind, &out.ResultDigest, &out.ProposalDigest, &out.ControlDigest,
		&out.SchemaRef, &bodyRaw, &out.ProducedBy, &out.ProducedAt, &out.RecordedAt)
	if err != nil {
		if isNoRows(err) {
			return Result{}, fmt.Errorf("%w: intent_result %s/%d", ErrNotFound, intentID, revision)
		}
		return Result{}, fmt.Errorf("intentcontrol: load result %s/%d: %w", intentID, revision, err)
	}
	out.Revision = uint64(stored)
	out.Body = json.RawMessage(bodyRaw)
	out.ProducedAt = out.ProducedAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

// Decision kinds and outcomes, matching the schema's closed vocabularies.
const (
	DecisionHumanApproval = "HUMAN_APPROVAL"
	DecisionPolicy        = "POLICY"
	DecisionAuthZ         = "AUTHZ"
	DecisionLegal         = "LEGAL"
	DecisionRisk          = "RISK"

	OutcomeApproved  = "APPROVED"
	OutcomeRejected  = "REJECTED"
	OutcomeAbstained = "ABSTAINED"
	OutcomeDelegated = "DELEGATED"
)

// Decision is one intent_decision row: a governance or human decision bound to
// an exact proposal revision and the control context it was seen under.
type Decision struct {
	TenantID   uuid.UUID
	DecisionID uuid.UUID
	IntentID   uuid.UUID
	Revision   uint64

	RequirementID    string
	Kind             string
	Outcome          string
	ProposalDigest   string
	ControlDigest    string
	MaterialityClass string

	DecidedBy    string
	AuthorityRef string
	Reason       string
	DecidedAt    time.Time
	RecordedAt   time.Time
}

// Validate rejects a decision that could not be stored.
func (d Decision) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", d.TenantID},
		{"decision_id", d.DecisionID},
		{"intent_id", d.IntentID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	if d.Revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	switch d.Kind {
	case DecisionHumanApproval, DecisionPolicy, DecisionAuthZ, DecisionLegal, DecisionRisk:
	default:
		return invalid("decision_kind", "kind is not a declared decision kind")
	}
	switch d.Outcome {
	case OutcomeApproved, OutcomeRejected, OutcomeAbstained, OutcomeDelegated:
	default:
		return invalid("decision_outcome", "outcome is not a declared decision outcome")
	}
	switch d.MaterialityClass {
	case Material, NonMaterial:
	default:
		return invalid("materiality_class", "value is not MATERIAL or NON_MATERIAL")
	}
	for _, req := range []struct{ field, value string }{
		{"requirement_id", d.RequirementID},
		{"decided_by", d.DecidedBy},
		{"authority_ref", d.AuthorityRef},
		{"decision_reason", d.Reason},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"proposal_digest", d.ProposalDigest},
		{"control_digest", d.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	return requireInstant("decided_at", d.DecidedAt)
}

// DecisionStore writes and reads intent_decision.
type DecisionStore struct{}

// Record inserts one decision. A second decision by the same principal on the
// same requirement of the same revision is [ErrDuplicate]: one vote each.
func (s DecisionStore) Record(ctx context.Context, ex Executor, in Decision) error {
	if err := in.Validate(); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO intent_decision (
			tenant_id, decision_id, intent_id, revision,
			requirement_id, decision_kind, decision_outcome,
			proposal_digest, control_digest, materiality_class,
			decided_by, authority_ref, decision_reason, decided_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.DecisionID, in.IntentID, int64(in.Revision),
		in.RequirementID, in.Kind, in.Outcome,
		in.ProposalDigest, in.ControlDigest, in.MaterialityClass,
		in.DecidedBy, in.AuthorityRef, in.Reason, in.DecidedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: record decision %s: %w", in.DecisionID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: intent_decision %s/%d/%s by %s",
			ErrDuplicate, in.IntentID, in.Revision, in.RequirementID, in.DecidedBy)
	}
	return nil
}

// CountForRevision returns how many decisions a revision carries. It is the one
// aggregate this store answers, because "has every requirement been decided" is
// a question the approval engine asks and this package must not answer.
func (s DecisionStore) CountForRevision(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64) (int, error) {
	var count int
	if err := ex.QueryRow(ctx, `
		SELECT count(*) FROM intent_decision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3`,
		tenantID, intentID, int64(revision)).Scan(&count); err != nil {
		return 0, fmt.Errorf("intentcontrol: count decisions %s/%d: %w", intentID, revision, err)
	}
	return count, nil
}

// Certificate kinds, matching the schema's closed vocabulary.
const (
	CertificatePreflight  = "PREFLIGHT"
	CertificateSimulation = "SIMULATION"
	CertificateGovernance = "GOVERNANCE"
	CertificateApproval   = "APPROVAL"
	CertificateCommit     = "COMMIT"
)

// Certificate is one intent_certificate row: a signed statement that a named
// check passed against an exact revision, with the bytes in the governed
// artifact store rather than inline.
type Certificate struct {
	TenantID      uuid.UUID
	CertificateID uuid.UUID
	IntentID      uuid.UUID
	Revision      uint64

	Kind           string
	Digest         string
	ProposalDigest string
	ControlDigest  string

	ArtifactRef string
	IssuedBy    string
	IssuedAt    time.Time
	ValidUntil  time.Time
	RecordedAt  time.Time
}

// Validate rejects a certificate that could not be stored.
func (c Certificate) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", c.TenantID},
		{"certificate_id", c.CertificateID},
		{"intent_id", c.IntentID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	if c.Revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	switch c.Kind {
	case CertificatePreflight, CertificateSimulation, CertificateGovernance,
		CertificateApproval, CertificateCommit:
	default:
		return invalid("certificate_kind", "kind is not a declared certificate kind")
	}
	for _, req := range []struct{ field, value string }{
		{"certificate_digest", c.Digest},
		{"proposal_digest", c.ProposalDigest},
		{"control_digest", c.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"artifact_ref", c.ArtifactRef},
		{"issued_by", c.IssuedBy},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireInstant("issued_at", c.IssuedAt); err != nil {
		return err
	}
	if !c.ValidUntil.IsZero() && !c.ValidUntil.After(c.IssuedAt) {
		return invalid("valid_until", "a certificate may not expire before it is issued")
	}
	return nil
}

// CertificateStore writes and reads intent_certificate.
type CertificateStore struct{}

// Issue inserts one certificate. A second certificate of the same kind for the
// same revision is [ErrDuplicate] -- it would be either a duplicate or a
// contradiction, and neither is resolved by overwriting.
func (s CertificateStore) Issue(ctx context.Context, ex Executor, in Certificate) error {
	if err := in.Validate(); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO intent_certificate (
			tenant_id, certificate_id, intent_id, revision,
			certificate_kind, certificate_digest, proposal_digest, control_digest,
			artifact_ref, issued_by, issued_at, valid_until)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.CertificateID, in.IntentID, int64(in.Revision),
		in.Kind, in.Digest, in.ProposalDigest, in.ControlDigest,
		in.ArtifactRef, in.IssuedBy, in.IssuedAt.UTC(), nullableInstant(in.ValidUntil))
	if err != nil {
		return fmt.Errorf("intentcontrol: issue certificate %s: %w", in.CertificateID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: intent_certificate %s/%d/%s", ErrDuplicate, in.IntentID, in.Revision, in.Kind)
	}
	return nil
}
