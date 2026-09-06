package workspace

import (
	"strings"
	"testing"
)

func TestSessionStripZeroSessionRendersNothing(t *testing.T) {
	out := renderNode(t, SessionStrip(Session{}))
	if out != "" {
		t.Errorf("SessionStrip(zero Session) = %q, want an empty render", out)
	}
}

func TestSessionStripRendersEveryPresentField(t *testing.T) {
	s := Session{
		Tenant:  "northwind",
		Subject: "avery.okafor@northwind.example",
		Roles:   []string{"hr.business_partner", "promotion.approver"},
		Purpose: "promotion_review",
	}
	out := renderNode(t, SessionStrip(s))
	for _, want := range []string{
		`id="` + AuthorityElementID + `"`,
		`aria-label="Authority context"`,
		"avery.okafor@northwind.example",
		"northwind",
		"promotion_review",
		"hr.business_partner, promotion.approver",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SessionStrip missing %q: %s", want, out)
		}
	}
}

func TestSessionStripOmitsFieldsNotPresent(t *testing.T) {
	out := renderNode(t, SessionStrip(Session{Subject: "avery.okafor@northwind.example"}))
	if !strings.Contains(out, "Acting as") {
		t.Errorf("SessionStrip omitted the one present field: %s", out)
	}
	for _, absent := range []string{"Tenant:", "Purpose:", "Roles:"} {
		if strings.Contains(out, absent) {
			t.Errorf("SessionStrip rendered a label for an absent field %q: %s", absent, out)
		}
	}
}
