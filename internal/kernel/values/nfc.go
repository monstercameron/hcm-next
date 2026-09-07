package values

import "golang.org/x/text/unicode/norm"

// NFC returns s in Unicode Normalization Form C. Domain packages normalise
// through this helper so the kernel remains the only owner of the x/text
// dependency (LIB-020); the empty string is returned unchanged.
func NFC(s string) string {
	if s == "" {
		return s
	}
	return norm.NFC.String(s)
}

// IsNFC reports whether s is already in Normalization Form C.
func IsNFC(s string) bool { return norm.NFC.IsNormalString(s) }
