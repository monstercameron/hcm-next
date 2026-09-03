package attendance

import (
	"errors"
	"testing"
	"time"
)

func ref(id, version string) VersionedRef { return VersionedRef{ID: id, Version: version} }
func validRequest() Request {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	return Request{WorkerID: "worker-1", Schedule: Schedule{Ref: ref("schedule", "7"), Shifts: []Shift{{ID: "shift-1", Interval: Interval{Start: start, End: start.Add(8 * time.Hour)}}}}, Punches: []Punch{{ID: "punch-1", Ref: ref("punches", "12"), At: start}}, Breaks: []Break{{ID: "break-1", Ref: ref("breaks", "4"), Interval: Interval{Start: start.Add(4 * time.Hour), End: start.Add(4*time.Hour + 30*time.Minute)}}}, Jurisdiction: Jurisdiction{Ref: ref("jurisdiction", "2026"), Code: "US-NY"}, Rules: RuleSet{Ref: ref("rules", "3"), Name: "attendance"}, Tolerance: Tolerance{Ref: ref("tolerance", "2"), Late: 5 * time.Minute, Early: 5 * time.Minute}, Context: EffectiveContext{EffectiveAt: start, KnownAt: start, Timezone: "UTC", Calendar: "calendar@1"}}
}

func TestTodo_ATTEND_001(t *testing.T) {
	r, err := Evaluate(validRequest())
	if err != nil || r.Outcome != Compliant {
		t.Fatalf("evaluation = %s, err=%v", r.Outcome, err)
	}
	if r.InputDigest == "" || r.Schedule.Version != "7" || len(r.Shifts) != 1 {
		t.Fatalf("evaluation did not retain pinned evidence: %+v", r)
	}
}

func TestTodo_ATTEND_001_Property(t *testing.T) {
	r := validRequest()
	a, _ := Evaluate(r)
	b, _ := Evaluate(r)
	if a.InputDigest != b.InputDigest || a.Outcome != b.Outcome {
		t.Fatal("identical pinned inputs are not deterministic")
	}
}

func TestTodo_ATTEND_001_Conformance(t *testing.T) {
	for name, mutate := range map[string]func(*Request){"schedule": func(r *Request) { r.Schedule.Ref = VersionedRef{} }, "rules": func(r *Request) { r.Rules.Ref = VersionedRef{} }, "jurisdiction": func(r *Request) { r.Jurisdiction.Code = "" }, "context": func(r *Request) { r.Context.Timezone = "" }} {
		t.Run(name, func(t *testing.T) {
			got, err := Evaluate(func() Request { r := validRequest(); mutate(&r); return r }())
			if err != nil || got.Outcome != Unknown {
				t.Fatalf("missing evidence = %s, err=%v", got.Outcome, err)
			}
		})
	}
}

func TestTodo_ATTEND_001_Security(t *testing.T) {
	r := validRequest()
	r.Punches[0].Ref = VersionedRef{ID: "punches", Version: ""}
	got, err := Evaluate(r)
	if err != nil || got.Outcome != Unknown {
		t.Fatalf("unversioned punch must not become compliant: %s, %v", got.Outcome, err)
	}
}

func TestTodo_ATTEND_001_Mutation(t *testing.T) {
	r := validRequest()
	r.Tolerance.Late = -time.Second
	if _, err := Evaluate(r); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("negative tolerance err=%v", err)
	}
}
