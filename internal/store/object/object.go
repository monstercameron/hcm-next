// Package object defines the small provider-neutral contract used for artifact
// bytes.  Providers translate their native errors and locators at this
// boundary; callers never use a bucket, key, version id, or SDK type as an
// artifact identity.
package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

var (
	ErrInvalidRequest   = errors.New("object: invalid request")
	ErrInvalidPath      = errors.New("object: invalid path")
	ErrAlreadyExists    = errors.New("object: object already exists")
	ErrNotFound         = errors.New("object: object not found")
	ErrPrecondition     = errors.New("object: generation precondition failed")
	ErrConditionFailed  = ErrPrecondition // compatibility spelling for adapters
	ErrDigestMismatch   = errors.New("object: digest mismatch")
	ErrAborted          = errors.New("object: object was aborted")
	ErrRangeUnsupported = errors.New("object: invalid byte range")
)

// PutRequest describes an immutable content-addressed write. ArtifactID is
// semantic identity; Key is only a provider locator and is deliberately not
// exposed by this package.
type PutRequest struct {
	ArtifactID string
	Content    []byte
	Digest     string
	MediaType  string
	// ExpectedGeneration is an optional provider generation fence. Put remains
	// create-only even when it matches; a matching fence merely distinguishes
	// a stale retry from an immutable overwrite attempt.
	ExpectedGeneration *uint64
}

type Request struct {
	ArtifactID         string
	ExpectedGeneration *uint64
}

// RangeRequest uses an inclusive start and exclusive end. A nil End reads to
// EOF. This avoids provider-specific range conventions.
type RangeRequest struct {
	ArtifactID string
	Start      int64
	End        *int64
}

type Info struct {
	ArtifactID string
	Digest     string
	Size       int64
	MediaType  string
	Generation uint64
}

type Store interface {
	PutIfAbsent(context.Context, PutRequest) (Info, error)
	Stat(context.Context, Request) (Info, error)
	Get(context.Context, Request) (io.ReadCloser, Info, error)
	GetRange(context.Context, RangeRequest) (io.ReadCloser, Info, error)
	Abort(context.Context, Request) error
}

// ObjectStore and Contract are descriptive aliases used by adapters and
// conformance suites; both intentionally expose the same provider-neutral
// surface.
type ObjectStore = Store
type Contract = Store

func validateID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || id == "." || id == ".." || strings.Contains(id, "..") {
		return fmt.Errorf("%w: artifact id %q", ErrInvalidPath, id)
	}
	return nil
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Fake is an in-memory conformance implementation suitable for unit tests.
// It is concurrency-safe and models the observable guarantees of an object
// provider, including idempotent replay and immutable generations.
type Fake struct {
	mu      sync.RWMutex
	objects map[string]entry
	aborted map[string]bool
}

type entry struct {
	info Info
	data []byte
}

func NewFake() *Fake { return &Fake{objects: make(map[string]entry), aborted: make(map[string]bool)} }

// MemoryStore is the in-process implementation name commonly used by tests.
type MemoryStore = Fake

func NewMemoryStore() *MemoryStore { return NewFake() }

func (f *Fake) PutIfAbsent(ctx context.Context, req PutRequest) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if err := validateID(req.ArtifactID); err != nil {
		return Info{}, err
	}
	if len(req.Content) == 0 {
		return Info{}, fmt.Errorf("%w: content is empty", ErrInvalidRequest)
	}
	want := digest(req.Content)
	if req.Digest != "" && req.Digest != want {
		return Info{}, fmt.Errorf("%w: want %s, got %s", ErrDigestMismatch, req.Digest, want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.aborted[req.ArtifactID] {
		return Info{}, fmt.Errorf("%w: %s", ErrAborted, req.ArtifactID)
	}
	if old, ok := f.objects[req.ArtifactID]; ok {
		if req.ExpectedGeneration != nil && old.info.Generation != *req.ExpectedGeneration {
			return Info{}, fmt.Errorf("%w: expected generation %d, got %d", ErrPrecondition, *req.ExpectedGeneration, old.info.Generation)
		}
		if old.info.Digest != want || old.info.MediaType != req.MediaType || !bytes.Equal(old.data, req.Content) {
			return Info{}, fmt.Errorf("%w: %s", ErrAlreadyExists, req.ArtifactID)
		}
		return old.info, nil // replay is not a second object/provider request
	}
	info := Info{ArtifactID: req.ArtifactID, Digest: want, Size: int64(len(req.Content)), MediaType: req.MediaType, Generation: 1}
	f.objects[req.ArtifactID] = entry{info: info, data: append([]byte(nil), req.Content...)}
	return info, nil
}

func (f *Fake) Stat(ctx context.Context, req Request) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if err := validateID(req.ArtifactID); err != nil {
		return Info{}, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.aborted[req.ArtifactID] {
		return Info{}, fmt.Errorf("%w: %s", ErrAborted, req.ArtifactID)
	}
	e, ok := f.objects[req.ArtifactID]
	if !ok {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, req.ArtifactID)
	}
	if req.ExpectedGeneration != nil && e.info.Generation != *req.ExpectedGeneration {
		return Info{}, fmt.Errorf("%w: expected generation %d, got %d", ErrPrecondition, *req.ExpectedGeneration, e.info.Generation)
	}
	return e.info, nil
}

func (f *Fake) Get(ctx context.Context, req Request) (io.ReadCloser, Info, error) {
	return f.get(ctx, req.ArtifactID, req.ExpectedGeneration, 0, nil)
}

func (f *Fake) GetRange(ctx context.Context, req RangeRequest) (io.ReadCloser, Info, error) {
	return f.get(ctx, req.ArtifactID, nil, req.Start, req.End)
}

func (f *Fake) get(ctx context.Context, id string, expected *uint64, start int64, end *int64) (io.ReadCloser, Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	if err := validateID(id); err != nil {
		return nil, Info{}, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.aborted[id] {
		return nil, Info{}, fmt.Errorf("%w: %s", ErrAborted, id)
	}
	e, ok := f.objects[id]
	if !ok {
		return nil, Info{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if expected != nil && e.info.Generation != *expected {
		return nil, Info{}, fmt.Errorf("%w: expected generation %d, got %d", ErrPrecondition, *expected, e.info.Generation)
	}
	if end == nil && start == 0 {
		return io.NopCloser(bytes.NewReader(append([]byte(nil), e.data...))), e.info, nil
	}
	finish := int64(len(e.data))
	if end != nil {
		finish = *end
	}
	if start < 0 || finish < start || finish > int64(len(e.data)) {
		return nil, Info{}, fmt.Errorf("%w: [%d,%d)", ErrRangeUnsupported, start, finish)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), e.data[start:finish]...))), e.info, nil
}

func (f *Fake) Abort(ctx context.Context, req Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateID(req.ArtifactID); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects[req.ArtifactID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, req.ArtifactID)
	}
	delete(f.objects, req.ArtifactID)
	f.aborted[req.ArtifactID] = true
	return nil
}
