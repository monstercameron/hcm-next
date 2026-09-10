package delivery

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/messagetemplate"
)

// Protected fixtures: salary, medical, bank and case values that must
// never appear on any delivery surface.
func forbiddenFixtures() []string {
	return []string{"95000.00", "DX-E11", "DE89370400440532013000", "CASE-7-RESTRICTED"}
}

func noticeTemplate() messagetemplate.Template {
	return messagetemplate.Template{
		Key: "pay-stub-ready", Version: 1, Purpose: messagetemplate.PurposeNotice,
		Channel: messagetemplate.ChannelEmail, Locale: "en-US",
		Classification: "INTERNAL", LegalBasis: "employment-contract",
		Subject:      "Your pay stub is ready, {{recipient_name}}",
		Body:         "Hello {{recipient_name}}, your stub for {{effective_date}} is ready. {{message}}",
		Placeholders: []string{"recipient_name", "effective_date", "message"},
	}
}

func renderNotice(t *testing.T, message string) messagetemplate.Rendered {
	t.Helper()
	rendered, err := noticeTemplate().Render(messagetemplate.RenderRequest{
		Purpose: messagetemplate.PurposeNotice, Channel: messagetemplate.ChannelEmail,
		Locale: "en-US", Classification: "INTERNAL", LegalBasis: "employment-contract",
		Parameters: map[string]string{
			"recipient_name": "jane", "effective_date": "2026-02-01", "message": message,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}

// TestTodo_MSG_010 is the MSG-010 primary test: the redaction scan hunts
// every forbidden fixture on every surface, the send gate refuses leaks,
// the inbox keeps authorized content and the external message carries
// only the approved notice.
func TestTodo_MSG_010(t *testing.T) {
	forbidden := forbiddenFixtures()
	rendered := renderNotice(t, "Open your secure inbox to view it.")

	surfaces := OperationalSurfaces{
		Metadata: map[string]string{"provider": "email-1", "template": rendered.Digest},
		Logs:     []string{"dispatch intent/msg-10 template=pay-stub-ready"},
		Traces:   []string{"span=send template=pay-stub-ready"},
		Metrics:  []string{"sends_total{template=\"pay-stub-ready\"} 1"},
	}
	result, err := Scan(rendered, surfaces, forbidden)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Clean() {
		t.Fatalf("clean send scans dirty: %+v", result.Findings)
	}
	if err := GateSend(rendered, surfaces, forbidden); err != nil {
		t.Fatalf("clean send gated: %v", err)
	}

	// The inbox retains authorized content: names and dates stay.
	if !strings.Contains(rendered.Subject, "jane") || !strings.Contains(rendered.Body, "2026-02-01") {
		t.Fatalf("inbox copy lost authorized content: %+v", rendered)
	}
	// The external message carries only the approved notice: identity,
	// never text.
	notice := BuildExternalNotice(rendered, "jane")
	noticeText := notice.TemplateKey + notice.TemplateDigest + notice.RecipientRef + fmt.Sprintf("%d", notice.TemplateVersion)
	for _, value := range forbidden {
		if strings.Contains(noticeText, value) {
			t.Fatalf("external notice leaks %q", value)
		}
	}
	if notice.TemplateKey != "pay-stub-ready" || notice.TemplateDigest == "" {
		t.Fatalf("notice is not the approved one: %+v", notice)
	}

	// The free-text message placeholder is the smuggling vector: a salary
	// value inside it trips the subject/body scan and the gate.
	smuggled := renderNotice(t, "Your new salary is 95000.00 effective now.")
	dirty, err := Scan(smuggled, surfaces, forbidden)
	if err != nil {
		t.Fatal(err)
	}
	if dirty.Clean() {
		t.Fatal("smuggled salary scans clean")
	}
	surfacesWithLeak := surfaces
	surfacesWithLeak.Metadata["debug"] = "amount=95000.00"
	if err := GateSend(smuggled, surfacesWithLeak, forbidden); err == nil {
		t.Fatal("leaking send passed the gate")
	}
}

func TestTodo_MSG_010_Race(t *testing.T) {
	rendered := renderNotice(t, "Open your secure inbox to view it.")
	surfaces := OperationalSurfaces{
		Metadata: map[string]string{"provider": "email-1"},
		Logs:     []string{"dispatch ok"},
	}
	forbidden := forbiddenFixtures()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := Scan(rendered, surfaces, forbidden)
			if err != nil {
				errs <- err
				return
			}
			if !result.Clean() {
				errs <- errors.New("clean send scans dirty")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent scan = %v", err)
	}
}

// TestTodo_MSG_010_Integration renders through the real template
// registry, gates the send and dispatches the notice: template identity
// on the provider metadata must itself scan clean.
func TestTodo_MSG_010_Integration(t *testing.T) {
	registry := messagetemplate.NewRegistry()
	if err := registry.Publish(noticeTemplate()); err != nil {
		t.Fatal(err)
	}
	rendered, err := registry.Render("pay-stub-ready", 1, messagetemplate.RenderRequest{
		Purpose: messagetemplate.PurposeNotice, Channel: messagetemplate.ChannelEmail,
		Locale: "en-US", Classification: "INTERNAL", LegalBasis: "employment-contract",
		Parameters: map[string]string{
			"recipient_name": "jane", "effective_date": "2026-02-01", "message": "Open your secure inbox.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	surfaces := OperationalSurfaces{
		Metadata: map[string]string{"provider": "email-1", "template": rendered.Digest},
		Logs:     []string{"rendered pay-stub-ready v1"},
		Traces:   []string{"span=render"},
		Metrics:  []string{"renders_total 1"},
	}
	if err := GateSend(rendered, surfaces, forbiddenFixtures()); err != nil {
		t.Fatalf("registry-rendered send gated: %v", err)
	}
	notice := BuildExternalNotice(rendered, "jane")
	if notice.TemplateDigest != rendered.Digest {
		t.Fatal("external notice does not cite the rendered digest")
	}
}

func TestTodo_MSG_010_Fault(t *testing.T) {
	rendered := renderNotice(t, "hello")
	forbidden := forbiddenFixtures()

	// A scan with nothing forbidden proves nothing.
	if _, err := Scan(rendered, OperationalSurfaces{}, nil); err == nil {
		t.Fatal("empty-fixture scan accepted")
	}
	// Every surface reports its own findings with exact names.
	leaky := OperationalSurfaces{
		Metadata: map[string]string{"account": "DE89370400440532013000"},
		Logs:     []string{"case CASE-7-RESTRICTED opened"},
		Traces:   []string{"diagnosis DX-E11 recorded"},
		Metrics:  []string{"salary_band{amount=\"95000.00\"} 1"},
	}
	result, err := Scan(rendered, leaky, forbidden)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[Surface]bool{}
	for _, finding := range result.Findings {
		seen[finding.Surface] = true
	}
	for _, surface := range []Surface{SurfaceMetadata, SurfaceLog, SurfaceTrace, SurfaceMetric} {
		if !seen[surface] {
			t.Fatalf("surface %s missing from %+v", surface, result.Findings)
		}
	}
	// Forbidden values embedded in longer tokens still match.
	embedded := renderNotice(t, "ref:PRE95000.00POST")
	embeddedResult, err := Scan(embedded, OperationalSurfaces{}, forbidden)
	if err != nil {
		t.Fatal(err)
	}
	if embeddedResult.Clean() {
		t.Fatal("embedded salary scans clean")
	}
}

func TestTodo_MSG_010_Security(t *testing.T) {
	forbidden := forbiddenFixtures()

	// Every fixture trips the gate from the body alone.
	for _, value := range forbidden {
		rendered := renderNotice(t, "value "+value)
		if err := GateSend(rendered, OperationalSurfaces{}, forbidden); err == nil {
			t.Fatalf("fixture %q passed the gate", value)
		}
	}
	// The notice path cannot be tricked into carrying text: it has no
	// text field, only identity.
	rendered := renderNotice(t, "Your new salary is 95000.00 effective now.")
	notice := BuildExternalNotice(rendered, "jane")
	if notice.TemplateKey == "" || notice.RecipientRef != "jane" {
		t.Fatalf("notice malformed: %+v", notice)
	}
	// A second render with identical parameters is byte-identical, so the
	// notice digest is stable evidence, not a per-send secret.
	again := renderNotice(t, "Your new salary is 95000.00 effective now.")
	if again.Digest != rendered.Digest {
		t.Fatal("identical renders diverge")
	}
}
