package mping

import (
	"testing"
	"time"
)

func TestStatsLoss(t *testing.T) {
	lossTests := []struct {
		count int
		fail  int
		want  float64
	}{
		{0, 0, 0.0},
		{1, 0, 0.0},
		{1, 1, 100.0},
		{2, 1, 50.0},
	}

	for _, v := range lossTests {
		s := stats{
			count: v.count,
			fail:  v.fail,
		}

		if s.loss() != v.want {
			t.Errorf("lossTest. actual: %f want: %f", s.loss(), v.want)
		}
	}
}

func TestStatsAverage(t *testing.T) {
	lossTests := []struct {
		success int
		total   time.Duration
		want    time.Duration
	}{
		{2, time.Second * 10, time.Second * 5},
		{4, time.Millisecond * 500, time.Millisecond * 125},
	}

	for _, v := range lossTests {
		s := stats{
			success: v.success,
			total:   v.total,
		}

		if s.average() != v.want {
			t.Errorf("averageTest. actual: %v want: %v", s.average(), v.want)
		}
	}
}
