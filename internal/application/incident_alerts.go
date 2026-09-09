package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrIncidentAlertInvalid  = errors.New("application: invalid incident alert")
	ErrIncidentAlertTampered = errors.New("application: alert digest mismatch")
	ErrIncidentAckInvalid    = errors.New("application: invalid incident acknowledgement")
)

// IncidentRoutePolicy is authoritative application policy. Its owner and
// fallback route are deliberately separate from the telemetry alert, so an
// alert payload cannot grant itself operational privileges.
type IncidentRoutePolicy struct {
	PrimaryOwner   string
	SecondaryRoute string
	StormLimit     int
	StormWindow    time.Duration
}

// IncidentAlertResult is the durable routing result. Created is false for an
// identical replay admitted by the store's semantic dedupe fence.
type IncidentAlertResult = opsmeta.AlertIncidentResult

// TrustedRoutedAlert binds a detector result to the tenant for which detection
// was requested. The caller remains responsible for supplying a signal port
// already scoped to that tenant; this prevents a post-detection tenant swap,
// but does not authenticate an arbitrary SignalPort.
type TrustedRoutedAlert struct {
	tenant uuid.UUID
	alert  securityevidence.RoutedAlert
}

// DetectOwnedAlerts is the only constructor for TrustedRoutedAlert. The
// application supplies the tenant boundary while the telemetry detector owns
// the rule lookup and alert construction.
func DetectOwnedAlerts(tenant uuid.UUID, detector *securityevidence.SequenceDetector, window securityevidence.Window) ([]TrustedRoutedAlert, error) {
	if tenant == uuid.Nil || detector == nil {
		return nil, ErrIncidentAlertInvalid
	}
	alerts, err := detector.Detect(window)
	if err != nil {
		return nil, err
	}
	trusted := make([]TrustedRoutedAlert, len(alerts))
	for i, alert := range alerts {
		trusted[i] = TrustedRoutedAlert{tenant: tenant, alert: alert}
	}
	return trusted, nil
}

// RouteOwnedAlert validates a detector-produced alert, derives its semantics,
// and routes it through the caller-owned tenant-scoped transaction. It never
// commits, rolls back, or acknowledges on the caller's behalf.
func RouteOwnedAlert(ctx context.Context, tx dbport.Tx, trusted TrustedRoutedAlert, policy IncidentRoutePolicy) (IncidentAlertResult, error) {
	if tx == nil || trusted.tenant == uuid.Nil {
		return IncidentAlertResult{}, ErrIncidentAlertInvalid
	}
	tenant, alert := trusted.tenant, trusted.alert
	if alert.Digest == "" || securityevidence.DigestOfAlert(alert) != alert.Digest {
		return IncidentAlertResult{}, ErrIncidentAlertTampered
	}
	if strings.TrimSpace(alert.RuleID) == "" || alert.RuleVersion < 1 || alert.ObservedCount < 1 || alert.WindowEnd.IsZero() || alert.EvidenceDigest == "" {
		return IncidentAlertResult{}, ErrIncidentAlertInvalid
	}
	decision, err := incidentstate.Route(incidentstate.Alert{
		TenantID: tenant.String(), RuleID: alert.RuleID, RuleVersion: alert.RuleVersion,
		Fingerprint: alert.Digest, Severity: severityForRoute(alert), Service: alert.AlertName,
		Capability: alert.RuleID, ObservedAt: alert.WindowEnd, EvidenceDigest: alert.EvidenceDigest,
		Count: alert.ObservedCount,
	}, incidentstate.Routes{Primary: policy.PrimaryOwner, Secondary: policy.SecondaryRoute}, policy.StormLimit)
	if err != nil {
		return IncidentAlertResult{}, err
	}
	incidentID := uuid.NewSHA1(uuid.NewSHA1(uuid.Nil, []byte("hcmnext.application.incident-alert/v1")), []byte(tenant.String()+"\x00"+decision.IncidentKey))
	return opsmeta.RouteAlert(ctx, tx, opsmeta.AlertIncident{
		TenantID: tenant, IncidentID: incidentID, IncidentKey: decision.IncidentKey,
		Severity: severityForRoute(alert), Scope: nil, CorrelationKey: decision.CorrelationKey,
		EvidenceDigest: alert.EvidenceDigest, DeclaredAt: alert.WindowEnd,
		PrimaryOwner: decision.Routes.Primary, SecondaryRoute: decision.Routes.Secondary,
		StormLimit: policy.StormLimit, StormWindow: policy.StormWindow,
	})
}

// severityForRoute supplies the bridge's bounded default. RoutedAlert does not
// carry a severity field, so urgency is never inferred from alert payload
// text or count at this boundary.
func severityForRoute(alert securityevidence.RoutedAlert) string {
	_ = alert
	return "SEV2"
}

// IncidentAcknowledgement is an explicit human acknowledgement. Receipt is
// reduced to a digest before persistence; its contents are never stored.
type IncidentAcknowledgement struct {
	TenantID     uuid.UUID
	IncidentID   uuid.UUID
	Receipt      string
	EvidenceTime time.Time
}

// IncidentTenantResolver maps the authenticated principal's canonical tenant
// key to its durable UUID. The composition root supplies the authoritative
// tenant registry lookup; acknowledgement never guesses between identifiers.
type IncidentTenantResolver interface {
	ResolveIncidentTenant(context.Context, dbport.Querier, values.TenantId) (uuid.UUID, error)
}

type IncidentTenantResolverFunc func(context.Context, dbport.Querier, values.TenantId) (uuid.UUID, error)

func (f IncidentTenantResolverFunc) ResolveIncidentTenant(ctx context.Context, q dbport.Querier, tenant values.TenantId) (uuid.UUID, error) {
	return f(ctx, q, tenant)
}

// AcknowledgeIncident records an authenticated primary-owner acknowledgement
// in the caller-owned tenant transaction. It has no automatic path from alert
// routing and persists only a receipt digest and observation time.
func AcknowledgeIncident(ctx context.Context, tx dbport.Tx, tenants IncidentTenantResolver, ack IncidentAcknowledgement) error {
	if tx == nil || tenants == nil || ack.TenantID == uuid.Nil || ack.IncidentID == uuid.Nil || strings.TrimSpace(ack.Receipt) == "" || ack.EvidenceTime.IsZero() {
		return ErrIncidentAckInvalid
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || strings.TrimSpace(principal.Subject()) == "" {
		return ErrIncidentAckInvalid
	}
	tenant, err := tenants.ResolveIncidentTenant(ctx, tx, principal.Tenant())
	if err != nil {
		return err
	}
	if tenant != ack.TenantID {
		return ErrIncidentAckInvalid
	}
	h := sha256.Sum256([]byte(ack.Receipt))
	return opsmeta.AcknowledgeOperationalIncident(ctx, tx, ack.TenantID, ack.IncidentID, principal.Subject(), hex.EncodeToString(h[:]), ack.EvidenceTime)
}
