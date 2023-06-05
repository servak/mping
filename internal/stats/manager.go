package stats

import (
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
func NewMetricsManager() *MetricsManager {
	return &MetricsManager{
		metrics: make(map[string]*Metrics),
	}
}

// 指定されたホストのMetricsを取得
func (mm *MetricsManager) GetMetrics(host string) *Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		mm.counter++
		m = &Metrics{ID: mm.counter}
		mm.metrics[host] = m
	}
	return m
}

// 全てのMetricsを取得
func (mm *MetricsManager) GetAllMetrics() map[string]*Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 新しいマップを生成して、その中に全てのMetricsをコピー
	copiedMetrics := make(map[string]*Metrics)
	for host, metrics := range mm.metrics {
		copiedMetrics[host] = metrics
	}
	return copiedMetrics
}

// 全てのMetricsをリセット
func (mm *MetricsManager) ResetAllMetrics() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for host := range mm.metrics {
		mm.metrics[host] = &Metrics{}
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

type HostMetrics struct {
	Hostname string
	Metrics  *Metrics
}

func (mm *MetricsManager) GetSortedMetricsByKey(k Key) []HostMetrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// HostMetricsのスライスを作成
	hostMetrics := make([]HostMetrics, 0, len(mm.metrics))
	for host, metrics := range mm.metrics {
		hostMetrics = append(hostMetrics, HostMetrics{
			Hostname: host,
			Metrics:  metrics,
		})
	}
	sort.SliceStable(hostMetrics, func(i, j int) bool {
		return hostMetrics[i].Metrics.ID < hostMetrics[j].Metrics.ID
	})
	sort.SliceStable(hostMetrics, func(i, j int) bool {
		mi := hostMetrics[i].Metrics
		mj := hostMetrics[j].Metrics
		switch k {
		case Host:
			return len(hostMetrics[i].Hostname) > len(hostMetrics[j].Hostname)
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

	return hostMetrics
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
