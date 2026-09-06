// Package subscriptionstore persists immutable subscription revisions and
// authorization-decision evidence. It stores no event payload and makes no
// delivery or provider call.
package subscriptionstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/subscription"
)

// DB is the transaction-opening capability used by the adapter.
type DB interface{ dbport.Beginner }

// Executor is useful to callers that need to inspect rows inside an existing
// transaction; Store itself opens and scopes a transaction for each operation.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

type StoreCode string

const (
	CodeInvalid   StoreCode = "INVALID"
	CodeDuplicate StoreCode = "DUPLICATE"
	CodeNotFound  StoreCode = "NOT_FOUND"
	CodeStaleCAS  StoreCode = "STALE_CAS"
)

// StoreError is the stable typed failure returned by persistence operations.
type StoreError struct {
	Code   StoreCode
	Detail string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "subscriptionstore: persistence error"
	}
	if e.Detail == "" {
		return "subscriptionstore: " + string(e.Code)
	}
	return fmt.Sprintf("subscriptionstore: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Is(target error) bool {
	t, ok := target.(*StoreError)
	return ok && e != nil && t != nil && e.Code == t.Code
}

var (
	ErrInvalid   = &StoreError{Code: CodeInvalid}
	ErrDuplicate = &StoreError{Code: CodeDuplicate}
	ErrNotFound  = &StoreError{Code: CodeNotFound}
	ErrStaleCAS  = &StoreError{Code: CodeStaleCAS}
)

func storeError(code StoreCode, detail string) error { return &StoreError{Code: code, Detail: detail} }

// CodeOf returns the stable persistence code carried by err.
func CodeOf(err error) StoreCode {
	var typed *StoreError
	if errors.As(err, &typed) && typed != nil {
		return typed.Code
	}
	return ""
}

// Store is bound to a database and, when provided, one tenant. The optional
// tenant makes the vocabulary-facing Repository methods safe: a durable row
// never relies on a stale session tenant, and the domain's tenant scope string
// remains distinct from the database tenant UUID used for RLS.
type Store struct {
	db     DB
	tenant uuid.UUID
}

func New(db DB, tenant ...uuid.UUID) *Store {
	s := &Store{db: db}
	if len(tenant) > 0 {
		s.tenant = tenant[0]
	}
	return s
}

func NewForTenant(db DB, tenantID uuid.UUID) *Store { return New(db, tenantID) }

var (
	_ subscription.Repository              = (*Store)(nil)
	_ subscription.AuthorizationRepository = (*Store)(nil)
)

// Append implements the domain revision port using the store's bound tenant.
func (s *Store) Append(revision subscription.EventSubscription) error {
	if s == nil {
		return storeError(CodeInvalid, "store is nil")
	}
	if s.tenant == uuid.Nil {
		return storeError(CodeInvalid, "store tenant is required")
	}
	return s.AppendRevision(context.Background(), s.tenant, revision)
}

// Revisions implements the domain revision port. The error-returning
// LoadRevisions method is available to callers that need diagnostic detail.
func (s *Store) Revisions(subscriptionID string) []subscription.EventSubscription {
	if s == nil || s.tenant == uuid.Nil {
		return nil
	}
	result, err := s.LoadRevisions(context.Background(), s.tenant, subscriptionID)
	if err != nil {
		return nil
	}
	return result
}

// Active implements the domain revision port.
func (s *Store) Active() []subscription.EventSubscription {
	if s == nil || s.tenant == uuid.Nil {
		return nil
	}
	result, err := s.LoadActive(context.Background(), s.tenant)
	if err != nil {
		return nil
	}
	return result
}

// AppendAuthorization implements the domain authorization port. Sequence is
// allocated from the durable stream inside the same transaction.
func (s *Store) AppendAuthorization(event subscription.AuthorizationEvent) error {
	if s == nil || s.tenant == uuid.Nil {
		return storeError(CodeInvalid, "store tenant is required")
	}
	return s.AppendAuthorizationEvent(context.Background(), s.tenant, event, 0)
}

func (s *Store) AuthorizationEvents(subscriptionID string) []subscription.AuthorizationEvent {
	if s == nil || s.tenant == uuid.Nil {
		return nil
	}
	result, err := s.LoadAuthorizationEvents(context.Background(), s.tenant, subscriptionID)
	if err != nil {
		return nil
	}
	return result
}

// AppendRevision appends exactly the next immutable revision for a tenant.
func (s *Store) AppendRevision(ctx context.Context, tenantID uuid.UUID, revision subscription.EventSubscription) error {
	if err := revision.Validate(); err != nil {
		return storeError(CodeInvalid, err.Error())
	}
	if err := s.validateTenant(tenantID, revision.TenantScope); err != nil {
		return err
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		current, err := maxRevision(ctx, tx, tenantID, revision.SubscriptionID)
		if err != nil {
			return err
		}
		if err := checkNextRevision(revision.Revision, current); err != nil {
			return err
		}
		if err := revision.Verify(); err != nil {
			return storeError(CodeInvalid, err.Error())
		}
		kinds, fields, filter, subscriber, err := encodeRevision(revision)
		if err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO event_subscription (
				row_id, tenant_id, subscription_id, revision, state, requester, approver,
				subscriber, event_kinds, declared_fields, filter, delivery_endpoint_ref,
				delivery_guarantee, tenant_scope, organization_scope_ref,
				population_scope_ref, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb,$12,$13,$14,$15,$16,$17)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, revision.SubscriptionID, int64(revision.Revision),
			string(revision.State), nullString(revision.Requester), nullString(revision.Approver),
			subscriber, string(kinds), nullableJSON(fields), nullableJSON(filter),
			revision.DeliveryEndpointRef, string(revision.DeliveryGuarantee), revision.TenantScope,
			nullString(revision.OrganizationScopeRef), nullString(revision.PopulationScopeRef), revision.Digest)
		if err != nil {
			return fmt.Errorf("subscriptionstore: insert revision: %w", err)
		}
		if affected == 0 {
			return storeError(CodeDuplicate, "subscription revision already exists")
		}
		return nil
	})
}

// Save is an explicit alias for AppendRevision.
func (s *Store) Save(ctx context.Context, tenantID uuid.UUID, revision subscription.EventSubscription) error {
	return s.AppendRevision(ctx, tenantID, revision)
}

// LoadRevisions reloads one subscription's complete immutable history.
func (s *Store) LoadRevisions(ctx context.Context, tenantID uuid.UUID, subscriptionID string) ([]subscription.EventSubscription, error) {
	if strings.TrimSpace(subscriptionID) == "" {
		return nil, storeError(CodeInvalid, "subscription id is required")
	}
	var result []subscription.EventSubscription
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT subscription_id, revision, state, COALESCE(requester,''), COALESCE(approver,''),
				subscriber, event_kinds, declared_fields, filter, delivery_endpoint_ref,
				delivery_guarantee, tenant_scope, COALESCE(organization_scope_ref,''),
				COALESCE(population_scope_ref,''), digest
			FROM event_subscription
			WHERE tenant_id=$1 AND subscription_id=$2
			ORDER BY revision`, tenantID, subscriptionID)
		if err != nil {
			return fmt.Errorf("subscriptionstore: load revisions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			revision, err := scanRevision(rows)
			if err != nil {
				return err
			}
			result = append(result, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, storeError(CodeNotFound, "subscription has no stored revisions")
	}
	return result, nil
}

func (s *Store) LoadActive(ctx context.Context, tenantID uuid.UUID) ([]subscription.EventSubscription, error) {
	var result []subscription.EventSubscription
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT e.subscription_id, e.revision, e.state, COALESCE(e.requester,''), COALESCE(e.approver,''),
				e.subscriber, e.event_kinds, e.declared_fields, e.filter, e.delivery_endpoint_ref,
				e.delivery_guarantee, e.tenant_scope, COALESCE(e.organization_scope_ref,''),
				COALESCE(e.population_scope_ref,''), e.digest
			FROM event_subscription e
			JOIN (
				SELECT subscription_id, max(revision) AS revision
				FROM event_subscription WHERE tenant_id=$1
				GROUP BY subscription_id
			) latest ON latest.subscription_id=e.subscription_id AND latest.revision=e.revision
			WHERE e.tenant_id=$1 AND e.state='ACTIVE'
			ORDER BY e.subscription_id`, tenantID)
		if err != nil {
			return fmt.Errorf("subscriptionstore: load active revisions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			revision, err := scanRevision(rows)
			if err != nil {
				return err
			}
			result = append(result, revision)
		}
		return rows.Err()
	})
	return result, err
}

// AppendAuthorizationEvent appends one authorization decision at sequence.
// Passing sequence zero allocates the next sequence for the bound operation.
func (s *Store) AppendAuthorizationEvent(ctx context.Context, tenantID uuid.UUID, event subscription.AuthorizationEvent, sequence uint64) error {
	if err := validateAuthorizationEvent(event); err != nil {
		return err
	}
	if tenantID == uuid.Nil {
		return storeError(CodeInvalid, "tenant id must be a non-nil UUID")
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if sequence == 0 {
			var current *int64
			if err := tx.QueryRow(ctx, `SELECT max(event_sequence) FROM subscription_authorization_event WHERE tenant_id=$1 AND subscription_id=$2`, tenantID, event.SubscriptionID).Scan(&current); err != nil {
				return fmt.Errorf("subscriptionstore: allocate authorization sequence: %w", err)
			}
			if current == nil {
				sequence = 1
			} else {
				sequence = uint64(*current) + 1
			}
		}
		current, err := maxAuthorizationSequence(ctx, tx, tenantID, event.SubscriptionID)
		if err != nil {
			return err
		}
		if err := checkNextEventSequence(sequence, current); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO subscription_authorization_event (
				row_id, tenant_id, subscription_id, revision, allowed, rule, reason,
				grant_digest, decision_digest, digest, event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, event.SubscriptionID, int64(event.Revision), event.Allowed,
			nullString(event.Rule), nullString(event.Reason), nullString(event.GrantDigest),
			nullString(event.DecisionDigest), event.Digest, int64(sequence))
		if err != nil {
			return fmt.Errorf("subscriptionstore: insert authorization event: %w", err)
		}
		if affected == 0 {
			return storeError(CodeDuplicate, "authorization event sequence already exists")
		}
		return nil
	})
}

func (s *Store) LoadAuthorizationEvents(ctx context.Context, tenantID uuid.UUID, subscriptionID string) ([]subscription.AuthorizationEvent, error) {
	if strings.TrimSpace(subscriptionID) == "" {
		return nil, storeError(CodeInvalid, "subscription id is required")
	}
	var result []subscription.AuthorizationEvent
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT subscription_id, revision, allowed, COALESCE(rule,''), COALESCE(reason,''),
				COALESCE(grant_digest,''), COALESCE(decision_digest,''), digest
			FROM subscription_authorization_event
			WHERE tenant_id=$1 AND subscription_id=$2
			ORDER BY event_sequence`, tenantID, subscriptionID)
		if err != nil {
			return fmt.Errorf("subscriptionstore: load authorization events: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var event subscription.AuthorizationEvent
			if err := rows.Scan(&event.SubscriptionID, &event.Revision, &event.Allowed, &event.Rule, &event.Reason, &event.GrantDigest, &event.DecisionDigest, &event.Digest); err != nil {
				return fmt.Errorf("subscriptionstore: scan authorization event: %w", err)
			}
			result = append(result, event)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, storeError(CodeNotFound, "subscription has no authorization events")
	}
	return result, nil
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return storeError(CodeInvalid, "database is nil")
	}
	if tenantID == uuid.Nil {
		return storeError(CodeInvalid, "tenant id must be a non-nil UUID")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("subscriptionstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("subscriptionstore: commit: %w", err)
	}
	return nil
}

func (s *Store) validateTenant(tenantID uuid.UUID, tenantScope string) error {
	if tenantID == uuid.Nil {
		return storeError(CodeInvalid, "tenant id must be a non-nil UUID")
	}
	if strings.TrimSpace(tenantScope) == "" || tenantScope != strings.TrimSpace(tenantScope) {
		return storeError(CodeInvalid, "subscription tenant scope is required and may not be padded")
	}
	return nil
}

func maxRevision(ctx context.Context, q Executor, tenantID uuid.UUID, subscriptionID string) (*int64, error) {
	var current *int64
	if err := q.QueryRow(ctx, `SELECT max(revision) FROM event_subscription WHERE tenant_id=$1 AND subscription_id=$2`, tenantID, subscriptionID).Scan(&current); err != nil {
		return nil, fmt.Errorf("subscriptionstore: read revision head: %w", err)
	}
	return current, nil
}

func maxAuthorizationSequence(ctx context.Context, q Executor, tenantID uuid.UUID, subscriptionID string) (*int64, error) {
	var current *int64
	if err := q.QueryRow(ctx, `SELECT max(event_sequence) FROM subscription_authorization_event WHERE tenant_id=$1 AND subscription_id=$2`, tenantID, subscriptionID).Scan(&current); err != nil {
		return nil, fmt.Errorf("subscriptionstore: read authorization head: %w", err)
	}
	return current, nil
}

func checkNextRevision(revision uint64, current *int64) error {
	if revision == 0 || revision > uint64(^uint64(0)>>1) {
		return storeError(CodeInvalid, "revision must fit a positive database integer")
	}
	if current == nil {
		if revision != 1 {
			return storeError(CodeStaleCAS, "first subscription revision must be 1")
		}
		return nil
	}
	want := uint64(*current)
	if revision == want {
		return storeError(CodeDuplicate, "subscription revision already exists")
	}
	if revision != want+1 {
		return storeError(CodeStaleCAS, fmt.Sprintf("subscription revision must follow %d", *current))
	}
	return nil
}

func checkNextEventSequence(sequence uint64, current *int64) error {
	if sequence == 0 || sequence > uint64(^uint64(0)>>1) {
		return storeError(CodeInvalid, "event sequence must fit a positive database integer")
	}
	if current == nil {
		if sequence != 1 {
			return storeError(CodeStaleCAS, "first authorization event sequence must be 1")
		}
		return nil
	}
	want := uint64(*current)
	if sequence == want {
		return storeError(CodeDuplicate, "authorization event sequence already exists")
	}
	if sequence != want+1 {
		return storeError(CodeStaleCAS, fmt.Sprintf("authorization event sequence must follow %d", *current))
	}
	return nil
}

func encodeRevision(revision subscription.EventSubscription) ([]byte, []byte, []byte, string, error) {
	kinds, err := json.Marshal(revision.EventKinds)
	if err != nil {
		return nil, nil, nil, "", storeError(CodeInvalid, "encode event kinds: "+err.Error())
	}
	fields, err := json.Marshal(revision.DeclaredFields)
	if err != nil {
		return nil, nil, nil, "", storeError(CodeInvalid, "encode declared fields: "+err.Error())
	}
	filter, err := json.Marshal(revision.Filter)
	if err != nil {
		return nil, nil, nil, "", storeError(CodeInvalid, "encode filter: "+err.Error())
	}
	subscriber, err := json.Marshal(revision.Subscriber)
	if err != nil {
		return nil, nil, nil, "", storeError(CodeInvalid, "encode subscriber: "+err.Error())
	}
	return kinds, fields, filter, string(subscriber), nil
}

func scanRevision(row dbport.Row) (subscription.EventSubscription, error) {
	var (
		revision              subscription.EventSubscription
		state, subscriber     string
		kinds, fields, filter []byte
	)
	if err := row.Scan(&revision.SubscriptionID, &revision.Revision, &state, &revision.Requester, &revision.Approver, &subscriber, &kinds, &fields, &filter, &revision.DeliveryEndpointRef, &revision.DeliveryGuarantee, &revision.TenantScope, &revision.OrganizationScopeRef, &revision.PopulationScopeRef, &revision.Digest); err != nil {
		return subscription.EventSubscription{}, fmt.Errorf("subscriptionstore: scan revision: %w", err)
	}
	revision.State = subscription.LifecycleState(state)
	if err := json.Unmarshal([]byte(subscriber), &revision.Subscriber); err != nil {
		return subscription.EventSubscription{}, fmt.Errorf("subscriptionstore: decode subscriber: %w", err)
	}
	if err := json.Unmarshal(kinds, &revision.EventKinds); err != nil {
		return subscription.EventSubscription{}, fmt.Errorf("subscriptionstore: decode event kinds: %w", err)
	}
	if len(fields) > 0 {
		if err := json.Unmarshal(fields, &revision.DeclaredFields); err != nil {
			return subscription.EventSubscription{}, fmt.Errorf("subscriptionstore: decode declared fields: %w", err)
		}
	}
	if len(filter) > 0 {
		if err := json.Unmarshal(filter, &revision.Filter); err != nil {
			return subscription.EventSubscription{}, fmt.Errorf("subscriptionstore: decode filter: %w", err)
		}
	}
	return revision, nil
}

func validateAuthorizationEvent(event subscription.AuthorizationEvent) error {
	if strings.TrimSpace(event.SubscriptionID) == "" || event.Revision == 0 || event.Rule == "" || !validDigest(event.Digest) {
		return storeError(CodeInvalid, "authorization event requires subscription, revision, rule and digest")
	}
	if event.GrantDigest != "" && !validDigest(event.GrantDigest) {
		return storeError(CodeInvalid, "authorization grant digest is not sha256")
	}
	if event.DecisionDigest != "" && !validDigest(event.DecisionDigest) {
		return storeError(CodeInvalid, "authorization decision digest is not sha256")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableJSON(value []byte) any {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return string(value)
}
