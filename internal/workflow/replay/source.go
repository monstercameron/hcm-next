package replay

import "context"

// Source hands a [Replayer] the durable record of one instance.
//
// It is a port rather than a concrete store because the two callers that
// matter read from different places and neither should have to pretend to be
// the other: an operator tool replaying an exported run holds the record
// already ([MemorySource]), and a live investigation reads it out of the
// durable tables ([StoreSource]). Both hand back the same shape, which is what
// lets the whole of this package's own test matrix run without a database
// while the pgtest-backed case proves the database path produces the same
// answer.
//
// A Source only reads. There is no Save, on purpose.
type Source interface {
	Load(ctx context.Context) (Record, error)
}

// MemorySource is a record held in process.
//
// It clones on construction and on every Load, so a caller mutating the record
// it handed over cannot change what a replay in flight reads, and two replays
// of the same source never share a backing array. That is what makes the
// PROPERTY case -- replaying twice is byte-identical -- a property of the
// package rather than of the caller's discipline.
type MemorySource struct{ rec Record }

var _ Source = (*MemorySource)(nil)

// NewMemorySource returns a source over a copy of rec.
func NewMemorySource(rec Record) *MemorySource {
	return &MemorySource{rec: rec.Clone()}
}

// Load implements [Source].
func (s *MemorySource) Load(context.Context) (Record, error) {
	if s == nil {
		return Record{}, refuse(CodeSourceFailed, "", "no source")
	}
	return s.rec.Clone(), nil
}

// Record returns a copy of the record this source holds, for a caller that
// wants to assert on it without replaying.
func (s *MemorySource) Record() Record { return s.rec.Clone() }

// sourceFunc adapts a plain function to [Source]. It is unexported because a
// caller that wants one writes it in three lines; it exists so this package's
// own fault fixtures can inject a failing source without a type per case.
type sourceFunc func(context.Context) (Record, error)

func (f sourceFunc) Load(ctx context.Context) (Record, error) { return f(ctx) }
