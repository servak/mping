package watch

import "github.com/servak/mping/internal/api"

// view is what is known of a watch at one moment, without its lock.
type view struct {
	api.Watch
	specs   []string
	gaps    int
	lastGap *api.Gap
	sess    Session
	final   *snapshot
	log     *eventLog
}

func (w *watch) view() view {
	v := view{
		Watch: api.Watch{
			ID: w.id, Name: w.name, State: w.state, Reason: w.reason, Error: w.err,
			StartedAt: w.started, ExpiresAt: w.expires,
			IntervalMs: w.interval.Milliseconds(), TimeoutMs: w.timeout.Milliseconds(),
			SourceInterface: w.source, TargetCount: w.targets,
		},
		specs: w.specs, gaps: w.gaps, lastGap: w.lastGap, sess: w.sess, final: w.final, log: w.log,
	}
	if !w.stopped.IsZero() {
		v.StoppedAt = &w.stopped // never changes again
	}
	return v
}

// snapshot fills in the counts. It calls the session: the lock of the manager
// must not be held.
func (v *view) snapshot() *snapshot {
	snap := v.final
	if snap == nil {
		snap = takeSnapshot(v.sess, false)
		if len(snap.targets) == 0 { // not registered yet
			snap.counts.Pending = v.TargetCount
		}
	}
	v.Counts = snap.counts
	return snap
}

func (m *Manager) lookup(idOrName string) (view, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.get(idOrName)
	if err != nil {
		return view{}, err
	}
	return w.view(), nil
}

// List returns all watches, oldest first.
func (m *Manager) List() []api.Watch {
	m.mu.Lock()
	var views []view
	for _, w := range m.watches {
		if w.state != "" {
			views = append(views, w.view())
		}
	}
	m.mu.Unlock()
	watches := []api.Watch{}
	for _, v := range views {
		v.snapshot()
		watches = append(watches, v.Watch)
	}
	return watches
}

// Get returns a watch by ID or name.
func (m *Manager) Get(idOrName string) (api.WatchDetail, error) {
	v, err := m.lookup(idOrName)
	if err != nil {
		return api.WatchDetail{}, err
	}
	v.snapshot()
	return api.WatchDetail{Watch: v.Watch, Targets: v.specs}, nil
}

// Summary returns the state of a watch, with all targets or those that are not
// ok.
func (m *Manager) Summary(idOrName string, all bool) (api.Summary, error) {
	v, err := m.lookup(idOrName)
	if err != nil {
		return api.Summary{}, err
	}
	// Before the snapshot, so that no event goes unnoticed between the two
	last, dropped := v.log.seqs()
	snap := v.snapshot()
	targets, omitted := snap.list(all)
	return api.Summary{
		Watch: v.Watch, AsOf: snap.at, Targets: targets, OmittedTargets: omitted,
		GapCount: v.gaps, LastGap: v.lastGap, LastEventSeq: last, DroppedEvents: dropped,
	}, nil
}

// Events returns at most limit events of a watch after a sequence number (>= 0).
func (m *Manager) Events(idOrName string, after int64, limit int) (api.Events, error) {
	v, err := m.lookup(idOrName)
	if err != nil {
		return api.Events{}, err
	}
	events := v.log.read(after, limit)
	events.WatchID = v.ID
	return events, nil
}
