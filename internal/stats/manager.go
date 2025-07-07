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
			switch r.Result {
			case prober.REGISTER:
				mm.autoRegister(r.Key, r.DisplayName)
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

func (mm *MetricsManager) SortBy(k Key, ascending bool) []Metrics {
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
		var result bool
		switch k {
		case Host:
			result = res[i].Name < res[j].Name
		case Sent:
			result = mi.Total < mj.Total  // 昇順：小さい値が先
		case Success:
			result = mi.Successful < mj.Successful  // 昇順：小さい値が先
		case Loss:
			result = mi.Loss < mj.Loss  // 昇順：小さい値が先
		case Fail:
			result = mi.Failed < mj.Failed  // 昇順：小さい値が先
		case Last:
			result = rejectLessAscending(mi.LastRTT, mj.LastRTT)  // 昇順対応
		case Avg:
			result = rejectLessAscending(mi.AverageRTT, mj.AverageRTT)  // 昇順対応
		case Best:
			result = rejectLessAscending(mi.MinimumRTT, mj.MinimumRTT)  // 昇順対応
		case Worst:
			result = rejectLessAscending(mi.MaximumRTT, mj.MaximumRTT)  // 昇順対応
		case LastSuccTime:
			result = mi.LastSuccTime.Before(mj.LastSuccTime)  // 昇順：古い時刻が先
		case LastFailTime:
			result = mi.LastFailTime.Before(mj.LastFailTime)  // 昇順：古い時刻が先
		default:
			return false
		}

		// ascending=falseの場合は結果を反転
		if ascending {
			return result
		} else {
			return !result
		}
	})
	return res
}

// rejectLessAscending は昇順ソート用のRTT比較関数
// 0値（未測定）は常に後ろに配置される
func rejectLessAscending(i, j time.Duration) bool {
	if i == 0 {
		return false  // i が 0 なら j を先に
	}
	if j == 0 {
		return true   // j が 0 なら i を先に
	}
	return i < j      // 両方とも 0 でないなら小さい方を先に
}
