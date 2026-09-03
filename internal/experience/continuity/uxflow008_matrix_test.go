package continuity

import (
	"errors"
	"testing"
)

// UXFLOW-008 is a presentation/continuity contract: alternate controls and
// language may change, but the canonical meaning, digest, deadline and
// privacy compartment must remain identical.
func TestUXFLOW008AccessibilityAndLocaleMatrixPreservesCanonicalMeaning(t *testing.T) {
	flow, browser := fixture(t, RouteBrowser)
	want, err := Execute(flow, browser)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		route  Route
		mutate func(*Request)
	}{
		{name: "keyboard", route: RouteKeyboard},
		{name: "screen-reader", route: RouteScreenReader},
		{name: "zoom", route: RouteZoom},
		{name: "rtl", route: RouteRTL, mutate: func(r *Request) {
			r.Locale = "ar"
			r.TemplateVersion = "promotion.ar.v3"
		}},
		{name: "translated", route: RouteKeyboard, mutate: func(r *Request) {
			r.Locale = "fr-FR"
			r.TemplateVersion = "promotion.fr.v3"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, req := fixture(t, tc.route)
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			got, err := Execute(flow, req)
			if err != nil {
				t.Fatal(err)
			}
			if !Equivalent(want, got) {
				t.Fatalf("presentation route changed canonical outcome: want=%+v got=%+v", want, got)
			}
			if got.IntentDigest != want.IntentDigest || got.Evidence.Meaning != want.Evidence.Meaning {
				t.Fatalf("%s changed canonical meaning/digest", tc.name)
			}
			if !got.Evidence.Deadline.Equal(want.Evidence.Deadline) || got.Evidence.PrivacyClass != want.Evidence.PrivacyClass {
				t.Fatalf("%s changed deadline/privacy continuity", tc.name)
			}
		})
	}
}

func TestUXFLOW008AttributedAssistanceIsRetainedInReceipt(t *testing.T) {
	flow, req := fixture(t, RouteAssisted)
	req.InterpreterFor = "worker:1"
	req.Representative = "representative:9"
	out, err := Execute(flow, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Evidence.AssistedBy != "support:7" || out.Evidence.InterpreterFor != "worker:1" || out.Evidence.Representative != "representative:9" {
		t.Fatalf("assistance attribution was not retained: %+v", out.Evidence)
	}
	if out.Evidence.IntentDigest == "" || out.Evidence.PrivacyClass == "" || out.Evidence.Deadline.IsZero() {
		t.Fatalf("assisted receipt omitted continuity fields: %+v", out.Evidence)
	}

	for _, mutate := range []func(*Request){
		func(r *Request) { r.AssistedBy = "" },
		func(r *Request) { r.AssistedBy = r.Subject },
	} {
		bad := req
		mutate(&bad)
		if _, err := Execute(flow, bad); !errors.Is(err, ErrAttribution) {
			t.Fatalf("invalid attribution error = %v, want ErrAttribution", err)
		}
	}
}

func TestUXFLOW008InaccessibleUploadUsesManualFallbackWithoutParityLoss(t *testing.T) {
	flow, req := fixture(t, RouteManual)
	wantDigest := req.ManualReceipt
	// No WCAG/focus/announcement evidence is available on this route: the
	// signed/manual receipt is the bounded continuity artifact instead.
	if req.WCAGEvidence != "" || req.FocusEvidence != "" || req.AnnouncementEvidence != "" {
		t.Fatal("manual fallback unexpectedly requires browser accessibility evidence")
	}
	out, err := Execute(flow, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.IntentDigest != wantDigest || out.Evidence.ManualReceipt != wantDigest {
		t.Fatalf("manual fallback lost digest: %+v", out.Evidence)
	}
	if !out.Evidence.Deadline.Equal(req.Deadline) || out.Evidence.PrivacyClass != req.PrivacyClass {
		t.Fatalf("manual fallback lost deadline/privacy: %+v", out.Evidence)
	}
}
