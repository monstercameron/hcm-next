package legal

import (
	"errors"
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
