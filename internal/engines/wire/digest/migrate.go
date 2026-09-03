package digest

import (
	"runtime/debug"
	"sort"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// Comparison records what changed between the old and new envelopes. It is
// evidence, not a decision: a migration approver reads it.
type Comparison struct {
	MaterialPathsAdded   []string
	MaterialPathsRemoved []string
	CanonicalLengthDelta int64
	AlgorithmChanged     bool
	DigestChanged        bool
}

// Migration links an old digest envelope and a new one for the same authorized
// source object. It never overwrites the old digest: both envelopes are carried
// forward, and both keep verifying.
type Migration struct {
	MigrationID string
	Old         Reference
	New         Reference
	From        Key
	To          Key
	Reason      string
	Approver    string
	// Implementation records the build that performed the migration.
	Implementation string
	RecordedAt     time.Time
	Comparison     Comparison
}

// DualDigests returns the old and new references as a dual-digest set. During a
// declared transition both must verify; see [Registry.VerifyDual].
func (m Migration) DualDigests() []Reference { return []Reference{m.Old, m.New} }

// MigrationOptions supplies the accountability fields a migration record must
// carry: who authorized it, why, and when.
type MigrationOptions struct {
	Reason   string
	Approver string
	// AlgorithmID migrates the algorithm as well as the profile. Empty keeps
	// the old reference's algorithm.
	AlgorithmID string
	// Scope overrides the scope derived from the message, matching whatever
	// was supplied when the old reference was computed.
	Scope *Scope
	// Now supplies the recorded time. Nil uses time.Now.
	Now func() time.Time
	// NewID supplies the migration identifier. Nil generates a UUID.
	NewID func() string
}

// Migrate verifies old against src, computes a new envelope under newProfile,
// and returns the linking record. It fails rather than migrating when the old
// reference does not verify: a digest that cannot be reproduced has nothing to
// migrate from.
//
// src is required because a digest cannot be recomputed from an envelope alone;
// the authorized source object is what both envelopes are bound to.
func (r *Registry) Migrate(src proto.Message, old Reference, newProfile Key, opts MigrationOptions) (Migration, error) {
	if opts.Reason == "" || opts.Approver == "" {
		return Migration{}, newError("Migrate", ErrInvalidReference,
			"a migration must record a reason and an approver")
	}
	if err := r.VerifyWithScope(src, old, opts.Scope); err != nil {
		return Migration{}, err
	}
	algID := opts.AlgorithmID
	if algID == "" {
		algID = old.AlgorithmID
	}
	next, _, err := r.ComputeWith(src, newProfile, Options{
		AlgorithmID:        algID,
		Scope:              opts.Scope,
		MaterialProfileRef: old.MaterialProfileRef,
	})
	if err != nil {
		return Migration{}, err
	}
	next.MaterialProfileRef = Ptr(newProfile.String())

	oldProfile, err := r.Profile(old.Key())
	if err != nil {
		return Migration{}, err
	}
	newProfileDef, err := r.Profile(newProfile)
	if err != nil {
		return Migration{}, err
	}

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	newID := func() string { return uuid.NewString() }
	if opts.NewID != nil {
		newID = opts.NewID
	}

	return Migration{
		MigrationID:    newID(),
		Old:            old,
		New:            next,
		From:           old.Key(),
		To:             newProfile,
		Reason:         opts.Reason,
		Approver:       opts.Approver,
		Implementation: implementation(),
		RecordedAt:     now().UTC(),
		Comparison: Comparison{
			MaterialPathsAdded:   difference(newProfileDef.MaterialPaths(), oldProfile.MaterialPaths()),
			MaterialPathsRemoved: difference(oldProfile.MaterialPaths(), newProfileDef.MaterialPaths()),
			CanonicalLengthDelta: int64(next.CanonicalLength) - int64(old.CanonicalLength),
			AlgorithmChanged:     next.AlgorithmID != old.AlgorithmID,
			DigestChanged:        next.Digest != old.Digest,
		},
	}, nil
}

// implementation identifies the build that performed a migration, so evidence
// can name the encoder that produced each envelope.
func implementation() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, modified := "", ""
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = "+modified"
			}
		}
	}
	if rev == "" {
		return bi.GoVersion
	}
	return bi.GoVersion + " " + rev + modified
}

func difference(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
