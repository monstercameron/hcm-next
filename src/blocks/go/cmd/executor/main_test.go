package main

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"hcm-next-executor/internal/blocks/compensation"
	"hcm-next-executor/internal/blocks/contactinfo"
	"hcm-next-executor/internal/blocks/emergencycontact"
	"hcm-next-executor/internal/blocks/legalname"
	"hcm-next-executor/internal/blocks/orgtransfer"
	"hcm-next-executor/internal/blocks/termination"
	"hcm-next-executor/internal/executor"
)

// registeredBlockNames lists every block reference the service binary must
// serve: each of the six block families registers exactly one preflight and
// one plan-transaction block.
func registeredBlockNames() []executor.BlockReference {
	families := []struct {
		preflight string
		planTx    string
		version   string
	}{
		{legalname.PreflightBlockName, legalname.PlanTransactionBlockName, legalname.BlockVersionV1},
		{emergencycontact.PreflightBlockName, emergencycontact.PlanTransactionBlockName, emergencycontact.BlockVersionV1},
		{contactinfo.PreflightBlockName, contactinfo.PlanTransactionBlockName, contactinfo.BlockVersionV1},
		{compensation.PreflightBlockName, compensation.PlanTransactionBlockName, compensation.BlockVersionV1},
		{orgtransfer.PreflightBlockName, orgtransfer.PlanTransactionBlockName, orgtransfer.BlockVersionV1},
		{termination.PreflightBlockName, termination.PlanTransactionBlockName, termination.BlockVersionV1},
	}
	var refs []executor.BlockReference
	for _, family := range families {
		refs = append(refs,
			executor.BlockReference{Name: family.preflight, Version: family.version},
			executor.BlockReference{Name: family.planTx, Version: family.version},
		)
	}
	return refs
}

// TestMainWiringRegistersEveryBlock proves the exact registration wiring
// main() performs: all six families register cleanly and every one of the
// twelve expected name@version references ends up in the registry.
func TestMainWiringRegistersEveryBlock(t *testing.T) {
	registry := executor.NewRegistry()

	registrations := []func(*executor.Registry) error{
		legalname.RegisterBlocks,
		emergencycontact.RegisterBlocks,
		contactinfo.RegisterBlocks,
		compensation.RegisterBlocks,
		orgtransfer.RegisterBlocks,
		termination.RegisterBlocks,
	}
	for _, register := range registrations {
		if err := register(registry); err != nil {
			t.Fatalf("RegisterBlocks: %v", err)
		}
	}

	dummy := func(executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
		return executor.BlockResult{}, nil
	}
	for _, ref := range registeredBlockNames() {
		err := registry.Register(ref, dummy)
		if err == nil || !strings.Contains(err.Error(), "already registered") {
			t.Errorf("re-registering %s@%s = %v, want an already-registered error", ref.Name, ref.Version, err)
		}
	}
}

// TestMainWiringDispatchesEveryRegisteredBlock proves every wired block is
// reachable through the registry: an execution request for each of the twelve
// name@version references must reach a handler (any typed block error is
// fine; an unknown-block error or a panic in the handler is a wiring or
// regression bug, since the HTTP server never recovers handler panics).
func TestMainWiringDispatchesEveryRegisteredBlock(t *testing.T) {
	registry := executor.NewRegistry()

	for _, register := range []func(*executor.Registry) error{
		legalname.RegisterBlocks,
		emergencycontact.RegisterBlocks,
		contactinfo.RegisterBlocks,
		compensation.RegisterBlocks,
		orgtransfer.RegisterBlocks,
		termination.RegisterBlocks,
	} {
		if err := register(registry); err != nil {
			t.Fatalf("RegisterBlocks: %v", err)
		}
	}

	for _, ref := range registeredBlockNames() {
		ref := ref
		t.Run(ref.Name+"@"+ref.Version, func(t *testing.T) {
			request := executor.ExecutionRequest{
				TenantID:           "tenant-t1",
				EnvironmentID:      "env-1",
				WorkflowInstanceID: "wf-1",
				WorkflowVersionID:  "wfver-1",
				Block:              ref,
				Input:              json.RawMessage(`{}`),
				Context: executor.ExecutionContext{
					ActorID:        "actor-1",
					EffectiveAt:    "2026-09-03T00:00:00Z",
					CorrelationID:  "corr-1",
					IdempotencyKey: "idem-1",
				},
			}
			_, execErr := registry.Execute(request)
			if execErr != nil && execErr.Code == executor.ErrorCodeUnknownBlock {
				t.Fatalf("registry.Execute returned unknown block for wired reference %s@%s", ref.Name, ref.Version)
			}
		})
	}
}

// TestMainWiringBuildsAndServeShapedServer proves the service constructs
// with the bounded HTTP server shape main() serves (fixed timeouts, handler
// attached) without binding a port.
func TestMainWiringBuildsAndServeShapedServer(t *testing.T) {
	registry := executor.NewRegistry()
	for _, register := range []func(*executor.Registry) error{
		legalname.RegisterBlocks,
		emergencycontact.RegisterBlocks,
		contactinfo.RegisterBlocks,
		compensation.RegisterBlocks,
		orgtransfer.RegisterBlocks,
		termination.RegisterBlocks,
	} {
		if err := register(registry); err != nil {
			t.Fatalf("RegisterBlocks: %v", err)
		}
	}

	logger := slog.New(slog.DiscardHandler)
	service := executor.NewServer(registry, logger.With("service", "executor"))
	if service == nil {
		t.Fatal("NewServer returned nil")
	}
	routes := service.Routes()
	if routes == nil {
		t.Fatal("Routes returned nil")
	}

	httpServer := executor.NewHTTPServer("127.0.0.1:0", routes)
	if httpServer == nil {
		t.Fatal("NewHTTPServer returned nil")
	}
	if httpServer.Addr != "127.0.0.1:0" {
		t.Errorf("server Addr = %q, want the configured address", httpServer.Addr)
	}
	if httpServer.Handler == nil {
		t.Error("server has no handler")
	}
	if httpServer.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", httpServer.ReadHeaderTimeout)
	}
	if httpServer.ReadTimeout != 10*time.Second || httpServer.WriteTimeout != 10*time.Second || httpServer.IdleTimeout != 60*time.Second {
		t.Errorf("server timeouts = read %v write %v idle %v, want 10s 10s 60s", httpServer.ReadTimeout, httpServer.WriteTimeout, httpServer.IdleTimeout)
	}
}
