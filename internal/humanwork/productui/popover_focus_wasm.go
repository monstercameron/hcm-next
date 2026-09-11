//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// usePopoverFocusDismissal keeps stateful popovers open during internal focus
// travel, but dismisses them when keyboard focus or a pointer moves outside.
func usePopoverFocusDismissal(rootID, triggerID string, open bool, dismiss func()) {
	ui.UseEffectOf(func() func() {
		if !open {
			return nil
		}
		return bindPopoverFocusDismissal(rootID, triggerID, dismiss)
	}, struct {
		Root, Trigger string
		Open          bool
	}{rootID, triggerID, open})
}

func bindPopoverFocusDismissal(rootID, triggerID string, dismiss func()) func() {
	doc := js.Global().Get("document")
	root := doc.Call("getElementById", rootID)
	if !root.Truthy() {
		return nil
	}
	var timer js.Value
	pending := false
	check := js.FuncOf(func(js.Value, []js.Value) any {
		pending = false
		active := doc.Get("activeElement")
		if !active.Truthy() || !root.Call("contains", active).Bool() {
			dismiss()
		}
		return nil
	})
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		inside := target.Truthy() && root.Call("contains", target).Bool()
		switch event.Get("type").String() {
		case "pointerdown":
			if !inside {
				dismiss()
			}
		case "focusout":
			if !inside {
				return nil
			}
			related := event.Get("relatedTarget")
			if related.Truthy() && root.Call("contains", related).Bool() {
				return nil
			}
			if pending {
				js.Global().Call("clearTimeout", timer)
			}
			pending = true
			timer = js.Global().Call("setTimeout", check, 180)
		case "keydown":
			if inside && event.Get("key").String() == "Escape" {
				event.Call("preventDefault")
				dismiss()
				if trigger := doc.Call("getElementById", triggerID); trigger.Truthy() {
					trigger.Call("focus")
				}
			}
		}
		return nil
	})
	for _, kind := range []string{"focusout", "pointerdown", "keydown"} {
		doc.Call("addEventListener", kind, listener)
	}
	return func() {
		for _, kind := range []string{"focusout", "pointerdown", "keydown"} {
			doc.Call("removeEventListener", kind, listener)
		}
		if pending {
			js.Global().Call("clearTimeout", timer)
		}
		listener.Release()
		check.Release()
	}
}
