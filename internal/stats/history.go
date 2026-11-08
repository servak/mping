package stats

import (
	"sort"
	"time"

	"github.com/servak/mping/internal/prober"
)

// History entry
type HistoryEntry struct {
	Timestamp time.Time            `json:"timestamp"`
	RTT       time.Duration        `json:"rtt"`
	Success   bool                 `json:"success"`
	Error     string               `json:"error,omitempty"`
	Details   *prober.ProbeDetails `json:"details,omitempty"`
}

// Structure to manage history for each target (ring buffer)
type TargetHistory struct {
	entries []HistoryEntry
	size    int // リングバッファサイズ
	index   int // 現在の書き込み位置
	count   int // 実際のエントリ数
}

// NewTargetHistory creates a new TargetHistory
func NewTargetHistory(size int) *TargetHistory {
	if size < 1 {
		size = 1
	}
	return &TargetHistory{
		entries: make([]HistoryEntry, size),
		size:    size,
		index:   0,
		count:   0,
	}
}

// AddEntry adds a new history entry. Entries are kept ordered by Timestamp
// (send time), so a late result for an older probe is inserted before newer
// ones instead of being appended at the newest end.
func (th *TargetHistory) AddEntry(entry HistoryEntry) {
	if th.count > 0 {
		newest := th.entries[(th.index-1+th.size)%th.size]
		if entry.Timestamp.Before(newest.Timestamp) {
			th.insertOrdered(entry)
			return
		}
	}
	th.entries[th.index] = entry
	th.index = (th.index + 1) % th.size
	if th.count < th.size {
		th.count++
	}
}

// insertOrdered inserts an entry that is older than the newest one at its
// chronological position, dropping the oldest entry if the buffer is full.
func (th *TargetHistory) insertOrdered(entry HistoryEntry) {
	list := make([]HistoryEntry, 0, th.count+1)
	for i := th.count - 1; i >= 0; i-- {
		list = append(list, th.entries[(th.index-1-i+th.size)%th.size])
	}
	// after any entries with an equal timestamp
	pos := sort.Search(len(list), func(i int) bool { return list[i].Timestamp.After(entry.Timestamp) })
	list = append(list, HistoryEntry{})
	copy(list[pos+1:], list[pos:])
	list[pos] = entry
	if len(list) > th.size {
		list = list[len(list)-th.size:]
	}
	copy(th.entries, list)
	th.count = len(list)
	th.index = th.count % th.size
}

// GetRecentEntries retrieves the latest n entries (newest first)
func (th *TargetHistory) GetRecentEntries(n int) []HistoryEntry {
	if n <= 0 || th.count == 0 {
		return []HistoryEntry{}
	}

	if n > th.count {
		n = th.count
	}

	result := make([]HistoryEntry, n)
	for i := 0; i < n; i++ {
		// Retrieve from newest to oldest
		pos := (th.index - 1 - i + th.size) % th.size
		result[i] = th.entries[pos]
	}

	return result
}

// GetEntriesSince retrieves entries since the specified time
func (th *TargetHistory) GetEntriesSince(since time.Time) []HistoryEntry {
	if th.count == 0 {
		return []HistoryEntry{}
	}

	var result []HistoryEntry
	for i := 0; i < th.count; i++ {
		pos := (th.index - 1 - i + th.size) % th.size
		entry := th.entries[pos]
		if entry.Timestamp.After(since) || entry.Timestamp.Equal(since) {
			result = append(result, entry)
		} else {
			break // Reached older entries, so stop
		}
	}

	return result
}

// GetConsecutiveFailures retrieves the number of consecutive failures
func (th *TargetHistory) GetConsecutiveFailures() int {
	if th.count == 0 {
		return 0
	}

	count := 0
	for i := 0; i < th.count; i++ {
		pos := (th.index - 1 - i + th.size) % th.size
		entry := th.entries[pos]
		if !entry.Success {
			count++
		} else {
			break
		}
	}

	return count
}

// GetConsecutiveSuccesses retrieves the number of consecutive successes
func (th *TargetHistory) GetConsecutiveSuccesses() int {
	if th.count == 0 {
		return 0
	}

	count := 0
	for i := 0; i < th.count; i++ {
		pos := (th.index - 1 - i + th.size) % th.size
		entry := th.entries[pos]
		if entry.Success {
			count++
		} else {
			break
		}
	}

	return count
}

// GetSuccessRateInPeriod retrieves the success rate within the specified period
func (th *TargetHistory) GetSuccessRateInPeriod(duration time.Duration) float64 {
	if th.count == 0 {
		return 0.0
	}

	since := time.Now().Add(-duration)
	entries := th.GetEntriesSince(since)

	if len(entries) == 0 {
		return 0.0
	}

	successCount := 0
	for _, entry := range entries {
		if entry.Success {
			successCount++
		}
	}

	return float64(successCount) / float64(len(entries)) * 100.0
}

// clone returns a deep copy of the history
func (th *TargetHistory) clone() *TargetHistory {
	c := *th
	c.entries = append([]HistoryEntry(nil), th.entries...)
	return &c
}

// Clear clears the history
func (th *TargetHistory) Clear() {
	th.index = 0
	th.count = 0
}
