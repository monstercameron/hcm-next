package recordsmeta

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	// ErrMissingNotice is returned when a notice or acknowledgement omits a
	// field its evidentiary value depends on.
	ErrMissingNotice = errors.New("recordsmeta: hold notice field missing")
)

// HoldNoticeTables is the exact set of base tables migration 00284 adds for
// the hold notice/acknowledgement family, sorted.
var HoldNoticeTables = []string{
	"legal_hold_acknowledgement",
	"legal_hold_notice",
}

// HoldNotice is durable evidence that a hold was issued to a named
// custodian or recipient. It is append-only: a notice is never edited to
// say it was acknowledged, an acknowledgement is recorded against it
// instead.
type HoldNotice struct {
	TenantID      uuid.UUID `json:"tenant_id"`
	NoticeID      uuid.UUID `json:"notice_id"`
	HoldID        uuid.UUID `json:"hold_id"`
	RecipientRef  string    `json:"recipient_ref"`
	RecipientRole string    `json:"recipient_role"`
	IssuedBy      string    `json:"issued_by"`
	IssuedAt      time.Time `json:"issued_at"`
	Method        string    `json:"method"`
	ContentDigest string    `json:"content_digest"`
}

func (n HoldNotice) Validate() error {
	if n.TenantID == uuid.Nil || n.NoticeID == uuid.Nil || n.HoldID == uuid.Nil {
		return ErrNilTenant
	}
	if n.RecipientRef == "" {
		return detail(ErrMissingNotice, "legal_hold_notice.recipient_ref")
	}
	if !oneOf(n.RecipientRole, "CUSTODIAN", "RECIPIENT", "COUNSEL") {
		return detail(ErrInvalidEnum, "legal_hold_notice.recipient_role=%q", n.RecipientRole)
	}
	if n.IssuedBy == "" || n.Method == "" || n.ContentDigest == "" {
		return detail(ErrMissingNotice, "legal_hold_notice issued_by/method/content_digest")
	}
	return nil
}

// InsertHoldNotice records that a hold was issued to a recipient.
func InsertHoldNotice(ctx context.Context, tx dbport.Tx, n HoldNotice) error {
	if err := n.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, n.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO legal_hold_notice (tenant_id, notice_id, hold_id, recipient_ref, recipient_role, issued_by, issued_at, method, content_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		n.TenantID, n.NoticeID, n.HoldID, n.RecipientRef, n.RecipientRole, n.IssuedBy, n.IssuedAt, n.Method, n.ContentDigest)
	return err
}

// LoadHoldNotice returns one notice.
func LoadHoldNotice(ctx context.Context, q dbport.Querier, tenantID, noticeID uuid.UUID) (HoldNotice, error) {
	var n HoldNotice
	err := q.QueryRow(ctx, `SELECT tenant_id, notice_id, hold_id, recipient_ref, recipient_role, issued_by, issued_at, method, content_digest FROM legal_hold_notice WHERE tenant_id=$1 AND notice_id=$2`, tenantID, noticeID).
		Scan(&n.TenantID, &n.NoticeID, &n.HoldID, &n.RecipientRef, &n.RecipientRole, &n.IssuedBy, &n.IssuedAt, &n.Method, &n.ContentDigest)
	if err != nil {
		return HoldNotice{}, err
	}
	return n, nil
}

// HoldAcknowledgement is durable evidence that one specific notice was
// acknowledged. legal_hold_acknowledgement_unique caps it at one
// acknowledgement per notice.
type HoldAcknowledgement struct {
	TenantID          uuid.UUID `json:"tenant_id"`
	AcknowledgementID uuid.UUID `json:"acknowledgement_id"`
	NoticeID          uuid.UUID `json:"notice_id"`
	HoldID            uuid.UUID `json:"hold_id"`
	AcknowledgedBy    string    `json:"acknowledged_by"`
	AcknowledgedAt    time.Time `json:"acknowledged_at"`
	Method            string    `json:"method"`
	ContentDigest     string    `json:"content_digest"`
}

func (a HoldAcknowledgement) Validate() error {
	if a.TenantID == uuid.Nil || a.AcknowledgementID == uuid.Nil || a.NoticeID == uuid.Nil || a.HoldID == uuid.Nil {
		return ErrNilTenant
	}
	if a.AcknowledgedBy == "" || a.Method == "" || a.ContentDigest == "" {
		return detail(ErrMissingNotice, "legal_hold_acknowledgement acknowledged_by/method/content_digest")
	}
	return nil
}

// InsertHoldAcknowledgement records that a notice was acknowledged.
func InsertHoldAcknowledgement(ctx context.Context, tx dbport.Tx, a HoldAcknowledgement) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, a.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO legal_hold_acknowledgement (tenant_id, acknowledgement_id, notice_id, hold_id, acknowledged_by, acknowledged_at, method, content_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		a.TenantID, a.AcknowledgementID, a.NoticeID, a.HoldID, a.AcknowledgedBy, a.AcknowledgedAt, a.Method, a.ContentDigest)
	return err
}

// LoadHoldAcknowledgement returns one acknowledgement.
func LoadHoldAcknowledgement(ctx context.Context, q dbport.Querier, tenantID, acknowledgementID uuid.UUID) (HoldAcknowledgement, error) {
	var a HoldAcknowledgement
	err := q.QueryRow(ctx, `SELECT tenant_id, acknowledgement_id, notice_id, hold_id, acknowledged_by, acknowledged_at, method, content_digest FROM legal_hold_acknowledgement WHERE tenant_id=$1 AND acknowledgement_id=$2`, tenantID, acknowledgementID).
		Scan(&a.TenantID, &a.AcknowledgementID, &a.NoticeID, &a.HoldID, &a.AcknowledgedBy, &a.AcknowledgedAt, &a.Method, &a.ContentDigest)
	if err != nil {
		return HoldAcknowledgement{}, err
	}
	return a, nil
}

// NoticeAcknowledged reports whether a notice has a recorded acknowledgement
// and, if so, when. It is a join, never a status column on the notice
// itself: the notice row is never rewritten to say it was acknowledged.
func NoticeAcknowledged(ctx context.Context, q dbport.Querier, tenantID, noticeID uuid.UUID) (bool, *time.Time, error) {
	var at time.Time
	err := q.QueryRow(ctx, `SELECT acknowledged_at FROM legal_hold_acknowledgement WHERE tenant_id=$1 AND notice_id=$2`, tenantID, noticeID).Scan(&at)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, err
	}
	return true, &at, nil
}
