package workitem

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	CodeAuthorityChanged = "AUTHORITY_CHANGED"
	CodeSessionRevoked   = "SESSION_REVOKED"
	CodeSessionInactive  = "SESSION_INACTIVE"
)

// SessionRevocationPort is the session lifecycle port used by the opt-in
// completion path. It deliberately carries a string reference so the human
// work package does not construct or own trust-session identifiers.
type SessionRevocationPort interface {
	CheckRevocation(context.Context, string, time.Time) error
}

// RevocationChecker is a short alias for callers that prefer the driver
// vocabulary used by internal/trust/session.
type RevocationChecker = SessionRevocationPort

// AuthorityRecheckRequest is the current item and completion context a
// caller-supplied authority adapter must evaluate immediately before the CAS.
// The adapter owns how it reads current directory, delegation, SoD and policy
// facts through ex; this package only owns the boundary and the transition.
type AuthorityRecheckRequest struct {
	TenantID   uuid.UUID
	Item       WorkItem
	Completion CompleteInput
	SessionRef string
	CheckedAt  time.Time
}

// AuthorityRecheckDecision is the current authority result. A denied result
// is projected to CodeAuthorityChanged and no WorkItem write is attempted.
type AuthorityRecheckDecision struct {
	Allowed     bool
	DecisionRef string
	Reason      string
}

// AuthorityRecheckPort resolves the completer's authority from current facts,
// not from the item's historical assignment alone.
type AuthorityRecheckPort interface {
	Recheck(context.Context, Executor, AuthorityRecheckRequest) (AuthorityRecheckDecision, error)
}

// CurrentAuthorityPort is an explicit name for the same additive port.
type CurrentAuthorityPort = AuthorityRecheckPort

// CompleteWithAuthorityRecheckInput opts a completion into current session
// and authority validation. CompleteInput remains embedded so all existing
// callers can keep using Store.Complete unchanged.
type CompleteWithAuthorityRecheckInput struct {
	CompleteInput
	SessionRef string
	Session    SessionRevocationPort
	Authority  AuthorityRecheckPort
}

// CompletionPort is the additive Port surface for a driver that opts into
// current authority and session checks. The historical Port interface is
// intentionally left source-compatible for callers that only use Complete.
type CompletionPort interface {
	Port
	CompleteWithAuthorityRecheck(context.Context, Executor, CompleteWithAuthorityRecheckInput) (WorkItem, error)
}

// CompleteWithAuthorityRecheck rechecks the current session and authority
// before completing a claimed item. It performs no write before both checks
// succeed, then delegates to Complete for the existing item-version CAS and
// append-only transition pair. A caller that does not opt in sees the exact
// historical Complete behavior.
func (s Store) CompleteWithAuthorityRecheck(
	ctx context.Context, ex Executor, in CompleteWithAuthorityRecheckInput,
) (WorkItem, error) {
	if in.Session == nil || in.Authority == nil || in.SessionRef == "" {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"authority-rechecked completion requires session reference and both current-state ports")
	}
	if in.Now.IsZero() {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"authority-rechecked completion requires Now")
	}
	if err := in.Meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	if !semanticKey(in.CompletedBy) || !ValidDigest(in.CompletedOutputDigest) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"authority-rechecked completion carries an invalid completer or output digest")
	}

	current, err := s.Load(ctx, ex, in.TenantID, in.WorkItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != in.ExpectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, in.TenantID, in.WorkItemID, in.ExpectedVersion)
	}

	if err := in.Session.CheckRevocation(ctx, in.SessionRef, in.Now.UTC()); err != nil {
		return WorkItem{}, sessionRefusal(in.WorkItemID.String(), err)
	}
	authority, err := in.Authority.Recheck(ctx, ex, AuthorityRecheckRequest{
		TenantID: in.TenantID, Item: current, Completion: in.CompleteInput,
		SessionRef: in.SessionRef, CheckedAt: in.Now.UTC(),
	})
	if err != nil {
		return WorkItem{}, wrap(CodeAuthorityChanged, in.WorkItemID.String(), err,
			"current authority recheck failed")
	}
	if !authority.Allowed {
		return WorkItem{}, refuse(CodeAuthorityChanged, in.WorkItemID.String(),
			"current authority refused completion: %s", authority.Reason)
	}

	return s.Complete(ctx, ex, in.CompleteInput)
}

type sessionCodeCarrier interface{ Code() string }

func sessionRefusal(workItemID string, err error) error {
	var carrier sessionCodeCarrier
	if errors.As(err, &carrier) && carrier.Code() == CodeSessionRevoked {
		return wrap(CodeSessionRevoked, workItemID, err, "session revocation check refused completion")
	}
	return wrap(CodeSessionInactive, workItemID, err, "session check refused completion")
}

var _ CompletionPort = Store{}
