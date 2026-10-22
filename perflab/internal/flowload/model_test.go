package flowload

import "testing"

func TestFailureWindow(t *testing.T) {
	for _, failed := range []int{49, 50, 51} {
		w := Window{Start: 1000}
		for i := 0; i < 100; i++ {
			w.Events = append(w.Events, Event{Type: "outcome", Completed: 10500, Success: i >= failed})
		}
		w.Events = append(w.Events, Event{Type: "cancel", Completed: 10500, Success: false})
		if stop, rate := w.Stop(10999); stop || rate != nil {
			t.Fatal("未满窗口")
		}
		stop, rate := w.Stop(11000)
		if stop != (failed > 50) || rate == nil || *rate != float64(failed)/100 {
			t.Fatal(failed, stop, rate)
		}
	}
	w := Window{Start: 1000}
	if stop, rate := w.Stop(12000); stop || rate != nil {
		t.Fatal("无完成请求")
	}
}
func TestEvaluateCompletionBoundaries(t *testing.T) {
	stage := Stage{Name: "test", Target: 1, Warmup: 10, Measurement: 10}
	events := []Event{}
	for i := 0; i < 10; i++ {
		events = append(events, Event{Type: "outcome", Phase: "warmup", Completed: 11000 + float64(i)*1000, Success: true, HTTPMS: 10})
	}
	r := Evaluate(stage, events, 1000, 22000, "", 0, 1)
	if !r.BusinessPassed || r.Completed != 10 || r.ActualRPS != 1 {
		t.Fatal(r)
	}
	r = Evaluate(stage, events, 1000, 15000, "failure_rate_over_50_percent", 1, 0)
	if r.ExecutionCompleted || r.BusinessPassed {
		t.Fatal(r)
	}
}
func TestPlateau(t *testing.T) {
	r := []Result{{Target: 100, SuccessRPS: 90, P95: 100}, {Target: 200, SuccessRPS: 91, P95: 101}, {Target: 400, SuccessRPS: 92, P95: 102}}
	if !Plateau(r) {
		t.Fatal("未识别平台")
	}
	r[2].SuccessRPS = 100
	if Plateau(r) {
		t.Fatal("增益足够")
	}
}
