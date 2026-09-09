package flow

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type Locator struct {
	Token     string
	FlowID    string
	StageID   string
	Owner     string
	ExpiresAt time.Time
	CreatedAt time.Time
	Version   int
}

type Draft struct {
	FlowID    string
	Owner     string
	StageID   string
	Version   int
	Data      map[string]string
	Masked    map[string]string
	UpdatedAt time.Time
}

type ResumeGov interface {
	Authorized(principal, flowID string, at time.Time) bool
}

type ResumeResult struct {
	FlowID           string
	StageID          string
	MaskedState      map[string]string
	CurrentVersion   int
	Stale            bool
	Conflict         bool
	RequiresReauth   bool
	SafeAlternatives []string
	Idempotent       bool
	LocatorExpired   bool
}

var (
	ErrLocatorNotFound = fmt.Errorf("flow: locator not found")
	ErrLocatorExpired  = fmt.Errorf("flow: locator expired")
	ErrNotAuthorized   = fmt.Errorf("flow: not authorized")
	ErrConflict        = fmt.Errorf("flow: version conflict")
	ErrProhibitedField = fmt.Errorf("flow: prohibited field unprotected")
)

var prohibitedFields = map[string]bool{
	"ssn":          true,
	"bank_account": true,
	"password":     true,
	"secret":       true,
}

type Store struct {
	mu       sync.Mutex
	locators map[string]Locator
	drafts   map[string]*Draft
	idem     map[string]ResumeResult
	now      func() time.Time
	tokenEnc *base64.Encoding
}

func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		locators: make(map[string]Locator),
		drafts:   make(map[string]*Draft),
		idem:     make(map[string]ResumeResult),
		now:      now,
		tokenEnc: base64.RawURLEncoding,
	}
}

func (s *Store) CreateLocator(flowID, stageID, owner string, ttl time.Duration) (Locator, error) {
	if flowID == "" || owner == "" {
		return Locator{}, fmt.Errorf("flow: flowID or owner empty")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return Locator{}, err
	}
	token := s.tokenEnc.EncodeToString(buf)
	now := s.now().UTC()
	loc := Locator{Token: token, FlowID: flowID, StageID: stageID, Owner: owner, CreatedAt: now, ExpiresAt: now.Add(ttl), Version: 1}
	s.mu.Lock()
	s.locators[token] = loc
	s.mu.Unlock()
	return loc, nil
}

func (s *Store) SaveDraft(flowID, owner, stageID string, expectedVersion int, data map[string]string, at time.Time) (*Draft, error) {
	for k := range data {
		if prohibitedFields[k] {
			return nil, ErrProhibitedField
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.drafts[flowID]
	if !ok {
		if expectedVersion != 0 && expectedVersion != 1 {
			return nil, ErrConflict
		}
		m := maskData(data)
		d := &Draft{FlowID: flowID, Owner: owner, StageID: stageID, Version: 1, Data: cloneMap(data), Masked: m, UpdatedAt: at}
		s.drafts[flowID] = d
		return cloneDraft(d), nil
	}
	if existing.Version != expectedVersion {
		return nil, ErrConflict
	}
	next := existing.Version + 1
	m := maskData(data)
	d := &Draft{FlowID: flowID, Owner: owner, StageID: stageID, Version: next, Data: cloneMap(data), Masked: m, UpdatedAt: at}
	s.drafts[flowID] = d
	return cloneDraft(d), nil
}

func (s *Store) GetDraft(flowID string) (*Draft, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.drafts[flowID]
	if !ok {
		return nil, false
	}
	return cloneDraft(d), true
}

func (s *Store) Resume(token string, principal *trust.Principal, at time.Time, gov ResumeGov) (ResumeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idemKey := token + ":" + principalFingerprint(principal) + ":" + at.Truncate(time.Second).UTC().Format(time.RFC3339)
	if cached, ok := s.idem[idemKey]; ok {
		cached.Idempotent = true
		return cached, nil
	}
	loc, ok := s.locators[token]
	if !ok {
		return ResumeResult{}, ErrLocatorNotFound
	}
	if at.After(loc.ExpiresAt) {
		res := ResumeResult{FlowID: loc.FlowID, StageID: loc.StageID, MaskedState: map[string]string{}, LocatorExpired: true, RequiresReauth: true, SafeAlternatives: []string{"reauth", "start_new"}}
		s.idem[idemKey] = res
		return res, nil
	}
	if principal == nil {
		return ResumeResult{}, ErrNotAuthorized
	}
	sub := principal.Subject()
	if gov != nil && !gov.Authorized(sub, loc.FlowID, at) {
		return ResumeResult{}, ErrNotAuthorized
	}
	draft, hasDraft := s.drafts[loc.FlowID]
	var masked map[string]string
	ver := loc.Version
	stale := false
	if hasDraft {
		masked = cloneMap(draft.Masked)
		ver = draft.Version
		if draft.Version != loc.Version {
			stale = true
		}
	} else {
		masked = map[string]string{}
	}
	alternatives := []string{"continue", "refresh"}
	if stale {
		alternatives = []string{"refresh", "replan"}
	}
	res := ResumeResult{FlowID: loc.FlowID, StageID: loc.StageID, MaskedState: masked, CurrentVersion: ver, Stale: stale, SafeAlternatives: alternatives}
	s.idem[idemKey] = res
	return res, nil
}

func (s *Store) ClearExpired(at time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, loc := range s.locators {
		if at.After(loc.ExpiresAt) {
			delete(s.locators, k)
			n++
		}
	}
	return n
}

func maskData(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if prohibitedFields[k] {
			out[k] = "***"
		} else if k == "salary" || k == "compensation" {
			if len(v) > 2 {
				out[k] = "***"
			} else {
				out[k] = v
			}
		} else {
			out[k] = v
		}
	}
	return out
}

func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneDraft(d *Draft) *Draft {
	if d == nil {
		return nil
	}
	return &Draft{FlowID: d.FlowID, Owner: d.Owner, StageID: d.StageID, Version: d.Version, Data: cloneMap(d.Data), Masked: cloneMap(d.Masked), UpdatedAt: d.UpdatedAt}
}

func principalFingerprint(p *trust.Principal) string {
	if p == nil {
		return ""
	}
	h := sha256.Sum256([]byte(p.Fingerprint()))
	return hex.EncodeToString(h[:8])
}
