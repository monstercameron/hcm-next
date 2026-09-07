package location

import (
	"context"
	"errors"
	"fmt"
)

// ErrWorksiteRevisionDrift reports a stored worksite revision whose canonical
// digest no longer matches the revision the caller holds.
var ErrWorksiteRevisionDrift = errors.New("location: stored worksite revision differs from the expected revision")

// VerifyWorksiteRevision reads expected's revision back through r and refuses
// when the stored record's canonical digest differs from expected's own, so a
// caller carrying a revision in hand proves the store still agrees before it
// acts on that revision. It is the in-process consumer of [WorksiteReader].
func VerifyWorksiteRevision(ctx context.Context, r WorksiteReader, expected WorksiteRevision) error {
	if r == nil {
		return errors.New("location: worksite reader is required")
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	want, err := expected.Digest()
	if err != nil {
		return err
	}
	stored, err := r.Worksite(ctx, expected.id(), expected.Revision)
	if err != nil {
		return err
	}
	got, err := stored.Digest()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w: revision %d stored %s, expected %s", ErrWorksiteRevisionDrift, expected.Revision, got, want)
	}
	return nil
}
