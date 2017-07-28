package model

import (
	"testing"
)

func TestComputeScore(t *testing.T) {
	var tests = []struct {
		instance *Performance
		want     float64
	}{
		{
			&Performance{},
			0.0,
		},
		{
			&Performance{OneMonth: 1 / 0.25, ThreeMonths: 1 / 0.3, SixMonths: 1 / 0.25, OneYear: 1 / 0.2, VolThreeYears: 1 / 0.1},
			3.0,
		},
	}

	for _, test := range tests {
		test.instance.ComputeScore()
		if test.instance.Score != test.want {
			t.Errorf("ComputeScore() with %v = %v, want %v", test.instance, test.instance.Score, test.want)
		}
	}
}