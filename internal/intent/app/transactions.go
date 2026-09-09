package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// transactionStreamKey names the ledger stream an intent's chronology is read
// from, for the revision tokens the explanation pins its events to.
const transactionStreamKey = "intent"

// ledgerTransactions is the [intelligence.TransactionHistory] this cell serves
// explain_transaction from: the authoritative ledger, read back through the
// same [Store] port every other answer uses.
//
// It reconstructs, it does not narrate. Every element it returns is something
// the chronology actually recorded - the stored request envelope, the pinned
// control snapshots, the lifecycle tuple the instance is in, the ledger events
// and their digests - and the sections P1A has nothing recorded for (write
// set, effects, observations, reconciliation, repair, projections) come back
// as declared gaps rather than as empty lists that read like "nothing
// happened". In a release that commits no effect, "there is no effect record"
// and "we did not look" must stay distinguishable.
type ledgerTransactions struct {
	store Store
	defs  *intent.Registry
}

var _ intelligence.TransactionHistory = (*ledgerTransactions)(nil)

// sectionsWithoutP1ARecords are the sections whose evidence only exists once a
// release has execution authority. They are reported as gaps whenever they are
// asked for.
func sectionsWithoutP1ARecords() []intelligence.Section {
	return []intelligence.Section{
		intelligence.SectionWriteSet,
		intelligence.SectionEffects,
		intelligence.SectionObservations,
		intelligence.SectionReconciliation,
		intelligence.SectionRepair,
		intelligence.SectionProjections,
	}
}

// TransactionRecordAt implements [intelligence.TransactionHistory].
func (l *ledgerTransactions) TransactionRecordAt(ctx context.Context, q intelligence.TransactionQuery) (intelligence.TransactionRecord, error) {
	rec, err := l.store.LoadIntent(ctx, string(q.Tenant), q.Transaction.Id)
	if err != nil {
		if errors.Is(err, ErrIntentNotFound) {
			return intelligence.TransactionRecord{Transaction: q.Transaction, Exists: false}, nil
		}
		return intelligence.TransactionRecord{}, err
	}
	inst, err := decodeEnvelope(rec.Envelope)
	if err != nil {
		return intelligence.TransactionRecord{}, err
	}
	entries, err := l.store.Timeline(ctx, string(q.Tenant), q.Transaction.Id)
	if err != nil {
		return intelligence.TransactionRecord{}, err
	}

	requested := make(map[intelligence.Section]struct{}, len(q.Sections))
	for _, s := range q.Sections {
		requested[s] = struct{}{}
	}
	wants := func(s intelligence.Section) bool {
		_, ok := requested[s]
		return ok
	}

	head := values.UnspecifiedRevision()
	var events []intelligence.LedgerEvent
	var refs []intelligence.EvidenceLink
	for _, entry := range entries {
		// The knowledge cut-off is applied by the port: an event recorded
		// after the caller's horizon is invisible, which is what makes a
		// replayed explanation reproduce the belief held at that instant
		// rather than today's.
		if entry.RecordedAt.After(q.KnownAt.Instant().Time()) {
			continue
		}
		revision, revErr := values.NewSequenceRevision(transactionStreamKey, uint64(entry.Sequence))
		if revErr != nil {
			return intelligence.TransactionRecord{}, fmt.Errorf("app: ledger revision: %w", revErr)
		}
		head = revision
		recordedAt, recErr := values.NewRecordedAt(values.NewInstant(entry.RecordedAt))
		if recErr != nil {
			return intelligence.TransactionRecord{}, fmt.Errorf("app: ledger recorded time: %w", recErr)
		}
		events = append(events, intelligence.LedgerEvent{
			ID:       entry.EventID,
			Type:     entry.Kind,
			At:       recordedAt,
			Revision: revision,
			Digest:   entry.DigestAlgorithm + ":" + entry.Digest,
		})
		refs = append(refs, intelligence.EvidenceLink{
			Ref:  entry.DigestAlgorithm + ":" + entry.Digest,
			Kind: "ledger_event",
		})
	}
	if !head.IsSpecified() {
		// A recorded intent always has at least the event that recorded it.
		// A chronology with none is a store that lost it, not a transaction
		// with nothing to say, so this refuses rather than answering.
		return intelligence.TransactionRecord{}, fmt.Errorf(
			"app: intent %s has no visible ledger chronology", q.Transaction.Id)
	}

	record := intelligence.TransactionRecord{
		Transaction: q.Transaction,
		Exists:      true,
		LedgerHead:  head,
		Watermark:   head,
	}
	if wants(intelligence.SectionEvents) {
		record.Events = events
	}
	if wants(intelligence.SectionEvidence) {
		record.EvidenceRefs = append(refs, intelligence.EvidenceLink{
			Ref:  inst.CanonicalRequestDigest.AlgorithmID + ":" + inst.CanonicalRequestDigest.Digest,
			Kind: "canonical_request_digest",
		})
	}
	if wants(intelligence.SectionRequest) {
		request, reqErr := l.requestOf(inst)
		if reqErr != nil {
			return intelligence.TransactionRecord{}, reqErr
		}
		record.Request = request
	}
	if wants(intelligence.SectionGovernance) {
		record.Controls = controlVersions(inst)
	}
	if wants(intelligence.SectionLifecycle) {
		transitions, transErr := transitionsOf(inst)
		if transErr != nil {
			return intelligence.TransactionRecord{}, transErr
		}
		record.Transitions = transitions
	}
	for _, section := range sectionsWithoutP1ARecords() {
		if wants(section) {
			record.Gaps = append(record.Gaps, intelligence.Gap{
				Section: section,
				Reason:  "p1a_release_records_no_evidence_for_this_section",
			})
		}
	}
	return record, nil
}

// requestOf projects the stored intent envelope onto the explanation's request
// section.
func (l *ledgerTransactions) requestOf(inst intent.Instance) (intelligence.Request, error) {
	def, err := l.defs.Resolve(inst.Definition)
	if err != nil {
		return intelligence.Request{}, err
	}
	recordedAt, err := values.NewRecordedAt(inst.RecordedAt)
	if err != nil {
		return intelligence.Request{}, fmt.Errorf("app: intent recorded time: %w", err)
	}
	subjects := make([]values.EntityRef, 0, len(inst.Subjects))
	for _, s := range inst.Subjects {
		ref := values.EntityRef{Tenant: inst.Tenant, Kind: subjectKind(s.Kind), Id: s.SubjectID}
		if ref.Validate() != nil {
			continue
		}
		subjects = append(subjects, ref)
	}
	return intelligence.Request{
		IntentType:     inst.Definition.TypeID,
		IntentVersion:  fmt.Sprintf("v%d", inst.Definition.Version),
		Family:         def.Family.String(),
		Mode:           inst.ExecutionMode.String(),
		Purpose:        inst.Purpose,
		Requester:      intelligence.PrincipalRef{Kind: inst.Initiator.Kind.String(), Id: inst.Initiator.PrincipalID},
		RequestedAt:    recordedAt,
		RequestDigest:  inst.CanonicalRequestDigest.AlgorithmID + ":" + inst.CanonicalRequestDigest.Digest,
		ProposalDigest: latestProposalDigest(inst),
		Subjects:       subjects,
	}, nil
}

// latestProposalDigest returns the material digest of the newest recorded
// proposal revision, or empty when the intent bound none. P1A binds no
// approval, so this is normally empty and says so rather than inventing one.
func latestProposalDigest(inst intent.Instance) string {
	if len(inst.ProposalRevisions) == 0 {
		return ""
	}
	rev := inst.ProposalRevisions[len(inst.ProposalRevisions)-1]
	return rev.MaterialDigest.AlgorithmID + ":" + rev.MaterialDigest.Digest
}

// subjectKind maps a declared subject kind onto the canonical entity kind
// vocabulary, which is lowercase.
func subjectKind(kind string) values.Kind {
	out := make([]byte, 0, len(kind))
	for i := 0; i < len(kind); i++ {
		c := kind[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			out = append(out, c)
		}
	}
	return values.Kind(out)
}

// controlVersions renders the pinned control snapshots as the governance
// section's control list. Only the snapshots the instance actually pinned
// appear: an empty digest is a control this definition never reached, and
// listing it with an empty version would claim a pin nobody made.
func controlVersions(inst intent.Instance) []evidence.ControlVersion {
	c := inst.ControlSnapshots
	candidates := []evidence.ControlVersion{
		{Name: "capability_registry", Version: c.CapabilityRegistryDigest},
		{Name: "policy_bundle", Version: c.PolicyBundleDigest},
		{Name: "legal_context", Version: c.LegalContextDigest},
		{Name: "entitlement", Version: c.EntitlementDigest},
		{Name: "reference_data", Version: c.ReferenceDataDigest},
		{Name: "classification_taxonomy", Version: c.ClassificationTaxonomyDigest},
		{Name: "classification_label_set", Version: c.ClassificationLabelSetDigest},
		{Name: "dlp_decision", Version: c.DLPDecisionDigest},
		{Name: "source_authority", Version: inst.SourceAuthoritySnapshotDigest},
		{Name: "risk_context", Version: inst.RiskContextDigest},
	}
	out := make([]evidence.ControlVersion, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Version != "" {
			out = append(out, candidate)
		}
	}
	return out
}

// transitionsOf renders the instance's five lifecycle dimensions as recorded
// transitions.
//
// P1A stores the tuple an intent is in, not a per-dimension move log, so each
// dimension is reported once, at the instance's last transition time, with the
// state it is actually in. That is what the record can prove; inventing a
// from-state for a move nobody recorded would be a narrative, not evidence.
func transitionsOf(inst intent.Instance) ([]intelligence.Transition, error) {
	at, err := values.NewRecordedAt(inst.LastTransitionAt)
	if err != nil {
		return nil, fmt.Errorf("app: intent transition time: %w", err)
	}
	actor := intelligence.PrincipalRef{Kind: inst.Initiator.Kind.String(), Id: inst.Initiator.PrincipalID}
	digest := inst.CanonicalRequestDigest.AlgorithmID + ":" + inst.CanonicalRequestDigest.Digest
	dims := lifecycle.AllDimensions()
	out := make([]intelligence.Transition, 0, len(dims))
	for i, dim := range dims {
		out = append(out, intelligence.Transition{
			Dimension: string(dim),
			To:        string(inst.Lifecycle.State(dim)),
			At:        at,
			Actor:     actor,
			Reason:    "intent.recorded",
			Sequence:  uint64(i + 1),
			Digest:    digest,
		})
	}
	return out, nil
}
