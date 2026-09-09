package promotionterminal

import (
	"errors"
	"fmt"
	"slices"

	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
)

var ErrPlanBinding = errors.New("promotion terminal: transaction plan binding is invalid")

// BindResolution materializes the admitted participant set from the immutable
// consistency-boundary resolution derived from a TransactionPlan. It also
// proves that every cross-boundary participant is represented by exactly one
// declared outbox effect. Callers therefore cannot hand the promotion writer
// a callback list that silently drops a local stream or invokes a remote one.
func BindResolution(resolution transaction.Resolution, cmd domaincommit.Command) (domaincommit.Command, error) {
	if err := resolution.VerifyDigest(); err != nil {
		return domaincommit.Command{}, fmt.Errorf("%w: %v", ErrPlanBinding, err)
	}
	if resolution.PlanID == "" || len(resolution.Admitted) == 0 {
		return domaincommit.Command{}, fmt.Errorf("%w: resolution has no plan or admitted participant", ErrPlanBinding)
	}

	participants := make([]string, 0, len(resolution.Admitted))
	for _, participant := range resolution.Admitted {
		if !participant.Local {
			return domaincommit.Command{}, fmt.Errorf("%w: admitted participant %s is not local", ErrPlanBinding, participant.ParticipantID)
		}
		participants = append(participants, participant.ParticipantID)
	}
	if err := cmd.ValidateParticipants(participants); err != nil {
		return domaincommit.Command{}, fmt.Errorf("%w: %v", ErrPlanBinding, err)
	}

	effectIDs := make([]string, 0, len(cmd.Effects))
	for _, effect := range cmd.Effects {
		effectIDs = append(effectIDs, effect.EffectID)
	}
	if len(effectIDs) != len(resolution.Effects) {
		return domaincommit.Command{}, fmt.Errorf("%w: resolution has %d remote participants but command has %d effects", ErrPlanBinding, len(resolution.Effects), len(effectIDs))
	}
	for _, participant := range resolution.Effects {
		if participant.Local || !slices.Contains(effectIDs, participant.ParticipantID) {
			return domaincommit.Command{}, fmt.Errorf("%w: remote participant %s has no exact outbox effect", ErrPlanBinding, participant.ParticipantID)
		}
	}

	cmd.PlanID = resolution.PlanID
	cmd.PlanDigest = resolution.Digest
	cmd.PlanParticipants = participants
	return cmd, nil
}
