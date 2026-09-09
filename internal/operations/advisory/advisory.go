// Package advisory publishes privacy-safe, tenant-scoped customer updates
// derived from canonical incidents.
package advisory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
)

const contractVersion = 1

func Version() int { return contractVersion }

type Status string

const (
	StatusInvestigating Status = "INVESTIGATING"
	StatusMitigating    Status = "MITIGATING"
	StatusMonitoring    Status = "MONITORING"
	StatusResolved      Status = "RESOLVED"
)

type Audience struct {
	TenantID   string
	Role       string
	Authorized bool
}

type DeliveryEvidence struct {
	ReceiptID string
	Channel   string
	Status    string
	At        time.Time
}

type Request struct {
	ID            string
	PolicyVersion string
	Incident      incidentstate.Incident
	Audience      Audience
	Delivery      DeliveryEvidence
	PublishedAt   time.Time
}

type PublicFact struct {
	Kind  string
	Value string
}

type Advisory struct {
	ID            string
	TenantID      string
	IncidentID    string
	PolicyVersion string
	Version       uint64
	Status        Status
	Scope         string
	Message       string
	Facts         []PublicFact
	Delivery      DeliveryEvidence
	PublishedAt   time.Time
	Digest        string
}

type Publisher struct {
	mu      sync.Mutex
	history map[string][]Advisory
}

var (
	ErrInvalidRequest = errors.New("advisory: invalid request")
	ErrUnauthorized   = errors.New("advisory: tenant audience is unauthorized")
	ErrConflict       = errors.New("advisory: advisory version conflict")
)

func NewPublisher() *Publisher { return &Publisher{history: make(map[string][]Advisory)} }

func statusFor(state incidentstate.State) (Status, bool) {
	switch state {
	case incidentstate.Detected, incidentstate.Triaged, incidentstate.Declared:
		return StatusInvestigating, true
	case incidentstate.Mitigating:
		return StatusMitigating, true
	case incidentstate.Monitoring, incidentstate.Resolved:
		return StatusMonitoring, true
	case incidentstate.Reviewed:
		return StatusResolved, true
	default:
		return "", false
	}
}

func safeFact(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "capability", "effective_date", "population":
		return true
	default:
		return false
	}
}

func publicMessage(status Status) string {
	switch status {
	case StatusMitigating:
		return "We are mitigating an issue affecting your Human Capital Management Suite workflow."
	case StatusMonitoring:
		return "The issue affecting your Human Capital Management Suite workflow is contained and under monitoring."
	case StatusResolved:
		return "The issue affecting your Human Capital Management Suite workflow has been resolved and reviewed."
	default:
		return "We are investigating an issue affecting your Human Capital Management Suite workflow."
	}
}

func digest(a Advisory) string {
	facts := make([]string, 0, len(a.Facts))
	for _, fact := range a.Facts {
		facts = append(facts, fact.Kind+"="+fact.Value)
	}
	sort.Strings(facts)
	raw := fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s", a.ID, a.TenantID, a.IncidentID, a.PolicyVersion, a.Version, a.Status, strings.Join(facts, ","))
	d := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(d[:])
}

func build(req Request) (Advisory, error) {
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.PolicyVersion) == "" || strings.TrimSpace(req.Incident.ID) == "" || strings.TrimSpace(req.Incident.TenantID) == "" || strings.TrimSpace(req.Audience.TenantID) == "" || !req.Audience.Authorized || req.PublishedAt.IsZero() {
		return Advisory{}, ErrInvalidRequest
	}
	if req.Audience.TenantID != req.Incident.TenantID {
		return Advisory{}, ErrUnauthorized
	}
	status, ok := statusFor(req.Incident.State)
	if !ok {
		return Advisory{}, fmt.Errorf("%w: incident state %s is not customer publishable", ErrInvalidRequest, req.Incident.State)
	}
	if strings.TrimSpace(req.Delivery.ReceiptID) == "" || strings.TrimSpace(req.Delivery.Channel) == "" || !strings.EqualFold(req.Delivery.Status, "delivered") || req.Delivery.At.IsZero() {
		return Advisory{}, fmt.Errorf("%w: delivery evidence is required", ErrInvalidRequest)
	}
	a := Advisory{ID: req.ID, TenantID: req.Incident.TenantID, IncidentID: req.Incident.ID, PolicyVersion: req.PolicyVersion, Version: req.Incident.Version, Status: status, Scope: "tenant:" + req.Incident.TenantID, Message: publicMessage(status), Delivery: req.Delivery, PublishedAt: req.PublishedAt}
	for _, fact := range req.Incident.Affected.Facts {
		if fact.TenantID != req.Incident.TenantID || !safeFact(fact.Kind) || strings.TrimSpace(fact.Value) == "" {
			continue
		}
		a.Facts = append(a.Facts, PublicFact{Kind: strings.ToLower(strings.TrimSpace(fact.Kind)), Value: fact.Value})
	}
	sort.SliceStable(a.Facts, func(i, j int) bool {
		if a.Facts[i].Kind != a.Facts[j].Kind {
			return a.Facts[i].Kind < a.Facts[j].Kind
		}
		return a.Facts[i].Value < a.Facts[j].Value
	})
	a.Digest = digest(a)
	return a, nil
}

// Publish validates and records a new advisory version. History is append-only
// and keyed by tenant plus incident, so a customer cannot read another tenant.
func (p *Publisher) Publish(req Request) (Advisory, error) {
	a, err := build(req)
	if err != nil {
		return Advisory{}, err
	}
	if p == nil {
		return Advisory{}, ErrInvalidRequest
	}
	key := a.TenantID + "\x00" + a.IncidentID
	p.mu.Lock()
	defer p.mu.Unlock()
	history := p.history[key]
	if len(history) > 0 {
		last := history[len(history)-1]
		if a.Version < last.Version {
			return Advisory{}, ErrConflict
		}
		if a.Version == last.Version {
			if a.Digest == last.Digest {
				return last, nil
			}
			return Advisory{}, ErrConflict
		}
	}
	p.history[key] = append(history, a)
	return a, nil
}

func (p *Publisher) History(tenantID, incidentID string) []Advisory {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	history := p.history[tenantID+"\x00"+incidentID]
	out := make([]Advisory, len(history))
	copy(out, history)
	for i := range out {
		out[i].Facts = append([]PublicFact(nil), out[i].Facts...)
	}
	return out
}

func Explain(a Advisory) string {
	return fmt.Sprintf("advisory %s tenant=%s incident=%s status=%s version=%d", a.ID, a.TenantID, a.IncidentID, a.Status, a.Version)
}
