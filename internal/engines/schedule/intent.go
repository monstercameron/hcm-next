package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync"
)

var (
	// ErrInvalidConversion reports a conversion request that cannot name an
	// occurrence: an empty occurrence key, an empty leader, an empty target
	// scope, or an unknown misfire disposition.
	ErrInvalidConversion = errors.New("schedule: invalid occurrence conversion")

	// ErrTriggerMismatch reports an occurrence driven against a different
	// trigger revision than the one that calculated it.
	ErrTriggerMismatch = errors.New("schedule: occurrence does not belong to this trigger")

	// ErrConversionConflict reports a redelivery that reuses an occurrence
	// key for a different target scope.
	ErrConversionConflict = errors.New("schedule: conflicting occurrence redelivery")
)

// ReceiptDisposition is one typed non-creation outcome.
type ReceiptDisposition string

// Typed conversion receipts.
const (
	ReceiptSkipped        ReceiptDisposition = "SKIPPED"
	ReceiptDeferred       ReceiptDisposition = "DEFERRED"
	ReceiptReviewRequired ReceiptDisposition = "REVIEW_REQUIRED"
)

// LeaderClaim fences one conversion attempt.
type LeaderClaim struct {
	ID    string
	Epoch uint64
}

// ConversionRequest drives one frozen occurrence into at most one intent.
// The trigger must be the publication that calculated the occurrence;
// AllowCatchUp gates CATCH_UP occurrences.
type ConversionRequest struct {
	Trigger      PublishedTrigger
	Occurrence   Occurrence
	TargetScope  []string
	Leader       LeaderClaim
	AllowCatchUp bool
}

// ScheduledIntent is the exactly-one intent one occurrence creates. The
// scheduler dispatches the trigger's own target, purpose and frozen time
// context; it carries no Promotion, Payroll or Leave semantics.
type ScheduledIntent struct {
	IntentID       string
	IdempotencyKey string
	Target         IntentRef
	TriggerDigest  string
	OccurrenceKey  string
	Purpose        string
	Scope          []string
	ScheduledAt    string
	NominalDate    string
	Source         SourceKind
}

// Receipt explains why an occurrence created no intent.
type Receipt struct {
	Disposition   ReceiptDisposition
	Reason        string
	OccurrenceKey string
	TriggerDigest string
}

// Conversion is one occurrence outcome: exactly one of Intent or Receipt
// is set. Duplicate marks a replayed or overlapped delivery of an already
// converted occurrence.
type Conversion struct {
	Intent    *ScheduledIntent
	Receipt   *Receipt
	Duplicate bool
}

type storedConversion struct {
	request    ConversionRequest
	conversion Conversion
}

// Converter turns frozen occurrences into idempotent intents. It is safe
// for concurrent use: overlapping leaders converting one occurrence name
// one intent.
type Converter struct {
	mu     sync.Mutex
	issued map[string]storedConversion
}

// NewConverter returns an empty converter.
func NewConverter() *Converter {
	return &Converter{issued: make(map[string]storedConversion)}
}

func intentID(triggerDigest, occurrenceKey string) string {
	sum := sha256.Sum256([]byte(triggerDigest + "\x00" + occurrenceKey))
	return "schedintent:" + hex.EncodeToString(sum[:])[:32]
}

// withoutLeader strips fencing from conflict comparison: leaders overlap,
// scopes must agree.
func withoutLeader(req ConversionRequest) ConversionRequest {
	req.Leader = LeaderClaim{}
	return req
}

// Convert drives one occurrence into exactly one intent or one typed
// receipt. Malformed input is refused; schedule revisions and policy
// dispositions become receipts, never silent drops or duplicate intents.
func (c *Converter) Convert(req ConversionRequest) (Conversion, error) {
	if strings.TrimSpace(req.Occurrence.Key) == "" {
		return Conversion{}, refuse("EMPTY_OCCURRENCE_KEY", "occurrence.key", ErrInvalidConversion, "occurrence key is required")
	}
	if strings.TrimSpace(req.Leader.ID) == "" {
		return Conversion{}, refuse("EMPTY_LEADER", "leader.id", ErrInvalidConversion, "leader claim is required")
	}
	if len(req.TargetScope) == 0 {
		return Conversion{}, refuse("EMPTY_TARGET_SCOPE", "target_scope", ErrInvalidConversion, "target scope is required")
	}
	if err := req.Trigger.Verify(); err != nil {
		return Conversion{}, refuse("TAMPERED_TRIGGER", "trigger", ErrInvalidConversion, "published trigger no longer matches its digest: %v", err)
	}
	want := req.Trigger.Ref()
	if req.Occurrence.Trigger.TenantID != want.TenantID || req.Occurrence.Trigger.ID != want.ID {
		return Conversion{}, refuse("TRIGGER_MISMATCH", "occurrence.trigger", ErrTriggerMismatch,
			"occurrence belongs to %s, not %s", req.Occurrence.Trigger, want)
	}
	memoKey := req.Trigger.Digest + "\x00" + req.Occurrence.Key
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok := c.issued[memoKey]; ok {
		if !reflect.DeepEqual(withoutLeader(prior.request), withoutLeader(req)) {
			return Conversion{}, refuse("CONVERSION_CONFLICT", "target_scope", ErrConversionConflict,
				"occurrence %q already converted for a different scope", req.Occurrence.Key)
		}
		prior.conversion.Duplicate = true
		return prior.conversion, nil
	}
	disposition, reason, create := convertDisposition(req)
	var conversion Conversion
	if create {
		intent := &ScheduledIntent{
			IntentID:       intentID(req.Trigger.Digest, req.Occurrence.Key),
			IdempotencyKey: req.Occurrence.Key,
			Target:         req.Trigger.Definition.Target,
			TriggerDigest:  req.Trigger.Digest,
			OccurrenceKey:  req.Occurrence.Key,
			Purpose:        req.Trigger.Definition.Purpose,
			Scope:          append([]string(nil), req.TargetScope...),
			ScheduledAt:    req.Occurrence.ScheduledAt.String(),
			NominalDate:    req.Occurrence.NominalDate.String(),
			Source:         req.Occurrence.Source,
		}
		conversion = Conversion{Intent: intent}
	} else {
		conversion = Conversion{Receipt: &Receipt{
			Disposition:   disposition,
			Reason:        reason,
			OccurrenceKey: req.Occurrence.Key,
			TriggerDigest: req.Trigger.Digest,
		}}
	}
	c.issued[memoKey] = storedConversion{request: req, conversion: conversion}
	return conversion, nil
}

// convertDisposition maps one frozen misfire decision to creation or a
// typed receipt. Schedule revisions surface as review, never as silent
// adoption of the new revision.
func convertDisposition(req ConversionRequest) (ReceiptDisposition, string, bool) {
	if req.Occurrence.Trigger.Version != req.Trigger.Ref().Version {
		return ReceiptReviewRequired, "schedule revised since the occurrence was calculated", false
	}
	switch req.Occurrence.Misfire {
	case MisfireOnTime, MisfireFire:
		return "", "", true
	case MisfireCatch:
		if req.AllowCatchUp {
			return "", "", true
		}
		return ReceiptDeferred, "catch-up occurrence deferred by policy", false
	case MisfireSkipped:
		return ReceiptSkipped, "occurrence skipped by the misfire policy", false
	case MisfireNeedsReview:
		return ReceiptReviewRequired, "occurrence needs review before it may name an intent", false
	default:
		return ReceiptReviewRequired, "unknown misfire disposition cannot name an intent", false
	}
}
