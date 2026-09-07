package productclient

import (
	"errors"
	"strconv"
	"strings"
)

// Browser state is a presentation hint, never a snapshot of product truth.
// The browser may retain the HistoryRouter's bounded forward watermark so a
// popstate can render truthful back/forward controls. Everything else that is
// durable (preferences, workflow state, authorization and transactions) is
// owned by the server and is deliberately outside this contract.
const (
	BrowserStateVersion     = "hcm-next.browser-state.v1"
	HistoryStorageKeyPrefix = BrowserStateVersion + ".history."
	MaxHistoryLedgerIDBytes = 32
	// Browser history implementations cannot materialize anywhere near this
	// many entries, but the bound keeps hostile storage finite without making a
	// normal long-lived tab silently lose forward semantics.
	MaxHistoryIndex = 2_147_483_647
)

var (
	ErrBrowserStateKey       = errors.New("browser state key is not an allowed presentation key")
	ErrBrowserStateValue     = errors.New("browser state value is not a bounded history index")
	ErrBrowserStateAuthority = errors.New("browser state cannot contain authority or business state")
)

// HistoryStorageKey returns the sole key that production code may write to
// browser session storage. An empty result means id is not a locally minted,
// opaque ledger identifier. In particular, tenant, principal, session and
// workflow identifiers are not accepted as namespaces.
func HistoryStorageKey(id string) string {
	if !ValidHistoryLedgerID(id) {
		return ""
	}
	return HistoryStorageKeyPrefix + id
}

// ValidHistoryLedgerID accepts exactly the 128-bit lowercase hexadecimal
// identifier minted for the current browser session. A fixed format keeps the
// storage key opaque and prevents delimiter, URL, tenant and control-character
// smuggling; semantic tenant/principal identifiers are never valid ledgers.
func ValidHistoryLedgerID(id string) bool {
	if len(id) != MaxHistoryLedgerIDBytes {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// EncodeHistoryIndex validates and serializes a high-water mark. Keeping the
// value decimal and bounded makes malformed or hostile storage harmless.
func EncodeHistoryIndex(index int) (string, bool) {
	if index < 0 || index > MaxHistoryIndex {
		return "", false
	}
	return strconv.Itoa(index), true
}

// DecodeHistoryIndex parses an untrusted browser-storage value. Invalid,
// negative, oversized and non-canonical values fail closed to (0, false).
func DecodeHistoryIndex(raw string) (int, bool) {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > 10 {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	if len(raw) > 1 && raw[0] == '0' {
		return 0, false
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || index > MaxHistoryIndex {
		return 0, false
	}
	return index, true
}

// ValidateHistoryStorageEntry is the adapter boundary for sessionStorage.
// Callers must validate both the key and value before setItem, and must treat
// a validation error as a no-op. This means a copied browser storage object
// cannot introduce credentials, tenant data, workflow truth, or arbitrary
// JSON into the product client.
func ValidateHistoryStorageEntry(key, value string) error {
	if !strings.HasPrefix(key, HistoryStorageKeyPrefix) ||
		!ValidHistoryLedgerID(strings.TrimPrefix(key, HistoryStorageKeyPrefix)) {
		return ErrBrowserStateKey
	}
	if _, ok := DecodeHistoryIndex(value); !ok {
		return ErrBrowserStateValue
	}
	return nil
}

// ValidateBrowserStorageWrite is intentionally stricter than a generic
// browser-storage helper: this product has exactly one durable presentation
// record. Any key that resembles a business, identity, authorization,
// workflow, transaction or credential record is refused with the same stable
// authority error used by policy tests.
func ValidateBrowserStorageWrite(key, value string) error {
	if err := ValidateHistoryStorageEntry(key, value); err != nil {
		if errors.Is(err, ErrBrowserStateValue) {
			return err
		}
		return ErrBrowserStateAuthority
	}
	return nil
}
