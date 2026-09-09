package adapters

import (
	"fmt"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
)

// This file mirrors connectivity's two mapping forms.
//
// Mirrored, field for field, from internal/connectivity/mapping:
//
//	mapping.MappingProfile /
//	mapping.MappingProfileVersion -> ConnectivityProfile (MappingID, Version,
//	                                 SourceSystemRef, TargetEntity, Mappings,
//	                                 TargetFields)
//	mapping.FieldMapping          -> ConnectivityFieldMapping (SourceField,
//	                                 TargetField, TransformationIRDigest,
//	                                 Identity, Required, Classification)
//	mapping.IR                    -> ConnectivityRules (Version, Rules)
//	mapping.Rule                  -> ConnectivityRule (Source, Target, Op,
//	                                 Argument, Lookup, Null)
//
// Deliberately NOT mirrored:
//
//	FieldMapping.IRDigest, .TransformDigest, .TransformationDigest,
//	.TransformationRef, .Optional, .SourceClassification,
//	.TargetClassification
//	                    - compatibility spellings and derived fields that
//	                      mapping.Compile normalizes away before a
//	                      MappingProfileVersion exists. A lowering reads the
//	                      compiled, normalized form, so it reads exactly one
//	                      spelling of each fact.
//	MappingProfileVersion.Digest()
//	                    - the profile's own content identity, computed by its
//	                      own canonical form. The lowered artifact has its own
//	                      digest; conflating the two would make one of them
//	                      unfalsifiable.

// ConnectivityFieldMapping mirrors mapping.FieldMapping in its compiled,
// normalized shape.
type ConnectivityFieldMapping struct {
	SourceField string
	TargetField string
	// TransformationIRDigest is either the literal identity token or a
	// sha256 digest naming a transformation IR program.
	TransformationIRDigest string
	Identity               bool
	Required               bool
	Classification         string
}

// ConnectivityTargetField mirrors mapping.TargetField.
type ConnectivityTargetField struct {
	Name           string
	Classification string
}

// ConnectivityProfile mirrors a compiled mapping.MappingProfileVersion.
type ConnectivityProfile struct {
	MappingID       string
	Version         int
	SourceSystemRef string
	TargetEntity    string
	Mappings        []ConnectivityFieldMapping
	TargetFields    []ConnectivityTargetField
}

// ConnectivityIdentityTransformation mirrors mapping.IdentityTransformation.
const ConnectivityIdentityTransformation = "identity"

// LowerConnectivityProfile lowers a compiled connectivity mapping profile.
//
// A profile's own operation vocabulary is exactly two things: the identity
// transformation, and a reference to a transformation IR program by digest.
// The first lowers to a projection. The second lowers to nothing here on
// purpose -- the referenced program is already a shared-engine program, and
// splicing a copy of it into this profile's program would create a second
// definition of the same transformation. It is recorded as a [Delegation]
// instead, so the profile's full field set is still accounted for.
func LowerConnectivityProfile(p ConnectivityProfile) (Lowered, error) {
	site := SiteConnectivityProfile
	if p.MappingID == "" || p.Version < 1 {
		return Lowered{}, refuse(site, FeatureInvalidMapping, p.MappingID,
			"profile declares no mapping id or no positive version")
	}
	if len(p.Mappings) == 0 {
		return Lowered{}, refuse(site, FeatureInvalidMapping, p.MappingID, "profile declares no field mappings")
	}
	declared := make(map[string]bool, len(p.TargetFields))
	for _, tf := range p.TargetFields {
		declared[tf.Name] = true
	}

	name := "connectivity.mapping_profile." + p.MappingID + ".v" + strconv.Itoa(p.Version)
	b, err := newBuilder(site, name, name+".source", name+".target")
	if err != nil {
		return Lowered{}, err
	}

	identities := 0
	for _, m := range p.Mappings {
		if m.SourceField == "" || m.TargetField == "" {
			return Lowered{}, refuse(site, FeatureInvalidMapping, m.TargetField,
				"field mapping declares no source or target field")
		}
		if len(p.TargetFields) > 0 && !declared[m.TargetField] {
			return Lowered{}, refuse(site, FeatureInvalidMapping, m.TargetField,
				"field mapping writes %q, which the profile's target fields do not declare", m.TargetField)
		}
		switch {
		case m.Identity || m.TransformationIRDigest == ConnectivityIdentityTransformation:
			if err := b.project(m.TargetField, m.SourceField, transformation.TypeString); err != nil {
				return Lowered{}, err
			}
			identities++
		case m.TransformationIRDigest != "":
			b.delegate(Delegation{Target: m.TargetField, Source: m.SourceField, ProgramDigest: m.TransformationIRDigest})
		default:
			return Lowered{}, refuse(site, FeatureInvalidMapping, m.TargetField,
				"field mapping names neither the identity transformation nor a transformation IR digest")
		}
	}

	b.setLimits(defaultLimitsMapping(identities,
		"connectivity mapping profiles declare no resource bounds of their own"))
	b.carrier(Carrier{
		Target:   "*",
		SiteType: "mapping.Classification",
		IRType:   transformation.TypeString,
		Loses:    "the per-field classification floor: the IR carries no classification, and mapping.Compile's downgrade refusal has no lowered counterpart (XFORM-004's taint package owns classification propagation)",
	})
	b.diverge(Divergence{
		Feature: "required_field",
		Vector:  "a Required field whose source column the row does not carry",
		Site:    "records the field as required; the compiled profile has no executor of its own that enforces it",
		Lowered: "yields an ABSENT property for that target and continues",
	})
	return b.finish()
}

// ConnectivityOp mirrors mapping.Op.
type ConnectivityOp string

// The connectivity mapping IR operations, spelled as mapping.Op spells them.
const (
	ConnectivityOpIdentity ConnectivityOp = "IDENTITY"
	ConnectivityOpTrim     ConnectivityOp = "TRIM"
	ConnectivityOpUpper    ConnectivityOp = "UPPER"
	ConnectivityOpLower    ConnectivityOp = "LOWER"
	ConnectivityOpConstant ConnectivityOp = "CONSTANT"
	ConnectivityOpLookup   ConnectivityOp = "LOOKUP"
	ConnectivityOpDate     ConnectivityOp = "DATE"
	ConnectivityOpMoney    ConnectivityOp = "MONEY"
	ConnectivityOpCompose  ConnectivityOp = "COMPOSE"
)

// ConnectivityNullPolicy mirrors mapping.NullPolicy.
type ConnectivityNullPolicy string

// The connectivity null policies, spelled as mapping.NullPolicy spells them.
const (
	ConnectivityNullError  ConnectivityNullPolicy = "ERROR"
	ConnectivityNullOmit   ConnectivityNullPolicy = "OMIT"
	ConnectivityNullDelete ConnectivityNullPolicy = "DELETE"
)

// ConnectivityRule mirrors mapping.Rule.
type ConnectivityRule struct {
	Source   string
	Target   string
	Op       ConnectivityOp
	Argument string
	Lookup   map[string]string
	Null     ConnectivityNullPolicy
}

// ConnectivityRules mirrors mapping.IR: the executable rule list the
// connectivity package runs with its own interpreter today.
type ConnectivityRules struct {
	Version string
	Rules   []ConnectivityRule
}

// LowerConnectivityRules lowers the connectivity mapping IR onto the shared
// engine. This is the form that matters most for the migration, because it
// is the one connectivity executes with a transform runtime of its own.
func LowerConnectivityRules(rules ConnectivityRules) (Lowered, error) {
	site := SiteConnectivityRules
	if rules.Version == "" {
		return Lowered{}, refuse(site, FeatureInvalidMapping, "", "mapping IR declares no version")
	}
	if len(rules.Rules) == 0 {
		return Lowered{}, refuse(site, FeatureInvalidMapping, rules.Version, "mapping IR declares no rules")
	}
	name := "connectivity.mapping_ir." + rules.Version
	b, err := newBuilder(site, name, name+".source", name+".target")
	if err != nil {
		return Lowered{}, err
	}

	sawDate, sawTrimmingOp, sawUndeclaredNull := false, false, false
	for _, r := range rules.Rules {
		if r.Target == "" {
			return Lowered{}, refuse(site, FeatureInvalidMapping, "", "a rule declares no target")
		}
		null := r.Null
		if null == "" {
			null = ConnectivityNullError // mapping.IR.Validate's own documented default.
			sawUndeclaredNull = true
		}
		switch null {
		case ConnectivityNullError:
		case ConnectivityNullOmit, ConnectivityNullDelete:
			return Lowered{}, refuse(site, FeatureNullPolicy, r.Target,
				"rule declares null policy %s, which changes the output field set (omitting the target, or emitting a delete marker); the IR always produces one property per instruction",
				null)
		default:
			return Lowered{}, refuse(site, FeatureInvalidMapping, r.Target,
				"null policy %q is not a declared connectivity null policy", string(null))
		}

		switch r.Op {
		case ConnectivityOpIdentity:
			if r.Source == "" {
				return Lowered{}, refuse(site, FeatureInvalidMapping, r.Target, "identity rule declares no source")
			}
			if err := b.project(r.Target, r.Source, transformation.TypeString); err != nil {
				return Lowered{}, err
			}
		case ConnectivityOpConstant:
			if err := b.literal(r.Target, r.Argument, transformation.TypeString); err != nil {
				return Lowered{}, err
			}
		case ConnectivityOpDate:
			if r.Source == "" {
				return Lowered{}, refuse(site, FeatureInvalidMapping, r.Target, "date rule declares no source")
			}
			if r.Argument != time.RFC3339 {
				return Lowered{}, refuse(site, FeatureLayoutDateParse, r.Target,
					"rule parses dates under layout %q; the IR's timestamp coercion parses RFC 3339 only and takes no layout parameter", r.Argument)
			}
			if err := b.convert(r.Target, r.Source, transformation.TypeString, transformation.TypeTimestamp); err != nil {
				return Lowered{}, err
			}
			sawDate = true
			sawTrimmingOp = true
		case ConnectivityOpTrim, ConnectivityOpUpper, ConnectivityOpLower:
			return Lowered{}, refuse(site, FeatureStringNormalization, r.Target,
				"rule applies %s; the IR's function vocabulary has no string normalizer", r.Op)
		case ConnectivityOpLookup:
			return Lowered{}, refuse(site, FeatureCrosswalkLookup, r.Target,
				"rule resolves the value through a %d-entry crosswalk; the IR instruction set has no lookup instruction", len(r.Lookup))
		case ConnectivityOpMoney:
			return Lowered{}, refuse(site, FeatureMoneyParse, r.Target,
				"rule parses money in %s (currency-token stripping and group separators); the IR's decimal coercion accepts plain signed decimal text only", r.Argument)
		case ConnectivityOpCompose:
			return Lowered{}, refuse(site, FeatureTemplateCompose, r.Target,
				"rule composes the value into template %q; the IR has no template instruction", r.Argument)
		default:
			return Lowered{}, refuse(site, FeatureInvalidMapping, r.Target,
				"operation %q is not a declared connectivity operation", string(r.Op))
		}
	}

	b.setLimits(defaultLimitsMapping(len(rules.Rules),
		"the connectivity mapping IR declares no resource bounds of its own"))
	b.diverge(Divergence{
		Feature: "missing_source",
		Vector:  "a rule whose source column the input does not carry, under the ERROR null policy",
		Site:    "returns ErrMissingSource and no result at all",
		Lowered: "yields an ABSENT property for that target and completes the row",
	})
	b.diverge(Divergence{
		Feature: "empty_output",
		Vector:  "a rule whose output is the empty string, under the ERROR null policy",
		Site:    "returns ErrTransform and no result at all",
		Lowered: "yields a present, empty-string property",
	})
	if sawUndeclaredNull {
		b.diverge(Divergence{
			Feature: "undeclared_null_policy",
			Vector:  "a rule that declares no null policy at all",
			Site:    "mapping.IR.Validate assigns the ERROR default to its own loop copy, so the rule validates as ERROR while mapping.Execute reads the original empty value and takes neither the ERROR nor the DELETE branch -- the rule executes as if OMIT",
			Lowered: "takes the documented ERROR default as declared, and yields an ABSENT property for the target",
		})
	}
	if sawTrimmingOp {
		b.diverge(Divergence{
			Feature: "whitespace_trim",
			Vector:  "a source value with leading or trailing whitespace",
			Site:    "trims the cell before parsing it",
			Lowered: "parses the value as given, so a padded value fails the coercion instead of being trimmed",
		})
	}
	if sawDate {
		b.diverge(Divergence{
			Feature: "date_layout_scope",
			Vector:  "any DATE rule under a layout other than RFC 3339",
			Site:    "parses under the declared layout",
			Lowered: "refuses at lowering time with " + FeatureLayoutDateParse,
		})
	}
	return b.finish()
}

// defaultLimitsMapping is the declared limits mapping for a site whose
// mapping form carries no resource bounds of its own. The reason is carried
// into the notes so a reader can tell an inherited bound from a defaulted
// one.
func defaultLimitsMapping(operations int, because string) LimitsMapping {
	return LimitsMapping{
		Definition: transformation.ResourceLimits{
			MaxOperations:  operations,
			MaxInputBytes:  DefaultMaxBytes,
			MaxOutputBytes: DefaultMaxBytes,
			MaxExpansion:   1,
		},
		Execution: execLimits{MaxRows: DefaultMaxRows, MaxSteps: operations * DefaultMaxRows, MaxOutputBytes: DefaultMaxBytes},
		Notes: []string{
			"max_input_bytes/max_output_bytes: adapters default (" + strconv.FormatInt(DefaultMaxBytes, 10) + "), because " + because,
			"exec max_rows: adapters default (" + strconv.Itoa(DefaultMaxRows) + "), because " + because,
			fmt.Sprintf("max_operations=%d: one instruction per lowered rule", operations),
			"max_expansion=1: every lowered rule reads at most one source",
		},
	}
}

// DefaultMaxRows is the dataset bound applied when a site's mapping form
// declares none of its own.
const DefaultMaxRows = 10_000
