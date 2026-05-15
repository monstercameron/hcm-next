package blockshared

import "strings"

// StringValue dereferences optional strings for normalization and comparisons.
func StringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// NormalizedEmail lowercases and trims optional email values.
func NormalizedEmail(value *string) string {
	if value == nil {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(*value))
}

// PhoneDigitCount returns the count of numeric characters in a phone value.
func PhoneDigitCount(value string) int {
	return len(DigitsOnly(value))
}

// DigitsOnly removes non-ASCII digits from a string.
func DigitsOnly(value string) string {
	var builder strings.Builder

	for _, runeValue := range value {
		if runeValue >= '0' && runeValue <= '9' {
			builder.WriteRune(runeValue)
		}
	}

	return builder.String()
}
