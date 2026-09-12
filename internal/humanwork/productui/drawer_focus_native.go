//go:build !js || !wasm

package productui

// useDrawerFocusTrap has no browser focus lifecycle to manage outside a
// js/wasm client: static rendering never opens the drawer, so there is
// nothing to trap or restore. See drawer_focus_wasm.go for the real
// implementation.
func useDrawerFocusTrap(dialogID, triggerID string, open bool) {}
