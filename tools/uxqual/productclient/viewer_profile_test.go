package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestViewerProfileMatchesAnAuthorizedWorkerIdentity(t *testing.T) {
	people := []productui.Person{{ID: "worker-42", WorkerID: "person-42", Name: "Taylor Morgan", Initials: "TM", PhotoURL: "/workspace/assets/person-42-small.jpg", Role: "People Partner"}}
	got := projectViewerProfile(Session{Principal: "person-42"}, people)
	if got.PersonID != "worker-42" || got.Name != "Taylor Morgan" || got.PhotoURL != "/workspace/assets/person-42-small.jpg" || got.Role != "People Partner" {
		t.Fatalf("viewer profile = %+v", got)
	}
}

func TestHarborCareDevPersonaBindsToItsDurableDirectoryWorker(t *testing.T) {
	people := []productui.Person{
		{ID: "hc-050-rafael-torres", WorkerID: "worker-id-50", WorkerNumber: "HC-21050", Name: "Rafael Torres", Initials: "RT", PhotoURL: "/workspace/assets/person-hc-050-small.jpg", Role: "Director of People Operations · M4"},
	}
	got := projectViewerProfile(Session{Tenant: "harborcare-demo", Principal: "hc-050-rafael-torres"}, people)
	if got.PersonID != "hc-050-rafael-torres" || got.Name != "Rafael Torres" || got.PhotoURL != "/workspace/assets/person-hc-050-small.jpg" {
		t.Fatalf("database-backed development viewer profile = %+v", got)
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
