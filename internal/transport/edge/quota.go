package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// QuotaKey is assembled from trusted server context. Callers must not copy
// tenant or principal values from an untrusted request payload into it.
type QuotaKey struct {
	TenantID    string
	PrincipalID string
	Route       string
	Epoch       uint64
}

type QuotaRequest struct {
	Key         QuotaKey
	Criticality string
	Cost        int
}

type QuotaSnapshot struct {
	KeyEpoch          uint64
	TenantConsumed    int
	PrincipalConsumed int
	RouteConsumed     int
	TenantPending     int
	PrincipalPending  int
	RoutePending      int
	ReservedCritical  int
}

type QuotaPolicy struct {
	TenantLimit    int
	PrincipalLimit int
	RouteLimit     int
	RetryAfter     int
}

type QuotaDecision struct {
	Outcome        IngressOutcome
	Reason         string
	Scope          string
	Criticality    string
	RetryAfter     int
	KeyFingerprint string
	Remaining      int
}

// CheckQuota enforces all three fairness dimensions from one trusted key and
// protects reserved critical capacity from lower-priority callers.
func CheckQuota(request QuotaRequest, snapshot QuotaSnapshot, policy QuotaPolicy) (QuotaDecision, error) {
	if strings.TrimSpace(request.Key.TenantID) == "" || strings.TrimSpace(request.Key.PrincipalID) == "" || strings.TrimSpace(request.Key.Route) == "" || request.Key.Epoch == 0 || request.Cost <= 0 || request.Criticality == "" {
		return QuotaDecision{}, errors.New("edge: invalid quota request")
	}
	if snapshot.KeyEpoch == 0 || request.Key.Epoch != snapshot.KeyEpoch || policy.TenantLimit <= 0 || policy.PrincipalLimit <= 0 || policy.RouteLimit <= 0 || policy.RetryAfter <= 0 || snapshot.TenantConsumed < 0 || snapshot.PrincipalConsumed < 0 || snapshot.RouteConsumed < 0 || snapshot.TenantPending < 0 || snapshot.PrincipalPending < 0 || snapshot.RoutePending < 0 || snapshot.ReservedCritical < 0 {
		return QuotaDecision{}, errors.New("edge: invalid quota snapshot")
	}
	d := QuotaDecision{Outcome: IngressAllow, Scope: "tenant/principal/route", Criticality: request.Criticality, RetryAfter: 0, KeyFingerprint: quotaFingerprint(request.Key)}
	if request.Criticality == "P0" && snapshot.ReservedCritical >= request.Cost {
		d.Reason, d.Scope, d.Remaining = "RESERVED_CRITICAL_CAPACITY", "critical-reservation", snapshot.ReservedCritical-request.Cost
		return d, nil
	}
	checks := []struct {
		name     string
		consumed int
		pending  int
		limit    int
	}{
		{"tenant", snapshot.TenantConsumed, snapshot.TenantPending, policy.TenantLimit},
		{"principal", snapshot.PrincipalConsumed, snapshot.PrincipalPending, policy.PrincipalLimit},
		{"route", snapshot.RouteConsumed, snapshot.RoutePending, policy.RouteLimit},
	}
	for _, check := range checks {
		if check.consumed > check.limit || check.pending > check.limit-check.consumed || request.Cost > check.limit-check.consumed-check.pending {
			d.Outcome, d.Reason, d.Scope, d.RetryAfter, d.Remaining = IngressThrottle, "QUOTA_EXHAUSTED", check.name, policy.RetryAfter, 0
			return d, nil
		}
	}
	d.Reason = "WITHIN_FAIR_SHARE"
	d.Remaining = minInt(policy.TenantLimit-snapshot.TenantConsumed-snapshot.TenantPending-request.Cost, policy.PrincipalLimit-snapshot.PrincipalConsumed-snapshot.PrincipalPending-request.Cost, policy.RouteLimit-snapshot.RouteConsumed-snapshot.RoutePending-request.Cost)
	return d, nil
}

func quotaFingerprint(key QuotaKey) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", key.TenantID, key.PrincipalID, key.Route, key.Epoch)))
	return hex.EncodeToString(sum[:])
}

func minInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}
