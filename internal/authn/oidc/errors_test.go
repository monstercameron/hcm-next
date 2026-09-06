package oidc_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/authn/oidc"
)

// TestSentinelErrorsAreDistinct proves every exported sentinel is non-nil
// and distinguishable from every other one via errors.Is, so a caller that
// switches on these typed reasons never accidentally conflates two of them.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()
	sentinels := map[string]error{
		"ErrInvalidFlowConfig":         oidc.ErrInvalidFlowConfig,
		"ErrInvalidClientRegistration": oidc.ErrInvalidClientRegistration,
		"ErrClientNotRegistered":       oidc.ErrClientNotRegistered,
		"ErrAuthorizationDenied":       oidc.ErrAuthorizationDenied,
		"ErrCallbackMalformed":         oidc.ErrCallbackMalformed,
		"ErrReplayedOrUnknownState":    oidc.ErrReplayedOrUnknownState,
		"ErrVerifierExpired":           oidc.ErrVerifierExpired,
		"ErrPKCEDerivationMismatch":    oidc.ErrPKCEDerivationMismatch,
		"ErrGrantRejected":             oidc.ErrGrantRejected,
		"ErrMissingIDToken":            oidc.ErrMissingIDToken,
		"ErrIDTokenMalformed":          oidc.ErrIDTokenMalformed,
		"ErrUnsupportedAlgorithm":      oidc.ErrUnsupportedAlgorithm,
		"ErrAlgorithmDowngrade":        oidc.ErrAlgorithmDowngrade,
		"ErrSignatureInvalid":          oidc.ErrSignatureInvalid,
		"ErrKeyNotFound":               oidc.ErrKeyNotFound,
		"ErrKeyExpired":                oidc.ErrKeyExpired,
		"ErrWrongIssuer":               oidc.ErrWrongIssuer,
		"ErrWrongAudience":             oidc.ErrWrongAudience,
		"ErrIDTokenExpired":            oidc.ErrIDTokenExpired,
		"ErrNonceMismatch":             oidc.ErrNonceMismatch,
		"ErrAccessTokenMismatch":       oidc.ErrAccessTokenMismatch,
		"ErrClaimMapping":              oidc.ErrClaimMapping,
	}
	for name, err := range sentinels {
		if err == nil {
			t.Fatalf("%s is nil", name)
		}
	}
	for nameA, a := range sentinels {
		for nameB, b := range sentinels {
			if nameA == nameB {
				continue
			}
			if errors.Is(a, b) {
				t.Fatalf("%s and %s are not distinguishable (errors.Is reports a match)", nameA, nameB)
			}
		}
	}
}
