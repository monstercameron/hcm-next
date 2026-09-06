package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/platform/execution/scheduler"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

// This command owns nothing but its flag set and its role. Everything below
// that -- the tick, the leases, the admission rule, the dispatch settlement --
// is internal/platform/execution/scheduler's, and is tested there. These tests
// are therefore deliberately narrow: they check that the right role is
// selected, that configuration is parsed and refused before anything starts,
// and that the one identifier this command parses becomes the claim the
// runtime package serves.

func noEnv(string) (string, bool) { return "", false }

func minimalArgs(tenantID uuid.UUID) []string {
	return []string{"-database-url=postgres://localhost/hcmnext", "-tenant-id=" + tenantID.String()}
}

// TestSpecSelectsTheSchedulerRole is the whole of this command's composition
// responsibility: name the role, hand bootstrap the runtime package's
// configuration surface, and let bootstrap.Run do the rest.
func TestSpecSelectsTheSchedulerRole(t *testing.T) {
	s := spec(minimalArgs(uuid.New()))
	if s.Role != bootstrap.RoleScheduler {
		t.Fatalf("role = %q, want %q", s.Role, bootstrap.RoleScheduler)
	}
	if err := s.Role.Validate(); err != nil {
		t.Fatalf("the selected role is not in the process-roles vocabulary: %v", err)
	}
	if s.DatabaseURLField != scheduler.FieldDatabaseURL {
		t.Fatalf("database URL field = %q, want %q", s.DatabaseURLField, scheduler.FieldDatabaseURL)
	}
	if s.Validate == nil || s.Build == nil || s.DBPoolFactory == nil {
		t.Fatal("the spec leaves validation, build or the pool factory unwired")
	}
	if len(s.ConfigFields) != len(scheduler.ConfigFields()) {
		t.Fatalf("the spec declares %d fields, the runtime package declares %d",
			len(s.ConfigFields), len(scheduler.ConfigFields()))
	}
}

// TestSpecPreResolvesTheHealthAddress covers the one piece of parsing this
// command does ahead of bootstrap.Run, because Spec.HealthAddr is a plain
// field Run reads before it parses ConfigFields at all.
func TestSpecPreResolvesTheHealthAddress(t *testing.T) {
	args := append(minimalArgs(uuid.New()), "-health-addr=127.0.0.1:9464")
	if got := spec(args).HealthAddr; got != "127.0.0.1:9464" {
		t.Fatalf("health address = %q, want the flag's value", got)
	}
	if got := spec(minimalArgs(uuid.New())).HealthAddr; got != "" {
		t.Fatalf("health address = %q with no flag, want empty (the endpoint disabled)", got)
	}
	// A spec built from unparsable arguments still comes back so that
	// bootstrap.Run reports the parse failure itself, with the banner.
	if got := spec([]string{"-not-a-flag"}).HealthAddr; got != "" {
		t.Fatalf("health address = %q from unparsable arguments, want empty", got)
	}
}

// TestValidateConfigRefusesABadTenantIdentifier is the one check this command
// adds to the runtime package's own: internal/platform may not import
// github.com/google/uuid, so parsing the tenant is this root's job, and doing
// it at validation time makes an unparsable tenant a startup failure rather
// than a first-tick one.
func TestValidateConfigRefusesABadTenantIdentifier(t *testing.T) {
	for name, tenant := range map[string]string{
		"not a uuid": "the-acme-corporation",
		"truncated":  "0f9d1a1e-1111-2222",
		"nil uuid":   uuid.Nil.String(),
	} {
		t.Run(name, func(t *testing.T) {
			values, err := bootstrap.ParseConfig(
				[]string{"-database-url=postgres://localhost/x", "-tenant-id=" + tenant}, noEnv, scheduler.ConfigFields())
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			err = validateConfig(values)
			if err == nil {
				t.Fatalf("validateConfig accepted tenant %q", tenant)
			}
			if !strings.Contains(err.Error(), scheduler.FieldTenantID) {
				t.Fatalf("refusal %q does not name the offending flag", err)
			}
		})
	}
}

func TestValidateConfigAcceptsTheMinimalConfiguration(t *testing.T) {
	values, err := bootstrap.ParseConfig(minimalArgs(uuid.New()), noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if err := validateConfig(values); err != nil {
		t.Fatalf("validateConfig on the minimal configuration: %v", err)
	}
}

// TestValidateConfigDefersToTheRuntimePackage proves this command adds a check
// rather than replacing one: a configuration the runtime package refuses is
// still refused here.
func TestValidateConfigDefersToTheRuntimePackage(t *testing.T) {
	values, err := bootstrap.ParseConfig(
		[]string{"-tenant-id=" + uuid.New().String()}, noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if err := validateConfig(values); err == nil {
		t.Fatal("validateConfig accepted a configuration with no database URL")
	}
}

// TestClaimOfCarriesTheParsedTenantIntoTheLeaseRequest is the seam between the
// two roots: the identifier this command parsed travels to the runtime package
// inside a lease.AcquireRequest, which is how that package handles a tenant
// without naming the identifier type.
func TestClaimOfCarriesTheParsedTenantIntoTheLeaseRequest(t *testing.T) {
	tenantID := uuid.New()
	args := append(minimalArgs(tenantID), "-queue-key=queue:workflow-runtime-shard-2")
	values, err := bootstrap.ParseConfig(args, noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	deps := bootstrap.Deps{Values: values, Identity: "role:scheduler:pid:99"}

	claim, err := claimOf(deps)
	if err != nil {
		t.Fatalf("claimOf: %v", err)
	}
	if claim.TenantID != tenantID {
		t.Fatalf("claim tenant = %s, want %s", claim.TenantID, tenantID)
	}
	if claim.Resource.Kind != lease.ResourceQueue || claim.Resource.ID != "queue:workflow-runtime-shard-2" {
		t.Fatalf("claim resource = %+v, want the configured QUEUE", claim.Resource)
	}
	if claim.Holder != (lease.Identity{
		WorkloadRef: scheduler.DefaultWorkloadRef, InstanceRef: "role:scheduler:pid:99",
	}) {
		t.Fatalf("claim holder = %+v, want the configured workload and this process instance", claim.Holder)
	}
}

func TestClaimOfRefusesABadTenantIdentifier(t *testing.T) {
	values, err := bootstrap.ParseConfig(
		[]string{"-database-url=postgres://localhost/x", "-tenant-id=not-a-uuid"}, noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if _, err := claimOf(bootstrap.Deps{Values: values, Identity: "role:scheduler:pid:1"}); err == nil {
		t.Fatal("claimOf accepted an unparsable tenant")
	}
}
