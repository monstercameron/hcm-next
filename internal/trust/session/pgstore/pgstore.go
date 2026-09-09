package pgstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// canonicalUUID matches a canonical, lowercase, hyphenated uuid string.
// This package deliberately does not import github.com/google/uuid:
// internal/trust is not among that module's allowed_import_roots in
// definitions/architecture/dependency-roles.yaml (LIB-002's third-party
// semantic firewall), which is not a file this lane may widen. A plain
// regex check is everything a validating adapter needs, matching
// internal/authn/issuerregistry/pgstore.go's own canonicalUUID exactly.
var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// assuranceWire and its inverse mirror trust.Assurance's own String()
// vocabulary (low/substantial/high), duplicated here because trust does not
// export a parser -- exactly the shape stepup's own unexported
// parseAssurance already uses for the same reason.
func parseAssurance(s string) (trust.Assurance, error) {
	for _, a := range []trust.Assurance{trust.AssuranceLow, trust.AssuranceSubstantial, trust.AssuranceHigh} {
		if a.String() == s {
			return a, nil
		}
	}
	return trust.AssuranceUnspecified, fmt.Errorf("pgstore: unknown assurance %q", s)
}

// parseStatus is [session.Status]'s own String() vocabulary, inverted. Like
// parseAssurance, this package cannot reach [session.Status]'s unexported
// String lookup table, so it is duplicated here from the same closed,
// documented wire vocabulary migration 00041's CHECK constraint also
// enforces.
func parseStatus(s string) (session.Status, error) {
	for _, st := range []session.Status{session.StatusActive, session.StatusExpiredIdle, session.StatusExpiredAbsolute, session.StatusRevoked} {
		if st.String() == s {
			return st, nil
		}
	}
	return session.StatusUnspecified, fmt.Errorf("pgstore: unknown session status %q", s)
}

// DB is the database capability [PGStore] needs: every operation opens its
// own transaction so it can establish (or, for the two pointer tables,
// deliberately not establish) migration 00008's row level security
// context, matching internal/authn/issuerregistry.DB's identical shape.
type DB interface {
	dbport.Beginner
}

// PGStore implements [session.Store] over migration 00041's tables.
type PGStore struct {
	db DB
}

var _ session.Store = (*PGStore)(nil)

// New returns a [PGStore] over db, typically a pgxadapter connection or
// pool already able to SET ROLE hcmnext_app.
func New(db DB) *PGStore {
	return &PGStore{db: db}
}

func validateTenant(tenant values.TenantId) error {
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("pgstore: tenant %q: %w", tenant, err)
	}
	if !canonicalUUID.MatchString(tenant.String()) {
		return fmt.Errorf("pgstore: tenant %q is not a valid uuid", tenant)
	}
	return nil
}

// withTenant validates tenant and runs fn inside a transaction with
// migration 00008/00041's row level security context established. fn's
// error, or a failure validating the tenant or setting its context, rolls
// the transaction back instead of committing it.
func (s *PGStore) withTenant(ctx context.Context, tenant values.TenantId, fn func(tx dbport.Tx) error) error {
	if err := validateTenant(tenant); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: begin transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, tenancy.SessionSetting, tenant.String()); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("pgstore: set %s: %w", tenancy.SessionSetting, err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// resolveTenant looks up the tenant a session id belongs to through
// trust_session_pointer -- the one table in this package deliberately not
// row-level-secured, precisely so this lookup is possible before any tenant
// context exists. See doc.go for why.
func (s *PGStore) resolveTenant(ctx context.Context, id session.ID) (values.TenantId, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("pgstore: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	var tenantStr string
	err = tx.QueryRow(ctx, `SELECT tenant_id::text FROM trust_session_pointer WHERE session_id = $1`, string(id)).Scan(&tenantStr)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", session.ErrSessionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("pgstore: resolve tenant for session %s: %w", id, err)
	}
	return values.TenantId(tenantStr), nil
}

// loadRow reads the current trust_session row for (tenant, id) within tx,
// which must already carry tenant's row level security context.
func (s *PGStore) loadRow(ctx context.Context, tx dbport.Tx, id session.ID) (session.StoreRecord, error) {
	var (
		sr                      session.StoreRecord
		assuranceStr, statusStr string
		idleSeconds             int64
	)
	sr.ID = id
	row := tx.QueryRow(ctx, `
		SELECT tenant_id::text, subject, principal_fingerprint, assurance, status, revoked_reason,
			created_at, last_activity_at, idle_timeout_seconds, absolute_expires_at,
			rotation_count, current_token_hash, current_generation, version
		FROM trust_session
		WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $1`, string(id))
	var tenantStr string
	err := row.Scan(&tenantStr, &sr.Subject, &sr.PrincipalFingerprint, &assuranceStr, &statusStr, &sr.RevokedReason,
		&sr.CreatedAt, &sr.LastActivityAt, &idleSeconds, &sr.AbsoluteExpiresAt,
		&sr.RotationCount, &sr.CurrentTokenHash, &sr.Generation, &sr.Version)
	if errors.Is(err, dbport.ErrNoRows) {
		return session.StoreRecord{}, session.ErrSessionNotFound
	}
	if err != nil {
		return session.StoreRecord{}, fmt.Errorf("pgstore: load session %s: %w", id, err)
	}
	sr.Tenant = values.TenantId(tenantStr)
	sr.Assurance, err = parseAssurance(assuranceStr)
	if err != nil {
		return session.StoreRecord{}, err
	}
	sr.Status, err = parseStatus(statusStr)
	if err != nil {
		return session.StoreRecord{}, err
	}
	sr.IdleTimeout = time.Duration(idleSeconds) * time.Second
	sr.CreatedAt = sr.CreatedAt.UTC()
	sr.LastActivityAt = sr.LastActivityAt.UTC()
	sr.AbsoluteExpiresAt = sr.AbsoluteExpiresAt.UTC()
	return sr, nil
}

// insertEvidence records one evidence row for id within tx.
func (s *PGStore) insertEvidence(ctx context.Context, tx dbport.Tx, id session.ID, kind session.EvidenceKind, reason string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO trust_session_evidence (tenant_id, session_id, kind, reason, occurred_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4)`,
		string(id), kind.String(), reason, at)
	if err != nil {
		return fmt.Errorf("pgstore: record %s evidence for session %s: %w", kind, id, err)
	}
	return nil
}

// applyExpiry runs [session.EvaluateExpiry] against sr and, if it reports a
// transition, durably persists the new status (under sr's own version) and
// its EXPIRED evidence within tx, mutating sr in place. It never errors on
// a benign lost race against a concurrent identical expiry write: if the
// compare-and-swap affects zero rows, the row is re-read and whatever is
// now on file (already transitioned by a racing reader, or superseded by a
// real revoke/rotate) is what sr becomes -- exactly like
// internal/data/tenancy.Bootstrap's own race-loser resolution.
func (s *PGStore) applyExpiry(ctx context.Context, tx dbport.Tx, sr *session.StoreRecord, now time.Time) error {
	next, reason, ok := session.EvaluateExpiry(sr.Status, sr.LastActivityAt, sr.AbsoluteExpiresAt, sr.IdleTimeout, now)
	if !ok {
		return nil
	}
	affected, err := tx.Exec(ctx, `
		UPDATE trust_session SET status = $1, revoked_reason = $2, version = version + 1
		WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $3 AND version = $4`,
		next.String(), reason, string(sr.ID), sr.Version)
	if err != nil {
		return fmt.Errorf("pgstore: persist expiry for session %s: %w", sr.ID, err)
	}
	if affected == 0 {
		fresh, loadErr := s.loadRow(ctx, tx, sr.ID)
		if loadErr != nil {
			return loadErr
		}
		*sr = fresh
		return nil
	}
	if err := s.insertEvidence(ctx, tx, sr.ID, session.EvidenceExpired, reason, now); err != nil {
		return err
	}
	sr.Status = next
	sr.RevokedReason = reason
	sr.Version++
	return nil
}

// Create implements [session.Store].
func (s *PGStore) Create(ctx context.Context, rec session.StoreRecord) error {
	return s.withTenant(ctx, rec.Tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO trust_session_pointer (session_id, tenant_id)
			VALUES ($1, current_setting('app.tenant_id')::uuid)`,
			string(rec.ID)); err != nil {
			return fmt.Errorf("pgstore: create pointer for session %s: %w", rec.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO trust_session (
				tenant_id, session_id, subject, principal_fingerprint, assurance, status, revoked_reason,
				created_at, last_activity_at, idle_timeout_seconds, absolute_expires_at,
				rotation_count, current_token_hash, current_generation, version
			) VALUES (
				current_setting('app.tenant_id')::uuid, $1,$2,$3,$4,$5,'',$6,$7,$8,$9,$10,$11,$12,$13
			)`,
			string(rec.ID), rec.Subject, rec.PrincipalFingerprint, rec.Assurance.String(), rec.Status.String(),
			rec.CreatedAt, rec.LastActivityAt, int64(rec.IdleTimeout/time.Second), rec.AbsoluteExpiresAt,
			rec.RotationCount, rec.CurrentTokenHash, rec.Generation, rec.Version,
		); err != nil {
			return fmt.Errorf("pgstore: create session %s: %w", rec.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO trust_session_refresh_generation (tenant_id, session_id, generation, token_hash)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3)`,
			string(rec.ID), rec.Generation, rec.CurrentTokenHash); err != nil {
			return fmt.Errorf("pgstore: record generation 0 for session %s: %w", rec.ID, err)
		}
		return s.insertEvidence(ctx, tx, rec.ID, session.EvidenceCreated, "", rec.CreatedAt)
	})
}

// Get implements [session.Store].
func (s *PGStore) Get(ctx context.Context, id session.ID, now time.Time) (session.StoreRecord, error) {
	tenant, err := s.resolveTenant(ctx, id)
	if err != nil {
		return session.StoreRecord{}, err
	}
	var out session.StoreRecord
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		sr, err := s.loadRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.applyExpiry(ctx, tx, &sr, now); err != nil {
			return err
		}
		out = sr
		return nil
	})
	if err != nil {
		return session.StoreRecord{}, err
	}
	return out, nil
}

// Touch implements [session.Store].
func (s *PGStore) Touch(ctx context.Context, id session.ID, now time.Time) (session.StoreRecord, error) {
	tenant, err := s.resolveTenant(ctx, id)
	if err != nil {
		return session.StoreRecord{}, err
	}
	var out session.StoreRecord
	var notActiveErr error
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		sr, err := s.loadRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.applyExpiry(ctx, tx, &sr, now); err != nil {
			return err
		}
		if sr.Status != session.StatusActive {
			out = sr
			notActiveErr = fmt.Errorf("%w: %s", session.ErrSessionNotActive, sr.Status)
			return nil
		}
		affected, err := tx.Exec(ctx, `
			UPDATE trust_session SET last_activity_at = $1, version = version + 1
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $2 AND version = $3`,
			now, string(id), sr.Version)
		if err != nil {
			return fmt.Errorf("pgstore: touch session %s: %w", id, err)
		}
		if affected == 0 {
			fresh, err := s.loadRow(ctx, tx, id)
			if err != nil {
				return err
			}
			out = fresh
			return nil
		}
		sr.LastActivityAt = now
		sr.Version++
		out = sr
		return nil
	})
	if err != nil {
		return session.StoreRecord{}, err
	}
	return out, notActiveErr
}

// Revoke implements [session.Store].
func (s *PGStore) Revoke(ctx context.Context, id session.ID, reason string, now time.Time) (session.StoreRecord, error) {
	tenant, err := s.resolveTenant(ctx, id)
	if err != nil {
		return session.StoreRecord{}, err
	}
	var out session.StoreRecord
	var notActiveErr error
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		sr, err := s.loadRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.applyExpiry(ctx, tx, &sr, now); err != nil {
			return err
		}
		if sr.Status != session.StatusActive {
			out = sr
			notActiveErr = fmt.Errorf("%w: %s", session.ErrSessionNotActive, sr.Status)
			return nil
		}
		affected, err := tx.Exec(ctx, `
			UPDATE trust_session SET status = $1, revoked_reason = $2, version = version + 1
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $3 AND version = $4`,
			session.StatusRevoked.String(), reason, string(id), sr.Version)
		if err != nil {
			return fmt.Errorf("pgstore: revoke session %s: %w", id, err)
		}
		if affected == 0 {
			fresh, err := s.loadRow(ctx, tx, id)
			if err != nil {
				return err
			}
			out = fresh
			if fresh.Status == session.StatusActive {
				notActiveErr = fmt.Errorf("pgstore: revoke session %s: concurrent write refused this compare-and-swap", id)
			}
			return nil
		}
		if err := s.insertEvidence(ctx, tx, id, session.EvidenceRevoked, reason, now); err != nil {
			return err
		}
		sr.Status = session.StatusRevoked
		sr.RevokedReason = reason
		sr.Version++
		out = sr
		return nil
	})
	if err != nil {
		return session.StoreRecord{}, err
	}
	return out, notActiveErr
}

// Refresh implements [session.Store]. See doc.go for the two-step shape
// (resolve, then act within tenant scope) and package pgstore's own doc.go
// for why trust_session_refresh_generation is not row-level-secured.
func (s *PGStore) Refresh(ctx context.Context, presentedHash, newHash string, now time.Time) (session.StoreRecord, error) {
	tx0, err := s.db.Begin(ctx)
	if err != nil {
		return session.StoreRecord{}, fmt.Errorf("pgstore: begin transaction: %w", err)
	}
	var tenantStr, sessionIDStr string
	var presentedGeneration int64
	scanErr := tx0.QueryRow(ctx, `
		SELECT tenant_id::text, session_id, generation
		FROM trust_session_refresh_generation
		WHERE token_hash = $1`, presentedHash).Scan(&tenantStr, &sessionIDStr, &presentedGeneration)
	_ = tx0.Rollback(ctx)
	if errors.Is(scanErr, dbport.ErrNoRows) {
		return session.StoreRecord{}, session.ErrRefreshUnknown
	}
	if scanErr != nil {
		return session.StoreRecord{}, fmt.Errorf("pgstore: resolve refresh token: %w", scanErr)
	}
	tenant := values.TenantId(tenantStr)
	id := session.ID(sessionIDStr)

	var out session.StoreRecord
	var outErr error
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		sr, err := s.loadRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.applyExpiry(ctx, tx, &sr, now); err != nil {
			return err
		}

		if presentedGeneration != sr.Generation {
			// The presented hash names a generation this session has
			// already rotated past: a replay. Revoke the whole family.
			if sr.Status == session.StatusActive {
				if _, err := tx.Exec(ctx, `
					UPDATE trust_session SET status = $1, revoked_reason = $2, version = version + 1
					WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $3 AND version = $4`,
					session.StatusRevoked.String(), session.ReasonRefreshReplay, string(id), sr.Version); err != nil {
					return fmt.Errorf("pgstore: revoke session %s on replay: %w", id, err)
				}
				if err := s.insertEvidence(ctx, tx, id, session.EvidenceRevoked, session.ReasonRefreshReplay, now); err != nil {
					return err
				}
			}
			outErr = session.ErrRefreshReplay
			return nil
		}

		if sr.Status != session.StatusActive {
			out = sr
			outErr = fmt.Errorf("%w: %s", session.ErrSessionNotActive, sr.Status)
			return nil
		}

		// Advance the generation ledger first: two callers racing to
		// rotate the exact same presented generation both attempt to
		// insert (tenant_id, session_id, presentedGeneration+1) and
		// UNIQUE(token_hash) besides, so the second collides on a genuine
		// unique-violation rather than a read-then-write window.
		if _, err := tx.Exec(ctx, `
			INSERT INTO trust_session_refresh_generation (tenant_id, session_id, generation, token_hash)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3)`,
			string(id), presentedGeneration+1, newHash); err != nil {
			return fmt.Errorf("pgstore: advance refresh generation for session %s: %w", id, err)
		}
		affected, err := tx.Exec(ctx, `
			UPDATE trust_session
			SET current_token_hash = $1, current_generation = $2, rotation_count = rotation_count + 1,
				last_activity_at = $3, version = version + 1
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $4
				AND version = $5 AND current_generation = $6`,
			newHash, presentedGeneration+1, now, string(id), sr.Version, presentedGeneration)
		if err != nil {
			return fmt.Errorf("pgstore: rotate session %s: %w", id, err)
		}
		if affected == 0 {
			return fmt.Errorf("pgstore: rotate session %s: compare-and-swap refused (concurrent write already advanced this session)", id)
		}
		if err := s.insertEvidence(ctx, tx, id, session.EvidenceRotated, "", now); err != nil {
			return err
		}
		sr.CurrentTokenHash = newHash
		sr.Generation = presentedGeneration + 1
		sr.RotationCount++
		sr.LastActivityAt = now
		sr.Version++
		out = sr
		return nil
	})
	if err != nil {
		return session.StoreRecord{}, err
	}
	if outErr != nil {
		return out, outErr
	}
	return out, nil
}

// RecordEvidence implements [session.Store].
func (s *PGStore) RecordEvidence(ctx context.Context, id session.ID, kind session.EvidenceKind, reason string, at time.Time) error {
	tenant, err := s.resolveTenant(ctx, id)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		return s.insertEvidence(ctx, tx, id, kind, reason, at)
	})
}

// Evidence implements [session.Store].
func (s *PGStore) Evidence(ctx context.Context, id session.ID) ([]session.Evidence, error) {
	tenant, err := s.resolveTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []session.Evidence
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT evidence_id, kind, reason, occurred_at
			FROM trust_session_evidence
			WHERE tenant_id = current_setting('app.tenant_id')::uuid AND session_id = $1
			ORDER BY occurred_at ASC, evidence_id ASC`, string(id))
		if err != nil {
			return fmt.Errorf("pgstore: list evidence for session %s: %w", id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var ev session.Evidence
			var kindStr string
			if err := rows.Scan(&ev.ID, &kindStr, &ev.Reason, &ev.At); err != nil {
				return fmt.Errorf("pgstore: scan evidence for session %s: %w", id, err)
			}
			kind, err := parseEvidenceKind(kindStr)
			if err != nil {
				return err
			}
			ev.SessionID = id
			ev.Kind = kind
			ev.At = ev.At.UTC()
			out = append(out, ev)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseEvidenceKind is [session.EvidenceKind]'s own String() vocabulary,
// inverted, for the same reason parseStatus and parseAssurance exist.
func parseEvidenceKind(s string) (session.EvidenceKind, error) {
	for _, k := range []session.EvidenceKind{
		session.EvidenceCreated, session.EvidenceRotated, session.EvidenceDenied,
		session.EvidenceRevoked, session.EvidenceExpired,
	} {
		if k.String() == s {
			return k, nil
		}
	}
	return session.EvidenceUnspecified, fmt.Errorf("pgstore: unknown evidence kind %q", s)
}
