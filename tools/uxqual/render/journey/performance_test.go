package journey

import (
	"fmt"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// BenchmarkPeopleTableLargeWorkforce guards the bounded journey-page preview.
// The canonical People route owns full-directory filtering and pagination;
// this operational surface must not scale its mounted controls with tenant size.
func BenchmarkPeopleTableLargeWorkforce(b *testing.B) {
	workers := make([]WorkerCard, 10_000)
	for index := range workers {
		workers[index] = WorkerCard{
			Ref: fmt.Sprintf("worker-%05d", index), Name: fmt.Sprintf("Worker %05d", index),
			JobCode: "ENG-SWE3", Grade: "P3", OrgUnit: "Engineering", Location: "Remote",
		}
	}
	view := PeopleView{Workers: workers, SelectedRef: workers[len(workers)-1].Ref}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(peopleTable(view)); err != nil {
			b.Fatal(err)
		}
	}
}
