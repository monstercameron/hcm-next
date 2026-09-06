// Package lifecycle owns telemetry query, retention, residency, hold and
// tenant-exit policy. It stores only opaque operational identifiers and
// content digests, never customer names or signal payloads.
package lifecycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

func Explain() string {
	return "tenant-scoped telemetry query retention residency hold and exit policy"
}

type Kind string

const (
	KindMetrics   Kind = "METRICS"
	KindLogs      Kind = "LOGS"
	KindTraces    Kind = "TRACES"
	KindExemplars Kind = "EXEMPLARS"
)

type CopyRole string

const (
	RolePrimary CopyRole = "PRIMARY"
	RoleIndex   CopyRole = "INDEX"
	RoleArchive CopyRole = "ARCHIVE"
	RoleBackup  CopyRole = "BACKUP"
)

type Policy struct {
	Version        int
	AllowedPurpose map[string]bool
	AllowedRegions map[string]bool
	Retention      map[Kind]time.Duration
}

type Copy struct {
	ID            string
	TenantToken   string
	Kind          Kind
	Role          CopyRole
	Backend       string
	Region        string
	Digest        string
	RetainUntil   time.Time
	HoldReason    string
	DeleteCapable bool
	Disposition   string
	LastVerified  time.Time
}

type Query struct {
	TenantToken string
	Purpose     string
	Kind        Kind
	Region      string
	AsOf        time.Time
}

type ExitItem struct {
	CopyID  string
	Role    CopyRole
	Outcome string
	Reason  string
}

type ExitReceipt struct {
	TenantToken string
	Status      string
	Items       []ExitItem
	Evidence    string
}

type Snapshot struct {
	Copies          []Copy
	CompleteTenants []string
}

type Store struct {
	mu       sync.RWMutex
	policy   Policy
	copies   map[string]Copy
	complete map[string]bool
}

var (
	ErrInvalidPolicy = errors.New("telemetry lifecycle: invalid policy")
	ErrInvalidCopy   = errors.New("telemetry lifecycle: invalid copy")
	ErrUnauthorized  = errors.New("telemetry lifecycle: query purpose is not authorized")
	ErrDuplicate     = errors.New("telemetry lifecycle: duplicate copy")
)

func New(policy Policy) (*Store, error) {
	if policy.Version <= 0 || len(policy.AllowedPurpose) == 0 || len(policy.AllowedRegions) == 0 || len(policy.Retention) == 0 {
		return nil, ErrInvalidPolicy
	}
	for kind, retention := range policy.Retention {
		if !validKind(kind) || retention <= 0 {
			return nil, ErrInvalidPolicy
		}
	}
	return &Store{policy: policy, copies: make(map[string]Copy), complete: make(map[string]bool)}, nil
}

func DefaultPolicy() Policy {
	return Policy{Version: 1, AllowedPurpose: map[string]bool{"incident": true, "slo": true}, AllowedRegions: map[string]bool{"us-east": true}, Retention: map[Kind]time.Duration{KindMetrics: 7 * 24 * time.Hour, KindLogs: 30 * 24 * time.Hour, KindTraces: 14 * 24 * time.Hour, KindExemplars: 14 * 24 * time.Hour}}
}

func (s *Store) Register(copy Copy) error {
	if strings.TrimSpace(copy.ID) == "" || invalidToken(copy.TenantToken) || !validKind(copy.Kind) || !validRole(copy.Role) || strings.TrimSpace(copy.Backend) == "" || strings.TrimSpace(copy.Region) == "" || strings.TrimSpace(copy.Digest) == "" || copy.RetainUntil.IsZero() || copy.LastVerified.IsZero() || !copy.DeleteCapable && copy.Disposition == "" {
		return ErrInvalidCopy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.policy.AllowedRegions[copy.Region] {
		return fmt.Errorf("%w: region %s", ErrInvalidPolicy, copy.Region)
	}
	if _, exists := s.copies[copy.ID]; exists {
		return ErrDuplicate
	}
	if copy.Disposition == "" {
		copy.Disposition = "ACTIVE"
	}
	s.copies[copy.ID] = copy
	return nil
}

func (s *Store) MarkInventoryComplete(tenantToken string) {
	if invalidToken(tenantToken) {
		return
	}
	s.mu.Lock()
	s.complete[tenantToken] = true
	s.mu.Unlock()
}

// Query requires a purpose, exact tenant token and approved residency. A
// non-matching tenant is indistinguishable from no results.
func (s *Store) Query(query Query) ([]Copy, error) {
	if invalidToken(query.TenantToken) || strings.TrimSpace(query.Purpose) == "" || !s.policy.AllowedPurpose[query.Purpose] {
		return nil, ErrUnauthorized
	}
	if !s.policy.AllowedRegions[query.Region] {
		return nil, ErrUnauthorized
	}
	now := query.AsOf
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Copy, 0)
	for _, copy := range s.copies {
		if copy.TenantToken == query.TenantToken && copy.Kind == query.Kind && copy.Region == query.Region && copy.Disposition != "DELETED" && now.Before(copy.RetainUntil) {
			result = append(result, copy)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *Store) PlaceHold(tenantToken, copyID, reason string) error {
	if invalidToken(tenantToken) || strings.TrimSpace(copyID) == "" || strings.TrimSpace(reason) == "" {
		return ErrInvalidCopy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy, ok := s.copies[copyID]
	if !ok || copy.TenantToken != tenantToken {
		return ErrInvalidCopy
	}
	copy.HoldReason = reason
	copy.Disposition = "HELD"
	s.copies[copyID] = copy
	return nil
}

// Exit dispositions every known copy for one tenant. Holds, active retention,
// unsupported deletion and an incomplete inventory remain explicit blockers.
func (s *Store) Exit(tenantToken string, now time.Time) ExitReceipt {
	receipt := ExitReceipt{TenantToken: tenantToken, Status: "CERTIFIABLE", Evidence: "telemetry-exit-v1"}
	if invalidToken(tenantToken) {
		receipt.Status = "BLOCKED"
		receipt.Items = []ExitItem{{Outcome: "EXCEPTION", Reason: "invalid tenant scope"}}
		return receipt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.complete[tenantToken] {
		receipt.Status = "BLOCKED"
		receipt.Items = append(receipt.Items, ExitItem{Outcome: "EXCEPTION", Reason: "inventory incomplete"})
	}
	ids := make([]string, 0)
	for id, copy := range s.copies {
		if copy.TenantToken == tenantToken {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		copy := s.copies[id]
		item := ExitItem{CopyID: copy.ID, Role: copy.Role}
		switch {
		case copy.HoldReason != "":
			item.Outcome, item.Reason = "EXCEPTION", "legal hold"
		case now.Before(copy.RetainUntil):
			item.Outcome, item.Reason = "EXCEPTION", "retention active"
		case !copy.DeleteCapable:
			item.Outcome, item.Reason = "EXCEPTION", "deletion unsupported"
		default:
			copy.Disposition = "DELETED"
			s.copies[id] = copy
			item.Outcome, item.Reason = "DESTROYED", "disposition applied"
		}
		if item.Outcome != "DESTROYED" {
			receipt.Status = "BLOCKED"
		}
		receipt.Items = append(receipt.Items, item)
	}
	return receipt
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := Snapshot{Copies: make([]Copy, 0, len(s.copies))}
	for _, copy := range s.copies {
		result.Copies = append(result.Copies, copy)
	}
	for tenant := range s.complete {
		result.CompleteTenants = append(result.CompleteTenants, tenant)
	}
	sort.Slice(result.Copies, func(i, j int) bool { return result.Copies[i].ID < result.Copies[j].ID })
	sort.Strings(result.CompleteTenants)
	return result
}

// Restore imports disposition state exactly, including DELETED copies, so a
// restore cannot resurrect data that tenant exit already removed.
func (s *Store) Restore(snapshot Snapshot) error {
	for _, copy := range snapshot.Copies {
		if invalidToken(copy.TenantToken) || !validKind(copy.Kind) || !validRole(copy.Role) || !s.policy.AllowedRegions[copy.Region] {
			return ErrInvalidCopy
		}
	}
	s.mu.Lock()
	s.copies = make(map[string]Copy, len(snapshot.Copies))
	for _, copy := range snapshot.Copies {
		s.copies[copy.ID] = copy
	}
	s.complete = make(map[string]bool, len(snapshot.CompleteTenants))
	for _, tenant := range snapshot.CompleteTenants {
		s.complete[tenant] = true
	}
	s.mu.Unlock()
	return nil
}

func validKind(kind Kind) bool {
	return kind == KindMetrics || kind == KindLogs || kind == KindTraces || kind == KindExemplars
}
func validRole(role CopyRole) bool {
	return role == RolePrimary || role == RoleIndex || role == RoleArchive || role == RoleBackup
}
func invalidToken(token string) bool {
	return strings.TrimSpace(token) == "" || strings.ContainsAny(token, " \t\r\n") || len(token) > 128
}

func (r ExitReceipt) Explain() string {
	return fmt.Sprintf("telemetry exit %s copies=%d", r.Status, len(r.Items))
}
