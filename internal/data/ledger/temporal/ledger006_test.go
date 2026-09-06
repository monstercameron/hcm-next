package temporal_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/temporal"
)

// TestTodo_LEDGER_006 proves the bitemporal query and state reconstruction
// APIs: all six modes answer from the ledger with authority labels and
// source event IDs, a correction recorded after a knowledge horizon never
// appears in a query taken at that horizon, the half-open effective window
// never returns a fact twice, and an external observation is never presented
// as domain truth.
func TestTodo_LEDGER_006(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	dec := f.decision()

	t.Run("reconstruct separates domain truth from unpromoted evidence", func(t *testing.T) {
		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), dec)
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}

		domain := byField(state.Domain)
		if got := string(domain[fieldTitle].Payload); got != "title:analyst" {
			t.Fatalf("job title reconstructs to %q, want the domain fact %q", got, "title:analyst")
		}
		if got := domain[fieldTitle].TruthClass; got != temporal.TruthDomain {
			t.Fatalf("job title truth class is %q, want %q", got, temporal.TruthDomain)
		}
		// The external observation is effective later and recorded later than
		// the domain fact. Under a naive latest-wins rule it would be the
		// answer; it must instead be evidence.
		for _, a := range state.Domain {
			if a.AssertionClass == datalogger.ExternalObservation {
				t.Fatalf("external observation %s@%d appears as domain truth", a.Ref.StreamKey, a.Ref.Sequence)
			}
		}
		unpromoted := payloadsOf(state.Unpromoted)
		if !slices.Contains(unpromoted, "title:from-adp") {
			t.Fatalf("unpromoted evidence is %v, want the external observation in it", unpromoted)
		}
		if !slices.Contains(unpromoted, "title:claimed-director") {
			t.Fatalf("unpromoted evidence is %v, want the claim in it", unpromoted)
		}
		if got := len(state.Transaction); got != 1 {
			t.Fatalf("state holds %d transaction records, want 1", got)
		}
		if got := string(state.Transaction[0].Payload); got != "badge:issued" {
			t.Fatalf("transaction record is %q, want %q", got, "badge:issued")
		}
	})

	t.Run("a correction wins and its target is superseded", func(t *testing.T) {
		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), dec)
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		base := byField(state.Domain)[fieldBase]
		if got := string(base.Payload); got != "base:120" {
			t.Fatalf("compensation base reconstructs to %q, want the corrected %q", got, "base:120")
		}
		if base.Ref.Sequence != f.seq["base-corrected"] {
			t.Fatalf("compensation base comes from sequence %d, want the correction at %d",
				base.Ref.Sequence, f.seq["base-corrected"])
		}
		// A CORRECTION inherits the truth class of what it corrects.
		if base.TruthClass != temporal.TruthDomain {
			t.Fatalf("a correction of a domain fact has truth class %q, want %q", base.TruthClass, temporal.TruthDomain)
		}
		for _, a := range state.Domain {
			if a.Ref.Sequence == f.seq["base-original"] {
				t.Fatal("the superseded original still appears in the reconstructed state")
			}
		}
	})

	t.Run("a future-known correction never leaks into a historical known-as-of read", func(t *testing.T) {
		// The correction was recorded on 2026-05-01. A reconstruction whose
		// knowledge horizon is 2026-04-01 must answer with the original.
		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, april), dec)
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		base := byField(state.Domain)[fieldBase]
		if got := string(base.Payload); got != "base:100" {
			t.Fatalf("as known on %s the compensation base is %q, want the uncorrected %q",
				april.Format(time.RFC3339), got, "base:100")
		}

		// The same must hold for the delegated KNOWN_AS_OF mode.
		res, err := temporal.KnownAsOf(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Subject: subject, Field: fieldBase, KnownAt: april, EffectiveAt: june,
		}, dec)
		if err != nil {
			t.Fatalf("known-as-of: %v", err)
		}
		if got := payloadsOf(res.Assertions); len(got) != 1 || got[0] != "base:100" {
			t.Fatalf("known-as-of returned %v, want exactly the uncorrected value", got)
		}
	})

	t.Run("the half-open effective window never returns a fact twice", func(t *testing.T) {
		// The observation is effective exactly on 2026-04-01, the shared
		// boundary of these two adjacent windows.
		early, err := temporal.Between(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Subject: subject, EffectiveFrom: march, EffectiveTo: april, KnownAt: june,
		}, dec)
		if err != nil {
			t.Fatalf("between march..april: %v", err)
		}
		late, err := temporal.Between(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Subject: subject, EffectiveFrom: april, EffectiveTo: may, KnownAt: june,
		}, dec)
		if err != nil {
			t.Fatalf("between april..may: %v", err)
		}

		seen := map[int64]bool{}
		for _, a := range append(slices.Clone(early.Assertions), late.Assertions...) {
			if seen[a.Ref.Sequence] {
				t.Fatalf("sequence %d appears in both adjacent windows", a.Ref.Sequence)
			}
			seen[a.Ref.Sequence] = true
		}
		if !slices.Contains(payloadsOf(late.Assertions), "title:from-adp") {
			t.Fatalf("the boundary fact is in neither window: late window holds %v", payloadsOf(late.Assertions))
		}
		if slices.Contains(payloadsOf(early.Assertions), "title:from-adp") {
			t.Fatal("a fact effective exactly at EffectiveTo was returned by the window that excludes it")
		}
	})

	t.Run("every answer carries an authority label and a source event id", func(t *testing.T) {
		res, err := temporal.History(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Subject: subject, KnownAt: june,
		}, dec)
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		if len(res.Assertions) != 6 {
			t.Fatalf("history returned %d assertions, want the 6 recorded on the subject", len(res.Assertions))
		}
		for _, a := range res.Assertions {
			if a.SourceEventID == uuid.Nil {
				t.Fatalf("assertion %s@%d carries no source event id", a.Ref.StreamKey, a.Ref.Sequence)
			}
			if a.AssertionClass.RequiresAuthority() {
				if !a.Authority.Present || !a.Authority.Resolved {
					t.Fatalf("%s at %s@%d has authority label %+v, want a resolved one",
						a.AssertionClass, a.Ref.StreamKey, a.Ref.Sequence, a.Authority)
				}
				if a.Authority.Kind == "" || a.Authority.DomainScope == "" {
					t.Fatalf("authority label %+v is missing the assignment's kind or scope", a.Authority)
				}
				if !a.Authority.CoversEffectiveAt {
					t.Fatalf("authority %s does not cover the effective instant of %s@%d",
						a.Authority.Ref, a.Ref.StreamKey, a.Ref.Sequence)
				}
			}
		}
	})

	t.Run("current and effective-as-of resolve the same field differently", func(t *testing.T) {
		// As of March the observation (effective April) is not yet in scope.
		res, err := temporal.AsOf(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Subject: subject, Field: fieldTitle, EffectiveAt: march, KnownAt: june,
		}, dec)
		if err != nil {
			t.Fatalf("effective-as-of march: %v", err)
		}
		if got := payloadsOf(res.Assertions); len(got) != 1 || got[0] != "title:analyst" {
			t.Fatalf("effective-as-of march returned %v, want only the domain fact", got)
		}
		if !res.Coordinate.EffectiveAt.Equal(march) {
			t.Fatalf("result coordinate reports effective %s, want %s", res.Coordinate.EffectiveAt, march)
		}

		// CURRENT defaults both axes to the injected clock.
		now := june
		cur, err := temporal.Query(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Mode: temporal.ModeCurrent, Subject: subject, Field: fieldBase,
		}, dec, temporal.WithClock(func() time.Time { return now }))
		if err != nil {
			t.Fatalf("current: %v", err)
		}
		if got := payloadsOf(cur.Assertions); len(got) != 1 || got[0] != "base:120" {
			t.Fatalf("current returned %v, want the corrected compensation base", got)
		}
		if !cur.Coordinate.KnownAt.Equal(now) {
			t.Fatalf("current resolved at known-at %s, want the injected clock %s", cur.Coordinate.KnownAt, now)
		}
	})

	t.Run("reconstruct is refused through Query", func(t *testing.T) {
		_, err := temporal.Query(ctx, f.db.Conn, f.request(june, june), dec)
		var invalid temporal.ErrRequestInvalid
		if !errors.As(err, &invalid) {
			t.Fatalf("Query on RECONSTRUCT returned %v, want ErrRequestInvalid", err)
		}
	})
}

// TestTodo_LEDGER_006_Property proves the property the REFACTOR clause asks
// for: two independent query plans - one pushing the coordinate into SQL,
// one folding internal/data/bitemporal's own HISTORY answer in Go - always
// reconstruct the same state, over every coordinate and every authorization
// shape the fixture can express.
func TestTodo_LEDGER_006_Property(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)

	coordinates := []struct {
		name                 string
		effectiveAt, knownAt time.Time
	}{
		{"before anything is effective", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), june},
		{"at the first effective boundary", march, june},
		{"between boundaries", midMarch, june},
		{"at the observation boundary", april, june},
		{"after everything", june, june},
		{"before the correction was recorded", june, april},
		{"before anything was recorded", june, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	decisions := []struct {
		name string
		dec  temporal.Decision
	}{
		{"unrestricted", f.decision()},
		{"one field allowed", temporal.Decision{Tenant: f.tenant, AllowFields: []string{fieldBase}}},
		{"one field denied", temporal.Decision{Tenant: f.tenant, DenyFields: []string{fieldTitle}}},
		{"subject allowed", temporal.Decision{Tenant: f.tenant, AllowSubjects: []string{subject}}},
		{"nothing allowed", temporal.Decision{Tenant: f.tenant, AllowSubjects: []string{}}},
		{"knowledge ceiling", temporal.Decision{Tenant: f.tenant, MaxKnownAt: april}},
	}
	plans := []temporal.Plan{temporal.LedgerPlan{}, temporal.HistoryFoldPlan{}}

	for _, coord := range coordinates {
		for _, d := range decisions {
			t.Run(coord.name+"/"+d.name, func(t *testing.T) {
				if _, err := temporal.VerifyPlanEquivalence(ctx, f.db.Conn,
					f.request(coord.effectiveAt, coord.knownAt), d.dec, plans); err != nil {
					t.Fatalf("plans disagree: %v", err)
				}
			})
		}
	}
}

// TestTodo_LEDGER_006_Race proves the read path is safe to fan out: many
// goroutines reconstructing and querying the same subject concurrently all
// reach the same answer, and nothing in this package holds shared mutable
// state that could make one caller's labelling visible to another.
func TestTodo_LEDGER_006_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	dec := f.decision()

	want, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), dec)
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	wantDigest, err := want.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	const readers = 8
	digests := make([]string, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Every goroutine reads on its own connection: dbport.Conn is
			// documented as unsafe for concurrent use, and proving this
			// package is concurrency-safe must not depend on a handle that
			// is not.
			conn := f.db.NewConn(t)
			state, stateErr := temporal.Reconstruct(ctx, conn, f.request(june, june), dec)
			if stateErr != nil {
				errs[i] = stateErr
				return
			}
			digests[i], errs[i] = state.Digest()
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("reader %d: %v", i, err)
		}
		if digests[i] != wantDigest {
			t.Fatalf("reader %d reconstructed digest %s, want %s", i, digests[i], wantDigest)
		}
	}
}

// TestTodo_LEDGER_006_Security proves the two boundaries a temporal read
// must never cross: another tenant's ledger, and a field or subject the
// caller's own decision withholds. Both are proven at the SQL level - the
// statement this package would run is executed directly and PostgreSQL's own
// result set is inspected - so a passing test cannot be explained by Go
// filtering rows after they arrived.
func TestTodo_LEDGER_006_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)

	t.Run("a denied field never leaves PostgreSQL", func(t *testing.T) {
		dec := temporal.Decision{Tenant: f.tenant, DenyFields: []string{fieldBase}}
		sqlText, args, err := temporal.BuildReconstructSQL(
			f.request(june, june), dec, temporal.Coordinate{EffectiveAt: june, KnownAt: june}, 100)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if !strings.Contains(sqlText, "e.schema_ref = ANY(") {
			t.Fatalf("statement does not filter on schema_ref:\n%s", sqlText)
		}
		for _, ref := range rawSchemaRefs(t, f, sqlText, args) {
			if ref == fieldBase {
				t.Fatalf("PostgreSQL returned the denied field %q", ref)
			}
		}

		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), dec)
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		if _, ok := byField(state.Domain)[fieldBase]; ok {
			t.Fatal("the denied field appears in the reconstructed state")
		}
	})

	t.Run("an unauthorized subject never leaves PostgreSQL", func(t *testing.T) {
		dec := temporal.Decision{Tenant: f.tenant, AllowSubjects: []string{otherSubject}}
		req := f.request(june, june)
		sqlText, args, err := temporal.BuildReconstructSQL(
			req, dec, temporal.Coordinate{EffectiveAt: june, KnownAt: june}, 100)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if got := len(rawSchemaRefs(t, f, sqlText, args)); got != 0 {
			t.Fatalf("PostgreSQL returned %d rows for a subject the decision withholds", got)
		}
	})

	t.Run("another tenant's ledger is unreachable", func(t *testing.T) {
		other := uuid.New()
		f.seedTenant(t, other)
		f.append(t, recordFeb, datalogger.AppendRequest{
			Tenant: other, StreamKey: subject, ExpectedHead: 0,
			AssertionClass: datalogger.DomainFact, Authority: authorityInternal,
			SchemaRef: fieldTitle, Payload: []byte("title:other-tenant"), EffectiveAt: march,
		})

		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), f.decision())
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		for _, a := range slices.Concat(state.Domain, state.Transaction, state.Unpromoted) {
			if string(a.Payload) == "title:other-tenant" {
				t.Fatal("another tenant's assertion appears in this tenant's reconstruction")
			}
			if a.Tenant != f.tenant {
				t.Fatalf("assertion carries tenant %s, want %s", a.Tenant, f.tenant)
			}
		}
	})

	t.Run("a request cannot outrun its own decision", func(t *testing.T) {
		_, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june),
			temporal.Decision{Tenant: uuid.New()})
		var mismatch temporal.ErrTenantMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("reconstruct across tenants returned %v, want ErrTenantMismatch", err)
		}
	})
}

// TestTodo_LEDGER_006_Mutation kills the mutants that would make the
// implementation trivially "pass": collapsing truth classes, dropping the
// supersession check, turning a half-open bound into a closed one, and
// widening the knowledge horizon past the decision's ceiling. Each subtest
// asserts a behaviour that a specific weakening would break.
func TestTodo_LEDGER_006_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)

	t.Run("resolution that crossed truth classes would return the observation", func(t *testing.T) {
		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), f.decision())
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		title := byField(state.Domain)[fieldTitle]
		// The observation is strictly later on both axes. Only the truth-class
		// partition keeps it from winning.
		if title.EffectiveAt.After(march) {
			t.Fatalf("job title resolved to an assertion effective %s; the domain fact is effective %s",
				title.EffectiveAt, march)
		}
	})

	t.Run("dropping the supersession check would return the superseded original", func(t *testing.T) {
		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), f.decision())
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		for _, a := range state.Domain {
			if a.Superseded {
				t.Fatalf("superseded assertion %s@%d is part of the state", a.Ref.StreamKey, a.Ref.Sequence)
			}
		}
	})

	t.Run("a closed effective bound would move the boundary fact", func(t *testing.T) {
		// [march, april) must exclude the fact effective exactly at april.
		res, err := temporal.Between(ctx, f.db.Conn, temporal.Request{
			Tenant: f.tenant, Subject: subject, EffectiveFrom: march, EffectiveTo: april, KnownAt: june,
		}, f.decision())
		if err != nil {
			t.Fatalf("between: %v", err)
		}
		for _, a := range res.Assertions {
			if !a.EffectiveAt.Before(april) {
				t.Fatalf("assertion effective %s is inside a window that ends at %s", a.EffectiveAt, april)
			}
		}
	})

	t.Run("a knowledge ceiling cannot be widened by asking for a later horizon", func(t *testing.T) {
		dec := temporal.Decision{Tenant: f.tenant, MaxKnownAt: april}
		state, err := temporal.Reconstruct(ctx, f.db.Conn, f.request(june, june), dec)
		if err != nil {
			t.Fatalf("reconstruct: %v", err)
		}
		if !state.Coordinate.KnownAt.Equal(april) {
			t.Fatalf("reconstruction resolved at known-at %s, want the ceiling %s", state.Coordinate.KnownAt, april)
		}
		if got := string(byField(state.Domain)[fieldBase].Payload); got != "base:100" {
			t.Fatalf("with a ceiling of %s the compensation base is %q, want the uncorrected %q",
				april, got, "base:100")
		}
	})

	t.Run("an oversized reconstruction is refused rather than truncated", func(t *testing.T) {
		req := f.request(june, june)
		req.Limit = 2
		_, err := temporal.Reconstruct(ctx, f.db.Conn, req, f.decision())
		var tooLarge temporal.ErrReconstructTooLarge
		if !errors.As(err, &tooLarge) {
			t.Fatalf("reconstruct with a limit below the history size returned %v, want ErrReconstructTooLarge", err)
		}
	})
}

// rawSchemaRefs runs a statement this package built directly and returns the
// schema_ref column of every row PostgreSQL actually produced.
func rawSchemaRefs(t *testing.T, f fixture, sqlText string, args []any) []string {
	t.Helper()
	rows, err := f.db.Conn.Query(context.Background(), sqlText, args...)
	if err != nil {
		t.Fatalf("run built statement: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var (
			tenant           uuid.UUID
			streamKey        string
			sequence         int64
			eventID          uuid.UUID
			assertionClass   string
			authority        *string
			sourceRef        string
			schemaRef        string
			payload          []byte
			artifact         *string
			digest           string
			digestAlgorithm  string
			occurredAt       time.Time
			effectiveAt      time.Time
			recordedAt       time.Time
			correlationID    uuid.UUID
			causationID      *uuid.UUID
			correctsStream   *string
			correctsSequence *int64
			correctionKind   string
		)
		if err := rows.Scan(&tenant, &streamKey, &sequence, &eventID, &assertionClass, &authority,
			&sourceRef, &schemaRef, &payload, &artifact, &digest, &digestAlgorithm,
			&occurredAt, &effectiveAt, &recordedAt, &correlationID, &causationID,
			&correctsStream, &correctsSequence, &correctionKind); err != nil {
			t.Fatalf("scan raw row: %v", err)
		}
		out = append(out, schemaRef)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read built statement: %v", err)
	}
	return out
}
