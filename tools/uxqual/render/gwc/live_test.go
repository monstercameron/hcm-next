package gwc

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLiveBindingIsAStableWireType(t *testing.T) {
	bound := LiveBinding{
		Action: "/workspace/promotion/simulate",
		FormID: "promotion-request",
		Hidden: map[string]string{"csrf_token": "abc", "worker": "w-1"},
	}
	body, err := json.Marshal(bound)
	if err != nil {
		t.Fatalf("marshal the binding: %v", err)
	}

	var back LiveBinding
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal the binding: %v", err)
	}
	if !reflect.DeepEqual(back, bound) {
		t.Errorf("the binding does not round-trip\n got: %+v\nwant: %+v", back, bound)
	}

	// The field names are the wire contract with the workspace package's
	// data island; pin the exact encoding so a rename cannot slip through.
	const want = `{"action":"/workspace/promotion/simulate","form_id":"promotion-request","hidden":{"csrf_token":"abc","worker":"w-1"}}`
	if string(body) != want {
		t.Errorf("wire encoding drifted from the island protocol\n got:  %s\nwant:  %s", body, want)
	}
}
