package hcmctl

import (
	"regexp"
)

// bearerPattern matches an "authorization: Bearer <token>"-shaped substring
// wherever it might appear in a diagnostic (a dial error, a wrapped gRPC
// status message, ...), case-insensitively.
var bearerPattern = regexp.MustCompile(`(?i)bearer\s+\S+`)

// redact scrubs a diagnostic string of anything that looks like a bearer
// credential. hcmctl never prints -token, -mint-key or a minted JIT token
// under any flag or error path; this is the one choke point every output
// path in this package runs through, so a future subcommand cannot forget
// it.
func redact(s string) string {
	return bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
}

// redactError is [redact] applied to an error's message, returned as a
// plain string so a caller cannot accidentally re-wrap the unredacted
// error.Error() value.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	return redact(err.Error())
}
