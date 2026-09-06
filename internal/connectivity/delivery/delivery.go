// Package delivery is the provider-neutral messaging delivery boundary.
// Semantic intents enter as references and digests; provider adapters are the
// only implementation of Transport and never receive workflow raw payloads.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Channel string

const (
	ChannelEmail Channel = "EMAIL"
	ChannelSMS   Channel = "SMS"
	ChannelPush  Channel = "PUSH"
	ChannelInbox Channel = "INBOX"
)

type State string

const (
	StateSubmitted       State = "SUBMITTED"
	StateFailed          State = "FAILED"
	StateReviewRequired  State = "REVIEW_REQUIRED"
	StateAlreadyObserved State = "ALREADY_OBSERVED"
)

var (
	ErrInvalidEnvelope = errors.New("delivery: invalid semantic envelope")
	ErrNoStore         = errors.New("delivery: attempt store is required")
	ErrNoTransport     = errors.New("delivery: transport is required")
)

// Envelope contains only semantic references and a content digest. It has no
// provider address, credential, rendered body or arbitrary payload field.
type Envelope struct {
	TenantID       string
	IntentID       string
	RecipientRef   string
	EndpointRef    string
	ContentRef     string
	TemplateRef    string
	ParametersRef  string
	Purpose        string
	Classification string
	Channel        Channel
	IdempotencyKey string
	CorrelationID  string
	ContentDigest  string
	AvailableAt    time.Time
	ExpiresAt      time.Time
}

func (e Envelope) Validate(now time.Time) error {
	for name, value := range map[string]string{
		"tenant_id": e.TenantID, "intent_id": e.IntentID, "recipient_ref": e.RecipientRef,
		"endpoint_ref": e.EndpointRef, "purpose": e.Purpose, "classification": e.Classification,
		"idempotency_key": e.IdempotencyKey, "correlation_id": e.CorrelationID, "content_digest": e.ContentDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidEnvelope, name)
		}
	}
	if e.ContentRef == "" && e.TemplateRef == "" {
		return fmt.Errorf("%w: content_ref or template_ref is required", ErrInvalidEnvelope)
	}
	switch e.Channel {
	case ChannelEmail, ChannelSMS, ChannelPush, ChannelInbox:
	default:
		return fmt.Errorf("%w: unsupported channel %q", ErrInvalidEnvelope, e.Channel)
	}
	if e.ExpiresAt.IsZero() || !e.ExpiresAt.After(now) {
		return fmt.Errorf("%w: envelope is expired or has no expiry", ErrInvalidEnvelope)
	}
	return nil
}

type ProviderResult struct{ ProviderRef, ObservationRef, Code string }

type ProviderError struct {
	Err                  error
	Retryable, Ambiguous bool
}

func (e *ProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "delivery: provider error"
	}
	return e.Err.Error()
}
func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Transport interface {
	Deliver(context.Context, Envelope) (ProviderResult, error)
}

type Claim struct {
	AttemptID       string
	Attempt         int
	AlreadyObserved bool
}

type Observation struct {
	AttemptID      string
	Attempt        int
	State          State
	ProviderRef    string
	ObservationRef string
	ProviderCode   string
	Reason         string
	RecordedAt     time.Time
}

type AttemptStore interface {
	Claim(context.Context, Envelope, int) (Claim, error)
	Observe(context.Context, Envelope, Claim, Observation) error
}

type Runner struct {
	Store       AttemptStore
	Transport   Transport
	Clock       func() time.Time
	MaxAttempts int
}

func (r Runner) Deliver(ctx context.Context, envelope Envelope) (Observation, error) {
	if r.Store == nil {
		return Observation{}, ErrNoStore
	}
	if r.Transport == nil {
		return Observation{}, ErrNoTransport
	}
	now := time.Now().UTC()
	if r.Clock != nil {
		now = r.Clock().UTC()
	}
	if err := envelope.Validate(now); err != nil {
		return Observation{}, err
	}
	max := r.MaxAttempts
	if max <= 0 {
		max = 3
	}
	for attempt := 1; attempt <= max; attempt++ {
		claim, err := r.Store.Claim(ctx, envelope, attempt)
		if err != nil {
			return Observation{}, fmt.Errorf("delivery: claim attempt %d: %w", attempt, err)
		}
		if claim.AlreadyObserved {
			obs := Observation{AttemptID: claim.AttemptID, Attempt: claim.Attempt, State: StateAlreadyObserved, RecordedAt: now}
			return obs, nil
		}
		result, sendErr := r.Transport.Deliver(ctx, envelope)
		obs := Observation{AttemptID: claim.AttemptID, Attempt: claim.Attempt, RecordedAt: now, ProviderRef: result.ProviderRef, ObservationRef: result.ObservationRef, ProviderCode: result.Code}
		if sendErr == nil {
			obs.State = StateSubmitted
			if err := r.Store.Observe(ctx, envelope, claim, obs); err != nil {
				return Observation{}, fmt.Errorf("delivery: record submitted observation: %w", err)
			}
			return obs, nil
		}
		var providerErr *ProviderError
		if errors.As(sendErr, &providerErr) && providerErr.Ambiguous {
			obs.State, obs.Reason = StateReviewRequired, sendErr.Error()
			if err := r.Store.Observe(ctx, envelope, claim, obs); err != nil {
				return Observation{}, fmt.Errorf("delivery: record ambiguous observation: %w", err)
			}
			return obs, sendErr
		}
		obs.State, obs.Reason = StateFailed, sendErr.Error()
		if err := r.Store.Observe(ctx, envelope, claim, obs); err != nil {
			return Observation{}, fmt.Errorf("delivery: record failed observation: %w", err)
		}
		if !isRetryable(sendErr) || attempt == max {
			return obs, sendErr
		}
	}
	return Observation{}, errors.New("delivery: exhausted attempts")
}

func isRetryable(err error) bool {
	var providerErr *ProviderError
	return errors.As(err, &providerErr) && providerErr.Retryable && !providerErr.Ambiguous
}
