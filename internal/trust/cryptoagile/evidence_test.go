package cryptoagile

import (
	"errors"
	"reflect"
	"testing"
)

func threeWindowPlan() MigrationPlan {
	return MigrationPlan{Windows: []Window{
		{Start: day(0), End: day(10), ActiveSuiteID: "a"},
		{Start: day(10), End: day(20), ActiveSuiteID: "b", DualSuiteID: "a"},
		{Start: day(20), ActiveSuiteID: "b"},
	}}
}

func TestResume_FromScratch(t *testing.T) {
	plan := threeWindowPlan()
	got, err := Resume(plan, nil, day(25))
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	want := []Evidence{
		{Index: 0, ActiveSuiteID: "a", At: day(0)},
		{Index: 1, ActiveSuiteID: "b", DualSuiteID: "a", At: day(10)},
		{Index: 2, ActiveSuiteID: "b", At: day(20)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resume(nil, day25) = %+v, want %+v", got, want)
	}
}

func TestResume_AlreadyCaughtUpReturnsNothing(t *testing.T) {
	plan := threeWindowPlan()
	recorded := []Evidence{{Index: 0, ActiveSuiteID: "a", At: day(0)}}
	got, err := Resume(plan, recorded, day(5))
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Resume(caught up) = %+v, want no new evidence", got)
	}
}

func TestResume_RefusesTimeOutsidePlan(t *testing.T) {
	plan := threeWindowPlan()
	if _, err := Resume(plan, nil, day(-1)); err == nil {
		t.Fatalf("Resume(before plan start) = nil, want error")
	}
}

func TestResume_RefusesRecordedAheadOfNow(t *testing.T) {
	plan := threeWindowPlan()
	recorded := []Evidence{{Index: 2, ActiveSuiteID: "b", At: day(20)}}
	if _, err := Resume(plan, recorded, day(5)); err == nil {
		t.Fatalf("Resume(recorded ahead of now) = nil, want error")
	}
}

func TestResume_InvalidPlanRejected(t *testing.T) {
	if _, err := Resume(MigrationPlan{}, nil, day(0)); !errors.Is(err, ErrPlanEmpty) {
		t.Fatalf("Resume(invalid plan) = %v, want ErrPlanEmpty", err)
	}
}
