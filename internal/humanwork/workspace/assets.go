package workspace

import (
	"embed"
	"errors"
	"io/fs"
)

// Asset file names the progressive-enhancement bundle is looked for under.
const (
	assetWasm     = "uxqual.wasm"
	assetWasmExec = "wasm_exec.js"
)

// assetsFS is the embedded bundle directory.
//
// It is embedded with the "all:" prefix so the directory can be committed
// containing only its marker file: a stock checkout carries no bundle, and
// the absence is a served 404 rather than a build failure. See this package's
// doc comment for the two commands that populate it.
//
//go:embed all:assets
var assetsFS embed.FS

// asset returns one embedded bundle file.
func asset(name string) ([]byte, bool) {
	switch name {
	case assetWasm, assetWasmExec:
	default:
		return nil, false
	}
	body, err := fs.ReadFile(assetsFS, "assets/"+name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, false
		}
		return nil, false
	}
	return body, true
}

// assetContentType is the media type one bundle file is served under. Both
// are named explicitly: sniffing a wasm module would produce
// application/octet-stream, which instantiateStreaming refuses.
func assetContentType(name string) string {
	if name == assetWasm {
		return "application/wasm"
	}
	return "text/javascript; charset=utf-8"
}

// BundleBuilt reports whether both halves of the progressive-enhancement
// bundle are embedded in this build. When it is false the workspace serves
// the server-rendered document only, which is fully functional on its own.
func BundleBuilt() bool {
	_, wasm := asset(assetWasm)
	_, shim := asset(assetWasmExec)
	return wasm && shim
}
