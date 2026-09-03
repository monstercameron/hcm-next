package explorer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
)

// ChainView is one stream's hash-chain verification result
// (internal/data/ledger/hashchain.Digester.Verify), rendered for display.
type ChainView struct {
	StreamKey string
	// Verified is true when the chain replayed cleanly from genesis to the
	// stream's current head.
	Verified bool
	// Head is the reproduced chain head when Verified is true.
	Head hashchain.Head
	// Empty is true when the stream has no events at all (hashchain.ErrEmptyStream):
	// this is not a break, just nothing to verify yet.
	Empty bool
	// Broken carries the exact sequence and reason verification failed at,
	// when Verified is false and Empty is false.
	Broken *BrokenLink
}

// BrokenLink is the exact point a hash-chain verification failed at
// (hashchain.ErrChainBroken), naming the sequence so an operator does not
// have to re-derive it from a stack trace or a full stream dump.
type BrokenLink struct {
	StreamKey string
	Sequence  int64
	Reason    string
}

// NewChainDigester builds the hashchain.Digester [VerifyChain] and
// [EventDetail] use to recompute a stream's chain. It is a pure constructor
// (internal/engines/wire/digest.NewRegistry followed by hashchain.NewRegistry/
// NewDigester) with no I/O, exposed so a caller builds it once and reuses it
// across many explorer calls rather than this package hiding that cost
// inside every call.
func NewChainDigester() (*hashchain.Digester, error) {
	registry, err := hashchain.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("explorer: build chain digester: %w", err)
	}
	return hashchain.NewDigester(registry), nil
}

// VerifyChain replays streamKey's recorded events and chain links from
// genesis and reports whether they still verify. It performs no write: it
// is read-only and idempotent, safe to call as often as an operator wants.
func VerifyChain(ctx context.Context, q hashchain.Querier, digester *hashchain.Digester, tenant uuid.UUID, streamKey string) (ChainView, error) {
	head, err := digester.Verify(ctx, q, tenant, streamKey)
	if err == nil {
		return ChainView{StreamKey: streamKey, Verified: true, Head: head}, nil
	}

	var empty hashchain.ErrEmptyStream
	if errors.As(err, &empty) {
		return ChainView{StreamKey: streamKey, Empty: true}, nil
	}

	var broken hashchain.ErrChainBroken
	if errors.As(err, &broken) {
		return ChainView{
			StreamKey: streamKey,
			Broken:    &BrokenLink{StreamKey: streamKey, Sequence: broken.Sequence, Reason: broken.Reason},
		}, nil
	}

	return ChainView{}, fmt.Errorf("explorer: verify chain for stream %s: %w", streamKey, err)
}
