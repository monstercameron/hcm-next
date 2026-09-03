package legal

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// AttributionRule identifies the first attribution rule that resolved a set.
type AttributionRule string

const (
	AttributionA1 AttributionRule = "A1"
	AttributionA2 AttributionRule = "A2"
	AttributionA3 AttributionRule = "A3"
	AttributionA4 AttributionRule = "A4"
	AttributionA5 AttributionRule = "A5"
	AttributionA6 AttributionRule = "A6"
)

// ErrMultiStateUnresolved is wrapped by ErrLegalContextUnknown when remote
// work is split across states without a state meeting the configured share.
var ErrMultiStateUnresolved = errors.New("legal: MULTI_STATE_UNRESOLVED")

// ScheduledWorkLocation attributes a fraction of the attribution window to a
// physical work jurisdiction. Share is a proportion in [0,1].
type ScheduledWorkLocation struct {
	Jurisdiction Jurisdiction
	Share        float64
}

// JurisdictionSetInput contains facts used by ResolveJurisdictionSet. It is
// deliberately separate from LegalContextInput so callers cannot accidentally
// treat a single work location as a multi-state schedule.
type JurisdictionSetInput struct {
	EmploymentJurisdiction Jurisdiction
	RemoteWork             bool
	WorkLocations          []ScheduledWorkLocation
	PrimaryWorkThreshold   *float64
	// EffectiveDate is used only to determine whether a locality release is
	// registered; it does not affect attribution.
	EffectiveDate values.LocalDate
}

// JurisdictionSet is one primary subdivision and any registered locality
// overlays. Slices are defensive copies at API boundaries.
type JurisdictionSet struct {
	Primary                 Jurisdiction
	Overlays                []Jurisdiction
	Confidence              Confidence
	AttributionRule         AttributionRule
	RemoteWorkPolicyApplied string
	MultiStateExposures     []Jurisdiction
	UnregisteredLocalities  []Jurisdiction
}

// ResolveJurisdictionSet applies A1-A6 from the legal jurisdiction contract.
// A locality is an overlay only when an exact locality release is registered;
// an unregistered locality is retained in the result for receipt handling.
func ResolveJurisdictionSet(input JurisdictionSetInput, registry *Registry) (JurisdictionSet, error) {
	if err := input.EmploymentJurisdiction.Validate(); err != nil {
		return JurisdictionSet{}, fmt.Errorf("%w: employment jurisdiction: %v", ErrLegalContextUnknown, err)
	}
	if len(input.WorkLocations) == 0 {
		return JurisdictionSet{}, fmt.Errorf("%w: work location is required", ErrLegalContextUnknown)
	}
	if input.EffectiveDate.Validate() != nil {
		return JurisdictionSet{}, fmt.Errorf("%w: effective date is required", ErrLegalContextUnknown)
	}
	shares := map[Jurisdiction]float64{}
	for _, w := range input.WorkLocations {
		if err := w.Jurisdiction.Validate(); err != nil || w.Jurisdiction.State == "" || w.Share < 0 {
			return JurisdictionSet{}, fmt.Errorf("%w: invalid scheduled work location", ErrLegalContextUnknown)
		}
		base := Jurisdiction{Country: w.Jurisdiction.Country, State: w.Jurisdiction.State}
		shares[base] += w.Share
	}
	if !input.RemoteWork {
		if len(shares) != 1 || !sameStateOnly(shares, input.EmploymentJurisdiction) {
			return JurisdictionSet{}, fmt.Errorf("%w: on-site work location disagrees with asserted employment jurisdiction", ErrLegalContextUnknown)
		}
		return finishSet(JurisdictionSet{Primary: Jurisdiction{Country: input.EmploymentJurisdiction.Country, State: input.EmploymentJurisdiction.State}, Confidence: ConfidenceVerified, AttributionRule: AttributionA1, RemoteWorkPolicyApplied: "not_remote"}, input, registry)
	}
	if len(shares) == 1 {
		for j := range shares {
			confidence := ConfidenceVerified
			if !j.Equal(input.EmploymentJurisdiction) {
				confidence = ConfidenceAsserted
			}
			return finishSet(JurisdictionSet{Primary: j, Confidence: confidence, AttributionRule: AttributionA3, RemoteWorkPolicyApplied: "physical_work_location_controls"}, input, registry)
		}
	}
	if input.PrimaryWorkThreshold == nil || *input.PrimaryWorkThreshold <= 0 || *input.PrimaryWorkThreshold > 1 {
		return JurisdictionSet{}, fmt.Errorf("%w: %w: primary_work_threshold is required", ErrLegalContextUnknown, ErrMultiStateUnresolved)
	}
	var primary Jurisdiction
	for j, share := range shares {
		if share >= *input.PrimaryWorkThreshold {
			if primary != (Jurisdiction{}) {
				return JurisdictionSet{}, fmt.Errorf("%w: %w", ErrLegalContextUnknown, ErrMultiStateUnresolved)
			}
			primary = j
		}
	}
	if primary.IsZero() {
		return JurisdictionSet{}, fmt.Errorf("%w: %w", ErrLegalContextUnknown, ErrMultiStateUnresolved)
	}
	set := JurisdictionSet{Primary: primary, Confidence: ConfidenceAsserted, AttributionRule: AttributionA4, RemoteWorkPolicyApplied: "physical_work_location_controls"}
	for j, share := range shares {
		if share > 0 && !j.Equal(primary) {
			set.MultiStateExposures = append(set.MultiStateExposures, j)
		}
	}
	sort.Slice(set.MultiStateExposures, func(i, j int) bool { return set.MultiStateExposures[i].String() < set.MultiStateExposures[j].String() })
	return finishSet(set, input, registry)
}

func sameStateOnly(shares map[Jurisdiction]float64, asserted Jurisdiction) bool {
	for j := range shares {
		if j.Country != asserted.Country || j.State != asserted.State {
			return false
		}
	}
	return true
}

func finishSet(set JurisdictionSet, input JurisdictionSetInput, registry *Registry) (JurisdictionSet, error) {
	if registry == nil {
		return JurisdictionSet{}, fmt.Errorf("%w: no rule-pack registry supplied", ErrLegalContextUnknown)
	}
	// Locality overlays are supplied as locality-bearing work facts. A
	// locality release must be exact; Registry.Lookup intentionally falls back
	// to the state release, so use IsRegisteredExact here.
	seenOverlays := map[Jurisdiction]struct{}{}
	seenUnregistered := map[Jurisdiction]struct{}{}
	for _, w := range input.WorkLocations {
		if w.Jurisdiction.Locality == "" {
			continue
		}
		if registry.IsRegisteredExact(w.Jurisdiction, input.EffectiveDate) {
			if _, seen := seenOverlays[w.Jurisdiction]; !seen {
				seenOverlays[w.Jurisdiction] = struct{}{}
				set.Overlays = append(set.Overlays, w.Jurisdiction)
			}
		} else {
			if _, seen := seenUnregistered[w.Jurisdiction]; !seen {
				seenUnregistered[w.Jurisdiction] = struct{}{}
				set.UnregisteredLocalities = append(set.UnregisteredLocalities, w.Jurisdiction)
			}
		}
	}
	sort.Slice(set.Overlays, func(i, j int) bool { return set.Overlays[i].String() < set.Overlays[j].String() })
	return set, nil
}
