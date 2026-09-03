// Package hrcase owns authoritative HR case meaning and lifecycle. Workflows
// may coordinate a case, but do not own its state or disposition.
package hrcase

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CaseState is deliberately closed: adding a state is a versioned domain
// change, not an arbitrary workflow status.
type CaseState string

const (
	Draft    CaseState = "DRAFT"
	Open     CaseState = "OPEN"
	Waiting  CaseState = "WAITING"
	Paused   CaseState = "PAUSED"
	Resolved CaseState = "RESOLVED"
	Closed   CaseState = "CLOSED"
	Reopened CaseState = "REOPENED"
	Appealed CaseState = "APPEALED"
)

// Long names are exported as aliases for callers that prefer explicit names.
const (
	StateDraft    = Draft
	StateOpen     = Open
	StateWaiting  = Waiting
	StatePaused   = Paused
	StateResolved = Resolved
	StateClosed   = Closed
	StateReopened = Reopened
	StateAppealed = Appealed
)

func validState(s CaseState) bool {
	switch s {
	case Draft, Open, Waiting, Paused, Resolved, Closed, Reopened, Appealed:
		return true
	}
	return false
}
func terminal(s CaseState) bool { return s == Closed }

// CaseDefinition is the versioned service/type contract for a case.
type CaseDefinition struct {
	Type           string
	Version        string
	Service        string
	Purpose        string
	Classification string
	Retention      string
}

func (d CaseDefinition) Validate() error {
	for _, x := range []struct{ n, v string }{{"type", d.Type}, {"version", d.Version}, {"service", d.Service}, {"purpose", d.Purpose}, {"classification", d.Classification}, {"retention", d.Retention}} {
		if strings.TrimSpace(x.v) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidDefinition, x.n)
		}
	}
	return nil
}

func NewCaseDefinition(d CaseDefinition) (CaseDefinition, error) {
	if err := d.Validate(); err != nil {
		return CaseDefinition{}, err
	}
	return d, nil
}

// Participant identifies a business participant without granting visibility;
// visibility is a separate, later authorization concern.
type Participant struct{ Role, Principal string }

// CaseRevision is immutable once constructed. Successive changes produce a
// new revision and retain the prior revision in the aggregate chronology.
type CaseRevision struct {
	CaseID         string
	Revision       uint64
	Definition     CaseDefinition
	Requester      string
	Subject        string
	Purpose        string
	Classification string
	Retention      string
	Participants   []Participant
	State          CaseState
	Previous       uint64
	Disposition    string
	Digest         string
}

func (r CaseRevision) Validate() error {
	if strings.TrimSpace(r.CaseID) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: case id and revision are required", ErrInvalidRevision)
	}
	if err := r.Definition.Validate(); err != nil {
		return err
	}
	for _, x := range []struct{ n, v string }{{"requester", r.Requester}, {"subject", r.Subject}, {"purpose", r.Purpose}, {"classification", r.Classification}, {"retention", r.Retention}} {
		if strings.TrimSpace(x.v) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRevision, x.n)
		}
	}
	if !validState(r.State) {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidRevision, r.State)
	}
	seen := map[string]bool{}
	for i, p := range r.Participants {
		if strings.TrimSpace(p.Role) == "" || strings.TrimSpace(p.Principal) == "" {
			return fmt.Errorf("%w: participants[%d] is incomplete", ErrInvalidRevision, i)
		}
		k := p.Role + "\x00" + p.Principal
		if seen[k] {
			return fmt.Errorf("%w: duplicate participant", ErrInvalidRevision)
		}
		seen[k] = true
	}
	return nil
}

func cloneRevision(r CaseRevision) CaseRevision {
	r.Participants = append([]Participant(nil), r.Participants...)
	return r
}
func canonicalRevision(r CaseRevision) string {
	participants := ""
	for _, p := range r.Participants {
		participants += p.Role + "\x00" + p.Principal + "\x00"
	}
	return fmt.Sprintf("%s\x1f%d\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%d\x1f%s\x1f%s", r.CaseID, r.Revision, r.Definition.Type, r.Definition.Version, r.Definition.Service, r.Requester, r.Subject, r.Purpose, r.Classification, r.Retention, r.State, r.Previous, r.Disposition, participants)
}

func NewCaseRevision(r CaseRevision) (CaseRevision, error) {
	r = cloneRevision(r)
	r.Digest = ""
	if err := r.Validate(); err != nil {
		return CaseRevision{}, err
	}
	h := sha256.Sum256([]byte(canonicalRevision(r)))
	r.Digest = hex.EncodeToString(h[:])
	return r, nil
}
func (r CaseRevision) Verify() bool {
	d := r.Digest
	r.Digest = ""
	n, e := NewCaseRevision(r)
	return e == nil && n.Digest == d
}

// CaseAggregate is an append-only in-memory case stream. Persistence and
// delivery belong to their owning adapters.
type CaseAggregate struct{ revisions []CaseRevision }

func NewCase(r HRRequest) (CaseAggregate, error) {
	rev, err := r.Revision()
	if err != nil {
		return CaseAggregate{}, err
	}
	return CaseAggregate{revisions: []CaseRevision{rev}}, nil
}
func (c CaseAggregate) Current() CaseRevision {
	if len(c.revisions) == 0 {
		return CaseRevision{}
	}
	return cloneRevision(c.revisions[len(c.revisions)-1])
}
func (c CaseAggregate) Revisions() []CaseRevision {
	out := make([]CaseRevision, len(c.revisions))
	for i := range c.revisions {
		out[i] = cloneRevision(c.revisions[i])
	}
	return out
}
func (c *CaseAggregate) Apply(to CaseState, disposition string) (Result, error) {
	if c == nil || len(c.revisions) == 0 {
		return Result{}, ErrInvalidRevision
	}
	result, err := Apply(c.revisions[len(c.revisions)-1], to, disposition)
	if err != nil {
		return Result{}, err
	}
	c.revisions = append(c.revisions, cloneRevision(result.Revision))
	return result, nil
}
