package execute

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
)

var (
	ErrMessageBeforeEffectivePoint = errors.New("workflow execute: message is before the governed effective point")
	ErrTeamNotificationOrdering    = errors.New("workflow execute: employee notification must precede team notification")
)

// MessageReleaseRequest is the execute-side ordering proof for a message
// effect. It contains only governed timing and ordering evidence; releasing
// the provider-independent MessageIntent remains a separate operation.
type MessageReleaseRequest struct {
	Intent                       messaging.MessageIntent
	EffectivePoint               time.Time
	Now                          time.Time
	EmployeeNotificationRecorded bool
}

// AuthorizeMessageRelease refuses a team notification until the promotion's
// effective point is reached and the employee notification ordering constraint
// has been recorded. It has no provider side effect.
func AuthorizeMessageRelease(req MessageReleaseRequest) error {
	if err := req.Intent.Validate(); err != nil {
		return fmt.Errorf("workflow execute: validate message intent: %w", err)
	}
	if req.EffectivePoint.IsZero() || req.Now.IsZero() {
		return errors.New("workflow execute: message release requires effective point and Now")
	}
	if req.Now.Before(req.EffectivePoint) {
		return ErrMessageBeforeEffectivePoint
	}
	if strings.EqualFold(strings.TrimSpace(req.Intent.AudienceExpression), "TEAM") && !req.EmployeeNotificationRecorded {
		return ErrTeamNotificationOrdering
	}
	return nil
}
