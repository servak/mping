package stats

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/servak/mping/internal/prober"
)

const (
	DefaultHistorySize = 100 // Default number of history entries to keep
)

type metricsManager struct {
	metrics     map[string]*metrics
	historySize int // Number of history entries to keep
	settle      time.Duration
	onFailure   func(name string)
	resetAt     time.Time // last reset; also applies to targets added since
	mu          sync.Mutex

	onEvent func(Event)
	events  eventQueue // not dispatched yet
	eventMu sync.Mutex // held while onEvent runs; taken before mu
}

// Options configures a MetricsManager
type Options struct {
	HistorySize int
	// SettleTime is how long results are buffered before outage detection so
	// that out-of-order results can be applied in send order. It should cover
	// the latest a result can be reported: timeout plus one interval (ICMP
	// timeouts are checked on the interval ticker).
	SettleTime time.Duration
	// OnFailure, if set, is called with the target's display name after each
	// failure (timeout or error) has been recorded. It runs on the event
	// processing goroutine outside the manager's lock, so it must not block,
	// nor wait until processing has ended (see OnEvent).
	OnFailure func(name string)
	// OnEvent, if set, is called with each outage of a target and each change
	// of its reply TTL (ICMP), or its peer or certificate (HTTP(S)).
	//
	// Events are facts of the results in send order, so they are established
	// only once those have settled (see SettleTime). Each is reported once,
	// those of a target in the order in which they were established.
	//
	// Nothing but results drives this: a result settles when a later one of
	// its target arrives. For a regularly probed target an event is therefore
	// reported within about SettleTime + interval + timeout of the probe that
	// establishes it. If results stop, the rest waits until Subscribe's
	// channel is closed: then all results are applied and all events
	// reported, before Subscribe's done channel is closed. Reading metrics
	// settles results too, but does not call the hook; events established
	// that way are reported with the next result of any target.
	//
	// The hook runs on the event processing goroutine (the one Subscribe
	// starts), one call at a time and outside the manager's lock, so it may
	// read metrics. It must not block: no results are processed meanwhile
	// and ResetAllMetrics waits for it. So it must neither call
	// ResetAllMetrics nor wait for a goroutine that may do so. The same goes
	// for waiting until processing has ended (Subscribe's done channel, and
	// thus stopping a session), which waits for the hook.
	//
	// ResetAllMetrics forgets the past silently: once it has returned nothing
	// from before it is reported anymore. An ongoing outage gets no end; if
	// the target stays down, a new outage starts with the probes sent after
	// the reset. The first value seen after the reset is not a change.
	//
	// An outage that is ongoing when Subscribe's channel is closed gets no
	// end either: whether the target has recovered is not known.
	OnEvent func(Event)
}

// SettleTimeFor returns the recommended SettleTime for the probe timing
func SettleTimeFor(interval, timeout time.Duration) time.Duration {
	return interval + timeout + 100*time.Millisecond
}

// Create a new MetricsManager
func NewMetricsManager() MetricsManager {
	return NewMetricsManagerWithOptions(Options{})
}

// NewMetricsManagerWithOptions creates a MetricsManager with custom options
func NewMetricsManagerWithOptions(opts Options) MetricsManager {
	if opts.HistorySize <= 0 {
		opts.HistorySize = DefaultHistorySize
	}
	mm := &metricsManager{
		metrics:     make(map[string]*metrics),
		historySize: opts.HistorySize,
		settle:      opts.SettleTime,
		onFailure:   opts.OnFailure,
		onEvent:     opts.OnEvent,
	}
	return mm
}

func (mm *metricsManager) newMetrics(key, name string) *metrics {
	m := newMetrics(name, mm.historySize, mm.settle)
	m.resetAt = mm.resetAt
	if mm.onEvent != nil {
		m.outage.emit = mm.events.emitter(key, name)
	}
	return m
}

// 指定されたホストのMetricsを取得（内部用）
func (mm *metricsManager) getMetrics(host string) *metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, ok := mm.metrics[host]
	if !ok {
		m = mm.newMetrics(host, host)
		// Register the new metrics for the host
		mm.metrics[host] = m
	}
	return m
}

func (mm *metricsManager) GetMetrics(host string) Metrics {
	m := mm.getMetrics(host)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return m.settledSnapshot(time.Now())
}

func (mm *metricsManager) ResetAllMetrics() {
	if mm.onEvent != nil {
		// Wait for a running dispatch, so that no event from before the
		// reset is reported once it has returned.
		mm.eventMu.Lock()
		defer mm.eventMu.Unlock()
	}
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.events = nil
	mm.resetAt = time.Now()
	for _, m := range mm.metrics {
		m.Reset()
	}
}

// SuccessWithDetails registers a success for host with detailed information
func (mm *metricsManager) SuccessWithDetails(host string, rtt time.Duration, sentTime time.Time, details *prober.ProbeDetails) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	if sentTime.Before(m.resetAt) {
		mm.mu.Unlock()
		return // probe sent before the last reset
	}
	m.Success(rtt, sentTime)
	if m.history != nil {
		m.history.AddEntry(HistoryEntry{
			Timestamp: sentTime,
			RTT:       rtt,
			Success:   true,
			Details:   details,
		})
	}
	m.conn.add(sentTime, details)
	m.outage.addResult(probeResult{sentTime: sentTime, success: true, ttl: replyTTL(details), http: mm.httpDetails(details)}, time.Now())
	dispatch := len(mm.events) > 0
	mm.mu.Unlock()

	if dispatch {
		mm.dispatchEvents()
	}
}

// replyTTL returns the ICMP reply TTL of details, 0 if unknown
func replyTTL(details *prober.ProbeDetails) int {
	if details == nil || details.ICMP == nil {
		return 0
	}
	return details.ICMP.ReplyTTL
}

// httpDetails returns the HTTP details of details, nil if none. They are
// only needed for events.
func (mm *metricsManager) httpDetails(details *prober.ProbeDetails) *prober.HTTPDetails {
	if mm.onEvent == nil || details == nil {
		return nil
	}
	return details.HTTP
}

// Register failure for host
func (mm *metricsManager) Failed(host string, sentTime time.Time, msg string) {
	mm.FailedWithDetails(host, sentTime, msg, nil)
}

// FailedWithDetails registers a failure for host. details, if set, come
// from a response that was received but rejected (e.g. an unexpected HTTP
// status); only its connection info is kept.
func (mm *metricsManager) FailedWithDetails(host string, sentTime time.Time, msg string, details *prober.ProbeDetails) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	if sentTime.Before(m.resetAt) {
		mm.mu.Unlock()
		return // probe sent before the last reset
	}
	name := m.Name
	m.Fail(sentTime, msg)
	if m.history != nil {
		m.history.AddEntry(HistoryEntry{
			Timestamp: sentTime,
			RTT:       0,
			Success:   false,
			Error:     msg,
		})
	}
	m.conn.add(sentTime, details)
	m.outage.addResult(probeResult{sentTime: sentTime, err: msg, http: mm.httpDetails(details)}, time.Now())
	dispatch := len(mm.events) > 0
	mm.mu.Unlock()

	// Called outside the lock so the hooks may read metrics without deadlock
	if mm.onFailure != nil {
		mm.onFailure(name)
	}
	if dispatch {
		mm.dispatchEvents()
	}
}

// Sent counts a sent probe. Probes sent before the last reset are dropped,
// like their results, so a SENT event queued before a reset cannot inflate
// Total afterwards.
func (mm *metricsManager) Sent(host string, sentTime time.Time) {
	m := mm.getMetrics(host)

	mm.mu.Lock()
	if !sentTime.Before(m.resetAt) {
		m.Sent()
	}
	mm.mu.Unlock()
}

func (mm *metricsManager) Subscribe(res <-chan *prober.Event) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Once the channel is closed no more results can arrive, so every
		// buffered result can be applied to outage detection.
		defer mm.flushOutages()
		for r := range res {
			switch r.Result {
			case prober.REGISTER:
				mm.autoRegister(r.Key, r.DisplayName, string(r.ProbeType))
			case prober.SENT:
				mm.Sent(r.Key, r.SentTime)
			case prober.SUCCESS:
				mm.SuccessWithDetails(r.Key, r.Rtt, r.SentTime, r.Details)
			case prober.TIMEOUT, prober.FAILED:
				mm.FailedWithDetails(r.Key, r.SentTime, r.Message, r.Details)
			}
		}
	}()
	return done
}

// autoRegister automatically registers target if not already registered
func (mm *metricsManager) autoRegister(key, displayName, probeType string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	m, exists := mm.metrics[key]
	if !exists {
		m = mm.newMetrics(key, displayName)
		mm.metrics[key] = m
	}
	if probeType != "" {
		m.ProbeType = probeType
	}
}

func (mm *metricsManager) flushOutages() {
	mm.mu.Lock()
	for _, m := range mm.metrics {
		m.outage.flushAll()
	}
	mm.mu.Unlock()
	mm.dispatchEvents()
}

// SortBy sorts metrics by specified key and returns Metrics slice.
// The returned values are snapshots, safe to read while probing continues.
func (mm *metricsManager) SortBy(k Key, ascending bool) []Metrics {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	now := time.Now()
	type item struct {
		key string
		m   Metrics
	}
	items := make([]item, 0, len(mm.metrics))
	for key, m := range mm.metrics {
		items = append(items, item{key, m.settledSnapshot(now)})
	}

	// less must be a strict ordering: descending order swaps the operands
	// instead of negating the result, which would make equal elements
	// "less" than each other and reorder them on every call.
	less := func(mi, mj Metrics) bool {
		switch k {
		case Host:
			return mi.GetName() < mj.GetName()
		case Sent:
			return mi.GetTotal() < mj.GetTotal()
		case Success:
			return mi.GetSuccessful() < mj.GetSuccessful()
		case Loss:
			return mi.GetLoss() < mj.GetLoss()
		case Fail:
			return mi.GetFailed() < mj.GetFailed()
		case Last, Avg, Best, Worst:
			return rttFor(k, mi) < rttFor(k, mj)
		case LastSuccTime:
			return mi.GetLastSuccTime().Before(mj.GetLastSuccTime())
		case LastFailTime:
			return mi.GetLastFailTime().Before(mj.GetLastFailTime())
		}
		return false
	}
	// One sort: primary key, then display name, then key as tie-breakers
	slices.SortFunc(items, func(a, b item) int {
		// Unmeasured RTTs (zero) always go last, whatever the direction
		if za, zb := rttFor(k, a.m) == 0, rttFor(k, b.m) == 0; za != zb {
			if zb {
				return -1
			}
			return 1
		}
		x, y := a.m, b.m
		if !ascending {
			x, y = y, x
		}
		if less(x, y) {
			return -1
		}
		if less(y, x) {
			return 1
		}
		if c := strings.Compare(a.m.GetName(), b.m.GetName()); c != 0 {
			return c
		}
		return strings.Compare(a.key, b.key)
	})
	res := make([]Metrics, len(items))
	for i, it := range items {
		res[i] = it.m
	}
	return res
}

// rttFor returns the RTT a sort key refers to, or -1 for non-RTT keys
func rttFor(k Key, m Metrics) time.Duration {
	switch k {
	case Last:
		return m.GetLastRTT()
	case Avg:
		return m.GetAverageRTT()
	case Best:
		return m.GetMinimumRTT()
	case Worst:
		return m.GetMaximumRTT()
	}
	return -1
}
