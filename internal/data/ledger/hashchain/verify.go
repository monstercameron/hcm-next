package hashchain

import "strconv"

// VerifyLinks recomputes a stream's hash chain from genesis and checks it,
// sequence by sequence, against both the event digests LEDGER-002/DATA-003
// recorded and the chain links this package previously recorded for them.
// It never mutates anything and never trusts recorded_at or any other wall
// clock value to order events - sequence, not time, is the only ordering
// authority (specs/transaction-ledger-reconciliation-and-repair.md 16).
//
// events and links must each be ordered by sequence ascending, exactly as
// [ReadEventDigests] and [ReadLinks] return them; VerifyLinks does not sort
// its input; the pure unit tests it is easiest to reason about deliberately
// hand it out-of-order or duplicated input, and require every failure mode
// below to be caught by comparing successive elements rather than by
// re-sorting first.
//
// On success it returns the reproduced [Head]: the chain hash at the
// stream's highest sequence. On the first sequence where the chain cannot be
// extended, it returns [ErrChainBroken] naming that exact stream and
// sequence and classifying why: a gap or reordering in the event stream
// itself, a gap or reordering in the recorded chain links, a chain link that
// names a different event than the one actually at that sequence
// (substitution or duplication), a prior hash that does not match the
// previous link's chain hash, or a chain hash that does not reproduce from
// the recorded prior hash and event digest (the event digest, the prior
// hash, or the chain hash itself was altered after being recorded).
//
// An empty stream (no events at all) reports [ErrEmptyStream] rather than a
// vacuous success, so a caller cannot mistake "nothing has happened yet" for
// "the chain was checked and is intact."
func (d *Digester) VerifyLinks(streamKey string, events []EventDigest, links []ChainedLink) (Head, error) {
	if len(events) == 0 && len(links) == 0 {
		return Head{}, ErrEmptyStream{StreamKey: streamKey}
	}

	expectedPrev := GenesisHash
	n := len(events)
	if len(links) > n {
		n = len(links)
	}

	var last Head
	for i := 0; i < n; i++ {
		wantSeq := int64(i + 1)

		if i >= len(events) {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq,
				Reason: "a chain link is recorded but no event exists at this sequence",
			}
		}
		if i >= len(links) {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq,
				Reason: "the event exists but no chain link was recorded for it",
			}
		}

		ev, link := events[i], links[i]

		if ev.Sequence != wantSeq {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq, Reason: "event sequence gap or reordering",
				Expected: strconv.FormatInt(wantSeq, 10), Actual: strconv.FormatInt(ev.Sequence, 10),
			}
		}
		if link.Sequence != wantSeq {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq, Reason: "chain link sequence gap or reordering",
				Expected: strconv.FormatInt(wantSeq, 10), Actual: strconv.FormatInt(link.Sequence, 10),
			}
		}
		if link.EventID != ev.EventID {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq,
				Reason:   "chain link references a different event (duplicated or substituted event)",
				Expected: ev.EventID.String(), Actual: link.EventID.String(),
			}
		}
		if link.PrevHash != expectedPrev {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq,
				Reason:   "prior hash does not match the previous link's chain hash",
				Expected: expectedPrev, Actual: link.PrevHash,
			}
		}

		_, wantChainHash, err := d.Link(expectedPrev, ev.Digest)
		if err != nil {
			return Head{}, err
		}
		if link.ChainHash != wantChainHash {
			return Head{}, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq,
				Reason:   "recomputed chain hash does not match the recorded chain hash",
				Expected: wantChainHash, Actual: link.ChainHash,
			}
		}

		expectedPrev = link.ChainHash
		last = Head{StreamKey: streamKey, Sequence: wantSeq, ChainHash: link.ChainHash, Algorithm: link.Algorithm}
	}

	return last, nil
}

// Fold computes the chain links a stream's event digests produce, in order,
// without reading or comparing against any previously recorded link. It is
// what [Appender.Append] does incrementally, one event at a time; Fold
// exists so a caller that already has a whole run of event digests in hand -
// a test fixture, or a from-genesis rebuild - can produce the same links in
// one pass and compare them against what was actually recorded, rather than
// replaying through PostgreSQL one append at a time.
//
// events must be ordered by sequence ascending starting at 1 with no gaps;
// Fold returns [ErrChainBroken] at the first sequence that violates that,
// exactly as [VerifyLinks] would once persisted links existed to check
// against.
func (d *Digester) Fold(streamKey string, events []EventDigest) ([]ChainedLink, error) {
	if len(events) == 0 {
		return nil, ErrEmptyStream{StreamKey: streamKey}
	}

	out := make([]ChainedLink, 0, len(events))
	prev := GenesisHash
	for i, ev := range events {
		wantSeq := int64(i + 1)
		if ev.Sequence != wantSeq {
			return nil, ErrChainBroken{
				StreamKey: streamKey, Sequence: wantSeq, Reason: "event sequence gap or reordering",
				Expected: strconv.FormatInt(wantSeq, 10), Actual: strconv.FormatInt(ev.Sequence, 10),
			}
		}
		algorithm, chainHash, err := d.Link(prev, ev.Digest)
		if err != nil {
			return nil, err
		}
		link := ChainedLink{
			StreamKey: streamKey, Sequence: wantSeq, EventID: ev.EventID,
			PrevHash: prev, ChainHash: chainHash, Algorithm: algorithm,
		}
		out = append(out, link)
		prev = chainHash
	}
	return out, nil
}
