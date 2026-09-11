package edge

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const edge009Version = 1

var (
	ErrInvalidStreamPolicy  = errors.New("edge: invalid stream policy")
	ErrInvalidStreamSession = errors.New("edge: invalid stream session")
)

// StreamPolicy bounds every resource and authority dimension of a long-lived
// edge session. The values are checked before a session is registered.
type StreamPolicy struct {
	ContractVersion      int
	MaxStreams           int
	MaxMessageBytes      int64
	MaxMessagesPerMinute int
	MaxReplayMessages    int
	MaxBufferedMessages  int
	MaxLifetime          time.Duration
	MaxIdle              time.Duration
	ReauthInterval       time.Duration
}

// StreamSession is the edge-visible identity and lifetime of one stream.
type StreamSession struct {
	ID        string
	TenantID  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// StreamRequest is the bounded input observed at open, message, or reconnect.
type StreamRequest struct {
	SessionID        string
	TenantID         string
	ContractVersion  int
	At               time.Time
	MessageBytes     int64
	BufferedMessages int
	ReplayMessages   int
	Reauthorized     bool
	Reconnect        bool
}

// StreamDecision is a payload-free admission result.
type StreamDecision struct {
	Allowed       bool
	Code          string
	Field         string
	State         string
	Version       int
	ActiveStreams int
	Sequence      uint64
	NextReauth    time.Time
}

// StreamRejection is a stable long-lived-session boundary error.
type StreamRejection struct {
	Code    string
	Field   string
	State   string
	Version int
}

func (r *StreamRejection) Error() string {
	if r == nil {
		return "edge: stream rejected"
	}
	return fmt.Sprintf("%s field=%s state=%s version=%d", r.Code, r.Field, r.State, r.Version)
}

type streamState struct {
	session      StreamSession
	open         bool
	revoked      bool
	lastActivity time.Time
	windowStart  time.Time
	messageCount int
	sequence     uint64
	nextReauth   time.Time
}

// StreamGate is a concurrency-safe in-memory edge gate. It owns admission
// state only; application data and stream payloads stay outside this package.
type StreamGate struct {
	mu       sync.Mutex
	policy   StreamPolicy
	active   int
	sessions map[string]*streamState
}

// NewStreamGate constructs a bounded streaming gate.
func NewStreamGate(policy StreamPolicy) (*StreamGate, error) {
	if err := validateStreamPolicy(policy); err != nil {
		return nil, err
	}
	return &StreamGate{policy: policy, sessions: make(map[string]*streamState)}, nil
}

// RegisterSession makes a session available for one stream. Registration is
// idempotent only for the exact same immutable session, avoiding identity drift.
func (g *StreamGate) RegisterSession(session StreamSession) error {
	if g == nil || strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.TenantID) == "" || session.IssuedAt.IsZero() || session.ExpiresAt.IsZero() || !session.ExpiresAt.After(session.IssuedAt) || session.ExpiresAt.Sub(session.IssuedAt) > g.policy.MaxLifetime {
		return ErrInvalidStreamSession
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if existing, ok := g.sessions[session.ID]; ok {
		if existing.session != session {
			return ErrInvalidStreamSession
		}
		return nil
	}
	g.sessions[session.ID] = &streamState{session: session, nextReauth: session.IssuedAt.Add(g.policy.ReauthInterval)}
	return nil
}

// Open admits one stream after checking lifetime, tenant, reauthorization,
// replay, and the global stream budget.
func (g *StreamGate) Open(req StreamRequest) (StreamDecision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, decision, err := g.authorizeLocked(req)
	if err != nil {
		return decision, err
	}
	if g.active >= g.policy.MaxStreams {
		return g.rejectLocked("active_streams", "over_limit")
	}
	if state.open {
		return g.rejectLocked("session", "already_open")
	}
	if req.MessageBytes < 0 || req.MessageBytes > g.policy.MaxMessageBytes {
		return g.rejectLocked("message_bytes", "oversized")
	}
	if req.BufferedMessages < 0 || req.BufferedMessages > g.policy.MaxBufferedMessages {
		return g.rejectLocked("buffered_messages", "backpressure")
	}
	if req.ReplayMessages < 0 || req.ReplayMessages > g.policy.MaxReplayMessages {
		return g.rejectLocked("replay_messages", "over_limit")
	}
	if !req.Reconnect && req.ReplayMessages != 0 {
		return g.rejectLocked("replay_messages", "unexpected")
	}
	if !req.At.Before(state.nextReauth) && !req.Reauthorized {
		return g.rejectLocked("reauthorization", "required")
	}
	if req.Reauthorized {
		state.nextReauth = req.At.Add(g.policy.ReauthInterval)
	}
	state.open, state.lastActivity, state.windowStart = true, req.At, req.At
	state.messageCount, state.sequence = 0, 0
	g.active++
	return g.allowedLocked(state), nil
}

// Accept admits one message or heartbeat on an open stream. It applies
// backpressure, message/rate/lifetime/idle limits, and periodic reauthorization.
func (g *StreamGate) Accept(req StreamRequest) (StreamDecision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, decision, err := g.authorizeLocked(req)
	if err != nil {
		return decision, err
	}
	if !state.open {
		return g.rejectLocked("session", "not_open")
	}
	if req.At.Before(state.lastActivity) {
		return g.rejectLocked("at", "clock_rollback")
	}
	if req.At.Sub(state.lastActivity) > g.policy.MaxIdle {
		return g.rejectLocked("idle_time", "expired")
	}
	if req.MessageBytes < 0 || req.MessageBytes > g.policy.MaxMessageBytes {
		return g.rejectLocked("message_bytes", "oversized")
	}
	if req.BufferedMessages < 0 || req.BufferedMessages > g.policy.MaxBufferedMessages {
		return g.rejectLocked("buffered_messages", "backpressure")
	}
	if req.At.Sub(state.windowStart) >= time.Minute {
		state.windowStart, state.messageCount = req.At, 0
	}
	if state.messageCount >= g.policy.MaxMessagesPerMinute {
		return g.rejectLocked("messages_per_minute", "over_limit")
	}
	if !req.At.Before(state.nextReauth) && !req.Reauthorized {
		return g.rejectLocked("reauthorization", "required")
	}
	if req.Reauthorized {
		state.nextReauth = req.At.Add(g.policy.ReauthInterval)
	}
	state.messageCount++
	state.sequence++
	state.lastActivity = req.At
	return g.allowedLocked(state), nil
}

// Revoke immediately closes a live stream and prevents all further opens or
// messages for the session.
func (g *StreamGate) Revoke(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.sessions[sessionID]
	if !ok {
		return ErrInvalidStreamSession
	}
	if state.open {
		state.open = false
		g.active--
	}
	state.revoked = true
	return nil
}

// Close releases the stream slot without changing the session's identity.
func (g *StreamGate) Close(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.sessions[sessionID]
	if !ok {
		return ErrInvalidStreamSession
	}
	if state.open {
		state.open = false
		g.active--
	}
	return nil
}

func (g *StreamGate) authorizeLocked(req StreamRequest) (*streamState, StreamDecision, error) {
	if req.ContractVersion != g.policy.ContractVersion {
		return nil, g.rejection("contract_version", "unsupported_version"), &StreamRejection{Code: "EDGE_009_REJECTED", Field: "contract_version", State: "unsupported_version", Version: g.policy.ContractVersion}
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.TenantID) == "" || req.At.IsZero() {
		return nil, g.rejection("session", "missing"), &StreamRejection{Code: "EDGE_009_REJECTED", Field: "session", State: "missing", Version: g.policy.ContractVersion}
	}
	state, ok := g.sessions[req.SessionID]
	if !ok {
		return nil, g.rejection("session", "unknown"), &StreamRejection{Code: "EDGE_009_REJECTED", Field: "session", State: "unknown", Version: g.policy.ContractVersion}
	}
	if state.session.TenantID != req.TenantID {
		return nil, g.rejection("tenant_id", "cross_tenant"), &StreamRejection{Code: "EDGE_009_REJECTED", Field: "tenant_id", State: "cross_tenant", Version: g.policy.ContractVersion}
	}
	if state.revoked {
		return nil, g.rejection("session", "revoked"), &StreamRejection{Code: "EDGE_009_REJECTED", Field: "session", State: "revoked", Version: g.policy.ContractVersion}
	}
	if req.At.Before(state.session.IssuedAt) || !req.At.Before(state.session.ExpiresAt) {
		return nil, g.rejection("session_lifetime", "expired"), &StreamRejection{Code: "EDGE_009_REJECTED", Field: "session_lifetime", State: "expired", Version: g.policy.ContractVersion}
	}
	return state, StreamDecision{}, nil
}

func (g *StreamGate) allowedLocked(state *streamState) StreamDecision {
	return StreamDecision{Allowed: true, Code: "EDGE_009_ACCEPTED", Version: g.policy.ContractVersion, ActiveStreams: g.active, Sequence: state.sequence, NextReauth: state.nextReauth}
}

func (g *StreamGate) rejection(field, state string) StreamDecision {
	return StreamDecision{Code: "EDGE_009_REJECTED", Field: field, State: state, Version: g.policy.ContractVersion, ActiveStreams: g.active}
}

func (g *StreamGate) rejectLocked(field, state string) (StreamDecision, error) {
	decision := g.rejection(field, state)
	return decision, &StreamRejection{Code: decision.Code, Field: field, State: state, Version: decision.Version}
}

func validateStreamPolicy(policy StreamPolicy) error {
	if policy.ContractVersion != edge009Version || policy.MaxStreams <= 0 || policy.MaxMessageBytes <= 0 || policy.MaxMessagesPerMinute <= 0 || policy.MaxReplayMessages < 0 || policy.MaxBufferedMessages < 0 || policy.MaxLifetime <= 0 || policy.MaxIdle <= 0 || policy.ReauthInterval <= 0 || policy.ReauthInterval >= policy.MaxLifetime {
		return ErrInvalidStreamPolicy
	}
	return nil
}

// ExplainStreaming describes the EDGE-009 session contract without payloads.
func ExplainStreaming() string {
	return "EDGE-009 v1: bounded streams, messages, replay, idle lifetime, backpressure, revocation, and reauthorization"
}
