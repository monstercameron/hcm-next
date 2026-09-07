package values

import "testing"

func TestNFC_ComposesDecomposedInputAndLeavesNormalisedTextAlone(t *testing.T) {
	decomposed := "é"
	composed := "é"
	if got := NFC(decomposed); got != composed {
		t.Fatalf("NFC(%q) = %q, want %q", decomposed, got, composed)
	}
	if got := NFC(composed); got != composed {
		t.Fatalf("NFC must leave composed text unchanged, got %q", got)
	}
	if NFC("") != "" {
		t.Fatal("NFC of the empty string must be empty")
	}
	if IsNFC(decomposed) || !IsNFC(composed) || !IsNFC("") {
		t.Fatal("IsNFC must distinguish decomposed from composed text")
	}
}
