//go:build !(js && wasm)

// This build of the command is a no-op placeholder so that
// `go build ./tools/uxqual/...` succeeds on the native (windows/arm64)
// toolchain. The real entrypoint is main_wasm.go, built only with
// `GOOS=js GOARCH=wasm go build ./tools/uxqual/cmd/uxqualwasm`.
package main

func main() {}
