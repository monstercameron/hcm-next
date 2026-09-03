// Package channelparity provides the channel-independent semantic boundary for
// user-flow adapters. Adapters may change presentation, but must submit this
// same request to the semantic capability and carry its receipt across handoff.
package channelparity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Channel string

const (
	Desktop       Channel = "desktop"
	Mobile        Channel = "mobile"
	Kiosk         Channel = "kiosk"
	SecureMessage Channel = "secure-message"
	Assisted      Channel = "assisted"
)

// Verbose aliases make channel names unambiguous at call sites.
const (
	ChannelDesktop       = Desktop
	ChannelMobile        = Mobile
	ChannelKiosk         = Kiosk
	ChannelSecureMessage = SecureMessage
	ChannelAssisted      = Assisted
)

var Channels = []Channel{Desktop, Mobile, Kiosk, SecureMessage, Assisted}

type Stage string

const (
	Validate Stage = "VALIDATE"
	Simulate Stage = "SIMULATE"
	StepUp   Stage = "STEP_UP"
	Confirm  Stage = "CONFIRM"
	Submit   Stage = "SUBMIT"
)

// Request is the semantic input produced by every presentation adapter.
type Request struct {
	IntentID        string            `json:"intent_id"`
	IntentVersion   string            `json:"intent_version"`
	Subject         string            `json:"subject"`
	Actor           string            `json:"actor"`
	Purpose         string            `json:"purpose"`
	Assurance       string            `json:"assurance"`
	Payload         map[string]string `json:"payload"`
	Simulation      bool              `json:"simulation"`
	StepUp          bool              `json:"step_up"`
	Confirmation    bool              `json:"confirmation"`
	IdempotencyKey  string            `json:"idempotency_key"`
	WorkflowVersion string            `json:"workflow_version"`
	MessageVersion  string            `json:"message_version,omitempty"`
	Deadline        string            `json:"deadline,omitempty"`
	Evidence        []string          `json:"evidence,omitempty"`
}

// Normalized is the canonical request consumed by the semantic capability.
type Normalized struct {
	IntentID, IntentVersion, Subject, Actor, Purpose, Assurance string
	Payload                                                     map[string]string
	RequestDigest                                               string
}

type NormalizedRequest = Normalized

type Outcome struct {
	IntentDigest string
	Result       string
	ErrorCode    string
	Stages       []Stage
	Evidence     []string
	Deadline     string
}

type Receipt struct {
	Channel Channel
	Request Normalized
	Outcome Outcome
	Handoff *HandoffReceipt
}

type HandoffReceipt struct {
	From, To     Channel
	IntentDigest string
	Outcome      Outcome
	Deadline     string
	Evidence     []string
}

// Executor is the small stateful boundary used by reconnecting/offline
// adapters. It makes the idempotency key an effect boundary, never a channel
// concern.
type Executor struct {
	mu      sync.Mutex
	applied map[string]Receipt
}

func NewExecutor() *Executor { return &Executor{applied: make(map[string]Receipt)} }

func (e *Executor) Apply(r Request, ch Channel) (Receipt, error) {
	n, err := Normalize(r)
	if err != nil {
		return Receipt{}, err
	}
	key := n.Subject + "\x00" + r.IdempotencyKey
	e.mu.Lock()
	defer e.mu.Unlock()
	if prior, ok := e.applied[key]; ok {
		if prior.Request.RequestDigest != n.RequestDigest {
			return Receipt{}, ErrDuplicate
		}
		return prior, nil
	}
	out, err := Execute(r, ch)
	if err != nil {
		return Receipt{}, err
	}
	e.applied[key] = out
	return out, nil
}

var (
	ErrInvalidRequest = errors.New("invalid channel parity request")
	ErrStaleMessage   = errors.New("secure message is stale")
	ErrDuplicate      = errors.New("idempotency key already applied")
)

func Normalize(r Request) (Normalized, error) {
	if strings.TrimSpace(r.IntentID) == "" || strings.TrimSpace(r.IntentVersion) == "" || strings.TrimSpace(r.Subject) == "" || strings.TrimSpace(r.Actor) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return Normalized{}, ErrInvalidRequest
	}
	p := map[string]string{}
	for k, v := range r.Payload {
		p[k] = v
	}
	n := Normalized{IntentID: r.IntentID, IntentVersion: r.IntentVersion, Subject: r.Subject, Actor: r.Actor, Purpose: r.Purpose, Assurance: r.Assurance, Payload: p}
	b, _ := json.Marshal(struct {
		I, V, S, A, P, R string
		D                map[string]string
	}{n.IntentID, n.IntentVersion, n.Subject, n.Actor, n.Purpose, n.Assurance, n.Payload})
	h := sha256.Sum256(b)
	n.RequestDigest = hex.EncodeToString(h[:])
	return n, nil
}

func Canonicalize(r Request) (Normalized, error) { return Normalize(r) }

func Execute(r Request, ch Channel) (Receipt, error) {
	if !supported(ch) {
		return Receipt{}, fmt.Errorf("%w: unsupported channel %q", ErrInvalidRequest, ch)
	}
	if ch == SecureMessage && r.MessageVersion != "" && r.MessageVersion != r.WorkflowVersion {
		return Receipt{}, ErrStaleMessage
	}
	n, err := Normalize(r)
	if err != nil {
		return Receipt{}, err
	}
	if !r.Simulation || !r.StepUp || !r.Confirmation {
		return Receipt{}, fmt.Errorf("%w: simulation, step-up and confirmation are required", ErrInvalidRequest)
	}
	stages := []Stage{Validate, Simulate, StepUp, Confirm, Submit}
	o := Outcome{IntentDigest: n.RequestDigest, Result: "accepted", Stages: stages, Deadline: r.Deadline, Evidence: append([]string(nil), r.Evidence...)}
	return Receipt{Channel: ch, Request: n, Outcome: o}, nil
}

func Handoff(receipt Receipt, to Channel) (Receipt, error) {
	if !supported(to) {
		return Receipt{}, fmt.Errorf("%w: unsupported handoff target", ErrInvalidRequest)
	}
	if receipt.Request.RequestDigest == "" || receipt.Outcome.IntentDigest != receipt.Request.RequestDigest {
		return Receipt{}, ErrInvalidRequest
	}
	e := append([]string(nil), receipt.Outcome.Evidence...)
	h := &HandoffReceipt{From: receipt.Channel, To: to, IntentDigest: receipt.Request.RequestDigest, Outcome: receipt.Outcome, Deadline: receipt.Outcome.Deadline, Evidence: e}
	receipt.Channel, receipt.Handoff = to, h
	return receipt, nil
}

func EqualSemantics(a, b Receipt) bool {
	return a.Request.IntentID == b.Request.IntentID && a.Request.IntentVersion == b.Request.IntentVersion && a.Request.Subject == b.Request.Subject && a.Request.Actor == b.Request.Actor && a.Request.Purpose == b.Request.Purpose && a.Request.Assurance == b.Request.Assurance && mapsEqual(a.Request.Payload, b.Request.Payload) && a.Request.RequestDigest == b.Request.RequestDigest && a.Outcome.IntentDigest == b.Outcome.IntentDigest && a.Outcome.Result == b.Outcome.Result && a.Outcome.ErrorCode == b.Outcome.ErrorCode && stagesEqual(a.Outcome.Stages, b.Outcome.Stages) && equalStrings(a.Outcome.Evidence, b.Outcome.Evidence) && a.Outcome.Deadline == b.Outcome.Deadline
}

func supported(c Channel) bool {
	for _, x := range Channels {
		if c == x {
			return true
		}
	}
	return false
}
func stagesEqual(a, b []Stage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
