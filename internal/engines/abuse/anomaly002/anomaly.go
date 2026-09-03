// Package anomaly002 owns the pure ABUSE-002 anomaly detector.  It consumes
// already-normalized activity observations and returns a review signal; it has
// no persistence, queue, provider, or accusation side effects.
package anomaly002

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	RejectedCode = "ABUSE_002_REJECTED"
	AcceptedCode = "ABUSE_002_ACCEPTED"
)

var (
	ErrDefinition = errors.New("abuse002: invalid detector definition")
	ErrActivity   = errors.New("abuse002: invalid activity")
)

// Activity is the minimized, governed representation of one privileged or
// sensitive-data operation. Raw payloads and protected attributes are not
// accepted by this engine.
type Activity struct {
	ID, Actor, Tenant, Resource, Location, Version string
	Hour, Volume, Scope, Sequence                  int
	Privileged, Sensitive                          bool
	ApprovedWork, IncidentWork                     bool
}

// Definition pins all anomaly boundaries. A zero definition is not silently
// defaulted: versioned detector policy is part of the evaluation evidence.
type Definition struct {
	ID, Version, Owner  string
	MaxVolume, MaxScope int
	AllowedHours        [2]int
	AllowedLocations    []string
}

// Finding is a review-oriented result. It never names an accused person and
// is safe to hand to a caller as a signal requiring governed follow-up.
type Finding struct {
	Code, ActivityID, OffendingField, State, Version string
}

type Result struct {
	Code    string
	Finding *Finding
}

func (d Definition) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" || strings.TrimSpace(d.Owner) == "" {
		return ErrDefinition
	}
	if d.MaxVolume <= 0 || d.MaxScope <= 0 || d.AllowedHours[0] < 0 || d.AllowedHours[1] > 24 || d.AllowedHours[0] >= d.AllowedHours[1] {
		return ErrDefinition
	}
	if len(d.AllowedLocations) == 0 {
		return ErrDefinition
	}
	return nil
}

func (a Activity) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Actor) == "" || strings.TrimSpace(a.Tenant) == "" || strings.TrimSpace(a.Resource) == "" || strings.TrimSpace(a.Version) == "" {
		return ErrActivity
	}
	if a.Hour < 0 || a.Hour > 23 || a.Volume < 0 || a.Scope < 0 || a.Sequence < 0 {
		return ErrActivity
	}
	if !a.Privileged && !a.Sensitive {
		return ErrActivity
	}
	return nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// Detect evaluates observations in input order and returns the first stable,
// deterministic anomaly. Approved batch and incident work is explicitly
// exempt from anomaly rejection (while still requiring valid observations).
func Detect(d Definition, activities []Activity) (Result, error) {
	if err := d.Validate(); err != nil {
		return Result{}, err
	}
	ordered := append([]Activity(nil), activities...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, a := range ordered {
		if err := a.Validate(); err != nil {
			return Result{}, fmt.Errorf("%w: %s", ErrActivity, a.ID)
		}
		if a.ApprovedWork || a.IncidentWork {
			continue
		}
		field := ""
		switch {
		case a.Hour < d.AllowedHours[0] || a.Hour >= d.AllowedHours[1]:
			field = "time"
		case !contains(d.AllowedLocations, a.Location):
			field = "location"
		case a.Volume > d.MaxVolume:
			field = "volume"
		case a.Scope > d.MaxScope:
			field = "scope"
		case a.Sequence > 1:
			field = "sequence"
		}
		if field != "" {
			return Result{Code: RejectedCode, Finding: &Finding{Code: RejectedCode, ActivityID: a.ID, OffendingField: field, State: "REVIEW_REQUIRED", Version: d.Version}}, nil
		}
	}
	return Result{Code: AcceptedCode}, nil
}
