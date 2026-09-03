// Package continuity defines the evidence contract for completing a user flow
// through browser, accessibility-assisted, and manual continuity routes.
// Presentation is deliberately separate from the normalized intent: a route
// may adapt controls and language, but may not change authority, meaning,
// deadline, privacy, attribution, or the resulting intent.
package continuity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Route string

const (
	RouteBrowser      Route = "browser"
	RouteKeyboard     Route = "keyboard"
	RouteScreenReader Route = "screen_reader"
	RouteZoom         Route = "zoom"
	RouteRTL          Route = "rtl"
	RouteAssisted     Route = "assisted"
	RouteManual       Route = "manual"
	// Descriptive aliases used by integrations.
	RouteAccessibilityAssisted Route = RouteAssisted
	RouteManualContinuity      Route = RouteManual
)

// Stage is an ordered, user-visible part of a complete flow.
type Stage struct {
	ID       string
	Required bool
}

// FlowSpec pins the version and ownership of a flow under test.
type FlowSpec struct {
	ID, Version, Owner string
	Stages             []Stage
}

// Request contains semantic input. Values are already canonical (for
// example, dates use YYYY-MM-DD and amounts use a period decimal separator).
type Request struct {
	FlowID, FlowVersion                               string
	Subject, Actor                                    string
	Meaning                                           string
	Intent                                            map[string]string
	Deadline                                          time.Time
	PrivacyClass                                      string
	Locale, TemplateVersion                           string
	Route                                             Route
	WCAGEvidence, FocusEvidence, AnnouncementEvidence string
	AssistedBy, InterpreterFor, Representative        string
	ManualReceipt                                     string
	At                                                time.Time
}

// Evidence binds the semantic request to the person and route that produced
// it. Digest is over normalized intent, not localized labels or control order.
type Evidence struct {
	FlowID, FlowVersion, Subject, Actor, Meaning              string
	Deadline                                                  time.Time
	PrivacyClass, Locale, TemplateVersion, Route              string
	AssistedBy, InterpreterFor, Representative, ManualReceipt string
	IntentDigest                                              string
	Stages                                                    []string
}

type Outcome struct {
	NormalizedIntent map[string]string
	IntentDigest     string
	Result           string
	Evidence         Evidence
}

// Flow, FlowRequest, and FlowOutcome are concise aliases useful to callers
// that model a complete flow directly.
type Flow = FlowSpec
type FlowRequest = Request
type FlowOutcome = Outcome

var (
	ErrInvalidFlow   = errors.New("continuity: invalid flow")
	ErrInvalidRoute  = errors.New("continuity: invalid route evidence")
	ErrSemanticDrift = errors.New("continuity: semantic drift")
	ErrDeadline      = errors.New("continuity: deadline unavailable or changed")
	ErrPrivacy       = errors.New("continuity: privacy contract missing")
	ErrAttribution   = errors.New("continuity: assisted/manual attribution missing")
)

func (f FlowSpec) Validate() error {
	if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Version) == "" || strings.TrimSpace(f.Owner) == "" || len(f.Stages) == 0 {
		return ErrInvalidFlow
	}
	seen := map[string]bool{}
	for _, s := range f.Stages {
		if strings.TrimSpace(s.ID) == "" || seen[s.ID] {
			return fmt.Errorf("%w: duplicate or empty stage", ErrInvalidFlow)
		}
		seen[s.ID] = true
	}
	return nil
}

func normalized(in map[string]string) (map[string]string, string, error) {
	if len(in) == 0 {
		return nil, "", fmt.Errorf("%w: empty intent", ErrSemanticDrift)
	}
	keys := make([]string, 0, len(in))
	out := make(map[string]string, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return nil, "", fmt.Errorf("%w: empty key", ErrSemanticDrift)
		}
		if _, ok := out[k]; ok {
			return nil, "", fmt.Errorf("%w: duplicate key", ErrSemanticDrift)
		}
		out[k] = v
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%d:%s=%d:%s;", len(k), k, len(out[k]), out[k])
	}
	h := sha256.Sum256([]byte(b.String()))
	return out, hex.EncodeToString(h[:]), nil
}

func routeValid(r Request) error {
	switch r.Route {
	case RouteBrowser:
		if r.WCAGEvidence == "" || r.FocusEvidence == "" || r.AnnouncementEvidence == "" {
			return ErrInvalidRoute
		}
		return nil
	case RouteKeyboard, RouteScreenReader, RouteZoom, RouteRTL:
		if r.WCAGEvidence == "" || r.FocusEvidence == "" || r.AnnouncementEvidence == "" {
			return ErrInvalidRoute
		}
		return nil
	case RouteAssisted:
		if r.WCAGEvidence == "" || r.FocusEvidence == "" || r.AnnouncementEvidence == "" {
			return ErrInvalidRoute
		}
		if strings.TrimSpace(r.AssistedBy) == "" {
			return ErrAttribution
		}
		return nil
	case RouteManual:
		if strings.TrimSpace(r.ManualReceipt) == "" {
			return ErrAttribution
		}
		return nil
	default:
		return fmt.Errorf("%w: route %q", ErrInvalidRoute, r.Route)
	}
}

// Execute validates a complete-flow attempt and returns a route-independent
// normalized result. It intentionally does not grant authority or perform a
// business mutation.
func Execute(flow FlowSpec, req Request) (Outcome, error) {
	if err := flow.Validate(); err != nil {
		return Outcome{}, err
	}
	if req.FlowID != flow.ID || req.FlowVersion != flow.Version || req.Subject == "" || req.Actor == "" || req.Meaning == "" {
		return Outcome{}, fmt.Errorf("%w: identity or pinned version", ErrSemanticDrift)
	}
	if req.Deadline.IsZero() || req.At.IsZero() || req.At.After(req.Deadline) {
		return Outcome{}, ErrDeadline
	}
	if req.PrivacyClass == "" {
		return Outcome{}, ErrPrivacy
	}
	if strings.TrimSpace(req.Locale) == "" || strings.TrimSpace(req.TemplateVersion) == "" {
		return Outcome{}, fmt.Errorf("%w: reviewed locale/template required", ErrInvalidRoute)
	}
	if err := routeValid(req); err != nil {
		return Outcome{}, err
	}
	intent, digest, err := normalized(req.Intent)
	if err != nil {
		return Outcome{}, err
	}
	if req.Route == RouteManual && req.ManualReceipt != digest {
		return Outcome{}, fmt.Errorf("%w: manual receipt digest", ErrSemanticDrift)
	}
	if req.Route == RouteAssisted && req.AssistedBy == req.Subject {
		return Outcome{}, ErrAttribution
	}
	stageIDs := make([]string, len(flow.Stages))
	for i, s := range flow.Stages {
		stageIDs[i] = s.ID
	}
	ev := Evidence{FlowID: flow.ID, FlowVersion: flow.Version, Subject: req.Subject, Actor: req.Actor, Meaning: req.Meaning, Deadline: req.Deadline, PrivacyClass: req.PrivacyClass, Locale: req.Locale, TemplateVersion: req.TemplateVersion, Route: string(req.Route), AssistedBy: req.AssistedBy, InterpreterFor: req.InterpreterFor, Representative: req.Representative, ManualReceipt: req.ManualReceipt, IntentDigest: digest, Stages: stageIDs}
	return Outcome{NormalizedIntent: intent, IntentDigest: digest, Result: "COMPLETE", Evidence: ev}, nil
}

// Run is the short form of Execute.
func Run(flow FlowSpec, req Request) (Outcome, error) { return Execute(flow, req) }

// ValidateCompleteFlow validates an attempt without performing any effect.
func ValidateCompleteFlow(flow FlowSpec, req Request) error { _, err := Execute(flow, req); return err }

// Equivalent reports whether two successful route outcomes retain every
// semantic field and the normalized intent digest.
func Equivalent(a, b Outcome) bool {
	if a.Result != "COMPLETE" || b.Result != "COMPLETE" || a.IntentDigest != b.IntentDigest {
		return false
	}
	x, y := a.Evidence, b.Evidence
	return x.FlowID == y.FlowID && x.FlowVersion == y.FlowVersion && x.Subject == y.Subject && x.Meaning == y.Meaning && x.Deadline.Equal(y.Deadline) && x.PrivacyClass == y.PrivacyClass
}

// NormalizeIntent exposes the canonical digest used by receipts and evidence.
func NormalizeIntent(intent map[string]string) (map[string]string, string, error) {
	return normalized(intent)
}
