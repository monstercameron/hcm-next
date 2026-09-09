package productclient

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

func TestTodo_WEB_031(t *testing.T) {
	key := HistoryStorageKey("0123456789abcdef0123456789abcdef")
	if key != HistoryStorageKeyPrefix+"0123456789abcdef0123456789abcdef" {
		t.Fatalf("history key = %q", key)
	}
	if err := ValidateBrowserStorageWrite(key, "17"); err != nil {
		t.Fatalf("valid presentation record rejected: %v", err)
	}
	if got, ok := DecodeHistoryIndex("17"); !ok || got != 17 {
		t.Fatalf("decoded history watermark = %d/%v", got, ok)
	}
	for _, forbidden := range []string{
		"hcm-next:tenant:tenant-a",
		"hcm-next:workflow:approval",
		"hcm-next:authorization:role",
		"hcm-next:transaction:commit",
		"hcm-next:credential:bearer",
	} {
		if err := ValidateBrowserStorageWrite(forbidden, "17"); !errors.Is(err, ErrBrowserStateAuthority) {
			t.Errorf("forbidden browser key %q error = %v, want authority refusal", forbidden, err)
		}
	}
}

func TestTodo_WEB_031_Golden(t *testing.T) {
	if BrowserStateVersion != "hcm-next.browser-state.v1" {
		t.Fatalf("browser state version = %q", BrowserStateVersion)
	}
	if HistoryStorageKeyPrefix != "hcm-next.browser-state.v1.history." {
		t.Fatalf("history key prefix = %q", HistoryStorageKeyPrefix)
	}
	for index, want := range map[int]string{0: "0", 1: "1", 9999: "9999", 10000: "10000"} {
		if got, ok := EncodeHistoryIndex(index); !ok || got != want {
			t.Errorf("encode %d = %q/%v, want %q/true", index, got, ok, want)
		}
	}
	for _, id := range []string{"0123456789abcdef0123456789abcdef", "abcdefabcdefabcdefabcdefabcdefab", strings.Repeat("f", HistoryLedgerIDLength)} {
		if !ValidHistoryLedgerID(id) || HistoryStorageKey(id) == "" {
			t.Errorf("valid opaque ledger id %q was refused", id)
		}
	}
}

func TestTodo_WEB_031_Browser(t *testing.T) {
	// This is the browser-facing value contract. The same pure code is linked
	// by the GOOS=js/GOARCH=wasm test binary; there is no fake storage or JS
	// implementation here, so Node and native execution cannot diverge.
	key := HistoryStorageKey("11111111111111111111111111111111")
	for _, value := range []string{"0", "4", "10000"} {
		if err := ValidateHistoryStorageEntry(key, value); err != nil {
			t.Errorf("browser storage value %q rejected: %v", value, err)
		}
	}
	if err := ValidateHistoryStorageEntry(key, "{"); !errors.Is(err, ErrBrowserStateValue) {
		t.Fatalf("browser JSON snapshot error = %v, want value refusal", err)
	}
}

func TestTodo_WEB_031_Conformance(t *testing.T) {
	for _, id := range []string{
		"", " ", "ledger-a", "ledger_1", "ledger.1", strings.Repeat("a", HistoryLedgerIDLength+1),
		"<script>", "tenantA", "ledger/1", strings.Repeat("g", HistoryLedgerIDLength),
	} {
		if ValidHistoryLedgerID(id) || HistoryStorageKey(id) != "" {
			t.Errorf("unsafe ledger id %q accepted", id)
		}
	}
	for _, raw := range []string{"", " ", " 1", "01", "-1", "+1", "1.0", "2147483648", "1e2", "nan", strings.Repeat("9", 64)} {
		if got, ok := DecodeHistoryIndex(raw); ok {
			t.Errorf("malformed history watermark %q decoded as %d", raw, got)
		}
	}
	for _, key := range []string{
		HistoryStorageKeyPrefix,
		HistoryStorageKeyPrefix + "ledger-a",
		HistoryStorageKeyPrefix + strings.Repeat("a", HistoryLedgerIDLength+1),
		"HCM-NEXT.BROWSER-STATE.V1.HISTORY.ledger1",
	} {
		if err := ValidateHistoryStorageEntry(key, "1"); !errors.Is(err, ErrBrowserStateKey) {
			t.Errorf("malformed key %q error = %v, want key refusal", key, err)
		}
	}
}

func TestTodo_WEB_031_Security(t *testing.T) {
	// A copied value from another tab/session is harmless unless its exact
	// opaque ledger key is selected by history.state. No tenant/principal or
	// authority context is persisted to make the value reusable.
	foreign := HistoryStorageKey("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	current := HistoryStorageKey("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if foreign == current || foreign == "" || current == "" {
		t.Fatal("session ledgers were not isolated by opaque key")
	}
	if err := ValidateBrowserStorageWrite(foreign, "9999"); err != nil {
		t.Fatalf("foreign opaque presentation record should remain harmless: %v", err)
	}
	for _, payload := range []string{
		`{"tenant":"tenant-a"}`,
		`{"principal":"alice","role":"admin"}`,
		`{"workflow":"approved","transaction":"committed"}`,
		"Bearer secret-token",
	} {
		if err := ValidateBrowserStorageWrite(current, payload); !errors.Is(err, ErrBrowserStateValue) {
			t.Errorf("authority-shaped payload %q error = %v, want value refusal", payload, err)
		}
	}
}

func TestTodo_WEB_031_Integration(t *testing.T) {
	storage := map[string]string{}
	write := func(key, value string) {
		if ValidateBrowserStorageWrite(key, value) == nil {
			storage[key] = value
		}
	}
	key := HistoryStorageKey("cccccccccccccccccccccccccccccccc")
	write(key, "12")
	write("hcm-next:workflow:truth", "approved")
	if len(storage) != 1 || storage[key] != "12" {
		t.Fatalf("storage contents = %#v, want only bounded presentation record", storage)
	}
	if got, ok := DecodeHistoryIndex(storage[key]); !ok || got != 12 {
		t.Fatalf("integrated watermark = %d/%v", got, ok)
	}
}

func TestTodo_WEB_031_Fault(t *testing.T) {
	key := HistoryStorageKey("dddddddddddddddddddddddddddddddd")
	for _, value := range []string{"null", "undefined", "NaN", "Infinity", "18446744073709551616", "0\x00", "1\n"} {
		if ValidateBrowserStorageWrite(key, value) == nil {
			t.Errorf("fault value %q was accepted", value)
		}
	}
	if ValidateBrowserStorageWrite("hcm-next.browser-state.v0.history.faultledger", "1") == nil {
		t.Fatal("stale browser-state version was accepted")
	}
}

func BenchmarkBrowserStateStorageBoundary(b *testing.B) {
	key := HistoryStorageKey("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		value, ok := EncodeHistoryIndex(index % (MaxHistoryIndex + 1))
		if !ok || ValidateBrowserStorageWrite(key, value) != nil {
			b.Fatal("boundary rejected a valid bounded record")
		}
	}
}

func TestTodo_WEB_031_Latency(t *testing.T) {
	key := HistoryStorageKey("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	budget := latencygate.Budget{Name: "browser-state boundary", P95: 2 * time.Millisecond, Warmups: 3, Samples: 25}
	result, err := latencygate.Measure(budget, func() error {
		value, ok := EncodeHistoryIndex(17)
		if !ok {
			return errors.New("valid watermark was not encodable")
		}
		return ValidateBrowserStorageWrite(key, value)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}
