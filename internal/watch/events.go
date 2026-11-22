package watch

import (
	"net/netip"
	"sync"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

// maxText is where errors and the names of targets are cut.
const maxText = 300

// eventLog keeps the latest events of a watch. Its lock is taken last: nothing
// is called with it held.
type eventLog struct {
	size int

	mu     sync.Mutex
	events []api.Event // ring: the event with sequence number n is at (n-1)%size
	last   int64       // sequence number
	closed bool
}

// add numbers and appends e, unless the log is closed.
func (l *eventLog) add(e api.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.last++
	e.Seq = l.last
	if len(l.events) < l.size {
		l.events = append(l.events, e)
	} else {
		l.events[(l.last-1)%int64(l.size)] = e
	}
	l.closed = e.Kind == api.EventWatchStopped
}

// seqs returns the sequence number of the last event and how many are no longer
// kept.
func (l *eventLog) seqs() (last, dropped int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last, l.last - int64(len(l.events))
}

// read returns at most limit events after the one with sequence number after.
// Beyond the last one there are none, and next is after.
func (l *eventLog) read(after int64, limit int) api.Events {
	l.mu.Lock()
	defer l.mu.Unlock()
	res := api.Events{Events: []api.Event{}}
	if dropped := l.last - int64(len(l.events)); after < dropped {
		after, res.Truncated = dropped, true
	}
	for ; after < l.last && len(res.Events) < limit; after++ {
		res.Events = append(res.Events, l.events[after%int64(l.size)])
	}
	res.Next, res.More = after, after < l.last
	return res
}

// addStats is the stats.Options.OnEvent of the watch.
func (l *eventLog) addStats(e stats.Event) {
	ev := api.Event{
		Kind:       string(e.Kind),
		Target:     api.Clean(e.Name, maxText),
		Time:       e.Time,
		RecordedAt: e.RecordedAt,
	}
	switch {
	case e.Outage != nil:
		ev.Outage = &api.Outage{OutageReport: cleanOutage(shared.NewOutageReport(*e.Outage, e.RecordedAt))}
	case e.TTL != nil:
		ev.TTL = &api.TTLChange{Old: e.TTL.Old, New: e.TTL.New}
		if hops, ok := e.TTL.HopsDelta(); ok {
			ev.TTL.HopsDelta = &hops
		}
	case e.Peer != nil:
		ev.Peer = &api.PeerChange{Old: addrPort(e.Peer.Old), New: addrPort(e.Peer.New)}
	case e.Cert != nil:
		ev.Cert = &api.CertChange{OldNotAfter: e.Cert.Old.NotAfter, NewNotAfter: e.Cert.New.NotAfter}
	}
	l.add(ev)
}

// addrPort returns s if it is an IP:port, else "".
func addrPort(s string) string {
	a, err := netip.ParseAddrPort(s)
	if err != nil {
		return ""
	}
	return a.String()
}

func cleanOutage(o shared.OutageReport) shared.OutageReport {
	o.LastError = api.Clean(o.LastError, maxText)
	return o
}
