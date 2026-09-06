package records

import "testing"

func TestTodo_RECORDS_DISP_001_Integration(t *testing.T) {
	report, err := Simulate(recordsFixture())
	if err != nil || len(report.Schedules) != 1 {
		t.Fatalf("simulation integration shape: report=%+v err=%v", report, err)
	}
}
