package mping

import "time"

type stats struct {
	order    int
	max      time.Duration
	min      time.Duration
	total    time.Duration
	last     time.Duration
	count    int
	success  int
	fail     int
	hostname string
}

func (s *stats) succeed(t time.Duration) {
	if s.max < t {
		s.max = t
	}

	if s.min > t || s.min == 0 {
		s.min = t
	}

	s.last = t
	s.total += t
	s.count++
	s.success++
}

func (s *stats) failed() {
	s.count++
	s.fail++
}

func (s stats) loss() float64 {
	if s.count == 0 {
		return 0.0
	}

	if s.fail == 0 {
		return 0.0
	}

	return float64(s.fail) / float64(s.count) * 100
}

func (s stats) average() time.Duration {
	if s.success == 0 {
		return time.Duration(0)
	}

	return time.Duration(int(s.total) / s.success)
}
