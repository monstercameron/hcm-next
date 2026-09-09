package balance

// This file owns the in-memory semantic side of balance posting. Persistence
// adapters can use the same request/receipt contract, but are deliberately not
// part of this package.
import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrEntryInvalid        = errors.New("balance: invalid entry")
	ErrStaleHead           = errors.New("balance: stale expected head")
	ErrIdempotencyConflict = errors.New("balance: idempotency key reused with different entry")
)

type EntryKind string

const (
	Debit       EntryKind = "DEBIT"
	Credit      EntryKind = "CREDIT"
	EntryDebit            = Debit
	EntryCredit           = Credit
)

// BalanceEntry is an immutable, typed fact. Amount is always unsigned; Kind
// supplies its accounting direction. Dimensions are copied at posting time.
//
// EffectiveAt, RecordedAt and AuthorizedAt are the three instants a BAL-003
// authorized-balance calculation reads (see asof.go). They are optional here
// -- BAL-002's own contract (Validate, Post) never requires them, so entries
// posted before BAL-003 existed, or posted through paths that never declare
// them, remain valid and continue to post exactly as before. A BAL-003
// calculation instead treats an unset instant as "never" for its axis (never
// recorded, never effective, never authorized), which is the safe default:
// an entry can only ever be under-counted by an absent instant, never
// over-counted.
//
//   - EffectiveAt is the business instant this entry affects (the as-of
//     axis). Caller-declared, part of the entry's asserted content.
//   - RecordedAt is the system instant this fact was appended to the ledger
//     (the known-at axis). EntryStore.Post stamps it from the store's own
//     clock; a caller-supplied value is only relevant when an entry is
//     constructed directly for a test, never for a real posting, so a
//     caller can never claim an earlier recording than actually happened.
//   - AuthorizedAt is the instant this entry became an authorized fact. It
//     may be before, equal to, or after RecordedAt (an already-decided
//     authorization can be scheduled to take effect later); unset means the
//     entry has never been authorized and is permanently excluded from an
//     authorized balance until a superseding fact says otherwise (out of
//     this ticket's scope; see BAL-006).
type BalanceEntry struct {
	AccountID           string
	DefinitionID        string
	DefinitionVersion   string
	Unit                string
	Currency            string
	Subject             string
	Period              string
	Dimensions          map[string]string
	Kind                EntryKind
	Amount              values.Decimal
	EntryType           string
	SourceTransactionID string
	IdempotencyKey      string
	EffectiveAt         values.Instant
	RecordedAt          values.Instant
	AuthorizedAt        values.Instant
	// SupersedesDigest links a correction entry to the immutable entry it
	// replaces. It is optional for legacy entries and included only when set.
	SupersedesDigest string
}

type Entry = BalanceEntry

type PostRequest struct {
	Entry        BalanceEntry
	ExpectedHead int64
}

type PostReceipt struct {
	Entry  BalanceEntry
	Head   int64
	Digest string
	Replay bool
}

func (e BalanceEntry) copy() BalanceEntry {
	e.Dimensions = mapsCopy(e.Dimensions)
	return e
}
func mapsCopy(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (e BalanceEntry) Validate(def AccumulatorDefinition) error {
	if strings.TrimSpace(e.AccountID) == "" || strings.TrimSpace(e.DefinitionID) == "" || strings.TrimSpace(e.DefinitionVersion) == "" {
		return fmt.Errorf("%w: account and definition identity are required", ErrEntryInvalid)
	}
	if e.DefinitionID != def.ID || e.DefinitionVersion != def.Version {
		return fmt.Errorf("%w: definition revision mismatch", ErrEntryInvalid)
	}
	if e.Unit != def.Unit || e.Currency != def.Currency {
		return fmt.Errorf("%w: unit or currency mismatch", ErrEntryInvalid)
	}
	if e.Subject != def.Subject || e.Period != string(def.Period) {
		return fmt.Errorf("%w: subject or period mismatch", ErrEntryInvalid)
	}
	if e.Kind != Debit && e.Kind != Credit {
		return fmt.Errorf("%w: kind must be DEBIT or CREDIT", ErrEntryInvalid)
	}
	if strings.TrimSpace(e.EntryType) == "" || !contains(def.EntryTypes, e.EntryType) {
		return fmt.Errorf("%w: entry type is not defined", ErrEntryInvalid)
	}
	if strings.TrimSpace(e.SourceTransactionID) == "" || strings.TrimSpace(e.IdempotencyKey) == "" {
		return fmt.Errorf("%w: source transaction and idempotency key are required", ErrEntryInvalid)
	}
	if err := e.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrEntryInvalid, err)
	}
	if e.Amount.Sign() < 0 || e.Amount.IsZero() {
		return fmt.Errorf("%w: amount must be positive", ErrEntryInvalid)
	}
	if len(e.Dimensions) != len(def.Dimensions) {
		return fmt.Errorf("%w: dimensions do not match definition", ErrEntryInvalid)
	}
	for _, d := range def.Dimensions {
		if v, ok := e.Dimensions[d.Name]; !ok || d.Required && strings.TrimSpace(v) == "" {
			return fmt.Errorf("%w: dimension %s is required", ErrEntryInvalid, d.Name)
		}
	}
	return nil
}
func contains(xs []string, value string) bool {
	for _, x := range xs {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

// Canonical returns the deterministic byte encoding of the entry.
func (e BalanceEntry) Canonical() []byte {
	keys := make([]string, 0, len(e.Dimensions))
	for k := range e.Dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := canonicalbytes.New("hcmnext.domains.balance.BalanceEntry", 1).String("account_id", e.AccountID).String("definition_id", e.DefinitionID).String("definition_version", e.DefinitionVersion).String("unit", e.Unit).String("currency", e.Currency).String("subject", e.Subject).String("period", e.Period).String("kind", string(e.Kind)).Value("amount", e.Amount).String("entry_type", e.EntryType).String("source_transaction_id", e.SourceTransactionID).String("idempotency_key", e.IdempotencyKey)
	for _, k := range keys {
		w = w.String("dimension."+k, e.Dimensions[k])
	}
	if e.EffectiveAt.IsSet() {
		w.Value("effective_at", e.EffectiveAt)
	}
	if e.AuthorizedAt.IsSet() {
		w.Value("authorized_at", e.AuthorizedAt)
	}
	if e.SupersedesDigest != "" {
		w.String("supersedes_digest", e.SupersedesDigest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical entry encoding.
func (e BalanceEntry) Digest() string {
	raw := e.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// EntryStore is a concurrency-safe append-only stream per account.
type EntryStore struct {
	mu      sync.RWMutex
	entries map[string][]PostReceipt
	byKey   map[string]PostReceipt
	clock   func() time.Time
}

func NewEntryStore() *EntryStore {
	return NewEntryStoreWithClock(func() time.Time { return time.Now().UTC() })
}

// NewEntryStoreWithClock builds a store whose posted entries stamp
// RecordedAt from clock instead of the wall clock. Tests use it to make a
// BAL-003 known-at calculation deterministic and reproducible.
func NewEntryStoreWithClock(clock func() time.Time) *EntryStore {
	return &EntryStore{entries: make(map[string][]PostReceipt), byKey: make(map[string]PostReceipt), clock: clock}
}
func (s *EntryStore) Post(req PostRequest, def AccumulatorDefinition) (PostReceipt, error) {
	if s == nil {
		return PostReceipt{}, ErrEntryInvalid
	}
	if err := req.Entry.Validate(def); err != nil {
		return PostReceipt{}, err
	}
	if req.ExpectedHead < 0 {
		return PostReceipt{}, fmt.Errorf("%w: expected head cannot be negative", ErrEntryInvalid)
	}
	d := req.Entry.Digest()
	s.mu.Lock()
	defer s.mu.Unlock()
	key := req.Entry.AccountID + "\x00" + req.Entry.IdempotencyKey
	if old, ok := s.byKey[key]; ok {
		if old.Digest != d {
			return PostReceipt{}, fmt.Errorf("%w: %s", ErrIdempotencyConflict, req.Entry.IdempotencyKey)
		}
		old.Replay = true
		return old, nil
	}
	head := int64(len(s.entries[req.Entry.AccountID]))
	if req.ExpectedHead != head {
		return PostReceipt{}, fmt.Errorf("%w: expected %d, actual %d", ErrStaleHead, req.ExpectedHead, head)
	}
	entry := req.Entry.copy()
	clock := s.clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	entry.RecordedAt = values.NewInstant(clock())
	r := PostReceipt{Entry: entry, Head: head + 1, Digest: d}
	s.entries[req.Entry.AccountID] = append(s.entries[req.Entry.AccountID], r)
	s.byKey[key] = r
	return r, nil
}
func (s *EntryStore) Entries(account string) []BalanceEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rs := s.entries[account]
	out := make([]BalanceEntry, len(rs))
	for i, r := range rs {
		out[i] = r.Entry.copy()
	}
	return out
}
func (s *EntryStore) Head(account string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.entries[account]))
}
