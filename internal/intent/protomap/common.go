package protomap

import (
	"errors"
	"fmt"
	"math/big"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes. Classify with [errors.Is].
var (
	// ErrNilMessage reports a nil Protobuf message where a value was required.
	// A nil message decodes to an error rather than to a zero value, because a
	// zero value would silently assert something the sender never said.
	ErrNilMessage = errors.New("protomap: nil message")

	// ErrLossyMapping reports a value that cannot cross the wire boundary
	// without losing kind, tenant, revision intent or presence.
	ErrLossyMapping = errors.New("protomap: mapping would lose meaning")

	// ErrUnknownEnum reports an enum number the kernel does not accept,
	// including the four reserved kernel-family numbers.
	ErrUnknownEnum = errors.New("protomap: unknown enum value")
)

func lossy(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrLossyMapping, fmt.Sprintf(format, args...))
}

// EntityIDToProto encodes a tenant-agnostic entity identity.
//
// hcmnext.common.v1 has no separate EntityId message, so a tenant-agnostic
// identity is an EntityRef with an empty tenant. The empty tenant is the
// marker, not an oversight: [EntityRefFromProto] rejects it, so a tenant-scoped
// reference can never be reconstructed from a tenant-agnostic identity by
// accident.
func EntityIDToProto(id values.EntityId) (*commonv1.EntityRef, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return &commonv1.EntityRef{Kind: string(id.Kind), Id: id.Id}, nil
}

// EntityIDFromProto decodes a tenant-agnostic entity identity. A message
// carrying a tenant is rejected: dropping the tenant here is exactly the loss
// this mapping exists to prevent.
func EntityIDFromProto(p *commonv1.EntityRef) (values.EntityId, error) {
	if p == nil {
		return values.EntityId{}, fmt.Errorf("%w: EntityRef", ErrNilMessage)
	}
	if p.GetTenantId() != "" {
		return values.EntityId{}, lossy(
			"message carries tenant %q; decode it as an EntityRef, not an EntityId",
			p.GetTenantId())
	}
	id := values.EntityId{Kind: values.Kind(p.GetKind()), Id: p.GetId()}
	if err := id.Validate(); err != nil {
		return values.EntityId{}, err
	}
	return id, nil
}

// EntityRefToProto encodes a tenant-scoped entity reference.
func EntityRefToProto(r values.EntityRef) (*commonv1.EntityRef, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &commonv1.EntityRef{
		TenantId: string(r.Tenant),
		Kind:     string(r.Kind),
		Id:       r.Id,
	}, nil
}

// EntityRefFromProto decodes a tenant-scoped entity reference. A tenantless
// message is an error: a bare entity id never crosses a tenant boundary.
func EntityRefFromProto(p *commonv1.EntityRef) (values.EntityRef, error) {
	if p == nil {
		return values.EntityRef{}, fmt.Errorf("%w: EntityRef", ErrNilMessage)
	}
	if p.GetTenantId() == "" {
		return values.EntityRef{}, lossy("EntityRef %q/%q carries no tenant",
			p.GetKind(), p.GetId())
	}
	r := values.EntityRef{
		Tenant: values.TenantId(p.GetTenantId()),
		Kind:   values.Kind(p.GetKind()),
		Id:     p.GetId(),
	}
	if err := r.Validate(); err != nil {
		return values.EntityRef{}, err
	}
	return r, nil
}

// ResourceKeyToProto encodes a resource key.
//
// The ordered segments travel as the key's canonical text in key_bytes rather
// than as a joined display string: the canonical encoding already escapes every
// separator, so a segment containing a slash or a colon cannot inject an extra
// path element.
func ResourceKeyToProto(k values.ResourceKey, organizationScopeID, orderingClass string, orderingVersion uint32) (*commonv1.ResourceKey, error) {
	canonical := k.Canonical()
	if canonical == nil {
		if err := k.Validate(); err != nil {
			return nil, err
		}
		return nil, lossy("resource key has no canonical encoding")
	}
	return &commonv1.ResourceKey{
		ResourceType:        string(k.ResourceType),
		TenantId:            string(k.Tenant),
		OrganizationScopeId: organizationScopeID,
		KeyBytes:            canonical,
		OrderingClass:       orderingClass,
		OrderingVersion:     orderingVersion,
	}, nil
}

// ResourceKeyFromProto decodes a resource key and checks that the redundant
// tenant and resource-type fields agree with the canonical bytes. A message
// whose scalar fields disagree with its canonical bytes is rejected rather than
// resolved in favour of one of them.
func ResourceKeyFromProto(p *commonv1.ResourceKey) (values.ResourceKey, error) {
	if p == nil {
		return values.ResourceKey{}, fmt.Errorf("%w: ResourceKey", ErrNilMessage)
	}
	if p.GetTenantId() == "" {
		return values.ResourceKey{}, lossy("resource key carries no tenant")
	}
	var k values.ResourceKey
	if err := k.UnmarshalText(p.GetKeyBytes()); err != nil {
		return values.ResourceKey{}, err
	}
	if string(k.Tenant) != p.GetTenantId() {
		return values.ResourceKey{}, lossy(
			"resource key tenant field %q disagrees with its canonical bytes %q",
			p.GetTenantId(), string(k.Tenant))
	}
	if string(k.ResourceType) != p.GetResourceType() {
		return values.ResourceKey{}, lossy(
			"resource key type field %q disagrees with its canonical bytes %q",
			p.GetResourceType(), string(k.ResourceType))
	}
	return k, nil
}

// RevisionTokenToProto encodes a revision token.
//
// The three token shapes are distinguished without an extra discriminator
// field: unspecified carries no source authority, a sequence token carries a
// source authority and no CAS bytes, and an opaque token carries both. That is
// why sequence zero and "no revision expectation" cannot be confused.
func RevisionTokenToProto(t values.RevisionToken, entity *commonv1.EntityRef, issuedAt values.Instant) (*commonv1.RevisionToken, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	out := &commonv1.RevisionToken{Entity: entity}
	if issuedAt.IsSet() {
		out.IssuedAt = timestamppb.New(issuedAt.Time())
	}
	if !t.IsSpecified() {
		return out, nil
	}
	out.SourceAuthorityId = t.Stream()
	if seq, ok := t.Sequence(); ok {
		out.Revision = seq
		return out, nil
	}
	cas, ok := t.Opaque()
	if !ok {
		return nil, lossy("specified revision token carries neither a sequence nor CAS bytes")
	}
	out.OpaqueCasBytes = cas
	return out, nil
}

// RevisionTokenFromProto decodes a revision token.
func RevisionTokenFromProto(p *commonv1.RevisionToken) (values.RevisionToken, error) {
	if p == nil {
		return values.RevisionToken{}, fmt.Errorf("%w: RevisionToken", ErrNilMessage)
	}
	stream := p.GetSourceAuthorityId()
	if stream == "" {
		if p.GetRevision() != 0 || len(p.GetOpaqueCasBytes()) > 0 {
			return values.RevisionToken{}, lossy(
				"revision token names a revision but no source authority")
		}
		return values.UnspecifiedRevision(), nil
	}
	if cas := p.GetOpaqueCasBytes(); len(cas) > 0 {
		if p.GetRevision() != 0 {
			return values.RevisionToken{}, lossy(
				"revision token carries both a sequence and CAS bytes")
		}
		return values.NewOpaqueRevision(stream, cas)
	}
	return values.NewSequenceRevision(stream, p.GetRevision())
}

// DecimalToProto encodes a fixed decimal as sign, big-endian unsigned magnitude
// and scale. There is no float anywhere on this path.
func DecimalToProto(d values.Decimal) (*commonv1.Decimal, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	sign := commonv1.DecimalSign_DECIMAL_SIGN_ZERO
	switch d.Sign() {
	case 1:
		sign = commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE
	case -1:
		sign = commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE
	}
	unscaled := d.Unscaled()
	magnitude := new(big.Int).Abs(unscaled).Bytes()
	return &commonv1.Decimal{
		Sign:              sign,
		UnscaledMagnitude: magnitude,
		Scale:             d.Scale(),
	}, nil
}

// DecimalFromProto decodes a fixed decimal. Rounding mode is not on the wire —
// it governs how a value was produced, not what it is — so the caller supplies
// the mode the decoded value should carry.
func DecimalFromProto(p *commonv1.Decimal, mode values.RoundingMode) (values.Decimal, error) {
	if p == nil {
		return values.Decimal{}, fmt.Errorf("%w: Decimal", ErrNilMessage)
	}
	magnitude := new(big.Int).SetBytes(p.GetUnscaledMagnitude())
	switch p.GetSign() {
	case commonv1.DecimalSign_DECIMAL_SIGN_ZERO:
		if magnitude.Sign() != 0 {
			return values.Decimal{}, lossy("decimal is signed ZERO but carries magnitude %s", magnitude)
		}
	case commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE:
		if magnitude.Sign() == 0 {
			return values.Decimal{}, lossy("decimal is signed POSITIVE but has zero magnitude")
		}
	case commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE:
		if magnitude.Sign() == 0 {
			return values.Decimal{}, lossy("decimal is signed NEGATIVE but has zero magnitude")
		}
		magnitude.Neg(magnitude)
	default:
		return values.Decimal{}, fmt.Errorf("%w: DecimalSign(%d)", ErrUnknownEnum, p.GetSign())
	}
	text := decimalText(magnitude, p.GetScale())
	return values.NewDecimal(text, p.GetScale(), mode)
}

// decimalText renders a signed unscaled integer and a scale as a decimal
// literal, which is the form values.NewDecimal parses.
func decimalText(unscaled *big.Int, scale int32) string {
	digits := new(big.Int).Abs(unscaled).String()
	negative := unscaled.Sign() < 0
	if scale <= 0 {
		out := digits
		for i := int32(0); i < -scale; i++ {
			out += "0"
		}
		if negative {
			return "-" + out
		}
		return out
	}
	for int32(len(digits)) <= scale {
		digits = "0" + digits
	}
	cut := int32(len(digits)) - scale
	out := digits[:cut] + "." + digits[cut:]
	if negative {
		return "-" + out
	}
	return out
}

// MoneyToProto encodes an amount plus its uppercase ISO-4217 code.
func MoneyToProto(m values.Money) (*commonv1.Money, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	amount, err := DecimalToProto(m.Amount())
	if err != nil {
		return nil, err
	}
	return &commonv1.Money{Amount: amount, CurrencyCode: m.Currency()}, nil
}

// MoneyFromProto decodes an amount and currency.
func MoneyFromProto(p *commonv1.Money, mode values.RoundingMode) (values.Money, error) {
	if p == nil {
		return values.Money{}, fmt.Errorf("%w: Money", ErrNilMessage)
	}
	amount, err := DecimalFromProto(p.GetAmount(), mode)
	if err != nil {
		return values.Money{}, err
	}
	return values.NewMoneyFromDecimal(amount, p.GetCurrencyCode())
}

// InstantToProto encodes an instant. An unset instant encodes as nil rather
// than as the epoch: "no time" and "1970" are different assertions.
func InstantToProto(i values.Instant) *timestamppb.Timestamp {
	if !i.IsSet() {
		return nil
	}
	return timestamppb.New(i.Time())
}

// InstantFromProto decodes an instant. A nil message decodes to the unset
// instant, which is the inverse of [InstantToProto].
func InstantFromProto(p *timestamppb.Timestamp) (values.Instant, error) {
	if p == nil {
		return values.Instant{}, nil
	}
	return values.NewInstantFromUnix(p.GetSeconds(), p.GetNanos())
}

// PresenceToProto encodes a presence state.
func PresenceToProto(s values.PresenceState) (commonv1.Presence, error) {
	switch s {
	case values.PresenceAbsent:
		return commonv1.Presence_PRESENCE_ABSENT, nil
	case values.PresenceNull:
		return commonv1.Presence_PRESENCE_NULL, nil
	case values.PresenceUnknown:
		return commonv1.Presence_PRESENCE_UNKNOWN, nil
	case values.PresenceRedacted:
		return commonv1.Presence_PRESENCE_REDACTED, nil
	case values.PresenceUnavailable:
		return commonv1.Presence_PRESENCE_UNAVAILABLE, nil
	case values.PresenceNotApplicable:
		return commonv1.Presence_PRESENCE_NOT_APPLICABLE, nil
	case values.PresenceValue:
		return commonv1.Presence_PRESENCE_VALUE, nil
	default:
		return commonv1.Presence_PRESENCE_UNSPECIFIED,
			fmt.Errorf("%w: PresenceState(%d) is never a valid wire assertion", ErrUnknownEnum, s)
	}
}

// PresenceFromProto decodes a presence state. UNSPECIFIED is an error: it is
// never a valid wire assertion, and decoding it to a zero value would collapse
// exactly the distinction presence exists to keep.
func PresenceFromProto(p commonv1.Presence) (values.PresenceState, error) {
	switch p {
	case commonv1.Presence_PRESENCE_ABSENT:
		return values.PresenceAbsent, nil
	case commonv1.Presence_PRESENCE_NULL:
		return values.PresenceNull, nil
	case commonv1.Presence_PRESENCE_UNKNOWN:
		return values.PresenceUnknown, nil
	case commonv1.Presence_PRESENCE_REDACTED:
		return values.PresenceRedacted, nil
	case commonv1.Presence_PRESENCE_UNAVAILABLE:
		return values.PresenceUnavailable, nil
	case commonv1.Presence_PRESENCE_NOT_APPLICABLE:
		return values.PresenceNotApplicable, nil
	case commonv1.Presence_PRESENCE_VALUE:
		return values.PresenceValue, nil
	default:
		return values.PresenceUnspecified,
			fmt.Errorf("%w: Presence(%d)", ErrUnknownEnum, p)
	}
}
