package watch

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

const (
	maxTargetOutages  = 5   // kept in a TargetSummary
	maxSummaryTargets = 100 // of a Summary without those that are ok
)

// snapshot is the state of all targets at one moment, worst first.
type snapshot struct {
	at      time.Time
	counts  api.Counts
	targets []api.TargetSummary
}

// takeSnapshot returns the state of the targets of a session, which has settled
// for good if it is the final one.
func takeSnapshot(s Session, final bool) *snapshot {
	snap := &snapshot{at: time.Now()}
	for _, m := range s.Metrics().SortBy(stats.Host, true) {
		t := summarize(m, final)
		switch t.Status {
		case api.StatusDown:
			snap.counts.Down++
		case api.StatusDegraded:
			snap.counts.Degraded++
		case api.StatusPending:
			snap.counts.Pending++
		case api.StatusOK:
			snap.counts.OK++
		}
		snap.targets = append(snap.targets, t)
	}
	slices.SortStableFunc(snap.targets, worstFirst)
	return snap
}

// summarize describes one target; see the statuses of package api.
func summarize(m stats.Metrics, final bool) api.TargetSummary {
	t := api.TargetSummary{
		ConsecutiveFailures: m.GetConsecutiveFailures(),
		TargetReport:        shared.NewTargetReport(m),
		Outages:             []api.Outage{},
	}
	t.Host = api.Clean(t.Host, maxText)
	t.LastFailReason = api.Clean(t.LastFailReason, maxText)
	outages := t.TargetReport.Outages
	t.TargetReport.Outages = nil

	down := false
	for _, o := range outages[max(0, len(outages)-maxTargetOutages):] {
		t.Outages = append(t.Outages, api.Outage{OutageReport: cleanOutage(o), EndUnknown: final && o.Ongoing})
		if down = o.Ongoing; down && t.ConsecutiveFailures > 0 {
			// The history, where they were counted, is limited
			t.ConsecutiveFailures = max(t.ConsecutiveFailures, o.LostProbes)
		}
	}
	switch {
	case t.Success+t.Fail == 0:
		t.Status = api.StatusPending
	case down, t.ConsecutiveFailures >= 3, t.Success == 0:
		t.Status = api.StatusDown
	case t.Fail > 0:
		t.Status = api.StatusDegraded
	default:
		t.Status = api.StatusOK
	}
	return t
}

var statuses = []string{api.StatusDown, api.StatusDegraded, api.StatusPending, api.StatusOK}

// worstFirst compares by status, by what fails now for longest and by what
// failed last.
func worstFirst(a, b api.TargetSummary) int {
	failed := func(t api.TargetSummary) time.Time {
		if t.LastFailAt == nil {
			return time.Time{}
		}
		return *t.LastFailAt
	}
	return cmp.Or(
		cmp.Compare(slices.Index(statuses, a.Status), slices.Index(statuses, b.Status)),
		cmp.Compare(b.ConsecutiveFailures, a.ConsecutiveFailures),
		failed(b).Compare(failed(a)),
		strings.Compare(a.Host, b.Host),
	)
}

// list returns the targets of a summary and how many that are not ok it
// leaves out.
func (s *snapshot) list(all bool) (targets []api.TargetSummary, omitted int) {
	if all {
		return append([]api.TargetSummary{}, s.targets...), 0
	}
	abnormal := len(s.targets) - s.counts.OK
	n := min(abnormal, maxSummaryTargets)
	return append([]api.TargetSummary{}, s.targets[:n]...), abnormal - n
}

// outageEnds returns the events that close the outages of a final snapshot.
func (s *snapshot) outageEnds() []api.Event {
	var events []api.Event
	for _, t := range s.targets {
		if n := len(t.Outages); n > 0 && t.Outages[n-1].EndUnknown {
			o := t.Outages[n-1]
			events = append(events, api.Event{
				Kind: string(stats.EventOutageEnd), Target: t.Host, Time: s.at, RecordedAt: s.at, Outage: &o,
			})
		}
	}
	return events
}
