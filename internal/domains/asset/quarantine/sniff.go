package quarantine

import (
	"bytes"
	"unicode/utf8"
)

// The magic-byte signatures this package recognizes. Each is checked against
// the start of the content; zipSignature is deliberately the generic ZIP
// local-file-header signature, because a DOCX file is a ZIP archive and this
// package sniffs the container format, not the OOXML parts inside it.
var (
	pdfSignature  = []byte("%PDF-")
	pngSignature  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegSignature = []byte{0xFF, 0xD8, 0xFF}
	zipSignature  = []byte{0x50, 0x4B, 0x03, 0x04}
)

// Sniff inspects content's own magic bytes and reports which [Category] it
// belongs to, independent of any type the uploader declared. A slice that
// matches none of the declared binary signatures and is not valid UTF-8 text
// is [CategoryUnknown].
//
// Plain text has no magic-byte signature at all, so Sniff falls back to a
// content check: bytes containing no NUL byte that decode as valid UTF-8 are
// [CategoryText]. That is deliberately permissive -- it cannot tell a CSV
// from a plain text, JSON or YAML file by bytes alone -- but it is exactly
// as permissive as it needs to be to catch this package's actual threat: a
// binary payload (a PDF, an image, a ZIP/DOCX archive, or a payload matching
// none of those) renamed to look like text.
func Sniff(content []byte) Category {
	switch {
	case bytes.HasPrefix(content, pdfSignature):
		return CategoryPDF
	case bytes.HasPrefix(content, pngSignature):
		return CategoryPNG
	case bytes.HasPrefix(content, jpegSignature):
		return CategoryJPEG
	case bytes.HasPrefix(content, zipSignature):
		return CategoryZIP
	case looksLikeText(content):
		return CategoryText
	default:
		return CategoryUnknown
	}
}

// looksLikeText reports whether content contains no NUL byte and is valid
// UTF-8, including the empty slice (an empty upload is not itself a binary
// signature mismatch; [Policy] and [Upload]'s own size/allowlist checks are
// what refuse an empty or oversized upload if that is unwanted).
func looksLikeText(content []byte) bool {
	if bytes.IndexByte(content, 0x00) >= 0 {
		return false
	}
	return utf8.Valid(content)
}
