package journey

import (
	"errors"
	"sync"
)

// MountState is the deliberately small lifecycle of a browser mount. It is
// kept independent of syscall/js so its ordering and cleanup guarantees can
// be exercised by the native race detector.
type MountState string

const (
	MountIdle    MountState = "idle"
	Mounting     MountState = "mounting"
	MountMounted MountState = "mounted"
	MountFailed  MountState = "failed"
	MountStopped MountState = "stopped"
)

const (
	RootElementID = "app"
	RootSelector  = "#" + RootElementID
)

var (
	ErrInvalidSelector = errors.New("journey mount: invalid root selector")
	ErrAlreadyMounted  = errors.New("journey mount: another mount is active")
	ErrMountStopped    = errors.New("journey mount: mount stopped")
	ErrMountFailed     = errors.New("journey mount: mount failed")
)

// mountAttempt is the result shared by all callers that arrive while one
// render is in flight. done is closed only after rollback/teardown finishes,
// so a retry can never be unmounted by cleanup from the preceding attempt.
type mountAttempt struct {
	done          chan struct{}
	result        error
	stopRequested bool
}

// MountLifecycle owns validation, transitions, single-mount policy and
// cleanup. The callbacks are the platform adapter; product/business state
// remains in Store and the component tree.
//
// A lifecycle is reusable after a failed or stopped attempt. That property is
// important for the package-wide browser entrypoint: a startup failure page
// and a host remount must not require replacing process-global state.
type MountLifecycle struct {
	mu       sync.Mutex
	state    MountState
	selector string
	cleanup  func()
	attempt  *mountAttempt
}

func NewMountLifecycle() *MountLifecycle { return &MountLifecycle{state: MountIdle} }

func (m *MountLifecycle) State() MountState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *MountLifecycle) Selector() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selector
}

// Mount claims the root and invokes render exactly once. Duplicate calls for
// the same root are idempotent. A duplicate that arrives during rendering
// waits for, and returns, that attempt's real result rather than reporting a
// success before the DOM outcome is known.
//
// Adapter errors and panics are deliberately collapsed to stable sentinels so
// selector/DOM details cannot cross the public boundary. The caller still has
// errors.Is-compatible outcomes for diagnostics and tests.
func (m *MountLifecycle) Mount(selector string, render func() error, cleanup func()) error {
	if selector != RootSelector || render == nil {
		return ErrInvalidSelector
	}

	for {
		m.mu.Lock()
		if active := m.attempt; active != nil {
			if m.selector != selector {
				m.mu.Unlock()
				return ErrAlreadyMounted
			}
			done := active.done
			remountAfterTeardown := active.stopRequested
			m.mu.Unlock()
			<-done
			if remountAfterTeardown {
				continue
			}
			return active.result
		}
		if m.state == MountMounted {
			if m.selector == selector {
				m.mu.Unlock()
				return nil
			}
			m.mu.Unlock()
			return ErrAlreadyMounted
		}

		attempt := &mountAttempt{done: make(chan struct{})}
		m.state = Mounting
		m.selector = selector
		m.cleanup = cleanup
		m.attempt = attempt
		m.mu.Unlock()

		renderErr := callMountCallback(render)

		m.mu.Lock()
		if renderErr == nil && !attempt.stopRequested {
			m.state = MountMounted
			attempt.result = nil
			m.attempt = nil
			close(attempt.done)
			m.mu.Unlock()
			return nil
		}
		rollbackCleanup := m.cleanup
		m.cleanup = nil
		m.mu.Unlock()

		// Rollback is part of the attempt. Keeping attempt non-nil until it is
		// done prevents an eager retry from racing this cleanup and losing its
		// freshly mounted tree.
		if rollbackCleanup != nil {
			callCleanup(rollbackCleanup)
		}

		m.mu.Lock()
		switch {
		case attempt.stopRequested:
			m.state = MountStopped
			attempt.result = ErrMountStopped
		case renderErr != nil:
			m.state = MountFailed
			attempt.result = ErrMountFailed
		}
		m.attempt = nil
		close(attempt.done)
		m.mu.Unlock()
		return attempt.result
	}
}

// Stop is idempotent, waits for an in-flight render to settle, and releases
// the adapter subscription exactly once. A subsequent Mount starts a fresh
// attempt; Stopped describes the last completed transition, not a poisoned
// terminal object.
func (m *MountLifecycle) Stop() {
	m.mu.Lock()
	if active := m.attempt; active != nil {
		active.stopRequested = true
		done := active.done
		m.mu.Unlock()
		<-done
		return
	}
	if m.state != MountMounted {
		m.state = MountStopped
		m.mu.Unlock()
		return
	}

	// Publish a teardown attempt before releasing the lock. Mount callers
	// wait on it, so cleanup from this tree cannot erase a concurrent remount.
	attempt := &mountAttempt{done: make(chan struct{}), stopRequested: true}
	cleanup := m.cleanup
	m.cleanup = nil
	m.state = MountStopped
	m.attempt = attempt
	m.mu.Unlock()
	if cleanup != nil {
		callCleanup(cleanup)
	}

	m.mu.Lock()
	attempt.result = ErrMountStopped
	m.attempt = nil
	close(attempt.done)
	m.mu.Unlock()
}

func callMountCallback(render func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrMountFailed
		}
	}()
	return render()
}

func callCleanup(cleanup func()) {
	defer func() { _ = recover() }()
	cleanup()
}
