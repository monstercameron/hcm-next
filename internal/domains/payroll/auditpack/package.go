package auditpack

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// SchemaVersion is the version of this package's own manifest and summary
// projections. It is inside the digested bytes, so a package built under one
// version can never be verified as though it were another.
const SchemaVersion = 1

// DigestAlgorithm names the digest algorithm every digest in an auditor
// package uses.
const DigestAlgorithm = "sha256"

// ManifestDigestProfile is folded into the outer manifest's digest and into
// the summary part's own digest, so neither can be mistaken for a digest this
// platform computes under any other profile.
const ManifestDigestProfile = "hcmnext.canonical.PAYROLL_AUDITPACK_MANIFEST.v1"

// Fixed paths. EvidencePrefix namespaces the wrapped
// evidence.Package unchanged: every path that package would produce on its
// own appears here with the prefix prepended, so the two packages' own path
// layouts never collide.
const (
	ManifestPath   = "manifest.json"
	SummaryPath    = "summary.json"
	EvidencePrefix = "evidence/"
)

// KeyDirectory is [checkpoint.KeyDirectory]; an auditor package's epochs are
// signed exactly the way a bare evidence.Package's are, so the same
// directory works for both.
type KeyDirectory = checkpoint.KeyDirectory

// AuditManifest describes a whole auditor package: what run and window it
// covers, the immutable idempotency key it was resolved under, and the two
// digests - one over the wrapped evidence package, one over the summary -
// that its own digest folds together.
type AuditManifest struct {
	SchemaVersion  int
	Tenant         TenantID
	RunID          string
	IdempotencyKey string
	CoversFrom     time.Time
	CoversTo       time.Time
	// EvidenceDigest is the wrapped evidence.Package's own manifest digest.
	// It is recomputed and compared by [Verify], never trusted as given.
	EvidenceDigest string
	// SummaryDigest is the digest of the summary part's bytes, likewise
	// recomputed and compared.
	SummaryDigest string
	// Digest folds every field above into one value.
	Digest          string
	DigestAlgorithm string
}

// Package is one built auditor package: the outer manifest, the summary, and
// the wrapped evidence package's own files under [EvidencePrefix].
type Package struct {
	Manifest AuditManifest

	manifestBytes []byte
	summaryBytes  []byte
	evidenceFiles map[string][]byte
}

// Files returns every path in the package, [ManifestPath] and [SummaryPath]
// included, with a copy of its bytes.
func (p Package) Files() map[string][]byte {
	out := make(map[string][]byte, len(p.evidenceFiles)+2)
	for path, raw := range p.evidenceFiles {
		out[EvidencePrefix+path] = append([]byte(nil), raw...)
	}
	if p.manifestBytes != nil {
		out[ManifestPath] = append([]byte(nil), p.manifestBytes...)
	}
	if p.summaryBytes != nil {
		out[SummaryPath] = append([]byte(nil), p.summaryBytes...)
	}
	return out
}

// Paths returns every path in the package in sorted order.
func (p Package) Paths() []string {
	files := p.Files()
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// Part returns one path's bytes.
func (p Package) Part(path string) ([]byte, bool) {
	switch path {
	case ManifestPath:
		if p.manifestBytes == nil {
			return nil, false
		}
		return append([]byte(nil), p.manifestBytes...), true
	case SummaryPath:
		if p.summaryBytes == nil {
			return nil, false
		}
		return append([]byte(nil), p.summaryBytes...), true
	default:
		if inner, ok := strings.CutPrefix(path, EvidencePrefix); ok {
			raw, ok := p.evidenceFiles[inner]
			if !ok {
				return nil, false
			}
			return append([]byte(nil), raw...), true
		}
		return nil, false
	}
}

// TotalBytes is the size of the whole package.
func (p Package) TotalBytes() int {
	total := len(p.manifestBytes) + len(p.summaryBytes)
	for _, raw := range p.evidenceFiles {
		total += len(raw)
	}
	return total
}

// ---- wire projections ------------------------------------------------------

type manifestFile struct {
	SchemaVersion   int    `json:"schema_version"`
	Tenant          string `json:"tenant"`
	RunID           string `json:"run_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	CoversFromNS    int64  `json:"covers_from_ns"`
	CoversToNS      int64  `json:"covers_to_ns"`
	EvidenceDigest  string `json:"evidence_digest"`
	SummaryDigest   string `json:"summary_digest"`
	Digest          string `json:"digest"`
	DigestAlgorithm string `json:"digest_algorithm"`
}

type summaryLine struct {
	Kind      string `json:"kind"`
	StreamKey string `json:"stream_key"`
	Sequence  int64  `json:"sequence"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
}

type summaryFile struct {
	SchemaVersion  int               `json:"schema_version"`
	Tenant         string            `json:"tenant"`
	RunID          string            `json:"run_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	CoversFromNS   int64             `json:"covers_from_ns"`
	CoversToNS     int64             `json:"covers_to_ns"`
	Totals         map[string]string `json:"totals"`
	Lines          []summaryLine     `json:"lines"`
	Pairs          []pairPayload     `json:"pairs"`
}

func nanos(t time.Time) int64 { return evidence.Truncate(t).UnixNano() }

func fromNanos(ns int64) time.Time { return time.Unix(0, ns).UTC() }

func encodePart(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("auditpack: encode part: %w", err)
	}
	return append(body, '\n'), nil
}

// ---- digest folding ---------------------------------------------------------

func newFolder() (*foldWriter, func() string) {
	h := sha256.New()
	return &foldWriter{h: h}, func() string { return hex.EncodeToString(h.Sum(nil)) }
}

type foldWriter struct {
	h interface{ Write([]byte) (int, error) }
}

func (f *foldWriter) bytes(b []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(b)))
	_, _ = f.h.Write(length[:])
	_, _ = f.h.Write(b)
}

func (f *foldWriter) text(s string) { f.bytes([]byte(s)) }

func (f *foldWriter) number(n int64) {
	var v [8]byte
	binary.BigEndian.PutUint64(v[:], uint64(n))
	f.bytes(v[:])
}

func computeManifestDigest(m AuditManifest) string {
	f, sum := newFolder()
	f.text(ManifestDigestProfile)
	f.number(int64(m.SchemaVersion))
	f.text(formatUUID(m.Tenant))
	f.text(m.RunID)
	f.text(m.IdempotencyKey)
	f.number(nanos(m.CoversFrom))
	f.number(nanos(m.CoversTo))
	f.text(m.EvidenceDigest)
	f.text(m.SummaryDigest)
	return sum()
}

func summaryDigestOf(raw []byte) string {
	f, sum := newFolder()
	f.text(ManifestDigestProfile + "/summary")
	f.bytes(raw)
	return sum()
}

// ---- errors -----------------------------------------------------------------

// ErrPackageMalformed reports package bytes that could not be read as an
// auditor package at all.
type ErrPackageMalformed struct {
	Path   string
	Reason string
}

func (ErrPackageMalformed) Code() string { return "PAYROLL_AUDITPACK_PACKAGE_MALFORMED" }

func (e ErrPackageMalformed) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Code(), e.Path, e.Reason)
}

// ---- build ------------------------------------------------------------------

// Build assembles one auditor package from content, its resolved totals and
// the variance decision over them. It is pure: no clock, no identifier
// source and no database is involved, so the same inputs always produce the
// same bytes.
//
// It refuses to build over an unexplained variance ([Decision.Err]): an
// auditor package exists to attest that a run reconciled, so a package built
// over one that did not would misrepresent exactly the thing it exists to
// prove.
func Build(content evidence.Content, totals RunTotals, decision Decision) (Package, error) {
	if totals.Tenant != content.Tenant {
		return Package{}, ErrTenantLeak{Expected: content.Tenant, Found: totals.Tenant}
	}
	if strings.TrimSpace(totals.RunID) == "" {
		return Package{}, fmt.Errorf("auditpack: a run id is required")
	}
	if err := decision.Err(); err != nil {
		return Package{}, err
	}

	evPkg, err := evidence.Build(content)
	if err != nil {
		return Package{}, fmt.Errorf("auditpack: build wrapped evidence package: %w", err)
	}

	key := IdempotencyKey(content.Tenant, totals.RunID)
	summaryRaw, err := encodeSummary(content.Tenant, totals, decision, key)
	if err != nil {
		return Package{}, err
	}

	manifest := AuditManifest{
		SchemaVersion: SchemaVersion, Tenant: content.Tenant, RunID: totals.RunID,
		IdempotencyKey: key, CoversFrom: content.CoversFrom, CoversTo: content.CoversTo,
		EvidenceDigest: evPkg.Manifest.Digest, SummaryDigest: summaryDigestOf(summaryRaw),
		DigestAlgorithm: DigestAlgorithm,
	}
	manifest.Digest = computeManifestDigest(manifest)

	manifestRaw, err := encodePart(manifestFile{
		SchemaVersion: manifest.SchemaVersion, Tenant: formatUUID(manifest.Tenant),
		RunID: manifest.RunID, IdempotencyKey: manifest.IdempotencyKey,
		CoversFromNS: nanos(manifest.CoversFrom), CoversToNS: nanos(manifest.CoversTo),
		EvidenceDigest: manifest.EvidenceDigest, SummaryDigest: manifest.SummaryDigest,
		Digest: manifest.Digest, DigestAlgorithm: manifest.DigestAlgorithm,
	})
	if err != nil {
		return Package{}, err
	}

	return Package{
		Manifest: manifest, manifestBytes: manifestRaw, summaryBytes: summaryRaw,
		evidenceFiles: evPkg.Files(),
	}, nil
}

func encodeSummary(tenant TenantID, totals RunTotals, decision Decision, key string) ([]byte, error) {
	wire := summaryFile{
		SchemaVersion: SchemaVersion, Tenant: formatUUID(tenant), RunID: totals.RunID,
		IdempotencyKey: key, CoversFromNS: nanos(totals.CoversFrom), CoversToNS: nanos(totals.CoversTo),
		Totals: make(map[string]string, len(totals.Totals)),
	}
	for _, k := range Kinds() {
		amount, ok := totals.Total(k)
		if !ok {
			return nil, ErrMissingTotal{RunID: totals.RunID, Kind: k}
		}
		text, err := amount.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("auditpack: encode total %s: %w", k, err)
		}
		wire.Totals[string(k)] = string(text)
	}
	for _, l := range totals.Lines {
		text, err := l.Amount.MarshalText()
		if err != nil {
			return nil, err
		}
		wire.Lines = append(wire.Lines, summaryLine{
			Kind: string(l.Kind), StreamKey: l.Ref.StreamKey, Sequence: l.Ref.Sequence,
			Amount: string(text), Currency: l.Currency,
		})
	}
	for _, p := range decision.Pairs {
		left, err := p.LeftAmount.MarshalText()
		if err != nil {
			return nil, err
		}
		right, err := p.RightAmount.MarshalText()
		if err != nil {
			return nil, err
		}
		diff, err := p.Difference.MarshalText()
		if err != nil {
			return nil, err
		}
		wire.Pairs = append(wire.Pairs, pairPayload{
			Name: p.Name, LeftAmount: string(left), RightAmount: string(right),
			Difference: string(diff), OK: p.OK,
		})
	}
	return encodePart(wire)
}

// ---- open -------------------------------------------------------------------

// Open reads package bytes back into a [Package] without checking anything
// beyond "the manifest parses". Everything a package can be wrong about
// beyond that is a finding from [Verify], never a parse error.
func Open(files map[string][]byte) (Package, error) {
	manifestRaw, ok := files[ManifestPath]
	if !ok {
		return Package{}, ErrPackageMalformed{Path: ManifestPath, Reason: "the package has no manifest"}
	}
	var mf manifestFile
	if err := json.Unmarshal(manifestRaw, &mf); err != nil {
		return Package{}, ErrPackageMalformed{Path: ManifestPath, Reason: err.Error()}
	}
	tenant, err := parseUUID(mf.Tenant)
	if err != nil {
		return Package{}, ErrPackageMalformed{Path: ManifestPath, Reason: fmt.Sprintf("tenant is not a uuid: %v", err)}
	}
	manifest := AuditManifest{
		SchemaVersion: mf.SchemaVersion, Tenant: tenant, RunID: mf.RunID,
		IdempotencyKey: mf.IdempotencyKey, CoversFrom: fromNanos(mf.CoversFromNS), CoversTo: fromNanos(mf.CoversToNS),
		EvidenceDigest: mf.EvidenceDigest, SummaryDigest: mf.SummaryDigest,
		Digest: mf.Digest, DigestAlgorithm: mf.DigestAlgorithm,
	}

	evidenceFiles := make(map[string][]byte, len(files))
	for path, raw := range files {
		if inner, ok := strings.CutPrefix(path, EvidencePrefix); ok {
			evidenceFiles[inner] = append([]byte(nil), raw...)
		}
	}

	pkg := Package{Manifest: manifest, manifestBytes: append([]byte(nil), manifestRaw...), evidenceFiles: evidenceFiles}
	if summaryRaw, ok := files[SummaryPath]; ok {
		pkg.summaryBytes = append([]byte(nil), summaryRaw...)
	}
	return pkg, nil
}

// ---- verify -----------------------------------------------------------------

// FindingKind classifies one way an auditor package failed to verify.
type FindingKind string

const (
	FindingMissingPart            FindingKind = "MISSING_PART"
	FindingMalformedPart          FindingKind = "MALFORMED_PART"
	FindingManifestDigest         FindingKind = "MANIFEST_DIGEST_MISMATCH"
	FindingSummaryDigest          FindingKind = "SUMMARY_DIGEST_MISMATCH"
	FindingEvidenceDigestMismatch FindingKind = "EVIDENCE_DIGEST_MISMATCH"
	FindingTotalsMismatch         FindingKind = "TOTALS_MISMATCH"
	FindingVarianceUnexplained    FindingKind = "VARIANCE_UNEXPLAINED"
	FindingKeyMismatch            FindingKind = "IDEMPOTENCY_KEY_MISMATCH"
	FindingBindingMismatch        FindingKind = "BINDING_MISMATCH"
)

// Finding is one typed failure.
type Finding struct {
	Kind     FindingKind
	Detail   string
	Expected string
	Actual   string
}

func (f Finding) Error() string { return f.String() }

func (f Finding) String() string {
	out := string(f.Kind)
	if f.Detail != "" {
		out += ": " + f.Detail
	}
	if f.Expected != "" || f.Actual != "" {
		out += fmt.Sprintf(" (expected %s, got %s)", f.Expected, f.Actual)
	}
	return out
}

// Report is what an offline verification concluded.
type Report struct {
	Manifest           AuditManifest
	Evidence           evidence.Report
	RecomputedTotals   RunTotals
	RecomputedDecision Decision
	Findings           []Finding
}

// OK reports whether the package verified with no finding at all, including
// the wrapped evidence package.
func (r Report) OK() bool { return len(r.Findings) == 0 && r.Evidence.OK() }

// Err joins every finding - this package's own and the wrapped evidence
// package's - into one error, or nil when the package verified.
func (r Report) Err() error {
	var errs []error
	for _, f := range r.Findings {
		errs = append(errs, f)
	}
	if err := r.Evidence.Err(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil
	}
	return joinErrors(errs)
}

// Has reports whether the report carries at least one finding of a kind
// (this package's own findings only; use r.Evidence.Has for the wrapped
// package's).
func (r Report) Has(kind FindingKind) bool {
	for _, f := range r.Findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

type verifier struct {
	report Report
}

func (v *verifier) add(f Finding) { v.report.Findings = append(v.report.Findings, f) }

// Verify checks an auditor package offline: it needs the package bytes, the
// checkpoint epochs' public keys, and the run id the package claims to
// cover. It opens no database and makes no network call.
//
// It re-derives everything rather than trusting what it was handed: the
// outer manifest digest, the summary digest, the wrapped evidence package
// (with its event digests recomputed under the ledger's own SHA256 profile),
// and - independently of the summary's own stated numbers - all four
// reconciliation totals and the variance decision, folded straight from the
// wrapped package's own covered events. A summary edited without touching
// the underlying ledger events is caught by [FindingTotalsMismatch] or
// [FindingVarianceUnexplained]; an underlying event altered is caught by the
// wrapped evidence package's own tamper findings.
func Verify(files map[string][]byte, dir KeyDirectory, runID string) Report {
	v := &verifier{}

	pkg, err := Open(files)
	if err != nil {
		path := ManifestPath
		if malformed, ok := errors.AsType[ErrPackageMalformed](err); ok {
			path = malformed.Path
		}
		v.add(Finding{Kind: FindingMalformedPart, Detail: fmt.Sprintf("%s: %v", path, err)})
		return v.report
	}
	v.report.Manifest = pkg.Manifest

	if pkg.Manifest.SchemaVersion != SchemaVersion {
		v.add(Finding{
			Kind: FindingMalformedPart, Detail: "the package was written under a manifest version this verifier does not know",
			Expected: fmt.Sprintf("%d", SchemaVersion), Actual: fmt.Sprintf("%d", pkg.Manifest.SchemaVersion),
		})
		return v.report
	}
	if pkg.summaryBytes == nil {
		v.add(Finding{Kind: FindingMissingPart, Detail: "the package has no summary"})
		return v.report
	}

	if pkg.Manifest.DigestAlgorithm != DigestAlgorithm {
		v.add(Finding{Kind: FindingMalformedPart, Detail: "manifest digest algorithm is not supported", Expected: DigestAlgorithm, Actual: pkg.Manifest.DigestAlgorithm})
	} else if got := computeManifestDigest(pkg.Manifest); got != pkg.Manifest.Digest {
		v.add(Finding{Kind: FindingManifestDigest, Expected: got, Actual: pkg.Manifest.Digest})
	}

	if got := summaryDigestOf(pkg.summaryBytes); got != pkg.Manifest.SummaryDigest {
		v.add(Finding{Kind: FindingSummaryDigest, Expected: got, Actual: pkg.Manifest.SummaryDigest})
	}

	evidenceFiles := make(map[string][]byte, len(pkg.evidenceFiles))
	for path, raw := range pkg.evidenceFiles {
		evidenceFiles[path] = raw
	}
	v.report.Evidence = evidence.Verify(evidenceFiles, dir, evidence.WithEventDigester(datalogger.SHA256Digester{}))
	if v.report.Evidence.Manifest.Digest != "" && v.report.Evidence.Manifest.Digest != pkg.Manifest.EvidenceDigest {
		v.add(Finding{Kind: FindingEvidenceDigestMismatch, Expected: v.report.Evidence.Manifest.Digest, Actual: pkg.Manifest.EvidenceDigest})
	}

	if want := IdempotencyKey(pkg.Manifest.Tenant, runID); want != pkg.Manifest.IdempotencyKey {
		v.add(Finding{Kind: FindingKeyMismatch, Detail: "the package's own idempotency key does not reproduce from its tenant and the requested run id", Expected: want, Actual: pkg.Manifest.IdempotencyKey})
	}

	totals, decision, binding, recomputeErr := recomputeFromEvidence(pkg.evidenceFiles, pkg.Manifest.Tenant, runID)
	if recomputeErr != nil {
		v.add(Finding{Kind: FindingTotalsMismatch, Detail: recomputeErr.Error()})
		return v.report
	}
	v.report.RecomputedTotals = totals
	v.report.RecomputedDecision = decision

	// The ledger-recorded binding is optional evidence: a package exported
	// before a run was ever bound legitimately carries no such event. When one
	// is present, it is the ledger's own attestation of the key and totals,
	// so it is cross-checked against what this verification just re-derived
	// independently.
	if binding != nil {
		if binding.IdempotencyKey != pkg.Manifest.IdempotencyKey {
			v.add(Finding{
				Kind: FindingBindingMismatch, Detail: "the ledger-recorded binding names a different idempotency key than the manifest",
				Expected: pkg.Manifest.IdempotencyKey, Actual: binding.IdempotencyKey,
			})
		}
		for _, k := range Kinds() {
			recomputed, ok := totals.Total(k)
			if !ok {
				continue
			}
			recomputedText, _ := recomputed.MarshalText()
			bound := binding.Totals[string(k)]
			var boundDecimal values.Decimal
			if err := boundDecimal.UnmarshalText([]byte(bound)); err != nil {
				v.add(Finding{Kind: FindingBindingMismatch, Detail: fmt.Sprintf("bound total %s: %v", k, err)})
				continue
			}
			if boundDecimal.Cmp(recomputed) != 0 {
				v.add(Finding{
					Kind: FindingBindingMismatch, Detail: fmt.Sprintf("bound total %s does not match the recomputed total", k),
					Expected: string(recomputedText), Actual: bound,
				})
			}
		}
	}

	var summary summaryFile
	if err := json.Unmarshal(pkg.summaryBytes, &summary); err != nil {
		v.add(Finding{Kind: FindingMalformedPart, Detail: fmt.Sprintf("summary.json: %v", err)})
		return v.report
	}
	for _, k := range Kinds() {
		recomputed, ok := totals.Total(k)
		if !ok {
			continue
		}
		recomputedText, _ := recomputed.MarshalText()
		stated := summary.Totals[string(k)]
		var statedDecimal values.Decimal
		if err := statedDecimal.UnmarshalText([]byte(stated)); err != nil {
			v.add(Finding{Kind: FindingMalformedPart, Detail: fmt.Sprintf("summary total %s: %v", k, err)})
			continue
		}
		if statedDecimal.Cmp(recomputed) != 0 {
			v.add(Finding{Kind: FindingTotalsMismatch, Detail: string(k), Expected: string(recomputedText), Actual: stated})
		}
	}

	if !decision.OK() {
		if err := decision.Err(); err != nil {
			v.add(Finding{Kind: FindingVarianceUnexplained, Detail: err.Error()})
		}
	}

	return v.report
}

// recomputeFromEvidence re-derives [RunTotals] and the [Decision] over them
// straight from a wrapped evidence package's own bytes: it decodes every
// covered-event part, keeps only the events under [LineSchemaRef] naming
// runID, and folds them exactly [ResolveFromContent] would over a live
// export. It touches no database and trusts nothing the summary states. It
// also returns the ledger-recorded reconciliation binding for runID, if the
// covered window happens to include one.
func recomputeFromEvidence(evidenceFiles map[string][]byte, tenant TenantID, runID string) (RunTotals, Decision, *bindingPayload, error) {
	evPkg, err := evidence.Open(evidenceFiles)
	if err != nil {
		return RunTotals{}, Decision{}, nil, fmt.Errorf("auditpack: open wrapped evidence package: %w", err)
	}

	content := evidence.Content{Tenant: tenant, CoversFrom: evPkg.Manifest.CoversFrom, CoversTo: evPkg.Manifest.CoversTo}
	var binding *bindingPayload
	for _, part := range evPkg.Manifest.Parts {
		if part.Kind != evidence.PartStreamEvents {
			continue
		}
		raw, ok := evPkg.Part(part.Path)
		if !ok {
			continue
		}
		var wire wireEventsFile
		if err := json.Unmarshal(raw, &wire); err != nil {
			return RunTotals{}, Decision{}, nil, fmt.Errorf("auditpack: decode %s: %w", part.Path, err)
		}
		stream := evidence.Stream{StreamKey: wire.StreamKey}
		for _, e := range wire.Events {
			eventTenant, err := parseUUID(e.Tenant)
			if err != nil {
				return RunTotals{}, Decision{}, nil, fmt.Errorf("auditpack: decode %s: event tenant: %w", part.Path, err)
			}
			stream.Events = append(stream.Events, evidence.Event{
				Tenant: eventTenant, StreamKey: e.StreamKey, Sequence: e.Sequence,
				SchemaRef: e.SchemaRef, Payload: e.Payload,
			})
			if e.SchemaRef == BindingSchemaRef {
				decoded, err := decodeBinding(e.Payload)
				if err != nil {
					return RunTotals{}, Decision{}, nil, fmt.Errorf("auditpack: decode binding at %s@%d: %w", e.StreamKey, e.Sequence, err)
				}
				if decoded.RunID == runID {
					binding = &decoded
				}
			}
		}
		content.Streams = append(content.Streams, stream)
	}

	totals, err := ResolveFromContent(content, tenant, runID)
	if err != nil {
		return RunTotals{}, Decision{}, nil, err
	}
	decision, err := Reconcile(totals)
	if err != nil {
		return RunTotals{}, Decision{}, nil, err
	}
	return totals, decision, binding, nil
}

// wireEventsFile and wireEvent mirror the public JSON contract
// internal/data/ledger/evidence's package documents its "streams/NNNN/
// events.json" part under (evidence.doc.go, "What a package contains"): a
// tenant, a stream key and typed envelopes carrying a schema reference and a
// payload. They are declared locally, rather than importing evidence's own
// unexported wire types, because this package depends only on evidence's
// published Go API and its documented, stable wire shape - never on its
// private implementation.
type wireEventsFile struct {
	StreamKey string      `json:"stream_key"`
	Events    []wireEvent `json:"events"`
}

type wireEvent struct {
	Tenant    string `json:"tenant"`
	StreamKey string `json:"stream_key"`
	Sequence  int64  `json:"sequence"`
	SchemaRef string `json:"schema_ref"`
	Payload   []byte `json:"payload"`
}
