// Package content owns the trust boundary for bytes received from outside a
// trusted, compiled artifact boundary. Raw bytes are retained as restricted
// evidence; only an immutable, classified safe derivative can be promoted.
package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Received     State = "RECEIVED"
	Quarantined  State = "QUARANTINED"
	Validating   State = "VALIDATING"
	Scanning     State = "SCANNING"
	Transforming State = "TRANSFORMING"
	Safe         State = "SAFE"
	Rejected     State = "REJECTED"
)

type Verdict string

const (
	VerdictSafe        Verdict = "SAFE"
	VerdictUnsafe      Verdict = "UNSAFE"
	VerdictUnscannable Verdict = "UNSCANNABLE"
)

var (
	ErrInvalid       = errors.New("content: invalid ingress")
	ErrWrongState    = errors.New("content: illegal lifecycle transition")
	ErrNotPromotable = errors.New("content: derivative is not promotable")
)

// Limits are enforced before parsing or scanning. Zero means no limit except
// MaxBytes, which must be non-zero for a production policy.
type Limits struct {
	MaxBytes, MaxExpandedBytes, MaxFiles uint64
	MaxArchiveDepth                      uint32
	MaxCompressionRatio                  uint64
}

// Policy is server-owned validation policy. Callers cannot supply an allowlist
// through an ingress request.
type Policy struct {
	Limits           Limits
	Extensions, MIME map[string]struct{}
}

type Ingress struct {
	ID, Tenant, Source, Filename, DeclaredMIME string
	ReceivedAt                                 time.Time
	Digest                                     string
	State                                      State
	Generation                                 uint64
	ArchiveDepth                               uint32
	ExpandedBytes, FileCount, CompressionRatio uint64
}
type Transition struct {
	From, To   State
	At         time.Time
	Reason     string
	Generation uint64
}
type Inspection struct {
	Verdict                Verdict
	ScannerVersion, Reason string
	At                     time.Time
	Digest                 string
}
type SafeDerivative struct {
	ID, Digest, MIME, Classification, SourceDigest, ScannerVersion string
	CreatedAt                                                      time.Time
	Revoked                                                        bool
	payload                                                        []byte
}

type Artifact struct {
	Ingress
	payload     []byte
	transitions []Transition
	inspections []Inspection
	derivatives []*SafeDerivative
	mu          sync.RWMutex
}

// New creates an ingress record and immediately places bytes in quarantine.
// The caller receives no mutable reference to the retained bytes.
func New(in Ingress, payload []byte, now time.Time) (*Artifact, error) {
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.Source) == "" || len(payload) == 0 {
		return nil, ErrInvalid
	}
	if in.ReceivedAt.IsZero() {
		in.ReceivedAt = now.UTC()
	}
	in.State = Received
	in.Digest = digest(payload)
	a := &Artifact{Ingress: in, payload: append([]byte(nil), payload...)}
	if err := a.transition(Quarantined, now, "external bytes isolated"); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Artifact) transition(to State, at time.Time, reason string) error {
	from := a.State
	if !legal(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrWrongState, from, to)
	}
	a.State = to
	a.transitions = append(a.transitions, Transition{From: from, To: to, At: at.UTC(), Reason: reason, Generation: a.Generation})
	return nil
}
func legal(from, to State) bool {
	switch from {
	case Received:
		return to == Quarantined
	case Quarantined:
		return to == Validating
	case Validating:
		return to == Scanning || to == Rejected || to == Quarantined
	case Scanning:
		return to == Transforming || to == Rejected || to == Quarantined
	case Transforming:
		return to == Safe || to == Rejected || to == Quarantined
	case Safe:
		return to == Quarantined
	case Rejected:
		return to == Quarantined
	}
	return false
}

func (a *Artifact) Snapshot() Ingress { a.mu.RLock(); defer a.mu.RUnlock(); return a.Ingress }
func (a *Artifact) Payload() []byte {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]byte(nil), a.payload...)
}
func (a *Artifact) Transitions() []Transition {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]Transition(nil), a.transitions...)
}
func (a *Artifact) Inspections() []Inspection {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]Inspection(nil), a.inspections...)
}
func (a *Artifact) Derivatives() []SafeDerivative {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]SafeDerivative, len(a.derivatives))
	for i, d := range a.derivatives {
		out[i] = *d
		out[i].payload = nil
	}
	return out
}

func (a *Artifact) BeginValidation(now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.transition(Validating, now, "server validation")
}
func (a *Artifact) Validate(p Policy, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.transition(Validating, now, "server validation"); err != nil {
		return err
	}
	if err := validate(a, p); err != nil {
		_ = a.transition(Rejected, now, err.Error())
		return err
	}
	return a.transition(Scanning, now, "validation passed")
}

func validate(a *Artifact, p Policy) error {
	l := p.Limits
	if l.MaxBytes == 0 || uint64(len(a.payload)) > l.MaxBytes {
		return fmt.Errorf("%w: size limit", ErrInvalid)
	}
	if l.MaxExpandedBytes > 0 && a.ExpandedBytes > l.MaxExpandedBytes {
		return fmt.Errorf("%w: expanded size limit", ErrInvalid)
	}
	if l.MaxFiles > 0 && a.FileCount > l.MaxFiles {
		return fmt.Errorf("%w: archive file count limit", ErrInvalid)
	}
	if l.MaxArchiveDepth > 0 && a.ArchiveDepth > l.MaxArchiveDepth {
		return fmt.Errorf("%w: archive depth limit", ErrInvalid)
	}
	if l.MaxCompressionRatio > 0 && a.CompressionRatio > l.MaxCompressionRatio {
		return fmt.Errorf("%w: compression ratio limit", ErrInvalid)
	}
	ext := strings.ToLower(filepath.Ext(a.Filename))
	if len(p.Extensions) > 0 {
		if _, ok := p.Extensions[ext]; !ok {
			return fmt.Errorf("%w: extension", ErrInvalid)
		}
	}
	if len(p.MIME) > 0 {
		if _, ok := p.MIME[strings.ToLower(a.DeclaredMIME)]; !ok {
			return fmt.Errorf("%w: mime", ErrInvalid)
		}
	}
	if !signatureMatches(a.DeclaredMIME, a.payload) {
		return fmt.Errorf("%w: signature", ErrInvalid)
	}
	return nil
}
func signatureMatches(m string, b []byte) bool {
	m = strings.ToLower(m)
	switch m {
	case "application/pdf":
		return bytes.HasPrefix(b, []byte("%PDF-"))
	case "image/png":
		return bytes.HasPrefix(b, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	case "image/jpeg":
		return bytes.HasPrefix(b, []byte{255, 216, 255})
	case "application/zip":
		return bytes.HasPrefix(b, []byte("PK\x03\x04"))
	case "text/csv", "text/plain", "application/json":
		return !bytes.Contains(b, []byte{0})
	default:
		return false
	}
}

type Scanner func(context.Context, []byte) (Inspection, error)
type Transformer func(context.Context, []byte) ([]byte, error)

func (a *Artifact) Scan(ctx context.Context, scanner Scanner, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.State != Scanning {
		return fmt.Errorf("%w: scan from %s", ErrWrongState, a.State)
	}
	if scanner == nil {
		_ = a.transition(Quarantined, now, "scanner unavailable")
		return errors.New("content: scanner unavailable")
	}
	in, err := scanner(ctx, append([]byte(nil), a.payload...))
	in.Digest = a.Digest
	in.At = now.UTC()
	a.inspections = append(a.inspections, in)
	if err != nil || in.Verdict != VerdictSafe {
		reason := in.Reason
		if err != nil {
			reason = err.Error()
		}
		_ = a.transition(Rejected, now, reason)
		if reason == "" {
			reason = "unsafe content"
		}
		return fmt.Errorf("content: scan rejected: %s", reason)
	}
	return a.transition(Transforming, now, "scanner classified safe")
}

func (a *Artifact) Transform(ctx context.Context, transform Transformer, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.State != Transforming {
		return fmt.Errorf("%w: transform from %s", ErrWrongState, a.State)
	}
	if transform == nil {
		_ = a.transition(Quarantined, now, "transformer unavailable")
		return errors.New("content: transformer unavailable")
	}
	p, err := transform(ctx, append([]byte(nil), a.payload...))
	if err != nil || len(p) == 0 {
		_ = a.transition(Rejected, now, "transform failed")
		if err != nil {
			return err
		}
		return errors.New("content: empty derivative")
	}
	d := &SafeDerivative{ID: fmt.Sprintf("%s:%d", a.ID, a.Generation+1), Digest: digest(p), MIME: a.DeclaredMIME, Classification: "SAFE", SourceDigest: a.Digest, CreatedAt: now.UTC(), ScannerVersion: a.inspections[len(a.inspections)-1].ScannerVersion, payload: append([]byte(nil), p...)}
	a.derivatives = append(a.derivatives, d)
	return a.transition(Safe, now, "safe derivative created")
}

// Rescan starts a new generation. Existing derivatives are preserved as
// history; a non-safe result revokes every currently usable derivative.
func (a *Artifact) Rescan(ctx context.Context, p Policy, scanner Scanner, transform Transformer, now time.Time) error {
	a.mu.Lock()
	if a.State != Quarantined {
		_ = a.transition(Quarantined, now, "rescan requested")
	}
	a.Generation++
	a.mu.Unlock()
	if err := a.Validate(p, now); err != nil {
		return err
	}
	if err := a.Scan(ctx, scanner, now); err != nil {
		a.revoke()
		return err
	}
	if err := a.Transform(ctx, transform, now); err != nil {
		a.revoke()
		return err
	}
	return nil
}
func (a *Artifact) revoke() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range a.derivatives {
		d.Revoked = true
	}
}
func (a *Artifact) Promoted() (SafeDerivative, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.State != Safe || len(a.derivatives) == 0 {
		return SafeDerivative{}, ErrNotPromotable
	}
	d := *a.derivatives[len(a.derivatives)-1]
	if d.Revoked {
		return SafeDerivative{}, ErrNotPromotable
	}
	d.payload = nil
	return d, nil
}
func digest(p []byte) string { s := sha256.Sum256(p); return hex.EncodeToString(s[:]) }
