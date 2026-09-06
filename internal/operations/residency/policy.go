// Package residency admits tenant data only when every observed copy and
// transfer is covered by a signed, jurisdiction-based policy.
package residency

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func Version() int { return 1 }

var (
	ErrInvalidPolicy    = errors.New("residency: invalid policy")
	ErrInvalidRecord    = errors.New("residency: invalid placement record")
	ErrInvalidSignature = errors.New("residency: invalid policy signature")
)

type State string

const (
	Allowed                State = "ALLOWED"
	Unknown                State = "UNKNOWN"
	TransferReviewRequired State = "TRANSFER_REVIEW_REQUIRED"
	Blocked                State = "BLOCKED"
)

type JurisdictionSet struct {
	IDs      []string
	Revision string
}
type Location struct {
	ID           string
	Jurisdiction string
}
type Processor struct {
	ID   string
	Kind string
}
type DataClass string
type CopyKind string

const (
	CopyPrimary   CopyKind = "PRIMARY"
	CopyDerived   CopyKind = "DERIVED"
	CopyProvider  CopyKind = "PROVIDER"
	CopyBackup    CopyKind = "BACKUP"
	CopyRecovery  CopyKind = "RECOVERY"
	CopyTelemetry CopyKind = "TELEMETRY"
	CopyArtifact  CopyKind = "ARTIFACT"
	CopySupport   CopyKind = "SUPPORT"
)

type Rule struct {
	DataClass       DataClass
	Purpose         string
	Processing      []Location
	Storage         []Location
	Support         []Location
	TransferTargets []Location
	Processors      []Processor
}

type Policy struct {
	ID              string
	Revision        string
	Jurisdictions   JurisdictionSet
	Rules           []Rule
	Digest          string
	SignerKeyID     string
	SignerPublicKey []byte
	Signature       []byte
}

type Copy struct {
	ID        string
	TenantID  string
	Class     DataClass
	Purpose   string
	Kind      CopyKind
	Location  Location
	Processor Processor
	Observed  bool
}

type Route struct {
	ID        string
	TenantID  string
	Class     DataClass
	Purpose   string
	From      Location
	To        Location
	Processor Processor
	Observed  bool
}

type Restore struct {
	ID        string
	TenantID  string
	Class     DataClass
	Purpose   string
	Target    Location
	Processor Processor
	Observed  bool
}

type Inventory struct {
	Copies   []Copy
	Routes   []Route
	Restores []Restore
}

type Decision struct {
	State                State
	PolicyID             string
	PolicyDigest         string
	JurisdictionRevision string
	CheckedCopies        int
	CheckedRoutes        int
	CheckedRestores      int
	Unknown              []string
	TransferReviews      []string
	Reason               string
}

func NewPolicy(id, revision string, jurisdictions JurisdictionSet, rules []Rule) (Policy, error) {
	p := Policy{ID: id, Revision: revision, Jurisdictions: cloneJurisdictions(jurisdictions), Rules: cloneRules(rules)}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	p.Digest = digest(policyBody(p))
	return p, nil
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Revision) == "" || strings.TrimSpace(p.Jurisdictions.Revision) == "" || len(p.Jurisdictions.IDs) == 0 || len(p.Rules) == 0 {
		return fmt.Errorf("%w: id, revision, jurisdiction set and rules are required", ErrInvalidPolicy)
	}
	for _, id := range p.Jurisdictions.IDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: empty jurisdiction", ErrInvalidPolicy)
		}
	}
	for _, rule := range p.Rules {
		if strings.TrimSpace(string(rule.DataClass)) == "" || strings.TrimSpace(rule.Purpose) == "" || len(rule.Processing) == 0 || len(rule.Storage) == 0 || len(rule.Processors) == 0 {
			return fmt.Errorf("%w: incomplete rule", ErrInvalidPolicy)
		}
		if len(rule.Support) == 0 {
			return fmt.Errorf("%w: support locations are required", ErrInvalidPolicy)
		}
		for _, location := range append(append(append([]Location{}, rule.Processing...), rule.Storage...), append(rule.Support, rule.TransferTargets...)...) {
			if location.ID == "" || location.Jurisdiction == "" {
				return fmt.Errorf("%w: location identity and jurisdiction are required", ErrInvalidPolicy)
			}
			if !containsString(p.Jurisdictions.IDs, location.Jurisdiction) {
				return fmt.Errorf("%w: location jurisdiction %q is outside the resolved jurisdiction set", ErrInvalidPolicy, location.Jurisdiction)
			}
		}
		for _, processor := range rule.Processors {
			if processor.ID == "" || processor.Kind == "" {
				return fmt.Errorf("%w: processor identity and kind are required", ErrInvalidPolicy)
			}
		}
	}
	return nil
}

func (p Policy) Sign(keyID string, privateKey ed25519.PrivateKey) (Policy, error) {
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	if len(privateKey) != ed25519.PrivateKeySize || strings.TrimSpace(keyID) == "" {
		return Policy{}, ErrInvalidSignature
	}
	p.Digest = digest(policyBody(p))
	p.SignerKeyID, p.SignerPublicKey = keyID, append([]byte(nil), privateKey.Public().(ed25519.PublicKey)...)
	p.Signature = ed25519.Sign(privateKey, []byte(p.Digest))
	return p, nil
}

func (p Policy) Verify() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if digest(policyBody(p)) != p.Digest || len(p.SignerPublicKey) != ed25519.PublicKeySize || len(p.Signature) != ed25519.SignatureSize || strings.TrimSpace(p.SignerKeyID) == "" || !ed25519.Verify(p.SignerPublicKey, []byte(p.Digest), p.Signature) {
		return ErrInvalidSignature
	}
	return nil
}

func (p Policy) Evaluate(in Inventory) (Decision, error) {
	if err := p.Verify(); err != nil {
		return Decision{}, err
	}
	d := Decision{State: Allowed, PolicyID: p.ID, PolicyDigest: p.Digest, JurisdictionRevision: p.Jurisdictions.Revision}
	for _, copy := range in.Copies {
		d.CheckedCopies++
		if !copy.Observed || copy.TenantID == "" || copy.Location.ID == "" || copy.Processor.ID == "" {
			d.Unknown = append(d.Unknown, copy.ID)
			continue
		}
		rule, ok := p.rule(copy.Class, copy.Purpose)
		if !ok {
			d.Unknown = append(d.Unknown, copy.ID)
			continue
		}
		if !containsLocation(rule.Storage, copy.Location) || !containsProcessor(rule.Processors, copy.Processor) {
			d.State = Blocked
			d.Reason = "copy is outside approved storage or processor"
		}
	}
	for _, route := range in.Routes {
		d.CheckedRoutes++
		if !route.Observed || route.TenantID == "" || route.From.ID == "" || route.To.ID == "" || route.Processor.ID == "" {
			d.Unknown = append(d.Unknown, route.ID)
			continue
		}
		rule, ok := p.rule(route.Class, route.Purpose)
		if !ok {
			d.Unknown = append(d.Unknown, route.ID)
			continue
		}
		if !containsLocation(rule.TransferTargets, route.To) || !containsProcessor(rule.Processors, route.Processor) {
			d.TransferReviews = append(d.TransferReviews, route.ID)
		}
	}
	for _, restore := range in.Restores {
		d.CheckedRestores++
		if !restore.Observed || restore.TenantID == "" || restore.Target.ID == "" || restore.Processor.ID == "" {
			d.Unknown = append(d.Unknown, restore.ID)
			continue
		}
		rule, ok := p.rule(restore.Class, restore.Purpose)
		if !ok {
			d.Unknown = append(d.Unknown, restore.ID)
			continue
		}
		if !containsLocation(rule.Storage, restore.Target) || !containsProcessor(rule.Processors, restore.Processor) {
			d.State = Blocked
			d.Reason = "restore target is outside approved storage or processor"
		}
	}
	sort.Strings(d.Unknown)
	sort.Strings(d.TransferReviews)
	if d.State == Blocked {
		return d, nil
	}
	if len(d.Unknown) > 0 {
		d.State = Unknown
		d.Reason = "inventory contains an unknown placement"
	}
	if len(d.TransferReviews) > 0 && d.State == Allowed {
		d.State = TransferReviewRequired
		d.Reason = "transfer requires an explicit review"
	}
	return d, nil
}

func (d Decision) Admitted() bool { return d.State == Allowed }
func (d Decision) Explain() string {
	return fmt.Sprintf("residency state=%s policy=%s copies=%d routes=%d restores=%d unknown=%d transfer_reviews=%d", d.State, d.PolicyID, d.CheckedCopies, d.CheckedRoutes, d.CheckedRestores, len(d.Unknown), len(d.TransferReviews))
}
func Explain(d Decision) string { return d.Explain() }

func (p Policy) rule(class DataClass, purpose string) (Rule, bool) {
	for _, rule := range p.Rules {
		if rule.DataClass == class && rule.Purpose == purpose {
			return cloneRule(rule), true
		}
	}
	return Rule{}, false
}
func containsLocation(list []Location, want Location) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
func containsProcessor(list []Processor, want Processor) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

func containsString(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
func digest(b []byte) string { sum := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(sum[:]) }

type canonicalPolicy struct {
	ID            string          `json:"id"`
	Revision      string          `json:"revision"`
	Jurisdictions JurisdictionSet `json:"jurisdictions"`
	Rules         []Rule          `json:"rules"`
}

func policyBody(p Policy) []byte {
	b, _ := json.Marshal(canonicalPolicy{p.ID, p.Revision, p.Jurisdictions, p.Rules})
	return b
}
func cloneJurisdictions(in JurisdictionSet) JurisdictionSet {
	return JurisdictionSet{IDs: append([]string(nil), in.IDs...), Revision: in.Revision}
}
func cloneRules(in []Rule) []Rule {
	out := make([]Rule, len(in))
	for i, rule := range in {
		out[i] = cloneRule(rule)
	}
	return out
}
func cloneRule(in Rule) Rule {
	in.Processing = append([]Location(nil), in.Processing...)
	in.Storage = append([]Location(nil), in.Storage...)
	in.Support = append([]Location(nil), in.Support...)
	in.TransferTargets = append([]Location(nil), in.TransferTargets...)
	in.Processors = append([]Processor(nil), in.Processors...)
	return in
}
