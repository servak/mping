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
		m = &Metrics{}
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
		switch k {
		case Host:
			return len(hostMetrics[i].Hostname) > len(hostMetrics[j].Hostname)
		case Sent:
			return hostMetrics[i].Metrics.Total > hostMetrics[j].Metrics.Total
		case Success:
			return hostMetrics[i].Metrics.Successful > hostMetrics[j].Metrics.Successful
		case Loss:
			return hostMetrics[i].Metrics.Loss > hostMetrics[j].Metrics.Loss
		case Last:
			return hostMetrics[i].Metrics.LastRTT > hostMetrics[j].Metrics.LastRTT
		case Fail:
			return hostMetrics[i].Metrics.Failed > hostMetrics[j].Metrics.Failed
		case Avg:
			return hostMetrics[i].Metrics.AverageRTT > hostMetrics[j].Metrics.AverageRTT
		case Best:
			return hostMetrics[i].Metrics.MinimumRTT > hostMetrics[j].Metrics.MinimumRTT
		case Worst:
			return hostMetrics[i].Metrics.MaximumRTT > hostMetrics[j].Metrics.MaximumRTT
		case LastSuccTime:
			return !hostMetrics[i].Metrics.LastSuccTime.After(hostMetrics[j].Metrics.LastSuccTime)
		case LastFailTime:
			return !hostMetrics[i].Metrics.LastFailTime.After(hostMetrics[j].Metrics.LastFailTime)
		}
		return false
	})

	return hostMetrics
}
