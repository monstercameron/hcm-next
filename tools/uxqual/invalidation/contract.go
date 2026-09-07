package invalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport/productquery"
)

const (
	// DefaultMaxMessageBytes is the product-query wire contract's ceiling. A
	// caller may choose a smaller bound, but cannot raise it above the contract.
	DefaultMaxMessageBytes = productquery.MaxInvalidationBytes
	DefaultMaxQueue        = 16
	MaxQueue               = 64
	MaxSubjects            = productquery.MaxCandidates
	maxObserverQueue       = MaxQueue*4 + 16
)

var (
	ErrInvalidMessage     = errors.New("invalidation: malformed message")
	ErrForeignMessage     = errors.New("invalidation: message outside active scope")
	ErrStaleMessage       = errors.New("invalidation: stale message")
	ErrQueueFull          = errors.New("invalidation: refresh queue is full")
	ErrAlreadyRunning     = errors.New("invalidation: client is already running")
	ErrNoStream           = errors.New("invalidation: stream is nil")
	ErrStreamNotCloseable = errors.New("invalidation: stream cannot be interrupted")
	ErrStreamReceive      = errors.New("invalidation: stream receive failed")
	ErrRefetchFailed      = errors.New("invalidation: authoritative refetch failed")
)

// Stream is the smallest transport seam needed by the client. A browser
// WebSocket adapter returns one complete productquery JSON message per Recv.
type Stream interface {
	Recv() ([]byte, error)
}

// CloseStream is the required subscription transport. Close must make a
// blocked Recv return. Requiring this capability prevents navigation or
// parent-context cancellation from stranding a reader goroutine.
type CloseStream interface {
	Stream
	Close() error
}

// Refresh is the only input delivered to an authoritative refetch. It carries
// bounded references and versions, never the invalidation's raw bytes or
// protected fields. The refetch implementation must authorize its RPC again.
type Refresh struct {
	Projection     string
	SourceSequence uint64
	Watermark      uint64
	Subjects       []values.EntityRef
}

// Refetch performs an authorized projection read. It must not treat Refresh
// as authority: the server's RPC authorization remains authoritative. It must
// honor ctx before applying a returned view. On cancellation the subscription
// stops waiting, but Go cannot revoke a callback that ignores ctx; such a
// callback can leak its own goroutine or apply stale state and violates this
// contract.
type Refetch func(context.Context, Refresh) error

// Scope is the active authorized browser projection. Subject membership is a
// deny-by-default allow-list supplied by the latest authorized RPC result.
// SubjectRevisions are local freshness hints only and never business truth.
type Scope struct {
	Tenant           values.TenantId
	Projection       string
	Watermark        uint64
	SourceSequence   uint64
	Subjects         []values.EntityRef
	SubjectRevisions map[string]uint64
}

// Validate checks the scope boundary before a stream can start.
func (s Scope) Validate() error {
	if err := s.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: invalid tenant", ErrForeignMessage)
	}
	if !safeToken(s.Projection, 128) {
		return fmt.Errorf("%w: invalid projection", ErrForeignMessage)
	}
	if s.Watermark > s.SourceSequence {
		return fmt.Errorf("%w: invalid cursor", ErrForeignMessage)
	}
	if len(s.Subjects) > MaxSubjects {
		return fmt.Errorf("%w: subject bound exceeded", ErrForeignMessage)
	}
	seen := make(map[string]struct{}, len(s.Subjects))
	for _, subject := range s.Subjects {
		if err := subject.Validate(); err != nil || subject.Tenant != s.Tenant {
			return fmt.Errorf("%w: invalid subject", ErrForeignMessage)
		}
		key := subject.String()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate subject", ErrForeignMessage)
		}
		seen[key] = struct{}{}
	}
	for key, revision := range s.SubjectRevisions {
		if _, ok := seen[key]; !ok || revision == 0 {
			return fmt.Errorf("%w: invalid subject revision", ErrForeignMessage)
		}
	}
	return nil
}

// Options controls only bounded client resources and observation. It cannot
// widen product-query's message or queue bounds.
type Options struct {
	MaxMessageBytes int
	MaxQueue        int
	Observe         func(Event)
}

func normalizeOptions(options Options) Options {
	if options.MaxMessageBytes <= 0 || options.MaxMessageBytes > DefaultMaxMessageBytes {
		options.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if options.MaxQueue <= 0 || options.MaxQueue > MaxQueue {
		options.MaxQueue = DefaultMaxQueue
	}
	return options
}

func normalizeScope(scope Scope) (Scope, error) {
	if err := scope.Validate(); err != nil {
		return Scope{}, err
	}
	revisions := make(map[string]uint64, len(scope.SubjectRevisions))
	for key, revision := range scope.SubjectRevisions {
		if key == "" || revision == 0 {
			return Scope{}, fmt.Errorf("%w: invalid subject revision", ErrForeignMessage)
		}
		revisions[key] = revision
	}
	subjects := append([]values.EntityRef(nil), scope.Subjects...)
	sort.Slice(subjects, func(i, j int) bool { return subjects[i].String() < subjects[j].String() })
	scope.Subjects = subjects
	scope.SubjectRevisions = revisions
	return scope, nil
}

// Decode validates one complete productquery invalidation before it reaches
// scope filtering. All failures intentionally collapse to one identifier-free
// error so malformed and unknown input cannot become a membership oracle.
func Decode(raw []byte, maxBytes int) (productquery.InvalidationMessage, error) {
	if maxBytes <= 0 || maxBytes > DefaultMaxMessageBytes {
		maxBytes = DefaultMaxMessageBytes
	}
	if len(raw) == 0 || len(raw) > maxBytes {
		return productquery.InvalidationMessage{}, ErrInvalidMessage
	}
	var message productquery.InvalidationMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return productquery.InvalidationMessage{}, ErrInvalidMessage
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return productquery.InvalidationMessage{}, ErrInvalidMessage
	}
	if err := message.Validate(); err != nil {
		return productquery.InvalidationMessage{}, ErrInvalidMessage
	}
	return message, nil
}

// Encode validates and returns the canonical, payload-free wire form.
func Encode(message productquery.InvalidationMessage) ([]byte, error) {
	if err := message.Validate(); err != nil {
		return nil, ErrInvalidMessage
	}
	b, err := message.CanonicalBytes()
	if err != nil || len(b) > DefaultMaxMessageBytes {
		return nil, ErrInvalidMessage
	}
	return b, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func safeToken(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}
