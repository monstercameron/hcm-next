package temporal

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

var (
	tMarch = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	tApril = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	tJune  = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
)

func TestClampKnownAtNeverExtendsAHorizon(t *testing.T) {
	cases := []struct {
		name      string
		ceiling   time.Time
		requested time.Time
		want      time.Time
	}{
		{"no ceiling passes the request through", time.Time{}, tJune, tJune},
		{"a later request is clamped down", tApril, tJune, tApril},
		{"an earlier request is left alone", tJune, tApril, tApril},
		{"a zero request takes the ceiling", tApril, time.Time{}, tApril},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampKnownAt(Decision{Tenant: uuid.New(), MaxKnownAt: tc.ceiling}, tc.requested)
			if !got.Equal(tc.want) {
				t.Fatalf("clampKnownAt = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestPrepareResolvesAndClampsTheCoordinate(t *testing.T) {
	tenant := uuid.New()
	now := tJune

	t.Run("both axes default to the injected clock", func(t *testing.T) {
		req := Request{Tenant: tenant, Subject: "worker:1"}
		coord, err := prepare(&req, Decision{Tenant: tenant}, []Option{WithClock(func() time.Time { return now })})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if !coord.EffectiveAt.Equal(now) || !coord.KnownAt.Equal(now) {
			t.Fatalf("coordinate = %+v, want both axes at %s", coord, now)
		}
		if req.Mode != ModeReconstruct {
			t.Fatalf("prepare left mode %q, want it normalized to RECONSTRUCT", req.Mode)
		}
	})

	t.Run("the decision's ceiling wins over the request", func(t *testing.T) {
		req := Request{Tenant: tenant, Subject: "worker:1", EffectiveAt: tJune, KnownAt: tJune}
		coord, err := prepare(&req, Decision{Tenant: tenant, MaxKnownAt: tApril}, nil)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if !coord.KnownAt.Equal(tApril) {
			t.Fatalf("known-at = %s, want the ceiling %s", coord.KnownAt, tApril)
		}
	})

	t.Run("a decision for another tenant is refused", func(t *testing.T) {
		req := Request{Tenant: tenant, Subject: "worker:1"}
		_, err := prepare(&req, Decision{Tenant: uuid.New()}, nil)
		var mismatch ErrTenantMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("prepare = %v, want ErrTenantMismatch", err)
		}
	})
}

func TestCoordinateOfReportsWhatEachModeActuallyBounded(t *testing.T) {
	tenant := uuid.New()
	dec := Decision{Tenant: tenant}
	now := tJune

	cases := []struct {
		name            string
		req             Request
		wantEffective   time.Time
		wantKnown       time.Time
		effectiveIsZero bool
	}{
		{
			name: "CURRENT resolves both axes at now",
			req:  Request{Tenant: tenant, Mode: ModeCurrent}, wantEffective: now, wantKnown: now,
		},
		{
			name: "EFFECTIVE_AS_OF reports the requested business instant",
			req:  Request{Tenant: tenant, Mode: ModeEffectiveAsOf, EffectiveAt: tMarch}, wantEffective: tMarch, wantKnown: now,
		},
		{
			name: "KNOWN_AS_OF reports the requested horizon",
			req:  Request{Tenant: tenant, Mode: ModeKnownAsOf, KnownAt: tApril}, wantEffective: now, wantKnown: tApril,
		},
		{
			name: "BETWEEN reports the window's exclusive edge",
			req:  Request{Tenant: tenant, Mode: ModeBetween, EffectiveFrom: tMarch, EffectiveTo: tApril}, wantEffective: tApril, wantKnown: now,
		},
		{
			name:            "HISTORY applied no effective bound and says so",
			req:             Request{Tenant: tenant, Mode: ModeHistory},
			wantKnown:       now,
			effectiveIsZero: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coordinateOf(tc.req, dec, now)
			if tc.effectiveIsZero {
				if !got.EffectiveAt.IsZero() {
					t.Fatalf("effective-at = %s, want the zero time", got.EffectiveAt)
				}
			} else if !got.EffectiveAt.Equal(tc.wantEffective) {
				t.Fatalf("effective-at = %s, want %s", got.EffectiveAt, tc.wantEffective)
			}
			if !got.KnownAt.Equal(tc.wantKnown) {
				t.Fatalf("known-at = %s, want %s", got.KnownAt, tc.wantKnown)
			}
		})
	}
}

func TestFromFactCarriesIdentityAndLeavesLabelsUnresolved(t *testing.T) {
	eventID := uuid.New()
	fact := bitemporal.Fact{
		Tenant: uuid.New(), StreamKey: "worker:1", Sequence: 4, EventID: eventID,
		SchemaRef: "f@1", AssertionClass: datalogger.DomainFact,
		CorrectionKind: bitemporal.KindOriginal, Authority: "hcmnext:people",
		SourceRef: "hcmnext:test", Digest: "d", DigestAlgorithm: "sha256",
		EffectiveAt: tMarch, RecordedAt: tApril,
	}
	got := fromFact(fact)

	if got.SourceEventID != eventID {
		t.Fatalf("source event id = %s, want %s", got.SourceEventID, eventID)
	}
	if got.Ref != (datalogger.EventRef{StreamKey: "worker:1", Sequence: 4}) {
		t.Fatalf("ref = %+v, want worker:1@4", got.Ref)
	}
	if !got.Authority.Present || got.Authority.Ref != "hcmnext:people" {
		t.Fatalf("authority = %+v, want the cited reference marked present", got.Authority)
	}
	if got.Authority.Resolved {
		t.Fatal("authority is marked resolved before loadAuthorityLabels ran")
	}
	if got.TruthClass != "" {
		t.Fatalf("truth class = %q, want it left to the labelling pass", got.TruthClass)
	}

	t.Run("an assertion citing no authority is not marked present", func(t *testing.T) {
		fact.Authority = ""
		if fromFact(fact).Authority.Present {
			t.Fatal("an assertion with no authority reference is marked present")
		}
	})
}

func TestLabelWithinPageResolvesOnlyFromThePage(t *testing.T) {
	origin := Assertion{
		Ref: datalogger.EventRef{StreamKey: "worker:1", Sequence: 1}, AssertionClass: datalogger.DomainFact,
	}
	correction := Assertion{
		Ref: datalogger.EventRef{StreamKey: "worker:1", Sequence: 2}, AssertionClass: datalogger.Correction,
		Corrects: &datalogger.EventRef{StreamKey: "worker:1", Sequence: 1},
	}
	orphan := Assertion{
		Ref: datalogger.EventRef{StreamKey: "worker:1", Sequence: 3}, AssertionClass: datalogger.Correction,
		Corrects: &datalogger.EventRef{StreamKey: "worker:1", Sequence: 99},
	}

	page := []Assertion{origin, correction, orphan}
	labelWithinPage(page)

	if page[0].TruthClass != TruthDomain {
		t.Fatalf("origin truth class = %q, want %q", page[0].TruthClass, TruthDomain)
	}
	if !page[0].Superseded {
		t.Fatal("the corrected origin is not marked superseded")
	}
	if page[1].TruthClass != TruthDomain {
		t.Fatalf("correction truth class = %q, want it inherited as %q", page[1].TruthClass, TruthDomain)
	}
	if page[1].Superseded {
		t.Fatal("the correction itself is marked superseded")
	}
	if page[2].TruthClass != TruthUnresolved {
		t.Fatalf("a correction whose target is off the page has truth class %q, want %q",
			page[2].TruthClass, TruthUnresolved)
	}
}
