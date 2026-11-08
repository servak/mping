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

	// staleAfterRounds is how many revisits a responder may miss before it
	// is flagged Stale; hideAfterRounds is when it is dropped from the
	// snapshot (it stays in the hop totals) and may be evicted from a full
	// table. A revisit is the number of rounds the responder (or, until it
	// has been seen twice, the hop's responders) is usually seen apart: 1
	// for a single path, about N for N ECMP branches, so the thresholds are
	// rounds on a single path and scale with the branches of a UDP trace.
	staleAfterRounds = 10
	hideAfterRounds  = 30

	// The destination is the TTL after the farthest Time Exceeded seen in
	// the last pathWindow rounds (the end of the longest ECMP branch). The
	// window is twice the number of flows (UDP ports; each TTL uses every
	// flow once per cycle, so every branch shows up within it), clamped to
	// [minPathWindow, maxPathWindow]. It is the hysteresis of the path
	// length: a shorter path is adopted only once the far hops have been
	// silent for that long.
	minPathWindow = 2 * hideAfterRounds
	maxPathWindow = 600
	// endExpireRounds is how long an answer from the end of the path (echo
	// reply / unreachable) at a TTL keeps that TTL a destination candidate.
	// A destination not answering for staleAfterRounds is unconfirmed:
	// probing then goes up to maxUnknownHops past it to find a longer path.
	endExpireRounds = hideAfterRounds
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
	// Untracked counts replies from responders that found the per-hop
	// responder table full of live responders; they are only in the totals
	Untracked int
	Sent      int // probes that have been answered or timed out
	Recv      int
	Last      time.Duration
	Avg       time.Duration
	Best      time.Duration
	Worst     time.Duration

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
	Stale bool // missed more than staleAfterRounds revisits
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
	untracked  int      // replies not credited to a responder (table full)

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
		if len(h.order) >= maxRespondersPerHop && !h.evictHidden(now) {
			h.untracked++ // counted in the hop totals only
			return
		}
		if h.resp == nil {
			h.resp = map[string]*respStats{}
		}
		r = &respStats{firstRound: round, lastRound: round}
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

// defaultRevisit is the revisit interval (see staleAfterRounds) of a
// responder seen only once: the mean of the other responders' intervals, 1
// if none is known yet.
func (h *hopStats) defaultRevisit() float64 {
	sum, n := 0.0, 0
	for _, addr := range h.order {
		if r := h.resp[addr]; r.recv >= 2 {
			sum += r.revisit(1)
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return sum / float64(n)
}

// revisit returns how many rounds apart r is seen on average (def until it
// has been seen in two rounds)
func (r *respStats) revisit(def float64) float64 {
	if r.recv < 2 || r.lastRound <= r.firstRound {
		return def
	}
	return max(1, float64(r.lastRound-r.firstRound)/float64(r.recv-1))
}

// silence returns how many revisits r has missed by round now
func (r *respStats) silence(now int, def float64) float64 {
	return float64(now-r.lastRound) / r.revisit(def)
}

// evictHidden drops the responder that missed the most revisits if it is
// hidden (see hideAfterRounds), making room for a new one. Live and merely
// stale responders are kept: with more ECMP branches than table slots the
// extra responders are counted in the hop totals only, instead of every
// branch evicting another one in turn. It reports whether one was dropped.
func (h *hopStats) evictHidden(now int) bool {
	def := h.defaultRevisit()
	victim, most := -1, float64(hideAfterRounds)
	for i, addr := range h.order {
		if s := h.resp[addr].silence(now, def); s > most {
			victim, most = i, s
		}
	}
	if victim < 0 {
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

	mu        sync.Mutex
	seq       uint16
	udpPorts  int                 // destination ports UDP probes rotate through
	udpWindow int                 // rounds whose UDP ports never collide, see udpPortWindow
	pending   map[uint16]inflight // by probe key: ICMP echo seq / UDP dst port
	hops      []hopStats          // index = ttl-1
	maxTTL    int                 // the destination's distance once known, else maxHops
	reached   bool
	confirmed bool // the destination answered at maxTTL within staleAfterRounds
	rounds    int
	names     map[string]string // reverse DNS cache ("" = lookup in progress/failed)

	onEvent func(probeEvent) // test hook, called with mu held

	closeOnce sync.Once
}

// New opens the sockets for tracing target with cfg.Proto. See
// newICMPTransport and newUDPTransport for the sockets used per platform.
func New(target string, cfg Config) (*Tracer, error) {
	cfg = cfg.withDefaults()
	host, preferV6 := HostFromTarget(target)
	dst, err := ResolveAddr(host, preferV6)
	if err != nil {
		return nil, err
	}
	v6 := dst.IP.To4() == nil

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
	return newTracer(cfg, dst.IP, tp), nil
}

func newTracer(cfg Config, dst net.IP, tp transport) *Tracer {
	cfg = cfg.withDefaults()
	ports := udpPortRange(cfg.Interval, cfg.Timeout)
	return &Tracer{
		cfg:       cfg,
		dst:       dst,
		tp:        tp,
		udpPorts:  ports,
		udpWindow: udpPortWindow(ports),
		pending:   map[uint16]inflight{},
		hops:      make([]hopStats, maxHops),
		maxTTL:    maxHops,
		names:     map[string]string{},
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
	round := t.startRoundLocked(time.Now())
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

// startRoundLocked starts the next round at now: probes past their timeout
// are lost, and so are UDP probes older than the port window (huge timeouts,
// see udpPortRange), so that no port of a pending probe is reused this
// round. It returns the new round.
func (t *Tracer) startRoundLocked(now time.Time) int {
	t.expire(now)
	t.rounds++
	if t.cfg.Proto == ProtoUDP && t.udpWindow > 0 {
		for key, p := range t.pending {
			if t.rounds-p.round > t.udpWindow {
				t.loseLocked(key, p)
			}
		}
	}
	t.updateReachLocked()
	return t.rounds
}

// probeKeyLocked returns the key of the next probe: the next echo sequence
// for ICMP, the rotating destination port for UDP. A probe still pending
// under the key (only possible after the key space wrapped, after its
// timeout; see udpPortRange) is counted as lost first.
func (t *Tracer) probeKeyLocked(round, ttl int) uint16 {
	var key uint16
	if t.cfg.Proto == ProtoUDP {
		key = udpPort(t.udpPorts, round, ttl)
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
	if ok && r.ttl != 0 && r.ttl != p.ttl {
		// The reply is for an earlier probe that used the same key with
		// another TTL (a late reply after the key came round again)
		ok = false
	}
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

// pathWindowLocked returns the path window in rounds, see minPathWindow
func (t *Tracer) pathWindowLocked() int {
	flows := 1 // ICMP: a single flow
	if t.cfg.Proto == ProtoUDP {
		flows = t.udpPorts
	}
	return min(max(2*flows, minPathWindow), maxPathWindow)
}

// updateReachLocked re-evaluates where the path ends. Every ECMP branch ends
// past the farthest TTL that sent a Time Exceeded within the path window, so
// the destination is the first TTL beyond it answered by the end of the path
// (echo reply / unreachable) within endExpireRounds. That is the end of the
// longest branch: shorter branches also reach the destination there, so it
// is answered every round, and the farthest Time Exceeded keeps being
// refreshed by the longest branch. A path that gets longer is noticed by a
// Time Exceeded at or past the destination, or by the destination falling
// silent (unconfirmed, probing goes past it; see probeLimitLocked) while a
// farther TTL answers. With no such TTL the destination is unknown again.
func (t *Tracer) updateReachLocked() {
	recent := func(round, window int) bool { return round > 0 && t.rounds-round <= window }
	window := t.pathWindowLocked()
	farthestTE := 0
	for i := range t.hops {
		if recent(t.hops[i].lastTE, window) {
			farthestTE = i + 1
		}
	}
	dst := 0
	for i := farthestTE; i < len(t.hops); i++ {
		if recent(t.hops[i].lastEnd, endExpireRounds) {
			dst = i + 1
			break
		}
	}
	t.reached = dst > 0
	t.maxTTL, t.confirmed = maxHops, false
	if t.reached {
		t.maxTTL = dst
		t.confirmed = recent(t.hops[dst-1].lastEnd, staleAfterRounds)
	}
}

// farthestResponderLocked returns the highest TTL that has replied (0 if none)
func (t *Tracer) farthestResponderLocked() int {
	last := 0
	for i := range t.hops {
		if t.hops[i].recv > 0 {
			last = i + 1
		}
	}
	return last
}

// probeLimitLocked returns the highest TTL to probe this round: the
// destination while it answers; maxUnknownHops past it while it does not
// (the path may have got longer behind silent hops); maxUnknownHops past
// the farthest hop that has replied while the destination is unknown.
func (t *Tracer) probeLimitLocked() int {
	switch {
	case t.reached && t.confirmed:
		return t.maxTTL
	case t.reached:
		return min(maxHops, t.maxTTL+maxUnknownHops)
	}
	return min(maxHops, t.farthestResponderLocked()+maxUnknownHops)
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
			TTL:       i + 1,
			Sent:      h.sent,
			Recv:      h.recv,
			Untracked: h.untracked,
			Last:      h.last,
			Best:      h.best,
			Worst:     h.worst,
		}
		if h.recv > 0 {
			hop.Avg = time.Duration(h.sum / float64(h.recv))
		}
		dominant := -1
		// The most recently seen responder (earliest seen on ties) is never
		// hidden, so a rate-limited hop that replies only now and then keeps
		// its address. Others are hidden once they missed hideAfterRounds
		// revisits and a more recent responder exists (the path moved).
		def := h.defaultRevisit()
		latest := ""
		for _, addr := range h.order {
			if latest == "" || h.resp[addr].lastRound > h.resp[latest].lastRound {
				latest = addr
			}
		}
		for _, addr := range h.order {
			r := h.resp[addr]
			silence := r.silence(t.rounds, def)
			if silence > hideAfterRounds && addr != latest {
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
				Stale: silence > staleAfterRounds,
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
