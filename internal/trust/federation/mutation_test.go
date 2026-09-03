package federation_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/federation"
)

// TestTodo_TRUST_002_Mutation is the TRUST-002 mutation test. It is not
// about input validation: it asserts that [federation.NewValidator] fails
// closed on an incomplete configuration, that the configuration it accepts
// is copied defensively (mutating the caller's map or slices afterward has
// no effect), and that every trusted claim actually participates in the
// principal Validate produces -- a mutation that drops a claim from the
// mapping fails here.
func TestTodo_TRUST_002_Mutation(t *testing.T) {
	keys := newTestKeys(t)

	t.Run("NewValidator fails closed on an incomplete configuration", func(t *testing.T) {
		validCfg := federation.Config{
			TenantIssuers: map[values.TenantId][]string{tenantAcme: {issuerAcme}},
			Audience:      audienceUnder,
			Keys:          keys.source,
		}
		if _, err := federation.NewValidator(validCfg); err != nil {
			t.Fatalf("NewValidator(valid) = %v", err)
		}
		for name, mutate := range map[string]func(*federation.Config){
			"no audience":      func(c *federation.Config) { c.Audience = "" },
			"no key source":    func(c *federation.Config) { c.Keys = nil },
			"no tenant issuer": func(c *federation.Config) { c.TenantIssuers = nil },
			"empty issuer list": func(c *federation.Config) {
				c.TenantIssuers = map[values.TenantId][]string{tenantAcme: {}}
			},
			"empty issuer string": func(c *federation.Config) {
				c.TenantIssuers = map[values.TenantId][]string{tenantAcme: {""}}
			},
			"invalid tenant": func(c *federation.Config) {
				c.TenantIssuers = map[values.TenantId][]string{"": {issuerAcme}}
			},
		} {
			cfg := validCfg
			mutate(&cfg)
			if _, err := federation.NewValidator(cfg); err == nil {
				t.Errorf("NewValidator(%s) succeeded, want a failure", name)
			}
		}
	})

	t.Run("the configuration is copied defensively", func(t *testing.T) {
		issuers := []string{issuerAcme}
		cfg := federation.Config{
			TenantIssuers: map[values.TenantId][]string{tenantAcme: issuers},
			Audience:      audienceUnder,
			Keys:          keys.source,
			Now:           func() time.Time { return baseTime },
		}
		v, err := federation.NewValidator(cfg)
		if err != nil {
			t.Fatalf("NewValidator: %v", err)
		}

		// Mutate the caller's backing slice and map after construction.
		issuers[0] = "https://attacker.example/"
		cfg.TenantIssuers[tenantOther] = []string{issuerOther}

		token := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		if _, err := v.Validate(context.Background(), token); err != nil {
			t.Fatalf("Validate after caller mutated its own config = %v, want success (acme's real issuer is still allow-listed)", err)
		}
	})

	t.Run("every trusted claim participates in the resulting principal", func(t *testing.T) {
		v := newValidator(t, keys.source, baseTime)
		ctx := context.Background()
		base := validAcmeClaims()
		baseToken := keys.signRS256(t, base, "acme-rsa-1")
		baseline, err := v.Validate(ctx, baseToken)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}

		mutations := map[string]func(*federation.Claims){
			"subject":    func(c *federation.Claims) { c.Subject = "user-different" },
			"org scope":  func(c *federation.Claims) { c.OrganizationScopeID = "org-emea" },
			"roles":      func(c *federation.Claims) { c.Roles = []string{"tenant_admin"} },
			"purposes":   func(c *federation.Claims) { c.Purposes = []string{"hcm_operations"} },
			"assurance":  func(c *federation.Claims) { c.Assurance = "high" },
			"delegation": func(c *federation.Claims) { c.DelegationRefs = nil },
			"issued at":  func(c *federation.Claims) { c.IssuedAtUnix = base.IssuedAtUnix - 30 },
			"expires at": func(c *federation.Claims) { c.ExpiresAtUnix = base.ExpiresAtUnix + 30 },
		}
		for name, mutate := range mutations {
			claims := base
			mutate(&claims)
			token := keys.signRS256(t, claims, "acme-rsa-1")
			mutated, err := v.Validate(ctx, token)
			if err != nil {
				t.Fatalf("Validate after mutating %s: %v", name, err)
			}
			if mutated.Fingerprint() == baseline.Fingerprint() {
				t.Errorf("mutating %s left the fingerprint unchanged", name)
			}
		}
	})

	t.Run("the same assertion always derives the same evidence id", func(t *testing.T) {
		v := newValidator(t, keys.source, baseTime)
		ctx := context.Background()
		token := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		first, err := v.Validate(ctx, token)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		second, err := v.Validate(ctx, token)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if first.EvidenceID() != second.EvidenceID() {
			t.Error("the same assertion derived two different evidence identifiers")
		}
		if first.SessionRef() != second.SessionRef() {
			t.Error("the same assertion derived two different session references")
		}
	})
}
