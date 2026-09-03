package ephemeralenv

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const rootPrefix = "hcmnext-tool014-"

var (
	// ErrSharedNamespace means another live Environment in this test process
	// already owns the requested backend namespace.
	ErrSharedNamespace = errors.New("ephemeralenv: namespace is already owned by another test environment")
	// ErrInvalidNamespace means a caller supplied an empty, unsafe, or
	// path-shaped namespace instead of an opaque test namespace.
	ErrInvalidNamespace = errors.New("ephemeralenv: invalid namespace")
	// ErrClosed means a caller attempted to use state after cleanup recorded
	// its evidence and removed the run directory.
	ErrClosed = errors.New("ephemeralenv: environment is closed")
	// ErrInvalidObjectKey means an object operation attempted to escape the
	// environment's own object namespace.
	ErrInvalidObjectKey = errors.New("ephemeralenv: invalid object key")
)

// Config controls one test environment. Empty namespace fields are replaced
// with a fresh name derived from RunID. Explicit names are supported only for
// fixture wiring; a duplicate live name is always rejected.
//
// BaseDir is an existing or creatable parent directory. The harness creates
// exactly one child of BaseDir and never removes BaseDir or a sibling. Empty
// uses the operating system temporary directory.
//
// PreserveOnFailure is the only diagnostic retention policy. It applies only
// to New's testing.TB cleanup callback when the owning test has already
// failed; Provision and Close always remove the environment's own state.
type Config struct {
	BaseDir           string
	ObjectNamespace   string
	QueueNamespace    string
	ProviderNamespace string
	PreserveOnFailure bool
}

// CleanupEvidence records what cleanup observed and did. It is deliberately
// value-only: callers can retain it in a test report after filesystem and
// in-memory state have gone away.
type CleanupEvidence struct {
	RunID                 string
	ObjectNamespace       string
	QueueNamespace        string
	ProviderNamespace     string
	ObjectFileCount       int
	QueueMessageCount     int
	ProviderResponseCount int
	StartedAt             time.Time
	CleanedAt             time.Time
	RootRemoved           bool
	ClaimsReleased        bool
	Preserved             bool
	CleanupError          string
}

// Environment owns one private integration-test namespace. It is safe for
// concurrent object, queue, and fake-provider calls; each Environment's
// mutable maps and root directory are separate from every other environment.
type Environment struct {
	RunID             string
	ObjectNamespace   string
	QueueNamespace    string
	ProviderNamespace string

	baseDir    string
	root       string
	objectRoot string

	mu       sync.Mutex
	closed   bool
	queue    []string
	provider map[string]string
	evidence CleanupEvidence
}

var liveNamespaces = struct {
	sync.Mutex
	claims map[string]string
}{claims: map[string]string{}}

// New provisions an environment and registers cleanup with t. Test failures
// remove all state by default; PreserveOnFailure must be explicitly set to
// retain one failed environment for diagnostics.
func New(t testing.TB, cfg Config) *Environment {
	t.Helper()
	env, err := Provision(cfg)
	if err != nil {
		t.Fatalf("provision ephemeral environment: %v", err)
	}
	t.Cleanup(func() {
		preserve := cfg.PreserveOnFailure && failed(t)
		if err := env.cleanup(preserve); err != nil {
			t.Errorf("clean up ephemeral environment %s: %v", env.RunID, err)
			return
		}
		if preserve {
			t.Logf("preserved failed TOOL-014 environment %s at %s", env.RunID, env.Root())
		}
	})
	return env
}

// Provision creates an environment without registering testing cleanup. It is
// useful for tests that need to inspect CleanupEvidence immediately; callers
// must call Close. No external resources are contacted.
func Provision(cfg Config) (*Environment, error) {
	runID, err := newRunID()
	if err != nil {
		return nil, err
	}
	base := cfg.BaseDir
	if base == "" {
		base = os.TempDir()
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("resolve base directory: %w", err)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, fmt.Errorf("create base directory: %w", err)
	}

	objectName := namespaceOrDefault(cfg.ObjectNamespace, "object", runID)
	queueName := namespaceOrDefault(cfg.QueueNamespace, "queue", runID)
	providerName := namespaceOrDefault(cfg.ProviderNamespace, "provider", runID)
	for _, namespace := range []string{objectName, queueName, providerName} {
		if !validNamespace(namespace) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidNamespace, namespace)
		}
	}
	if err := claim(runID, objectName, queueName, providerName); err != nil {
		return nil, err
	}

	root := filepath.Join(base, rootPrefix+runID)
	objectRoot := filepath.Join(root, "objects", objectName)
	// Mkdir (rather than MkdirAll) proves this invocation created the one root
	// that cleanup may later remove. A collision must fail closed instead of
	// adopting an existing diagnostic directory.
	if err := os.Mkdir(root, 0o700); err != nil {
		release(runID, objectName, queueName, providerName)
		return nil, fmt.Errorf("create private run directory: %w", err)
	}
	if err := os.MkdirAll(objectRoot, 0o700); err != nil {
		// This invocation created root above and it has not escaped BaseDir, so
		// remove only this partial run before returning.
		_ = os.RemoveAll(root)
		release(runID, objectName, queueName, providerName)
		return nil, fmt.Errorf("create private object namespace: %w", err)
	}
	now := time.Now().UTC()
	return &Environment{
		RunID:             runID,
		ObjectNamespace:   objectName,
		QueueNamespace:    queueName,
		ProviderNamespace: providerName,
		baseDir:           base,
		root:              root,
		objectRoot:        objectRoot,
		provider:          map[string]string{},
		evidence: CleanupEvidence{
			RunID:             runID,
			ObjectNamespace:   objectName,
			QueueNamespace:    queueName,
			ProviderNamespace: providerName,
			StartedAt:         now,
		},
	}, nil
}

// Root returns the run directory created by this Environment. It is only a
// diagnostic path; callers should use PutObject and GetObject for object data.
func (e *Environment) Root() string { return e.root }

// PutObject writes one object only under this environment's private object
// namespace. Keys are slash-separated relative names, never host paths.
func (e *Environment) PutObject(key string, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	path, err := e.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create object parent: %w", err)
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		return fmt.Errorf("write object: %w", err)
	}
	return nil
}

// GetObject reads one object from this environment's private namespace.
func (e *Environment) GetObject(key string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrClosed
	}
	path, err := e.objectPath(key)
	if err != nil {
		return nil, err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// Enqueue appends one message to this environment's in-memory queue.
func (e *Environment) Enqueue(message string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.queue = append(e.queue, message)
	return nil
}

// Dequeue returns the oldest queued message. ok is false when the private
// queue is empty.
func (e *Environment) Dequeue() (message string, ok bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return "", false, ErrClosed
	}
	if len(e.queue) == 0 {
		return "", false, nil
	}
	message = e.queue[0]
	e.queue = e.queue[1:]
	return message, true, nil
}

// QueueLen reports the number of messages in this environment's private queue.
func (e *Environment) QueueLen() (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return 0, ErrClosed
	}
	return len(e.queue), nil
}

// SetProviderResponse configures one response in this environment's private
// fake provider. It never shares responses with another Environment.
func (e *Environment) SetProviderResponse(key, response string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.provider[key] = response
	return nil
}

// ProviderResponse returns a response from this environment's fake provider.
func (e *Environment) ProviderResponse(key string) (response string, ok bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return "", false, ErrClosed
	}
	response, ok = e.provider[key]
	return response, ok, nil
}

// Evidence returns a copy of the cleanup evidence captured so far. Before
// Close it has identity and StartedAt; after Close it additionally records
// observed state counts and the exact cleanup outcome.
func (e *Environment) Evidence() CleanupEvidence {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.evidence
}

// Close captures cleanup evidence and removes only the run directory created
// by Provision. It is idempotent. A failed deletion keeps the namespace claim
// reserved so another test cannot reuse a possibly live resource name.
func (e *Environment) Close() error { return e.cleanup(false) }

func (e *Environment) cleanup(preserve bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	e.evidence.ObjectFileCount = countFiles(e.objectRoot)
	e.evidence.QueueMessageCount = len(e.queue)
	e.evidence.ProviderResponseCount = len(e.provider)
	e.evidence.CleanedAt = time.Now().UTC()
	e.queue = nil
	e.provider = nil

	if preserve {
		e.evidence.Preserved = true
		return nil
	}
	if !ownedRoot(e.baseDir, e.root, e.RunID) {
		err := fmt.Errorf("refusing unsafe cleanup target %q", e.root)
		e.evidence.CleanupError = err.Error()
		return err
	}
	if err := os.RemoveAll(e.root); err != nil {
		e.evidence.CleanupError = err.Error()
		return fmt.Errorf("remove run directory: %w", err)
	}
	if _, err := os.Stat(e.root); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			err = fmt.Errorf("run directory still exists")
		}
		e.evidence.CleanupError = err.Error()
		return fmt.Errorf("verify run-directory cleanup: %w", err)
	}
	e.evidence.RootRemoved = true
	release(e.RunID, e.ObjectNamespace, e.QueueNamespace, e.ProviderNamespace)
	e.evidence.ClaimsReleased = true
	return nil
}

func (e *Environment) objectPath(key string) (string, error) {
	// Object keys use forward slashes regardless of the host. Check that
	// representation before filepath normalizes it: on Windows, /name is not
	// necessarily reported as absolute even though accepting it would violate
	// the cross-platform object-key contract.
	if key == "" || strings.HasPrefix(key, "/") || filepath.IsAbs(key) || filepath.VolumeName(key) != "" || strings.Contains(key, "\\") {
		return "", fmt.Errorf("%w: %q", ErrInvalidObjectKey, key)
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidObjectKey, key)
	}
	path := filepath.Join(e.objectRoot, clean)
	rel, err := filepath.Rel(e.objectRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidObjectKey, key)
	}
	return path, nil
}

func newRunID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("create random test namespace: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func namespaceOrDefault(got, kind, runID string) string {
	if got != "" {
		return got
	}
	return kind + "-" + runID
}

func validNamespace(name string) bool {
	if len(name) < 3 || len(name) > 96 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return name[0] != '-' && name[len(name)-1] != '-'
}

func claim(runID, object, queue, provider string) error {
	claims := []string{"object:" + object, "queue:" + queue, "provider:" + provider}
	liveNamespaces.Lock()
	defer liveNamespaces.Unlock()
	for _, key := range claims {
		if owner, exists := liveNamespaces.claims[key]; exists {
			return fmt.Errorf("%w: %s is owned by run %s", ErrSharedNamespace, key, owner)
		}
	}
	for _, key := range claims {
		liveNamespaces.claims[key] = runID
	}
	return nil
}

func release(runID, object, queue, provider string) {
	claims := []string{"object:" + object, "queue:" + queue, "provider:" + provider}
	liveNamespaces.Lock()
	defer liveNamespaces.Unlock()
	for _, key := range claims {
		if liveNamespaces.claims[key] == runID {
			delete(liveNamespaces.claims, key)
		}
	}
}

func ownedRoot(base, root, runID string) bool {
	if filepath.Dir(root) != base || filepath.Base(root) != rootPrefix+runID {
		return false
	}
	return strings.HasPrefix(filepath.Base(root), rootPrefix) && validNamespace("run-"+runID)
}

func countFiles(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func failed(t testing.TB) bool {
	if withStatus, ok := t.(interface{ Failed() bool }); ok {
		return withStatus.Failed()
	}
	return false
}
