package dataops_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// baseRequest is the fixture debugger request: the compensation, grade and
// job-code fields of one worker, effective mid-June, as known at the end of
// August - which is after the retroactive correction was recorded.
func baseRequest(t *testing.T) dataops.ExplainFieldHistoryRequest {
	t.Helper()
	return dataops.ExplainFieldHistoryRequest{
		Tenant:        subject(t).Tenant,
		Subject:       subject(t),
		Fields:        []dataops.FieldID{fieldBase, fieldGrade, fieldJobCode},
		AsOfEffective: localDate(t, "2026-06-15"),
		AsKnownAt:     knownAt(t, "2026-08-31T00:00:00Z"),
		Authorization: allowAll(fieldBase, fieldGrade, fieldJobCode),
	}
}

// TestTodo_DATAOPS_007 is the DATAOPS-007 primary test: an effective-dated
// field history is explained as ordered assertions with effective and recorded
// intervals, authority class, correction lineage and field-level redaction
// reasons - and it is never flattened.
func TestTodo_DATAOPS_007(t *testing.T) {
	ctx := context.Background()

	t.Run("GREEN: ordered assertions carry both temporal coordinates and their authority", func(t *testing.T) {
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), baseRequest(t))
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if got.Disclosure != dataops.DisclosureFull {
			t.Fatalf("disclosure = %s, want FULL", got.Disclosure)
		}
		if !got.Exists {
			t.Fatal("subject reported absent")
		}
		timeline, ok := got.Timeline(fieldBase)
		if !ok {
			t.Fatalf("no timeline for %s", fieldBase)
		}
		if len(timeline.Versions) != 4 {
			t.Fatalf("disclosed %d version(s), want the 4 the record holds: %v",
				len(timeline.Versions), versionIDs(timeline))
		}
		wantOrder := []string{"a1", "a2", "a3", "a4"}
		if ids := versionIDs(timeline); !equalStrings(ids, wantOrder) {
			t.Fatalf("version order = %v, want %v", ids, wantOrder)
		}
		for _, v := range timeline.Versions {
			if v.Effective.Validate() != nil {
				t.Fatalf("version %s has no effective interval", v.ID)
			}
			if v.KnownAt.Canonical() == nil || v.Provenance.RecordedAt.Canonical() == nil {
				t.Fatalf("version %s does not carry both known-at and recorded-at", v.ID)
			}
			if v.Authority.Validate() != nil {
				t.Fatalf("version %s has no source authority", v.ID)
			}
			if !v.Class.Valid() {
				t.Fatalf("version %s has no assertion class", v.ID)
			}
		}
	})

	t.Run("GREEN: the correction wins in force and the corrected version is retained", func(t *testing.T) {
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), baseRequest(t))
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		timeline, _ := got.Timeline(fieldBase)
		if !timeline.Asserted || timeline.InForceID != "a3" {
			t.Fatalf("in force = %q (asserted %t), want a3 - the August correction",
				timeline.InForceID, timeline.Asserted)
		}
		if timeline.CorrectionCount != 1 {
			t.Fatalf("correction count = %d, want 1", timeline.CorrectionCount)
		}

		// RED: a correction presented as a deletion. The superseded assertion
		// must still be on the timeline, marked, and pointing at what replaced
		// it.
		superseded := versionByID(t, timeline, "a2")
		if !superseded.Superseded || superseded.SupersededBy != "a3" {
			t.Fatalf("a2 superseded=%t by=%q, want true/a3", superseded.Superseded, superseded.SupersededBy)
		}
		if superseded.InForce {
			t.Fatal("the corrected version is still reported as in force")
		}
		if v, ok := superseded.Value.Get(); !ok || v != "130000.00 USD" {
			t.Fatalf("the superseded value was not retained: %q (readable %t)", v, ok)
		}
		correction := versionByID(t, timeline, "a3")
		if correction.Change != dataops.ChangeCorrection || correction.Corrects != "a2" {
			t.Fatalf("a3 change=%s corrects=%q, want CORRECTION/a2", correction.Change, correction.Corrects)
		}
	})

	t.Run("GREEN: current, future and retroactive versions stay distinguishable", func(t *testing.T) {
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), baseRequest(t))
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		timeline, _ := got.Timeline(fieldBase)

		// RED: flattening. Each of the four versions occupies a different
		// position in the two timelines, and the explanation must say which.
		for _, want := range []struct {
			id              string
			effectiveAtAsOf bool
			knownAtAsOf     bool
			inForce         bool
		}{
			{"a1", false, true, false}, // past interval, long known
			{"a2", true, true, false},  // covers the date, superseded by a3
			{"a3", true, true, true},   // the retroactive correction, in force
			{"a4", false, true, false}, // future-dated, already known
		} {
			v := versionByID(t, timeline, want.id)
			if v.EffectiveAtAsOf != want.effectiveAtAsOf ||
				v.KnownAtAsOf != want.knownAtAsOf ||
				v.InForce != want.inForce {
				t.Fatalf("%s effective=%t known=%t in_force=%t, want %t/%t/%t",
					want.id, v.EffectiveAtAsOf, v.KnownAtAsOf, v.InForce,
					want.effectiveAtAsOf, want.knownAtAsOf, want.inForce)
			}
		}
	})

	t.Run("GREEN: as-known-at reproduces the earlier belief", func(t *testing.T) {
		// RED: a known-at-time query answered with today's knowledge. Asked as
		// of the same business date but as known in June, the record must
		// still say 130000 and must not mention the August correction at all.
		req := baseRequest(t)
		req.AsKnownAt = knownAt(t, "2026-06-15T00:00:00Z")
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		timeline, _ := got.Timeline(fieldBase)
		if timeline.InForceID != "a2" {
			t.Fatalf("in force at the June cut-off = %q, want a2", timeline.InForceID)
		}
		for _, v := range timeline.Versions {
			if v.ID == "a3" || v.ID == "a4" {
				t.Fatalf("version %s was recorded after the cut-off and must be invisible", v.ID)
			}
		}
		if timeline.CorrectionCount != 0 {
			t.Fatalf("correction count = %d at a cut-off before the correction", timeline.CorrectionCount)
		}
	})

	t.Run("GREEN: an absent subject is an answer, not a fault", func(t *testing.T) {
		store := corpus(t)
		store.Absent = true
		got, err := dataops.ExplainFieldHistory(ctx, store, baseRequest(t))
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if got.Exists {
			t.Fatal("absent subject reported as existing")
		}
		for _, tl := range got.Fields {
			if tl.Asserted || len(tl.Versions) != 0 {
				t.Fatalf("%s carries %d version(s) for an absent subject", tl.Field, len(tl.Versions))
			}
		}
	})

	t.Run("GREEN: claims are excluded by default and labelled when requested", func(t *testing.T) {
		req := baseRequest(t)
		req.Fields = []dataops.FieldID{fieldPrefName}
		req.Authorization = allowAll(fieldPrefName)
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if tl, _ := got.Timeline(fieldPrefName); len(tl.Versions) != 0 {
			t.Fatalf("an unaccepted claim was disclosed without being asked for: %v", versionIDs(tl))
		}

		req.IncludeClaims = true
		got, err = dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if err != nil {
			t.Fatalf("ExplainFieldHistory with claims: %v", err)
		}
		tl, _ := got.Timeline(fieldPrefName)
		if len(tl.Versions) != 1 || tl.Versions[0].Class != dataops.ClassClaim {
			t.Fatalf("claim not disclosed and labelled: %+v", versionIDs(tl))
		}
		// A claim is displayed, never in force.
		if tl.Asserted {
			t.Fatal("an unaccepted claim was reported as the value in force")
		}
	})

	t.Run("GREEN: the result carries a zero-effect receipt", func(t *testing.T) {
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), baseRequest(t))
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if !got.Effects.IsZero() {
			t.Fatalf("the debugger counted effects: %v", got.Effects.NonZero())
		}
		if err := got.Receipt.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if got.Receipt.ResultDigest != got.ResultDigest || got.Receipt.InputsDigest != got.InputsDigest {
			t.Fatal("the receipt does not bind the explanation it certifies")
		}
	})

	t.Run("RED: a field the decision is silent about is refused, not defaulted", func(t *testing.T) {
		req := baseRequest(t)
		req.Authorization = allowAll(fieldBase, fieldGrade) // no ruling for job code
		_, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if !errors.Is(err, dataops.ErrAuthorizationIncomplete) {
			t.Fatalf("err = %v, want ErrAuthorizationIncomplete", err)
		}
	})

	t.Run("RED: a port that widens the projection is refused", func(t *testing.T) {
		store := corpus(t)
		store.Widen = fieldLegal
		req := baseRequest(t)
		_, err := dataops.ExplainFieldHistory(ctx, store, req)
		if !errors.Is(err, dataops.ErrPortWidenedProjection) {
			t.Fatalf("err = %v, want ErrPortWidenedProjection", err)
		}
	})
}

// TestTodo_DATAOPS_007_Security proves the disclosure boundary: a denied field
// leaks neither its value nor its provenance, and a non-disclosable subject
// leaks not even its existence.
func TestTodo_DATAOPS_007_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("RED: a denied field leaks its value, coordinates or provenance", func(t *testing.T) {
		req := baseRequest(t)
		req.Authorization = denyField(req.Authorization, fieldBase, "compensation_restricted")
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if got.Disclosure != dataops.DisclosurePartial {
			t.Fatalf("disclosure = %s, want PARTIAL", got.Disclosure)
		}
		timeline, ok := got.Timeline(fieldBase)
		if !ok {
			t.Fatal("the denied field was dropped instead of being named")
		}
		if timeline.Access != dataops.AccessDenied || timeline.DenialReason != "compensation_restricted" {
			t.Fatalf("access=%s reason=%q, want DENIED/compensation_restricted",
				timeline.Access, timeline.DenialReason)
		}
		if len(timeline.Versions) != 0 || timeline.InForceID != "" {
			t.Fatalf("the denied field disclosed %d version(s)", len(timeline.Versions))
		}
		// Nothing about the denied field may appear in the canonical bytes.
		raw := got.Canonical()
		if raw == nil {
			t.Fatal("explanation has no canonical encoding")
		}
		for _, leaked := range []string{"135000.00 USD", "130000.00 USD", "evidence:comp-a3"} {
			if bytes.Contains(raw, []byte(leaked)) {
				t.Fatalf("denied field leaked %q into the canonical encoding", leaked)
			}
		}
	})

	t.Run("RED: a denied field is still read from the repository", func(t *testing.T) {
		store := corpus(t)
		req := baseRequest(t)
		req.Authorization = denyField(req.Authorization, fieldBase, "compensation_restricted")
		if _, err := dataops.ExplainFieldHistory(ctx, store, req); err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		for _, f := range store.LastFields {
			if f == fieldBase {
				t.Fatal("the denied field was loaded from the repository anyway")
			}
		}
	})

	t.Run("RED: existence leaks through a withheld subject", func(t *testing.T) {
		req := baseRequest(t)
		req.Authorization = withheldSubject(req.Authorization, "subject_out_of_population")
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if got.Disclosure != dataops.DisclosureWithheld {
			t.Fatalf("disclosure = %s, want WITHHELD", got.Disclosure)
		}
		if got.Exists {
			t.Fatal("a withheld explanation asserted that the subject exists")
		}
		if len(got.Fields) != 0 {
			t.Fatalf("a withheld explanation carried %d field(s)", len(got.Fields))
		}
		if got.WithheldReason != "subject_out_of_population" {
			t.Fatalf("withheld reason = %q", got.WithheldReason)
		}
	})

	t.Run("RED: a withheld subject is read from the repository at all", func(t *testing.T) {
		store := corpus(t)
		req := baseRequest(t)
		req.Authorization = withheldSubject(req.Authorization, "subject_out_of_population")
		if _, err := dataops.ExplainFieldHistory(ctx, store, req); err != nil {
			t.Fatalf("ExplainFieldHistory: %v", err)
		}
		if store.Calls != 0 {
			t.Fatalf("the repository was read %d time(s) for a withheld subject", store.Calls)
		}
	})
}

// TestTodo_DATAOPS_007_Property proves the properties the debugger must hold
// over every bitemporal coordinate in the fixture, not only the sampled ones.
func TestTodo_DATAOPS_007_Property(t *testing.T) {
	ctx := context.Background()
	dates := []string{"2025-12-31", "2026-01-01", "2026-05-31", "2026-06-01",
		"2026-08-31", "2026-09-01", "2027-01-01"}
	cuts := []string{"2025-12-20T00:00:00Z", "2026-05-25T00:00:00Z",
		"2026-06-15T00:00:00Z", "2026-08-15T00:00:00Z", "2026-09-30T00:00:00Z"}

	for _, date := range dates {
		for _, cut := range cuts {
			req := baseRequest(t)
			req.AsOfEffective = localDate(t, date)
			req.AsKnownAt = knownAt(t, cut)
			got, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
			if err != nil {
				t.Fatalf("as of %s known at %s: %v", date, cut, err)
			}
			for _, timeline := range got.Fields {
				inForce := 0
				for _, v := range timeline.Versions {
					// Property: nothing recorded after the cut-off is ever
					// visible.
					if !v.KnownAtAsOf {
						t.Fatalf("as of %s known at %s: %s disclosed version %s from the future",
							date, cut, timeline.Field, v.ID)
					}
					// Property: the value in force always covers the business
					// date and is never a superseded one.
					if v.InForce {
						inForce++
						if !v.EffectiveAtAsOf {
							t.Fatalf("as of %s: %s in force but not effective", date, v.ID)
						}
						if v.Superseded {
							t.Fatalf("as of %s known at %s: superseded %s reported in force",
								date, cut, v.ID)
						}
					}
				}
				// Property: at most one version is in force, and Asserted says
				// whether there is one.
				if inForce > 1 {
					t.Fatalf("as of %s known at %s: %d versions in force", date, cut, inForce)
				}
				if (inForce == 1) != timeline.Asserted {
					t.Fatalf("as of %s known at %s: asserted=%t with %d in force",
						date, cut, timeline.Asserted, inForce)
				}
			}
			// Property: the explanation is reproducible.
			again, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			if again.ResultDigest != got.ResultDigest {
				t.Fatalf("as of %s known at %s: digest is not reproducible", date, cut)
			}
		}
	}
}

// FuzzTodo_DATAOPS_007 drives the request coordinates and the projection with
// arbitrary input. No input may panic, and no input may produce a disclosure
// that contradicts its own authorization.
func FuzzTodo_DATAOPS_007(f *testing.F) {
	f.Add("2026-06-15", "2026-08-31T00:00:00Z", "compensation.base", true)
	f.Add("", "", "", false)
	f.Add("2026-13-45", "not-a-time", "Compensation.Base", true)
	f.Add("0001-01-01", "2026-08-31T00:00:00Z", "a..b", false)

	f.Fuzz(func(t *testing.T, date, cut, field string, allow bool) {
		ctx := context.Background()
		req := dataops.ExplainFieldHistoryRequest{
			Tenant:        subject(t).Tenant,
			Subject:       subject(t),
			Fields:        []dataops.FieldID{dataops.FieldID(field)},
			Authorization: allowAll(dataops.FieldID(field)),
		}
		if !allow {
			req.Authorization = denyField(req.Authorization, dataops.FieldID(field), "fuzz_denied")
		}
		if parsed, err := values.ParseLocalDate(date); err == nil {
			req.AsOfEffective = parsed
		}
		if parsed, err := parseInstant(cut); err == nil {
			if k, err := values.NewKnownAt(parsed); err == nil {
				req.AsKnownAt = k
			}
		}
		got, err := dataops.ExplainFieldHistory(ctx, corpus(t), req)
		if err != nil {
			return
		}
		if got.Disclosure == dataops.DisclosureUnspecified {
			t.Fatalf("accepted request produced an unspecified disclosure")
		}
		for _, timeline := range got.Fields {
			if timeline.Access == dataops.AccessDenied && len(timeline.Versions) != 0 {
				t.Fatalf("denied field %s disclosed %d version(s)", timeline.Field, len(timeline.Versions))
			}
			if timeline.Access == dataops.AccessUnspecified {
				t.Fatalf("field %s has no access outcome", timeline.Field)
			}
		}
		if !got.Effects.IsZero() {
			t.Fatalf("a fuzzed explanation counted effects: %v", got.Effects.NonZero())
		}
	})
}

// versionIDs returns the version identifiers of a timeline, in order.
func versionIDs(t dataops.FieldTimeline) []string {
	out := make([]string, 0, len(t.Versions))
	for _, v := range t.Versions {
		out = append(out, v.ID)
	}
	return out
}

// versionByID returns the named version, failing the test when it is absent.
func versionByID(t *testing.T, timeline dataops.FieldTimeline, id string) dataops.ExplainedAssertion {
	t.Helper()
	for _, v := range timeline.Versions {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("version %s is not on the timeline: %v", id, versionIDs(timeline))
	return dataops.ExplainedAssertion{}
}

// equalStrings reports whether two string slices are identical.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
