//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/productclient"
)

// TestTodo_WEB_031_Browser runs in the real Go js/wasm runtime under Node and
// invokes the production browser-storage adapter. The small Web Storage
// capability objects below exercise only the native JS API seam; no router,
// DOM, network or application state is mocked.
func TestTodo_WEB_031_Browser(t *testing.T) {
	if !js.Global().Truthy() {
		t.Fatal("js runtime did not expose global object")
	}
	ledgerID := newProductHistoryLedgerID()
	if !productclient.ValidHistoryLedgerID(ledgerID) {
		t.Fatalf("browser ledger id %q is not an exact opaque v1 identifier", ledgerID)
	}
	object := js.Global().Get("Object")
	storage := object.New()
	values := map[string]string{}
	setItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		values[args[0].String()] = args[1].String()
		return nil
	})
	getItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if value, ok := values[args[0].String()]; ok {
			return value
		}
		return nil
	})
	defer setItem.Release()
	defer getItem.Release()
	storage.Set("setItem", setItem)
	storage.Set("getItem", getItem)
	oldStorage := js.Global().Get("sessionStorage")
	js.Global().Set("sessionStorage", storage)
	defer js.Global().Set("sessionStorage", oldStorage)

	controller := &browserProductHistoryController{id: ledgerID, maxIndex: 31}
	controller.writeMax()
	key := productclient.HistoryStorageKey(controller.id)
	if values[key] != "31" {
		t.Fatalf("production adapter stored %q = %q, want 31", key, values[key])
	}
	if got := readProductHistoryMax(controller.id); got != 31 {
		t.Fatalf("production adapter read watermark = %d, want 31", got)
	}
	if browserStorageSet(storage, "hcm-next.workflow.truth", "1") {
		t.Fatal("production adapter accepted an authority-shaped key")
	}
	if _, present := values["hcm-next.workflow.truth"]; present {
		t.Fatal("authority-shaped key reached the browser storage API")
	}

	// Web Storage exceptions are ordinary browser conditions (private mode,
	// quota and sandbox policy). They must be absorbed by the optional hint.
	throwing := js.Global().Call("eval", `({setItem(){throw new Error("quota")},getItem(){throw new Error("denied")}})`)
	js.Global().Set("sessionStorage", throwing)
	if browserStorageSet(throwing, key, "31") {
		t.Fatal("quota exception was reported as a successful write")
	}
	if value, ok := browserStorageGet(throwing, key); ok || value != "" {
		t.Fatalf("storage exception read = %q/%v, want empty/false", value, ok)
	}
	if got := readProductHistoryMax(controller.id); got != 0 {
		t.Fatalf("exceptional storage produced watermark %d, want fail-closed zero", got)
	}

	// A host with storage disabled exposes no object at all. This is the
	// non-throwing form of the same browser policy and must remain optional.
	js.Global().Call("eval", `delete globalThis.sessionStorage`)
	if _, ok := browserSessionStorage(); ok {
		t.Fatal("missing sessionStorage was reported as available")
	}
}

func TestTodo_WEB_031_Golden(t *testing.T) {
	// replaceState receives a closed object. A hostile old entry cannot smuggle
	// credentials, tenant, workflow or transaction-shaped fields into the next
	// entry merely because the router is refreshing its ledger annotation.
	object := js.Global().Get("Object")
	history := object.New()
	current := object.New()
	current.Set("tenant", "tenant-a")
	current.Set("credential", "Bearer secret")
	current.Set("workflow", "approved")
	history.Set("state", current)
	var captured js.Value
	replace := js.FuncOf(func(_ js.Value, args []js.Value) any {
		captured = args[0]
		return nil
	})
	defer replace.Release()
	history.Set("replaceState", replace)
	oldHistory := js.Global().Get("history")
	oldLocation := js.Global().Get("location")
	location := object.New()
	location.Set("href", "/workspace/app/home")
	js.Global().Set("history", history)
	js.Global().Set("location", location)
	defer js.Global().Set("history", oldHistory)
	defer js.Global().Set("location", oldLocation)

	controller := &browserProductHistoryController{id: "0123456789abcdef0123456789abcdef"}
	controller.replaceCurrentState(3)
	if captured.Type() != js.TypeObject {
		t.Fatal("replaceState did not receive a state object")
	}
	for _, field := range []string{"tenant", "credential", "workflow", "transaction", "authority"} {
		if captured.Get(field).Type() != js.TypeUndefined {
			t.Errorf("hostile history field %q survived state replacement", field)
		}
	}
	if captured.Get(productHistoryIDField).String() != controller.id || captured.Get(productHistoryIndexField).Int() != 3 {
		t.Fatalf("closed history state = %s/%d", captured.Get(productHistoryIDField).String(), captured.Get(productHistoryIndexField).Int())
	}
}
