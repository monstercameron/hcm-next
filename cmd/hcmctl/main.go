// Command hcmctl is the SVC-011/ADMIN-001 operator CLI's composition root.
// internal/transport/admin/hcmctl is the whole implementation - flag
// parsing, JIT token minting, bearer redaction, exactly one AdminService
// call per invocation and its evidence line - as a testable library; this
// file only wires that library to the real process boundary (os.Args,
// os.Stdout, os.Stderr, a real process exit code) and the real gRPC dialer
// (hcmctl.DialInsecure), the same way cmd/migrate's main.go stays a thin
// caller of internal/platform/bootstrap.Run. It contains no business or
// store logic, and it must never grow any: see
// internal/transport/admin/hcmctl's package doc for why this command's
// entire surface lives there instead.
package main

import (
	"os"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin/hcmctl"
)

func main() {
	os.Exit(hcmctl.Main(os.Args[1:], os.Stdout, os.Stderr, hcmctl.DialInsecure))
}
