// Package buildinfo exposes the identity of the running binary. It is the only
// package every command shares; it owns no business behavior.
package buildinfo

import "runtime/debug"

// Info describes the build that produced the running process.
type Info struct {
	Module    string
	Revision  string
	Modified  bool
	GoVersion string
}

// Current returns the build identity embedded by the Go toolchain.
func Current() Info {
	info := Info{Module: "github.com/monstercameron/hcm-next"}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.GoVersion = bi.GoVersion
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}
