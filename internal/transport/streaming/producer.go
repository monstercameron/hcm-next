package streaming

import (
	"errors"
	"fmt"
	"time"
)

// Producer drives the send side of one resumable stream: it mints
// sequential chunks, each carrying a freshly signed cursor naming its own
// sequence, and it validates a client-presented cursor on reconnect so the
// sequence continues rather than restarts.
//
// A Producer is not safe for concurrent use; a stream has exactly one
// goroutine deciding what its next chunk is, which is also true of every
// streaming handler in this repository (see internal/transport/journey's
// WatchJourney, which runs entirely on the RPC's own goroutine).
type Producer[T any] struct {
	signer   Signer
	tenant   string
	streamID string
	ttl      time.Duration
	now      func() time.Time
	seq      uint64
}

// NewProducer returns a Producer for one (tenant, streamID) stream. ttl
// bounds how long a cursor this Producer mints stays presentable; now
// supplies the current time and defaults to [time.Now] when nil.
func NewProducer[T any](signer Signer, tenant, streamID string, ttl time.Duration, now func() time.Time) *Producer[T] {
	if now == nil {
		now = time.Now
	}
	return &Producer[T]{signer: signer, tenant: tenant, streamID: streamID, ttl: ttl, now: now}
}

// Resume validates token against this Producer's own tenant and stream id
// and, on success, positions the Producer so the next chunk [Next] mints
// continues the sequence token named rather than restarting it at one.
//
// An empty token means "nothing to resume from" and leaves the sequence at
// its construction-time position (normally zero, so the very first [Next]
// call mints sequence 1). A token that is forged, expired, or names a
// different tenant or stream is refused with the same sentinel error
// [Signer.Decode] returns ([ErrCursorForged], [ErrCursorExpired],
// [ErrCursorForeign], [ErrCursorMalformed]); the Producer's own sequence is
// left untouched by a refused Resume, so a caller cannot use a bad cursor to
// silently rewind or fast-forward a stream that never accepted it.
func (p *Producer[T]) Resume(token string) error {
	if token == "" {
		return nil
	}
	c, err := p.signer.Decode(token, p.now(), p.tenant, p.streamID)
	if err != nil {
		return err
	}
	p.seq = c.Sequence
	return nil
}

// Next mints the chunk for the sequence immediately after whatever this
// Producer has already emitted - from construction, or from the last
// successful [Resume] - carrying value, terminal, and a cursor signed for
// that new sequence.
func (p *Producer[T]) Next(value T, terminal bool) (Chunk[T], error) {
	p.seq++
	cursor, err := p.signer.Encode(Cursor{
		Tenant:    p.tenant,
		StreamID:  p.streamID,
		Sequence:  p.seq,
		ExpiresAt: p.now().Add(p.ttl),
	})
	if err != nil {
		p.seq--
		return Chunk[T]{}, err
	}
	return Chunk[T]{Sequence: p.seq, Cursor: cursor, Terminal: terminal, Value: value}, nil
}

// Sequence reports the sequence number of the last chunk [Next] minted (or
// that a successful [Resume] positioned the Producer at), so a caller that
// only wants the number - not a fresh cursor - does not have to mint one to
// learn it.
func (p *Producer[T]) Sequence() uint64 { return p.seq }

// ErrRevoked is the sentinel [Terminate] wraps around whatever an
// [AuthorizationCheck] reported, so a stream ending because its caller's
// authorization was revoked mid-flight is distinguishable, by
// errors.Is(err, ErrRevoked), from the stream ending because the resource
// underneath it failed or refused for an ordinary reason.
var ErrRevoked = errors.New("streaming: authorization for this stream was revoked")

// AuthorizationCheck reports whether a stream's caller is still authorized
// to keep receiving it. It returns nil while authorization holds and a
// non-nil error the moment it does not; a stream implementation calls it on
// whatever cadence it already re-checks the resource (e.g. once per poll)
// rather than this package prescribing a schedule of its own.
type AuthorizationCheck func() error

// Terminate reports whether cause - the result of an [AuthorizationCheck] -
// should end an open stream, and if so, returns the typed error a transport
// adapter projects onto a revocation status (in this repository,
// envelope.CodePermissionDenied via envelope.New, exactly as an ordinary
// mid-stream authorization refusal already is; see
// internal/transport/journey's ownedError). Terminate(nil) reports "keep
// going" by returning nil.
func Terminate(cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrRevoked, cause)
}
