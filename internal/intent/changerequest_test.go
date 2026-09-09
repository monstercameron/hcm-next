package intent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// fakePreflighter is the domain preflight port a domain package will implement.
// The kernel knows the request and result shapes and nothing else.
type fakePreflighter struct {
	result intent.DomainPreflightResult
	err    error
	calls  int
}

func (f *fakePreflighter) Preflight(context.Context, intent.PreflightRequest) (intent.DomainPreflightResult, error) {
	f.calls++
	return f.result, f.err
}

// TestTodo_INTENT_004 is the PRIMARY test for HCMChangeRequest draft and
// preflight.
//
// RED: an unknown subject, an unknown reference, a forbidden field, an invalid
// effective date, a stale baseline and missing required data cannot enter
// simulation as valid.
//
// GREEN: preflight returns a typed READY, NEEDS_DATA, BLOCKED or DENIED, with
// findings and the exact input snapshot.
func TestTodo_INTENT_004(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	ctx := context.Background()

	draft := func(t *testing.T) intent.Instance {
		t.Helper()
		inst, _, err := intent.Draft(promoteSpec(), def, d, nil, fixedClock())
		if err != nil {
			t.Fatalf("draft: %v", err)
		}
		return inst
	}

	t.Run("draft rejects a non-change-request definition", func(t *testing.T) {
		analytical := mustResolve(t, reg, "hcmnext.operations.detect_drift/v1")
		spec := promoteSpec()
		spec.Subjects = []intent.SubjectReference{
			{Kind: "BUSINESS_TRANSACTION", SubjectID: "tx:1", AuthorityDomain: "RECONCILIATION"},
		}
		spec.Request.Schema = schemaOf("hcmnext.operations.v1.DetectDriftRequest")
		spec.ExecutionMode = intent.ModeExecute
		if _, _, err := intent.Draft(spec, analytical, d, nil, fixedClock()); !errors.Is(err, intent.ErrInvalidDefinition) {
			t.Fatalf("an analytical request was drafted as a change request: %v", err)
		}
	})

	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.BaselineSnapshot)
			code   intent.FindingCode
			status intent.PreflightStatus
		}{
			{
				name:   "unknown subject",
				break_: func(b *intent.BaselineSnapshot) { b.KnownSubjects = b.KnownSubjects[:1] },
				code:   intent.FindingUnknownSubject,
				status: intent.PreflightBlocked,
			},
			{
				name:   "missing required data",
				break_: func(b *intent.BaselineSnapshot) { b.PresentInputs = []string{"employment_ref"} },
				code:   intent.FindingMissingRequired,
				status: intent.PreflightNeedsData,
			},
			{
				name: "forbidden field",
				break_: func(b *intent.BaselineSnapshot) {
					b.ForbiddenFields = []string{"target_position_ref"}
				},
				code:   intent.FindingForbiddenField,
				status: intent.PreflightDenied,
			},
			{
				name:   "stale baseline with no observation time",
				break_: func(b *intent.BaselineSnapshot) { b.ObservedAt = values.Instant{} },
				code:   intent.FindingStaleBaseline,
				status: intent.PreflightBlocked,
			},
			{
				name: "unpinned baseline read",
				break_: func(b *intent.BaselineSnapshot) {
					b.Revisions["people.employment.9001"] = values.UnspecifiedRevision()
				},
				code:   intent.FindingStaleBaseline,
				status: intent.PreflightBlocked,
			},
			{
				name: "negative state the policy blocks",
				break_: func(b *intent.BaselineSnapshot) {
					b.NegativeStates = []intent.NegativeState{intent.NegativeUnknown}
				},
				code:   intent.FindingNegativeStateBlock,
				status: intent.PreflightBlocked,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				baseline := promoteBaseline(t)
				tc.break_(&baseline)
				got, err := intent.Preflight(ctx, intent.PreflightRequest{
					Instance: draft(t), Definition: def, Baseline: baseline,
				}, reg, nil)
				if err != nil {
					t.Fatalf("preflight failed to run: %v", err)
				}
				if got.Ready() {
					t.Fatalf("preflight reported READY despite %s", tc.name)
				}
				if got.Status != tc.status {
					t.Fatalf("status = %s, want %s (findings %v)", got.Status, tc.status, got.Findings)
				}
				found := false
				for _, code := range got.FindingCodes() {
					if code == tc.code {
						found = true
					}
				}
				if !found {
					t.Fatalf("findings %v do not include %s", got.FindingCodes(), tc.code)
				}
				// A non-READY preflight never advances the request state.
				if got.Lifecycle.Request != lifecycle.RequestDraft {
					t.Fatalf("a failed preflight advanced the request to %s", got.Lifecycle.Request)
				}
			})
		}

		t.Run("invalid effective date", func(t *testing.T) {
			// An unset instant is not a date. The envelope refuses it outright,
			// so such a request never reaches simulation at all.
			spec := promoteSpec()
			bad := values.Instant{}
			spec.RequestedEffectiveAt = &bad
			if _, _, err := intent.Draft(spec, def, d, nil, fixedClock()); !errors.Is(err, intent.ErrInvalidInstance) {
				t.Fatalf("an unset effective date was accepted into an envelope: %v", err)
			}

			// A request that omits the effective time a definition declares
			// required is NEEDS_DATA, not a silent default.
			spec = promoteSpec()
			spec.RequestedEffectiveAt = nil
			inst, _, err := intent.Draft(spec, def, d, nil, fixedClock())
			if err != nil {
				t.Fatalf("draft: %v", err)
			}
			got, err := intent.Preflight(ctx, intent.PreflightRequest{
				Instance: inst, Definition: def, Baseline: promoteBaseline(t),
			}, reg, nil)
			if err != nil {
				t.Fatalf("preflight failed to run: %v", err)
			}
			if got.Ready() {
				t.Fatalf("preflight accepted a request with no effective time")
			}
			if got.Status != intent.PreflightNeedsData {
				t.Fatalf("status = %s, want NEEDS_DATA", got.Status)
			}
			found := false
			for _, c := range got.FindingCodes() {
				if c == intent.FindingInvalidEffective {
					found = true
				}
			}
			if !found {
				t.Fatalf("findings %v do not include %s",
					got.FindingCodes(), intent.FindingInvalidEffective)
			}
		})

		t.Run("a domain preflight that declares an effect fails the call", func(t *testing.T) {
			p := &fakePreflighter{result: intent.DomainPreflightResult{
				Status: intent.PreflightReady,
				DeclaredEffects: []intent.PlannedEffect{{
					EffectID: "effect:write-assignment", Kind: "DOMAIN_WRITE",
				}},
			}}
			_, err := intent.Preflight(ctx, intent.PreflightRequest{
				Instance: draft(t), Definition: def, Baseline: promoteBaseline(t),
			}, reg, p)
			if !errors.Is(err, intent.ErrEffectInPreflight) {
				t.Fatalf("preflight accepted a declared effect: %v", err)
			}
		})

		t.Run("a domain preflight error is a failure to run, not a verdict", func(t *testing.T) {
			p := &fakePreflighter{err: errors.New("projection unavailable")}
			_, err := intent.Preflight(ctx, intent.PreflightRequest{
				Instance: draft(t), Definition: def, Baseline: promoteBaseline(t),
			}, reg, p)
			if !errors.Is(err, intent.ErrPreflight) {
				t.Fatalf("a domain error became a verdict: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		p := &fakePreflighter{result: intent.DomainPreflightResult{Status: intent.PreflightReady}}
		baseline := promoteBaseline(t)
		got, err := intent.Preflight(ctx, intent.PreflightRequest{
			Instance: draft(t), Definition: def, Baseline: baseline,
		}, reg, p)
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if got.Status != intent.PreflightReady {
			t.Fatalf("status = %s, findings %v", got.Status, got.Findings)
		}
		if len(got.Findings) != 0 {
			t.Fatalf("a READY preflight reported findings: %v", got.Findings)
		}
		if got.InputSnapshot.SnapshotID != baseline.SnapshotID {
			t.Fatalf("the result does not carry the exact input snapshot")
		}
		if got.Lifecycle.Request != lifecycle.RequestPreflighted {
			t.Fatalf("a READY preflight left the request at %s", got.Lifecycle.Request)
		}
		if got.Lifecycle.Execution != lifecycle.ExecutionNotPlanned {
			t.Fatalf("preflight moved ExecutionState to %s", got.Lifecycle.Execution)
		}
		if p.calls != 1 {
			t.Fatalf("the domain port was called %d times", p.calls)
		}
	})

	t.Run("the worst answer wins", func(t *testing.T) {
		// A NEEDS_DATA kernel finding and a DENIED domain verdict combine to
		// DENIED: extra data cannot cure an authority answer.
		baseline := promoteBaseline(t)
		baseline.PresentInputs = []string{"employment_ref"}
		p := &fakePreflighter{result: intent.DomainPreflightResult{
			Status: intent.PreflightDenied,
			Findings: []intent.Finding{{
				Code: intent.FindingAuthorityDenied, FieldPath: "employment_ref",
				Detail: "requester holds no promotion authority", Status: intent.PreflightDenied,
			}},
		}}
		got, err := intent.Preflight(ctx, intent.PreflightRequest{
			Instance: draft(t), Definition: def, Baseline: baseline,
		}, reg, p)
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if got.Status != intent.PreflightDenied {
			t.Fatalf("combined status = %s, want DENIED", got.Status)
		}
		if len(got.FindingCodes()) < 2 {
			t.Fatalf("combined findings lost one side: %v", got.FindingCodes())
		}
	})
}

// TestTodo_INTENT_004_Golden pins the finding set a broken request produces, so
// a silent change to preflight's classification shows up as a diff.
func TestTodo_INTENT_004_Golden(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	inst, _, err := intent.Draft(promoteSpec(), def, d, countingIDs("01234567"), fixedClock())
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	baseline := promoteBaseline(t)
	baseline.KnownSubjects = baseline.KnownSubjects[:1]
	baseline.PresentInputs = []string{"employment_ref", "effective_time"}
	baseline.ForbiddenFields = []string{"reason_ref"}
	baseline.NegativeStates = []intent.NegativeState{intent.NegativePartial}

	got, err := intent.Preflight(context.Background(), intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: baseline,
	}, reg, nil)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	type finding struct {
		Code, FieldPath, Detail, Status string
	}
	rows := make([]finding, 0, len(got.Findings))
	for _, f := range got.Findings {
		rows = append(rows, finding{
			Code: string(f.Code), FieldPath: f.FieldPath,
			Detail: f.Detail, Status: f.Status.String(),
		})
	}
	goldenJSON(t, "intent_004_preflight.json", struct {
		Status    string
		Lifecycle string
		Findings  []finding
	}{
		Status:    got.Status.String(),
		Lifecycle: got.Lifecycle.String(),
		Findings:  rows,
	})
}

// TestTodo_INTENT_004_Mutation removes one baseline guarantee at a time and
// requires preflight to notice each removal. It also proves the whole call is
// read-only: no input the kernel was handed comes back changed.
func TestTodo_INTENT_004_Mutation(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	ctx := context.Background()

	inst, _, err := intent.Draft(promoteSpec(), def, d, nil, fixedClock())
	if err != nil {
		t.Fatalf("draft: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*intent.BaselineSnapshot)
	}{
		{"drop a known subject", func(b *intent.BaselineSnapshot) { b.KnownSubjects = nil }},
		{"drop the present inputs", func(b *intent.BaselineSnapshot) { b.PresentInputs = nil }},
		{"drop the observation time", func(b *intent.BaselineSnapshot) { b.ObservedAt = values.Instant{} }},
		{"unpin every read", func(b *intent.BaselineSnapshot) {
			for k := range b.Revisions {
				b.Revisions[k] = values.UnspecifiedRevision()
			}
		}},
		{"forbid a required field", func(b *intent.BaselineSnapshot) {
			b.ForbiddenFields = []string{"employment_ref"}
		}},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			baseline := promoteBaseline(t)
			m.mutate(&baseline)
			got, err := intent.Preflight(ctx, intent.PreflightRequest{
				Instance: inst, Definition: def, Baseline: baseline,
			}, reg, nil)
			if err != nil {
				t.Fatalf("preflight: %v", err)
			}
			if got.Ready() {
				t.Fatalf("mutation %q survived preflight", m.name)
			}
		})
	}

	t.Run("preflight mutates nothing", func(t *testing.T) {
		before := inst
		baseline := promoteBaseline(t)
		beforeSnapshotID := baseline.SnapshotID
		beforeInputs := append([]string(nil), baseline.PresentInputs...)

		if _, err := intent.Preflight(ctx, intent.PreflightRequest{
			Instance: inst, Definition: def, Baseline: baseline,
		}, reg, &fakePreflighter{result: intent.DomainPreflightResult{Status: intent.PreflightReady}}); err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if inst.Lifecycle != before.Lifecycle {
			t.Fatalf("preflight advanced the instance in place: %v", inst.Lifecycle)
		}
		if inst.CanonicalRequestDigest != before.CanonicalRequestDigest {
			t.Fatalf("preflight changed the request digest")
		}
		if baseline.SnapshotID != beforeSnapshotID {
			t.Fatalf("preflight rewrote the snapshot id")
		}
		if len(baseline.PresentInputs) != len(beforeInputs) {
			t.Fatalf("preflight rewrote the snapshot inputs")
		}
	})

	t.Run("preflight is refused for a non-zero-effect definition", func(t *testing.T) {
		writeDef := mustResolve(t, reg, "hcmnext.rewards.change_base_pay/v1")
		spec := promoteSpec()
		spec.Subjects = []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
		}
		spec.Request.Schema = schemaOf("hcmnext.rewards.v1.ChangeBasePayRequest")
		writeInst, _, err := intent.Draft(spec, writeDef, d, nil, fixedClock())
		if err != nil {
			t.Fatalf("draft: %v", err)
		}
		_, err = intent.Preflight(ctx, intent.PreflightRequest{
			Instance: writeInst, Definition: writeDef, Baseline: promoteBaseline(t),
		}, reg, nil)
		if !errors.Is(err, intent.ErrEffectInPreflight) {
			t.Fatalf("preflight ran for a definition whose release ceiling is not zero-effect: %v", err)
		}
	})
}

// startOfNextYear is used by the plan tests for a plan expiry that is clearly
// in the future of the fixed clock.
func startOfNextYear() values.Instant {
	return values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
}
