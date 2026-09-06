// Package telemetry applies the pilot's privacy, cardinality, and sampling
// policy before an observation can leave the process.
package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const policyContractVersion = 1

// Version identifies the telemetry policy evaluator contract.
func Version() int { return policyContractVersion }

type Signal string

const (
	SignalMetric    Signal = "metric"
	SignalTrace     Signal = "trace"
	SignalLog       Signal = "log"
	SignalSecurity  Signal = "security"
	SignalFinancial Signal = "financial"
)

// AttributeRule is the only way an event attribute may reach an export.
type AttributeRule struct {
	Allowed     bool
	Redact      bool
	MetricLabel bool
}

type CardinalityBudget struct {
	Signal    Signal
	Attribute string
	Limit     int
}

type SamplingPolicy struct {
	DefaultRate  float64
	CriticalRate float64
}

// Policy is immutable configuration. SigningKey is used only to sign the
// receipt and is never included in a digest or exported event.
type Policy struct {
	ID                 string
	Version            string
	Owner              string
	AllowedAttributes  map[string]AttributeRule
	CardinalityBudgets []CardinalityBudget
	Sampling           SamplingPolicy
	SigningKey         []byte
}

// Event is a pre-policy observation. Attributes are copied before inspection.
type Event struct {
	ID           string
	TenantID     string
	Signal       Signal
	Name         string
	Outcome      string
	FailureClass string
	Critical     bool
	Attributes   map[string]string
}

type Receipt struct {
	PolicyID             string
	PolicyVersion        string
	PolicyDigest         string
	Signature            string
	Exported             int
	Dropped              int
	Redacted             int
	Aggregated           int
	CriticalFailuresKept int
}

type ExportResult struct {
	Events  []Event
	Receipt Receipt
}

var (
	ErrInvalidPolicy = errors.New("telemetry: invalid policy")
	ErrInvalidEvent  = errors.New("telemetry: invalid event")
)

func (p Policy) Validate() error {
	for name, value := range map[string]string{"id": p.ID, "version": p.Version, "owner": p.Owner} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPolicy, name)
		}
	}
	if len(p.SigningKey) == 0 {
		return fmt.Errorf("%w: signing key is required", ErrInvalidPolicy)
	}
	if p.Sampling.DefaultRate < 0 || p.Sampling.DefaultRate > 1 || p.Sampling.CriticalRate < 0 || p.Sampling.CriticalRate > 1 {
		return fmt.Errorf("%w: sampling rates must be between zero and one", ErrInvalidPolicy)
	}
	if p.Sampling.CriticalRate != 1 {
		return fmt.Errorf("%w: critical sampling rate must be 1", ErrInvalidPolicy)
	}
	seen := map[string]bool{}
	for name, rule := range p.AllowedAttributes {
		if strings.TrimSpace(name) == "" || !rule.Allowed {
			return fmt.Errorf("%w: attribute %q is not explicitly allowed", ErrInvalidPolicy, name)
		}
	}
	for _, budget := range p.CardinalityBudgets {
		key := string(budget.Signal) + "\x00" + budget.Attribute
		if budget.Signal != SignalMetric || strings.TrimSpace(budget.Attribute) == "" || budget.Limit < 1 || seen[key] {
			return fmt.Errorf("%w: invalid cardinality budget %q", ErrInvalidPolicy, key)
		}
		seen[key] = true
		if rule, ok := p.AllowedAttributes[budget.Attribute]; !ok || !rule.MetricLabel {
			return fmt.Errorf("%w: cardinality budget requires a metric label %q", ErrInvalidPolicy, budget.Attribute)
		}
	}
	for name, rule := range p.AllowedAttributes {
		if rule.MetricLabel && !seen[string(SignalMetric)+"\x00"+name] {
			return fmt.Errorf("%w: metric label %q has no cardinality budget", ErrInvalidPolicy, name)
		}
	}
	return nil
}

func (e Event) validate() error {
	for name, value := range map[string]string{"id": e.ID, "tenant_id": e.TenantID, "name": e.Name} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidEvent, name)
		}
	}
	if e.Signal == "" {
		return fmt.Errorf("%w: signal is required", ErrInvalidEvent)
	}
	return nil
}

func budgetMap(p Policy) map[string]int {
	out := make(map[string]int, len(p.CardinalityBudgets))
	for _, budget := range p.CardinalityBudgets {
		out[string(budget.Signal)+"\x00"+budget.Attribute] = budget.Limit
	}
	return out
}

func sensitiveAttribute(name string) bool {
	name = strings.ToLower(name)
	for _, word := range []string{"salary", "medical", "bank", "case", "prompt", "payload"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

func keepEvent(policy Policy, event Event) bool {
	if mandatory(event) {
		return true
	}
	rate := policy.Sampling.DefaultRate
	if rate == 0 {
		return false
	}
	digest := sha256.Sum256([]byte(policy.Version + "\x00" + event.ID))
	value := binary.BigEndian.Uint64(digest[:8])
	return float64(value)/float64(math.MaxUint64) < rate
}

func mandatory(event Event) bool {
	if event.Critical {
		return true
	}
	if event.Signal != SignalSecurity && event.Signal != SignalFinancial {
		return false
	}
	text := strings.ToLower(event.Outcome + " " + event.FailureClass)
	return strings.Contains(text, "fail") || strings.Contains(text, "error") || strings.Contains(text, "den")
}

func canonicalPolicy(p Policy) string {
	attributes := make([]string, 0, len(p.AllowedAttributes))
	for name, rule := range p.AllowedAttributes {
		attributes = append(attributes, fmt.Sprintf("%s:%t:%t:%t", name, rule.Allowed, rule.Redact, rule.MetricLabel))
	}
	sort.Strings(attributes)
	budgets := make([]string, 0, len(p.CardinalityBudgets))
	for _, budget := range p.CardinalityBudgets {
		budgets = append(budgets, fmt.Sprintf("%s:%s:%d", budget.Signal, budget.Attribute, budget.Limit))
	}
	sort.Strings(budgets)
	return strings.Join([]string{p.ID, p.Version, p.Owner, strings.Join(attributes, ","), strings.Join(budgets, ","), fmt.Sprintf("%.9f:%.9f", p.Sampling.DefaultRate, p.Sampling.CriticalRate)}, "|")
}

func policyDigest(p Policy) string {
	digest := sha256.Sum256([]byte(canonicalPolicy(p)))
	return hex.EncodeToString(digest[:])
}

func signReceipt(p Policy, receipt Receipt) string {
	message := fmt.Sprintf("%s|%s|%s|%d|%d|%d|%d", receipt.PolicyID, receipt.PolicyVersion, receipt.PolicyDigest, receipt.Exported, receipt.Dropped, receipt.Redacted, receipt.CriticalFailuresKept)
	digest := hmac.New(sha256.New, p.SigningKey)
	_, _ = digest.Write([]byte(message))
	return hex.EncodeToString(digest.Sum(nil))
}

// Export applies privacy, sampling, and cardinality controls as one pure
// operation. It never mutates policy or caller-owned event maps.
func Export(policy Policy, events []Event) (ExportResult, error) {
	if err := policy.Validate(); err != nil {
		return ExportResult{}, err
	}
	result := ExportResult{Events: make([]Event, 0, len(events))}
	limits := budgetMap(policy)
	seen := map[string]map[string]map[string]bool{}
	for _, original := range events {
		if err := original.validate(); err != nil {
			return ExportResult{}, err
		}
		if !keepEvent(policy, original) {
			result.Receipt.Dropped++
			continue
		}
		event := original
		event.Attributes = make(map[string]string)
		for name, value := range original.Attributes {
			rule, allowed := policy.AllowedAttributes[name]
			if !allowed || !rule.Allowed {
				result.Receipt.Redacted++
				continue
			}
			if rule.Redact || sensitiveAttribute(name) {
				event.Attributes[name] = "[REDACTED]"
				result.Receipt.Redacted++
				continue
			}
			if rule.MetricLabel {
				key := string(event.Signal) + "\x00" + name
				if limit, ok := limits[key]; ok {
					if seen[key] == nil {
						seen[key] = map[string]map[string]bool{}
					}
					if seen[key][event.TenantID] == nil {
						seen[key][event.TenantID] = map[string]bool{}
					}
					if !seen[key][event.TenantID][value] && len(seen[key][event.TenantID]) >= limit {
						event.Attributes[name] = "[AGGREGATED]"
						result.Receipt.Aggregated++
						continue
					}
					seen[key][event.TenantID][value] = true
				}
			}
			event.Attributes[name] = value
		}
		if mandatory(event) {
			result.Receipt.CriticalFailuresKept++
		}
		result.Events = append(result.Events, event)
	}
	result.Receipt.Exported = len(result.Events)
	result.Receipt.PolicyID = policy.ID
	result.Receipt.PolicyVersion = policy.Version
	result.Receipt.PolicyDigest = policyDigest(policy)
	result.Receipt.Signature = signReceipt(policy, result.Receipt)
	return result, nil
}

// Explain returns a compact, privacy-safe description of an export receipt.
func Explain(result ExportResult) string {
	return fmt.Sprintf("telemetry policy %s/%s exported=%d dropped=%d redacted=%d aggregated=%d critical_kept=%d", result.Receipt.PolicyID, result.Receipt.PolicyVersion, result.Receipt.Exported, result.Receipt.Dropped, result.Receipt.Redacted, result.Receipt.Aggregated, result.Receipt.CriticalFailuresKept)
}

// VerifyReceipt validates the HMAC-backed policy receipt without exposing the
// signing material or requiring a stateful verifier.
func VerifyReceipt(policy Policy, receipt Receipt) bool {
	if policy.Validate() != nil || receipt.PolicyID != policy.ID || receipt.PolicyVersion != policy.Version || receipt.PolicyDigest != policyDigest(policy) {
		return false
	}
	want := signReceipt(policy, receipt)
	return hmac.Equal([]byte(want), []byte(receipt.Signature))
}
