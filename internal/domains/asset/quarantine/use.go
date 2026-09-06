package quarantine

import "context"

// Use is the gate every other capability (preview, extraction, indexing, a
// workflow step, a provider call) must call before treating a content id as
// safe to read. It refuses by state: only [Admitted] is granted. A content
// id that is still [Quarantined], one that was [Rejected], and one [Store]
// has never heard of at all (reported by [Store.CurrentState] as
// [ErrNotFound]) are all refused identically -- there is no fourth,
// implicit "safe by default" outcome.
//
// Use returns the [StateRecord] on success so a caller can, for example,
// echo the admitting scanner's identity into its own audit trail; on refusal
// it returns the zero [StateRecord] and a typed [ErrUseRefused] (or the
// underlying [ErrNotFound], for a content id [Store] has no record of at
// all).
//
// Use never returns bytes itself: retrieving an admitted artifact's actual
// content is a separate call into whatever byte store holds it (for the
// artifacts this package promotes, internal/data/artifacts.Retrieve). Use is
// strictly the policy checkpoint in front of that call, not a replacement
// for it.
func Use(ctx context.Context, store Store, tenant, contentID string) (StateRecord, error) {
	rec, err := store.CurrentState(ctx, tenant, contentID)
	if err != nil {
		return StateRecord{}, err
	}
	if rec.State != Admitted {
		return StateRecord{}, ErrUseRefused{ContentID: contentID, State: rec.State, Reason: rec.Reason}
	}
	return rec, nil
}
