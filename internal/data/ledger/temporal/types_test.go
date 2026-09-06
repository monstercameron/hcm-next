package temporal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/bitemporal"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

func TestModeValid(t *testing.T) {
	for _, m := range []Mode{ModeCurrent, ModeEffectiveAsOf, ModeKnownAsOf, ModeBetween, ModeHistory, ModeReconstruct} {
		if !m.Valid() {
			t.Errorf("mode %q is not valid", m)
		}
	}
	for _, m := range []Mode{"", "current", "AS_OF", "RECONSTRUCTED"} {
		if m.Valid() {
			t.Errorf("mode %q is valid, want rejected", m)
		}
	}
}

func TestTruthClassOfMapsEveryNonCorrectionClass(t *testing.T) {
	cases := map[datalogger.AssertionClass]TruthClass{
		datalogger.DomainFact:          TruthDomain,
		datalogger.TransactionFact:     TruthTransaction,
		datalogger.ExternalObservation: TruthObserved,
		datalogger.Claim:               TruthClaimed,
		// A CORRECTION has no class of its own; resolveTruthClass walks its
		// chain instead, and anything that reaches truthClassOf unresolved
		// must fail closed.
		datalogger.Correction: TruthUnresolved,
		"NOT_A_CLASS":         TruthUnresolved,
	}
	for class, want := range cases {
		if got := truthClassOf(class); got != want {
			t.Errorf("truthClassOf(%q) = %q, want %q", class, got, want)
		}
	}
}

func TestRequestValidate(t *testing.T) {
	tenant := uuid.New()
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	t.Run("reconstruct requires a subject", func(t *testing.T) {
		err := Request{Tenant: tenant, Mode: ModeReconstruct}.Validate()
		var invalid ErrRequestInvalid
		if !errors.As(err, &invalid) || invalid.Field != "Subject" {
			t.Fatalf("Validate() = %v, want ErrRequestInvalid on Subject", err)
		}
	})

	t.Run("reconstruct rejects a negative limit", func(t *testing.T) {
		err := Request{Tenant: tenant, Mode: ModeReconstruct, Subject: "worker:1", Limit: -1}.Validate()
		var invalid ErrRequestInvalid
		if !errors.As(err, &invalid) || invalid.Field != "Limit" {
			t.Fatalf("Validate() = %v, want ErrRequestInvalid on Limit", err)
		}
	})

	t.Run("tenant is always required", func(t *testing.T) {
		err := Request{Mode: ModeCurrent}.Validate()
		var invalid ErrRequestInvalid
		if !errors.As(err, &invalid) || invalid.Field != "Tenant" {
			t.Fatalf("Validate() = %v, want ErrRequestInvalid on Tenant", err)
		}
	})

	t.Run("an unknown mode names the six it accepts", func(t *testing.T) {
		err := Request{Tenant: tenant, Mode: "SOMETHING"}.Validate()
		if err == nil || !strings.Contains(err.Error(), "RECONSTRUCT") {
			t.Fatalf("Validate() = %v, want an error naming RECONSTRUCT among the modes", err)
		}
	})

	t.Run("a delegated mode is validated by the adapter that answers it", func(t *testing.T) {
		// EFFECTIVE_AS_OF requires EffectiveAt; the rule lives in
		// internal/data/bitemporal and must not be re-implemented here.
		if err := (Request{Tenant: tenant, Mode: ModeEffectiveAsOf}).Validate(); err == nil {
			t.Fatal("EFFECTIVE_AS_OF without EffectiveAt validated")
		}
		if err := (Request{Tenant: tenant, Mode: ModeEffectiveAsOf, EffectiveAt: at}).Validate(); err != nil {
			t.Fatalf("well-formed EFFECTIVE_AS_OF: %v", err)
		}
	})
}

func TestRequestDelegateCarriesEveryBound(t *testing.T) {
	tenant := uuid.New()
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	req := Request{
		Tenant: tenant, Mode: ModeBetween, Subject: "worker:1", Field: "f@1",
		EffectiveFrom: from, EffectiveTo: to, KnownAt: to, Cursor: "abc", Limit: 7,
	}
	got := req.delegate()
	want := bitemporal.Request{
		Tenant: tenant, Mode: bitemporal.ModeBetween, Subject: "worker:1", Field: "f@1",
		EffectiveFrom: from, EffectiveTo: to, KnownAt: to, Cursor: "abc", Limit: 7,
	}
	if got != want {
		t.Fatalf("delegate() = %+v, want %+v", got, want)
	}
}

func TestErrorCodesAreStableAndSelfDescribing(t *testing.T) {
	cases := []struct {
		err       error
		code      string
		mustNames []string
	}{
		{ErrRequestInvalid{Field: "Subject", Reason: "is required"}, "LEDGER_TEMPORAL_REQUEST_INVALID", []string{"Subject", "is required"}},
		{ErrTenantMismatch{RequestTenant: uuid.Nil, DecisionTenant: uuid.Nil}, "LEDGER_TEMPORAL_TENANT_MISMATCH", []string{uuid.Nil.String()}},
		{ErrReconstructTooLarge{Subject: "worker:1", Limit: 3}, "LEDGER_TEMPORAL_RECONSTRUCT_TOO_LARGE", []string{"worker:1", "3"}},
		{ErrPlansDisagree{Subject: "worker:1", PlanA: "a", DigestA: "d1", PlanB: "b", DigestB: "d2"}, "LEDGER_TEMPORAL_PLANS_DISAGREE", []string{"d1", "d2", "a", "b"}},
	}
	for _, tc := range cases {
		msg := tc.err.Error()
		if !strings.HasPrefix(msg, tc.code+":") {
			t.Errorf("%T reports %q, want it to start with %q", tc.err, msg, tc.code+":")
		}
		for _, name := range tc.mustNames {
			if !strings.Contains(msg, name) {
				t.Errorf("%T reports %q, want it to name %q", tc.err, msg, name)
			}
		}
	}
}
