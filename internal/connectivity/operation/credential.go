package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// MachineLeaseAuthorizer is the narrow TRUST-029 boundary used at dispatch.
// Implementations must validate tenant, destination, operation, expiry and
// revocation epoch before returning; they never return credential material.
type MachineLeaseAuthorizer interface {
	Use(lease.MachineCredentialLease, string, custody.Operation) (lease.MachineEvidence, error)
}

// CredentialWriter receives only a reference-only machine lease in addition
// to the ordinary provider request. A provider adapter may use that lease at
// its own custody/egress boundary, but cannot receive a persisted raw secret.
type CredentialWriter interface {
	WriteWithCredential(context.Context, WriteRequest, lease.MachineCredentialLease) (WriteResponse, error)
}

// CredentialDispatchRequest supplies the exact dispatch credential binding.
// CredentialOperation is the custody operation permitted by the short-lived
// lease, not the vendor-specific semantic operation name.
type CredentialDispatchRequest struct {
	Lease               Lease
	Credential          lease.MachineCredentialLease
	CredentialOperation custody.Operation
	Writer              CredentialWriter
}

type credentialWriterAdapter struct {
	writer     CredentialWriter
	credential lease.MachineCredentialLease
}

func credentialRejection(field string, state State, version uint64, cause error) *CredentialRejection {
	return &CredentialRejection{Code: CredentialRejectedCode, Field: field, State: state, Version: version, Cause: cause}
}

func (w credentialWriterAdapter) Write(ctx context.Context, req WriteRequest) (WriteResponse, error) {
	return w.writer.WriteWithCredential(ctx, req, w.credential)
}

// DispatchWithCredential binds and consumes the machine credential lease
// immediately before dispatch. Any refusal returns before the operation moves
// to SENDING and before the provider writer is called.
func (j *MemoryJournal) DispatchWithCredential(ctx context.Context, req CredentialDispatchRequest, authorizer MachineLeaseAuthorizer) (DispatchResult, error) {
	if err := contextError(ctx); err != nil {
		return DispatchResult{}, err
	}
	if authorizer == nil || req.Writer == nil {
		return DispatchResult{}, ErrCredentialRequired
	}
	op, err := j.Get(ctx, req.Lease.TenantID, req.Lease.OperationID)
	if err != nil {
		return DispatchResult{}, err
	}
	if req.Credential.Tenant != op.TenantID {
		return DispatchResult{}, credentialRejection("tenant", op.State, op.FenceToken, fmt.Errorf("want %q", op.TenantID))
	}
	if op.State != StateLeased {
		return DispatchResult{}, credentialRejection("state", op.State, op.FenceToken, ErrLeaseRequired)
	}
	if req.Lease.Token == uuid.Nil || req.Lease.FenceToken != op.FenceToken || req.Lease.ExpiresAt.IsZero() {
		return DispatchResult{}, credentialRejection("dispatch_lease", op.State, op.FenceToken, ErrLeaseFenced)
	}
	if req.Credential.Lease.Destination != op.DestinationRef {
		return DispatchResult{}, credentialRejection("destination", op.State, op.FenceToken, fmt.Errorf("want %q", op.DestinationRef))
	}
	if req.CredentialOperation == "" {
		return DispatchResult{}, credentialRejection("operation", op.State, op.FenceToken, custody.ErrDenied)
	}
	if _, err := authorizer.Use(req.Credential, op.DestinationRef, req.CredentialOperation); err != nil {
		return DispatchResult{}, credentialRejection(credentialField(err), op.State, op.FenceToken, err)
	}

	j.mu.Lock()
	current, ok := j.operations[op.OperationID]
	if !ok {
		j.mu.Unlock()
		return DispatchResult{}, fmt.Errorf("%w: %s", ErrNotFound, op.OperationID)
	}
	if current.State != StateLeased || current.FenceToken != req.Lease.FenceToken {
		j.mu.Unlock()
		return DispatchResult{}, fmt.Errorf("%w: credential-bound lease no longer owns operation", ErrLeaseFenced)
	}
	j.appendJournalLocked(current, current.State, current.State, "CREDENTIAL_BOUND", uuid.Nil, req.Lease.FenceToken, req.Credential.Lease.ID, "", current.UpdatedAt)
	j.mu.Unlock()
	return j.Dispatch(ctx, req.Lease, credentialWriterAdapter{writer: req.Writer, credential: req.Credential})
}

func credentialField(err error) string {
	switch {
	case errors.Is(err, lease.ErrExpired):
		return "expiry"
	case errors.Is(err, lease.ErrRevoked), errors.Is(err, lease.ErrEpochStale):
		return "revocation"
	case errors.Is(err, lease.ErrDestinationMismatch):
		return "destination"
	case errors.Is(err, lease.ErrOperationNotPermitted):
		return "operation"
	case errors.Is(err, lease.ErrTampered), errors.Is(err, lease.ErrUnknown):
		return "lease"
	default:
		return "credential"
	}
}
