package delivery

// Protected-content redaction (MSG-010): salary, medical, bank and case
// fixtures must never enter attention email or any operational surface.
// The scanner checks the rendered subject and body, provider metadata,
// structured logs, trace spans and metric labels for every forbidden
// value; the send gate refuses anything unclean. The inbox retains
// authorized content while the external message carries only the approved
// notice. Classification-aware structured logging stays with the
// telemetry owner; this package proves the delivery surfaces clean.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/messagetemplate"
)

// Surface names every scanned operational surface.
type Surface string

const (
	SurfaceSubject  Surface = "subject"
	SurfaceBody     Surface = "body"
	SurfaceMetadata Surface = "provider-metadata"
	SurfaceLog      Surface = "log"
	SurfaceTrace    Surface = "trace"
	SurfaceMetric   Surface = "metric"
)

// Finding names one forbidden value on one surface.
type Finding struct {
	Surface Surface
	Value   string
}

// OperationalSurfaces carries the non-message surfaces for one send.
type OperationalSurfaces struct {
	Metadata map[string]string
	Logs     []string
	Traces   []string
	Metrics  []string
}

// ScanResult is the exact redaction verdict.
type ScanResult struct {
	Findings []Finding
}

// Clean reports zero forbidden values anywhere.
func (r ScanResult) Clean() bool { return len(r.Findings) == 0 }

// Scan checks the rendered message plus every operational surface for
// every forbidden value. An empty fixture set proves nothing and is
// refused: the scan must always hunt something.
func Scan(rendered messagetemplate.Rendered, surfaces OperationalSurfaces, forbidden []string) (ScanResult, error) {
	if len(forbidden) == 0 {
		return ScanResult{}, fmt.Errorf("delivery: scan with no forbidden values proves nothing")
	}
	texts := map[Surface][]string{
		SurfaceSubject: {rendered.Subject},
		SurfaceBody:    {rendered.Body},
	}
	for key, value := range surfaces.Metadata {
		texts[SurfaceMetadata] = append(texts[SurfaceMetadata], key, value)
	}
	texts[SurfaceLog] = append(texts[SurfaceLog], surfaces.Logs...)
	texts[SurfaceTrace] = append(texts[SurfaceTrace], surfaces.Traces...)
	texts[SurfaceMetric] = append(texts[SurfaceMetric], surfaces.Metrics...)
	var result ScanResult
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		for surface, candidates := range texts {
			for _, text := range candidates {
				if strings.Contains(text, value) {
					result.Findings = append(result.Findings, Finding{Surface: surface, Value: value})
					break
				}
			}
		}
	}
	sort.Slice(result.Findings, func(i, j int) bool {
		if result.Findings[i].Surface != result.Findings[j].Surface {
			return result.Findings[i].Surface < result.Findings[j].Surface
		}
		return result.Findings[i].Value < result.Findings[j].Value
	})
	return result, nil
}

// ExternalNotice is the only content approved for the external message:
// template identity plus recipient reference, never field values.
type ExternalNotice struct {
	TemplateKey     string
	TemplateVersion int
	TemplateDigest  string
	RecipientRef    string
}

// BuildExternalNotice derives the approved external notice. It carries no
// rendered text at all, so protected content cannot leak through it.
func BuildExternalNotice(rendered messagetemplate.Rendered, recipientRef string) ExternalNotice {
	return ExternalNotice{
		TemplateKey: rendered.Key, TemplateVersion: rendered.Version,
		TemplateDigest: rendered.Digest, RecipientRef: recipientRef,
	}
}

// GateSend refuses the send unless every surface scans clean.
func GateSend(rendered messagetemplate.Rendered, surfaces OperationalSurfaces, forbidden []string) error {
	result, err := Scan(rendered, surfaces, forbidden)
	if err != nil {
		return err
	}
	if !result.Clean() {
		first := result.Findings[0]
		return fmt.Errorf("delivery: send gate: forbidden value on %s (%d findings)", first.Surface, len(result.Findings))
	}
	return nil
}
