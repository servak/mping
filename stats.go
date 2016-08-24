package mping

import (
	"fmt"
	"time"
)

const (
	Host    = "Host"
	Success = "Succ"
	Loss    = "Loss(%)"
	Last    = "Last"
	Fail    = "Fail"
	Avg     = "Avg"
	Best    = "Best"
	Worst   = "Worst"
)

type statistics []*stats

func (s statistics) keys() []string {
	return []string{Host, Success, Fail, Loss, Last, Avg, Best, Worst}
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

func (s *stats) init() {
	s.max = 0
	s.min = 0
	s.total = 0
	s.last = 0
	s.count = 0
	s.success = 0
	s.fail = 0
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
	if s.hostname != s.ip {
		v[Host] = fmt.Sprintf("%s(%s)", s.hostname, s.ip)
	}

	v[Success] = fmt.Sprintf("%d", s.success)
	v[Fail] = fmt.Sprintf("%d", s.fail)
	v[Loss] = fmt.Sprintf("%5.1f%%", s.loss())
	v[Last] = fmt.Sprintf("%v", s.last)
	v[Avg] = fmt.Sprintf("%v", s.average())
	v[Best] = fmt.Sprintf("%v", s.min)
	v[Worst] = fmt.Sprintf("%v", s.max)
	return v
}

type byLast struct {
	statistics
}

func (b byLast) Less(i, j int) bool {
	return b.statistics[i].last < b.statistics[j].last
}

type byAvg struct {
	statistics
}

func (b byAvg) Less(i, j int) bool {
	return b.statistics[i].average() < b.statistics[j].average()
}

type byWorst struct {
	statistics
}

func (b byWorst) Less(i, j int) bool {
	return b.statistics[i].max < b.statistics[j].max
}

type bySuccess struct {
	statistics
}

func (b bySuccess) Less(i, j int) bool {
	return b.statistics[i].success < b.statistics[j].success
}

type byLoss struct {
	statistics
}

func (b byLoss) Less(i, j int) bool {
	return b.statistics[i].fail < b.statistics[j].fail
}

type byBest struct {
	statistics
}

func (b byBest) Less(i, j int) bool {
	return b.statistics[i].min < b.statistics[j].min
}

type byHost struct {
	statistics
}

func (b byHost) Less(i, j int) bool {
	return len(b.statistics[i].hostname) < len(b.statistics[j].hostname)
}
