package dsr

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

func TestIdentityEvidence_ValidateRejectsMalformedEvidence(t *testing.T) {
	valid := fixtureEvidence(t, trust.AssuranceHigh)

	noRef := valid
	noRef.Ref = ""
	if err := noRef.Validate(); err == nil {
		t.Error("evidence with no reference validated")
	}

	badTenant := valid
	badTenant.Tenant = "Not Valid!"
	if err := badTenant.Validate(); err == nil {
		t.Error("evidence with a malformed tenant validated")
	}

	noAssurance := valid
	noAssurance.Assurance = trust.AssuranceUnspecified
	if err := noAssurance.Validate(); err == nil {
		t.Error("evidence with unspecified assurance validated")
	}

	noVerifiedAt := valid
	noVerifiedAt.VerifiedAt = values.Instant{}
	if err := noVerifiedAt.Validate(); err == nil {
		t.Error("evidence with no verified_at validated")
	}

	alreadyExpired := valid
	alreadyExpired.ExpiresAt = valid.VerifiedAt
	if err := alreadyExpired.Validate(); err == nil {
		t.Error("evidence whose expires_at is not after verified_at validated")
	}
}
