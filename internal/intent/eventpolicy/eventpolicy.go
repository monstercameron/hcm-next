// Package eventpolicy converts internal and external event observations
// into policy-bound intents (INTENT-018). Events describe observations and
// occurrences; a versioned trigger policy chooses whether a business
// instruction should exist. The policy filters event type, schema and
// version, source and scope; current truth is resolved where required and
// never taken from the payload; one accepted event names bounded intents
// under a deterministic causal key; ignored, quarantined and unknown
// outcomes carry evidence. Pure policy plus a mutex-guarded converter: no
// database, network or clock.
package eventpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrInvalidObservation reports an event that cannot be evaluated: a
	// missing id, type, schema, version or tenant.
	ErrInvalidObservation = errors.New("eventpolicy: invalid event observation")

	// ErrInvalidPolicy reports a trigger policy that cannot govern: a
	// missing version, no tenants, no types, or a non-positive bound.
	ErrInvalidPolicy = errors.New("eventpolicy: invalid trigger policy")

	// ErrStormBound reports a redelivery past the policy storm bound.
	ErrStormBound = errors.New("eventpolicy: redelivery past the storm bound")

	// ErrConflictingRedelivery reports a redelivery whose content differs
	// from the first delivery of its event id.
	ErrConflictingRedelivery = errors.New("eventpolicy: conflicting event redelivery")

	// ErrSelfCausation reports an event caused by itself.
	ErrSelfCausation = errors.New("eventpolicy: event caused by itself")

	// ErrUnknownEvent reports an event no policy rule accepts: an unknown
	// type, schema, version or tenant.
	ErrUnknownEvent = errors.New("eventpolicy: event matches no rule")

	// ErrUnlistedProvider reports an external event from a provider the
	// rule does not allow.
	ErrUnlistedProvider = errors.New("eventpolicy: event provider is not allowed")

	// ErrScopeSelection reports a provider-requested scope outside the
	// rule's fixed scope: providers never select scope.
	ErrScopeSelection = errors.New("eventpolicy: provider selected out-of-policy scope")

	// ErrPayloadAsTruth reports a rule that requires resolved current
	// truth but was given no resolver: the payload is never truth.
	ErrPayloadAsTruth = errors.New("eventpolicy: current truth needs a resolver, not the payload")

	// ErrTruthUnresolved reports a resolver failure: the event waits for
	// eyes instead of inventing authority.
	ErrTruthUnresolved = errors.New("eventpolicy: current truth could not be resolved")
)

// EventSource is where an observation was seen.
type EventSource string

// Observation sources.
const (
	SourceInternal EventSource = "INTERNAL"
	SourceExternal EventSource = "EXTERNAL"
)

// Decision is one evidenced outcome.
type Decision string

// Outcome decisions.
const (
	DecisionAccepted    Decision = "ACCEPTED"
	DecisionIgnored     Decision = "IGNORED"
	DecisionQuarantined Decision = "QUARANTINED"
	DecisionUnknown     Decision = "UNKNOWN"
)

// Observation is one event as seen: identity, type, schema, tenant,
// source, digests and causation. The payload travels as a digest; it is
// never authoritative current truth.
type Observation struct {
	EventID         string
	EventType       string
	Schema          string
	SchemaVersion   string
	Tenant          string
	Source          EventSource
	Provider        string
	RequestedScope  []string
	PayloadDigest   string
	CausationID     string
	RedeliveryCount int
}

// TypeRule governs one event type.
type TypeRule struct {
	Schema          string
	AllowedVersions []string
	Sources         []EventSource
	Providers       []string
	FixedScope      []string
	Fanout          int
	RequireTruth    bool
	Ignore          bool
	IgnoreReason    string
}

// TriggerPolicy is one versioned event-to-intent policy.
type TriggerPolicy struct {
	PolicyVersion string
	Tenants       []string
	Types         map[string]TypeRule
	MaxRedelivery int
	MaxFanout     int
}

// TruthResolver resolves current truth for one observation where the
// rule requires it.
type TruthResolver func(Observation) (string, error)

// EmittedIntent is one bounded intent one accepted event names. Scope is
// always the rule's fixed scope: providers never select it.
type EmittedIntent struct {
	IntentID       string
	IdempotencyKey string
	Ordinal        int
	Scope          []string
	TruthDigest    string
	ObservedDigest string
}

// Outcome is one evidenced conversion outcome.
type Outcome struct {
	Decision  Decision
	Intents   []EmittedIntent
	Evidence  string
	Duplicate bool
}

type storedOutcome struct {
	observation Observation
	outcome     Outcome
}

// Converter applies trigger policies to observations. It is safe for
// concurrent use: concurrent redeliveries of one event converge on one
// outcome.
type Converter struct {
	mu     sync.Mutex
	issued map[string]storedOutcome
}

// NewConverter returns an empty converter.
func NewConverter() *Converter {
	return &Converter{issued: make(map[string]storedOutcome)}
}

func causalKey(policy TriggerPolicy, observation Observation, ordinal int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		policy.PolicyVersion, observation.Tenant, observation.EventType,
		observation.SchemaVersion, observation.EventID,
		fmt.Sprintf("ordinal=%d", ordinal),
	}, "\x00")))
	return "evt:" + hex.EncodeToString(sum[:])
}

func intentID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "evtintent:" + hex.EncodeToString(sum[:])[:32]
}

func tenantAllowed(policy TriggerPolicy, tenant string) bool {
	for _, allowed := range policy.Tenants {
		if tenant == allowed {
			return true
		}
	}
	return false
}

func versionAllowed(rule TypeRule, version string) bool {
	for _, allowed := range rule.AllowedVersions {
		if version == allowed {
			return true
		}
	}
	return false
}

func sourceAllowed(rule TypeRule, source EventSource) bool {
	for _, allowed := range rule.Sources {
		if source == allowed {
			return true
		}
	}
	return false
}

func providerAllowed(rule TypeRule, provider string) bool {
	for _, allowed := range rule.Providers {
		if provider == allowed {
			return true
		}
	}
	return false
}

func scopeWithin(fixed, requested []string) bool {
	allowed := make(map[string]bool, len(fixed))
	for _, scope := range fixed {
		allowed[scope] = true
	}
	for _, scope := range requested {
		if !allowed[scope] {
			return false
		}
	}
	return true
}

func evidence(decision Decision, format string, args ...any) string {
	return string(decision) + ": " + fmt.Sprintf(format, args...)
}

// Convert evaluates one observation against one policy. Malformed input
// is an error; every policy outcome — accepted, ignored, quarantined or
// unknown — is returned as evidence with no intents except acceptance.
func (c *Converter) Convert(policy TriggerPolicy, observation Observation, resolve TruthResolver) (Outcome, error) {
	if strings.TrimSpace(policy.PolicyVersion) == "" || len(policy.Tenants) == 0 ||
		len(policy.Types) == 0 || policy.MaxRedelivery < 0 || policy.MaxFanout <= 0 {
		return Outcome{}, fmt.Errorf("eventpolicy: Convert: %w", ErrInvalidPolicy)
	}
	if strings.TrimSpace(observation.EventID) == "" || strings.TrimSpace(observation.EventType) == "" ||
		strings.TrimSpace(observation.Schema) == "" || strings.TrimSpace(observation.SchemaVersion) == "" ||
		strings.TrimSpace(observation.Tenant) == "" {
		return Outcome{}, fmt.Errorf("eventpolicy: Convert: %w", ErrInvalidObservation)
	}
	memoKey := policy.PolicyVersion + "\x00" + observation.Tenant + "\x00" + observation.EventID
	if observation.CausationID != "" && observation.CausationID == observation.EventID {
		return c.memoizedQuarantine(memoKey, observation, ErrSelfCausation,
			"event %q is caused by itself", observation.EventID), nil
	}
	if observation.RedeliveryCount > policy.MaxRedelivery {
		return c.memoizedQuarantine(memoKey, observation, ErrStormBound,
			"redelivery %d past storm bound %d", observation.RedeliveryCount, policy.MaxRedelivery), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok := c.issued[memoKey]; ok {
		if !reflect.DeepEqual(prior.observation, observation) {
			return c.quarantineLocked(memoKey, observation, ErrConflictingRedelivery,
				"redelivery of %q conflicts with its first delivery", observation.EventID), nil
		}
		prior.outcome.Duplicate = true
		return prior.outcome, nil
	}
	rule, ok := policy.Types[observation.EventType]
	if !ok || !tenantAllowed(policy, observation.Tenant) ||
		rule.Schema != observation.Schema || !versionAllowed(rule, observation.SchemaVersion) ||
		!sourceAllowed(rule, observation.Source) {
		outcome := Outcome{Decision: DecisionUnknown,
			Evidence: evidence(DecisionUnknown, "event %q matches no rule in policy %s",
				observation.EventID, policy.PolicyVersion)}
		c.issued[memoKey] = storedOutcome{observation: observation, outcome: outcome}
		return outcome, nil
	}
	if rule.Ignore {
		outcome := Outcome{Decision: DecisionIgnored, Evidence: evidence(DecisionIgnored, "%s", rule.IgnoreReason)}
		c.issued[memoKey] = storedOutcome{observation: observation, outcome: outcome}
		return outcome, nil
	}
	if observation.Source == SourceExternal && !providerAllowed(rule, observation.Provider) {
		return c.quarantineLocked(memoKey, observation, ErrUnlistedProvider,
			"provider %q is not allowed for %q", observation.Provider, observation.EventType), nil
	}
	if len(observation.RequestedScope) != 0 && !scopeWithin(rule.FixedScope, observation.RequestedScope) {
		return c.quarantineLocked(memoKey, observation, ErrScopeSelection,
			"requested scope %v is outside fixed scope %v", observation.RequestedScope, rule.FixedScope), nil
	}
	var truth string
	if rule.RequireTruth {
		if resolve == nil {
			return c.quarantineLocked(memoKey, observation, ErrPayloadAsTruth,
				"rule for %q requires resolved truth, not the payload", observation.EventType), nil
		}
		digest, err := resolve(observation)
		if err != nil || strings.TrimSpace(digest) == "" {
			return c.quarantineLocked(memoKey, observation, ErrTruthUnresolved,
				"current truth for %q could not be resolved: %v", observation.EventID, err), nil
		}
		truth = digest
	}
	fanout := rule.Fanout
	if fanout <= 0 {
		fanout = 1
	}
	if fanout > policy.MaxFanout {
		fanout = policy.MaxFanout
	}
	scope := append([]string(nil), rule.FixedScope...)
	sort.Strings(scope)
	outcome := Outcome{Decision: DecisionAccepted,
		Evidence: evidence(DecisionAccepted, "event %q accepted under policy %s",
			observation.EventID, policy.PolicyVersion)}
	for ordinal := 0; ordinal < fanout; ordinal++ {
		key := causalKey(policy, observation, ordinal)
		outcome.Intents = append(outcome.Intents, EmittedIntent{
			IntentID:       intentID(key),
			IdempotencyKey: key,
			Ordinal:        ordinal,
			Scope:          scope,
			TruthDigest:    truth,
			ObservedDigest: observation.PayloadDigest,
		})
	}
	c.issued[memoKey] = storedOutcome{observation: observation, outcome: outcome}
	return outcome, nil
}

// memoizedQuarantine evidences one pre-lock policy violation without naming
// an intent. A redelivered identical observation replays its stored outcome.
func (c *Converter) memoizedQuarantine(memoKey string, observation Observation, cause error, format string, args ...any) Outcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quarantineLocked(memoKey, observation, cause, format, args...)
}

// quarantineLocked evidences one policy violation without naming an intent.
// The caller holds c.mu. A conflicting redelivery never overwrites the
// stored first delivery.
func (c *Converter) quarantineLocked(memoKey string, observation Observation, cause error, format string, args ...any) Outcome {
	outcome := Outcome{Decision: DecisionQuarantined,
		Evidence: evidence(DecisionQuarantined, "%s: %s", cause, fmt.Sprintf(format, args...))}
	if prior, ok := c.issued[memoKey]; ok {
		if reflect.DeepEqual(prior.observation, observation) {
			prior.outcome.Duplicate = true
			return prior.outcome
		}
		return outcome
	}
	c.issued[memoKey] = storedOutcome{observation: observation, outcome: outcome}
	return outcome
}
