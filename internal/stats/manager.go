package stats

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/servak/mping/internal/prober"
)

type MetricsManager struct {
	metrics map[string]*Metrics
	mu      sync.Mutex
	counter int
}

// 新しいMetricsManagerを生成
func NewMetricsManager(targets map[string]string) *MetricsManager {
	metrics := make(map[string]*Metrics)
	count := 1
	for k, v := range targets {
		name := v
		if net.ParseIP(v) == nil {
			name = fmt.Sprintf("%s(%s)", v, k)
		}
		metrics[k] = &Metrics{
			ID:   count,
			Name: name,
		}
		count++
	}
	return &MetricsManager{
		metrics: metrics,
		counter: count,
	}
}

// 指定されたホストのMetricsを取得
func (mm *MetricsManager) GetMetrics(host string) *Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		mm.counter++
		m = &Metrics{
			ID:   mm.counter,
			Name: host,
		}
		mm.metrics[host] = m
	}
	return m
}

// 全てのMetricsをリセット
func (mm *MetricsManager) ResetAllMetrics() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for _, m := range mm.metrics {
		m.Reset()
	}
}

// ホストに対する成功を登録
func (mm *MetricsManager) Success(host string, rtt time.Duration, sentTime time.Time) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Success(rtt, sentTime)
	mm.mu.Unlock()
}

// ホストに対する失敗を登録
func (mm *MetricsManager) Failed(host string, sentTime time.Time) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Fail(sentTime)
	mm.mu.Unlock()
}

func (mm *MetricsManager) Sent(host string) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Sent()
	mm.mu.Unlock()
}

func (mm *MetricsManager) Subscribe(res chan *prober.Event) {
	go func() {
		for r := range res {
			switch r.Result {
			case prober.SENT:
				mm.Sent(r.Target)
			case prober.SUCCESS:
				mm.Success(r.Target, r.Rtt, r.SentTime)
			case prober.TIMEOUT:
				mm.Failed(r.Target, r.SentTime)
			case prober.FAILED:
				mm.Failed(r.Target, r.SentTime)
			}
		}
	}()
}

func (mm *MetricsManager) GetSortedMetricsByKey(k Key) []Metrics {
	mm.mu.Lock()
	var res []Metrics
	for _, m := range mm.metrics {
		res = append(res, *m)
	}
	mm.mu.Unlock()
	sort.SliceStable(res, func(i, j int) bool {
		return res[i].ID < res[j].ID
	})
	sort.SliceStable(res, func(i, j int) bool {
		mi := res[i]
		mj := res[j]
		switch k {
		case Host:
			return len(res[i].Name) > len(res[j].Name)
		case Sent:
			return mi.Total > mj.Total
		case Success:
			return mi.Successful > mj.Successful
		case Loss:
			return mi.Loss > mj.Loss
		case Fail:
			return mi.Failed > mj.Failed
		case Last:
			return rejectLess(mi.LastRTT, mj.LastRTT)
		case Avg:
			return rejectLess(mi.AverageRTT, mj.AverageRTT)
		case Best:
			return rejectLess(mi.MinimumRTT, mj.MinimumRTT)
		case Worst:
			return rejectLess(mi.MaximumRTT, mj.MaximumRTT)
		case LastSuccTime:
			return mi.LastSuccTime.After(mj.LastSuccTime)
		case LastFailTime:
			return mi.LastFailTime.After(mj.LastFailTime)
		}
		return false
	})
	return res
}

func rejectLess(i, j time.Duration) bool {
	if i == 0 {
		return false
	}
	if j == 0 {
		return true
	}
	return i < j
}
