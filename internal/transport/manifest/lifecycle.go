package manifest

import (
	"fmt"
	"sort"
)

// LifecycleStatus is the publication state of an API version.
type LifecycleStatus string

const (
	LifecycleActive     LifecycleStatus = "ACTIVE"
	LifecycleDeprecated LifecycleStatus = "DEPRECATED"
	LifecycleRetired    LifecycleStatus = "RETIRED"
)

// ContractField is the semantic shape of one request or response field. A
// field's name is its stable, dotted contract path. Required is interpreted in
// the direction of the wire: a newly-required request field and a removed
// response field are breaking changes.
type ContractField struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// Consumer records a tenant's dependency on an API version. Hard consumers
// must have an adoption record before a version can be retired.
type Consumer struct {
	ID             string `json:"id"`
	Tenant         string `json:"tenant"`
	Required       bool   `json:"required"`
	Active         bool   `json:"active"`
	AdoptedVersion uint32 `json:"adopted_version"`
	AdoptionRecord string `json:"adoption_record"`
}

// Version is the reviewable API contract used by Check. SemanticDigest is
// deliberately separate from transport/schema fields: transport-compatible
// bytes must not conceal a changed business meaning.
type Version struct {
	EndpointID        string                   `json:"endpoint_id"`
	Version           uint32                   `json:"version"`
	Status            LifecycleStatus          `json:"status"`
	RequestFields     map[string]ContractField `json:"request_fields,omitempty"`
	ResponseFields    map[string]ContractField `json:"response_fields,omitempty"`
	ErrorCodes        map[string]string        `json:"error_codes,omitempty"`
	SemanticDigest    string                   `json:"semantic_digest"`
	MigrationEvidence string                   `json:"migration_evidence"`
	Consumers         []Consumer               `json:"consumers,omitempty"`
}

type Decision string

const (
	DecisionCompatible Decision = "COMPATIBLE"
	DecisionMigrate    Decision = "MIGRATE"
	DecisionBlock      Decision = "BLOCK"
)

type BreakCode string

const (
	BreakEndpointIdentity BreakCode = "endpoint_identity_changed"
	BreakRequestRequired  BreakCode = "request_field_required"
	BreakRequestRemoved   BreakCode = "request_field_removed"
	BreakResponseRemoved  BreakCode = "response_field_removed"
	BreakFieldType        BreakCode = "field_type_changed"
	BreakErrorRemoved     BreakCode = "error_removed"
	BreakErrorMeaning     BreakCode = "error_meaning_changed"
	BreakSemantic         BreakCode = "semantic_behavior_changed"
	BreakVersion          BreakCode = "version_not_advanced"
)

type Break struct {
	Code   BreakCode `json:"code"`
	Path   string    `json:"path"`
	Detail string    `json:"detail"`
}

type ConsumerImpact struct {
	ID       string `json:"id"`
	Tenant   string `json:"tenant"`
	Required bool   `json:"required"`
	Reason   string `json:"reason"`
}

type Result struct {
	EndpointID string           `json:"endpoint_id"`
	Previous   uint32           `json:"previous"`
	Current    uint32           `json:"current"`
	Decision   Decision         `json:"decision"`
	Breaks     []Break          `json:"breaks"`
	Consumers  []ConsumerImpact `json:"consumers"`
}

func (r Result) OK() bool { return r.Decision == DecisionCompatible }

// Check compares two versions without consulting transport adapters or global
// state. Results are deterministic and identify every incompatible location.
func Check(previous, current Version) Result {
	r := Result{EndpointID: current.EndpointID, Previous: previous.Version, Current: current.Version, Decision: DecisionCompatible, Breaks: []Break{}, Consumers: []ConsumerImpact{}}
	if previous.EndpointID == "" || current.EndpointID == "" || previous.EndpointID != current.EndpointID {
		r.addBreak(BreakEndpointIdentity, current.EndpointID, "endpoint identity changed")
	}
	if current.Version <= previous.Version {
		r.addBreak(BreakVersion, "version", fmt.Sprintf("current version %d must exceed previous version %d", current.Version, previous.Version))
	}
	compareFields(&r, previous.RequestFields, current.RequestFields, true)
	compareFields(&r, previous.ResponseFields, current.ResponseFields, false)
	for code, meaning := range previous.ErrorCodes {
		newMeaning, ok := current.ErrorCodes[code]
		if !ok {
			r.addBreak(BreakErrorRemoved, "error."+code, "error code was removed")
		} else if meaning != newMeaning {
			r.addBreak(BreakErrorMeaning, "error."+code, "error meaning changed")
		}
	}
	if previous.SemanticDigest != current.SemanticDigest {
		r.addBreak(BreakSemantic, "semantic", "business behavior changed; transport compatibility cannot mask it")
	}
	for _, c := range current.Consumers {
		if c.Active && c.Required && c.AdoptedVersion < current.Version {
			r.Consumers = append(r.Consumers, ConsumerImpact{ID: c.ID, Tenant: c.Tenant, Required: c.Required, Reason: "active consumer has not adopted the candidate version"})
		}
	}
	if len(r.Breaks) > 0 {
		r.Decision = DecisionMigrate
	}
	sort.Slice(r.Breaks, func(i, j int) bool {
		if r.Breaks[i].Code != r.Breaks[j].Code {
			return r.Breaks[i].Code < r.Breaks[j].Code
		}
		return r.Breaks[i].Path < r.Breaks[j].Path
	})
	return r
}

// GateLifecycle enforces deprecation and retirement evidence. A deprecated
// version needs an adoption record for every active consumer; retirement also
// requires migration evidence and no active unsupported consumer.
func GateLifecycle(previous, candidate Version) Result {
	r := Check(previous, candidate)
	for _, c := range candidate.Consumers {
		adopted := c.AdoptedVersion >= candidate.Version
		if !c.Active || (adopted && (candidate.Status != LifecycleDeprecated || c.AdoptionRecord != "")) {
			continue
		}
		if !hasConsumerImpact(r.Consumers, c.ID, c.Tenant) {
			r.Consumers = append(r.Consumers, ConsumerImpact{ID: c.ID, Tenant: c.Tenant, Required: c.Required, Reason: "active consumer lacks adoption record for lifecycle change"})
		}
	}
	if candidate.Status == LifecycleDeprecated && len(r.Consumers) > 0 {
		r.Decision = DecisionBlock
	}
	if candidate.Status == LifecycleRetired {
		if candidate.MigrationEvidence == "" || len(r.Consumers) > 0 {
			r.Decision = DecisionBlock
		}
	}
	if len(r.Breaks) > 0 && r.Decision != DecisionBlock {
		r.Decision = DecisionMigrate
	}
	sort.Slice(r.Consumers, func(i, j int) bool {
		if r.Consumers[i].Tenant != r.Consumers[j].Tenant {
			return r.Consumers[i].Tenant < r.Consumers[j].Tenant
		}
		return r.Consumers[i].ID < r.Consumers[j].ID
	})
	return r
}

func hasConsumerImpact(impacts []ConsumerImpact, id, tenant string) bool {
	for _, impact := range impacts {
		if impact.ID == id && impact.Tenant == tenant {
			return true
		}
	}
	return false
}

func (r *Result) addBreak(code BreakCode, path, detail string) {
	r.Breaks = append(r.Breaks, Break{Code: code, Path: path, Detail: detail})
}

func compareFields(r *Result, old, next map[string]ContractField, request bool) {
	for path, f := range old {
		n, ok := next[path]
		if !ok {
			if request {
				r.addBreak(BreakRequestRemoved, path, "request field was removed")
			} else {
				r.addBreak(BreakResponseRemoved, path, "response field was removed")
			}
			continue
		}
		if f.Type != n.Type {
			r.addBreak(BreakFieldType, path, "field type changed")
		}
		if request && !f.Required && n.Required {
			r.addBreak(BreakRequestRequired, path, "request field became required")
		}
	}
}
