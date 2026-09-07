//go:build !js || !wasm

package journey

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Native shims exercise the exact same lifecycle and component tree without
// importing syscall/js. They are useful to callers that share startup code
// between the wasm command and native conformance tests.
func Mount(p Page, selector string) error { return MountLive(NewStore(p), selector) }

func MountLive(store *Store, selector string) error {
	if store == nil {
		return ErrMountFailed
	}
	return defaultMount.Mount(selector, func() error {
		_, err := ui.RenderToString(LiveComponent(store))
		return err
	}, nil)
}

var defaultMount = NewMountLifecycle()

func StopMount() { defaultMount.Stop() }
