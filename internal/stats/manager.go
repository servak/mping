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
	metrics := make(map[string]*Metrics)
	return &MetricsManager{
		metrics: metrics,
	}
}

func (mm *MetricsManager) Register(target, name string) {
	v, ok := mm.metrics[target]
	if ok && v.Name != target {
		return
	}
	mm.metrics[target] = &Metrics{
		Name: name,
	}
}

// 指定されたホストのMetricsを取得
func (mm *MetricsManager) GetMetrics(host string) *Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		m = &Metrics{
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
func (mm *MetricsManager) Failed(host string, sentTime time.Time, msg string) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Fail(sentTime, msg)
	mm.mu.Unlock()
}

func (mm *MetricsManager) Sent(host string) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Sent()
	mm.mu.Unlock()
}

func (mm *MetricsManager) Subscribe(res <-chan *prober.Event) {
	go func() {
		for r := range res {
			// Auto-register target on first event
			mm.autoRegister(r.Key, r.DisplayName)
			
			switch r.Result {
			case prober.SENT:
				mm.Sent(r.Key)
			case prober.SUCCESS:
				mm.Success(r.Key, r.Rtt, r.SentTime)
			case prober.TIMEOUT:
				mm.Failed(r.Key, r.SentTime, r.Message)
			case prober.FAILED:
				mm.Failed(r.Key, r.SentTime, r.Message)
			}
		}
	}()
}

// autoRegister automatically registers target if not already registered
func (mm *MetricsManager) autoRegister(key, displayName string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	
	if _, exists := mm.metrics[key]; !exists {
		mm.metrics[key] = &Metrics{
			Name: displayName,
		}
	}
}

func (mm *MetricsManager) GetSortedMetricsByKey(k Key) []Metrics {
	mm.mu.Lock()
	var res []Metrics
	for _, m := range mm.metrics {
		res = append(res, *m)
	}
	mm.mu.Unlock()
	if k != Host {
		sort.SliceStable(res, func(i, j int) bool {
			return res[i].Name < res[j].Name
		})
	}
	sort.SliceStable(res, func(i, j int) bool {
		mi := res[i]
		mj := res[j]
		switch k {
		case Host:
			return res[i].Name < res[j].Name
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
