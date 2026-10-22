package flowload

import "testing"

func TestScan(t *testing.T) {
	for _, tc := range []struct {
		start int
		want  []int
	}{{10, []int{10, 20, 40, 80, 160, 320, 640, 1280, 2560, 5120, 6400}}, {250, []int{250, 500, 1000, 2000, 4000, 6400}}, {6400, []int{6400}}} {
		stages, err := Scan(tc.start, 6400, 30, 300)
		if err != nil || len(stages) != len(tc.want) {
			t.Fatalf("%v %v", stages, err)
		}
		for i, s := range stages {
			if s.Target != tc.want[i] {
				t.Fatal(stages)
			}
		}
	}
	for _, start := range []int{-1, 0, 6401} {
		if _, err := Scan(start, 6400, 30, 300); err == nil {
			t.Fatal(start)
		}
	}
	if Refine(0, 250, .1) != nil || Refine(100, 110, .1) != nil || Refine(100, 200, .1).Target != 150 {
		t.Fatal("边界错误")
	}
}
