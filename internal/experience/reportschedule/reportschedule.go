// Package reportschedule owns scheduling and historical reproduction semantics
// for dashboards. It is deliberately independent of transport and storage.
package reportschedule

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type RegistryEntry struct{ Code, Label, Description string }
type State string

const (
	Scheduled   State = "scheduled"
	Running     State = "running"
	Succeeded   State = "succeeded"
	Failed      State = "failed"
	Paused      State = "paused"
	Exact       State = "exact"
	Recomputed  State = "recomputed"
	Unavailable State = "unavailable"
)

var registry = [...]RegistryEntry{
	{string(Scheduled), "Scheduled", "A dashboard run is eligible to start."},
	{string(Running), "Running", "A dashboard run is executing."},
	{string(Succeeded), "Succeeded", "A dashboard run completed."},
	{string(Failed), "Failed", "A dashboard run failed."},
	{string(Paused), "Paused", "The schedule is retained but not eligible to run."},
	{string(Exact), "Exact", "Historical output matches the retained pinned result."},
	{string(Recomputed), "Recomputed", "Historical output was recomputed from retained pins."},
	{string(Unavailable), "Unavailable", "Required historical inputs are unavailable."},
}

func RegistryMatrix() []RegistryEntry { return append([]RegistryEntry(nil), registry[:]...) }
func Lookup(code string) (RegistryEntry, bool) {
	for _, e := range registry {
		if e.Code == code {
			return e, true
		}
	}
	return RegistryEntry{}, false
}

type Pin struct{ ID, Version, Digest string }
type Definition struct{ ID, Version, Digest string }
type Data struct{ ID, Version, Digest string }
type Control struct{ ID, Version, Digest string }
type Locale struct{ ID, Version, Digest string }

type Schedule struct {
	ID, TenantID string
	Definition   Definition
	Data         Data
	Control      Control
	Locale       Locale
	Every        time.Duration
	Next         time.Time
	State        State
}
type Authorization struct {
	PrincipalID, TenantID, Purpose string
	ValidUntil                     time.Time
}
type DeliveryIntent struct {
	ID, TenantID, Destination, AttachmentRef string
	Authorized                               bool
}
type Run struct {
	ID, ScheduleID, TenantID string
	State                    State
	Definition               Definition
	Data                     Data
	Control                  Control
	Locale                   Locale
	EvidenceDigest           string
	Delivery                 DeliveryIntent
	StartedAt, FinishedAt    time.Time
}
type Reproduction struct {
	State                          State
	OriginalID, ResultDigest, Note string
}

var (
	ErrInvalid      = errors.New("reportschedule: invalid schedule or pin")
	ErrUnauthorized = errors.New("reportschedule: authorization is expired or invalid")
	ErrNotFound     = errors.New("reportschedule: not found")
	ErrInactive     = errors.New("reportschedule: schedule is inactive")
)

func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func validSchedule(s Schedule) bool {
	return s.ID != "" && s.TenantID != "" && s.Every > 0 && s.State != Paused && s.State != Unavailable && s.Definition.ID != "" && s.Definition.Version != "" && s.Definition.Digest != "" && s.Data.ID != "" && s.Data.Version != "" && s.Data.Digest != "" && s.Control.ID != "" && s.Control.Version != "" && s.Control.Digest != "" && s.Locale.ID != "" && s.Locale.Version != "" && s.Locale.Digest != ""
}
func authorize(a Authorization, tenant string, now time.Time) bool {
	return a.PrincipalID != "" && a.TenantID == tenant && a.Purpose != "" && (a.ValidUntil.IsZero() || now.Before(a.ValidUntil))
}

type Store struct {
	mu        sync.RWMutex
	schedules map[string]Schedule
	runs      map[string][]Run
	max       int
}
type Scheduler = Store

func NewStore(max int) *Store {
	if max < 1 {
		max = 20
	}
	return &Store{schedules: map[string]Schedule{}, runs: map[string][]Run{}, max: max}
}
func NewScheduler(max int) *Scheduler { return NewStore(max) }
func (s *Store) Create(v Schedule) (Schedule, error) {
	if v.State == "" {
		v.State = Scheduled
	}
	if !validSchedule(v) {
		return Schedule{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules[v.ID] = v
	return v, nil
}
func (s *Store) Schedule(v Schedule) (Schedule, error) { return s.Create(v) }
func (s *Store) Pause(id, tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.schedules[id]
	if !ok || v.TenantID != tenant {
		return ErrNotFound
	}
	v.State = Paused
	s.schedules[id] = v
	return nil
}
func (s *Store) Run(id string, a Authorization, now time.Time, evidence, destination, attachment string) (Run, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if !authorize(a, a.TenantID, now) {
		return Run{}, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.schedules[id]
	if !ok || v.TenantID != a.TenantID {
		return Run{}, ErrNotFound
	}
	if v.State == Paused {
		return Run{}, ErrInactive
	}
	r := Run{ID: id + "-" + now.UTC().Format("20060102T150405.000000000Z"), ScheduleID: id, TenantID: a.TenantID, State: Succeeded, Definition: v.Definition, Data: v.Data, Control: v.Control, Locale: v.Locale, EvidenceDigest: digest(evidence), StartedAt: now, FinishedAt: now, Delivery: DeliveryIntent{ID: id + "-delivery", TenantID: a.TenantID, Destination: destination, AttachmentRef: attachment, Authorized: destination != "" && attachment != ""}}
	h := append(s.runs[id], r)
	if len(h) > s.max {
		h = h[len(h)-s.max:]
	}
	s.runs[id] = h
	return r, nil
}
func (s *Store) History(id, tenant string, a Authorization) ([]Run, error) {
	now := time.Now()
	if !authorize(a, tenant, now) {
		return nil, ErrUnauthorized
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.schedules[id]
	if !ok || v.TenantID != tenant {
		return nil, ErrNotFound
	}
	out := append([]Run(nil), s.runs[id]...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].FinishedAt.Before(out[j].FinishedAt) })
	return out, nil
}
func Reproduce(original Run, current Definition, data Data, control Control, locale Locale, resultDigest string) Reproduction {
	if original.ID == "" || original.State != Succeeded {
		return Reproduction{State: Unavailable, OriginalID: original.ID, Note: "original result is unavailable"}
	}
	if original.Definition == current && original.Data == data && original.Control == control && original.Locale == locale && resultDigest == original.EvidenceDigest {
		return Reproduction{State: Exact, OriginalID: original.ID, ResultDigest: resultDigest}
	}
	if current.ID == original.Definition.ID && data.ID == original.Data.ID && control.ID == original.Control.ID && locale.ID == original.Locale.ID && strings.TrimSpace(resultDigest) != "" {
		return Reproduction{State: Recomputed, OriginalID: original.ID, ResultDigest: resultDigest, Note: "recomputed output differs from the retained result"}
	}
	return Reproduction{State: Unavailable, OriginalID: original.ID, Note: "one or more pinned inputs are unavailable"}
}
