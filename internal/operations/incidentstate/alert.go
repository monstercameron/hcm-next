package incidentstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrAlertInvalid = errors.New("incident: invalid alert")
	ErrNoOwner      = errors.New("INCIDENT_NO_OWNER")
	ErrNoRoute      = errors.New("INCIDENT_NO_ROUTE")
	ErrAlertStorm   = errors.New("INCIDENT_ALERT_STORM")
)

// Alert is the bounded, already-redacted input from the telemetry plane.
// Raw payloads are excluded; TenantID identifies the routing boundary.
type Alert struct {
	TenantID       string
	RuleID         string
	RuleVersion    int
	Fingerprint    string
	Severity       string
	Service        string
	Capability     string
	ObservedAt     time.Time
	EvidenceDigest string
	Count          int
}

type Routes struct{ Primary, Secondary string }

type RoutingDecision struct {
	IncidentKey             string
	CorrelationKey          string
	Routes                  Routes
	AcknowledgementRequired bool
	CustomerSafeEvidence    string
}

func (a Alert) Validate() error {
	if strings.TrimSpace(a.TenantID) == "" || strings.TrimSpace(a.RuleID) == "" || a.RuleVersion < 1 ||
		strings.TrimSpace(a.Fingerprint) == "" || strings.TrimSpace(a.Severity) == "" || a.ObservedAt.IsZero() ||
		strings.TrimSpace(a.EvidenceDigest) == "" || a.Count < 1 {
		return ErrAlertInvalid
	}
	if len(a.TenantID) > 128 || len(a.RuleID) > 128 || len(a.Fingerprint) > 256 || len(a.EvidenceDigest) > 256 || len(a.Service) > 128 || len(a.Capability) > 128 ||
		!oneOf(a.Severity, "SEV1", "SEV2", "SEV3", "SEV4", "SEV5") ||
		strings.ContainsAny(a.RuleID+a.Fingerprint+a.EvidenceDigest, " \t{}[]\r\n") {
		return ErrAlertInvalid
	}
	return nil
}

// Route validates ownership and bounds a storm before any durable side effect.
func Route(a Alert, routes Routes, stormLimit int) (RoutingDecision, error) {
	if err := a.Validate(); err != nil {
		return RoutingDecision{}, err
	}
	if strings.TrimSpace(routes.Primary) == "" {
		return RoutingDecision{}, ErrNoOwner
	}
	if strings.TrimSpace(routes.Secondary) == "" {
		return RoutingDecision{}, ErrNoRoute
	}
	if routes.Primary == routes.Secondary {
		return RoutingDecision{}, ErrNoRoute
	}
	if len(routes.Primary) > 256 || len(routes.Secondary) > 256 || strings.ContainsAny(routes.Primary+routes.Secondary, "\r\n{}[]") {
		return RoutingDecision{}, ErrAlertInvalid
	}
	if stormLimit < 1 || a.Count > stormLimit {
		return RoutingDecision{}, ErrAlertStorm
	}
	key := "alert:v1:" + tupleDigest(a.RuleID, strconv.Itoa(a.RuleVersion), a.Fingerprint)
	corr := tupleDigest("incident-correlation.v1", a.TenantID, key)
	// Evidence exposed to customers is intentionally a digest and state only.
	safe := "incident=" + digest(corr) + ";severity=" + a.Severity + ";status=OPEN"
	return RoutingDecision{IncidentKey: key, CorrelationKey: corr, Routes: routes, AcknowledgementRequired: true, CustomerSafeEvidence: safe}, nil
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Lengths count bytes so arbitrary delimiters inside a field cannot change
// the tuple boundaries. This encoding is versioned by its calling identity.
func tupleDigest(fields ...string) string {
	var encoded strings.Builder
	for _, field := range fields {
		encoded.WriteString(strconv.Itoa(len(field)))
		encoded.WriteByte(':')
		encoded.WriteString(field)
	}
	return digest(encoded.String())
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
