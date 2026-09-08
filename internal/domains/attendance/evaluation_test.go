package attendance

import (
	"errors"
	"reflect"
	"strings"
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

func TestTodo_ATTEND_002(t *testing.T) {
	base := validRequest()
	start := base.Schedule.Shifts[0].Interval.Start
	seeded := base
	seeded.Schedule.Shifts = append(seeded.Schedule.Shifts, Shift{ID: "overlap", Interval: Interval{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)}})
	got, err := Evaluate(seeded)
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "field=schedule.shifts.interval state=overlap:shift-1:overlap version=7") || got.Outcome != Unknown || len(got.Exceptions) != 0 {
		t.Fatalf("seeded pinned-schedule defect must reject without findings: result=%+v err=%v", got, err)
	}
	cases := []struct {
		name     string
		mutate   func(*Request)
		kind     ExceptionKind
		minutes  int
		interval Interval
		reason   string
	}{
		{"late", func(r *Request) { r.Punches[0].At = start.Add(20 * time.Minute) }, LateException, 15, Interval{Start: start.Add(5 * time.Minute), End: start.Add(20 * time.Minute)}, "arrival is outside late tolerance"},
		{"early", func(r *Request) {
			r.Punches = append(r.Punches, Punch{ID: "out", Ref: ref("punches", "12"), At: start.Add(7 * time.Hour)})
		}, EarlyException, 55, Interval{Start: start.Add(7 * time.Hour), End: start.Add(7*time.Hour + 55*time.Minute)}, "departure is outside early tolerance"},
		{"missing", func(r *Request) { r.Punches[0].At = start.Add(12 * time.Hour) }, MissingException, 480, Interval{Start: start, End: start.Add(8 * time.Hour)}, "no punch evidence for scheduled shift"},
		{"unscheduled", func(r *Request) {
			r.Schedule.Shifts[0].Interval = Interval{Start: start.Add(2 * time.Hour), End: start.Add(10 * time.Hour)}
		}, UnscheduledException, 0, Interval{Start: start, End: start}, "punch is outside every scheduled shift"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.Punches = append([]Punch(nil), base.Punches...)
			tc.mutate(&r)
			got, err := Evaluate(r)
			if err != nil || got.Outcome != Exception {
				t.Fatalf("outcome=%s err=%v findings=%+v", got.Outcome, err, got.Exceptions)
			}
			found := false
			for _, e := range got.Exceptions {
				if e.Kind == tc.kind {
					found = true
					if e.Minutes != tc.minutes {
						t.Errorf("minutes=%d want %d", e.Minutes, tc.minutes)
					}
					if durationMinutes(e.Interval) != e.Minutes {
						t.Errorf("interval=%v has %d minutes, finding says %d", e.Interval, durationMinutes(e.Interval), e.Minutes)
					}
					if !e.Interval.Start.Equal(tc.interval.Start) || !e.Interval.End.Equal(tc.interval.End) || e.Reason != tc.reason {
						t.Errorf("finding=%+v want interval=%+v reason=%q", e, tc.interval, tc.reason)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s finding: %+v", tc.kind, got.Exceptions)
			}
		})
	}
}

func TestTodo_ATTEND_002_Property(t *testing.T) {
	r := validRequest()
	start := r.Schedule.Shifts[0].Interval.Start
	r.Schedule.Shifts = []Shift{
		{ID: "second", Interval: Interval{Start: start.Add(10 * time.Hour), End: start.Add(12 * time.Hour)}},
		{ID: "first", Interval: Interval{Start: start, End: start.Add(2 * time.Hour)}},
	}
	r.Punches = []Punch{
		{ID: "in", Ref: ref("p", "1"), At: start.Add(10 * time.Minute)},
		{ID: "out", Ref: ref("p", "1"), At: start.Add(1 * time.Hour)},
		{ID: "second-in", Ref: ref("p", "1"), At: start.Add(11 * time.Hour)},
	}
	a, err := Evaluate(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Evaluate(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Exceptions) != 3 || a.InputDigest != b.InputDigest {
		t.Fatalf("non-deterministic findings: a=%+v b=%+v", a, b)
	}
	if a.Exceptions[0].Kind != LateException || a.Exceptions[1].Kind != EarlyException || a.Exceptions[2].Kind != LateException {
		t.Fatalf("unexpected ordered findings: %+v", a.Exceptions)
	}
}

func TestTodo_ATTEND_002_Property_PunchPermutation(t *testing.T) {
	a := validRequest()
	start := a.Schedule.Shifts[0].Interval.Start
	a.Punches = []Punch{
		{ID: "scheduled", Ref: ref("p", "1"), At: start},
		{ID: "later-unscheduled", Ref: ref("p", "1"), At: start.Add(12 * time.Hour)},
		{ID: "earlier-unscheduled", Ref: ref("p", "1"), At: start.Add(-2 * time.Hour)},
	}
	b := a
	b.Punches = []Punch{a.Punches[2], a.Punches[0], a.Punches[1]}

	gotA, err := Evaluate(a)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := Evaluate(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotA, gotB) {
		t.Fatalf("punch permutation changed complete result:\na=%+v\nb=%+v", gotA, gotB)
	}
	if len(gotA.Exceptions) != 2 || gotA.Exceptions[0].PunchID != "earlier-unscheduled" || gotA.Exceptions[1].PunchID != "later-unscheduled" {
		t.Fatalf("unscheduled findings are not canonical: %+v", gotA.Exceptions)
	}
}

func TestTodo_ATTEND_002_Mutation(t *testing.T) {
	dup := validRequest()
	dup.Schedule.Shifts = append(dup.Schedule.Shifts, dup.Schedule.Shifts[0])
	if _, err := Evaluate(dup); !errors.Is(err, ErrRejected) || !errors.Is(err, ErrInvalidEvidence) || !strings.Contains(err.Error(), "field=schedule.shifts.id state=duplicate:shift-1 version=7") {
		t.Fatalf("duplicate pinned shift must be rejected: %v", err)
	}
	duplicatePunch := validRequest()
	duplicatePunch.Punches = append(duplicatePunch.Punches, duplicatePunch.Punches[0])
	if _, err := Evaluate(duplicatePunch); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "field=punches.id state=duplicate:punch-1 version=12") {
		t.Fatalf("duplicate pinned punch must identify its evidence: %v", err)
	}
	duplicateBreak := validRequest()
	duplicateBreak.Breaks = append(duplicateBreak.Breaks, duplicateBreak.Breaks[0])
	if _, err := Evaluate(duplicateBreak); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "field=breaks.id state=duplicate:break-1 version=4") {
		t.Fatalf("duplicate pinned break must identify its evidence: %v", err)
	}
	overlap := validRequest()
	start := overlap.Schedule.Shifts[0].Interval.Start
	overlap.Schedule.Shifts = append(overlap.Schedule.Shifts, Shift{ID: "overlap", Interval: Interval{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)}})
	if _, err := Evaluate(overlap); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "field=schedule.shifts.interval state=overlap:shift-1:overlap version=7") {
		t.Fatalf("overlapping shifts must reject ambiguous punch assignment: %v", err)
	}
	ambiguous := validRequest()
	ambiguous.Schedule.Shifts = []Shift{
		{ID: "first", Interval: Interval{Start: start, End: start.Add(time.Hour)}},
		{ID: "second", Interval: Interval{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)}},
	}
	ambiguous.Punches[0].At = start.Add(time.Hour)
	if _, err := Evaluate(ambiguous); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "field=punches.assignment state=ambiguous:punch-1:first:second version=12") {
		t.Fatalf("shared boundary punch must not be assigned twice or greedily: %v", err)
	}
	r := validRequest()
	// A DST transition is represented by instants with the same location; the
	// elapsed interval, not wall-clock arithmetic, determines the finding.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start = time.Date(2026, 3, 8, 1, 30, 0, 0, loc)
	r.Context.Timezone = loc.String()
	r.Context.EffectiveAt = start
	r.Context.KnownAt = start
	r.Schedule.Shifts[0].Interval = Interval{Start: start, End: start.Add(2 * time.Hour)}
	r.Punches[0].At = start.Add(20 * time.Minute)
	got, err := Evaluate(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Exceptions) != 1 || got.Exceptions[0].Kind != LateException || got.Exceptions[0].Minutes != 15 {
		t.Fatalf("DST boundary finding=%+v", got.Exceptions)
	}
	fallStart := time.Date(2026, 11, 1, 0, 30, 0, 0, loc)
	r.Context.EffectiveAt = fallStart
	r.Context.KnownAt = fallStart
	r.Schedule.Shifts[0].Interval = Interval{Start: fallStart, End: fallStart.Add(3 * time.Hour)}
	r.Punches[0].At = fallStart.Add(20 * time.Minute)
	got, err = Evaluate(r)
	if err != nil || len(got.Exceptions) != 1 || got.Exceptions[0].Minutes != 15 || got.Exceptions[0].Interval.End.Sub(got.Exceptions[0].Interval.Start) != 15*time.Minute {
		t.Fatalf("fall-back boundary finding=%+v err=%v", got.Exceptions, err)
	}
	reversed := validRequest()
	reversed.Schedule.Shifts[0].Interval.End = reversed.Schedule.Shifts[0].Interval.Start.Add(-time.Minute)
	if _, err := Evaluate(reversed); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("negative elapsed interval must be rejected: %v", err)
	}
}

func TestTodo_ATTEND_002_SplitShiftAssignmentAndMissingEvidence(t *testing.T) {
	r := validRequest()
	start := r.Schedule.Shifts[0].Interval.Start
	r.Schedule.Shifts = []Shift{
		{ID: "morning", Interval: Interval{Start: start, End: start.Add(4 * time.Hour)}},
		{ID: "evening", Interval: Interval{Start: start.Add(6 * time.Hour), End: start.Add(10 * time.Hour)}},
	}
	r.Punches = []Punch{
		{ID: "evening-out", Ref: ref("p", "1"), At: start.Add(10 * time.Hour)},
		{ID: "morning-out", Ref: ref("p", "1"), At: start.Add(4 * time.Hour)},
		{ID: "evening-in", Ref: ref("p", "1"), At: start.Add(6 * time.Hour)},
		{ID: "morning-in", Ref: ref("p", "1"), At: start},
	}
	got, err := Evaluate(r)
	if err != nil || got.Outcome != Compliant || len(got.Exceptions) != 0 || len(got.Shifts) != 2 {
		t.Fatalf("split shift assignment = %+v, err=%v", got, err)
	}

	missing := validRequest()
	missing.Punches = nil
	got, err = Evaluate(missing)
	if err != nil || got.Outcome != Unknown || len(got.Exceptions) != 0 {
		t.Fatalf("absent punch evidence must be unknown, not an inferred absence: %+v, err=%v", got, err)
	}
}

func durationMinutes(interval Interval) int {
	d := interval.End.Sub(interval.Start)
	if d <= 0 {
		return 0
	}
	return int((d + time.Minute - 1) / time.Minute)
}

func TestTodo_ATTEND_002_DirectionMissingSide(t *testing.T) {
	start := validRequest().Schedule.Shifts[0].Interval.Start
	for _, tc := range []struct {
		name      string
		direction PunchDirection
		want      Finding
	}{
		{"out_without_in", PunchOut, Finding{Kind: MissingException, ShiftID: "shift-1", Interval: Interval{Start: start, End: start}, Minutes: 0, Reason: "missing IN arrival evidence"}},
		{"in_without_out", PunchIn, Finding{Kind: MissingException, ShiftID: "shift-1", Interval: Interval{Start: start.Add(8 * time.Hour), End: start.Add(8 * time.Hour)}, Minutes: 0, Reason: "missing OUT departure evidence"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			at := start
			if tc.direction == PunchOut {
				at = r.Schedule.Shifts[0].Interval.End
			}
			r.Punches = []Punch{{ID: "p", Ref: ref("punches", "12"), At: at, Direction: tc.direction}}
			got, err := Evaluate(r)
			wantShift := ShiftResult{ShiftID: "shift-1", Outcome: Exception, Reason: tc.want.Reason, Exceptions: []Finding{tc.want}}
			if err != nil || got.Outcome != Exception || !reflect.DeepEqual(got.Exceptions, []Finding{tc.want}) || !reflect.DeepEqual(got.Shifts, []ShiftResult{wantShift}) {
				t.Fatalf("directioned missing side outcome=%s err=%v findings=%+v", got.Outcome, err, got.Exceptions)
			}
		})
	}
}

func TestTodo_ATTEND_002_DirectionSequenceAndDigest(t *testing.T) {
	if Version() != 2 {
		t.Fatalf("direction-aware contract version=%d want 2", Version())
	}
	start := validRequest().Schedule.Shifts[0].Interval.Start
	valid := validRequest()
	valid.Punches = []Punch{
		{ID: "in-1", Ref: ref("p", "1"), At: start, Direction: PunchIn},
		{ID: "out-1", Ref: ref("p", "1"), At: start.Add(2 * time.Hour), Direction: PunchOut},
		{ID: "in-2", Ref: ref("p", "1"), At: start.Add(3 * time.Hour), Direction: PunchIn},
		{ID: "out-2", Ref: ref("p", "1"), At: start.Add(8 * time.Hour), Direction: PunchOut},
	}
	got, err := Evaluate(valid)
	if err != nil || got.Outcome != Compliant || len(got.Exceptions) != 0 {
		t.Fatalf("ordered IN/OUT sessions = %+v, err=%v", got, err)
	}

	open := valid
	open.Punches = open.Punches[:3]
	got, err = Evaluate(open)
	if err != nil || len(got.Exceptions) != 1 || got.Exceptions[0].Reason != "missing OUT departure evidence" || got.Shifts[0].Early != 0 {
		t.Fatalf("open final session must have an unknown departure: %+v, err=%v", got, err)
	}

	for name, punches := range map[string][]Punch{
		"duplicate_in": {
			{ID: "in-1", Ref: ref("p", "1"), At: start, Direction: PunchIn},
			{ID: "in-2", Ref: ref("p", "1"), At: start.Add(time.Minute), Direction: PunchIn},
		},
		"out_before_in": {
			{ID: "out", Ref: ref("p", "1"), At: start, Direction: PunchOut},
			{ID: "in", Ref: ref("p", "1"), At: start.Add(time.Minute), Direction: PunchIn},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := validRequest()
			r.Punches = punches
			got, err := Evaluate(r)
			if !errors.Is(err, ErrRejected) || got.Outcome != Unknown || len(got.Exceptions) != 0 {
				t.Fatalf("invalid direction stream result=%+v err=%v", got, err)
			}
		})
	}

	legacy := validRequest()
	directed := legacy
	directed.Punches = append([]Punch(nil), legacy.Punches...)
	directed.Punches[0].Direction = PunchIn
	legacyResult, _ := Evaluate(legacy)
	directedResult, _ := Evaluate(directed)
	if legacyResult.InputDigest == directedResult.InputDigest {
		t.Fatal("input digest does not bind punch direction")
	}
}

func TestTodo_ATTEND_002_DirectionMissingSideDoesNotDoubleCount(t *testing.T) {
	start := validRequest().Schedule.Shifts[0].Interval.Start
	for _, tc := range []struct {
		name      string
		punch     Punch
		otherKind ExceptionKind
	}{
		{"late_in", Punch{ID: "late-in", Ref: ref("p", "1"), At: start.Add(20 * time.Minute), Direction: PunchIn}, LateException},
		{"early_out", Punch{ID: "early-out", Ref: ref("p", "1"), At: start.Add(7 * time.Hour), Direction: PunchOut}, EarlyException},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			r.Punches = []Punch{tc.punch}
			got, err := Evaluate(r)
			if err != nil || len(got.Exceptions) != 2 {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			missing, quantified := got.Exceptions[0], got.Exceptions[1]
			if missing.Kind != MissingException || missing.Minutes != 0 || !missing.Interval.Start.Equal(missing.Interval.End) || quantified.Kind != tc.otherKind || quantified.Minutes <= 0 {
				t.Fatalf("missing side and quantified finding overlap or double count: %+v", got.Exceptions)
			}
		})
	}
}
