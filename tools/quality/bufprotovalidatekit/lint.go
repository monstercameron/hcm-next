package bufprotovalidatekit

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	versionedPackage = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)*\.v[1-9][0-9]*$`)
	snakeCaseField   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
)

// Diagnostic is a stable owned projection of the narrow schema lint rules
// required by the offline fallback. It is not a replacement for all Buf rules.
type Diagnostic struct {
	Code        string `json:"code"`
	Descriptor  string `json:"descriptor"`
	Description string `json:"description"`
}

// LintFile applies a deterministic subset of Buf STANDARD rules needed by the
// qualification seam when the pinned developer binary is unavailable.
func LintFile(file protoreflect.FileDescriptor) []Diagnostic {
	if file == nil {
		return []Diagnostic{{Code: "FILE_REQUIRED", Descriptor: "_file", Description: "file descriptor is unavailable"}}
	}
	diagnostics := make([]Diagnostic, 0)
	add := func(code string, descriptor protoreflect.FullName, description string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Descriptor: string(descriptor), Description: description})
	}
	if file.Syntax() != protoreflect.Proto3 {
		add("SYNTAX_PROTO3", file.Package(), "schema syntax must be proto3")
	}
	if !versionedPackage.MatchString(string(file.Package())) {
		add("PACKAGE_VERSION_SUFFIX", file.Package(), "package must end in a positive vN suffix")
	}
	if strings.TrimSpace(file.Options().ProtoReflect().Get(file.Options().ProtoReflect().Descriptor().Fields().ByName("go_package")).String()) == "" {
		add("FILE_OPTION_GO_PACKAGE", file.Package(), "go_package option is required")
	}
	for i := 0; i < file.Messages().Len(); i++ {
		message := file.Messages().Get(i)
		seenNumbers := make(map[protoreflect.FieldNumber]struct{}, message.Fields().Len())
		for j := 0; j < message.Fields().Len(); j++ {
			field := message.Fields().Get(j)
			if !snakeCaseField.MatchString(string(field.Name())) {
				add("FIELD_LOWER_SNAKE_CASE", field.FullName(), "field name must use lower_snake_case")
			}
			if field.Number() <= 0 {
				add("FIELD_NUMBER_POSITIVE", field.FullName(), "field number must be positive")
			}
			if _, duplicate := seenNumbers[field.Number()]; duplicate {
				add("FIELD_NUMBER_UNIQUE", field.FullName(), fmt.Sprintf("field number %d is reused", field.Number()))
			}
			seenNumbers[field.Number()] = struct{}{}
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Descriptor != diagnostics[j].Descriptor {
			return diagnostics[i].Descriptor < diagnostics[j].Descriptor
		}
		return diagnostics[i].Code < diagnostics[j].Code
	})
	return diagnostics
}
