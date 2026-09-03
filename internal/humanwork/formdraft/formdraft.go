// Package formdraft provides encrypted, resumable human form drafts.
package formdraft

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("formdraft: draft not found")
	ErrConflict = errors.New("formdraft: compare-and-swap conflict")
	ErrExpired  = errors.New("formdraft: draft expired")
	ErrInvalid  = errors.New("formdraft: invalid draft")
)

// Draft is the decrypted, caller-visible representation of a draft.
type Draft struct {
	ID        string                     `json:"id"`
	Form      string                     `json:"form"`
	Subject   string                     `json:"subject"`
	Version   uint64                     `json:"version"`
	ExpiresAt time.Time                  `json:"expires_at"`
	Data      map[string]json.RawMessage `json:"data"`
}

// PutRequest describes a draft update. Only fields named in Requested are
// persisted; all other values are deliberately ignored.
type PutRequest struct {
	ID        string
	Form      string
	Subject   string
	Requested []string
	Data      map[string]json.RawMessage
	TTL       time.Duration
}

type envelope struct {
	Form, Subject string
	Version       uint64
	ExpiresAt     time.Time
	Data          map[string]json.RawMessage
}
type record struct {
	nonce, ciphertext []byte
	version           uint64
	expiresAt         time.Time
}

// Store is a concurrency-safe encrypted draft store. It has no external
// effects: records exist only for the lifetime of this value.
type Store struct {
	mu      sync.RWMutex
	aead    cipher.AEAD
	drafts  map[string]record
	effects uint64
	now     func() time.Time
}

// NewStore creates a store. Keys of any length are deterministically expanded
// to an AES-256 key; callers should supply a stable, secret key.
func NewStore(key []byte) (*Store, error) {
	if len(key) == 0 {
		return nil, ErrInvalid
	}
	h := sha256.Sum256(key)
	b, err := aes.NewCipher(h[:])
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &Store{aead: a, drafts: make(map[string]record), now: time.Now}, nil
}

// Effects reports successful persistent mutations. Failed validation, expiry,
// CAS conflicts, and clear misses do not increment it.
func (s *Store) Effects() uint64 { s.mu.RLock(); defer s.mu.RUnlock(); return s.effects }

func requestedSet(fields []string) map[string]bool {
	m := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f != "" {
			m[f] = true
		}
	}
	return m
}
func cloneData(in map[string]json.RawMessage, allowed map[string]bool) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage)
	for k, v := range in {
		if allowed[k] {
			out[k] = append(json.RawMessage(nil), v...)
		}
	}
	return out
}

// Put creates or updates a draft. expectedVersion is zero for creation; for
// updates it must equal the currently stored version.
func (s *Store) Put(req PutRequest, expectedVersion uint64) (Draft, error) {
	if req.ID == "" || req.Form == "" || req.TTL <= 0 || req.TTL > 365*24*time.Hour {
		return Draft{}, ErrInvalid
	}
	allowed := requestedSet(req.Requested)
	data := cloneData(req.Data, allowed)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	old, exists := s.drafts[req.ID]
	if exists && !old.expiresAt.After(now) {
		delete(s.drafts, req.ID)
		exists = false
	}
	if expectedVersion == 0 {
		if exists {
			return Draft{}, ErrConflict
		}
	} else if !exists || old.version != expectedVersion {
		return Draft{}, ErrConflict
	}
	version := uint64(1)
	if exists {
		version = old.version + 1
	}
	exp := now.Add(req.TTL)
	e := envelope{Form: req.Form, Subject: req.Subject, Version: version, ExpiresAt: exp, Data: data}
	plain, err := json.Marshal(e)
	if err != nil {
		return Draft{}, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return Draft{}, err
	}
	ct := s.aead.Seal(nil, nonce, plain, nil)
	s.drafts[req.ID] = record{nonce: nonce, ciphertext: ct, version: version, expiresAt: exp}
	s.effects++
	return Draft{ID: req.ID, Form: e.Form, Subject: e.Subject, Version: version, ExpiresAt: exp, Data: cloneData(data, mapAll(data))}, nil
}

func mapAll(m map[string]json.RawMessage) map[string]bool {
	x := make(map[string]bool, len(m))
	for k := range m {
		x[k] = true
	}
	return x
}
func (s *Store) decode(id string, r record, now time.Time) (Draft, error) {
	p, err := s.aead.Open(nil, r.nonce, r.ciphertext, nil)
	if err != nil {
		return Draft{}, ErrInvalid
	}
	var e envelope
	if err = json.Unmarshal(p, &e); err != nil {
		return Draft{}, ErrInvalid
	}
	// Authenticate and decode before evaluating expiry. Otherwise a foreign
	// key (or tampered ciphertext) could be misreported as an expired draft,
	// leaking storage state and making resume authentication ambiguous.
	if !r.expiresAt.After(now) {
		return Draft{}, ErrExpired
	}
	return Draft{ID: id, Form: e.Form, Subject: e.Subject, Version: e.Version, ExpiresAt: e.ExpiresAt, Data: e.Data}, nil
}

// Get resumes a draft. Expired records are cleared and are never returned.
func (s *Store) Get(id string) (Draft, error) {
	if id == "" {
		return Draft{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.drafts[id]
	if !ok {
		return Draft{}, ErrNotFound
	}
	d, err := s.decode(id, r, s.now())
	if err == ErrExpired {
		delete(s.drafts, id)
	}
	return d, err
}

// Clear removes a draft. It returns ErrNotFound when no record exists and has
// no effect in that case.
func (s *Store) Clear(id string) error {
	if id == "" {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.drafts[id]; !ok {
		return ErrNotFound
	}
	delete(s.drafts, id)
	s.effects++
	return nil
}

// Save is an alias for Put, retained as the persistence-oriented spelling.
func (s *Store) Save(req PutRequest, expectedVersion uint64) (Draft, error) {
	return s.Put(req, expectedVersion)
}

// Resume is an alias for Get.
func (s *Store) Resume(id string) (Draft, error) { return s.Get(id) }

// Delete is an alias for Clear.
func (s *Store) Delete(id string) error { return s.Clear(id) }

// IDs returns a stable snapshot of currently live draft identifiers.
func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := make([]string, 0, len(s.drafts))
	for id, r := range s.drafts {
		if r.expiresAt.After(now) {
			out = append(out, id)
		} else {
			delete(s.drafts, id)
		}
	}
	sort.Strings(out)
	return out
}

// SetClock is intended for deterministic tests; it does not alter stored data.
func (s *Store) SetClock(now func() time.Time) error {
	if now == nil {
		return fmt.Errorf("%w: nil clock", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
	return nil
}
