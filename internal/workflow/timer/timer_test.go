package timer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

// refusingExecutor fails the test if any statement reaches the database, so a
// case that claims to be refused before any write actually is.
type refusingExecutor struct{ t *testing.T }

func (e refusingExecutor) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	e.t.Fatalf("a refused call still issued a statement: %s", sql)
	return 0, nil
}

func (e refusingExecutor) Query(_ context.Context, sql string, _ ...any) (dbport.Rows, error) {
	e.t.Fatalf("a refused call still issued a query: %s", sql)
	return nil, nil
}

func (e refusingExecutor) QueryRow(_ context.Context, sql string, _ ...any) dbport.Row {
	e.t.Fatalf("a refused call still issued a query: %s", sql)
	return nil
}

var _ Executor = refusingExecutor{}

var (
	unitTenant   = uuid.MustParse("55555555-5555-4555-8555-555555555555")
	unitInstance = uuid.MustParse("66666666-6666-4666-8666-666666666666")
	unitFires    = time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	unitNow      = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
)

func unitRequirement(t *testing.T, nodeID string) wait.TimerRequirement {
	t.Helper()
	req, err := wait.ComputeTimerRequirement(wait.CompiledWaitNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: nodeID,
		WakeInstant: values.NewInstant(unitFires),
		Zone:        values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Calendar:    values.CalendarRef{Ref: "us-federal", Version: "2026.1"},
		Policy:      values.ReferenceUpdatePin,
	}, values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"})
	if err != nil {
		t.Fatalf("ComputeTimerRequirement: %v", err)
	}
	return req
}

func TestKind_MapsOntoTheDurableTimerKinds(t *testing.T) {
	for kind, want := range map[Kind]string{
		KindWait:     runtimestate.TimerDelay,
		KindUntil:    runtimestate.TimerDelay,
		KindDeadline: runtimestate.TimerDeadline,
	} {
		got, ok := kind.durable()
		if !ok || got != want {
			t.Fatalf("Kind(%s).durable() = %q,%v; want %q,true", kind, got, ok, want)
		}
	}
	if _, ok := Kind("HEARTBEAT").durable(); ok {
		t.Fatal("an undeclared wait shape was mapped to a durable kind")
	}
}

// The identities have to be derived rather than minted: a replayed
// advancement must address the row it already wrote.
func TestTimerID_IsDerivedAndSensitiveToEveryPart(t *testing.T) {
	base := TimerID(unitTenant, unitInstance, "wait.a", "digest-1")
	if base == uuid.Nil {
		t.Fatal("TimerID minted the nil UUID")
	}
	if base != TimerID(unitTenant, unitInstance, "wait.a", "digest-1") {
		t.Fatal("TimerID is not deterministic")
	}
	for name, got := range map[string]uuid.UUID{
		"tenant":      TimerID(unitInstance, unitInstance, "wait.a", "digest-1"),
		"instance":    TimerID(unitTenant, unitTenant, "wait.a", "digest-1"),
		"node":        TimerID(unitTenant, unitInstance, "wait.b", "digest-1"),
		"requirement": TimerID(unitTenant, unitInstance, "wait.a", "digest-2"),
	} {
		if got == base {
			t.Fatalf("changing the %s did not change the derived timer id", name)
		}
	}
}

func TestReadyWorkID_IsDerivedPerNodeAttempt(t *testing.T) {
	base := ReadyWorkID(unitTenant, unitInstance, "wait.a", 1)
	if base != ReadyWorkID(unitTenant, unitInstance, "wait.a", 1) {
		t.Fatal("ReadyWorkID is not deterministic")
	}
	if base == ReadyWorkID(unitTenant, unitInstance, "wait.a", 2) {
		t.Fatal("two attempts of one node share a ready-work id")
	}
	if base == ReadyWorkID(unitTenant, unitInstance, "wait.b", 1) {
		t.Fatal("two nodes share a ready-work id")
	}
	if base == TimerID(unitTenant, unitInstance, "wait.a", "digest-1") {
		t.Fatal("a timer and its ready work share an identity")
	}
}

func TestSchedule_RefusesAMalformedRequestBeforeAnyStatement(t *testing.T) {
	ctx := context.Background()
	ex := refusingExecutor{t: t}
	var s Scheduler
	good := unitRequirement(t, "wait.effective_date")

	base := Request{
		TenantID: unitTenant, InstanceID: unitInstance, NodeID: "wait.effective_date",
		Kind: KindUntil, Requirement: good, CreatedAt: unitNow,
	}
	cases := map[string]func(r *Request){
		"no tenant":        func(r *Request) { r.TenantID = uuid.Nil },
		"no instance":      func(r *Request) { r.InstanceID = uuid.Nil },
		"no node":          func(r *Request) { r.NodeID = "" },
		"no clock reading": func(r *Request) { r.CreatedAt = time.Time{} },
		"no digest":        func(r *Request) { r.Requirement.Digest = "" },
		"undeclared kind":  func(r *Request) { r.Kind = "SNOOZE" },
		"no wake instant":  func(r *Request) { r.Requirement.FireAt = values.Instant{} },
	}
	for name, mutate := range cases {
		req := base
		mutate(&req)
		if _, err := s.Schedule(ctx, ex, req); err == nil {
			t.Fatalf("%s was accepted", name)
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

// A wake condition that could not be resolved to an instant is not a promise:
// it is a review item, and internal/workflow/steps/wait routes such a node to
// its declared failure route instead.
func TestSchedule_RefusesARequirementThatNeedsReview(t *testing.T) {
	ctx := context.Background()
	ex := refusingExecutor{t: t}
	var s Scheduler

	req := unitRequirement(t, "wait.effective_date")
	req.ReviewRequired = true
	req.ReviewReason = "the declared local time does not exist in this zone"
	req.FireAt = values.Instant{}

	_, err := s.Schedule(ctx, ex, Request{
		TenantID: unitTenant, InstanceID: unitInstance, NodeID: "wait.effective_date",
		Kind: KindUntil, Requirement: req, CreatedAt: unitNow,
	})
	if !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("err = %v, want ErrReviewRequired", err)
	}
	if got := CodeOf(err); got != CodeReviewRequired {
		t.Fatalf("CodeOf = %q, want %q", got, CodeReviewRequired)
	}
}

func TestFire_RefusesAMissingPolicyOrClockBeforeAnyStatement(t *testing.T) {
	ctx := context.Background()
	ex := refusingExecutor{t: t}
	var s Scheduler

	if _, err := s.Fire(ctx, ex, FireRequest{Now: unitNow}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no tenant: err = %v, want ErrInvalid", err)
	}
	if _, err := s.Fire(ctx, ex, FireRequest{TenantID: unitTenant}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no clock reading: err = %v, want ErrInvalid", err)
	}
	_, err := s.Fire(ctx, ex, FireRequest{TenantID: unitTenant, Now: unitNow})
	if !errors.Is(err, ErrMisfirePolicyRequired) {
		t.Fatalf("no misfire policy: err = %v, want ErrMisfirePolicyRequired", err)
	}
	if got := CodeOf(err); got != CodeMisfirePolicyRequired {
		t.Fatalf("CodeOf = %q, want %q", got, CodeMisfirePolicyRequired)
	}
}

// The dataset clause, checked without a database: a requirement recomputed
// against a republished dataset digests differently, and CheckRequirement
// refuses the mismatch rather than settling the old promise anyway.
func TestCheckRequirement_RefusesADatasetShiftAndAcceptsTheOriginal(t *testing.T) {
	var s Scheduler
	original := unitRequirement(t, "wait.effective_date")
	row := Timer{
		TenantID: unitTenant, TimerID: TimerID(unitTenant, unitInstance, "wait.effective_date", original.Digest),
		InstanceID: unitInstance, NodeID: "wait.effective_date", Key: original.Digest,
	}
	if err := s.CheckRequirement(row, original); err != nil {
		t.Fatalf("the promise's own requirement was refused: %v", err)
	}

	republished, err := wait.ComputeTimerRequirement(wait.CompiledWaitNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "wait.effective_date",
		WakeInstant: values.NewInstant(unitFires),
		Zone:        values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026b"},
		Calendar:    values.CalendarRef{Ref: "us-federal", Version: "2026.2"},
		Policy:      values.ReferenceUpdatePin,
	}, values.DatasetVersions{TzdbVersion: "2026b", CalendarVersion: "2026.2"})
	if err != nil {
		t.Fatalf("ComputeTimerRequirement against the republished dataset: %v", err)
	}
	if republished.Digest == original.Digest {
		t.Fatal("a republished dataset produced the same requirement digest; the drift check would be vacuous")
	}
	err = s.CheckRequirement(row, republished)
	if !errors.Is(err, ErrRequirementDrift) {
		t.Fatalf("err = %v, want ErrRequirementDrift", err)
	}
	if got := CodeOf(err); got != CodeRequirementDrift {
		t.Fatalf("CodeOf = %q, want %q", got, CodeRequirementDrift)
	}
	if err := s.CheckRequirement(row, wait.TimerRequirement{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a requirement with no digest: err = %v, want ErrInvalid", err)
	}
}

func TestItoa_RendersAttempts(t *testing.T) {
	for in, want := range map[int]string{0: "0", 1: "1", 42: "42", -3: "-3"} {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestOnly_KeepsTheNamedTimersInOrder(t *testing.T) {
	a := Timer{TimerID: uuid.MustParse("77777777-7777-4777-8777-777777777777")}
	b := Timer{TimerID: uuid.MustParse("88888888-8888-4888-8888-888888888888")}
	c := Timer{TimerID: uuid.MustParse("99999999-9999-4999-8999-999999999999")}
	got := only([]Timer{a, b, c}, []uuid.UUID{c.TimerID, a.TimerID})
	if len(got) != 2 || got[0].TimerID != a.TimerID || got[1].TimerID != c.TimerID {
		t.Fatalf("only() = %+v, want a then c in the source order", got)
	}
	if len(only([]Timer{a}, []uuid.UUID{b.TimerID})) != 0 {
		t.Fatal("only() kept a timer that was not named")
	}
}

func TestTimerIDForAttempt_QualifiesLaterActivationsOnly(t *testing.T) {
	first := TimerIDForAttempt(unitTenant, unitInstance, "wait.a", "digest-1", 1)
	if first != TimerID(unitTenant, unitInstance, "wait.a", "digest-1") {
		t.Fatal("the first activation must keep the unqualified timer identity")
	}
	if TimerIDForAttempt(unitTenant, unitInstance, "wait.a", "digest-1", 0) != first {
		t.Fatal("attempt 0 must be treated as the first activation")
	}
	second := TimerIDForAttempt(unitTenant, unitInstance, "wait.a", "digest-1", 2)
	if second == first {
		t.Fatal("a re-entered WAIT must get its own timer identity")
	}
	if second != TimerIDForAttempt(unitTenant, unitInstance, "wait.a", "digest-1", 2) {
		t.Fatal("TimerIDForAttempt is not deterministic")
	}
	if TimerIDForAttempt(unitTenant, unitInstance, "wait.a", "digest-1", 3) == second {
		t.Fatal("distinct later activations must not share an identity")
	}
}

func TestTimerKey_QualifiesLaterActivationsAndRoundTrips(t *testing.T) {
	if got := TimerKey("digest-1", 1); got != "digest-1" {
		t.Fatalf("first activation key = %q, want the bare digest", got)
	}
	second := TimerKey("digest-1", 2)
	if second == "digest-1" || KeyRequirementDigest(second) != "digest-1" {
		t.Fatalf("second activation key = %q, want a qualified key that still names digest-1", second)
	}
	if KeyRequirementDigest("digest-1") != "digest-1" {
		t.Fatal("a bare digest must round-trip unchanged")
	}
	if err := (Scheduler{}).CheckRequirement(Timer{Key: second}, wait.TimerRequirement{Digest: "digest-1"}); err != nil {
		t.Fatalf("CheckRequirement must accept a qualified key for its own digest: %v", err)
	}
	if err := (Scheduler{}).CheckRequirement(Timer{Key: second}, wait.TimerRequirement{Digest: "digest-2"}); err == nil {
		t.Fatal("CheckRequirement must still refuse a different digest")
	}
}
