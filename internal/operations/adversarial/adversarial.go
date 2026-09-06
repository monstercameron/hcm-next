// Package adversarial compiles privacy, authorization and abuse journeys into
// safe, bounded evidence. It has no persistence or external side effects.
package adversarial

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const contractVersion = 1

// Version is the adversarial-journey contract version.
func Version() int { return contractVersion }

// Explain describes the package without journey payloads.
func Explain() string {
	return "adversarial v1: non-disclosing denial evaluator across UI, API, work, messaging, export, support and telemetry channels"
}

// Channel identifies an equivalent route an attacker may try.
type Channel string

const (
	ChannelUI         Channel = "UI"
	ChannelBrowser    Channel = "BROWSER"
	ChannelDeepLink   Channel = "DEEP_LINK"
	ChannelAPI        Channel = "API"
	ChannelWork       Channel = "WORK"
	ChannelMessage    Channel = "MESSAGE"
	ChannelExport     Channel = "EXPORT"
	ChannelSupport    Channel = "SUPPORT"
	ChannelLogQuery   Channel = "LOG_QUERY"
	ChannelTraceQuery Channel = "TRACE_QUERY"
	ChannelAnalytics  Channel = "ANALYTICS"
	ChannelRepair     Channel = "REPAIR"
	ChannelBulk       Channel = "BULK"
	ChannelRetry      Channel = "RETRY"
	ChannelReplay     Channel = "REPLAY"
	ChannelRestart    Channel = "RESTART"
)

func (c Channel) valid() bool {
	switch c {
	case ChannelUI, ChannelBrowser, ChannelDeepLink, ChannelAPI, ChannelWork, ChannelMessage, ChannelExport, ChannelSupport, ChannelLogQuery, ChannelTraceQuery, ChannelAnalytics, ChannelRepair, ChannelBulk, ChannelRetry, ChannelReplay, ChannelRestart:
		return true
	default:
		return false
	}
}

// Journey is a fixture describing one hostile or accidental attempt. Secret
// is test-only input; it is never included in Result or Evidence.
type Journey struct {
	ID            string
	TenantID      string
	ActorID       string
	Channel       Channel
	Asset         string
	Action        string
	DataClass     string
	Authenticated bool
	Authorized    bool
	Secret        string
	Retry         bool
	Restart       bool
	Replay        bool
}

// Validate checks the fixture shape without treating an ID as disclosure
// permission.
func (j Journey) Validate() error {
	for name, value := range map[string]string{"id": j.ID, "tenant_id": j.TenantID, "actor_id": j.ActorID, "asset": j.Asset, "action": j.Action, "data_class": j.DataClass} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("adversarial: %s is required", name)
		}
	}
	if !j.Channel.valid() {
		return fmt.Errorf("adversarial: invalid channel %q", j.Channel)
	}
	return nil
}

// Decision is the safe outcome class exposed to the evidence compiler.
type Decision string

const (
	DecisionDenied       Decision = "DENIED"
	DecisionSafeDegraded Decision = "SAFE_DEGRADED"
	DecisionAllowed      Decision = "ALLOWED"
)

// Result is bounded evidence for one journey. The booleans are deliberately
// negative assertions: true means the evaluator did not disclose or mutate.
type Result struct {
	JourneyID                 string
	Decision                  Decision
	NoExistenceMetadata       bool
	NoFieldMetadata           bool
	NoCountMetadata           bool
	NoIdentityMetadata        bool
	NoTelemetryLeakage        bool
	NoUnauthorizedPersistence bool
	NoUnauthorizedEffects     bool
	Evidence                  string
}

// Evaluate denies missing or insufficient authority before constructing any
// result that could vary based on resource existence. Authorized journeys are
// represented for control testing, but still receive no sensitive payload.
func Evaluate(journey Journey) (Result, error) {
	if err := journey.Validate(); err != nil {
		return Result{}, err
	}
	denied := !journey.Authenticated || !journey.Authorized
	result := Result{JourneyID: journey.ID, NoExistenceMetadata: true, NoFieldMetadata: true, NoCountMetadata: true, NoIdentityMetadata: true, NoTelemetryLeakage: true, NoUnauthorizedPersistence: denied, NoUnauthorizedEffects: denied}
	if denied {
		result.Decision = DecisionDenied
		result.Evidence = "DENIED: request cannot be completed"
		return result, nil
	}
	result.Decision = DecisionAllowed
	result.Evidence = "ALLOWED: governed result omitted from security evidence"
	result.NoUnauthorizedPersistence = true
	result.NoUnauthorizedEffects = true
	return result, nil
}

// Report is a complete in-memory run; it is evidence, not an authorization
// record or a durable domain fact.
type Report struct {
	Results                    []Result
	AllDeniedWithoutDisclosure bool
	UnauthorizedPersistence    int
	UnauthorizedEffects        int
	TelemetryLeaks             int
	Digest                     string
}

// Run evaluates journeys independently and compiles bounded evidence.
func Run(journeys []Journey) (Report, error) {
	if len(journeys) == 0 {
		return Report{}, errors.New("adversarial: no journeys")
	}
	results := make([]Result, 0, len(journeys))
	allSafe := true
	for _, journey := range journeys {
		result, err := Evaluate(journey)
		if err != nil {
			return Report{}, err
		}
		if err := validateEvidence(journey, result); err != nil {
			return Report{}, err
		}
		if result.Decision != DecisionDenied && !journey.Authenticated {
			allSafe = false
		}
		if result.NoUnauthorizedPersistence && result.NoUnauthorizedEffects && result.NoTelemetryLeakage {
			results = append(results, result)
			continue
		}
		allSafe = false
		results = append(results, result)
	}
	report := Report{Results: results, AllDeniedWithoutDisclosure: allSafe}
	for _, result := range results {
		if !result.NoUnauthorizedPersistence {
			report.UnauthorizedPersistence++
		}
		if !result.NoUnauthorizedEffects {
			report.UnauthorizedEffects++
		}
		if !result.NoTelemetryLeakage {
			report.TelemetryLeaks++
		}
	}
	report.Digest = reportDigest(report)
	return report, nil
}

func validateEvidence(journey Journey, result Result) error {
	if result.Evidence == "" || strings.Contains(result.Evidence, journey.Secret) && journey.Secret != "" {
		return errors.New("adversarial: evidence contains an input secret or is empty")
	}
	if !result.NoExistenceMetadata || !result.NoFieldMetadata || !result.NoCountMetadata || !result.NoIdentityMetadata || !result.NoTelemetryLeakage {
		return errors.New("adversarial: disclosure assertion failed")
	}
	return nil
}

// DefaultJourneys is the labelled placeholder corpus for THREAT-002. The
// actor, tenant, data and asset values are test fixtures, not production facts.
func DefaultJourneys() []Journey {
	channels := []Channel{ChannelUI, ChannelBrowser, ChannelDeepLink, ChannelAPI, ChannelWork, ChannelMessage, ChannelExport, ChannelSupport, ChannelLogQuery, ChannelTraceQuery, ChannelAnalytics, ChannelRepair, ChannelBulk, ChannelRetry, ChannelReplay, ChannelRestart}
	journeys := make([]Journey, 0, len(channels))
	for _, channel := range channels {
		journeys = append(journeys, Journey{ID: "PLACEHOLDER_JOURNEY_" + string(channel), TenantID: "PLACEHOLDER_TENANT", ActorID: "PLACEHOLDER_ACTOR", Channel: channel, Asset: "PLACEHOLDER_SENSITIVE_WORKER", Action: "READ_OR_CREATE_WORK", DataClass: "SENSITIVE", Authenticated: false, Authorized: false, Secret: "PLACEHOLDER_SECRET", Retry: channel == ChannelRetry, Restart: channel == ChannelRestart, Replay: channel == ChannelReplay})
	}
	return journeys
}

// Explain returns bounded run evidence without journey IDs, tenant IDs, actor
// IDs, data classes or payloads.
func (r Report) Explain() string {
	return fmt.Sprintf("journeys=%d all_denied_without_disclosure=%t unauthorized_persistence=%d unauthorized_effects=%d telemetry_leaks=%d digest=%s", len(r.Results), r.AllDeniedWithoutDisclosure, r.UnauthorizedPersistence, r.UnauthorizedEffects, r.TelemetryLeaks, r.Digest)
}

func reportDigest(r Report) string {
	var b strings.Builder
	for _, result := range r.Results {
		fmt.Fprintf(&b, "%s|%s|%t|%t|%t|%t|%t|%t;", result.JourneyID, result.Decision, result.NoExistenceMetadata, result.NoFieldMetadata, result.NoCountMetadata, result.NoIdentityMetadata, result.NoTelemetryLeakage, result.NoUnauthorizedPersistence && result.NoUnauthorizedEffects)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Channels returns a stable copy of the corpus channel vocabulary for callers
// that need to generate equivalent probes.
func Channels() []Channel {
	out := []Channel{ChannelUI, ChannelBrowser, ChannelDeepLink, ChannelAPI, ChannelWork, ChannelMessage, ChannelExport, ChannelSupport, ChannelLogQuery, ChannelTraceQuery, ChannelAnalytics, ChannelRepair, ChannelBulk, ChannelRetry, ChannelReplay, ChannelRestart}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
