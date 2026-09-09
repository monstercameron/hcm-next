package configregistry

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

// PutObject implements [platformconfig.Store]. It inserts o as a new,
// immutable row. [platformconfig.Publish] only calls PutObject once it has
// already confirmed (via [Store.GetObject]) that no row exists for o.Ref(),
// so an ordinary INSERT is correct for the intended call path; a genuine
// race between two concurrent Publish calls for the same key surfaces as a
// primary key violation, which this method reports rather than swallows —
// P1B minimal depth does not attempt to resolve that race itself.
func (s *Store) PutObject(o platformconfig.ConfigurationObject) error {
	ctx := context.Background()
	return s.withTenant(ctx, o.Scope.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO config_object (
				tenant_id, cell_id, kind, object_id, revision,
				canonical_body_digest, body, record_digest,
				schema_ref, publisher_principal, published_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			o.Scope.TenantID, o.Scope.CellID, string(o.Kind), o.ID, int64(o.Revision),
			o.CanonicalBodyDigest, o.Body, o.Digest(),
			o.SchemaRef, o.PublisherPrincipal, o.PublishedAt,
		)
		return err
	})
}

// rehydrateRow turns one scanned config_object row into a
// [platformconfig.ConfigurationObject] whose Digest() and Verify() behave
// exactly as one minted in-process by [platformconfig.Publish] would — see
// [platformconfig.Rehydrate].
func rehydrateRow(
	scope platformconfig.Scope, kind platformconfig.Kind, id string, revision int64,
	canonicalBodyDigest string, body []byte, recordDigest string,
	schemaRef, publisherPrincipal string, publishedAt time.Time,
) (platformconfig.ConfigurationObject, error) {
	obj := platformconfig.ConfigurationObject{
		Kind:                kind,
		ID:                  id,
		Revision:            uint32(revision),
		Body:                body,
		CanonicalBodyDigest: canonicalBodyDigest,
		SchemaRef:           schemaRef,
		Scope:               scope,
		PublisherPrincipal:  publisherPrincipal,
		PublishedAt:         publishedAt,
	}
	return platformconfig.Rehydrate(obj, recordDigest)
}

// GetObject implements [platformconfig.Store].
func (s *Store) GetObject(ref platformconfig.ObjectRef) (platformconfig.ConfigurationObject, bool, error) {
	ctx := context.Background()
	var (
		revision                          int64
		canonicalBodyDigest, recordDigest string
		body                              []byte
		schemaRef, publisherPrincipal     string
		publishedAt                       time.Time
		found                             bool
	)
	err := s.withTenant(ctx, ref.Scope.TenantID, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT revision, canonical_body_digest, body, record_digest, schema_ref, publisher_principal, published_at
			FROM config_object
			WHERE tenant_id = $1 AND cell_id = $2 AND kind = $3 AND object_id = $4 AND revision = $5`,
			ref.Scope.TenantID, ref.Scope.CellID, string(ref.Kind), ref.ID, int64(ref.Revision),
		)
		scanErr := row.Scan(&revision, &canonicalBodyDigest, &body, &recordDigest, &schemaRef, &publisherPrincipal, &publishedAt)
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
		return platformconfig.ConfigurationObject{}, false, err
	}
	if !found {
		return platformconfig.ConfigurationObject{}, false, nil
	}
	obj, err := rehydrateRow(ref.Scope, ref.Kind, ref.ID, revision, canonicalBodyDigest, body, recordDigest, schemaRef, publisherPrincipal, publishedAt)
	if err != nil {
		return platformconfig.ConfigurationObject{}, false, err
	}
	return obj, true, nil
}

// ListRevisions implements [platformconfig.Store], oldest published first.
func (s *Store) ListRevisions(scope platformconfig.Scope, kind platformconfig.Kind, id string) ([]platformconfig.ConfigurationObject, error) {
	ctx := context.Background()
	var out []platformconfig.ConfigurationObject
	err := s.withTenant(ctx, scope.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT revision, canonical_body_digest, body, record_digest, schema_ref, publisher_principal, published_at
			FROM config_object
			WHERE tenant_id = $1 AND cell_id = $2 AND kind = $3 AND object_id = $4
			ORDER BY revision ASC`,
			scope.TenantID, scope.CellID, string(kind), id,
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				revision                          int64
				canonicalBodyDigest, recordDigest string
				body                              []byte
				schemaRef, publisherPrincipal     string
				publishedAt                       time.Time
			)
			if err := rows.Scan(&revision, &canonicalBodyDigest, &body, &recordDigest, &schemaRef, &publisherPrincipal, &publishedAt); err != nil {
				return err
			}
			obj, err := rehydrateRow(scope, kind, id, revision, canonicalBodyDigest, body, recordDigest, schemaRef, publisherPrincipal, publishedAt)
			if err != nil {
				return err
			}
			out = append(out, obj)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
