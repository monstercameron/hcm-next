// Package links validates local Markdown links and heading anchors across
// planning/**/*.md and README.md (GOV-004).
package links

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// LinkIssue represents a broken or invalid link.
type LinkIssue struct {
	File   string
	Line   int
	Link   string
	Reason string
}

var (
	markdownLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	anchorNameRe   = regexp.MustCompile(`<a\s+name\s*=\s*"([^"]+)"`)
	anchorBraceRe  = regexp.MustCompile(`\{#([A-Za-z0-9_-]+)\}`)
)

// CheckPlanningLinks validates all markdown links in the planning directory and README.md.
// Returns a slice of issues found.
func CheckPlanningLinks(rootPath string) ([]LinkIssue, error) {
	var issues []LinkIssue

	markdownFiles, err := findMarkdownFiles(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find markdown files: %w", err)
	}

	for _, markdownFile := range markdownFiles {
		fileIssues, err := checkFile(markdownFile)
		if err != nil {
			return nil, fmt.Errorf("error checking file %s: %w", markdownFile, err)
		}
		issues = append(issues, fileIssues...)
	}

	return issues, nil
}

// findMarkdownFiles finds all .md files in planning/ and README.md at the root.
func findMarkdownFiles(rootPath string) ([]string, error) {
	var files []string

	planningDir := filepath.Join(rootPath, "planning")
	err := filepath.Walk(planningDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	readmePath := filepath.Join(rootPath, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		files = append(files, readmePath)
	}

	return files, nil
}

// checkFile checks a single markdown file for broken links. Links inside
// fenced code blocks (``` ... ```) are ignored.
func checkFile(filePath string) ([]LinkIssue, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var issues []LinkIssue
	lines := strings.Split(string(content), "\n")

	inCodeBlock := false
	for i, line := range lines {
		lineNum := i + 1

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		for _, linkMatch := range findMarkdownLinks(line) {
			text, path, anchor := parseLink(linkMatch)
			if text == "" || path == "" {
				continue
			}
			// CommonMark allows the destination to be wrapped in angle
			// brackets (`[t](<https://x/(y)>)`), which is how URLs that
			// contain parentheses are written; the brackets are not part of
			// the destination.
			path = strings.TrimSuffix(strings.TrimPrefix(path, "<"), ">")

			if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "mailto:") {
				continue
			}

			if issue := checkLink(filePath, path, anchor, lineNum); issue != nil {
				issues = append(issues, *issue)
			}
		}
	}

	return issues, nil
}

// findMarkdownLinks finds all inline markdown link patterns [text](url) in a line.
// Reference-style links ([text][ref] / [ref]: url) do not currently occur in
// the planning corpus; none are emitted by this scanner.
func findMarkdownLinks(line string) []string {
	return markdownLinkRe.FindAllString(line, -1)
}

// parseLink extracts text, path, and anchor from a markdown link.
func parseLink(link string) (text, path, anchor string) {
	matches := markdownLinkRe.FindStringSubmatch(link)
	if len(matches) != 3 {
		return "", "", ""
	}

	text = matches[1]
	fullPath := matches[2]

	if idx := strings.Index(fullPath, "#"); idx != -1 {
		path = fullPath[:idx]
		anchor = fullPath[idx+1:]
	} else {
		path = fullPath
	}

	return text, path, anchor
}

// checkLink validates that a link's target file exists and, if an anchor is
// present, that the anchor resolves in the target file.
func checkLink(filePath, path, anchor string, lineNum int) *LinkIssue {
	fileDir := filepath.Dir(filePath)
	targetPath := filepath.Join(fileDir, path)

	if _, err := os.Stat(targetPath); err != nil {
		return &LinkIssue{
			File:   filePath,
			Line:   lineNum,
			Link:   path,
			Reason: fmt.Sprintf("file not found: %s", path),
		}
	}

	if anchor == "" {
		return nil
	}

	headings, err := extractHeadings(targetPath)
	if err != nil {
		return &LinkIssue{
			File:   filePath,
			Line:   lineNum,
			Link:   path + "#" + anchor,
			Reason: fmt.Sprintf("failed to read target file: %v", err),
		}
	}

	if !headings[anchor] {
		return &LinkIssue{
			File:   filePath,
			Line:   lineNum,
			Link:   path + "#" + anchor,
			Reason: fmt.Sprintf("anchor not found in %s: %s", path, anchor),
		}
	}

	return nil
}

// extractHeadings finds all heading anchors in a markdown file: GitHub-style
// anchors generated from `#` headings, plus any explicit `<a name="...">`
// or `{#id}` anchors (none currently exist in the planning corpus, but both
// forms are honored if introduced).
func extractHeadings(filePath string) (map[string]bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	headings := make(map[string]bool)
	headingCounts := make(map[string]int)
	inCodeBlock := false

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		for _, m := range anchorNameRe.FindAllStringSubmatch(line, -1) {
			headings[m[1]] = true
		}
		for _, m := range anchorBraceRe.FindAllStringSubmatch(line, -1) {
			headings[m[1]] = true
		}

		if !strings.HasPrefix(line, "#") {
			continue
		}

		headingText := strings.TrimLeft(strings.TrimSpace(line), "#")
		headingText = strings.TrimSpace(headingText)
		if headingText == "" {
			continue
		}

		anchor := githubAnchor(headingText)

		// Duplicate headings: first occurrence keeps the bare anchor,
		// later ones get -1, -2, ... suffixes in document order.
		headingCounts[anchor]++
		if headingCounts[anchor] > 1 {
			anchor = fmt.Sprintf("%s-%d", anchor, headingCounts[anchor]-1)
		}

		headings[anchor] = true
	}

	return headings, nil
}

// githubAnchor converts a heading to a GitHub anchor using GitHub's actual
// algorithm: lowercase; drop every character that is not a letter, digit,
// space, hyphen or underscore; replace spaces with hyphens. Consecutive
// spaces/hyphens are NOT collapsed and leading/trailing hyphens are NOT
// trimmed - e.g. an em dash surrounded by spaces disappears and leaves the
// two adjacent spaces to become two hyphens ("A — B" -> "a--b").
func githubAnchor(heading string) string {
	heading = strings.ToLower(heading)

	var result strings.Builder
	for _, r := range heading {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			result.WriteRune(r)
		case r == ' ':
			result.WriteRune('-')
		}
	}

	return result.String()
}

// KnownBrokenLink is an allow-listed broken link, recorded with its
// rationale in definitions/planning/known-broken-links.yaml.
type KnownBrokenLink struct {
	File   string `yaml:"file"`
	Line   int    `yaml:"line"`
	Link   string `yaml:"link"`
	Reason string `yaml:"reason"`
}

type knownBrokenLinksFile struct {
	KnownBrokenLinks []KnownBrokenLink `yaml:"known_broken_links"`
}

// LoadKnownBrokenLinks reads the known-broken-links allow-list. A missing
// file is treated as an empty list.
func LoadKnownBrokenLinks(path string) ([]KnownBrokenLink, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var parsed knownBrokenLinksFile
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return parsed.KnownBrokenLinks, nil
}
