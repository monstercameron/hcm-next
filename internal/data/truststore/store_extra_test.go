package truststore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/accessreview"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

func TestErrorsAndValidationHelpers_Boundaries(t *testing.T) {
	if got := (&Error{}).Error(); got != "" {
		t.Fatalf("empty Error.Error() = %q", got)
	}
	if got := (*Error)(nil).Error(); got != "truststore: nil error" {
		t.Fatalf("nil Error.Error() = %q", got)
	}
	cause := errors.New("cause")
	err := failure(CodeDatabase, "table", "key", cause)
	if CodeOf(err) != CodeDatabase || !errors.Is(err, cause) {
		t.Fatalf("failure classification = %s, errors.Is=%v", CodeOf(err), errors.Is(err, cause))
	}
	if CodeOf(errors.New("plain")) != "" {
		t.Fatal("CodeOf classified an unrelated error")
	}
	if err := requireText("table", "field", ""); CodeOf(err) != CodeInvalid {
		t.Fatalf("requireText code = %s", CodeOf(err))
	}
	validDigest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := requireDigest("table", "digest", validDigest); err != nil {
		t.Fatalf("valid digest rejected: %v", err)
	}
	for _, digest := range []string{"", "ABC" + validDigest[3:], validDigest + "0"} {
		if err := requireDigest("table", "digest", digest); CodeOf(err) != CodeInvalid {
			t.Fatalf("digest %q code = %s", digest, CodeOf(err))
		}
	}
	if _, err := jsonValue(func() {}); err == nil {
		t.Fatal("jsonValue marshaled an unsupported value")
	}
	if got := nonNilUUID(uuid.Nil); got == uuid.Nil {
		t.Fatal("nonNilUUID returned nil UUID")
	}
	if got := nonNilUUID(uuid.New()); got == uuid.Nil {
		t.Fatal("nonNilUUID changed a non-nil UUID to nil")
	}
	if nullableTime(time.Time{}) != nil || nullableTime(time.Unix(1, 0)) == nil {
		t.Fatal("nullableTime boundary is incorrect")
	}
	if nullableJSON(nil) != nil || nullableJSON([]byte("null")) != nil || nullableJSON([]byte(`{}`)) == nil {
		t.Fatal("nullableJSON boundary is incorrect")
	}
	for _, code := range []string{"23505", "23514", "23502", "22P02", "99999"} {
		classified := classifyWriteError("table", "key", &pgconn.PgError{Code: code})
		want := CodeDatabase
		if code == "23505" {
			want = CodeDuplicate
		} else if code == "23514" || code == "23502" || code == "22P02" {
			want = CodeInvalid
		}
		if CodeOf(classified) != want {
			t.Fatalf("postgres code %s classified as %s, want %s", code, CodeOf(classified), want)
		}
	}
}

func TestStoreTenantScopeAndConsume_RefusesInvalidInputs(t *testing.T) {
	ctx := context.Background()
	if err := (&Store{}).withTenant(ctx, uuid.New(), func(_ dbport.Tx) error { return nil }); CodeOf(err) != CodeInvalid {
		t.Fatalf("nil database code = %s", CodeOf(err))
	}
	store, _, tenant := trustFixture(t)
	if err := store.withTenant(ctx, uuid.Nil, func(_ dbport.Tx) error { return nil }); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil tenant code = %s", CodeOf(err))
	}
	if _, err := parseTenant("not-a-uuid"); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("invalid tenant code = %s", CodeOf(err))
	}
	if _, err := parseTenant(uuid.Nil.String()); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil parsed tenant code = %s", CodeOf(err))
	}
	if _, err := store.Consume(ctx, "proof-no-scope", "used"); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("unscoped Consume code = %s", CodeOf(err))
	}
	if _, err := store.ForTenant(uuid.Nil).Consume(ctx, "proof-nil", "used"); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil scoped Consume code = %s", CodeOf(err))
	}
	for _, tc := range []struct{ proof, outcome string }{{"", "used"}, {"proof", ""}} {
		if _, err := store.ForTenant(tenant).Consume(ctx, tc.proof, tc.outcome); CodeOf(err) != CodeInvalid {
			t.Fatalf("Consume(%q,%q) code = %s", tc.proof, tc.outcome, CodeOf(err))
		}
	}
	ctx = ContextWithTenant(ctx, tenant)
	consumed, err := store.Consume(ctx, "proof-context", "used")
	if err != nil || consumed {
		t.Fatalf("first context Consume = %v, %v", consumed, err)
	}
	consumed, err = store.Consume(ctx, "proof-context", "replay")
	if err != nil || !consumed {
		t.Fatalf("replayed context Consume = %v, %v", consumed, err)
	}
}

func TestJITStore_ValidationRevisionAndEvidenceBoundaries(t *testing.T) {
	store, _, tenant := trustFixture(t)
	ctx := context.Background()
	base := sampleGrant(tenant, "jit-extra")
	cases := []struct {
		name   string
		mutate func(*JITGrantRecord)
		want   Code
	}{
		{"zero revision", func(g *JITGrantRecord) { g.Revision = 0 }, CodeInvalid},
		{"noninitial revision", func(g *JITGrantRecord) { g.Revision = 2 }, CodeVersionConflict},
		{"missing state", func(g *JITGrantRecord) { g.State = "" }, CodeInvalid},
		{"reversed window", func(g *JITGrantRecord) { g.ExpiresAt = g.NotBefore }, CodeInvalid},
		{"missing scope", func(g *JITGrantRecord) { g.Scope = nil }, CodeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			if err := store.PutJITGrant(ctx, tenant, in); CodeOf(err) != tc.want {
				t.Fatalf("PutJITGrant code = %s, want %s", CodeOf(err), tc.want)
			}
		})
	}
	if _, err := store.LoadJITGrant(ctx, tenant, "missing", 1); CodeOf(err) != CodeNotFound {
		t.Fatalf("missing LoadJITGrant code = %s", CodeOf(err))
	}
	if _, err := store.LoadJITGrant(ctx, uuid.Nil, "missing", 1); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil LoadJITGrant code = %s", CodeOf(err))
	}
	if _, err := store.UpdateJITGrant(ctx, tenant, "missing", 1, "ACTIVE", false); CodeOf(err) != CodeVersionConflict {
		t.Fatalf("missing UpdateJITGrant code = %s", CodeOf(err))
	}
	if _, err := store.UpdateJITGrant(ctx, tenant, base.GrantID, 0, "ACTIVE", false); CodeOf(err) != CodeInvalid {
		t.Fatalf("invalid UpdateJITGrant code = %s", CodeOf(err))
	}
	if err := store.AppendJITEvidence(ctx, JITEvidenceRecord{TenantID: tenant, RowID: uuid.New(), GrantID: base.GrantID, EvidenceKind: "GRANTED", At: base.NotBefore, EventSequence: 2}); CodeOf(err) != CodeVersionConflict {
		t.Fatalf("gapped evidence code = %s", CodeOf(err))
	}
	if err := store.AppendJITEvidence(ctx, JITEvidenceRecord{TenantID: tenant, RowID: uuid.Nil, GrantID: base.GrantID, EvidenceKind: "GRANTED", At: base.NotBefore, EventSequence: 1}); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil row evidence code = %s", CodeOf(err))
	}
}

func TestDomainAdaptersAndAccessReviewLedger_StateAndAliases(t *testing.T) {
	store, _, tenant := trustFixture(t)
	ctx := context.Background()
	grant, err := jit.New("domain-jit", jit.Request{Principal: "operator-a", Tenant: values.TenantId(tenant.String()), Role: jit.RoleIncidentResponder,
		TicketRef: "INC-1", Justification: "incident", Capabilities: []string{"read:metadata"}, Purpose: "incident", TTL: time.Hour},
		jit.Approval{Approver: "operator-b", At: time.Date(2026, 9, 6, 11, 0, 0, 0, time.UTC)}, time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveJITGrant(ctx, grant, 1, "ACTIVE"); err != nil {
		t.Fatalf("SaveJITGrant: %v", err)
	}
	loaded, err := store.LoadJITGrant(ctx, tenant, grant.ID, 1)
	if err != nil || loaded.Requester != grant.Principal || loaded.Approver != grant.Approver {
		t.Fatalf("loaded domain JIT = %+v, err=%v", loaded, err)
	}
	accessGrant := accessreview.Grant{ID: "access-extra", Holder: "principal", Class: accessreview.GrantClassAuthz, GrantedAt: grant.IssuedAt, ExpiresAt: grant.ExpiresAt}
	if err := store.SaveDomainGrant(ctx, tenant, accessGrant, map[string]any{"read": true}); err != nil {
		t.Fatalf("SaveDomainGrant: %v", err)
	}
	if err := store.SaveAccessReviewGrant(ctx, tenant, sampleAccessReviewGrant(tenant, "alias-grant")); err != nil {
		t.Fatal(err)
	}
	schedule := sampleSchedule()
	schedule.Entries = []accessreview.ScheduleEntry{{Grant: accessGrant, ReviewDue: accessGrant.GrantedAt.Add(time.Hour)}}
	stored, err := store.PutAccessReviewSchedule(ctx, tenant, "schedule-extra", schedule)
	if err != nil {
		t.Fatal(err)
	}
	review := accessreview.ReviewRecord{GrantID: accessGrant.ID, Reviewer: "reviewer", Action: accessreview.Continue, Justification: "checked", ReviewedAt: accessGrant.GrantedAt.Add(time.Hour)}
	if err := store.AppendDomainReview(ctx, tenant, stored.ScheduleID, review, 1); err != nil {
		t.Fatalf("AppendDomainReview: %v", err)
	}
	if err := store.AppendReviewRecord(ctx, AccessReviewReviewRecord{TenantID: tenant, RowID: uuid.New(), ScheduleID: stored.ScheduleID, ReviewerID: "reviewer-2", Decision: "CONTINUE", At: accessGrant.GrantedAt.Add(2 * time.Hour), EventSequence: 2}); err != nil {
		t.Fatalf("AppendReviewRecord: %v", err)
	}
	records, err := store.ListAccessReviewRecords(ctx, tenant, stored.ScheduleID)
	if err != nil || len(records) != 2 || records[0].EventSequence != 1 || records[1].EventSequence != 2 {
		t.Fatalf("review records = %+v, err=%v", records, err)
	}
	if _, err := store.PutAccessReviewSchedule(ctx, tenant, "bad-digest", accessreview.Schedule{Digest: "BAD"}); CodeOf(err) != CodeInvalid {
		t.Fatalf("bad digest code = %s", CodeOf(err))
	}
	if _, err := store.PutAccessReviewSchedule(ctx, tenant, "missing-grant", accessreview.Schedule{Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Entries: []accessreview.ScheduleEntry{{Grant: accessreview.Grant{ID: "nope"}, ReviewDue: time.Now()}}}); CodeOf(err) != CodeNotFound {
		t.Fatalf("missing schedule grant code = %s", CodeOf(err))
	}
	if err := store.SaveDomainGrant(ctx, tenant, accessreview.Grant{ID: "bad-json", Holder: "p", GrantedAt: time.Now()}, func() {}); CodeOf(err) != CodeInvalid {
		t.Fatalf("unmarshalable capabilities code = %s", CodeOf(err))
	}
}
