package legal

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTodo_LEGAL_009(t *testing.T) {
	reg := testRegistry(t)
	d := mustDate(t, 2026, time.March, 1)

	t.Run("A4 requires threshold and records exposure", func(t *testing.T) {
		threshold := .6
		set, err := ResolveJurisdictionSet(JurisdictionSetInput{
			EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true,
			PrimaryWorkThreshold: &threshold, EffectiveDate: d,
			WorkLocations: []ScheduledWorkLocation{{Jurisdiction: testCAJurisdiction(), Share: .7}, {Jurisdiction: testNYJurisdiction(), Share: .3}},
		}, reg)
		if err != nil || set.Primary != testCAJurisdiction() || set.AttributionRule != AttributionA4 || len(set.MultiStateExposures) != 1 {
			t.Fatalf("set=%+v err=%v", set, err)
		}
	})

	t.Run("A5 fails closed", func(t *testing.T) {
		threshold := .6
		_, err := ResolveJurisdictionSet(JurisdictionSetInput{
			EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true, PrimaryWorkThreshold: &threshold, EffectiveDate: d,
			WorkLocations: []ScheduledWorkLocation{{Jurisdiction: testCAJurisdiction(), Share: .5}, {Jurisdiction: testNYJurisdiction(), Share: .5}},
		}, reg)
		if !errors.Is(err, ErrLegalContextUnknown) || !errors.Is(err, ErrMultiStateUnresolved) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("A6 keeps an unregistered locality as receipt input", func(t *testing.T) {
		set, err := ResolveJurisdictionSet(JurisdictionSetInput{
			EmploymentJurisdiction: Jurisdiction{Country: "US", State: "CA"}, EffectiveDate: d,
			WorkLocations: []ScheduledWorkLocation{{Jurisdiction: Jurisdiction{Country: "US", State: "CA", Locality: "San Francisco"}, Share: 1}},
		}, reg)
		if err != nil || len(set.UnregisteredLocalities) != 1 || len(set.Overlays) != 0 {
			t.Fatalf("set=%+v err=%v", set, err)
		}
	})
}

func TestTodo_LEGAL_009_Golden(t *testing.T) {
	reg := testRegistry(t)
	d := mustDate(t, 2026, time.March, 1)
	threshold := .6
	set, err := ResolveJurisdictionSet(JurisdictionSetInput{
		EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true,
		PrimaryWorkThreshold: &threshold, EffectiveDate: d,
		WorkLocations: []ScheduledWorkLocation{
			{Jurisdiction: testCAJurisdiction(), Share: .7},
			{Jurisdiction: testNYJurisdiction(), Share: .3},
		},
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := set.Primary.String()+"|"+set.AttributionRule.String()+"|"+set.RemoteWorkPolicyApplied, "US-CA|A4|physical_work_location_controls"; got != want {
		t.Fatalf("golden set = %q, want %q", got, want)
	}
}

func (a AttributionRule) String() string { return string(a) }

func TestTodo_LEGAL_009_Property(t *testing.T) {
	reg := testRegistry(t)
	d := mustDate(t, 2026, time.March, 1)
	set, err := ResolveJurisdictionSet(JurisdictionSetInput{
		EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true, EffectiveDate: d,
		WorkLocations: []ScheduledWorkLocation{{Jurisdiction: testCAJurisdiction(), Share: 1}},
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	set.MultiStateExposures = append(set.MultiStateExposures, testNYJurisdiction())
	set2, err := ResolveJurisdictionSet(JurisdictionSetInput{
		EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true, EffectiveDate: d,
		WorkLocations: []ScheduledWorkLocation{{Jurisdiction: testCAJurisdiction(), Share: 1}},
	}, reg)
	if err != nil || len(set2.MultiStateExposures) != 0 {
		t.Fatalf("result aliased or changed: %+v %v", set2, err)
	}
}

func TestTodo_LEGAL_009_Race(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	d := mustDate(t, 2026, time.March, 1)
	threshold := .6
	in := JurisdictionSetInput{EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true, PrimaryWorkThreshold: &threshold, EffectiveDate: d, WorkLocations: []ScheduledWorkLocation{{Jurisdiction: testCAJurisdiction(), Share: .7}, {Jurisdiction: testNYJurisdiction(), Share: .3}}}
	t.Run("parallel", func(t *testing.T) {
		t.Parallel()
		for i := 0; i < 20; i++ {
			if _, err := ResolveJurisdictionSet(in, reg); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestTodo_LEGAL_009_Security(t *testing.T) {
	reg := testRegistry(t)
	d := mustDate(t, 2026, time.March, 1)
	threshold := .6
	_, err := ResolveJurisdictionSet(JurisdictionSetInput{EmploymentJurisdiction: testCAJurisdiction(), RemoteWork: true, PrimaryWorkThreshold: &threshold, EffectiveDate: d, WorkLocations: []ScheduledWorkLocation{{Jurisdiction: Jurisdiction{Country: "US", State: "CA", Locality: " San Francisco"}, Share: 1}}}, reg)
	if !errors.Is(err, ErrLegalContextUnknown) {
		t.Fatalf("invalid locality accepted: %v", err)
	}
}

func TestTodo_LEGAL_009_Mutation(t *testing.T) {
	reg := testRegistry(t)
	d := mustDate(t, 2026, time.March, 1)
	loc := Jurisdiction{Country: "US", State: "CA", Locality: "San Francisco"}
	// Duplicate facts must not duplicate receipt inputs.
	set, err := ResolveJurisdictionSet(JurisdictionSetInput{EmploymentJurisdiction: testCAJurisdiction(), EffectiveDate: d, WorkLocations: []ScheduledWorkLocation{{Jurisdiction: loc, Share: 1}, {Jurisdiction: loc, Share: 0}}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.UnregisteredLocalities, []Jurisdiction{loc}) {
		t.Fatalf("duplicates leaked: %+v", set.UnregisteredLocalities)
	}
}
