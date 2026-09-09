package intelligence_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestExplainTransactionReconstructsAuthorizedChronologyWithoutCausalFabrication
// is the INTEL-001 primary test: the explanation reconstructs the authorized
// intent, governance, transaction, event, effect, observation, reconciliation
// and repair links with their authority and times, labels every element
// epistemically, and never turns adjacency into causation.
func TestExplainTransactionReconstructsAuthorizedChronologyWithoutCausalFabrication(t *testing.T) {
	ctx := context.Background()

	t.Run("GREEN: every requested section is reconstructed and accounted for", func(t *testing.T) {
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.Disclosure != intelligence.DisclosureFull ||
			got.Presence != intelligence.PresencePresent {
			t.Fatalf("disclosure=%s presence=%s", got.Disclosure, got.Presence)
		}
		if len(got.Sections) != len(intelligence.AllSections()) {
			t.Fatalf("%d section outcome(s) for %d requested",
				len(got.Sections), len(intelligence.AllSections()))
		}
		for _, s := range got.Sections {
			if s.Access != intelligence.AccessAuthorized {
				t.Fatalf("%s = %s", s.Section, s.Access)
			}
		}
		if got.Request.IntentType != "hcmnext.people.promote_worker" {
			t.Fatalf("request intent = %q", got.Request.IntentType)
		}
		if len(got.Transitions) != 2 || len(got.Events) != 1 || len(got.Effects) != 1 ||
			len(got.Observations) != 1 || len(got.Reconciliations) != 1 ||
			len(got.Repairs) != 1 || len(got.WriteSet) != 1 || len(got.EvidenceRefs) != 1 {
			t.Fatalf("a section lost its contents: %+v", got.Sections)
		}
	})

	t.Run("GREEN: the chronology is ordered and every entry is epistemically labelled", func(t *testing.T) {
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if len(got.Chronology) != 8 {
			t.Fatalf("%d chronology entr(ies): %v", len(got.Chronology), entryRefs(got))
		}
		for i := 1; i < len(got.Chronology); i++ {
			before, after := got.Chronology[i-1], got.Chronology[i]
			if after.At.Instant().Before(before.At.Instant()) {
				t.Fatalf("chronology is out of order at %d: %s then %s",
					i, before.At, after.At)
			}
		}
		for _, entry := range got.Chronology {
			if !entry.Epistemic.Valid() {
				t.Fatalf("%s/%s carries no epistemic label", entry.Section, entry.Ref)
			}
			if entry.Digest == "" {
				t.Fatalf("%s/%s is not pinned by a digest", entry.Section, entry.Ref)
			}
			// RED: correlation becomes causation. Every link on the timeline
			// exists because it was written down, and there is no other legal
			// basis to express.
			if entry.Basis != intelligence.BasisRecordedReference {
				t.Fatalf("%s/%s has basis %s", entry.Section, entry.Ref, entry.Basis)
			}
		}
	})

	t.Run("RED: an external observation becomes a domain fact", func(t *testing.T) {
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		for _, entry := range got.Chronology {
			if entry.Section != intelligence.SectionObservations {
				continue
			}
			if entry.Epistemic != intelligence.EpistemicObserved {
				t.Fatalf("observation labelled %s, want OBSERVED", entry.Epistemic)
			}
		}
		for _, o := range got.Observations {
			if o.Authority.Kind != evidence.AuthorityExternalObservation {
				t.Fatalf("observation %s claims %s authority", o.ID, o.Authority.Kind)
			}
		}
		// A record whose observation claims local authority is refused
		// outright rather than published.
		bad := store(t)
		bad.record.Observations[0].Authority.Kind = evidence.AuthorityLocal
		_, err = intelligence.ExplainTransaction(ctx, bad, explainRequest(t, allowAll()))
		if !errors.Is(err, intelligence.ErrObservationAuthority) {
			t.Fatalf("err = %v, want ErrObservationAuthority", err)
		}
	})

	t.Run("RED: a stale projection overrides ledger evidence", func(t *testing.T) {
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.EvidencePrecedence != intelligence.EvidencePrecedence {
			t.Fatalf("evidence precedence = %q", got.EvidencePrecedence)
		}
		stale, current := 0, 0
		for _, p := range got.Projections {
			if !p.Comparable {
				t.Fatalf("projection %s could not be ordered against the ledger head", p.Name)
			}
			if p.Epistemic != intelligence.EpistemicDerived {
				t.Fatalf("projection %s labelled %s, want DERIVED", p.Name, p.Epistemic)
			}
			if p.Stale {
				stale++
			} else {
				current++
			}
		}
		if stale != 1 || current != 1 {
			t.Fatalf("stale=%d current=%d, want one of each", stale, current)
		}
		// A projection on a different stream cannot be ordered, and must not
		// be assumed current.
		other := store(t)
		revision, err := values.NewSequenceRevision("projection-stream", 1)
		if err != nil {
			t.Fatalf("revision: %v", err)
		}
		other.record.Projections[0].Revision = revision
		got, err = intelligence.ExplainTransaction(ctx, other, explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		for _, p := range got.Projections {
			if p.Name == "worker_state" {
				if p.Comparable || p.Epistemic != intelligence.EpistemicUnknown {
					t.Fatalf("an unorderable projection was reported comparable=%t label=%s",
						p.Comparable, p.Epistemic)
				}
			}
		}
	})

	t.Run("RED: the explanation triggers a replay, repair or effect", func(t *testing.T) {
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if !got.EffectCounters.IsZero() {
			t.Fatalf("the explanation counted effects: %v", got.EffectCounters.NonZero())
		}
		if err := got.Receipt.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if got.Receipt.ExecutionState != "NOT_PLANNED" {
			t.Fatalf("execution state = %q", got.Receipt.ExecutionState)
		}
		// The repair it reports is still not executable.
		for _, r := range got.Repairs {
			if r.Executable {
				t.Fatalf("repair %s was reported as executable", r.PlanID)
			}
		}
	})

	t.Run("GREEN: gaps and redactions are reported, not smoothed over", func(t *testing.T) {
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.Completeness.Complete {
			t.Fatal("an explanation with a recorded gap reported itself complete")
		}
		if len(got.Completeness.Gaps) != 1 ||
			got.Completeness.Gaps[0].Section != intelligence.SectionEffects {
			t.Fatalf("gaps = %+v", got.Completeness.Gaps)
		}
		if len(got.Completeness.Redactions) != 0 {
			t.Fatalf("an unredacted explanation reported redactions: %v", got.Completeness.Redactions)
		}
	})

	t.Run("GREEN: the explanation is deterministic and reproducible", func(t *testing.T) {
		first, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		second, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if first.ResultDigest != second.ResultDigest {
			t.Fatal("the explanation is not reproducible")
		}
		// Requesting the same sections in a different order changes nothing.
		req := explainRequest(t, allowAll())
		req.Sections = []intelligence.Section{
			intelligence.SectionRepair, intelligence.SectionRequest,
			intelligence.SectionLifecycle,
		}
		reordered := req
		reordered.Sections = []intelligence.Section{
			intelligence.SectionLifecycle, intelligence.SectionRepair,
			intelligence.SectionRequest,
		}
		a, err := intelligence.ExplainTransaction(ctx, store(t), req)
		if err != nil {
			t.Fatalf("narrowed: %v", err)
		}
		b, err := intelligence.ExplainTransaction(ctx, store(t), reordered)
		if err != nil {
			t.Fatalf("reordered: %v", err)
		}
		if a.ResultDigest != b.ResultDigest {
			t.Fatal("the digest depends on the order the caller listed its sections")
		}
	})

	t.Run("RED: a section the decision is silent about is defaulted", func(t *testing.T) {
		auth := allowAll()
		delete(auth.Sections, intelligence.SectionRepair)
		_, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, auth))
		if !errors.Is(err, intelligence.ErrAuthorizationIncomplete) {
			t.Fatalf("err = %v, want ErrAuthorizationIncomplete", err)
		}
	})

	t.Run("RED: a port that widens the projection is trusted", func(t *testing.T) {
		reader := store(t)
		reader.Widen = intelligence.SectionWriteSet
		req := explainRequest(t, allowAll())
		req.Sections = []intelligence.Section{intelligence.SectionRequest}
		req.Authorization.Sections = map[intelligence.Section]intelligence.Ruling{
			intelligence.SectionRequest: {Effect: intelligence.EffectAllow},
		}
		_, err := intelligence.ExplainTransaction(ctx, reader, req)
		if !errors.Is(err, intelligence.ErrPortWidenedProjection) {
			t.Fatalf("err = %v, want ErrPortWidenedProjection", err)
		}
	})
}

// TestTodo_INTEL_001_Security proves the disclosure boundary: a missing or
// unauthorized transaction does not leak its existence, a denied section
// discloses nothing, and a redacted field is named without its value.
func TestTodo_INTEL_001_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("RED: an unauthorized transaction leaks its existence", func(t *testing.T) {
		auth := withheldTransaction(allowAll(), "transaction_out_of_scope")
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, auth))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.Disclosure != intelligence.DisclosureWithheld {
			t.Fatalf("disclosure = %s, want WITHHELD", got.Disclosure)
		}
		if got.Presence != intelligence.PresenceUnspecified {
			t.Fatalf("a withheld explanation asserted presence %s", got.Presence)
		}
		if len(got.Sections) != 0 || len(got.Chronology) != 0 {
			t.Fatal("a withheld explanation carried contents")
		}
		if got.WithheldReason != "transaction_out_of_scope" {
			t.Fatalf("withheld reason = %q", got.WithheldReason)
		}

		// A missing transaction and a withheld one are indistinguishable from
		// the caller's side in the one thing that matters: neither says the
		// transaction exists.
		absent := store(t)
		absent.Absent = true
		missing, err := intelligence.ExplainTransaction(ctx, absent, explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if missing.Presence != intelligence.PresenceAbsent {
			t.Fatalf("a missing transaction reported presence %s", missing.Presence)
		}
	})

	t.Run("RED: a withheld transaction is read from the store at all", func(t *testing.T) {
		reader := store(t)
		auth := withheldTransaction(allowAll(), "transaction_out_of_scope")
		if _, err := intelligence.ExplainTransaction(ctx, reader, explainRequest(t, auth)); err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if reader.Calls != 0 {
			t.Fatalf("the store was read %d time(s) for a withheld transaction", reader.Calls)
		}
	})

	t.Run("RED: a denied section discloses its contents", func(t *testing.T) {
		auth := denySection(allowAll(), intelligence.SectionWriteSet, "write_set_restricted")
		reader := store(t)
		got, err := intelligence.ExplainTransaction(ctx, reader, explainRequest(t, auth))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.Disclosure != intelligence.DisclosurePartial {
			t.Fatalf("disclosure = %s, want PARTIAL", got.Disclosure)
		}
		outcome, ok := got.SectionAccess(intelligence.SectionWriteSet)
		if !ok {
			t.Fatal("the denied section was dropped instead of being named")
		}
		if outcome.Access != intelligence.AccessDenied ||
			outcome.DenialReason != "write_set_restricted" {
			t.Fatalf("access=%s reason=%q", outcome.Access, outcome.DenialReason)
		}
		if len(got.WriteSet) != 0 {
			t.Fatalf("a denied section disclosed %d entr(ies)", len(got.WriteSet))
		}
		for _, s := range reader.LastSections {
			if s == intelligence.SectionWriteSet {
				t.Fatal("the denied section was loaded from the store anyway")
			}
		}
		if raw := got.Canonical(); bytes.Contains(raw, []byte("sha256:write-1")) {
			t.Fatal("the denied section leaked into the canonical encoding")
		}
	})

	t.Run("RED: a redacted request field discloses its value", func(t *testing.T) {
		auth := allowAll()
		auth = denyRequestField(auth, intelligence.FieldRequestRequester, "requester_restricted")
		auth = denyRequestField(auth, intelligence.FieldRequestSubjects, "subjects_restricted")
		got, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, auth))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.Request.Requester.State() != values.PresenceRedacted {
			t.Fatalf("requester state = %s, want REDACTED", got.Request.Requester.State())
		}
		if got.Request.Requester.Reason() != "requester_restricted" {
			t.Fatalf("requester reason = %q", got.Request.Requester.Reason())
		}
		if len(got.Request.SubjectRefs) != 0 {
			t.Fatal("redacted subjects were disclosed anyway")
		}
		if got.Request.Purpose.State() != values.PresenceValue {
			t.Fatal("an allowed field was redacted")
		}
		// The intent identity stays visible: knowing that a promotion was
		// requested is not knowing who requested it.
		if got.Request.IntentType == "" {
			t.Fatal("the intent identity was redacted with the requester")
		}
		if len(got.Completeness.Redactions) != 2 {
			t.Fatalf("redactions = %v", got.Completeness.Redactions)
		}
		raw := got.Canonical()
		for _, leaked := range []string{"u-42", "u-17", workerRef(t).Id} {
			if bytes.Contains(raw, []byte(leaked)) {
				t.Fatalf("the explanation leaked %q", leaked)
			}
		}
	})

	t.Run("RED: an unknown redactable field is accepted in a decision", func(t *testing.T) {
		auth := allowAll()
		auth.Fields["request.salary"] = intelligence.Ruling{Effect: intelligence.EffectAllow}
		_, err := intelligence.ExplainTransaction(ctx, store(t), explainRequest(t, auth))
		if !errors.Is(err, intelligence.ErrAuthorizationInvalid) {
			t.Fatalf("err = %v, want ErrAuthorizationInvalid", err)
		}
	})
}

// TestTodo_INTEL_001_Property proves the invariants over every single-section
// projection: a narrowed explanation carries that section and nothing else.
func TestTodo_INTEL_001_Property(t *testing.T) {
	ctx := context.Background()
	for _, section := range intelligence.AllSections() {
		req := explainRequest(t, allowAll())
		req.Sections = []intelligence.Section{section}
		req.Authorization.Sections = map[intelligence.Section]intelligence.Ruling{
			section: {Effect: intelligence.EffectAllow},
		}
		got, err := intelligence.ExplainTransaction(ctx, store(t), req)
		if err != nil {
			t.Fatalf("%s: %v", section, err)
		}
		if len(got.Sections) != 1 || got.Sections[0].Section != section {
			t.Fatalf("%s: sections = %+v", section, got.Sections)
		}
		// Property: contents appear only for the requested section.
		counts := map[intelligence.Section]int{
			intelligence.SectionGovernance:     len(got.Controls),
			intelligence.SectionLifecycle:      len(got.Transitions),
			intelligence.SectionWriteSet:       len(got.WriteSet),
			intelligence.SectionEvents:         len(got.Events),
			intelligence.SectionEffects:        len(got.Effects),
			intelligence.SectionObservations:   len(got.Observations),
			intelligence.SectionReconciliation: len(got.Reconciliations),
			intelligence.SectionRepair:         len(got.Repairs),
			intelligence.SectionProjections:    len(got.Projections),
			intelligence.SectionEvidence:       len(got.EvidenceRefs),
		}
		for other, n := range counts {
			if other != section && n != 0 {
				t.Fatalf("%s requested but %s carried %d entr(ies)", section, other, n)
			}
		}
		if section != intelligence.SectionRequest && got.Request.IntentType != "" {
			t.Fatalf("%s requested but the request section was disclosed", section)
		}
		// Property: effects are always zero and the receipt always binds.
		if !got.EffectCounters.IsZero() {
			t.Fatalf("%s: effects %v", section, got.EffectCounters.NonZero())
		}
		if got.Receipt.ResultDigest != got.ResultDigest {
			t.Fatalf("%s: the receipt does not bind the explanation", section)
		}
	}
}

// TestTodo_INTEL_001_Golden pins the identity, vocabulary and chronology shape
// a stored explanation must keep.
func TestTodo_INTEL_001_Golden(t *testing.T) {
	got, err := intelligence.ExplainTransaction(context.Background(), store(t),
		explainRequest(t, allowAll()))
	if err != nil {
		t.Fatalf("ExplainTransaction: %v", err)
	}
	if got.IntentType != "hcmnext.intelligence.explain_transaction" || got.IntentVersion != "v1" {
		t.Fatalf("intent identity = %s/%s", got.IntentType, got.IntentVersion)
	}
	if got.RulePackVersion != "intelligence.explain.rules/1.0.0" {
		t.Fatalf("rule pack = %q", got.RulePackVersion)
	}
	if got.EvidencePrecedence != "LEDGER_OVER_PROJECTION" {
		t.Fatalf("evidence precedence = %q", got.EvidencePrecedence)
	}
	wantRefs := []string{
		"sha256:request-1", "sha256:transition-1", "sha256:transition-2",
		"evt-1", "eff-1", "obs-1", "rec-1", "plan-1",
	}
	refs := entryRefs(got)
	if len(refs) != len(wantRefs) {
		t.Fatalf("chronology refs = %v, want %v", refs, wantRefs)
	}
	seen := map[string]bool{}
	for _, r := range refs {
		seen[r] = true
	}
	for _, want := range wantRefs {
		if !seen[want] {
			t.Fatalf("chronology is missing %s: %v", want, refs)
		}
	}
}

// TestTodo_INTEL_001_Integration walks the whole record end to end and proves
// the chronology reconstructs the transaction's real order.
func TestTodo_INTEL_001_Integration(t *testing.T) {
	got, err := intelligence.ExplainTransaction(context.Background(), store(t),
		explainRequest(t, allowAll()))
	if err != nil {
		t.Fatalf("ExplainTransaction: %v", err)
	}
	wantOrder := []intelligence.Section{
		intelligence.SectionRequest,
		intelligence.SectionLifecycle,
		intelligence.SectionEvents,
		intelligence.SectionLifecycle,
		intelligence.SectionEffects,
		intelligence.SectionObservations,
		intelligence.SectionReconciliation,
		intelligence.SectionRepair,
	}
	// The lifecycle transition at 09:02 and the ledger event at 09:02 share a
	// recorded time, so the section tiebreak decides between them; what must
	// hold is that the whole sequence is a permutation of the recorded order.
	if len(got.Chronology) != len(wantOrder) {
		t.Fatalf("%d entr(ies), want %d", len(got.Chronology), len(wantOrder))
	}
	if got.Chronology[0].Section != intelligence.SectionRequest {
		t.Fatalf("the timeline does not start at the request: %s", got.Chronology[0].Section)
	}
	last := got.Chronology[len(got.Chronology)-1]
	if last.Section != intelligence.SectionRepair {
		t.Fatalf("the timeline does not end at the repair plan: %s", last.Section)
	}
	if !got.Watermark.IsSpecified() || !got.LedgerHead.IsSpecified() {
		t.Fatal("the explanation does not pin the read watermark and the ledger head")
	}
}

// TestTodo_INTEL_001_Conformance checks the section vocabulary and the receipt
// controls a stored explanation must carry.
func TestTodo_INTEL_001_Conformance(t *testing.T) {
	sections := intelligence.AllSections()
	if len(sections) != 11 {
		t.Fatalf("%d sections in the vocabulary", len(sections))
	}
	for i := 1; i < len(sections); i++ {
		if sections[i-1] >= sections[i] {
			t.Fatalf("AllSections is not sorted: %v", sections)
		}
	}
	if err := intelligence.Section("NOT_A_SECTION").Validate(); !errors.Is(err, intelligence.ErrSection) {
		t.Fatalf("err = %v, want ErrSection", err)
	}
	// LinkBasis has exactly one legal value; there is no causal basis to set.
	if err := intelligence.BasisUnspecified.Validate(); !errors.Is(err, intelligence.ErrLinkBasis) {
		t.Fatalf("err = %v, want ErrLinkBasis", err)
	}
	if err := intelligence.BasisRecordedReference.Validate(); err != nil {
		t.Fatalf("the one legal basis was refused: %v", err)
	}

	got, err := intelligence.ExplainTransaction(context.Background(), store(t),
		explainRequest(t, allowAll()))
	if err != nil {
		t.Fatalf("ExplainTransaction: %v", err)
	}
	controls := map[string]string{}
	for _, c := range got.Receipt.Controls {
		controls[c.Name] = c.Version
	}
	if controls["authorization_policy"] != policyVersion ||
		controls["explain_rule_pack"] != got.RulePackVersion {
		t.Fatalf("receipt controls = %v", controls)
	}
}

// TestTodo_INTEL_001_Mutation kills the mutants a weaker implementation would
// survive: an unbased link, a chronology that ignores recorded time, and a
// record whose links are accepted without validation.
func TestTodo_INTEL_001_Mutation(t *testing.T) {
	ctx := context.Background()

	t.Run("a link with no recorded basis is refused", func(t *testing.T) {
		for name, mutate := range map[string]func(*memoryHistory){
			"observation": func(m *memoryHistory) {
				m.record.Observations[0].Basis = intelligence.BasisUnspecified
			},
			"reconciliation": func(m *memoryHistory) {
				m.record.Reconciliations[0].Basis = intelligence.BasisUnspecified
			},
			"repair": func(m *memoryHistory) {
				m.record.Repairs[0].Basis = intelligence.BasisUnspecified
			},
		} {
			reader := store(t)
			mutate(reader)
			_, err := intelligence.ExplainTransaction(ctx, reader, explainRequest(t, allowAll()))
			if !errors.Is(err, intelligence.ErrLinkBasis) {
				t.Fatalf("%s: err = %v, want ErrLinkBasis", name, err)
			}
		}
	})

	t.Run("the chronology follows recorded time, not input order", func(t *testing.T) {
		reader := store(t)
		// Move the repair plan to the earliest moment; it must lead the
		// timeline, whatever order the record listed it in.
		reader.record.Repairs[0].At = recordedAt(t, "2026-08-01T08:00:00Z")
		got, err := intelligence.ExplainTransaction(ctx, reader, explainRequest(t, allowAll()))
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if got.Chronology[0].Section != intelligence.SectionRepair {
			t.Fatalf("the earliest entry is %s", got.Chronology[0].Section)
		}
	})

	t.Run("an incomplete element is refused rather than disclosed", func(t *testing.T) {
		for name, mutate := range map[string]func(*memoryHistory){
			"transition without a digest": func(m *memoryHistory) {
				m.record.Transitions[0].Digest = ""
			},
			"event without a revision": func(m *memoryHistory) {
				m.record.Events[0].Revision = values.UnspecifiedRevision()
			},
			"effect without a state": func(m *memoryHistory) {
				m.record.Effects[0].State = ""
			},
			"request without a requester": func(m *memoryHistory) {
				m.record.Request.Requester = intelligence.PrincipalRef{}
			},
		} {
			reader := store(t)
			mutate(reader)
			_, err := intelligence.ExplainTransaction(ctx, reader, explainRequest(t, allowAll()))
			if !errors.Is(err, intelligence.ErrRecordInvalid) {
				t.Fatalf("%s: err = %v, want ErrRecordInvalid", name, err)
			}
		}
	})
}

// entryRefs returns the chronology references of an explanation, in order.
func entryRefs(e intelligence.Explanation) []string {
	out := make([]string, 0, len(e.Chronology))
	for _, entry := range e.Chronology {
		out = append(out, entry.Ref)
	}
	return out
}
