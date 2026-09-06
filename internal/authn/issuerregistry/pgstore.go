package issuerregistry

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
	"github.com/monstercameron/hcm-next/internal/trust/stepup"
)

// canonicalUUID matches a canonical, lowercase, hyphenated uuid string
// (RFC 4122 textual form). This package deliberately does not import
// github.com/google/uuid: internal/authn is not among that module's
// allowed_import_roots in definitions/architecture/dependency-roles.yaml
// (LIB-002's third-party semantic firewall), and neither that manifest nor
// migrations/ are in this lane's file roots to widen. A plain regex check
// plus [newEventID]'s own crypto/rand-based v4 generator below give this
// adapter everything it needs from that module (validate a caller-supplied
// uuid string, mint a fresh one for an event_id) without the import.
var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// newEventID mints a fresh, random RFC 4122 version 4 uuid as a string,
// standing in for uuid.New() without this package importing
// github.com/google/uuid (see [canonicalUUID]'s comment for why).
func newEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("issuerregistry: generate event id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// pgPinnedKey and pgClaimMapping are the JSON shapes [PGStore] stores in
// migration 00038's jwks_pinned_keys and claim_mappings jsonb columns.
// Kept private and explicit (rather than marshaling [PinnedKey] /
// [ClaimMapping] directly) so a future field added to either exported type
// does not silently change this adapter's on-disk JSON shape.
type pgPinnedKey struct {
	KeyID        string    `json:"key_id"`
	Algorithm    string    `json:"algorithm"`
	PublicKeyDER []byte    `json:"public_key_der"`
	NotBefore    time.Time `json:"not_before,omitempty"`
	NotAfter     time.Time `json:"not_after,omitempty"`
}

type pgClaimMapping struct {
	SourceClaim string `json:"source_claim"`
	Target      string `json:"target"`
}

// pgClaimMappingsEnvelope preserves the existing jsonb column while adding
// the assurance contract without requiring a new persistence column. GetIssuer
// remains backward-compatible with the original array shape, so already
// published revisions remain readable and immutable.
type pgClaimMappingsEnvelope struct {
	Mappings          []pgClaimMapping     `json:"mappings"`
	GovernmentTenant  bool                 `json:"government_tenant,omitempty"`
	AssuranceContract *pgAssuranceContract `json:"assurance_contract,omitempty"`
}

type pgAssuranceContract struct {
	TableVersion int   `json:"table_version"`
	IAL          uint8 `json:"ial"`
	AAL          uint8 `json:"aal"`
	FAL          uint8 `json:"fal"`
}

// DB is the database capability [PGStore] needs, stated in dbport's
// driver-free terms: every operation opens its own transaction (so
// withTenant can establish migration 00038's row level security context
// first), matching internal/data/configregistry.Store's own DB port.
type DB interface {
	dbport.Beginner
}

// PGStore implements [Store] over migration 00038's issuer_profile and
// issuer_state_event tables. See doc.go for why it does not implement
// [crossTenantIndex]: row level security scopes every read to exactly one
// tenant, so this adapter has no way to answer "which tenants know this
// issuer" and does not pretend to.
type PGStore struct {
	db DB
}

var _ Store = (*PGStore)(nil)

// NewPGStore returns a [PGStore] over db, typically a pgxadapter connection
// or pool already able to SET ROLE hcmnext_app -- the same expectation
// every other adapter in internal/data/* carries.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// withTenant validates tenant's storage identity and runs fn inside a
// transaction with migration 00038's row level security context
// established via [tenancy.SessionSetting]. fn's error, or a failure
// validating the tenant or setting its context, rolls the transaction back
// instead of committing it.
//
// tenant.String() must be the tenant table's own uuid primary key in its
// canonical string form -- exactly what internal/platform/configregistry.Scope.TenantID
// already requires of every persistence-layer caller in this codebase --
// not an arbitrary human-readable slug. [values.TenantId]'s own character
// class (lowercase alphanumerics and non-boundary, non-consecutive
// hyphens) happens to accept a canonical uuid string as one valid
// spelling, so this is a legal (if narrower) use of that type rather than
// a mismatch with it. This is a deliberate, RLS-driven choice, not an
// oversight: migration 00008 puts row level security on the tenant table
// itself (FORCE ROW LEVEL SECURITY, scoped by the very app.tenant_id
// session setting this method exists to establish), so a tenant_key ->
// tenant_id lookup query issued *before* that context is set would see
// zero rows under the identical policy every other tenant-scoped table in
// this codebase enforces -- there is no chicken-and-egg lookup available
// to a least-privilege hcmnext_app connection, only a caller-supplied
// identity, exactly as internal/data/configregistry.Store.withTenant
// already assumes.
//
// This method sets migration 00027/00008's app.tenant_id session setting
// itself with a literal set_config call rather than calling
// [tenancy.WithTenant] (which takes a github.com/google/uuid.UUID) -- see
// [canonicalUUID]'s comment for why this package does not import that
// module. [tenancy.SessionSetting] is the exact same session parameter
// name that helper sets, so migration 00038's row level security policy
// (copied verbatim from 00027/00008's own) is established identically.
func (s *PGStore) withTenant(ctx context.Context, tenant values.TenantId, fn func(tx dbport.Tx) error) error {
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("issuerregistry: tenant %q: %w", tenant, err)
	}
	if !canonicalUUID.MatchString(tenant.String()) {
		return fmt.Errorf("issuerregistry: tenant %q is not a valid uuid", tenant)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("issuerregistry: begin transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, tenancy.SessionSetting, tenant.String()); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("issuerregistry: set %s: %w", tenancy.SessionSetting, err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// PutIssuer implements [Store].
func (s *PGStore) PutIssuer(rev Issuer) error {
	ctx := context.Background()

	pinnedKeys := make([]pgPinnedKey, 0, len(rev.JWKS.PinnedKeys))
	for _, k := range rev.JWKS.PinnedKeys {
		pinnedKeys = append(pinnedKeys, pgPinnedKey{
			KeyID: k.KeyID, Algorithm: string(k.Algorithm), PublicKeyDER: k.PublicKeyDER,
			NotBefore: k.NotBefore, NotAfter: k.NotAfter,
		})
	}
	pinnedKeysJSON, err := json.Marshal(pinnedKeys)
	if err != nil {
		return fmt.Errorf("issuerregistry: marshal pinned keys: %w", err)
	}

	mappings := make([]pgClaimMapping, 0, len(rev.ClaimMappings))
	for _, m := range rev.ClaimMappings {
		mappings = append(mappings, pgClaimMapping{SourceClaim: m.SourceClaim, Target: string(m.Target)})
	}
	envelope := pgClaimMappingsEnvelope{Mappings: mappings}
	if rev.GovernmentTenant || rev.AssuranceContract.Configured() {
		tier := rev.AssuranceContractTier()
		envelope.GovernmentTenant = rev.GovernmentTenant
		envelope.AssuranceContract = &pgAssuranceContract{
			TableVersion: rev.AssuranceContract.TableVersion,
			IAL:          uint8(tier.IAL),
			AAL:          uint8(tier.AAL),
			FAL:          uint8(tier.FAL),
		}
	}
	mappingsJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("issuerregistry: marshal claim mappings: %w", err)
	}

	algorithms := make([]string, len(rev.Algorithms))
	for i, a := range rev.Algorithms {
		algorithms[i] = string(a)
	}

	return s.withTenant(ctx, rev.Tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO issuer_profile (
				tenant_id, issuer_url, revision, audience,
				jwks_kind, jwks_pinned_keys, jwks_bundle_ref, jwks_bundle_version, jwks_discovery_url,
				algorithms, claim_mappings, clock_skew_seconds, staleness_seconds,
				publisher_principal, published_at
			) VALUES (
				current_setting('app.tenant_id')::uuid, $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14
			)`,
			rev.IssuerURL, int64(rev.Revision), rev.Audience,
			string(rev.JWKS.Kind), pinnedKeysJSON, rev.JWKS.BundleRef, rev.JWKS.BundleVersion, rev.JWKS.DiscoveryURL,
			algorithms, mappingsJSON, int64(rev.ClockSkew/time.Second), int64(rev.MetadataStaleness/time.Second),
			rev.PublisherPrincipal, rev.PublishedAt,
		)
		return err
	})
}

// GetIssuer implements [Store].
func (s *PGStore) GetIssuer(ref Ref) (Issuer, bool, error) {
	ctx := context.Background()
	var (
		audience                                  string
		jwksKind, jwksBundleRef, jwksDiscoveryURL string
		jwksBundleVersion                         int
		pinnedKeysJSON, mappingsJSON              []byte
		algorithms                                []string
		clockSkewSeconds, stalenessSeconds        int64
		publisherPrincipal                        string
		publishedAt                               time.Time
		found                                     bool
	)
	err := s.withTenant(ctx, ref.Tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT audience, jwks_kind, jwks_pinned_keys, jwks_bundle_ref, jwks_bundle_version, jwks_discovery_url,
			       algorithms, claim_mappings, clock_skew_seconds, staleness_seconds, publisher_principal, published_at
			FROM issuer_profile
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND issuer_url = $1 AND revision = $2`,
			ref.IssuerURL, int64(ref.Revision),
		)
		scanErr := row.Scan(&audience, &jwksKind, &pinnedKeysJSON, &jwksBundleRef, &jwksBundleVersion, &jwksDiscoveryURL,
			&algorithms, &mappingsJSON, &clockSkewSeconds, &stalenessSeconds, &publisherPrincipal, &publishedAt)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		found = true
		return nil
	})
	if err != nil {
		return Issuer{}, false, err
	}
	if !found {
		return Issuer{}, false, nil
	}

	var pinnedKeys []pgPinnedKey
	if err := json.Unmarshal(pinnedKeysJSON, &pinnedKeys); err != nil {
		return Issuer{}, false, fmt.Errorf("issuerregistry: unmarshal pinned keys: %w", err)
	}
	var mappings []pgClaimMapping
	var envelope pgClaimMappingsEnvelope
	if err := json.Unmarshal(mappingsJSON, &envelope); err == nil && envelope.Mappings != nil {
		mappings = envelope.Mappings
	} else if err := json.Unmarshal(mappingsJSON, &mappings); err != nil {
		return Issuer{}, false, fmt.Errorf("issuerregistry: unmarshal claim mappings: %w", err)
	} else {
		envelope.Mappings = mappings
	}

	issuer := Issuer{
		Tenant: ref.Tenant, IssuerURL: ref.IssuerURL, Revision: ref.Revision,
		Audience: audience,
		JWKS: JWKSSource{
			Kind: JWKSSourceKind(jwksKind), BundleRef: jwksBundleRef,
			BundleVersion: jwksBundleVersion, DiscoveryURL: jwksDiscoveryURL,
		},
		ClockSkew:          time.Duration(clockSkewSeconds) * time.Second,
		MetadataStaleness:  time.Duration(stalenessSeconds) * time.Second,
		PublisherPrincipal: publisherPrincipal,
		PublishedAt:        publishedAt.UTC(),
		GovernmentTenant:   envelope.GovernmentTenant,
	}
	if envelope.AssuranceContract != nil {
		issuer.AssuranceContract = AssuranceContract{
			TableVersion: envelope.AssuranceContract.TableVersion,
			IAL:          stepup.IAL(envelope.AssuranceContract.IAL),
			AAL:          stepup.AAL(envelope.AssuranceContract.AAL),
			FAL:          stepup.FAL(envelope.AssuranceContract.FAL),
		}
	}
	for _, k := range pinnedKeys {
		issuer.JWKS.PinnedKeys = append(issuer.JWKS.PinnedKeys, PinnedKey{
			KeyID: k.KeyID, Algorithm: trustfederation.Algorithm(k.Algorithm), PublicKeyDER: k.PublicKeyDER,
			NotBefore: k.NotBefore, NotAfter: k.NotAfter,
		})
	}
	for _, a := range algorithms {
		issuer.Algorithms = append(issuer.Algorithms, trustfederation.Algorithm(a))
	}
	for _, m := range mappings {
		issuer.ClaimMappings = append(issuer.ClaimMappings, ClaimMapping{SourceClaim: m.SourceClaim, Target: PrincipalField(m.Target)})
	}
	return issuer, true, nil
}

// LatestRevision implements [Store].
func (s *PGStore) LatestRevision(tenant values.TenantId, issuerURL string) (uint32, bool, error) {
	ctx := context.Background()
	var (
		revision int64
		found    bool
	)
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT revision FROM issuer_profile
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND issuer_url = $1
			ORDER BY revision DESC LIMIT 1`, issuerURL)
		scanErr := row.Scan(&revision)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		found = true
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return uint32(revision), found, nil
}

// PutEvent implements [Store]. event_sequence is computed as one more than
// the current maximum for (tenant, issuer_url), matching
// internal/data/configregistry.Store.PutActivation's identical rationale:
// migration 00038 grants hcmnext_app only SELECT and INSERT on this
// append-only table, so a plain SELECT (not SELECT ... FOR UPDATE, which
// needs UPDATE privilege even for a lock-only read) computes the next
// sequence, and the sequence's own UNIQUE constraint catches a genuine race
// between two concurrent transitions as an insert error rather than
// silently losing an event.
func (s *PGStore) PutEvent(evt StateEvent) error {
	ctx := context.Background()
	return s.withTenant(ctx, evt.Tenant, func(tx dbport.Tx) error {
		var currentMax int64
		row := tx.QueryRow(ctx, `
			SELECT event_sequence FROM issuer_state_event
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND issuer_url = $1
			ORDER BY event_sequence DESC LIMIT 1`, evt.IssuerURL)
		switch err := row.Scan(&currentMax); {
		case errors.Is(err, dbport.ErrNoRows):
			currentMax = 0
		case err != nil:
			return err
		}

		eventID, err := newEventID()
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO issuer_state_event (
				tenant_id, event_id, issuer_url, revision, event_sequence,
				from_status, to_status, acted_by, authority, reason, occurred_at
			) VALUES (current_setting('app.tenant_id')::uuid,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			eventID, evt.IssuerURL, int64(evt.Revision), currentMax+1,
			string(evt.From), string(evt.To), evt.ActedBy, evt.Authority, evt.Reason, evt.At,
		)
		return err
	})
}

// LatestEvent implements [Store].
func (s *PGStore) LatestEvent(tenant values.TenantId, issuerURL string) (StateEvent, bool, error) {
	ctx := context.Background()
	var (
		evt   StateEvent
		found bool
	)
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT revision, from_status, to_status, acted_by, authority, reason, occurred_at
			FROM issuer_state_event
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND issuer_url = $1
			ORDER BY event_sequence DESC LIMIT 1`, issuerURL)
		var revision int64
		scanErr := row.Scan(&revision, &evt.From, &evt.To, &evt.ActedBy, &evt.Authority, &evt.Reason, &evt.At)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		evt.Tenant, evt.IssuerURL, evt.Revision = tenant, issuerURL, uint32(revision)
		evt.At = evt.At.UTC()
		found = true
		return nil
	})
	if err != nil {
		return StateEvent{}, false, err
	}
	return evt, found, nil
}

// ListEvents implements [Store], oldest first -- the full, permanent
// transition history.
func (s *PGStore) ListEvents(tenant values.TenantId, issuerURL string) ([]StateEvent, error) {
	ctx := context.Background()
	var out []StateEvent
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT revision, from_status, to_status, acted_by, authority, reason, occurred_at
			FROM issuer_state_event
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND issuer_url = $1
			ORDER BY event_sequence ASC`, issuerURL)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var evt StateEvent
			var revision int64
			if err := rows.Scan(&revision, &evt.From, &evt.To, &evt.ActedBy, &evt.Authority, &evt.Reason, &evt.At); err != nil {
				return err
			}
			evt.Tenant, evt.IssuerURL, evt.Revision = tenant, issuerURL, uint32(revision)
			evt.At = evt.At.UTC()
			out = append(out, evt)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
