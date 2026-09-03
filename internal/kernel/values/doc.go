// Package values holds the canonical kernel value types that every HCM Next
// domain shares: entity identifiers and references, explicit property presence,
// fixed-precision decimals and money, business-time primitives, and the
// timezone/business-calendar dataset versioning that future timers carry.
//
// Semantic owner: kernel. Phase: P1A.
//
// The package is deliberately dependency-light and domain-free. Domains may add
// constraints on top of these types; they may not redefine interval boundaries,
// decimal rounding, presence states or canonical encodings, and they may not
// introduce domain-specific string aliases for the identifiers defined here.
//
// Two rules govern everything in this package:
//
//  1. Every value has exactly one canonical byte encoding, exposed as
//     Canonical() []byte, and that encoding is stable across processes,
//     architectures, locales and Go map iteration order. Canonical() returns
//     nil for a value that fails Validate; it never returns a partial encoding.
//  2. Nothing is inferred. Missing tenant, missing currency, missing timezone,
//     missing rounding mode, ambiguous local time and unspecified revision are
//     all errors or explicit states, never silent defaults.
//
// Material financial values never use binary floating point; see Decimal.
package values
