package mping

import (
	"fmt"
	"time"
)

const (
	Host  = "Host"
	Sent  = "Sent"
	Loss  = "Loss(%)"
	Last  = "Last"
	Res   = "Succ/Fail"
	Avg   = "Avg"
	Best  = "Best"
	Worst = "Worst"
)

type statistics []*stats

func (s statistics) keys() []string {
	return []string{Host, Sent, Res, Loss, Last, Avg, Best, Worst}
}

func (s statistics) Len() int {
	return len(s)
}

func (s statistics) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s statistics) getMaxLength(key string) int {
	length := len(key)
	for _, v := range s {
		if l := len(v.values()[key]); length < l {
			length = l
		}
	}

	return length
}

func (s statistics) setFailed(ip string) {
	for _, v := range s {
		if v.ip == ip {
			v.failed()
		}
	}
}

func (s statistics) setSucceed(ip string, rtt time.Duration) {
	for _, v := range s {
		if v.ip == ip {
			v.succeed(rtt)
		}
	}
}

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
	ip       string
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

func (s stats) values() map[string]string {
	v := make(map[string]string)
	v[Host] = s.hostname
	v[Sent] = fmt.Sprintf("%d", s.count)
	v[Res] = fmt.Sprintf("%d/%d", s.success, s.fail)
	v[Loss] = fmt.Sprintf("%5.1f%%", s.loss())
	v[Last] = fmt.Sprintf("%v", s.last)
	v[Avg] = fmt.Sprintf("%v", s.average())
	v[Best] = fmt.Sprintf("%v", s.min)
	v[Worst] = fmt.Sprintf("%v", s.max)
	return v
}
