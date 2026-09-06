package main

import "testing"

func TestNavigationGroupStateIsBoundedAndRejectsUntrustedKeys(t *testing.T) {
	raw := `{"work":true,"admin":false,"<script>":true,"UPPER":true}`
	state := decodeNavigationGroupState(raw)
	if len(state) != 2 || !state["work"] || state["admin"] {
		t.Fatalf("decoded navigation state = %+v", state)
	}
	encoded := encodeNavigationGroupState(map[string]bool{"work": false, "admin": true, "bad key": true})
	if encoded != `{"admin":true,"work":false}` {
		t.Fatalf("encoded navigation state = %s", encoded)
	}
}

func TestNavigationGroupStateRefusesOversizedStorage(t *testing.T) {
	raw := make([]byte, navigationGroupStateMaxBytes+1)
	for index := range raw {
		raw[index] = 'x'
	}
	if state := decodeNavigationGroupState(string(raw)); len(state) != 0 {
		t.Fatalf("oversized navigation state = %+v", state)
	}
}
