package stats

import (
	"sort"
	"sync"
	"time"

	"github.com/servak/mping/internal/prober"
)

const (
	DefaultHistorySize = 100 // デフォルトの履歴保持数
)

type MetricsManager struct {
	metrics     map[string]*Metrics
	historySize int // 履歴保持数
	mu          sync.Mutex
}

// 新しいMetricsManagerを生成
func NewMetricsManager() *MetricsManager {
	return NewMetricsManagerWithHistorySize(DefaultHistorySize)
}

// 履歴サイズを指定してMetricsManagerを生成
func NewMetricsManagerWithHistorySize(historySize int) *MetricsManager {
	return &MetricsManager{
		metrics:     make(map[string]*Metrics),
		historySize: historySize,
	}
}

func (mm *MetricsManager) Register(target, name string) {
	v, ok := mm.metrics[target]
	if ok && v.Name != target {
		return
	}
	mm.metrics[target] = &Metrics{
		Name:    name,
		history: NewTargetHistory(mm.historySize),
	}
}

// 指定されたホストのMetricsを取得
func (mm *MetricsManager) GetMetrics(host string) *Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		m = &Metrics{
			Name:    host,
			history: NewTargetHistory(mm.historySize),
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
	mm.SuccessWithDetails(host, rtt, sentTime, nil)
}

// ホストに対する成功を詳細情報付きで登録
func (mm *MetricsManager) SuccessWithDetails(host string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Success(rtt, sentTime)
	if m.history != nil {
		m.history.AddEntry(HistoryEntry{
			Timestamp: sentTime,
			RTT:       rtt,
			Success:   true,
			Details:   details,
		})
	}
	mm.mu.Unlock()
}

// ホストに対する失敗を登録
func (mm *MetricsManager) Failed(host string, sentTime time.Time, msg string) {
	m := mm.GetMetrics(host)

	mm.mu.Lock()
	m.Fail(sentTime, msg)
	if m.history != nil {
		m.history.AddEntry(HistoryEntry{
			Timestamp: sentTime,
			RTT:       0,
			Success:   false,
			Error:     msg,
		})
	}
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
				mm.SuccessWithDetails(r.Key, r.Rtt, r.SentTime, r.Details)
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
			Name:    displayName,
			history: NewTargetHistory(mm.historySize),
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
			result = mi.Total < mj.Total
		case Success:
			result = mi.Successful < mj.Successful
		case Loss:
			result = mi.Loss < mj.Loss
		case Fail:
			result = mi.Failed < mj.Failed
		case Last:
			result = rejectLessAscending(mi.LastRTT, mj.LastRTT)
		case Avg:
			result = rejectLessAscending(mi.AverageRTT, mj.AverageRTT)
		case Best:
			result = rejectLessAscending(mi.MinimumRTT, mj.MinimumRTT)
		case Worst:
			result = rejectLessAscending(mi.MaximumRTT, mj.MaximumRTT)
		case LastSuccTime:
			result = mi.LastSuccTime.Before(mj.LastSuccTime)
		case LastFailTime:
			result = mi.LastFailTime.Before(mj.LastFailTime)
		default:
			return false
		}

		if ascending {
			return result
		} else {
			return !result
		}
	})
	return res
}

// SortByWithReader は MetricsReader インターフェースを使用するバージョン
func (mm *MetricsManager) SortByWithReader(k Key, ascending bool) []MetricsReader {
	mm.mu.Lock()
	var res []MetricsReader
	for _, m := range mm.metrics {
		res = append(res, m)
	}
	mm.mu.Unlock()
	
	if k != Host {
		sort.SliceStable(res, func(i, j int) bool {
			return res[i].GetName() < res[j].GetName()
		})
	}
	sort.SliceStable(res, func(i, j int) bool {
		mi := res[i]
		mj := res[j]
		var result bool
		switch k {
		case Host:
			result = mi.GetName() < mj.GetName()
		case Sent:
			result = mi.GetTotal() < mj.GetTotal()
		case Success:
			result = mi.GetSuccessful() < mj.GetSuccessful()
		case Loss:
			result = mi.GetLoss() < mj.GetLoss()
		case Fail:
			result = mi.GetFailed() < mj.GetFailed()
		case Last:
			result = rejectLessAscending(mi.GetLastRTT(), mj.GetLastRTT())
		case Avg:
			result = rejectLessAscending(mi.GetAverageRTT(), mj.GetAverageRTT())
		case Best:
			result = rejectLessAscending(mi.GetMinimumRTT(), mj.GetMinimumRTT())
		case Worst:
			result = rejectLessAscending(mi.GetMaximumRTT(), mj.GetMaximumRTT())
		case LastSuccTime:
			result = mi.GetLastSuccTime().Before(mj.GetLastSuccTime())
		case LastFailTime:
			result = mi.GetLastFailTime().Before(mj.GetLastFailTime())
		default:
			return false
		}

		if ascending {
			return result
		} else {
			return !result
		}
	})
	return res
}

// GetAllTargets は全ターゲットのリストを取得
func (mm *MetricsManager) GetAllTargets() []string {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	
	var targets []string
	for target := range mm.metrics {
		targets = append(targets, target)
	}
	return targets
}

// GetTargetHistory は指定ターゲットの履歴を取得
func (mm *MetricsManager) GetTargetHistory(target string, n int) []HistoryEntry {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	
	if m, exists := mm.metrics[target]; exists && m.history != nil {
		return m.history.GetRecentEntries(n)
	}
	return []HistoryEntry{}
}

// GetAllTargetsRecentHistory は全ターゲットの最新履歴を取得
func (mm *MetricsManager) GetAllTargetsRecentHistory(n int) map[string][]HistoryEntry {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	
	result := make(map[string][]HistoryEntry)
	for target, m := range mm.metrics {
		if m.history != nil {
			result[target] = m.history.GetRecentEntries(n)
		}
	}
	return result
}

// GetMetricsAsReader は MetricsReader インターフェースとして取得
func (mm *MetricsManager) GetMetricsAsReader(target string) MetricsReader {
	return mm.GetMetrics(target)
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
