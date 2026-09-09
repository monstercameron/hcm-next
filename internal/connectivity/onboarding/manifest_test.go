package onboarding_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

func preflightOf(h *harness, pub ed25519.PublicKey) onboarding.Preflight {
	return onboarding.Preflight{Connector: h.Incumbent, Connection: h.Connection, PublicKey: pub, Now: h.Clock}
}

// rejection asserts err is an *onboarding.Error carrying the ONBOARD-001
// rejection code, returning it for further field-level assertions.
func rejection(t *testing.T, result onboarding.PreflightResult, err error) *onboarding.Error {
	t.Helper()
	if result.Passed {
		t.Fatal("preflight passed a manifest that should have been rejected")
	}
	if err == nil {
		t.Fatal("preflight returned no error for a rejected manifest")
	}
	var oerr *onboarding.Error
	if !errors.As(err, &oerr) {
		t.Fatalf("error is not *onboarding.Error: %v", err)
	}
	if oerr.Code != onboarding.CodeManifestRejected {
		t.Fatalf("code = %q, want %q", oerr.Code, onboarding.CodeManifestRejected)
	}
	if !errors.Is(err, onboarding.ErrRejected) {
		t.Fatalf("error does not wrap ErrRejected: %v", err)
	}
	if result.Reason != oerr {
		t.Fatalf("result.Reason %p is not the returned error %p", result.Reason, oerr)
	}
	return oerr
}

// TestTodo_ONBOARD_001 proves the manifest cannot be signed without every
// required governance fact, and that preflight refuses to activate it on any
// mismatch between what it claims and what the live connector/connection
// actually offer - always naming the offending field.
func TestTodo_ONBOARD_001(t *testing.T) {
	t.Parallel()
	pub, priv := testKeyPair(t)

	t.Run("a validly signed manifest passes preflight", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		sm, err := onboarding.SignManifest(validManifest(h), "key-2026-09", priv)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if !result.Passed {
			t.Fatalf("preflight did not pass: %+v", result)
		}
		if result.Reason != nil {
			t.Fatalf("a passing result carries a reason: %+v", result.Reason)
		}
	})

	t.Run("an unsigned manifest with unresolved authority is rejected and persists nothing", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		m.SourceAuthorityRef = "authority.unresolved.nonexistent"
		// Seeded defect: never signed. SignManifest was never called, so
		// Signature and SignerKeyID are the zero value.
		sm := onboarding.SignedManifest{Manifest: m}

		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field == "" {
			t.Fatal("rejection names no offending field")
		}

		obs, lerr := h.Store.List(context.Background(), observe.Query{TenantID: testTenant})
		if lerr != nil {
			t.Fatalf("list observations: %v", lerr)
		}
		if len(obs) != 0 {
			t.Fatalf("rejected preflight left %d observations behind", len(obs))
		}
	})

	t.Run("a properly signed manifest naming an authority the connector does not observe under is rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		m.SourceAuthorityRef = "authority.someone.else"
		sm := mustSign(t, m, priv)

		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field != "source_authority_ref" {
			t.Fatalf("field = %q, want source_authority_ref", oerr.Field)
		}
		if oerr.State != h.Incumbent.Descriptor().AuthorityRef {
			t.Fatalf("state = %q, want the connector's own authority ref", oerr.State)
		}
	})

	t.Run("a mismatched connector version is rejected naming the offending version", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		m.ConnectorVersion = connectivity.Version{Major: 9, Minor: 9, Patch: 9}
		sm := mustSign(t, m, priv)

		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field != "connector_version" {
			t.Fatalf("field = %q, want connector_version", oerr.Field)
		}
		if oerr.Version != h.Connection.ConnectorVersion().String() {
			t.Fatalf("version = %q, want %q", oerr.Version, h.Connection.ConnectorVersion().String())
		}
	})

	t.Run("a mismatched schema version pin is rejected naming the offending object's live version", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		m.SchemaVersionPins[connectivity.ObjectWorker] = "workday.worker/1999.1"
		sm := mustSign(t, m, priv)

		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field != "schema_version" {
			t.Fatalf("field = %q, want schema_version", oerr.Field)
		}
		if oerr.State != "workday.worker/2026.1" {
			t.Fatalf("state (live schema) = %q, want workday.worker/2026.1", oerr.State)
		}
		if oerr.Version != "workday.worker/1999.1" {
			t.Fatalf("version (pinned schema) = %q", oerr.Version)
		}
	})

	t.Run("an unusable connection state is rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		sm := mustSign(t, validManifest(h), priv)
		if err := h.Connection.Transition(connectivity.StateSuspended, evidence("pause", time.Hour)); err != nil {
			t.Fatalf("suspend: %v", err)
		}

		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field != "connection_state" {
			t.Fatalf("field = %q, want connection_state", oerr.Field)
		}
		if oerr.State != string(connectivity.StateSuspended) {
			t.Fatalf("state = %q, want SUSPENDED", oerr.State)
		}
	})

	t.Run("a manifest claiming a capability the connection never claimed is rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		registry := connectivity.NewRegistry()
		pubDef, err := registry.Publish(fakeincumbent.DefaultDefinition(), connectivity.PublicationMeta{
			PublishedBy: "user:platform@hcmnext", PublishedAt: publishedAt,
		})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		credential, err := connectivity.ParseCredentialRef("secretref://harborcare/workday/client")
		if err != nil {
			t.Fatalf("credential: %v", err)
		}
		narrow, err := connectivity.NewConnection(pubDef, connectivity.ConnectionSpec{
			ConnectionID: "conn-worker-only", TenantID: testTenant, OrgID: "org-harborcare-us",
			SystemID: "sys-workday-prod", Environment: connectivity.EnvironmentProduction, Residency: "us-east",
			ConnectorID: pubDef.Definition.ConnectorID, ConnectorVersion: pubDef.Definition.Version,
			AuthMode: connectivity.AuthOAuth2ClientCredentials, CredentialRef: credential,
			Scopes: []string{"worker.read"},
			EndpointPolicy: connectivity.EndpointPolicy{
				AllowedHosts: []string{"api.workday.example"}, RequireTLS: true, EgressProfile: "cell-egress/us-east",
			},
			Capabilities: connectivity.ReadCapabilities(connectivity.ObjectWorker),
			Bounds:       pubDef.Definition.Bounds,
			CreatedAt:    draftedAt,
		})
		if err != nil {
			t.Fatalf("new connection: %v", err)
		}
		for i, step := range []connectivity.LifecycleState{
			connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive,
		} {
			if err := narrow.Transition(step, evidence("enable", time.Duration(i+1)*time.Minute)); err != nil {
				t.Fatalf("transition: %v", err)
			}
		}

		m := validManifest(h)
		m.ConnectionID = narrow.ID()
		sm := mustSign(t, m, priv)
		pf := onboarding.Preflight{Connector: h.Incumbent, Connection: narrow, PublicKey: pub, Now: h.Clock}

		result, err := pf.Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field != "capability" {
			t.Fatalf("field = %q, want capability", oerr.Field)
		}
	})
}

// TestTodo_ONBOARD_001_Golden pins the manifest's canonical wire encoding: a
// fixed manifest must always digest and sign to the same bytes, so a change
// to the canonical encoding is caught here rather than by a silently
// different digest downstream.
func TestTodo_ONBOARD_001_Golden(t *testing.T) {
	t.Parallel()
	pub, priv := testKeyPair(t)
	h := newHarness(t)
	m := validManifest(h)

	digest, err := m.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	sm, err := onboarding.SignManifest(m, "key-2026-09", priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if sm.Digest != digest {
		t.Fatalf("signed digest %s does not match standalone digest %s", sm.Digest, digest)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "manifest_id: %s\n", m.ManifestID)
	fmt.Fprintf(&b, "tenant_id: %s\n", m.TenantID)
	fmt.Fprintf(&b, "owner_ref: %s\n", m.OwnerRef)
	fmt.Fprintf(&b, "source_authority_ref: %s\n", m.SourceAuthorityRef)
	fmt.Fprintf(&b, "connector: %s@%s\n", m.ConnectorID, m.ConnectorVersion)
	fmt.Fprintf(&b, "connection_id: %s\n", m.ConnectionID)
	fmt.Fprintf(&b, "objects: %v\n", m.Objects)
	fmt.Fprintf(&b, "budget: pages=%d records=%d bytes=%d wall=%s\n",
		m.Budget.MaxPages, m.Budget.MaxRecords, m.Budget.MaxBytes, m.Budget.MaxWallTime)
	fmt.Fprintf(&b, "digest: %s\n", digest)
	fmt.Fprintf(&b, "signer_key_id: %s\n", sm.SignerKeyID)
	compareGoldenFile(t, filepath.Join("testdata", "onboard001_manifest.golden"), b.String())

	// The digest is deterministic across repeated computation.
	again, err := m.Digest()
	if err != nil {
		t.Fatalf("digest again: %v", err)
	}
	if again != digest {
		t.Fatalf("digest is not stable across recomputation: %s vs %s", again, digest)
	}

	status, err := onboarding.VerifyManifest(sm, pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if status != onboarding.VerifyValid {
		t.Fatalf("status = %s, want VALID", status)
	}
}

func compareGoldenFile(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(got, "\r\n", "\n") {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// TestTodo_ONBOARD_001_Race proves signing, verifying and preflighting a
// manifest are safe under concurrent use and always agree with each other:
// many goroutines checking the identical inputs must reach the identical
// verdict.
func TestTodo_ONBOARD_001_Race(t *testing.T) {
	t.Parallel()
	pub, priv := testKeyPair(t)
	h := newHarness(t)
	sm := mustSign(t, validManifest(h), priv)
	pf := preflightOf(h, pub)

	const goroutines = 32
	var wg sync.WaitGroup
	passed := make([]bool, goroutines)
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := pf.Run(context.Background(), sm)
			passed[idx], errs[idx] = result.Passed, err
		}(i)
	}
	wg.Wait()

	for i := range goroutines {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if !passed[i] {
			t.Fatalf("goroutine %d: preflight did not pass", i)
		}
	}
}

// TestTodo_ONBOARD_001_Security proves a manifest tampered with after signing
// is caught as TAMPERED rather than silently re-verified, that the wrong key
// is caught as INVALID_SIGNATURE, and that preflight itself refuses a
// tampered manifest rather than trusting the caller's own Digest field.
func TestTodo_ONBOARD_001_Security(t *testing.T) {
	t.Parallel()
	pub, priv := testKeyPair(t)

	t.Run("content changed after signing is TAMPERED", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		sm := mustSign(t, validManifest(h), priv)
		sm.Manifest.Budget.MaxRecords *= 1000
		status, err := onboarding.VerifyManifest(sm, pub)
		if status != onboarding.VerifyTampered {
			t.Fatalf("status = %s, want TAMPERED", status)
		}
		if !errors.Is(err, onboarding.ErrManifestTampered) {
			t.Fatalf("error does not wrap ErrManifestTampered: %v", err)
		}
	})

	t.Run("the recorded digest edited to match tampered content is still not the signed one", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		sm := mustSign(t, validManifest(h), priv)
		sm.Manifest.Budget.MaxRecords *= 1000
		recomputed, err := sm.Manifest.Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		sm.Digest = recomputed // forge the recorded digest to match
		status, err := onboarding.VerifyManifest(sm, pub)
		if status != onboarding.VerifyInvalidSignature {
			t.Fatalf("status = %s, want INVALID_SIGNATURE", status)
		}
		if !errors.Is(err, onboarding.ErrSignatureInvalid) {
			t.Fatalf("error does not wrap ErrSignatureInvalid: %v", err)
		}
	})

	t.Run("the wrong public key is INVALID_SIGNATURE", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		sm := mustSign(t, validManifest(h), priv)
		wrongPub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		status, err := onboarding.VerifyManifest(sm, wrongPub)
		if status != onboarding.VerifyInvalidSignature {
			t.Fatalf("status = %s, want INVALID_SIGNATURE", status)
		}
		if !errors.Is(err, onboarding.ErrSignatureInvalid) {
			t.Fatalf("error does not wrap ErrSignatureInvalid: %v", err)
		}
	})

	t.Run("preflight refuses a tampered manifest even though it still claims a valid digest", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		sm := mustSign(t, validManifest(h), priv)
		sm.Manifest.CreatedBy = "attacker:forged"
		// sm.Digest is left as the original, now-stale, recorded digest: the
		// forgery attempt is "change the content but not the recorded proof".

		result, err := preflightOf(h, pub).Run(context.Background(), sm)
		oerr := rejection(t, result, err)
		if oerr.Field != "signature" {
			t.Fatalf("field = %q, want signature", oerr.Field)
		}
	})

	t.Run("an empty signer key id is refused rather than silently signing anonymously", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, err := onboarding.SignManifest(validManifest(h), "", priv)
		if err == nil {
			t.Fatal("signing with an empty key id did not fail")
		}
		if !errors.Is(err, onboarding.ErrInvalidManifest) {
			t.Fatalf("error does not wrap ErrInvalidManifest: %v", err)
		}
	})
}

// TestTodo_ONBOARD_001_Mutation proves every governance-material manifest
// field changes the signed digest, so a mutation to any of them is caught by
// signature verification rather than silently accepted.
func TestTodo_ONBOARD_001_Mutation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	base := validManifest(h)
	baseDigest, err := base.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	mutations := map[string]func(*onboarding.OnboardingManifest){
		"manifest id":       func(m *onboarding.OnboardingManifest) { m.ManifestID += "-x" },
		"tenant":            func(m *onboarding.OnboardingManifest) { m.TenantID += "-x" },
		"owner":             func(m *onboarding.OnboardingManifest) { m.OwnerRef += "-x" },
		"source authority":  func(m *onboarding.OnboardingManifest) { m.SourceAuthorityRef += "-x" },
		"classification":    func(m *onboarding.OnboardingManifest) { m.Classification += "-x" },
		"residency":         func(m *onboarding.OnboardingManifest) { m.Residency += "-x" },
		"retention policy":  func(m *onboarding.OnboardingManifest) { m.RetentionPolicy += "-x" },
		"rollback policy":   func(m *onboarding.OnboardingManifest) { m.RollbackPolicy += "-x" },
		"idempotency key":   func(m *onboarding.OnboardingManifest) { m.IdempotencyKey += "-x" },
		"connector id":      func(m *onboarding.OnboardingManifest) { m.ConnectorID += "-x" },
		"connector version": func(m *onboarding.OnboardingManifest) { m.ConnectorVersion.Patch++ },
		"connection id":     func(m *onboarding.OnboardingManifest) { m.ConnectionID += "-x" },
		"schema pin": func(m *onboarding.OnboardingManifest) {
			pins := map[connectivity.ObjectKind]string{}
			for k, v := range m.SchemaVersionPins {
				pins[k] = v
			}
			pins[connectivity.ObjectWorker] += "-x"
			m.SchemaVersionPins = pins
		},
		"budget max pages":   func(m *onboarding.OnboardingManifest) { m.Budget.MaxPages++ },
		"budget max records": func(m *onboarding.OnboardingManifest) { m.Budget.MaxRecords++ },
		"budget max bytes":   func(m *onboarding.OnboardingManifest) { m.Budget.MaxBytes++ },
		"budget wall time":   func(m *onboarding.OnboardingManifest) { m.Budget.MaxWallTime += time.Second },
		"created at":         func(m *onboarding.OnboardingManifest) { m.CreatedAt = m.CreatedAt.Add(time.Second) },
		"created by":         func(m *onboarding.OnboardingManifest) { m.CreatedBy += "-x" },
		"field allow list": func(m *onboarding.OnboardingManifest) {
			m.FieldAllowList = map[connectivity.ObjectKind][]string{connectivity.ObjectWorker: {"legal_name"}}
		},
		"expected population": func(m *onboarding.OnboardingManifest) {
			m.ExpectedPopulation = map[connectivity.ObjectKind]int64{connectivity.ObjectWorker: 42}
		},
		"reference pin": func(m *onboarding.OnboardingManifest) {
			m.ReferencePins = []onboarding.ReferencePin{{Name: "x", Version: "1", Digest: "ab"}}
		},
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mutated := validManifest(h)
			mutate(&mutated)
			got, err := mutated.Digest()
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			if got == baseDigest {
				t.Fatalf("changing the %s did not change the digest", name)
			}
		})
	}
}
