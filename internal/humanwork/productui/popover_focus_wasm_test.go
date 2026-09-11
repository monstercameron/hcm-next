//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
	"time"
)

func TestActionLauncherFocusDismissal(t *testing.T) {
	global := js.Global()
	previous := global.Get("document")
	doc := global.Get("Object").New()
	root := global.Get("Object").New()
	inside := global.Get("Object").New()
	outside := global.Get("Object").New()
	handlers := map[string]js.Value{}
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any { return args[0].Equal(inside) })
	lookup := js.FuncOf(func(js.Value, []js.Value) any { return root })
	add := js.FuncOf(func(_ js.Value, args []js.Value) any { handlers[args[0].String()] = args[1]; return nil })
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any { delete(handlers, args[0].String()); return nil })
	root.Set("contains", contains)
	doc.Set("getElementById", lookup)
	doc.Set("addEventListener", add)
	doc.Set("removeEventListener", remove)
	doc.Set("activeElement", inside)
	global.Set("document", doc)
	t.Cleanup(func() {
		global.Set("document", previous)
		contains.Release()
		lookup.Release()
		add.Release()
		remove.Release()
	})
	dismissed := 0
	cleanup := bindPopoverFocusDismissal("root", "trigger", func() { dismissed++ })
	emit := func(kind string, target, related js.Value) {
		event := global.Get("Object").New()
		event.Set("type", kind)
		event.Set("target", target)
		event.Set("relatedTarget", related)
		handlers[kind].Invoke(event)
	}
	emit("focusout", inside, inside)
	time.Sleep(220 * time.Millisecond)
	if dismissed != 0 {
		t.Fatal("moving within the popover dismissed it")
	}
	doc.Set("activeElement", outside)
	emit("focusout", inside, outside)
	time.Sleep(220 * time.Millisecond)
	if dismissed != 1 {
		t.Fatal("leaving focus did not dismiss the popover")
	}
	emit("pointerdown", inside, js.Null())
	if dismissed != 1 {
		t.Fatal("inside click dismissed the popover")
	}
	emit("pointerdown", outside, js.Null())
	if dismissed != 2 {
		t.Fatal("outside click did not dismiss the popover")
	}
	emit("focusout", inside, outside)
	cleanup()
	time.Sleep(220 * time.Millisecond)
	if dismissed != 2 || len(handlers) != 0 {
		t.Fatal("cleanup left live callbacks")
	}
}
