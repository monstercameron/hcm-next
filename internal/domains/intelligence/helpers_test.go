package intelligence_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	policyVersion = "authz.policy/2026.1"
	purpose       = "workforce_administration"
	ledgerStream  = "ledger"
)

// instantAt parses an RFC3339 timestamp.
func instantAt(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(parsed)
}

// knownAt wraps an RFC3339 timestamp as a knowledge time.
func knownAt(t *testing.T, text string) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(instantAt(t, text))
	if err != nil {
		t.Fatalf("known at %q: %v", text, err)
	}
	return k
}

// recordedAt wraps an RFC3339 timestamp as a recorded time.
func recordedAt(t *testing.T, text string) values.RecordedAt {
	t.Helper()
	r, err := values.NewRecordedAt(instantAt(t, text))
	if err != nil {
		t.Fatalf("recorded at %q: %v", text, err)
	}
	return r
}

// ledgerRevision builds a sequence revision on the ledger stream.
func ledgerRevision(t *testing.T, seq uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision(ledgerStream, seq)
	if err != nil {
		t.Fatalf("revision %d: %v", seq, err)
	}
	return r
}

// transactionID is the fixture transaction identity. Entity ids are canonical
// ULIDs, so the fixture uses one rather than a readable slug.
var transactionID = "01K3TXN" + strings.Repeat("0", 18) + "1"

// transactionRef is the fixture business transaction.
func transactionRef(t *testing.T) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{
		Tenant: fixtures.Tenant,
		Kind:   intelligence.KindTransaction,
		Id:     transactionID,
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("fixture transaction ref: %v", err)
	}
	return ref
}

// workerRef is the fixture subject of the transaction.
func workerRef(t *testing.T) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatalf("fixture worker: %v", err)
	}
	return ref
}

// fixtureRecord is the whole recorded evidence of one P1A promotion
// transaction: what was requested, what the platform decided, what it wrote,
// what it did not emit, what was observed afterwards, and what was reconciled
// and planned against it.
func fixtureRecord(t *testing.T) intelligence.TransactionRecord {
	t.Helper()
	return intelligence.TransactionRecord{
		Transaction: transactionRef(t),
		Exists:      true,
		Request: intelligence.Request{
			IntentType:    "hcmnext.people.promote_worker",
			IntentVersion: "v1",
			Family:        "CHANGE_REQUEST",
			Mode:          "SIMULATE",
			Purpose:       purpose,
			Requester: intelligence.PrincipalRef{
				Kind: "HUMAN", Id: "u-42", OnBehalfOf: "u-17",
			},
			RequestedAt:    recordedAt(t, "2026-08-01T09:00:00Z"),
			RequestDigest:  "sha256:request-1",
			ProposalDigest: "sha256:proposal-1",
			Subjects:       []values.EntityRef{workerRef(t)},
		},
		Controls: []evidence.ControlVersion{
			{Name: "promotion_rule_pack", Version: "promotion.rules/1.0.0"},
			{Name: "authorization_policy", Version: policyVersion},
		},
		Transitions: []intelligence.Transition{
			{
				Dimension: "REQUEST_STATE", From: "DRAFT", To: "PREFLIGHTED",
				At:       recordedAt(t, "2026-08-01T09:01:00Z"),
				Actor:    intelligence.PrincipalRef{Kind: "SERVICE", Id: "intent-kernel"},
				Reason:   "preflight_passed",
				Sequence: 1, Digest: "sha256:transition-1",
			},
			{
				Dimension: "REQUEST_STATE", From: "PREFLIGHTED", To: "SIMULATED",
				At:       recordedAt(t, "2026-08-01T09:02:00Z"),
				Actor:    intelligence.PrincipalRef{Kind: "SERVICE", Id: "intent-kernel"},
				Reason:   "simulation_completed",
				Sequence: 2, Digest: "sha256:transition-2",
			},
		},
		WriteSet: []intelligence.WriteSetEntry{
			{
				Resource: "intent/txn-2026-08-0001", Kind: "INTENT_RECORD",
				Revision: ledgerRevision(t, 9), Digest: "sha256:write-1",
			},
		},
		Events: []intelligence.LedgerEvent{
			{
				ID: "evt-1", Type: "IntentSimulated",
				At:       recordedAt(t, "2026-08-01T09:02:00Z"),
				Revision: ledgerRevision(t, 10), Digest: "sha256:event-1",
			},
		},
		Effects: []intelligence.EffectRecord{
			{
				ID: "eff-1", Channel: "outbox", State: "NOT_ENQUEUED",
				At: recordedAt(t, "2026-08-01T09:03:00Z"), Digest: "sha256:effect-1",
			},
		},
		Observations: []intelligence.ObservationLink{
			{
				ID: "obs-1", Source: "incumbent-hris",
				At:     recordedAt(t, "2026-08-01T09:30:00Z"),
				Digest: "sha256:observation-1",
				Authority: evidence.SourceAuthority{
					Kind:      evidence.AuthorityExternalObservation,
					System:    "incumbent-hris",
					PolicyRef: "authority.by_field/2026.1",
				},
				Basis: intelligence.BasisRecordedReference,
			},
		},
		Reconciliations: []intelligence.ReconciliationLink{
			{
				ID: "rec-1", Outcome: "MISMATCH",
				At: recordedAt(t, "2026-08-01T10:00:00Z"), Digest: "sha256:reconciliation-1",
				Basis: intelligence.BasisRecordedReference,
			},
		},
		Repairs: []intelligence.RepairLink{
			{
				PlanID: "plan-1", PlanDigest: "sha256:plan-1", Executable: false,
				At: recordedAt(t, "2026-08-01T10:05:00Z"), Digest: "sha256:repair-1",
				Basis: intelligence.BasisRecordedReference,
			},
		},
		Projections: []intelligence.ProjectionLink{
			{
				Name: "worker_state", Version: "1.0.0",
				Revision: ledgerRevision(t, 8), Digest: "sha256:projection-stale",
			},
			{
				Name: "promotion_queue", Version: "1.0.0",
				Revision: ledgerRevision(t, 10), Digest: "sha256:projection-current",
			},
		},
		EvidenceRefs: []intelligence.EvidenceLink{
			{Ref: "receipt:zero-effect-1", Kind: "ZERO_EFFECT_RECEIPT"},
		},
		LedgerHead: ledgerRevision(t, 10),
		Watermark:  ledgerRevision(t, 10),
		Gaps: []intelligence.Gap{
			{Section: intelligence.SectionEffects, Reason: "provider_log_retention_expired"},
		},
	}
}

// memoryHistory is an in-memory intelligence.TransactionHistory that serves
// exactly the sections it is asked for.
type memoryHistory struct {
	record intelligence.TransactionRecord
	// Absent makes the store answer that the transaction does not exist.
	Absent bool
	// Widen makes the store answer with a section nobody asked for.
	Widen intelligence.Section
	// Calls counts reads, and LastSections records the projection asked for.
	Calls        int
	LastSections []intelligence.Section
}

// TransactionRecordAt implements intelligence.TransactionHistory.
func (m *memoryHistory) TransactionRecordAt(
	_ context.Context,
	q intelligence.TransactionQuery,
) (intelligence.TransactionRecord, error) {
	if err := q.Validate(); err != nil {
		return intelligence.TransactionRecord{}, err
	}
	m.Calls++
	m.LastSections = append([]intelligence.Section(nil), q.Sections...)
	if m.Absent {
		return intelligence.TransactionRecord{Transaction: q.Transaction, Exists: false}, nil
	}

	wanted := make(map[intelligence.Section]struct{}, len(q.Sections))
	for _, s := range q.Sections {
		wanted[s] = struct{}{}
	}
	if m.Widen != "" {
		wanted[m.Widen] = struct{}{}
	}
	full := m.record
	out := intelligence.TransactionRecord{
		Transaction: full.Transaction,
		Exists:      true,
		LedgerHead:  full.LedgerHead,
		Watermark:   full.Watermark,
	}
	has := func(s intelligence.Section) bool { _, ok := wanted[s]; return ok }
	if has(intelligence.SectionRequest) {
		out.Request = full.Request
	}
	if has(intelligence.SectionGovernance) {
		out.Controls = full.Controls
	}
	if has(intelligence.SectionLifecycle) {
		out.Transitions = full.Transitions
	}
	if has(intelligence.SectionWriteSet) {
		out.WriteSet = full.WriteSet
	}
	if has(intelligence.SectionEvents) {
		out.Events = full.Events
	}
	if has(intelligence.SectionEffects) {
		out.Effects = full.Effects
		out.Gaps = full.Gaps
	}
	if has(intelligence.SectionObservations) {
		out.Observations = full.Observations
	}
	if has(intelligence.SectionReconciliation) {
		out.Reconciliations = full.Reconciliations
	}
	if has(intelligence.SectionRepair) {
		out.Repairs = full.Repairs
	}
	if has(intelligence.SectionProjections) {
		out.Projections = full.Projections
	}
	if has(intelligence.SectionEvidence) {
		out.EvidenceRefs = full.EvidenceRefs
	}
	return out, nil
}

// store builds the in-memory history over the fixture record.
func store(t *testing.T) *memoryHistory {
	t.Helper()
	return &memoryHistory{record: fixtureRecord(t)}
}

// allowAll builds a decision that allows every section and every redactable
// field.
func allowAll() intelligence.AuthorizationDecision {
	d := intelligence.AuthorizationDecision{
		PolicyVersion:          policyVersion,
		Purpose:                purpose,
		TransactionDisclosable: true,
		Sections:               map[intelligence.Section]intelligence.Ruling{},
		Fields:                 map[string]intelligence.Ruling{},
	}
	for _, s := range intelligence.AllSections() {
		d.Sections[s] = intelligence.Ruling{Effect: intelligence.EffectAllow}
	}
	for _, f := range intelligence.RedactableFields() {
		d.Fields[f] = intelligence.Ruling{Effect: intelligence.EffectAllow}
	}
	return d
}

// denySection turns one section of a decision into a denial.
func denySection(
	d intelligence.AuthorizationDecision,
	section intelligence.Section,
	reason string,
) intelligence.AuthorizationDecision {
	out := d
	out.Sections = map[intelligence.Section]intelligence.Ruling{}
	for k, v := range d.Sections {
		out.Sections[k] = v
	}
	out.Sections[section] = intelligence.Ruling{Effect: intelligence.EffectDeny, Reason: reason}
	return out
}

// denyRequestField turns one redactable field into a denial.
func denyRequestField(
	d intelligence.AuthorizationDecision,
	field, reason string,
) intelligence.AuthorizationDecision {
	out := d
	out.Fields = map[string]intelligence.Ruling{}
	for k, v := range d.Fields {
		out.Fields[k] = v
	}
	out.Fields[field] = intelligence.Ruling{Effect: intelligence.EffectDeny, Reason: reason}
	return out
}

// withheldTransaction turns a decision into a non-disclosable transaction.
func withheldTransaction(
	d intelligence.AuthorizationDecision,
	reason string,
) intelligence.AuthorizationDecision {
	out := d
	out.TransactionDisclosable = false
	out.DenialReason = reason
	return out
}

// explainRequest is the fixture explanation request.
func explainRequest(t *testing.T, auth intelligence.AuthorizationDecision) intelligence.ExplainTransactionRequest {
	t.Helper()
	return intelligence.ExplainTransactionRequest{
		Tenant:        fixtures.Tenant,
		Transaction:   transactionRef(t),
		AsKnownAt:     knownAt(t, "2026-08-02T00:00:00Z"),
		Authorization: auth,
	}
}
