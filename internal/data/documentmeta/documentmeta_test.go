package documentmeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documentmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newTemplate(tenant uuid.UUID) documentmeta.DocumentTemplate {
	return documentmeta.DocumentTemplate{
		TenantID:              tenant,
		TemplateID:            uuid.New(),
		TemplateKey:           "tpl:" + uuid.NewString(),
		TemplateVersion:       1,
		Purpose:               "OFFER_LETTER",
		SourceLocale:          "en-US",
		ParameterSchema:       json.RawMessage(`{"salary":"money"}`),
		ContentDigest:         digestOf("template-" + uuid.NewString()),
		LegalApproval:         "APPROVED",
		AccessibilityApproval: "APPROVED",
		EffectiveFrom:         fixedInstant,
		PublicationState:      "PUBLISHED",
	}
}

func newDocument(tenant uuid.UUID) documentmeta.Document {
	return documentmeta.Document{
		TenantID:       tenant,
		DocumentID:     uuid.New(),
		DocumentType:   "OFFER_LETTER",
		OwnerRef:       "principal:hr",
		SubjectRef:     "person:" + uuid.NewString(),
		Classification: "CONFIDENTIAL",
		Compartment:    "HR",
		Lifecycle:      "DRAFT",
		CreatedAt:      fixedInstant,
		UpdatedAt:      fixedInstant,
	}
}

func newVersion(tenant, documentID uuid.UUID, templateID uuid.UUID, number int64) documentmeta.DocumentVersion {
	sealed := fixedInstant.Add(time.Minute)
	templateVersion := int64(1)
	return documentmeta.DocumentVersion{
		TenantID:                tenant,
		VersionID:               uuid.New(),
		DocumentID:              documentID,
		VersionNumber:           number,
		TemplateID:              &templateID,
		TemplateVersion:         &templateVersion,
		RenderVersion:           1,
		CanonicalizationVersion: 1,
		SourceArtifactDigest:    digestOf("source-" + uuid.NewString()),
		RenderedArtifactDigest:  digestOf("rendered-" + uuid.NewString()),
		Locale:                  "en-US",
		JurisdictionRef:         "US-FL",
		Classification:          "CONFIDENTIAL",
		CreatedAt:               fixedInstant,
		SealedAt:                &sealed,
		Status:                  "SEALED",
	}
}

func newRequest(tenant, versionID uuid.UUID) documentmeta.SignatureRequest {
	return documentmeta.SignatureRequest{
		TenantID:           tenant,
		RequestID:          uuid.New(),
		VersionID:          versionID,
		AssuranceMode:      "NATIVE_EVIDENCE",
		SignerRequirements: json.RawMessage(`{"order":["candidate","hr"]}`),
		DeadlineAt:         fixedInstant.Add(72 * time.Hour),
		CreatedAt:          fixedInstant,
		Status:             "OPEN",
	}
}

func newSignature(tenant, requestID, versionID uuid.UUID, signedDigest string) documentmeta.Signature {
	return documentmeta.Signature{
		TenantID:          tenant,
		SignatureID:       uuid.New(),
		RequestID:         requestID,
		VersionID:         versionID,
		SignerRef:         "principal:" + uuid.NewString(),
		SignedDigest:      signedDigest,
		SignatureValueRef: "artifact://sig/" + uuid.NewString(),
		CertificateRef:    "kms://cert/1",
		IdentityAssurance: "IAL2",
		SignedAt:          fixedInstant.Add(time.Hour),
		ValidityState:     "VALID",
	}
}

type paper struct {
	templateID, documentID, versionID, requestID uuid.UUID
	renderedDigest                               string
}

func writePaper(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID) paper {
	t.Helper()
	ctx := context.Background()
	var p paper
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		template := newTemplate(tenant)
		if err := documentmeta.InsertDocumentTemplate(ctx, tx, template); err != nil {
			return err
		}
		doc := newDocument(tenant)
		if err := documentmeta.InsertDocument(ctx, tx, doc); err != nil {
			return err
		}
		version := newVersion(tenant, doc.DocumentID, template.TemplateID, 1)
		if err := documentmeta.InsertDocumentVersion(ctx, tx, version); err != nil {
			return err
		}
		if err := documentmeta.SetCurrentVersion(ctx, tx, tenant, doc.DocumentID, version.VersionID, fixedInstant); err != nil {
			return err
		}
		request := newRequest(tenant, version.VersionID)
		if err := documentmeta.InsertSignatureRequest(ctx, tx, request); err != nil {
			return err
		}
		p = paper{template.TemplateID, doc.DocumentID, version.VersionID, request.RequestID, version.RenderedArtifactDigest}
		return nil
	})
	return p
}

// TestTodo_DB_014_Documents is DB-014's content-side test. The matrix names
// live in internal/data/integrationmeta (the lane's primary package); this
// suite proves the same clauses for the six document, template, signature and
// artifact-reference tables migration 00031 creates.
func TestTodo_DB_014_Documents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db014-documents")
	other := insertTenant(t, db, "db014-documents-other")

	t.Run("the document table set is exactly what 00031 declares", func(t *testing.T) {
		want := []string{
			"document", "document_artifact_reference", "document_template",
			"document_version", "signature", "signature_request",
		}
		if !slices.Equal(documentmeta.DocumentTables, want) {
			t.Fatalf("DocumentTables=%v, want %v", documentmeta.DocumentTables, want)
		}
		for _, table := range want {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("a document round trips through its sealed version", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			doc, err := documentmeta.LoadDocument(ctx, tx, tenant, p.documentID)
			if err != nil {
				return err
			}
			if doc.CurrentVersionID == nil || *doc.CurrentVersionID != p.versionID {
				return fmt.Errorf("document %+v does not point at its sealed version", doc)
			}
			if doc.Lifecycle != "ACTIVE" || doc.Classification == "" {
				return fmt.Errorf("document %+v lost its lifecycle or classification", doc)
			}
			v, err := documentmeta.LoadDocumentVersion(ctx, tx, tenant, p.versionID)
			if err != nil {
				return err
			}
			if v.RenderVersion < 1 || v.CanonicalizationVersion < 1 || v.TemplateVersion == nil {
				return fmt.Errorf("version %+v lost a version it was rendered with", v)
			}
			return nil
		})
	})

	t.Run("a signature binds the exact rendered bytes", func(t *testing.T) {
		p := writePaper(t, conn, tenant)

		wrong := newSignature(tenant, p.requestID, p.versionID, digestOf("some other bytes"))
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertSignature(ctx, tx, wrong)
		})
		if !errors.Is(err, documentmeta.ErrDigestMismatch) {
			t.Fatalf("a signature over other bytes returned %v, want ErrDigestMismatch", err)
		}

		// The schema refuses the same thing without the Go layer: the
		// composite foreign key is over (tenant, version, rendered digest).
		if err := db.ExecErr(`INSERT INTO signature (tenant_id, signature_id, request_id, version_id, signer_ref, signed_digest, signature_value_ref, identity_assurance, signed_at) VALUES ($1,$2,$3,$4,'principal:x',$5,'artifact://sig/x','IAL2',now())`,
			tenant, uuid.New(), p.requestID, p.versionID, digestOf("some other bytes")); err == nil {
			t.Fatal("the schema accepted a signature over bytes the version never rendered")
		}

		right := newSignature(tenant, p.requestID, p.versionID, p.renderedDigest)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertSignature(ctx, tx, right)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			s, err := documentmeta.LoadSignature(ctx, tx, tenant, right.SignatureID)
			if err != nil {
				return err
			}
			if s.SignedDigest != p.renderedDigest {
				return fmt.Errorf("signature bound %s, version rendered %s", s.SignedDigest, p.renderedDigest)
			}
			return nil
		})
	})

	t.Run("a signature value is a reference, never the value", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		for _, ref := range []string{"", "MEUCIQD...", "https://provider/sig", "artifact:/x"} {
			s := newSignature(tenant, p.requestID, p.versionID, p.renderedDigest)
			s.SignatureValueRef = ref
			if err := s.Validate(); !errors.Is(err, documentmeta.ErrRawSignatureValue) {
				t.Errorf("signature value %q validated as %v, want ErrRawSignatureValue", ref, err)
			}
		}
		if err := db.ExecErr(`INSERT INTO signature (tenant_id, signature_id, request_id, version_id, signer_ref, signed_digest, signature_value_ref, identity_assurance, signed_at) VALUES ($1,$2,$3,$4,'principal:y',$5,'MEUCIQD','IAL2',now())`,
			tenant, uuid.New(), p.requestID, p.versionID, p.renderedDigest); err == nil {
			t.Fatal("the schema accepted a raw signature value")
		}
	})

	t.Run("one signer signs a request once", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		first := newSignature(tenant, p.requestID, p.versionID, p.renderedDigest)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertSignature(ctx, tx, first)
		})
		again := newSignature(tenant, p.requestID, p.versionID, p.renderedDigest)
		again.SignerRef = first.SignerRef
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertSignature(ctx, tx, again)
		}); err == nil {
			t.Fatal("the same signer signed one request twice")
		}
	})

	t.Run("an unapproved template never publishes", func(t *testing.T) {
		unapproved := newTemplate(tenant)
		unapproved.AccessibilityApproval = "PENDING"
		if err := unapproved.Validate(); !errors.Is(err, documentmeta.ErrUnapprovedPublication) {
			t.Fatalf("an unapproved template validated as %v, want ErrUnapprovedPublication", err)
		}
		if err := db.ExecErr(`INSERT INTO document_template (tenant_id, template_id, template_key, template_version, purpose, source_locale, content_digest, legal_approval, accessibility_approval, effective_from, publication_state) VALUES ($1,$2,$3,1,'OFFER','en-US',$4,'APPROVED','PENDING',now(),'PUBLISHED')`,
			tenant, uuid.New(), "tpl:"+uuid.NewString(), digestOf("t")); err == nil {
			t.Fatal("the schema published a template without its accessibility approval")
		}
	})

	t.Run("an artifact reference carries a pointer, never bytes", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		ref := documentmeta.ArtifactReference{
			TenantID: tenant, ReferenceID: uuid.New(), DocumentID: p.documentID,
			VersionID: &p.versionID, ContentID: p.renderedDigest,
			Purpose: "RENDERED", Classification: "CONFIDENTIAL",
			RetentionClass: "PERMANENT", HoldState: "NONE", ReferencedAt: fixedInstant,
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertArtifactReference(ctx, tx, ref)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := documentmeta.LoadArtifactReference(ctx, tx, tenant, ref.ReferenceID)
			if err != nil {
				return err
			}
			if loaded.ContentID != p.renderedDigest || loaded.DigestAlgorithm != "sha256" {
				return fmt.Errorf("artifact reference %+v lost its content id or algorithm", loaded)
			}
			return nil
		})

		// The table holds no byte-bearing column at all: the bytes stay in
		// the object store migration 00010 owns.
		var byteColumns int
		if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='document_artifact_reference' AND data_type IN ('bytea','text[]')`).Scan(&byteColumns); err != nil {
			t.Fatalf("inspect columns: %v", err)
		}
		if byteColumns != 0 {
			t.Fatalf("document_artifact_reference holds %d byte-bearing columns, want 0", byteColumns)
		}

		duplicate := ref
		duplicate.ReferenceID = uuid.New()
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertArtifactReference(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("the same artifact was referenced twice for the same document and purpose")
		}
	})

	t.Run("sealed versions, signatures and references reject rewrites", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		sig := newSignature(tenant, p.requestID, p.versionID, p.renderedDigest)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertSignature(ctx, tx, sig)
		})
		if err := db.ExecErr(`UPDATE document_version SET locale='fr-FR' WHERE tenant_id=$1 AND version_id=$2`, tenant, p.versionID); err == nil {
			t.Fatal("a sealed document version was rewritten")
		}
		if err := db.ExecErr(`DELETE FROM signature WHERE tenant_id=$1 AND signature_id=$2`, tenant, sig.SignatureID); err == nil {
			t.Fatal("a signature was deleted")
		}
		if err := db.ExecErr(`UPDATE document SET lifecycle='ARCHIVED' WHERE tenant_id=$1 AND document_id=$2`, tenant, p.documentID); err != nil {
			t.Fatalf("document is live serving state and must stay updatable: %v", err)
		}
	})

	t.Run("a draft version never becomes the current one", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		draft := newVersion(tenant, p.documentID, p.templateID, 2)
		draft.Status = "DRAFT"
		draft.SealedAt = nil
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.InsertDocumentVersion(ctx, tx, draft)
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return documentmeta.SetCurrentVersion(ctx, tx, tenant, p.documentID, draft.VersionID, fixedInstant)
		})
		if err == nil {
			t.Fatal("a DRAFT version became the document's current version")
		}
	})

	t.Run("one tenant cannot reach another tenant's documents", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			_, err := documentmeta.LoadDocumentVersion(ctx, tx, tenant, p.versionID)
			return err
		})
		if !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("another tenant read this tenant's document version: %v", err)
		}
		crossTenant := newRequest(other, p.versionID)
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			return documentmeta.InsertSignatureRequest(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("another tenant opened a signature request over this tenant's version")
		}
	})

	t.Run("an external ceremony always names its provider and its deadline", func(t *testing.T) {
		p := writePaper(t, conn, tenant)
		noProvider := newRequest(tenant, p.versionID)
		noProvider.AssuranceMode = "EXTERNAL_PROVIDER"
		if err := noProvider.Validate(); !errors.Is(err, documentmeta.ErrInvalidEnum) {
			t.Fatalf("an external ceremony with no provider validated as %v", err)
		}
		noDeadline := newRequest(tenant, p.versionID)
		noDeadline.DeadlineAt = time.Time{}
		if err := noDeadline.Validate(); !errors.Is(err, documentmeta.ErrMissingDeadline) {
			t.Fatalf("a request with no deadline validated as %v", err)
		}
	})
}
