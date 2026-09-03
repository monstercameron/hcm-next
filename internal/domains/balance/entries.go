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

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
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

func (e BalanceEntry) canonical() ([]byte, error) {
	keys := make([]string, 0, len(e.Dimensions))
	for k := range e.Dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := canonicalbytes.New("hcmnext.domains.balance.BalanceEntry", 1).String("account_id", e.AccountID).String("definition_id", e.DefinitionID).String("definition_version", e.DefinitionVersion).String("unit", e.Unit).String("currency", e.Currency).String("subject", e.Subject).String("period", e.Period).String("kind", string(e.Kind)).Value("amount", e.Amount).String("entry_type", e.EntryType).String("source_transaction_id", e.SourceTransactionID).String("idempotency_key", e.IdempotencyKey)
	for _, k := range keys {
		w = w.String("dimension."+k, e.Dimensions[k])
	}
	return w.Bytes()
}
func (e BalanceEntry) Digest() string {
	b, err := e.canonical()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// EntryStore is a concurrency-safe append-only stream per account.
type EntryStore struct {
	mu      sync.RWMutex
	entries map[string][]PostReceipt
	byKey   map[string]PostReceipt
}

func NewEntryStore() *EntryStore {
	return &EntryStore{entries: make(map[string][]PostReceipt), byKey: make(map[string]PostReceipt)}
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
	r := PostReceipt{Entry: req.Entry.copy(), Head: head + 1, Digest: d}
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
