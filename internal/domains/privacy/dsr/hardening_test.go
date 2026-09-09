package dsr

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestSubjectClaims_ValidateAndKey_Boundaries(t *testing.T) {
	if err := (SubjectClaims{}).Validate(); !errors.Is(err, ErrClaimsInvalid) {
		t.Fatalf("empty claims error = %v, want ErrClaimsInvalid", err)
	}
	cases := []struct {
		name   string
		claims SubjectClaims
		want   string
	}{
		{"external ref wins", SubjectClaims{ExternalRef: "  PERSON-1 ", Email: "other@example.com"}, "person-1"},
		{"email wins", SubjectClaims{Email: "  Alex@Example.COM ", Phone: "555"}, "alex@example.com"},
		{"phone wins", SubjectClaims{Phone: "  555-0100 ", FullName: "Alex Example"}, "555-0100"},
		{"name wins", SubjectClaims{FullName: "  Alex Example "}, "alex example"},
		{"no key", SubjectClaims{ExternalRef: "  ", Email: "\t"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.claims.Key(); got != tc.want {
				t.Fatalf("Key() = %q, want %q", got, tc.want)
			}
		})
	}
	if err := (SubjectClaims{RepresentativeRef: "rep-only"}).Validate(); !errors.Is(err, ErrClaimsInvalid) {
		t.Fatalf("representative-only claims error = %v, want ErrClaimsInvalid", err)
	}
	base := SubjectClaims{ExternalRef: "id", FullName: "name", Email: "email", Phone: "phone", RepresentativeRef: "rep"}
	if string(base.canonicalBytes(nil)) == string((SubjectClaims{ExternalRef: "id", FullName: "name", Email: "email", Phone: "phone"}).canonicalBytes(nil)) {
		t.Fatal("RepresentativeRef was omitted from canonical claims")
	}
}

func TestClockTable_Deadline_ResolutionAndErrors(t *testing.T) {
	received := mustInstant(t, fxReceivedAt)
	local := legal.Jurisdiction{Country: "US", State: "CA", Locality: "San Francisco"}
	state := legal.Jurisdiction{Country: "US", State: "CA"}
	country := legal.Jurisdiction{Country: "US"}
	table, err := NewClockTable(
		ClockEntry{Kind: KindAccess, Days: 1, Basis: DayBasisCalendar},
		ClockEntry{Jurisdiction: country, Kind: KindAccess, Days: 2, Basis: DayBasisCalendar},
		ClockEntry{Jurisdiction: state, Kind: KindAccess, Days: 3, Basis: DayBasisCalendar},
		ClockEntry{Jurisdiction: local, Kind: KindAccess, Days: 4, Basis: DayBasisCalendar},
	)
	if err != nil {
		t.Fatalf("NewClockTable: %v", err)
	}
	checks := []struct {
		name         string
		jurisdiction legal.Jurisdiction
		days         int64
	}{
		{"exact", local, 4},
		{"state fallback", legal.Jurisdiction{Country: "US", State: "CA", Locality: "Los Angeles"}, 3},
		{"country fallback", legal.Jurisdiction{Country: "US", State: "NY"}, 2},
		{"default fallback", legal.Jurisdiction{Country: "CA"}, 1},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			got, err := table.Deadline(tc.jurisdiction, KindAccess, received)
			if err != nil {
				t.Fatalf("Deadline: %v", err)
			}
			want, err := values.NewInstantFromUnix(int64(fxReceivedAt)+tc.days*86400, 0)
			if err != nil || got != want {
				t.Fatalf("deadline = %s, want %s", got, want)
			}
		})
	}
	if _, err := table.Deadline(legal.Jurisdiction{}, Kind("BOGUS"), received); !errors.Is(err, ErrKindInvalid) {
		t.Fatalf("undeclared kind error = %v, want ErrKindInvalid", err)
	}
	if _, err := table.Deadline(legal.Jurisdiction{}, KindAccess, values.Instant{}); err == nil {
		t.Fatal("unset received_at unexpectedly succeeded")
	}
	if _, err := NewClockTable(ClockEntry{Kind: KindAccess, Days: 1, Basis: DayBasisCalendar}, ClockEntry{Kind: KindAccess, Days: 2, Basis: DayBasisCalendar}); !errors.Is(err, ErrClockEntryInvalid) {
		t.Fatalf("duplicate error = %v, want ErrClockEntryInvalid", err)
	}
	if _, err := table.Deadline(legal.Jurisdiction{Country: "DE"}, KindErasure, received); !errors.Is(err, ErrClockUndeclared) {
		t.Fatalf("undeclared clock error = %v, want ErrClockUndeclared", err)
	}
	for _, kind := range AllKinds() {
		floor := AssuranceFloor(kind)
		if floor == trust.AssuranceUnspecified {
			t.Errorf("AssuranceFloor(%s) is unspecified", kind)
		}
	}
}

func TestIntake_RecordsPrimaryAndDuplicateState(t *testing.T) {
	primary, err := Intake(fixtureIntakeSpec(t, "primary", KindAccess), DefaultClockTable(), nil, fxWindow)
	if err != nil {
		t.Fatalf("primary Intake: %v", err)
	}
	if primary.DuplicateOf != "" || len(primary.Trail) != 1 || primary.Trail[0].Kind != EventIntake || primary.VerificationState != VerificationUnverified {
		t.Fatalf("primary intake state = %+v, want unlinked unverified intake trail", primary)
	}
	secondSpec := fixtureIntakeSpec(t, "second", KindAccess)
	second, err := Intake(secondSpec, DefaultClockTable(), []DataSubjectRequest{primary}, fxWindow)
	if err != nil {
		t.Fatalf("duplicate Intake: %v", err)
	}
	if second.DuplicateOf != primary.ID || len(second.Trail) != 2 || second.Trail[1].Kind != EventDuplicateLinked || second.Trail[1].Detail != primary.ID {
		t.Fatalf("duplicate intake state = %+v, want link evidence", second)
	}
	other := primary
	other.ID = "other-tenant"
	other.Tenant = fxOtherTenant
	other = other.withEvidenceID()
	if _, ok := DetectDuplicate([]DataSubjectRequest{other}, second, fxWindow); ok {
		t.Fatal("cross-tenant request matched as duplicate")
	}
	old := primary
	old.ID = "older"
	old.ReceivedAt = mustInstant(t, fxReceivedAt-10)
	old = old.withEvidenceID()
	tie := primary
	tie.ID = "aaa"
	if got, ok := DetectDuplicate([]DataSubjectRequest{primary, tie, old}, second, fxWindow); !ok || got.ID != old.ID {
		t.Fatalf("DetectDuplicate earliest = (%+v, %v), want %q", got, ok, old.ID)
	}
	far := primary
	far.ReceivedAt = mustInstant(t, fxReceivedAt-int64((fxWindow/time.Second)+1))
	if _, ok := DetectDuplicate([]DataSubjectRequest{far}, second, fxWindow); ok {
		t.Fatal("request outside duplicate window matched")
	}
}

func TestDataSubjectRequest_Validate_AllRequiredAndEvidenceBranches(t *testing.T) {
	base := fixtureRequest(t)
	cases := []struct {
		name   string
		mutate func(DataSubjectRequest) DataSubjectRequest
	}{
		{"missing id", func(r DataSubjectRequest) DataSubjectRequest { r.ID = ""; return r }},
		{"invalid tenant", func(r DataSubjectRequest) DataSubjectRequest { r.Tenant = "bad tenant!"; return r }},
		{"invalid kind", func(r DataSubjectRequest) DataSubjectRequest { r.Kind = "BOGUS"; return r }},
		{"missing claims", func(r DataSubjectRequest) DataSubjectRequest { r.Claims = SubjectClaims{}; return r }},
		{"invalid jurisdiction", func(r DataSubjectRequest) DataSubjectRequest {
			r.Jurisdiction = legal.Jurisdiction{Country: "us"}
			return r
		}},
		{"missing received at", func(r DataSubjectRequest) DataSubjectRequest { r.ReceivedAt = values.Instant{}; return r }},
		{"invalid channel", func(r DataSubjectRequest) DataSubjectRequest { r.Channel = "BOGUS"; return r }},
		{"missing deadline", func(r DataSubjectRequest) DataSubjectRequest { r.Deadline = values.Instant{}; return r }},
		{"deadline before received", func(r DataSubjectRequest) DataSubjectRequest { r.Deadline = mustInstant(t, fxReceivedAt-1); return r }},
		{"invalid verification state", func(r DataSubjectRequest) DataSubjectRequest { r.VerificationState = "BOGUS"; return r }},
		{"verified without evidence ref", func(r DataSubjectRequest) DataSubjectRequest { r.VerificationState = VerificationVerified; return r }},
		{"verified with unspecified assurance", func(r DataSubjectRequest) DataSubjectRequest {
			r.VerificationState = VerificationVerified
			r.IdentityEvidenceRef = "ev:x"
			r.VerifiedAt = mustInstant(t, fxVerifiedAt)
			return r
		}},
		{"verified without verified at", func(r DataSubjectRequest) DataSubjectRequest {
			r.VerificationState = VerificationVerified
			r.IdentityEvidenceRef = "ev:x"
			r.IdentityAssurance = trust.AssuranceHigh
			return r
		}},
		{"unverified with evidence ref", func(r DataSubjectRequest) DataSubjectRequest { r.IdentityEvidenceRef = "ev:x"; return r }},
		{"unverified with assurance", func(r DataSubjectRequest) DataSubjectRequest { r.IdentityAssurance = trust.AssuranceHigh; return r }},
		{"unverified with verified at", func(r DataSubjectRequest) DataSubjectRequest { r.VerifiedAt = mustInstant(t, fxVerifiedAt); return r }},
		{"missing evidence id", func(r DataSubjectRequest) DataSubjectRequest { r.EvidenceID = ""; return r }},
		{"tampered evidence id", func(r DataSubjectRequest) DataSubjectRequest { r.EvidenceID = "ev:privacy:dsr:tampered"; return r }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(); !errors.Is(err, ErrRequestInvalid) {
				t.Fatalf("Validate() error = %v, want ErrRequestInvalid", err)
			}
		})
	}
	verified, err := base.Verify(fixtureEvidence(t, trust.AssuranceHigh))
	if err != nil {
		t.Fatalf("Verify valid request: %v", err)
	}
	if err := verified.Validate(); err != nil {
		t.Fatalf("valid verified request rejected: %v", err)
	}
}

func TestDataSubjectRequest_CanAdvance_DecisionMatrix(t *testing.T) {
	base := fixtureRequest(t)
	cases := []struct {
		name string
		req  DataSubjectRequest
		want bool
		code AdvanceCode
	}{
		{"invalid", func() DataSubjectRequest { r := base; r.Kind = "BOGUS"; return r }(), false, AdvanceRecordInvalid},
		{"unverified", base, false, AdvanceUnverified},
		{"duplicate", func() DataSubjectRequest { r := base; r.DuplicateOf = "primary"; r = r.withEvidenceID(); return r }(), false, AdvanceDuplicateNotPrimary},
	}
	verified, err := base.Verify(fixtureEvidence(t, trust.AssuranceHigh))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	below := verified
	below.IdentityAssurance = trust.AssuranceLow
	below = below.withEvidenceID()
	cases = append(cases, struct {
		name string
		req  DataSubjectRequest
		want bool
		code AdvanceCode
	}{"below floor", below, false, AdvanceAssuranceBelowFloor})
	cases = append(cases, struct {
		name string
		req  DataSubjectRequest
		want bool
		code AdvanceCode
	}{"allowed", verified, true, AdvanceAllowed})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, code := tc.req.CanAdvance()
			if got != tc.want || code != tc.code {
				t.Fatalf("CanAdvance() = (%v, %s), want (%v, %s)", got, code, tc.want, tc.code)
			}
		})
	}
}

func TestIdentityEvidence_ValidateAndVerify_DecisionMatrix(t *testing.T) {
	valid := fixtureEvidence(t, trust.AssuranceHigh)
	invalid := []struct {
		name   string
		mutate func(IdentityEvidence) IdentityEvidence
	}{
		{"missing ref", func(e IdentityEvidence) IdentityEvidence { e.Ref = ""; return e }},
		{"bad tenant", func(e IdentityEvidence) IdentityEvidence { e.Tenant = "bad tenant!"; return e }},
		{"unspecified assurance", func(e IdentityEvidence) IdentityEvidence { e.Assurance = trust.AssuranceUnspecified; return e }},
		{"missing verified at", func(e IdentityEvidence) IdentityEvidence { e.VerifiedAt = values.Instant{}; return e }},
		{"expired at boundary", func(e IdentityEvidence) IdentityEvidence { e.ExpiresAt = e.VerifiedAt; return e }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(valid).Validate(); !errors.Is(err, ErrIdentityEvidenceInvalid) {
				t.Fatalf("Validate() error = %v, want ErrIdentityEvidenceInvalid", err)
			}
		})
	}
	base := fixtureRequest(t)
	cases := []struct {
		name     string
		evidence IdentityEvidence
		code     VerifyRefusalCode
	}{
		{"missing evidence", IdentityEvidence{}, VerifyRefusedNoEvidence},
		{"expired", func() IdentityEvidence { e := valid; e.ExpiresAt = e.VerifiedAt; return e }(), VerifyRefusedEvidenceExpired},
		{"cross tenant", func() IdentityEvidence { e := valid; e.Tenant = fxOtherTenant; return e }(), VerifyRefusedCrossTenant},
		{"assurance below floor", func() IdentityEvidence { e := valid; e.Assurance = trust.AssuranceLow; return e }(), VerifyRefusedAssuranceBelowFloor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := base.Verify(tc.evidence)
			if err == nil || !strings.Contains(err.Error(), string(tc.code)) {
				t.Fatalf("Verify() error = %v, want refusal %s", err, tc.code)
			}
			if got.VerificationState != VerificationRefused || len(got.Trail) != len(base.Trail)+1 || got.Trail[len(got.Trail)-1].Detail != string(tc.code) {
				t.Fatalf("refused result = %+v, want refused state and typed trail", got)
			}
			if base.VerificationState != VerificationUnverified {
				t.Fatal("Verify mutated the original request")
			}
		})
	}
	repSpec := fixtureIntakeSpec(t, "rep", KindAccess)
	repSpec.Claims.RepresentativeRef = "rep-1"
	repReq, err := Intake(repSpec, DefaultClockTable(), nil, fxWindow)
	if err != nil {
		t.Fatalf("representative Intake: %v", err)
	}
	repEvidence := valid
	repEvidence.RepresentativeAuthorized = false
	got, err := repReq.Verify(repEvidence)
	if err == nil || !strings.Contains(err.Error(), string(VerifyRefusedRepresentative)) || got.VerificationState != VerificationRefused {
		t.Fatalf("unauthorized representative result = (%+v, %v)", got, err)
	}
	repEvidence.RepresentativeAuthorized = true
	if got, err = repReq.Verify(repEvidence); err != nil || got.VerificationState != VerificationVerified {
		t.Fatalf("authorized representative result = (%+v, %v)", got, err)
	}
	verified, err := base.Verify(valid)
	if err != nil || verified.VerificationState != VerificationVerified || verified.IdentityEvidenceRef != valid.Ref || verified.IdentityAssurance != valid.Assurance || verified.VerifiedAt != valid.VerifiedAt {
		t.Fatalf("successful Verify result = (%+v, %v)", verified, err)
	}
}

func TestKindChannelVerificationState_StringAndErrors(t *testing.T) {
	for _, k := range AllKinds() {
		if k.String() != string(k) || k.Validate() != nil {
			t.Errorf("kind %q String/Validate mismatch", k)
		}
	}
	for _, c := range allChannels {
		if c.String() != string(c) || c.Validate() != nil {
			t.Errorf("channel %q String/Validate mismatch", c)
		}
	}
	for _, s := range allVerificationStates {
		if s.String() != string(s) || s.Validate() != nil {
			t.Errorf("state %q String/Validate mismatch", s)
		}
	}
	if err := Kind("BOGUS").Validate(); !errors.Is(err, ErrKindInvalid) {
		t.Fatalf("kind error = %v", err)
	}
	if err := Channel("BOGUS").Validate(); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("channel error = %v", err)
	}
	if err := VerificationState("BOGUS").Validate(); err == nil {
		t.Fatal("bogus verification state validated")
	}
}
