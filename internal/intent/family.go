package intent

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Family is the stable kernel execution family. There are exactly three, and
// what distinguishes them is one question: may this intent cause a material
// mutation or effect?
//
// Process, filing, batch and case semantics are attributes of a CHANGE_REQUEST
// definition (side-effect profile, population scope, child-intent bindings),
// not families. A fourth family is added only when a funded domain proves an
// attribute cannot express the distinction.
//
// Numeric identity matches hcmnext.intents.v1.KernelFamily, which reserves 2,
// 4, 5 and 6 where the retired PROCESS_REQUEST, FILING_REQUEST, CASE and
// BATCH_OPERATION families used to sit. The gaps are load-bearing: a stored
// value of 2 is a retired family, not an unknown one.
type Family uint8

// The three kernel families. The numbers are the Protobuf enum numbers.
const (
	FamilyUnspecified        Family = 0
	FamilyChangeRequest      Family = 1
	FamilyCalculationRequest Family = 3
	FamilyAnalyticalRequest  Family = 7
)

var familyNames = map[Family]string{
	FamilyUnspecified:        "UNSPECIFIED",
	FamilyChangeRequest:      "CHANGE_REQUEST",
	FamilyCalculationRequest: "CALCULATION_REQUEST",
	FamilyAnalyticalRequest:  "ANALYTICAL_REQUEST",
}

// reservedFamilyNumbers are the enum numbers of the four retired families.
// Resolving one names the family that was retired rather than reporting an
// unknown number, so a historical record explains itself.
var reservedFamilyNumbers = map[uint8]string{
	2: "KERNEL_FAMILY_PROCESS_REQUEST",
	4: "KERNEL_FAMILY_FILING_REQUEST",
	5: "KERNEL_FAMILY_CASE",
	6: "KERNEL_FAMILY_BATCH_OPERATION",
}

// Families returns the three kernel families.
func Families() []Family {
	return []Family{FamilyChangeRequest, FamilyCalculationRequest, FamilyAnalyticalRequest}
}

// ReservedFamilyName returns the retired family name for a reserved enum
// number, and whether the number is reserved.
func ReservedFamilyName(number uint8) (string, bool) {
	name, ok := reservedFamilyNumbers[number]
	return name, ok
}

func (f Family) String() string {
	if n, ok := familyNames[f]; ok {
		return n
	}
	if n, ok := reservedFamilyNumbers[uint8(f)]; ok {
		return "RESERVED(" + n + ")"
	}
	return "KernelFamily(" + strconv.Itoa(int(f)) + ")"
}

// Valid reports whether f is one of the three kernel families.
func (f Family) Valid() bool {
	return f == FamilyChangeRequest || f == FamilyCalculationRequest || f == FamilyAnalyticalRequest
}

// MayMutate reports whether the family may cause a material mutation or
// external effect. Only CHANGE_REQUEST may.
func (f Family) MayMutate() bool { return f == FamilyChangeRequest }

// ParseFamily resolves a canonical family name.
func ParseFamily(name string) (Family, error) {
	for f, n := range familyNames {
		if n == name && f.Valid() {
			return f, nil
		}
	}
	return FamilyUnspecified, newError("ParseFamily", "kernel_family", ErrInvalidDefinition,
		"%q is not one of CHANGE_REQUEST, CALCULATION_REQUEST or ANALYTICAL_REQUEST", name)
}

// SideEffect is a definition's declared side-effect profile. Numeric identity
// matches hcmnext.intents.v1.SideEffectProfile.
type SideEffect uint8

// SideEffect values.
const (
	SideEffectUnspecified                  SideEffect = 0
	SideEffectPure                         SideEffect = 1
	SideEffectReadOnly                     SideEffect = 2
	SideEffectInternalMutation             SideEffect = 3
	SideEffectExternalMutation             SideEffect = 4
	SideEffectIrreversibleExternalMutation SideEffect = 5
)

var sideEffectNames = map[SideEffect]string{
	SideEffectUnspecified:                  "UNSPECIFIED",
	SideEffectPure:                         "PURE",
	SideEffectReadOnly:                     "READ_ONLY",
	SideEffectInternalMutation:             "INTERNAL_MUTATION",
	SideEffectExternalMutation:             "EXTERNAL_MUTATION",
	SideEffectIrreversibleExternalMutation: "IRREVERSIBLE_EXTERNAL_MUTATION",
}

func (s SideEffect) String() string { return enumName(sideEffectNames, s, "SideEffectProfile") }

// Valid reports whether s is a declared side-effect profile other than
// UNSPECIFIED.
func (s SideEffect) Valid() bool {
	return s != SideEffectUnspecified && s <= SideEffectIrreversibleExternalMutation
}

// Mutates reports whether the profile declares a material mutation or external
// effect.
func (s SideEffect) Mutates() bool {
	switch s {
	case SideEffectInternalMutation, SideEffectExternalMutation, SideEffectIrreversibleExternalMutation:
		return true
	default:
		return false
	}
}

// Irreversible reports whether the profile declares an irreversible external
// effect, which additionally requires correction semantics.
func (s SideEffect) Irreversible() bool { return s == SideEffectIrreversibleExternalMutation }

// EffectClass is the effect ceiling a definition may actually reach in the
// release it is scheduled for. It is distinct from [SideEffect], which is the
// definition's permanent declared profile: PromoteWorker declares
// INTERNAL_MUTATION forever, and its P1A effect class is nevertheless
// ZERO_EFFECT because P1A only simulates it.
type EffectClass uint8

// EffectClass values.
const (
	EffectClassUnspecified EffectClass = iota
	// EffectClassZero means the intent produces no domain mutation and no
	// external effect in its scheduled release.
	EffectClassZero
	EffectClassInternalMutation
	EffectClassExternalMutation
	EffectClassIrreversibleExternal
)

var effectClassNames = map[EffectClass]string{
	EffectClassUnspecified:          "UNSPECIFIED",
	EffectClassZero:                 "ZERO_EFFECT",
	EffectClassInternalMutation:     "INTERNAL_MUTATION",
	EffectClassExternalMutation:     "EXTERNAL_MUTATION",
	EffectClassIrreversibleExternal: "IRREVERSIBLE_EXTERNAL_MUTATION",
}

func (e EffectClass) String() string { return enumName(effectClassNames, e, "EffectClass") }

// Valid reports whether e is a declared effect class other than UNSPECIFIED.
func (e EffectClass) Valid() bool {
	return e != EffectClassUnspecified && e <= EffectClassIrreversibleExternal
}

// Maturity is a definition's explicit maturity. Numeric identity matches
// hcmnext.intents.v1.DefinitionMaturity, whose 1 is reserved: CATALOGUED is
// retired, and a name below DRAFT_CONTRACT is simply not in the catalog.
type Maturity uint8

// Maturity values.
const (
	MaturityUnspecified   Maturity = 0
	MaturityContracted    Maturity = 2
	MaturityCompiled      Maturity = 3
	MaturityPublished     Maturity = 4
	MaturityDeprecated    Maturity = 5
	MaturityRetired       Maturity = 6
	MaturityDraftContract Maturity = 7
)

var maturityNames = map[Maturity]string{
	MaturityUnspecified:   "UNSPECIFIED",
	MaturityContracted:    "CONTRACTED",
	MaturityCompiled:      "COMPILED",
	MaturityPublished:     "PUBLISHED",
	MaturityDeprecated:    "DEPRECATED",
	MaturityRetired:       "RETIRED",
	MaturityDraftContract: "DRAFT_CONTRACT",
}

func (m Maturity) String() string { return enumName(maturityNames, m, "DefinitionMaturity") }

// Valid reports whether m is a declared maturity other than UNSPECIFIED.
func (m Maturity) Valid() bool { _, ok := maturityNames[m]; return ok && m != MaturityUnspecified }

// rank orders maturity for the "at least DRAFT_CONTRACT" test. The Protobuf
// numbers are not ordered — DRAFT_CONTRACT is 7 and sits below CONTRACTED at 2
// — so ordering is explicit rather than numeric.
func (m Maturity) rank() int {
	switch m {
	case MaturityDraftContract:
		return 1
	case MaturityContracted:
		return 2
	case MaturityCompiled:
		return 3
	case MaturityPublished:
		return 4
	case MaturityDeprecated:
		return 5
	case MaturityRetired:
		return 6
	default:
		return 0
	}
}

// InCatalog reports whether the maturity is at or above DRAFT_CONTRACT. Nothing
// below DRAFT_CONTRACT is in the catalog at all.
func (m Maturity) InCatalog() bool { return m.rank() >= MaturityDraftContract.rank() }

// Invocable reports whether an instance may be created against a definition at
// this maturity. RETIRED prohibits invocation while preserving historical
// resolution.
func (m Maturity) Invocable() bool { return m.InCatalog() && m != MaturityRetired }

// Initiator is an allowed initiator kind. Numeric identity matches
// hcmnext.intents.v1.InitiatorKind.
type Initiator uint8

// Initiator values.
const (
	InitiatorUnspecified Initiator = 0
	InitiatorHuman       Initiator = 1
	InitiatorAgent       Initiator = 2
	InitiatorService     Initiator = 3
	InitiatorIntegration Initiator = 4
	InitiatorSchedule    Initiator = 5
	InitiatorRule        Initiator = 6
	InitiatorSystemEvent Initiator = 7
)

var initiatorNames = map[Initiator]string{
	InitiatorUnspecified: "UNSPECIFIED",
	InitiatorHuman:       "HUMAN",
	InitiatorAgent:       "AGENT",
	InitiatorService:     "SERVICE",
	InitiatorIntegration: "INTEGRATION",
	InitiatorSchedule:    "SCHEDULE",
	InitiatorRule:        "RULE",
	InitiatorSystemEvent: "SYSTEM_EVENT",
}

func (i Initiator) String() string { return enumName(initiatorNames, i, "InitiatorKind") }

// Valid reports whether i is a declared initiator kind other than UNSPECIFIED.
func (i Initiator) Valid() bool { _, ok := initiatorNames[i]; return ok && i != InitiatorUnspecified }

// Mode is an execution mode. Numeric identity matches
// hcmnext.intents.v1.ExecutionMode.
type Mode uint8

// Mode values.
const (
	ModeUnspecified Mode = 0
	ModeSimulate    Mode = 1
	ModeExecute     Mode = 2
	ModeReplay      Mode = 3
	ModeRepair      Mode = 4
	ModeShadow      Mode = 5
)

var modeNames = map[Mode]string{
	ModeUnspecified: "UNSPECIFIED",
	ModeSimulate:    "SIMULATE",
	ModeExecute:     "EXECUTE",
	ModeReplay:      "REPLAY",
	ModeRepair:      "REPAIR",
	ModeShadow:      "SHADOW",
}

func (m Mode) String() string { return enumName(modeNames, m, "ExecutionMode") }

// Valid reports whether m is a declared execution mode other than UNSPECIFIED.
func (m Mode) Valid() bool { _, ok := modeNames[m]; return ok && m != ModeUnspecified }

// Release is the release a definition is scheduled for. It has no Protobuf
// counterpart: it is a delivery fact the registry enforces, not wire state.
type Release uint8

// Release values.
const (
	ReleaseUnspecified Release = iota
	// ReleaseP1A is the paid observe/preflight/simulate release. Every P1A
	// definition must carry EffectClassZero and must not allow EXECUTE.
	ReleaseP1A
	// ReleaseP1B is the bounded write release, reachable only after a signed
	// Gate A PROCEED.
	ReleaseP1B
	// ReleaseConformance marks a design/conformance fixture that no release
	// executes.
	ReleaseConformance
)

var releaseNames = map[Release]string{
	ReleaseUnspecified: "UNSPECIFIED",
	ReleaseP1A:         "P1A",
	ReleaseP1B:         "P1B",
	ReleaseConformance: "CONFORMANCE",
}

func (r Release) String() string { return enumName(releaseNames, r, "Release") }

// Valid reports whether r is a declared release other than UNSPECIFIED.
func (r Release) Valid() bool { _, ok := releaseNames[r]; return ok && r != ReleaseUnspecified }

func enumName[T ~uint8](table map[T]string, v T, kind string) string {
	if n, ok := table[v]; ok {
		return n
	}
	return kind + "(" + strconv.Itoa(int(v)) + ")"
}

// Ref is a definition reference: the domain-qualified intent type id plus a
// monotonically increasing positive version. It is the only runtime identity of
// a definition. Display names are presentation, never identity.
type Ref struct {
	TypeID  string
	Version uint32
}

// ParseRef parses the canonical "intent_type_id/vN" form.
func ParseRef(s string) (Ref, error) {
	typeID, version, ok := strings.Cut(s, "/v")
	if !ok {
		return Ref{}, newError("ParseRef", "definition_ref", ErrInvalidReference,
			"%q is not <intent_type_id>/v<version>", s)
	}
	if version == "" || (len(version) > 1 && version[0] == '0') {
		return Ref{}, newError("ParseRef", "definition_ref", ErrInvalidReference,
			"%q has a non-canonical version", s)
	}
	n, err := strconv.ParseUint(version, 10, 32)
	if err != nil {
		return Ref{}, newError("ParseRef", "definition_ref", ErrInvalidReference,
			"%q has an unparseable version: %v", s, err)
	}
	ref := Ref{TypeID: typeID, Version: uint32(n)}
	if err := ref.Validate(); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// String returns the canonical definition_ref text.
func (r Ref) String() string {
	return r.TypeID + "/v" + strconv.FormatUint(uint64(r.Version), 10)
}

// Validate rejects an unqualified id, a free-form display name and a zero
// version. The required shape is hcmnext.<domain>.<verb_noun>: domain
// qualification is what keeps the access and commercial grant_entitlement
// definitions distinct.
func (r Ref) Validate() error {
	if r.Version == 0 {
		return newError("Validate", "version", ErrInvalidReference,
			"version must be a positive integer")
	}
	if !utf8.ValidString(r.TypeID) {
		return newError("Validate", "intent_type_id", ErrInvalidReference,
			"intent type id is not valid UTF-8")
	}
	parts := strings.Split(r.TypeID, ".")
	if len(parts) != 3 {
		return newError("Validate", "intent_type_id", ErrInvalidReference,
			"%q is not hcmnext.<domain>.<verb_noun>", r.TypeID)
	}
	if parts[0] != "hcmnext" {
		return newError("Validate", "intent_type_id", ErrInvalidReference,
			"%q is not rooted at hcmnext", r.TypeID)
	}
	for i, p := range parts[1:] {
		if err := validateSnake(p); err != nil {
			return newError("Validate", "intent_type_id", ErrInvalidReference,
				"segment %d of %q: %v", i+1, r.TypeID, err)
		}
	}
	return nil
}

// Domain returns the owning domain segment of the type id.
func (r Ref) Domain() string {
	parts := strings.Split(r.TypeID, ".")
	if len(parts) != 3 {
		return ""
	}
	return parts[1]
}

func validateSnake(s string) error {
	if s == "" {
		return fmt.Errorf("segment is empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_'):
		default:
			return fmt.Errorf("%q is not lower_snake_case", s)
		}
	}
	if s[len(s)-1] == '_' {
		return fmt.Errorf("%q ends with an underscore", s)
	}
	return nil
}

// SchemaRef pins one version of a request or result schema.
type SchemaRef struct {
	SchemaID         string
	Version          uint32
	ProtobufFullName string
	DescriptorDigest string
}

// String returns the canonical "schema_id/vN" text.
func (s SchemaRef) String() string {
	return s.SchemaID + "/v" + strconv.FormatUint(uint64(s.Version), 10)
}

// Validate rejects an unnamed or unversioned schema reference.
func (s SchemaRef) Validate() error {
	if s.SchemaID == "" {
		return newError("Validate", "schema_id", ErrInvalidReference, "schema id is empty")
	}
	if s.Version == 0 {
		return newError("Validate", "schema.version", ErrInvalidReference,
			"schema %q has no version", s.SchemaID)
	}
	return nil
}
