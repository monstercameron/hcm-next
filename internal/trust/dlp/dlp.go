// Package dlp implements pure outbound data-loss prevention inspection,
// destination policy evaluation, and append-only egress receipts.
//
// An Inspector retains only declared detectors. Payload bytes exist only for
// the duration of Inspect; findings contain classes, detector identifiers,
// locations, and severities, never matched text or the payload itself. Policy
// keeps a reference to outbound.Policy so destination trust remains owned by
// the outbound package rather than being copied into a second allowlist.
package dlp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

// DataClass is the closed vocabulary understood by this package. A caller
// cannot invent a class in a detector or a policy at runtime.
type DataClass string

// Class and DataClassification are compatibility aliases for callers that
// use either vocabulary for the same closed classification value.
type Class = DataClass
type DataClassification = DataClass

const (
	ClassPublic          DataClass = "PUBLIC"
	ClassInternal        DataClass = "INTERNAL"
	ClassPII             DataClass = "PII"
	ClassCompensation    DataClass = "COMPENSATION"
	ClassBank            DataClass = "BANK"
	ClassMedical         DataClass = "MEDICAL"
	ClassImmigration     DataClass = "IMMIGRATION"
	ClassCase            DataClass = "CASE"
	ClassSpecialCategory DataClass = "SPECIAL_CATEGORY"

	// Short aliases make declarations read naturally while retaining one
	// canonical wire vocabulary. The aliases below preserve useful DLP
	// terminology while mapping it to the governed MODEL-023 labels.
	Public                      = ClassPublic
	Internal                    = ClassInternal
	PII                         = ClassPII
	Compensation                = ClassCompensation
	Bank                        = ClassBank
	Medical                     = ClassMedical
	Immigration                 = ClassImmigration
	Case                        = ClassCase
	SpecialCategory             = ClassSpecialCategory
	ClassConfidential DataClass = ClassInternal
	ClassPHI          DataClass = ClassMedical
	ClassFinancial    DataClass = ClassBank
	ClassSecret       DataClass = ClassSpecialCategory
	ClassRestricted   DataClass = ClassSpecialCategory
)

func (c DataClass) valid() bool {
	switch c {
	case ClassPublic, ClassInternal, ClassPII, ClassCompensation, ClassBank,
		ClassMedical, ClassImmigration, ClassCase, ClassSpecialCategory:
		return true
	default:
		return false
	}
}

// Valid reports whether c belongs to the closed classification vocabulary.
func (c DataClass) Valid() bool { return c.valid() }

// Severity is the closed vocabulary for detector findings.
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"

	Info     = SeverityInfo
	Low      = SeverityLow
	Medium   = SeverityMedium
	High     = SeverityHigh
	Critical = SeverityCritical
)

func (s Severity) valid() bool {
	switch s {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

// Valid reports whether s belongs to the closed severity vocabulary.
func (s Severity) Valid() bool { return s.valid() }

func severityRank(s Severity) int {
	switch s {
	case SeverityInfo:
		return 0
	case SeverityLow:
		return 1
	case SeverityMedium:
		return 2
	case SeverityHigh:
		return 3
	case SeverityCritical:
		return 4
	default:
		return -1
	}
}

// Detector declares one class detector. Pattern is interpreted as a Go
// regular expression over UTF-8 bytes. Matcher can be used for a detector
// whose matching logic is not expressible as a regexp; it returns locations
// only and must not return matched content.
type Detector struct {
	ID       string
	Class    DataClass
	Severity Severity
	Pattern  string
	Matcher  func([]byte) []Location
}

// NewDetector constructs a regexp-backed detector and validates the closed
// class and severity vocabularies.
func NewDetector(id string, class DataClass, severity Severity, pattern string) (Detector, error) {
	d := Detector{ID: id, Class: class, Severity: severity, Pattern: pattern}
	if err := d.validate(); err != nil {
		return Detector{}, err
	}
	return d, nil
}

func (d Detector) validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.ID) != d.ID {
		return fmt.Errorf("%w: detector id is required", ErrInvalidDetector)
	}
	if !d.Class.valid() {
		return fmt.Errorf("%w: unknown data class %q", ErrInvalidDetector, d.Class)
	}
	if !d.Severity.valid() {
		return fmt.Errorf("%w: unknown severity %q", ErrInvalidDetector, d.Severity)
	}
	if d.Pattern == "" && d.Matcher == nil {
		return fmt.Errorf("%w: detector %q has no matcher", ErrInvalidDetector, d.ID)
	}
	if d.Pattern != "" {
		if _, err := regexp.Compile(d.Pattern); err != nil { // regexhoist:dynamic
			return fmt.Errorf("%w: detector %q pattern: %v", ErrInvalidDetector, d.ID, err)
		}
	}
	return nil
}

// Location identifies a byte range in the inspected payload. Path is an
// optional declared field path; neither field contains payload content.
type Location struct {
	Path  string
	Start int
	End   int
}

func (l Location) validate(payloadLength int) error {
	if l.Start < 0 || l.End <= l.Start || l.End > payloadLength {
		return fmt.Errorf("%w: invalid location [%d,%d)", ErrInvalidFinding, l.Start, l.End)
	}
	if strings.TrimSpace(l.Path) != l.Path {
		return fmt.Errorf("%w: location path is padded", ErrInvalidFinding)
	}
	return nil
}

// Finding is a redaction-safe inspection result. It deliberately has no
// field for a matched value, snippet, or payload copy.
type Finding struct {
	DetectorID string
	Class      DataClass
	Location   Location
	Severity   Severity
}

func (f Finding) validate(payloadLength int) error {
	if strings.TrimSpace(f.DetectorID) == "" || !f.Class.valid() || !f.Severity.valid() {
		return ErrInvalidFinding
	}
	return f.Location.validate(payloadLength)
}

// Inspection is the complete redaction-safe result of scanning one payload.
// The findings slice is copied at construction and by Findings so callers
// cannot mutate the canonical evidence accidentally.
type Inspection struct {
	Findings []Finding
}

// FindingsCopy returns the findings without exposing Inspection's backing
// slice.
func (i Inspection) FindingsCopy() []Finding {
	return append([]Finding(nil), i.Findings...)
}

// Digest returns a canonical SHA-256 digest of the sorted findings.
func (i Inspection) Digest() string { return DigestFindings(i.Findings) }

// Inspector scans payloads with its immutable detector set.
type Inspector struct {
	detectors []compiledDetector
}

type compiledDetector struct {
	declared Detector
	pattern  *regexp.Regexp
}

// NewInspector validates and freezes detectors. Duplicate detector IDs are
// rejected because they would make the findings digest ambiguous.
func NewInspector(detectors ...Detector) (*Inspector, error) {
	seen := make(map[string]struct{}, len(detectors))
	compiled := make([]compiledDetector, 0, len(detectors))
	for _, d := range detectors {
		if err := d.validate(); err != nil {
			return nil, err
		}
		if _, ok := seen[d.ID]; ok {
			return nil, fmt.Errorf("%w: duplicate detector %q", ErrInvalidDetector, d.ID)
		}
		seen[d.ID] = struct{}{}
		var pattern *regexp.Regexp
		if d.Pattern != "" {
			var err error
			// Detector patterns are configuration compiled once per scanner here.
			pattern, err = regexp.Compile(d.Pattern) // regexhoist:dynamic
			if err != nil {
				return nil, fmt.Errorf("%w: detector %q pattern: %v", ErrInvalidDetector, d.ID, err)
			}
		}
		compiled = append(compiled, compiledDetector{declared: d, pattern: pattern})
	}
	sort.Slice(compiled, func(i, j int) bool { return compiled[i].declared.ID < compiled[j].declared.ID })
	return &Inspector{detectors: compiled}, nil
}

// Inspect scans payload and retains no payload bytes after returning.
func (i *Inspector) Inspect(payload []byte) (Inspection, error) {
	if i == nil {
		return Inspection{}, ErrInvalidInspector
	}
	findings := make([]Finding, 0)
	for _, d := range i.detectors {
		locations := make([]Location, 0)
		if d.pattern != nil {
			for _, pair := range d.pattern.FindAllIndex(payload, -1) {
				locations = append(locations, Location{Start: pair[0], End: pair[1]})
			}
		} else {
			locations = append(locations, d.declared.Matcher(payload)...)
		}
		for _, location := range locations {
			finding := Finding{DetectorID: d.declared.ID, Class: d.declared.Class, Location: location, Severity: d.declared.Severity}
			if err := finding.validate(len(payload)); err != nil {
				return Inspection{}, fmt.Errorf("%w: detector %q: %v", ErrInvalidFinding, d.declared.ID, err)
			}
			findings = append(findings, finding)
		}
	}
	sortFindings(findings)
	return Inspection{Findings: findings}, nil
}

// Decision is the action required before an inspected payload may leave.
type Decision string

const (
	Allow            Decision = "ALLOW"
	Redact           Decision = "REDACT"
	Refuse           Decision = "REFUSE"
	ApprovalRequired Decision = "APPROVAL_REQUIRED"

	DecisionAllow            = Allow
	DecisionRedact           = Redact
	DecisionRefuse           = Refuse
	DecisionApprovalRequired = ApprovalRequired
)

func (d Decision) valid() bool {
	switch d {
	case Allow, Redact, Refuse, ApprovalRequired:
		return true
	default:
		return false
	}
}

// Valid reports whether d is a recognized policy action.
func (d Decision) Valid() bool { return d.valid() }

// Clearance declares the DLP action for one destination and class. Classes
// is a convenience for assigning one action to several classes; DataClass is
// retained for the common single-class form.
type Clearance struct {
	Destination string
	DataClass   DataClass
	Classes     []DataClass
	Decision    Decision
}

// Policy composes DLP clearances with an outbound allowlist by reference.
// The outbound pointer is never copied, so a gateway and DLP cannot silently
// drift to different destination trust policies.
type Policy struct {
	outbound   *outbound.Policy
	clearances map[string]map[DataClass]Decision
}

var (
	ErrInvalidDetector  = errors.New("dlp: invalid detector")
	ErrInvalidFinding   = errors.New("dlp: invalid finding")
	ErrInvalidInspector = errors.New("dlp: invalid inspector")
	ErrInvalidPolicy    = errors.New("dlp: invalid policy")
	ErrInvalidRequest   = errors.New("dlp: invalid decision request")
	ErrInvalidReceipt   = errors.New("dlp: invalid egress receipt")
	ErrReceiptTampered  = errors.New("dlp: egress receipt chain is tampered")
)

// NewPolicy validates and freezes DLP clearances while retaining the exact
// outbound policy pointer supplied by the caller.
func NewPolicy(egress *outbound.Policy, clearances ...Clearance) (*Policy, error) {
	if egress == nil {
		return nil, fmt.Errorf("%w: outbound policy is required", ErrInvalidPolicy)
	}
	set := make(map[string]map[DataClass]Decision)
	for _, clearance := range clearances {
		if strings.TrimSpace(clearance.Destination) == "" || strings.TrimSpace(clearance.Destination) != clearance.Destination {
			return nil, fmt.Errorf("%w: clearance destination is required", ErrInvalidPolicy)
		}
		if !clearance.Decision.valid() {
			return nil, fmt.Errorf("%w: invalid decision %q", ErrInvalidPolicy, clearance.Decision)
		}
		classes := append([]DataClass(nil), clearance.Classes...)
		if clearance.DataClass != "" {
			classes = append(classes, clearance.DataClass)
		}
		if len(classes) == 0 {
			return nil, fmt.Errorf("%w: clearance %q has no classes", ErrInvalidPolicy, clearance.Destination)
		}
		if _, ok := set[clearance.Destination]; !ok {
			set[clearance.Destination] = make(map[DataClass]Decision)
		}
		for _, class := range classes {
			if !class.valid() {
				return nil, fmt.Errorf("%w: unknown data class %q", ErrInvalidPolicy, class)
			}
			if _, duplicate := set[clearance.Destination][class]; duplicate {
				return nil, fmt.Errorf("%w: duplicate clearance for %q/%q", ErrInvalidPolicy, clearance.Destination, class)
			}
			set[clearance.Destination][class] = clearance.Decision
		}
	}
	return &Policy{outbound: egress, clearances: set}, nil
}

// DecisionRequest is the redaction-safe input to Policy.Evaluate. The
// payload itself is intentionally absent: callers must inspect first and
// pass only the resulting findings.
type DecisionRequest struct {
	Destination     string
	Purpose         string
	Principal       string
	DeclaredClasses []DataClass
	Inspection      Inspection
}

// Evaluation contains the action and the safe reason for it. It has no raw
// payload or matched text.
type Evaluation struct {
	Decision       Decision
	Destination    string
	Purpose        string
	Principal      string
	Classes        []DataClass
	FindingsDigest string
	Reason         string
}

func (r DecisionRequest) validate() error {
	for _, item := range []struct{ name, value string }{
		{"destination", r.Destination}, {"purpose", r.Purpose}, {"principal", r.Principal},
	} {
		if strings.TrimSpace(item.value) == "" || strings.TrimSpace(item.value) != item.value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidRequest, item.name)
		}
	}
	for _, class := range r.DeclaredClasses {
		if !class.valid() {
			return fmt.Errorf("%w: unknown declared class %q", ErrInvalidRequest, class)
		}
	}
	for _, finding := range r.Inspection.Findings {
		if !finding.Class.valid() || !finding.Severity.valid() || strings.TrimSpace(finding.DetectorID) == "" || finding.Location.Start < 0 || finding.Location.End <= finding.Location.Start {
			return fmt.Errorf("%w: invalid inspection finding", ErrInvalidRequest)
		}
	}
	return nil
}

// Evaluate applies the outbound destination/purpose/class allowlist first,
// then the DLP action for each class. An expected refusal is a successful
// evaluation with Decision Refuse; errors are reserved for malformed input
// or policy configuration.
func (p *Policy) Evaluate(r DecisionRequest) (Evaluation, error) {
	if p == nil || p.outbound == nil {
		return Evaluation{}, fmt.Errorf("%w: outbound policy is missing", ErrInvalidPolicy)
	}
	if err := r.validate(); err != nil {
		return Evaluation{}, err
	}
	classes := uniqueClasses(r.DeclaredClasses)
	for _, finding := range r.Inspection.Findings {
		classes = appendUniqueClass(classes, finding.Class)
	}
	if len(classes) == 0 {
		classes = []DataClass{ClassPublic}
	}

	eval := Evaluation{
		Decision:       Allow,
		Destination:    r.Destination,
		Purpose:        r.Purpose,
		Principal:      r.Principal,
		Classes:        append([]DataClass(nil), classes...),
		FindingsDigest: r.Inspection.Digest(),
	}
	for _, class := range classes {
		if _, err := p.outbound.Check(outbound.CheckRequest{Destination: r.Destination, Purpose: r.Purpose, DataClass: string(class)}); err != nil {
			eval.Decision = Refuse
			eval.Reason = "destination is not cleared for every declared or detected data class"
			return eval, nil
		}
		decision := Allow
		if byClass, ok := p.clearances[r.Destination]; ok {
			if configured, ok := byClass[class]; ok {
				decision = configured
			}
		}
		switch decision {
		case Refuse, ApprovalRequired:
			eval.Decision = decision
			eval.Reason = "destination clearance requires refusal or separate approval"
			return eval, nil
		case Redact:
			if eval.Decision == Allow {
				eval.Decision = Redact
			}
		}
	}
	if len(r.Inspection.Findings) > 0 && eval.Decision == Allow {
		// A detector finding that is explicitly cleared remains allowed. The
		// distinction is intentional: DLP policy, not detector presence alone,
		// decides whether a declared class must be transformed.
		eval.Reason = "all detected classes are cleared"
	}
	return eval, nil
}

// Decide is the compact policy API for callers that need only the action.
func (p *Policy) Decide(r DecisionRequest) (Decision, error) {
	eval, err := p.Evaluate(r)
	if err != nil {
		return "", err
	}
	return eval.Decision, nil
}

// ReceiptInput is the data needed to append an egress receipt. Payload is
// consumed only to compute its digest and is never retained. Callers that
// already computed the digest may set PayloadDigest and leave Payload nil.
type ReceiptInput struct {
	Destination    string
	Purpose        string
	Principal      string
	Payload        []byte
	PayloadDigest  string
	Inspection     Inspection
	FindingsDigest string
	Decision       Decision
}

// EgressReceipt is one immutable ledger entry. PreviousDigest makes the
// receipt stream append-only and tamper-evident without storing content.
type EgressReceipt struct {
	Sequence       uint64
	PreviousDigest string
	Destination    string
	Purpose        string
	PayloadDigest  string
	FindingsDigest string
	Decision       Decision
	Principal      string
	Digest         string
}

// Canonical returns the framed representation hashed for Digest.
func (r EgressReceipt) Canonical() string {
	var b strings.Builder
	frame := func(name, value string) {
		fmt.Fprintf(&b, "%d:%s=%d:%s;", len(name), name, len(value), value)
	}
	frame("sequence", fmt.Sprintf("%d", r.Sequence))
	frame("previous_digest", r.PreviousDigest)
	frame("destination", r.Destination)
	frame("purpose", r.Purpose)
	frame("payload_digest", r.PayloadDigest)
	frame("findings_digest", r.FindingsDigest)
	frame("decision", string(r.Decision))
	frame("principal", r.Principal)
	return b.String()
}

// DigestOfReceipt returns the canonical SHA-256 receipt digest.
func DigestOfReceipt(r EgressReceipt) string {
	sum := sha256.Sum256([]byte(r.Canonical()))
	return hex.EncodeToString(sum[:])
}

// DigestPayload returns the SHA-256 digest of payload bytes.
func DigestPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// DigestFindings returns a canonical SHA-256 digest over redaction-safe
// finding fields only.
func DigestFindings(findings []Finding) string {
	copyFindings := append([]Finding(nil), findings...)
	sortFindings(copyFindings)
	var b strings.Builder
	for _, finding := range copyFindings {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s\n",
			finding.DetectorID, finding.Class, finding.Severity, finding.Location.Path,
			finding.Location.Start, finding.Location.End, "finding")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ReceiptLog is an in-memory append-only egress ledger suitable for the pure
// policy layer and tests. Production persistence can implement the same
// append semantics around EgressReceipt without changing its digest.
type ReceiptLog struct {
	mu       sync.Mutex
	receipts []EgressReceipt
}

// NewReceiptLog creates an empty append-only receipt stream.
func NewReceiptLog() *ReceiptLog { return &ReceiptLog{} }

// NewLedger is an alias for callers that name the stream a ledger.
func NewLedger() *ReceiptLog { return NewReceiptLog() }

// Append computes missing digests, validates the redaction-safe input, and
// appends exactly one receipt. It never stores Payload or any matched text.
func (l *ReceiptLog) Append(in ReceiptInput) (EgressReceipt, error) {
	if l == nil {
		return EgressReceipt{}, ErrInvalidReceipt
	}
	if err := validateReceiptInput(in); err != nil {
		return EgressReceipt{}, err
	}
	payloadDigest := in.PayloadDigest
	if payloadDigest == "" {
		payloadDigest = DigestPayload(in.Payload)
	}
	findingsDigest := in.FindingsDigest
	if findingsDigest == "" {
		findingsDigest = in.Inspection.Digest()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := ""
	if len(l.receipts) > 0 {
		previous = l.receipts[len(l.receipts)-1].Digest
	}
	receipt := EgressReceipt{
		Sequence:       uint64(len(l.receipts) + 1),
		PreviousDigest: previous,
		Destination:    in.Destination,
		Purpose:        in.Purpose,
		PayloadDigest:  payloadDigest,
		FindingsDigest: findingsDigest,
		Decision:       in.Decision,
		Principal:      in.Principal,
	}
	receipt.Digest = DigestOfReceipt(receipt)
	l.receipts = append(l.receipts, receipt)
	return receipt, nil
}

func validateReceiptInput(in ReceiptInput) error {
	for _, item := range []struct{ name, value string }{
		{"destination", in.Destination}, {"purpose", in.Purpose}, {"principal", in.Principal},
	} {
		if strings.TrimSpace(item.value) == "" || strings.TrimSpace(item.value) != item.value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidReceipt, item.name)
		}
	}
	if !in.Decision.valid() {
		return fmt.Errorf("%w: invalid decision %q", ErrInvalidReceipt, in.Decision)
	}
	if in.PayloadDigest != "" && !validDigest(in.PayloadDigest) {
		return fmt.Errorf("%w: invalid payload digest", ErrInvalidReceipt)
	}
	if in.FindingsDigest != "" && !validDigest(in.FindingsDigest) {
		return fmt.Errorf("%w: invalid findings digest", ErrInvalidReceipt)
	}
	if in.PayloadDigest != "" && len(in.Payload) > 0 && in.PayloadDigest != DigestPayload(in.Payload) {
		return fmt.Errorf("%w: payload digest does not match payload", ErrInvalidReceipt)
	}
	if in.FindingsDigest != "" && len(in.Inspection.Findings) > 0 && in.FindingsDigest != in.Inspection.Digest() {
		return fmt.Errorf("%w: findings digest does not match inspection", ErrInvalidReceipt)
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Receipts returns a copy of the immutable receipt stream.
func (l *ReceiptLog) Receipts() []EgressReceipt {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]EgressReceipt(nil), l.receipts...)
}

// Verify checks sequence numbers, canonical digests, and previous-digest
// links for the complete stream.
func (l *ReceiptLog) Verify() error {
	if l == nil {
		return ErrReceiptTampered
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := ""
	for index, receipt := range l.receipts {
		if receipt.Sequence != uint64(index+1) || receipt.PreviousDigest != previous || receipt.Digest != DigestOfReceipt(receipt) {
			return fmt.Errorf("%w: sequence %d", ErrReceiptTampered, receipt.Sequence)
		}
		previous = receipt.Digest
	}
	return nil
}

func uniqueClasses(classes []DataClass) []DataClass {
	out := make([]DataClass, 0, len(classes))
	for _, class := range classes {
		out = appendUniqueClass(out, class)
	}
	return out
}

func appendUniqueClass(classes []DataClass, class DataClass) []DataClass {
	for _, existing := range classes {
		if existing == class {
			return classes
		}
	}
	return append(classes, class)
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.Location.Path != right.Location.Path {
			return left.Location.Path < right.Location.Path
		}
		if left.Location.Start != right.Location.Start {
			return left.Location.Start < right.Location.Start
		}
		if left.Location.End != right.Location.End {
			return left.Location.End < right.Location.End
		}
		if left.Class != right.Class {
			return left.Class < right.Class
		}
		if left.Severity != right.Severity {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		return left.DetectorID < right.DetectorID
	})
}
