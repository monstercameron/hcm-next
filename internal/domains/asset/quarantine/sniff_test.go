package quarantine_test

import (
	"bytes"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// pngFixture is a minimal, structurally valid one-pixel PNG: the eight-byte
// signature plus a plausible IHDR chunk. [quarantine.Sniff] only ever
// inspects the leading signature bytes, but a realistic fixture keeps this
// golden vector honest about what a real upload looks like.
var pngFixture = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // signature
	0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00,
}

var jpegFixture = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}

// zipFixture is a ZIP local-file-header signature: the same leading bytes a
// DOCX (or any OOXML) file, or a plain ZIP archive, begins with.
var zipFixture = []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00, 0x00, 0x00, 0x08, 0x00}

var pdfFixture = []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog >>\nendobj\n")

var csvFixture = []byte("employee_id,name,department\n1,Ada Lovelace,Engineering\n")

var txtFixture = []byte("quarterly compensation review notes\nno action required\n")

// TestTodo_DOC_MAL_001_Golden proves Sniff's magic-byte sniff table against
// one fixed, realistic byte vector per declared content type -- pdf, png,
// jpeg, docx (zip container), csv and txt -- plus a binary blob that matches
// none of them, exactly the sniff table DOC-MAL-001 asks for.
func TestTodo_DOC_MAL_001_Golden(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		want    quarantine.Category
	}{
		{"pdf", pdfFixture, quarantine.CategoryPDF},
		{"png", pngFixture, quarantine.CategoryPNG},
		{"jpeg", jpegFixture, quarantine.CategoryJPEG},
		{"docx (zip container)", zipFixture, quarantine.CategoryZIP},
		{"csv", csvFixture, quarantine.CategoryText},
		{"txt", txtFixture, quarantine.CategoryText},
		{"unrecognized binary blob", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01, 0x02, 0xFF}, quarantine.CategoryUnknown},
		{"empty content sniffs as text", []byte{}, quarantine.CategoryText},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := quarantine.Sniff(c.content)
			if got != c.want {
				t.Errorf("Sniff(%s) = %s, want %s", c.name, got, c.want)
			}
		})
	}

	t.Run("a PDF signature embedded past the first byte does not sniff as PDF", func(t *testing.T) {
		polyglot := append([]byte("PK-DECOY"), pdfFixture...)
		if got := quarantine.Sniff(polyglot); got == quarantine.CategoryPDF {
			t.Error("Sniff matched a PDF signature that was not at the start of the content")
		}
	})

	t.Run("a zip archive is never mistaken for text even though it contains no NUL byte", func(t *testing.T) {
		// A ZIP local file header's third and fourth bytes are frequently
		// non-UTF8-continuation bytes, but this asserts the binary
		// signature check runs before the text fallback regardless.
		if got := quarantine.Sniff(zipFixture); got == quarantine.CategoryText {
			t.Error("a ZIP signature must never sniff as text")
		}
	})

	t.Run("a NUL byte anywhere disqualifies text", func(t *testing.T) {
		withNul := bytes.Join([][]byte{[]byte("before"), {0x00}, []byte("after")}, nil)
		if got := quarantine.Sniff(withNul); got != quarantine.CategoryUnknown {
			t.Errorf("Sniff(content with embedded NUL) = %s, want %s", got, quarantine.CategoryUnknown)
		}
	})

	t.Run("invalid UTF-8 disqualifies text", func(t *testing.T) {
		invalid := []byte{0xFF, 0xFE, 0xFD}
		if got := quarantine.Sniff(invalid); got != quarantine.CategoryUnknown {
			t.Errorf("Sniff(invalid UTF-8) = %s, want %s", got, quarantine.CategoryUnknown)
		}
	})
}
