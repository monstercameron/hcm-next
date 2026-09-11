// The leave process anchor persists one idempotent capability invocation and
// nothing else. It binds the validated RequestLeave process (LEAVE-001),
// stores the CHANGE_REQUEST intent instance with its child bindings, the
// requested LeaveRequest revision and the LeaveRequested chronology entry,
// and performs no workforce mutation and no external effect.
package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// Anchor state and event names: the closed anchor vocabulary.
const (
	AnchorRequested = "REQUESTED"
	AnchorEvent     = "LeaveRequested"
)

// AnchorCategory names one durable write category. Only the first three are
// anchor writes; the rest exist so a regression that mutates workforce state
// or dispatches an effect through this store shows up in the write log.
type AnchorCategory string

const (
	AnchorWriteIntent     AnchorCategory = "intent"
	AnchorWriteRequest    AnchorCategory = "request"
	AnchorWriteChronology AnchorCategory = "chronology"
	AnchorWriteEmployment AnchorCategory = "employment"
	AnchorWriteAvailable  AnchorCategory = "availability"
	AnchorWriteSchedule   AnchorCategory = "schedule"
	AnchorWriteBalance    AnchorCategory = "balance"
	AnchorWriteEffect     AnchorCategory = "effect"
)

// anchorNamespace derives anchor identities from the request digest so a
// repeated invocation names the same intent, request and event.
var anchorNamespace = uuid.MustParse("7d9f3b2e-1c4a-4e8f-9b6d-2f5a8c1e3a47")

var (
	ErrInvalidAnchor = errors.New("leave: invalid process anchor")
	ErrStaleAnchor   = errors.New("leave: anchored request is superseded")
)

// AnchorRecord is the frozen process anchor: the persisted intent instance
// identity, the requested LeaveRequest revision and the LeaveRequested
// chronology entry with its provenance.
type AnchorRecord struct {
	IntentID          string
	RequestID         string
	EventID           string
	Event             string
	Revision          int
	Family            intent.Family
	DefinitionType    string
	DefinitionVersion int
	Children          []ChildKind
	CanonicalDigest   string
	State             string
	Tenant            string
	Principal         string
	OrgScope          string
	WorkerID          string
	Interval          string
	Evidence          []string
	Digest            string
}

// AnchorWrite is one categorized durable write the anchor performed.
type AnchorWrite struct {
	Category AnchorCategory
	Ref      string
}

// AnchorStore is the mutex-guarded anchor repository. It holds at most one
// record per canonical request digest: a duplicate invocation replays the
// stored record instead of persisting a second intent or request.
type AnchorStore struct {
	mu      sync.Mutex
	records map[string]AnchorRecord
	clients map[string]string
	writes  []AnchorWrite
}

// NewAnchorStore returns an empty anchor repository.
func NewAnchorStore() *AnchorStore {
	return &AnchorStore{records: make(map[string]AnchorRecord), clients: make(map[string]string)}
}

// derive mints one stable identity for a purpose and request digest.
func derive(purpose, digest string) string {
	return uuid.NewSHA1(anchorNamespace, []byte(purpose+"|"+digest)).String()
}

// anchorDigest binds every material anchor field. Any mutation of a stored
// record breaks verification.
func anchorDigest(r AnchorRecord) string {
	children := make([]string, 0, len(r.Children))
	for _, c := range r.Children {
		children = append(children, string(c))
	}
	evidence := append([]string(nil), r.Evidence...)
	sort.Strings(evidence)
	parts := []string{"leave-anchor", r.IntentID, r.RequestID, r.EventID, r.Event, fmt.Sprint(r.Revision), r.Family.String(), r.DefinitionType, fmt.Sprint(r.DefinitionVersion), strings.Join(children, ","), r.CanonicalDigest, r.State, r.Tenant, r.Principal, r.OrgScope, r.WorkerID, r.Interval, strings.Join(evidence, ",")}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the anchor digest and rejects a tampered record.
func (r AnchorRecord) Verify() error {
	if r.Digest == "" || r.Digest != anchorDigest(r) {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidAnchor)
	}
	return nil
}

// validateProcess refuses anything but a well-formed bound RequestLeave
// process: the definition identity, the CHANGE_REQUEST family, the full
// child binding set and a trusted context.
func validateProcess(p ProcessRequest) error {
	switch {
	case p.DefinitionType != RequestLeaveIntentType:
		return fmt.Errorf("%w: definition type", ErrInvalidAnchor)
	case p.DefinitionVersion != RequestLeaveIntentVersion:
		return fmt.Errorf("%w: definition version", ErrInvalidAnchor)
	case p.Family != intent.FamilyChangeRequest:
		return fmt.Errorf("%w: family is not CHANGE_REQUEST", ErrInvalidAnchor)
	case strings.TrimSpace(p.CanonicalDigest) == "":
		return fmt.Errorf("%w: canonical digest", ErrInvalidAnchor)
	case len(p.ChildKinds) != len(childKinds):
		return fmt.Errorf("%w: child bindings", ErrInvalidAnchor)
	}
	seen := make(map[ChildKind]bool, len(childKinds))
	for _, c := range p.ChildKinds {
		seen[c] = true
	}
	for _, c := range childKinds {
		if !seen[c] {
			return fmt.Errorf("%w: missing child %s", ErrInvalidAnchor, c)
		}
	}
	if err := p.Request.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAnchor, err)
	}
	if err := p.Context.TenantID.Validate(); err != nil || strings.TrimSpace(p.Context.OrganizationScopeID) == "" || strings.TrimSpace(p.Context.PrincipalID) == "" {
		return fmt.Errorf("%w: trusted context", ErrInvalidAnchor)
	}
	return nil
}

// Invoke persists the process anchor for one bound RequestLeave and returns
// the frozen record. A repeated invocation with the same canonical digest
// replays the stored record: created is false and no further write lands.
// Invoke performs exactly three durable writes — intent, request, chronology —
// and no workforce mutation and no external effect.
func (s *AnchorStore) Invoke(p ProcessRequest) (record AnchorRecord, created bool, err error) {
	if err := validateProcess(p); err != nil {
		return AnchorRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.records[p.CanonicalDigest]; ok {
		return existing.clone(), false, nil
	}
	// A reused client request id with different content is a conflicting
	// invocation, not a replay: it refuses instead of forking the anchor.
	if digest, ok := s.clients[p.Request.ClientRequestID]; ok && digest != p.CanonicalDigest {
		return AnchorRecord{}, false, fmt.Errorf("%w: client request %s", ErrStaleAnchor, p.Request.ClientRequestID)
	}
	s.clients[p.Request.ClientRequestID] = p.CanonicalDigest
	evidence := append([]string(nil), p.Request.EvidenceRefs...)
	record = AnchorRecord{
		IntentID:          derive("intent", p.CanonicalDigest),
		RequestID:         derive("request", p.CanonicalDigest),
		EventID:           derive("event", p.CanonicalDigest),
		Event:             AnchorEvent,
		Revision:          1,
		Family:            intent.FamilyChangeRequest,
		DefinitionType:    p.DefinitionType,
		DefinitionVersion: p.DefinitionVersion,
		Children:          append([]ChildKind(nil), p.ChildKinds...),
		CanonicalDigest:   p.CanonicalDigest,
		State:             AnchorRequested,
		Tenant:            string(p.Context.TenantID),
		Principal:         p.Context.PrincipalID,
		OrgScope:          p.Context.OrganizationScopeID,
		WorkerID:          p.Request.WorkerID,
		Interval:          string(p.Request.Interval.Canonical()),
		Evidence:          evidence,
	}
	record.Digest = anchorDigest(record)
	// The store keeps its own copies: mutating a returned record never
	// changes the stored anchor.
	stored := record.clone()
	s.records[p.CanonicalDigest] = stored
	s.writes = append(s.writes,
		AnchorWrite{Category: AnchorWriteIntent, Ref: record.IntentID},
		AnchorWrite{Category: AnchorWriteRequest, Ref: record.RequestID},
		AnchorWrite{Category: AnchorWriteChronology, Ref: record.EventID},
	)
	return record, true, nil
}

// clone copies the record's aliased slices so a returned record never shares
// backing arrays with the stored anchor.
func (r AnchorRecord) clone() AnchorRecord {
	r.Children = append([]ChildKind(nil), r.Children...)
	r.Evidence = append([]string(nil), r.Evidence...)
	return r
}

// Get replays the anchor stored for a canonical digest, if any. The returned
// record is a copy: mutating it never changes the stored anchor.
func (s *AnchorStore) Get(digest string) (AnchorRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[digest]
	if !ok {
		return AnchorRecord{}, false
	}
	return record.clone(), true
}

// Writes replays the categorized write log in order.
func (s *AnchorStore) Writes() []AnchorWrite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AnchorWrite(nil), s.writes...)
}

// CountByCategory reports how many durable writes landed per category.
func (s *AnchorStore) CountByCategory() map[AnchorCategory]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[AnchorCategory]int)
	for _, w := range s.writes {
		counts[w.Category]++
	}
	return counts
}
