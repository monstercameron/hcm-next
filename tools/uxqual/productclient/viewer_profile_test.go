package productclient

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

func TestViewerProfileMatchesAnAuthorizedWorkerIdentity(t *testing.T) {
	people := []productui.Person{{ID: "worker-42", WorkerID: "person-42", Name: "Taylor Morgan", Initials: "TM", PhotoURL: "/workspace/assets/person-42-small.jpg", Role: "People Partner"}}
	got := projectViewerProfile(Session{Principal: "person-42"}, people)
	if got.PersonID != "worker-42" || got.Name != "Taylor Morgan" || got.PhotoURL != "/workspace/assets/person-42-small.jpg" || got.Role != "People Partner" {
		t.Fatalf("viewer profile = %+v", got)
	}
}

func TestHarborCareDevelopmentProfileUsesTheUploadedEmployeeProxy(t *testing.T) {
	people := []productui.Person{
		{WorkerNumber: "HC-21001", Name: "Amina Rahman", PhotoURL: "/workspace/assets/person-hc-001-small.jpg"},
		{WorkerNumber: harborcareDeveloperWorkerNumber, Name: "Rafael Torres", Initials: "RT", PhotoURL: "/workspace/assets/person-hc-050-small.jpg", Role: "Director of People Operations · M4"},
	}
	got := projectViewerProfile(Session{Tenant: "harborcare-demo", Principal: "local-developer"}, people)
	if got.Name != "Rafael Torres" || got.PhotoURL != "/workspace/assets/person-hc-050-small.jpg" {
		t.Fatalf("HarborCare development viewer profile = %+v", got)
	}
}

func TestViewerProfileFallsBackWithoutInventingAPhoto(t *testing.T) {
	got := projectViewerProfile(Session{Principal: "external-auditor"}, nil)
	if got.Name != "External Auditor" || got.Initials != "EA" || got.PhotoURL != "" {
		t.Fatalf("fallback viewer profile = %+v", got)
	}
}

func TestViewerProfileNeverBindsByDisplayName(t *testing.T) {
	people := []productui.Person{{ID: "worker-42", WorkerID: "internal-42", Name: "External Auditor", PhotoURL: "/workspace/assets/private-small.jpg"}}
	got := projectViewerProfile(Session{Principal: "External Auditor"}, people)
	if got.PersonID != "" || got.PhotoURL != "" {
		t.Fatalf("display name became an identity binding: %+v", got)
	}
}
