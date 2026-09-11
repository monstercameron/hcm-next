//go:build !js || !wasm

package productui

// Static rendering has no browser focus lifecycle.
func usePopoverFocusDismissal(rootID, triggerID string, open bool, dismiss func()) {}
