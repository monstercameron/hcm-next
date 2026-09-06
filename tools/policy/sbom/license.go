package sbom

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/module"
)

// LicenseEvidenceReader is the file-system port used by license resolution.
// Keeping the port narrow lets tests provide a fixture module cache without
// changing the resolver's production behavior.
type LicenseEvidenceReader interface {
	ReadFile(name string) ([]byte, error)
}

// FileReader is a short compatibility name for LicenseEvidenceReader.
type FileReader = LicenseEvidenceReader

// OSFileReader reads module-owned evidence from the operating system.
type OSFileReader struct{}

// ReadFile implements LicenseEvidenceReader.
func (OSFileReader) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// LicenseResolver resolves module-owned license declarations from a module
// directory. It never assigns a license based on the module path, publisher,
// or a permissive default.
type LicenseResolver struct {
	Reader      LicenseEvidenceReader
	ModuleCache string
}

// NewLicenseResolver constructs a resolver for a module cache. A nil reader
// uses OSFileReader. ModuleCache is required only when resolving a dependency
// rather than the root module directory.
func NewLicenseResolver(reader LicenseEvidenceReader, moduleCache string) LicenseResolver {
	if reader == nil {
		reader = OSFileReader{}
	}
	return LicenseResolver{Reader: reader, ModuleCache: moduleCache}
}

// Resolve resolves the root module when rootDir is non-empty; otherwise it
// reads the exact module@version directory in ModuleCache. A missing or
// unrecognized declaration returns UNKNOWN without guessing.
func (r LicenseResolver) Resolve(modulePath, version, rootDir string) (LicenseEvidence, error) {
	if r.Reader == nil {
		r.Reader = OSFileReader{}
	}
	dir := rootDir
	if dir == "" {
		if r.ModuleCache == "" {
			return LicenseEvidence{Expression: UnknownLicense}, nil
		}
		escaped, err := module.EscapePath(modulePath)
		if err != nil {
			return LicenseEvidence{}, fmt.Errorf("sbom: escaping module %s: %w", modulePath, err)
		}
		dir = filepath.Join(r.ModuleCache, escaped+"@"+version)
	}
	return resolveLicenseInDir(r.Reader, dir)
}

// ResolveLicenseEvidence reads one module directory through the supplied
// file port. It is useful for adapters that already know the module-cache
// directory and for deterministic unit tests.
func ResolveLicenseEvidence(reader LicenseEvidenceReader, moduleDir string) (LicenseEvidence, error) {
	return resolveLicenseInDir(reader, moduleDir)
}

// ResolveLicense is the concise form of ResolveLicenseEvidence.
func ResolveLicense(reader LicenseEvidenceReader, moduleDir string) (LicenseEvidence, error) {
	return ResolveLicenseEvidence(reader, moduleDir)
}

var licenseEvidenceFiles = []string{
	"LICENSE",
	"LICENSE.txt",
	"LICENSE.md",
	"LICENSE-MIT",
	"LICENSE-APACHE",
	"LICENCE",
	"LICENCE.txt",
	"COPYING",
	"COPYING.txt",
}

func resolveLicenseInDir(reader LicenseEvidenceReader, dir string) (LicenseEvidence, error) {
	if reader == nil {
		reader = OSFileReader{}
	}

	// An explicit module-owned declaration wins over a license text file.
	if data, err := reader.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		if expression := declaredSPDXExpression(string(data), true); expression != "" {
			return LicenseEvidence{Expression: expression, Source: "go.mod"}, nil
		}
	} else if !isMissingFile(err) {
		return LicenseEvidence{}, fmt.Errorf("sbom: reading %s: %w", filepath.Join(dir, "go.mod"), err)
	}

	found := ""
	for _, name := range licenseEvidenceFiles {
		path := filepath.Join(dir, name)
		data, err := reader.ReadFile(path)
		if err != nil {
			if isMissingFile(err) {
				continue
			}
			return LicenseEvidence{}, fmt.Errorf("sbom: reading %s: %w", path, err)
		}
		if found == "" {
			found = name
		}
		if expression := declaredSPDXExpression(string(data), false); expression != "" {
			return LicenseEvidence{Expression: expression, Source: name}, nil
		}
		if expression := recognizedLicenseText(string(data)); expression != "" {
			return LicenseEvidence{Expression: expression, Source: name}, nil
		}
	}
	if found != "" {
		return LicenseEvidence{Expression: UnknownLicense, Source: found}, nil
	}
	return LicenseEvidence{Expression: UnknownLicense}, nil
}

func isMissingFile(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist)
}

// declaredSPDXExpression accepts only an explicit SPDX marker or a narrowly
// shaped license declaration. It intentionally does not derive a license
// from arbitrary words in go.mod or a license file.
func declaredSPDXExpression(text string, goMod bool) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(line)
		const marker = "SPDX-LICENSE-IDENTIFIER:"
		if i := strings.Index(upper, marker); i >= 0 {
			if expression := cleanSPDXExpression(line[i+len(marker):]); validSPDXExpression(expression) {
				return expression
			}
		}

		lower := strings.ToLower(line)
		if goMod || strings.HasPrefix(lower, "license") || strings.HasPrefix(lower, "licence") {
			if expression := licenseDeclarationValue(line); validSPDXExpression(expression) {
				return expression
			}
		}
	}
	return ""
}

func licenseDeclarationValue(line string) string {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{"//", "#", ";"} {
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	lower := strings.ToLower(line)
	for _, key := range []string{"license", "licenses", "licence", "licences"} {
		if !strings.HasPrefix(lower, key) {
			continue
		}
		rest := strings.TrimSpace(line[len(key):])
		rest = strings.TrimSpace(strings.TrimLeft(rest, ":="))
		rest = strings.TrimSpace(rest)
		if len(rest) >= 2 && ((rest[0] == '"' && rest[len(rest)-1] == '"') || (rest[0] == '\'' && rest[len(rest)-1] == '\'')) {
			rest = rest[1 : len(rest)-1]
		}
		return cleanSPDXExpression(rest)
	}
	return ""
}

func cleanSPDXExpression(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.Index(value, "//"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	if i := strings.IndexByte(value, '#'); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value
}

// recognizedLicenseText recognizes canonical license headings and notices.
// These are evidence classifications, not module-path guesses; an unknown
// text remains UNKNOWN.
func recognizedLicenseText(text string) string {
	upper := strings.ToUpper(text)
	switch {
	case strings.Contains(upper, "GNU AFFERO GENERAL PUBLIC LICENSE"):
		if strings.Contains(upper, "VERSION 3") {
			return "AGPL-3.0"
		}
	case strings.Contains(upper, "SERVER SIDE PUBLIC LICENSE"):
		return "SSPL-1.0"
	case strings.Contains(upper, "GNU GENERAL PUBLIC LICENSE"):
		if strings.Contains(upper, "VERSION 3") {
			return "GPL-3.0"
		}
		if strings.Contains(upper, "VERSION 2") {
			return "GPL-2.0"
		}
	case strings.Contains(upper, "MOZILLA PUBLIC LICENSE") && strings.Contains(text, "2.0"):
		return "MPL-2.0"
	case strings.Contains(upper, "PUBLIC DOMAIN"):
		return "Public-Domain"
	case strings.Contains(upper, "APACHE LICENSE") && strings.Contains(text, "2.0"):
		return "Apache-2.0"
	case strings.Contains(upper, "ISC LICENSE") || strings.Contains(upper, "PERMISSION TO USE, COPY, MODIFY, AND/OR DISTRIBUTE"):
		return "ISC"
	case strings.Contains(upper, "REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS"):
		if strings.Contains(upper, "NEITHER THE NAME") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	case strings.Contains(upper, "MIT LICENSE") || strings.Contains(text, "Permission is hereby granted, free of charge"):
		return "MIT"
	}
	return ""
}

type licenseToken struct {
	kind byte
	text string
}

// validSPDXExpression validates the small SPDX expression grammar without
// pretending that an undeclared identifier is a known license. The source
// declaration supplies identity; this parser only prevents malformed values
// from being emitted as expressions.
func validSPDXExpression(expression string) bool {
	tokens, ok := tokenizeSPDX(expression)
	if !ok || len(tokens) == 0 {
		return false
	}
	p := spdxParser{tokens: tokens}
	if !p.parseOr() || p.pos != len(tokens) {
		return false
	}
	return true
}

func tokenizeSPDX(expression string) ([]licenseToken, bool) {
	var tokens []licenseToken
	for i := 0; i < len(expression); {
		switch expression[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '(':
			tokens = append(tokens, licenseToken{kind: '(', text: "("})
			i++
		case ')':
			tokens = append(tokens, licenseToken{kind: ')', text: ")"})
			i++
		default:
			start := i
			for i < len(expression) && !bytes.ContainsRune([]byte(" \t\r\n()"), rune(expression[i])) {
				if !isSPDXIdentifierByte(expression[i]) {
					return nil, false
				}
				i++
			}
			if start == i {
				return nil, false
			}
			word := expression[start:i]
			tokens = append(tokens, licenseToken{kind: 'i', text: word})
		}
	}
	return tokens, true
}

func isSPDXIdentifierByte(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || strings.ContainsRune(".-+:", rune(b))
}

type spdxParser struct {
	tokens []licenseToken
	pos    int
}

func (p *spdxParser) parseOr() bool {
	if !p.parseAnd() {
		return false
	}
	for p.acceptWord("OR") {
		if !p.parseAnd() {
			return false
		}
	}
	return true
}

func (p *spdxParser) parseAnd() bool {
	if !p.parseWith() {
		return false
	}
	for p.acceptWord("AND") {
		if !p.parseWith() {
			return false
		}
	}
	return true
}

func (p *spdxParser) parseWith() bool {
	if !p.parsePrimary() {
		return false
	}
	if p.acceptWord("WITH") {
		return p.acceptIdentifier()
	}
	return true
}

func (p *spdxParser) parsePrimary() bool {
	if p.pos >= len(p.tokens) {
		return false
	}
	if p.tokens[p.pos].kind == '(' {
		p.pos++
		if !p.parseOr() || p.pos >= len(p.tokens) || p.tokens[p.pos].kind != ')' {
			return false
		}
		p.pos++
		return true
	}
	return p.acceptIdentifier()
}

func (p *spdxParser) acceptWord(want string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == 'i' && p.tokens[p.pos].text == want {
		p.pos++
		return true
	}
	return false
}

func (p *spdxParser) acceptIdentifier() bool {
	if p.pos >= len(p.tokens) || p.tokens[p.pos].kind != 'i' {
		return false
	}
	word := p.tokens[p.pos].text
	if word == "AND" || word == "OR" || word == "WITH" {
		return false
	}
	p.pos++
	return true
}

// LicenseCounts returns deterministic counts for dependency components.
func LicenseCounts(doc *Document) map[string]int {
	counts := make(map[string]int)
	if doc == nil {
		return counts
	}
	for _, component := range doc.Components {
		license := component.License
		if license == "" {
			license = UnknownLicense
		}
		counts[license]++
	}
	return counts
}

// FormatLicenseCounts renders counts in SPDX/UNKNOWN lexical order for CLI
// diagnostics and machine-log comparison.
func FormatLicenseCounts(doc *Document) string {
	counts := LicenseCounts(doc)
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func defaultModuleCache() (string, error) {
	if cache := os.Getenv("GOMODCACHE"); cache != "" {
		return cache, nil
	}
	cmd := exec.Command("go", "env", "GOMODCACHE")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sbom: locating module cache: %w", err)
	}
	cache := strings.TrimSpace(string(output))
	if cache == "" {
		return "", errors.New("sbom: go env GOMODCACHE returned an empty path")
	}
	return cache, nil
}
