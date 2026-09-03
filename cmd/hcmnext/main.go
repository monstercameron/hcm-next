// Command hcmnext is a composition root. It wires packages; it owns no semantics.
package main

import (
	"fmt"
	"os"

	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
)

func main() {
	info := buildinfo.Current()
	fmt.Fprintf(os.Stdout, "hcmnext %s revision=%s modified=%t go=%s\n", info.Module, info.Revision, info.Modified, info.GoVersion)
}
