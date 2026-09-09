package hcmctl

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// mintToken mints a short-lived development bearer token via
// internal/trust.HMACVerifier.Issue for the JIT (-mint) flow. This is
// authentication plumbing this codebase already owns, not business logic:
// hcmctl invents no token format and no claim semantics of its own.
func mintToken(g globalFlags) (string, error) {
	if g.signingKey == "" {
		return "", fmt.Errorf("hcmctl: -mint requires -mint-key")
	}
	if g.issuer == "" || g.audience == "" || g.tenant == "" || g.subject == "" {
		return "", fmt.Errorf("hcmctl: -mint requires -mint-issuer, -mint-audience, -mint-tenant and -mint-subject")
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(g.signingKey),
		Issuer:   g.issuer,
		Audience: g.audience,
	})
	if err != nil {
		return "", fmt.Errorf("hcmctl: configuring the JIT signer: %w", err)
	}

	now := time.Now()
	var roles []string
	for _, r := range strings.Split(g.roles, ",") {
		if r = strings.TrimSpace(r); r != "" {
			roles = append(roles, r)
		}
	}

	token, err := verifier.Issue(trust.Claims{
		Issuer:               g.issuer,
		Audience:             g.audience,
		Subject:              g.subject,
		SubjectKind:          "human",
		Tenant:               g.tenant,
		Roles:                roles,
		Purposes:             []string{g.purpose},
		AuthenticationMethod: "bearer_token",
		Assurance:            "high",
		SessionRef:           "hcmctl-jit-session",
		IssuedAtUnix:         now.Unix(),
		ExpiresAtUnix:        now.Add(g.ttl).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("hcmctl: minting the JIT credential: %w", err)
	}
	return token, nil
}
