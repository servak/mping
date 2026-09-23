// Package trace implements an MTR-style path tracer: it repeatedly sends ICMP
// echo requests with increasing TTL and keeps per-hop loss and RTT statistics.
package trace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	DefaultInterval = time.Second
	DefaultTimeout  = time.Second

	maxHops       = 30
	maxPacketSize = 1500
)

// idCounter makes echo IDs unique per tracer within the process, and distinct
// from the ICMP prober which uses pid&0xffff.
var idCounter atomic.Uint32

// Config configures a Tracer
type Config struct {
	Interval time.Duration // time between rounds
	Timeout  time.Duration // a probe without reply after this is lost
	NoDNS    bool          // skip reverse DNS lookups of hop addresses
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	return c
}

// Hop is a snapshot of statistics for one TTL
type Hop struct {
	TTL    int
	Addr   string // most recent responder ("" if none yet)
	Name   string // reverse DNS name of Addr, if resolved
	Others int    // number of other responders seen (ECMP / path changes)
	Sent   int    // probes that have been answered or timed out
	Recv   int
	Last   time.Duration
	Avg    time.Duration
	Best   time.Duration
	Worst  time.Duration
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
	Reached    bool // the destination (or an unreachable report) ended the path
	Privileged bool // raw socket in use; false means datagram ICMP fallback
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

type hopStats struct {
	sent, recv int
	last       time.Duration
	best       time.Duration
	worst      time.Duration
	sum        float64
	addr       string
	addrs      map[string]struct{}
}

func (h *hopStats) record(addr string, rtt time.Duration) {
	h.recv++
	h.last = rtt
	if h.best == 0 || rtt < h.best {
		h.best = rtt
	}
	if rtt > h.worst {
		h.worst = rtt
	}
	h.sum += float64(rtt)
	h.addr = addr
	if h.addrs == nil {
		h.addrs = map[string]struct{}{}
	}
	h.addrs[addr] = struct{}{}
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
	cfg        Config
	dst        net.IP
	v6         bool
	privileged bool
	conn       *icmp.PacketConn
	dstAddr    net.Addr
	id         uint16
	checkID    bool

	writeMu sync.Mutex // TTL is a socket option, so set+write must be atomic

	mu      sync.Mutex
	seq     uint16
	pending map[uint16]inflight
	hops    []hopStats // index = ttl-1
	maxTTL  int        // shrinks to the destination's distance once known
	reached bool
	rounds  int
	names   map[string]string // reverse DNS cache ("" = lookup in progress/failed)

	onEvent func(probeEvent) // test hook, called with mu held

	closeOnce sync.Once
}

// New opens an ICMP socket for tracing target. It prefers a raw socket and
// falls back to an unprivileged datagram ICMP socket; on macOS the fallback
// still receives Time Exceeded messages, on Linux only the destination may
// be visible without privileges.
func New(target string, cfg Config) (*Tracer, error) {
	cfg = cfg.withDefaults()
	host, preferV6 := HostFromTarget(target)
	dst, err := Resolve(host, preferV6)
	if err != nil {
		return nil, err
	}
	v6 := dst.To4() == nil

	t := &Tracer{
		cfg:     cfg,
		dst:     dst,
		v6:      v6,
		id:      uint16(os.Getpid() + 1 + int(idCounter.Add(1))),
		pending: map[uint16]inflight{},
		hops:    make([]hopStats, maxHops),
		maxTTL:  maxHops,
		names:   map[string]string{},
	}

	rawNet, dgramNet, laddr := "ip4:icmp", "udp4", "0.0.0.0"
	if v6 {
		rawNet, dgramNet, laddr = "ip6:ipv6-icmp", "udp6", "::"
	}
	if c, err := icmp.ListenPacket(rawNet, laddr); err == nil {
		t.conn, t.privileged = c, true
		t.dstAddr = &net.IPAddr{IP: dst}
		t.checkID = true
	} else if c, err2 := icmp.ListenPacket(dgramNet, laddr); err2 == nil {
		t.conn = c
		t.dstAddr = &net.UDPAddr{IP: dst}
		// Linux rewrites the echo ID of datagram ICMP sockets and demuxes
		// replies per socket, so only check the ID elsewhere (e.g. macOS).
		t.checkID = runtime.GOOS != "linux"
	} else {
		return nil, fmt.Errorf("cannot open ICMP socket (%v; %v): run as root or grant cap_net_raw", err, err2)
	}
	return t, nil
}

// Close releases the socket
func (t *Tracer) Close() error {
	var err error
	t.closeOnce.Do(func() { err = t.conn.Close() })
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
		t.receive()
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
		if err := t.sendRound(); err != nil {
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

func (t *Tracer) sendRound() error {
	t.mu.Lock()
	t.expire(time.Now())
	maxTTL := t.maxTTL
	t.rounds++
	round := t.rounds
	t.mu.Unlock()

	for ttl := 1; ttl <= maxTTL; ttl++ {
		t.mu.Lock()
		t.seq++
		seq := t.seq
		t.mu.Unlock()

		msg := icmp.Message{Body: &icmp.Echo{ID: int(t.id), Seq: int(seq), Data: []byte("mping-trace")}}
		if t.v6 {
			msg.Type = ipv6.ICMPTypeEchoRequest
		} else {
			msg.Type = ipv4.ICMPTypeEcho
		}
		b, err := msg.Marshal(nil)
		if err != nil {
			return err
		}

		t.writeMu.Lock()
		if t.v6 {
			err = t.conn.IPv6PacketConn().SetHopLimit(ttl)
		} else {
			err = t.conn.IPv4PacketConn().SetTTL(ttl)
		}
		if err == nil {
			t.mu.Lock()
			t.pending[seq] = inflight{ttl: ttl, round: round, sent: time.Now()}
			t.mu.Unlock()
			_, err = t.conn.WriteTo(b, t.dstAddr)
		}
		t.writeMu.Unlock()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("send ttl=%d: %w", ttl, err)
		}
	}
	return nil
}

func (t *Tracer) receive() {
	buf := make([]byte, maxPacketSize)
	for {
		n, peer, err := t.conn.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		now := time.Now()
		r, err := parseReply(t.v6, buf[:n])
		if err != nil {
			continue
		}
		if t.checkID && r.id != t.id {
			continue // another process or tracer
		}
		t.handle(r, peerIP(peer), now)
	}
}

func peerIP(a net.Addr) string {
	switch v := a.(type) {
	case *net.IPAddr:
		return v.IP.String()
	case *net.UDPAddr:
		return v.IP.String()
	}
	return a.String()
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
	h.record(addr, now.Sub(p.sent))
	if t.onEvent != nil {
		t.onEvent(probeEvent{round: p.round, ttl: p.ttl, addr: addr, rtt: now.Sub(p.sent), outcome: outcomeReply})
	}

	if r.kind == kindEchoReply || r.kind == kindUnreachable {
		t.reached = true
		if p.ttl < t.maxTTL {
			t.maxTTL = p.ttl
		}
	}
	if !t.cfg.NoDNS {
		t.lookupLocked(addr)
	}
}

// expire counts pending probes sent before now-Timeout as lost
func (t *Tracer) expire(now time.Time) {
	for seq, p := range t.pending {
		if now.Sub(p.sent) >= t.cfg.Timeout {
			t.hops[p.ttl-1].sent++
			delete(t.pending, seq)
			if t.onEvent != nil {
				t.onEvent(probeEvent{round: p.round, ttl: p.ttl, outcome: outcomeLost})
			}
		}
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
	last := 0
	for i := 0; i < t.maxTTL; i++ {
		if t.hops[i].recv > 0 {
			last = i + 1
		}
	}
	if t.reached {
		last = t.maxTTL
	}

	res := Result{
		Dst:        t.dst,
		Rounds:     t.rounds,
		Reached:    t.reached,
		Privileged: t.privileged,
		Hops:       make([]Hop, 0, last),
	}
	for i := 0; i < last; i++ {
		h := t.hops[i]
		hop := Hop{
			TTL:   i + 1,
			Addr:  h.addr,
			Name:  t.names[h.addr],
			Sent:  h.sent,
			Recv:  h.recv,
			Last:  h.last,
			Best:  h.best,
			Worst: h.worst,
		}
		if len(h.addrs) > 1 {
			hop.Others = len(h.addrs) - 1
		}
		if h.recv > 0 {
			hop.Avg = time.Duration(h.sum / float64(h.recv))
		}
		res.Hops = append(res.Hops, hop)
	}
	return res
}
