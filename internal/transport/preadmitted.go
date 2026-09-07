package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// preAdmission is the immutable result of the edge's early admission pass.
// Its unexported type and context key keep handlers from constructing or
// consuming an admission result themselves.
type preAdmission struct {
	principal   *trust.Principal
	requestID   string
	method      string
	fingerprint string
}

type preAdmissionContextKey struct{}

// WithPreAdmission performs early admission and carries its result forward
// for Admit to reuse when the request metadata is unchanged.
func WithPreAdmission(ctx context.Context, cfg Config, md Metadata, method string) (context.Context, *trust.Principal, string, *envelope.Error) {
	principal, requestID, admitErr := PreAdmit(ctx, cfg, md, method)
	if admitErr != nil {
		return ctx, nil, requestID, admitErr
	}
	carrier := &preAdmission{
		principal:   principal,
		requestID:   requestID,
		method:      method,
		fingerprint: preAdmissionFingerprint(md),
	}
	return context.WithValue(ctx, preAdmissionContextKey{}, carrier), principal, requestID, nil
}

// preAdmissionFingerprint hashes the metadata that early admission consumes.
// Values are length-delimited so distinct metadata sequences cannot share an
// encoding; reserved names are sorted by trust.RejectCallerSelectedAuthority.
func preAdmissionFingerprint(md Metadata) string {
	if md == nil {
		md = MapMetadata(nil)
	}
	h := sha256.New()
	write := func(label, value string) {
		fmt.Fprintf(h, "%s=%d:%s;", label, len(value), value)
	}
	writeValues := func(label string, values []string) {
		write(label+".count", fmt.Sprintf("%d", len(values)))
		for i, value := range values {
			write(fmt.Sprintf("%s.%d", label, i), value)
		}
	}
	writeValues(AuthorizationMetadataKey, md.Get(AuthorizationMetadataKey))
	writeValues(RequestIDMetadataKey, md.Get(RequestIDMetadataKey))
	for _, key := range trust.RejectCallerSelectedAuthority(md.Keys()) {
		write("reserved", key)
	}
	return hex.EncodeToString(h.Sum(nil))
}
