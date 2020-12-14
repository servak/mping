package mping

import (
	"fmt"
	"time"
)

type SortType uint8

const (
	Host SortType = iota
	Success
	Fail
	Loss
	Last
	Avg
	Best
	Worst
	LastSuccTime
	LastFailTime
)

func (s SortType) String() string {
	switch s {
	case Host:
		return "Host"
	case Success:
		return "Succ"
	case Loss:
		return "Loss(%)"
	case Last:
		return "Last"
	case Fail:
		return "Fail"
	case Avg:
		return "Avg"
	case Best:
		return "Best"
	case Worst:
		return "Worst"
	case LastSuccTime:
		return "Last Success"
	case LastFailTime:
		return "Last Fail"
	}

	return ""
}

func NewStatistics() Statistics {
	return Statistics{
		sortType: Success,
		values:   []*stats{},
	}
}

type Statistics struct {
	values   []*stats
	sortType SortType
}

func (s *Statistics) SetNextSort() {
	s.sortType++
	if int(s.sortType) >= len(s.keys()) {
		s.sortType = 0
	}
}

func (s Statistics) keys() []SortType {
	return []SortType{Host, Success, Fail, Loss, Last, Avg, Best, Worst, LastSuccTime, LastFailTime}
}

func (s Statistics) Len() int {
	return len(s.values)
}

func (s Statistics) Swap(i, j int) {
	s.values[i], s.values[j] = s.values[j], s.values[i]
}

func (s Statistics) Less(i, j int) bool {
	switch s.sortType {
	case Host:
		return len(s.values[i].hostname) < len(s.values[j].hostname)
	case Success:
		return s.values[i].success < s.values[j].success
	case Loss:
		return s.values[i].fail > s.values[j].fail
	case Last:
		return s.values[i].last < s.values[j].last
	case Fail:
		return s.values[i].fail < s.values[j].fail
	case Avg:
		return s.values[i].average() < s.values[j].average()
	case Best:
		return s.values[i].min < s.values[j].min
	case Worst:
		return s.values[i].max < s.values[j].max
	case LastSuccTime:
		return s.values[i].lastSuccTime.After(s.values[j].lastSuccTime)
	case LastFailTime:
		return s.values[i].lastFailTime.After(s.values[j].lastFailTime)
	}
	return true
}

func (s Statistics) getMaxLength(key SortType) int {
	length := len(key.String())
	for _, v := range s.values {
		if l := len(v.values()[key]); length < l {
			length = l
		}
	}

	return length
}

func (s Statistics) setFailed(ip string) {
	for _, v := range s.values {
		if v.ip == ip {
			v.failed()
			return
		}
	}
}

func (s Statistics) setSucceed(ip string, rtt time.Duration) {
	for _, v := range s.values {
		if v.ip == ip {
			v.succeed(rtt)
			return
		}
	}
}

type stats struct {
	order        int
	max          time.Duration
	min          time.Duration
	total        time.Duration
	last         time.Duration
	count        int
	success      int
	fail         int
	hostname     string
	ip           string
	lastFailTime time.Time
	lastSuccTime time.Time
}

func (s *stats) init() {
	s.max = 0
	s.min = 0
	s.total = 0
	s.last = 0
	s.count = 0
	s.success = 0
	s.fail = 0
	s.lastSuccTime = time.Time{}
	s.lastFailTime = time.Time{}
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
	s.lastSuccTime = time.Now()
}

func (s *stats) failed() {
	s.count++
	s.fail++
	s.lastFailTime = time.Now()
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

func (s stats) values() map[SortType]string {
	v := make(map[SortType]string)
	v[Host] = s.hostname
	if s.hostname != s.ip {
		v[Host] = fmt.Sprintf("%s(%s)", s.hostname, s.ip)
	}

	v[Success] = fmt.Sprintf("%d", s.success)
	v[Fail] = fmt.Sprintf("%d", s.fail)
	v[Loss] = fmt.Sprintf("%5.1f%%", s.loss())
	v[Last] = durationFormater(s.last)
	v[Avg] = durationFormater(s.average())
	v[Best] = durationFormater(s.min)
	v[Worst] = durationFormater(s.max)

	lastSuccTime := "-"
	lastFailTime := "-"

	if !s.lastSuccTime.IsZero() {
		lastSuccTime = s.lastSuccTime.Format("15:04:05")
	}

	if !s.lastFailTime.IsZero() {
		lastFailTime = s.lastFailTime.Format("15:04:05")
	}

	v[LastSuccTime] = lastSuccTime
	v[LastFailTime] = lastFailTime

	return v
}

func durationFormater(duration time.Duration) string {
	if duration.Microseconds() < 1000 {
		return fmt.Sprintf("%3dµs", duration.Microseconds())
	} else if duration.Milliseconds() < 1000 {
		return fmt.Sprintf("%3dms", duration.Milliseconds())
	} else {
		return fmt.Sprintf("%3.0fs", duration.Seconds())
	}
}
