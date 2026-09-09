package evidence

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
)

// FindingKind classifies one way an evidence package failed to verify. A
// verifier branches on the kind; the free-text Detail is for a human reading
// the report and must never be parsed.
type FindingKind string

// The finding kinds. Each names one specific failure, because "the package
// is invalid" tells an auditor nothing about what to do next.
const (
	// FindingMissingPart reports a path the manifest lists that the package
	// does not contain.
	FindingMissingPart FindingKind = "MISSING_PART"
	// FindingUnlistedPart reports bytes in the package at a path the manifest
	// does not list. An unlisted part is covered by no digest at all.
	FindingUnlistedPart FindingKind = "UNLISTED_PART"
	// FindingTamperedPart reports a part whose bytes do not reproduce the
	// length and digest the manifest records for it. One altered byte
	// anywhere produces this, naming the exact path.
	FindingTamperedPart FindingKind = "TAMPERED_PART"
	// FindingManifestDigest reports a manifest whose own recorded digest is
	// not the digest its part rows produce.
	FindingManifestDigest FindingKind = "MANIFEST_DIGEST_MISMATCH"
	// FindingMalformedPart reports a part that could not be read at all, or a
	// package written under a layout this verifier does not know.
	FindingMalformedPart FindingKind = "MALFORMED_PART"
	// FindingTamperedEvent reports a covered event that disagrees with the
	// chain's own record of it, or whose payload does not reproduce its
	// recorded digest under a supplied [WithEventDigester].
	FindingTamperedEvent FindingKind = "TAMPERED_EVENT"
	// FindingBrokenChain reports the exact stream and sequence at which a
	// hash chain stopped reproducing.
	FindingBrokenChain FindingKind = "BROKEN_CHAIN"
	// FindingUnattestedHead reports a covered stream no signed epoch attests
	// to, or a chain hash that disagrees with the head an epoch signed.
	FindingUnattestedHead FindingKind = "UNATTESTED_HEAD"
	// FindingMissingEpochCoverage reports part of the covered window that no
	// exported epoch's window covers.
	FindingMissingEpochCoverage FindingKind = "MISSING_EPOCH_COVERAGE"
	// FindingSignatureMismatch reports an epoch whose signature does not
	// verify, or was made by a key the directory did not permit to make it.
	FindingSignatureMismatch FindingKind = "SIGNATURE_MISMATCH"
	// FindingTenantLeak reports any part naming a tenant other than the
	// manifest's. It is always a refusal, never a warning.
	FindingTenantLeak FindingKind = "TENANT_LEAK"
)

// Finding is one typed failure. Part names where it was found, and the
// stream/sequence/epoch fields are set when the failure is located more
// precisely than a whole part.
type Finding struct {
	Kind        FindingKind
	Part        string
	Stream      string
	Sequence    int64
	EpochNumber int64
	Detail      string
	Expected    string
	Actual      string
}

// Error lets one finding be returned as an error on its own.
func (f Finding) Error() string { return f.String() }

// String renders the finding for a human reading a report.
func (f Finding) String() string {
	out := string(f.Kind)
	if f.Part != "" {
		out += " at " + f.Part
	}
	if f.Stream != "" {
		out += fmt.Sprintf(" (stream %s", f.Stream)
		if f.Sequence > 0 {
			out += fmt.Sprintf("@%d", f.Sequence)
		}
		out += ")"
	}
	if f.EpochNumber > 0 {
		out += fmt.Sprintf(" (epoch %d)", f.EpochNumber)
	}
	if f.Detail != "" {
		out += ": " + f.Detail
	}
	if f.Expected != "" || f.Actual != "" {
		out += fmt.Sprintf(" (expected %s, got %s)", f.Expected, f.Actual)
	}
	return out
}

// Report is what an offline verification concluded: everything it checked,
// and every way the package failed. A package with no findings verified
// completely; there is no partial pass.
type Report struct {
	Manifest           Manifest
	Findings           []Finding
	StreamsVerified    int
	ChainLinksVerified int
	EventsVerified     int
	EpochsVerified     int
}

// OK reports whether the package verified with no finding at all.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Err joins every finding into one error, or returns nil when the package
// verified. It exists so a caller that only wants pass/fail can write
// `if err := Verify(...).Err(); err != nil`.
func (r Report) Err() error {
	if len(r.Findings) == 0 {
		return nil
	}
	errs := make([]error, 0, len(r.Findings))
	for _, f := range r.Findings {
		errs = append(errs, f)
	}
	return errors.Join(errs...)
}

// Of returns every finding of one kind.
func (r Report) Of(kind FindingKind) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// Has reports whether the report carries at least one finding of a kind.
func (r Report) Has(kind FindingKind) bool { return len(r.Of(kind)) > 0 }

// VerifyOption configures a verification.
type VerifyOption func(*verifier)

// WithEventDigester makes [Verify] recompute every covered event's payload
// digest with d and report [FindingTamperedEvent] when it does not
// reproduce.
//
// It is opt-in because which canonicalization profile minted an event's
// digest is a property of the cell that recorded it, and both profiles this
// repository ships record the algorithm as "sha256"
// (internal/data/ledger.SHA256Digester, internal/ledger.KernelDigester).
// Guessing would make a verifier accuse a sound package of tampering, which
// is worse than checking one binding fewer.
func WithEventDigester(d Digester) VerifyOption {
	return func(v *verifier) { v.eventDigester = d }
}

type verifier struct {
	eventDigester Digester
	report        Report
}

func (v *verifier) add(f Finding) { v.report.Findings = append(v.report.Findings, f) }

// Verify checks a package offline, holding only its bytes and the public
// keys its epochs were signed with. It opens no database, makes no network
// call and needs no credential: an auditor verifies a package on a laptop
// that has never been near the production cell.
//
// It re-derives everything rather than trusting what it was handed - every
// part digest, the manifest digest, every hash chain from genesis, every
// epoch signature and the epochs' coverage of the window - and reports one
// typed [Finding] per failure rather than stopping at the first, so a single
// pass tells an auditor everything that is wrong.
//
// Verify never returns an error for a package that parses. A package that
// does not parse at all is reported as a single [FindingMalformedPart].
func Verify(files map[string][]byte, dir KeyDirectory, opts ...VerifyOption) Report {
	v := &verifier{}
	for _, opt := range opts {
		opt(v)
	}

	pkg, err := Open(files)
	if err != nil {
		path := ManifestPath
		if malformed, ok := errors.AsType[ErrPackageMalformed](err); ok {
			path = malformed.Path
		}
		v.add(Finding{Kind: FindingMalformedPart, Part: path, Detail: err.Error()})
		return v.report
	}
	v.report.Manifest = pkg.Manifest

	if pkg.Manifest.SchemaVersion != SchemaVersion || pkg.Manifest.LayoutVersion != LayoutVersion {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: ManifestPath,
			Detail:   "the package was written under a manifest or layout version this verifier does not know",
			Expected: fmt.Sprintf("schema %d layout %d", SchemaVersion, LayoutVersion),
			Actual:   fmt.Sprintf("schema %d layout %d", pkg.Manifest.SchemaVersion, pkg.Manifest.LayoutVersion),
		})
		return v.report
	}

	v.checkParts(pkg, files)
	v.checkManifestDigest(pkg)

	header, ok := v.readHeader(pkg)
	if !ok {
		return v.report
	}
	streams := v.checkStreams(pkg, header)
	epochs := v.checkEpochs(pkg, header, dir)
	v.checkCoverage(pkg.Manifest, epochs)
	v.checkAttestation(streams, epochs)
	return v.report
}

// checkParts is the first and coarsest gate: every listed path is present,
// reproduces its recorded length and digest, and nothing else is in the
// package at all.
func (v *verifier) checkParts(pkg Package, files map[string][]byte) {
	listed := make(map[string]bool, len(pkg.Manifest.Parts))
	for _, part := range pkg.Manifest.Parts {
		listed[part.Path] = true
		raw, ok := pkg.Part(part.Path)
		if !ok {
			v.add(Finding{Kind: FindingMissingPart, Part: part.Path, Detail: "the manifest lists this part but the package does not contain it"})
			continue
		}
		if len(raw) != part.Length {
			v.add(Finding{
				Kind: FindingTamperedPart, Part: part.Path, Detail: "part length does not match the manifest",
				Expected: fmt.Sprintf("%d bytes", part.Length), Actual: fmt.Sprintf("%d bytes", len(raw)),
			})
			continue
		}
		if part.Algorithm != DigestAlgorithm {
			v.add(Finding{
				Kind: FindingMalformedPart, Part: part.Path, Detail: "part digest algorithm is not supported",
				Expected: DigestAlgorithm, Actual: part.Algorithm,
			})
			continue
		}
		if got := partDigest(part.Path, raw); got != part.Digest {
			v.add(Finding{
				Kind: FindingTamperedPart, Part: part.Path, Detail: "part bytes do not reproduce the digest the manifest records",
				Expected: part.Digest, Actual: got,
			})
		}
	}
	for path := range files {
		if path == ManifestPath || listed[path] {
			continue
		}
		v.add(Finding{Kind: FindingUnlistedPart, Part: path, Detail: "the package carries bytes the manifest does not cover"})
	}
}

func (v *verifier) checkManifestDigest(pkg Package) {
	if pkg.Manifest.DigestAlgorithm != DigestAlgorithm {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: ManifestPath, Detail: "manifest digest algorithm is not supported",
			Expected: DigestAlgorithm, Actual: pkg.Manifest.DigestAlgorithm,
		})
		return
	}
	got, err := ComputeDigest(pkg.Manifest)
	if err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Part: ManifestPath, Detail: err.Error()})
		return
	}
	if got != pkg.Manifest.Digest {
		v.add(Finding{
			Kind: FindingManifestDigest, Part: ManifestPath,
			Detail:   "the manifest's recorded digest is not the digest its own parts produce",
			Expected: got, Actual: pkg.Manifest.Digest,
		})
	}
}

func (v *verifier) readHeader(pkg Package) (headerFile, bool) {
	raw, ok := pkg.Part(HeaderPath)
	if !ok {
		v.add(Finding{Kind: FindingMissingPart, Part: HeaderPath, Detail: "the package has no header"})
		return headerFile{}, false
	}
	var header headerFile
	if err := decodePart(HeaderPath, raw, &header); err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Part: HeaderPath, Detail: err.Error()})
		return headerFile{}, false
	}
	if header.LayoutVersion != LayoutVersion {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: HeaderPath, Detail: "header layout version is not the manifest's",
			Expected: fmt.Sprintf("%d", LayoutVersion), Actual: fmt.Sprintf("%d", header.LayoutVersion),
		})
		return headerFile{}, false
	}
	tenant, err := parseUUID(HeaderPath, "tenant", header.Tenant)
	if err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Part: HeaderPath, Detail: err.Error()})
		return headerFile{}, false
	}
	if tenant != pkg.Manifest.Tenant {
		v.add(Finding{
			Kind: FindingTenantLeak, Part: HeaderPath, Detail: "the header names a different tenant than the manifest",
			Expected: pkg.Manifest.Tenant.String(), Actual: tenant.String(),
		})
		return headerFile{}, false
	}
	if fromNanos(header.CoversFromNS) != pkg.Manifest.CoversFrom || fromNanos(header.CoversToNS) != pkg.Manifest.CoversTo {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: HeaderPath,
			Detail: "the header covers a different window than the manifest",
			Expected: fmt.Sprintf("[%s, %s)", pkg.Manifest.CoversFrom.Format(time.RFC3339Nano),
				pkg.Manifest.CoversTo.Format(time.RFC3339Nano)),
			Actual: fmt.Sprintf("[%s, %s)", fromNanos(header.CoversFromNS).Format(time.RFC3339Nano),
				fromNanos(header.CoversToNS).Format(time.RFC3339Nano)),
		})
	}
	if header.SchemaReleaseVersion != pkg.Manifest.Schema.Version || header.SchemaReleaseDigest != pkg.Manifest.Schema.Digest {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: HeaderPath,
			Detail: "the header names a different schema release than the manifest",
		})
	}
	return header, true
}

// verifiedStream is what checkStreams proved about one stream, for
// checkAttestation to compare against the epochs.
type verifiedStream struct {
	streamKey string
	head      int64
	// chainHashBySequence is the reproduced chain hash at every sequence, so
	// an epoch's signed head can be checked against the sequence it names
	// rather than only against the latest one.
	chainHashBySequence map[int64]string
	sound               bool
}

func (v *verifier) checkStreams(pkg Package, header headerFile) []verifiedStream {
	registry, err := hashchain.NewRegistry()
	if err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Detail: fmt.Sprintf("cannot build a chain digester: %v", err)})
		return nil
	}
	digester := hashchain.NewDigester(registry)

	out := make([]verifiedStream, 0, len(header.Streams))
	for _, hs := range header.Streams {
		out = append(out, v.checkStream(pkg, digester, hs))
	}
	return out
}

func (v *verifier) checkStream(pkg Package, digester *hashchain.Digester, hs headerStream) verifiedStream {
	result := verifiedStream{streamKey: hs.StreamKey, head: hs.HeadSequence, chainHashBySequence: map[int64]string{}}

	chainRaw, ok := pkg.Part(hs.ChainPath)
	if !ok {
		v.add(Finding{Kind: FindingMissingPart, Part: hs.ChainPath, Stream: hs.StreamKey, Detail: "the stream's chain part is absent"})
		return result
	}
	var chain chainFile
	if err := decodePart(hs.ChainPath, chainRaw, &chain); err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Part: hs.ChainPath, Stream: hs.StreamKey, Detail: err.Error()})
		return result
	}
	links, digests, chainTenant, err := restoreChain(hs.ChainPath, chain)
	if err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Part: hs.ChainPath, Stream: hs.StreamKey, Detail: err.Error()})
		return result
	}
	if chainTenant != pkg.Manifest.Tenant {
		v.add(Finding{
			Kind: FindingTenantLeak, Part: hs.ChainPath, Stream: hs.StreamKey,
			Detail:   "the chain part names a different tenant than the manifest",
			Expected: pkg.Manifest.Tenant.String(), Actual: chainTenant.String(),
		})
		return result
	}
	if chain.StreamKey != hs.StreamKey {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: hs.ChainPath, Stream: hs.StreamKey,
			Detail:   "the chain part names a different stream than the header",
			Expected: hs.StreamKey, Actual: chain.StreamKey,
		})
		return result
	}

	head, err := digester.VerifyLinks(hs.StreamKey, digests, links)
	if err != nil {
		if broken, ok := errors.AsType[hashchain.ErrChainBroken](err); ok {
			v.add(Finding{
				Kind: FindingBrokenChain, Part: hs.ChainPath, Stream: broken.StreamKey, Sequence: broken.Sequence,
				Detail: broken.Reason, Expected: broken.Expected, Actual: broken.Actual,
			})
		} else {
			v.add(Finding{Kind: FindingBrokenChain, Part: hs.ChainPath, Stream: hs.StreamKey, Detail: err.Error()})
		}
		return result
	}
	if head.Sequence != hs.HeadSequence || head.ChainHash != hs.ChainHash {
		v.add(Finding{
			Kind: FindingBrokenChain, Part: hs.ChainPath, Stream: hs.StreamKey, Sequence: hs.HeadSequence,
			Detail:   "the reproduced head is not the head the header records",
			Expected: fmt.Sprintf("%d/%s", hs.HeadSequence, hs.ChainHash),
			Actual:   fmt.Sprintf("%d/%s", head.Sequence, head.ChainHash),
		})
		return result
	}
	for _, link := range links {
		result.chainHashBySequence[link.Sequence] = link.ChainHash
	}
	result.sound = true
	v.report.StreamsVerified++
	v.report.ChainLinksVerified += len(links)

	v.checkEvents(pkg, hs, digests)
	return result
}

func (v *verifier) checkEvents(pkg Package, hs headerStream, digests []EventDigest) {
	raw, ok := pkg.Part(hs.EventsPath)
	if !ok {
		v.add(Finding{Kind: FindingMissingPart, Part: hs.EventsPath, Stream: hs.StreamKey, Detail: "the stream's covered-event part is absent"})
		return
	}
	var file eventsFile
	if err := decodePart(hs.EventsPath, raw, &file); err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Part: hs.EventsPath, Stream: hs.StreamKey, Detail: err.Error()})
		return
	}
	if len(file.Events) != hs.CoveredEvents {
		v.add(Finding{
			Kind: FindingTamperedPart, Part: hs.EventsPath, Stream: hs.StreamKey,
			Detail:   "the part holds a different number of covered events than the header records",
			Expected: fmt.Sprintf("%d", hs.CoveredEvents), Actual: fmt.Sprintf("%d", len(file.Events)),
		})
	}

	bySequence := make(map[int64]EventDigest, len(digests))
	for _, d := range digests {
		bySequence[d.Sequence] = d
	}

	for _, ef := range file.Events {
		event, err := restoreEvent(hs.EventsPath, ef)
		if err != nil {
			v.add(Finding{Kind: FindingMalformedPart, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: ef.Sequence, Detail: err.Error()})
			continue
		}
		if event.Tenant != pkg.Manifest.Tenant {
			v.add(Finding{
				Kind: FindingTenantLeak, Part: hs.EventsPath, Stream: event.StreamKey, Sequence: event.Sequence,
				Detail:   "a covered event belongs to another tenant",
				Expected: pkg.Manifest.Tenant.String(), Actual: event.Tenant.String(),
			})
			continue
		}
		if event.StreamKey != hs.StreamKey {
			v.add(Finding{
				Kind: FindingTamperedEvent, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
				Detail: "a covered event was filed under another stream", Expected: hs.StreamKey, Actual: event.StreamKey,
			})
			continue
		}
		recorded := Truncate(event.RecordedAt)
		if recorded.Before(pkg.Manifest.CoversFrom) || !recorded.Before(pkg.Manifest.CoversTo) {
			v.add(Finding{
				Kind: FindingTamperedEvent, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
				Detail: "a covered event was recorded outside the window the package claims",
				Actual: recorded.Format(time.RFC3339Nano),
			})
		}
		chained, ok := bySequence[event.Sequence]
		if !ok {
			v.add(Finding{
				Kind: FindingTamperedEvent, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
				Detail: "the chain holds no digest at this sequence, so the event is not committed to by anything",
			})
			continue
		}
		if chained.Digest != event.Digest {
			v.add(Finding{
				Kind: FindingTamperedEvent, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
				Detail:   "the event's digest is not the digest the chain folds at this sequence",
				Expected: chained.Digest, Actual: event.Digest,
			})
			continue
		}
		if chained.EventID != event.EventID {
			v.add(Finding{
				Kind: FindingTamperedEvent, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
				Detail:   "the chain links a different event at this sequence",
				Expected: chained.EventID.String(), Actual: event.EventID.String(),
			})
			continue
		}
		if v.eventDigester != nil {
			v.checkEventDigest(hs, event)
		}
		v.report.EventsVerified++
	}
}

// checkEventDigest recomputes one event's payload digest under the caller's
// digester. The input is the inline payload when there is one and the
// artifact reference standing in for it otherwise, which is
// internal/data/ledger's own rule for what a digest covers.
func (v *verifier) checkEventDigest(hs headerStream, event Event) {
	input := event.Payload
	if input == nil {
		input = []byte(event.ArtifactRef)
	}
	algorithm, digest, length, err := v.eventDigester.Digest(input, event.SchemaRef)
	if err != nil {
		v.add(Finding{
			Kind: FindingMalformedPart, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
			Detail: fmt.Sprintf("cannot recompute the event digest: %v", err),
		})
		return
	}
	if algorithm != event.DigestAlgorithm || digest != event.Digest || length != event.CanonicalLength {
		v.add(Finding{
			Kind: FindingTamperedEvent, Part: hs.EventsPath, Stream: hs.StreamKey, Sequence: event.Sequence,
			Detail:   "the event's payload does not reproduce its recorded digest",
			Expected: fmt.Sprintf("%s/%s/%d", event.DigestAlgorithm, event.Digest, event.CanonicalLength),
			Actual:   fmt.Sprintf("%s/%s/%d", algorithm, digest, length),
		})
	}
}

func (v *verifier) checkEpochs(pkg Package, header headerFile, dir KeyDirectory) []Epoch {
	var out []Epoch
	for _, he := range header.Epochs {
		raw, ok := pkg.Part(he.Path)
		if !ok {
			v.add(Finding{Kind: FindingMissingPart, Part: he.Path, EpochNumber: he.EpochNumber, Detail: "a signed checkpoint epoch is absent"})
			continue
		}
		epoch, err := decodeEpoch(he.Path, raw)
		if err != nil {
			v.add(Finding{Kind: FindingMalformedPart, Part: he.Path, EpochNumber: he.EpochNumber, Detail: err.Error()})
			continue
		}
		if epoch.Tenant != pkg.Manifest.Tenant {
			v.add(Finding{
				Kind: FindingTenantLeak, Part: he.Path, EpochNumber: epoch.EpochNumber,
				Detail:   "a checkpoint epoch belongs to another tenant",
				Expected: pkg.Manifest.Tenant.String(), Actual: epoch.Tenant.String(),
			})
			continue
		}
		if epoch.EpochNumber != he.EpochNumber {
			v.add(Finding{
				Kind: FindingTamperedPart, Part: he.Path, EpochNumber: he.EpochNumber,
				Detail:   "the epoch part is not the epoch the header lists at this path",
				Expected: fmt.Sprintf("%d", he.EpochNumber), Actual: fmt.Sprintf("%d", epoch.EpochNumber),
			})
			continue
		}
		if err := checkpoint.Verify(epoch, dir); err != nil {
			v.add(epochFinding(he.Path, epoch.EpochNumber, err))
			continue
		}
		out = append(out, epoch)
		v.report.EpochsVerified++
	}
	return out
}

// epochFinding classifies why an epoch did not verify. A bad signature or a
// key that was not permitted to sign is a signature failure; anything else -
// a root digest that does not re-fold, a manifest missing a field it must
// bind - is the epoch part having been altered.
func epochFinding(path string, epochNumber int64, err error) Finding {
	_, badSignature := errors.AsType[checkpoint.ErrSignatureInvalid](err)
	_, badKey := errors.AsType[checkpoint.ErrKeyNotUsable](err)
	if badSignature || badKey {
		return Finding{Kind: FindingSignatureMismatch, Part: path, EpochNumber: epochNumber, Detail: err.Error()}
	}
	return Finding{Kind: FindingTamperedPart, Part: path, EpochNumber: epochNumber, Detail: err.Error()}
}

func (v *verifier) checkCoverage(m Manifest, epochs []Epoch) {
	gapFrom, gapUntil, ok := epochCoverageGap(m.CoversFrom, m.CoversTo, epochs)
	if ok {
		return
	}
	v.add(Finding{
		Kind: FindingMissingEpochCoverage,
		Detail: fmt.Sprintf("no verified checkpoint epoch covers [%s, %s)",
			gapFrom.Format(time.RFC3339Nano), gapUntil.Format(time.RFC3339Nano)),
	})
}

// checkAttestation ties the exported chains to the signatures. A package
// whose events reproduce a chain nobody ever signed proves only that its own
// arithmetic is consistent, so every covered stream must appear in a
// verified epoch, and every head that epoch signed must be the head the
// exported chain reproduces at that sequence.
func (v *verifier) checkAttestation(streams []verifiedStream, epochs []Epoch) {
	attested := make(map[string]bool, len(streams))
	for _, epoch := range epochs {
		for _, head := range epoch.Streams {
			attested[head.StreamKey] = true
			for _, s := range streams {
				if s.streamKey != head.StreamKey || !s.sound {
					continue
				}
				chainHash, ok := s.chainHashBySequence[head.Sequence]
				if !ok {
					// The epoch attests to a sequence beyond what this window
					// exports. That is not a disagreement: the epoch may have
					// been taken after the window closed.
					continue
				}
				if chainHash != head.ChainHash {
					v.add(Finding{
						Kind: FindingUnattestedHead, Stream: head.StreamKey, Sequence: head.Sequence,
						EpochNumber: epoch.EpochNumber,
						Detail:      "the exported chain does not reproduce the head this epoch signed",
						Expected:    head.ChainHash, Actual: chainHash,
					})
				}
			}
		}
	}

	names := make([]string, 0, len(streams))
	for _, s := range streams {
		if !attested[s.streamKey] {
			names = append(names, s.streamKey)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		v.add(Finding{
			Kind: FindingUnattestedHead, Stream: name,
			Detail: "no verified checkpoint epoch in this package attests to this stream",
		})
	}
}

// VerifyPackage is [Verify] over an assembled [Package] rather than a bytes
// map. It re-serializes through [Package.Files] first, so it checks exactly
// what a recipient would receive and never the in-memory value that produced
// it.
func VerifyPackage(pkg Package, dir KeyDirectory, opts ...VerifyOption) Report {
	return Verify(pkg.Files(), dir, opts...)
}
