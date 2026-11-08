// Package trace implements an MTR-style path tracer: it repeatedly sends
// probes with increasing TTL and keeps per-hop loss and RTT statistics.
//
// Probes are ICMP echo requests (ProtoICMP) or UDP datagrams to rotating
// traceroute ports (ProtoUDP). The transport only sends probes and matches
// ICMP replies back to them; rounds, timeouts, late replies and the per-hop /
// per-responder statistics are shared by both protocols.
package trace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultInterval = time.Second
	DefaultTimeout  = time.Second

	maxHops       = 30
	maxPacketSize = 1500

	// maxUnknownHops is how many TTLs past the farthest hop that has replied
	// are probed while the destination is unknown. Home routers treat a
	// burst of probes up to maxHops every round as an ICMP flood and drop
	// ICMP for every host behind them. mtr stops after 5 unknown hops; this
	// allows more because many routers only answer occasionally.
	maxUnknownHops = 8

	// maxRespondersPerHop caps the per-responder statistics kept for one
	// TTL. Further responders are still counted in the hop totals.
	maxRespondersPerHop = 16

	// staleAfterRounds is how many rounds a responder may stay silent before
	// it is flagged Stale; hideAfterRounds is when it is dropped from the
	// snapshot (it stays in the hop totals).
	staleAfterRounds = 10
	hideAfterRounds  = 30
)

// Proto is the protocol of the probes
type Proto string

const (
	// ProtoICMP sends ICMP echo requests with a fixed echo ID: every probe
	// is the same flow, so ECMP routers send all of them down one branch,
	// like the probes of mping's ICMP prober.
	ProtoICMP Proto = "icmp"
	// ProtoUDP sends UDP datagrams to rotating destination ports, so probes
	// spread over the ECMP branches like TCP/UDP based probes (which use a
	// new source port per probe).
	ProtoUDP Proto = "udp"
)

// Config configures a Tracer
type Config struct {
	Interval time.Duration // time between rounds
	Timeout  time.Duration // a probe without reply after this is lost
	NoDNS    bool          // skip reverse DNS lookups of hop addresses
	Proto    Proto         // probe protocol (default ProtoICMP)
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Proto == "" {
		c.Proto = ProtoICMP
	}
	return c
}

// Hop is a snapshot of statistics for one TTL
type Hop struct {
	TTL    int
	Addr   string // dominant responder: most replies, ties to the earliest seen ("" if none)
	Name   string // reverse DNS name of Addr, if resolved
	Others int    // number of other visible responders (len(Responders)-1)
	Sent   int    // probes that have been answered or timed out
	Recv   int
	Last   time.Duration
	Avg    time.Duration
	Best   time.Duration
	Worst  time.Duration

	// Responders lists every visible responder in first-seen order
	Responders []Responder
}

// Responder is the per-address statistics of one hop (ECMP / load balancing
// shows several responders on the same TTL)
type Responder struct {
	Addr  string
	Name  string // reverse DNS name, if resolved
	Recv  int
	Last  time.Duration
	Avg   time.Duration
	Best  time.Duration
	Worst time.Duration
	Stale bool // not seen for more than staleAfterRounds rounds
}

// Loss returns the loss percentage of settled probes
func (h Hop) Loss() float64 {
	if h.Sent == 0 {
		return 0
	}
	return float64(h.Sent-h.Recv) / float64(h.Sent) * 100
}

// Result is a snapshot of a trace
type Result struct {
	Dst        net.IP
	Rounds     int
	Hops       []Hop
	Proto      Proto
	Reached    bool // the destination (or an unreachable report) ended the path
	Privileged bool // raw socket in use; false means an unprivileged fallback
}

// HasRateLimitedHops reports whether an intermediate hop shows loss while the
// destination does not. That is almost always ICMP rate limiting on the
// router (Time Exceeded generation is low priority), not real packet loss.
func (r Result) HasRateLimitedHops() bool {
	if !r.Reached || len(r.Hops) < 2 {
		return false
	}
	final := r.Hops[len(r.Hops)-1]
	if final.Sent == 0 || final.Loss() > 0 {
		return false
	}
	for _, h := range r.Hops[:len(r.Hops)-1] {
		if h.Recv > 0 && h.Loss() > 0 {
			return true
		}
	}
	return false
}

type respStats struct {
	recv       int
	last       time.Duration
	best       time.Duration
	worst      time.Duration
	sum        float64
	firstRound int
	lastRound  int
}

type hopStats struct {
	sent, recv int
	last       time.Duration
	best       time.Duration
	worst      time.Duration
	sum        float64
	resp       map[string]*respStats
	order      []string // responder addresses in first-seen order

	// rounds of the latest probe to this TTL answered by the end of the
	// path (echo reply / unreachable) and by a Time Exceeded (0 = never)
	lastEnd, lastTE int
}

// record counts a reply from addr received for a probe of the given round;
// now is the tracer's current round.
func (h *hopStats) record(addr string, rtt time.Duration, round, now int) {
	h.recv++
	h.last = rtt
	if h.best == 0 || rtt < h.best {
		h.best = rtt
	}
	if rtt > h.worst {
		h.worst = rtt
	}
	h.sum += float64(rtt)

	r := h.resp[addr]
	if r == nil {
		if len(h.order) >= maxRespondersPerHop && !h.evictStale(now) {
			return // counted in the hop totals only
		}
		if h.resp == nil {
			h.resp = map[string]*respStats{}
		}
		r = &respStats{firstRound: round}
		h.resp[addr] = r
		h.order = append(h.order, addr)
	}
	r.recv++
	r.last = rtt
	if r.best == 0 || rtt < r.best {
		r.best = rtt
	}
	if rtt > r.worst {
		r.worst = rtt
	}
	r.sum += float64(rtt)
	if round > r.lastRound {
		r.lastRound = round
	}
}

// evictStale drops the responder silent for the longest time if it is at
// least stale (hidden responders are the oldest, so they go first), making
// room for a new one. It reports whether a responder was dropped.
func (h *hopStats) evictStale(now int) bool {
	victim := -1
	for i, addr := range h.order {
		if victim < 0 || h.resp[addr].lastRound < h.resp[h.order[victim]].lastRound {
			victim = i
		}
	}
	if victim < 0 || now-h.resp[h.order[victim]].lastRound <= staleAfterRounds {
		return false
	}
	delete(h.resp, h.order[victim])
	h.order = append(h.order[:victim], h.order[victim+1:]...)
	return true
}

type inflight struct {
	ttl   int
	round int
	sent  time.Time
}

// probeEvent describes the outcome of a single probe. It is only used by
// tests (via Tracer.onEvent) to verify per-probe accounting.
type probeEvent struct {
	round, ttl int
	addr       string
	rtt        time.Duration
	outcome    probeOutcome
}

type probeOutcome int

const (
	outcomeReply probeOutcome = iota + 1
	outcomeLost               // expired without reply
	outcomeLate               // reply arrived after the probe was counted as lost
)

// Tracer traces the path to a destination
type Tracer struct {
	cfg Config
	dst net.IP
	tp  transport

	mu      sync.Mutex
	seq     uint16
	pending map[uint16]inflight // by probe key: ICMP echo seq / UDP dst port
	hops    []hopStats          // index = ttl-1
	maxTTL  int                 // shrinks to the destination's distance once known
	reached bool
	rounds  int
	names   map[string]string // reverse DNS cache ("" = lookup in progress/failed)

	onEvent func(probeEvent) // test hook, called with mu held

	closeOnce sync.Once
}

// New opens the sockets for tracing target with cfg.Proto. See
// newICMPTransport and newUDPTransport for the sockets used per platform.
func New(target string, cfg Config) (*Tracer, error) {
	cfg = cfg.withDefaults()
	host, preferV6 := HostFromTarget(target)
	dst, err := Resolve(host, preferV6)
	if err != nil {
		return nil, err
	}
	v6 := dst.To4() == nil

	var tp transport
	switch cfg.Proto {
	case ProtoICMP:
		tp, err = newICMPTransport(dst, v6)
	case ProtoUDP:
		tp, err = newUDPTransport(dst, v6)
	default:
		err = fmt.Errorf("unknown trace protocol %q", cfg.Proto)
	}
	if err != nil {
		return nil, err
	}
	return newTracer(cfg, dst, tp), nil
}

func newTracer(cfg Config, dst net.IP, tp transport) *Tracer {
	return &Tracer{
		cfg:     cfg.withDefaults(),
		dst:     dst,
		tp:      tp,
		pending: map[uint16]inflight{},
		hops:    make([]hopStats, maxHops),
		maxTTL:  maxHops,
		names:   map[string]string{},
	}
}

// Close releases the sockets
func (t *Tracer) Close() error {
	var err error
	t.closeOnce.Do(func() { err = t.tp.close() })
	return err
}

// Run sends `rounds` rounds of probes (forever when rounds <= 0) until ctx
// is done, then waits up to Timeout for outstanding replies. Run closes the
// tracer when it returns.
func (t *Tracer) Run(ctx context.Context, rounds int) error {
	defer t.Close()

	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		t.tp.receive(t.handle)
	}()

	ticker := time.NewTicker(t.cfg.Interval)
	defer ticker.Stop()
	var sendErr error
	for i := 0; rounds <= 0 || i < rounds; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return t.drain(recvDone, nil)
			case <-ticker.C:
			}
		}
		if err := t.sendRound(ctx); err != nil {
			sendErr = err
			break
		}
	}

	// Wait for replies to the last round
	select {
	case <-ctx.Done():
	case <-time.After(t.cfg.Timeout):
	}
	return t.drain(recvDone, sendErr)
}

func (t *Tracer) drain(recvDone <-chan struct{}, err error) error {
	t.Close()
	<-recvDone
	t.mu.Lock()
	t.expire(time.Now().Add(t.cfg.Timeout)) // everything unanswered is lost
	t.mu.Unlock()
	return err
}

// sendRound probes TTL 1..probeLimit, spread evenly over the interval (like
// mtr) so that a round never reaches routers as a burst.
func (t *Tracer) sendRound(ctx context.Context) error {
	t.mu.Lock()
	t.expire(time.Now())
	t.rounds++
	round := t.rounds
	t.updateReachLocked()
	maxTTL := t.probeLimitLocked()
	t.mu.Unlock()

	gap := t.cfg.Interval / time.Duration(maxTTL)
	for ttl := 1; ttl <= maxTTL; ttl++ {
		if ttl > 1 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(gap):
			}
		}
		t.mu.Lock()
		key := t.probeKeyLocked(round, ttl)
		t.pending[key] = inflight{ttl: ttl, round: round, sent: time.Now()}
		t.mu.Unlock()

		if err := t.tp.send(ttl, key); err != nil {
			if errors.Is(err, net.ErrClosed) {
				t.mu.Lock()
				delete(t.pending, key) // not sent: neither lost nor answered
				t.mu.Unlock()
				return nil
			}
			if isTransientSendErr(err) {
				// e.g. no route right now, a local firewall, full
				// buffers: the probe is lost, the trace goes on
				t.mu.Lock()
				if p, ok := t.pending[key]; ok {
					t.loseLocked(key, p)
				}
				t.mu.Unlock()
				continue
			}
			t.mu.Lock()
			delete(t.pending, key)
			t.mu.Unlock()
			return fmt.Errorf("send ttl=%d: %w", ttl, err)
		}
	}
	return nil
}

// isTransientSendErr reports whether a failed send only loses that probe:
// the errnos a send reports for a local routing problem, a firewall rule,
// full buffers or an ICMP error of an earlier probe. Anything else (a broken
// socket) ends the trace.
func isTransientSendErr(err error) bool {
	for _, e := range []syscall.Errno{
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENETDOWN, syscall.EHOSTDOWN,
		syscall.ENOBUFS, syscall.ENOMEM, syscall.EAGAIN, syscall.EPERM, syscall.EACCES,
		syscall.ECONNREFUSED, syscall.EPROTO, syscall.EMSGSIZE, syscall.EADDRNOTAVAIL,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// probeKeyLocked returns the key of the next probe: the next echo sequence
// for ICMP, the rotating destination port for UDP. A probe still pending
// under the key (only possible after the key space wrapped, after its
// timeout; see udpPortRange) is counted as lost first.
func (t *Tracer) probeKeyLocked(round, ttl int) uint16 {
	var key uint16
	if t.cfg.Proto == ProtoUDP {
		key = udpPort(round, ttl)
	} else {
		t.seq++
		key = t.seq
	}
	if p, ok := t.pending[key]; ok {
		t.loseLocked(key, p)
	}
	return key
}

func (t *Tracer) handle(r reply, addr string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.pending[r.seq]
	if !ok {
		if t.onEvent != nil {
			t.onEvent(probeEvent{addr: addr, outcome: outcomeLate})
		}
		return // late (already counted as lost) or unknown
	}
	delete(t.pending, r.seq)

	h := &t.hops[p.ttl-1]
	h.sent++
	h.record(addr, now.Sub(p.sent), p.round, max(t.rounds, p.round))
	if t.onEvent != nil {
		t.onEvent(probeEvent{round: p.round, ttl: p.ttl, addr: addr, rtt: now.Sub(p.sent), outcome: outcomeReply})
	}

	if r.kind == kindEchoReply || r.kind == kindUnreachable {
		h.lastEnd = max(h.lastEnd, p.round)
	} else {
		h.lastTE = max(h.lastTE, p.round)
	}
	t.updateReachLocked()
	if !t.cfg.NoDNS {
		t.lookupLocked(addr)
	}
}

// updateReachLocked re-evaluates where the path ends. The destination is the
// nearest TTL answered by the end of the path (echo reply / unreachable)
// unless a Time Exceeded came from that TTL or beyond after that answer or
// within the last staleAfterRounds rounds: then the path goes on past it (a
// transient unreachable, a longer route, or a longer ECMP branch). With no
// such TTL the destination is unknown again and probing goes beyond.
func (t *Tracer) updateReachLocked() {
	dst, newestTE := 0, 0
	for i := len(t.hops) - 1; i >= 0; i-- {
		h := &t.hops[i]
		newestTE = max(newestTE, h.lastTE) // newest Time Exceeded at TTL >= i+1
		if h.lastEnd == 0 {
			continue
		}
		if newestTE > 0 && (newestTE > h.lastEnd || t.rounds-newestTE < staleAfterRounds) {
			continue
		}
		dst = i + 1
	}
	t.reached = dst > 0
	t.maxTTL = maxHops
	if t.reached {
		t.maxTTL = dst
	}
}

// farthestResponderLocked returns the highest TTL that has replied (0 if none)
func (t *Tracer) farthestResponderLocked() int {
	last := 0
	for i := 0; i < t.maxTTL; i++ {
		if t.hops[i].recv > 0 {
			last = i + 1
		}
	}
	return last
}

// probeLimitLocked returns the highest TTL to probe this round: the
// destination once it is known, otherwise maxUnknownHops past the farthest
// hop that has replied.
func (t *Tracer) probeLimitLocked() int {
	if t.reached {
		return t.maxTTL
	}
	return min(t.maxTTL, t.farthestResponderLocked()+maxUnknownHops)
}

// expire counts pending probes sent before now-Timeout as lost
func (t *Tracer) expire(now time.Time) {
	for key, p := range t.pending {
		if now.Sub(p.sent) >= t.cfg.Timeout {
			t.loseLocked(key, p)
		}
	}
}

// loseLocked counts the pending probe p as lost
func (t *Tracer) loseLocked(key uint16, p inflight) {
	t.hops[p.ttl-1].sent++
	delete(t.pending, key)
	if t.onEvent != nil {
		t.onEvent(probeEvent{round: p.round, ttl: p.ttl, outcome: outcomeLost})
	}
}

func (t *Tracer) lookupLocked(addr string) {
	if _, ok := t.names[addr]; ok {
		return
	}
	t.names[addr] = ""
	go func() {
		names, err := net.LookupAddr(addr)
		if err != nil || len(names) == 0 {
			return
		}
		name := names[0]
		if len(name) > 1 && name[len(name)-1] == '.' {
			name = name[:len(name)-1]
		}
		t.mu.Lock()
		t.names[addr] = name
		t.mu.Unlock()
	}()
}

// Snapshot returns the current statistics
func (t *Tracer) Snapshot() Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expire(time.Now())

	// Show hops up to the destination, or up to the farthest responder
	last := t.farthestResponderLocked()
	if t.reached {
		last = t.maxTTL
	}

	res := Result{
		Dst:        t.dst,
		Rounds:     t.rounds,
		Proto:      t.cfg.Proto,
		Reached:    t.reached,
		Privileged: t.tp != nil && t.tp.privileged(),
		Hops:       make([]Hop, 0, last),
	}
	for i := 0; i < last; i++ {
		h := &t.hops[i]
		hop := Hop{
			TTL:   i + 1,
			Sent:  h.sent,
			Recv:  h.recv,
			Last:  h.last,
			Best:  h.best,
			Worst: h.worst,
		}
		if h.recv > 0 {
			hop.Avg = time.Duration(h.sum / float64(h.recv))
		}
		dominant := -1
		// The most recently seen responder (earliest seen on ties) is never
		// hidden, so a rate-limited hop that replies only now and then keeps
		// its address. Others are hidden once silent for hideAfterRounds and
		// a more recent responder exists (the path moved).
		latest := ""
		for _, addr := range h.order {
			if latest == "" || h.resp[addr].lastRound > h.resp[latest].lastRound {
				latest = addr
			}
		}
		for _, addr := range h.order {
			r := h.resp[addr]
			age := t.rounds - r.lastRound
			if age > hideAfterRounds && addr != latest {
				continue
			}
			resp := Responder{
				Addr:  addr,
				Name:  t.names[addr],
				Recv:  r.recv,
				Last:  r.last,
				Avg:   time.Duration(r.sum / float64(r.recv)),
				Best:  r.best,
				Worst: r.worst,
				Stale: age > staleAfterRounds,
			}
			// strict > keeps the earliest seen responder on ties
			if dominant < 0 || resp.Recv > hop.Responders[dominant].Recv {
				dominant = len(hop.Responders)
			}
			hop.Responders = append(hop.Responders, resp)
		}
		if dominant >= 0 {
			hop.Addr = hop.Responders[dominant].Addr
			hop.Name = hop.Responders[dominant].Name
			hop.Others = len(hop.Responders) - 1
		}
		res.Hops = append(res.Hops, hop)
	}
	return res
}
