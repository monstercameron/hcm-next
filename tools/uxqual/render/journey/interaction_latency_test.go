//go:build !race

package journey

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

func TestInteractionLatencyGate(t *testing.T) {
	workers := make([]WorkerCard, 10_000)
	for index := range workers {
		workers[index] = WorkerCard{
			Ref: fmt.Sprintf("worker-%05d", index), Name: fmt.Sprintf("Worker %05d", index),
			JobCode: "ENG-SWE3", Grade: "P3", OrgUnit: "Engineering", Location: "Remote",
		}
	}
	view := PeopleView{Workers: workers, SelectedRef: workers[len(workers)-1].Ref}
	budget := latencygate.Budget{
		Name: "journey workforce preview (10000 workers)", P95: 16 * time.Millisecond, Warmups: 3, Samples: 25,
	}
	result, err := latencygate.Measure(budget, func() error {
		_, err := ui.RenderToString(peopleTable(view))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result)
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatal(err)
	}
}
