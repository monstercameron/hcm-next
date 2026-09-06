//go:build !(js && wasm)

// This build of the command is its own build tool.
//
// The journey page's client only exists as a wasm module, and the shell that
// loads it names the command that produces one:
//
//	go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets
//
// That sentence is printed in the served document when the bundle is
// missing, so it has to be a command that works rather than a note pointing
// at a two-line shell recipe. It builds main_wasm.go with GOOS=js
// GOARCH=wasm, copies the matching wasm_exec.js out of the toolchain that
// built it, and prints what it wrote.
//
// Both halves come from one toolchain on purpose: wasm_exec.js is the Go
// runtime's own JavaScript shim and is versioned with the compiler, so a
// shim copied from a different Go than the one that produced the module is a
// page that fails at instantiation with an error nobody can read.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// wasmPackage is what gets built. It is the module-absolute import path
// rather than "./tools/..." so the command works from any directory inside
// the module, including the package's own directory under `go test`.
const wasmPackage = "github.com/monstercameron/hcm-next/tools/uxqual/cmd/journeywasm"

// Output file names. They are the names internal/humanwork/workspace's
// embedded asset directory serves (assets.go's assetJourneyWasm and
// assetWasmExec), and the shell's script tags point at both.
const (
	wasmFile     = "journey.wasm"
	wasmExecFile = "wasm_exec.js"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its process boundary handed in, so the whole command is
// testable.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("journeywasm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "directory to write "+wasmFile+" and "+wasmExecFile+" into (required)")
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: go run ./tools/uxqual/cmd/journeywasm -out <dir>\n\n"+
			"Builds the Promotion journey page's wasm client and the matching Go\n"+
			"wasm_exec.js shim into <dir>.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*out) == "" {
		fmt.Fprintln(stderr, "journeywasm: -out is required")
		flags.Usage()
		return 2
	}
	if err := build(*out, stdout); err != nil {
		fmt.Fprintf(stderr, "journeywasm: %v\n", err)
		return 1
	}
	return 0
}

// build writes both halves of the bundle into outDir.
func build(outDir string, stdout io.Writer) error {
	goBin, err := goBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	wasmPath := filepath.Join(outDir, wasmFile)
	cmd := exec.Command(goBin, "build", "-o", wasmPath, wasmPackage)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	// The build's own diagnostics are the useful part of a failure, so they
	// are carried into the error rather than discarded.
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("building %s for js/wasm: %w\n%s", wasmPackage, err, output)
		}
		return fmt.Errorf("building %s for js/wasm: %w", wasmPackage, err)
	}

	shimSource, err := wasmExecSource(goBin)
	if err != nil {
		return err
	}
	shimPath := filepath.Join(outDir, wasmExecFile)
	if err := copyFile(shimSource, shimPath); err != nil {
		return fmt.Errorf("copying %s: %w", wasmExecFile, err)
	}

	for _, path := range []string{wasmPath, shimPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return fmt.Errorf("stat %s: %w", path, statErr)
		}
		fmt.Fprintf(stdout, "%s  %s\n", path, humanSize(info.Size()))
	}
	return nil
}

// goBinary finds the toolchain to build with.
//
// It is the go on PATH: the one the person typing `go run` is using, and
// therefore the one whose wasm_exec.js matches the module this build
// produces. runtime.GOROOT is deliberately not consulted -- it is deprecated
// precisely because it describes the machine a binary was built on rather
// than the one it is running on, and `go env GOROOT` (below) asks the
// toolchain itself, which is always right.
func goBinary() (string, error) {
	path, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("no go toolchain found on PATH: %w", err)
	}
	return path, nil
}

// wasmExecSource locates the shim inside the toolchain that just built the
// module. Go 1.24 moved it from misc/wasm to lib/wasm; both are looked for
// so this command is not pinned to one toolchain layout.
func wasmExecSource(goBin string) (string, error) {
	root, err := goRoot(goBin)
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(root, "lib", "wasm", wasmExecFile),
		filepath.Join(root, "misc", "wasm", wasmExecFile),
	}
	for _, candidate := range candidates {
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in %s (looked in lib/wasm and misc/wasm)", wasmExecFile, root)
}

// goRoot asks the toolchain where it lives, rather than assuming this
// process and the build share one.
func goRoot(goBin string) (string, error) {
	output, err := exec.Command(goBin, "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("asking %s for GOROOT: %w", goBin, err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", fmt.Errorf("%s reported an empty GOROOT", goBin)
	}
	return root, nil
}

// copyFile writes source to destination, replacing whatever was there.
func copyFile(source, destination string) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, body, 0o644)
}

// humanSize renders a byte count the way a person reads a bundle size, with
// the exact count kept alongside it: "12.4 MB (13029312 bytes)". The exact
// figure is what a size regression is noticed by.
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + " bytes"
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB"}
	chosen := units[0]
	for _, name := range units {
		value /= unit
		chosen = name
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s (%d bytes)", value, chosen, size)
}
