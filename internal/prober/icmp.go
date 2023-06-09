package prober

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	ICMPV4 ProbeType = "icmpv4"
	ICMPV6 ProbeType = "icmpv6"
)

type (
	ICMPProber struct {
		version ProbeType
		c       *icmp.PacketConn
		body    []byte
		targets []*net.IPAddr
		timeout time.Duration
		runCnt  int
		runID   int
		tables  map[runTime]map[string]bool
		mu      sync.Mutex
	}

	runTime struct {
		runCnt   int
		sentTime time.Time
	}

	rcvdPkt struct {
		id, seq uint16
		data    []byte
		tsUnix  int64
		target  string
	}
)

func NewICMPProber(t ProbeType, addrs []*net.IPAddr, cfg *ICMPConfig) (*ICMPProber, error) {
	var (
		c   *icmp.PacketConn
		err error
	)
	if t == ICMPV4 {
		c, err = icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	} else {
		c, err = icmp.ListenPacket("ip6:ipv6-icmp", "::")
	}
	return &ICMPProber{
		version: t,
		c:       c,
		tables:  make(map[runTime]map[string]bool),
		targets: addrs,
		runID:   os.Getpid() & 0xffff,
		runCnt:  0,
		body:    []byte(cfg.Body),
	}, err
}

func (p *ICMPProber) addTable(runCnt int, sentTime time.Time) {
	rt := runTime{runCnt: runCnt, sentTime: sentTime}
	addrMap := make(map[string]bool, len(p.targets))
	for _, t := range p.targets {
		addrMap[t.String()] = false
	}
	p.mu.Lock()
	p.tables[rt] = addrMap
	p.mu.Unlock()
}

func (p *ICMPProber) sent(r chan *Event, addr string) {
	r <- &Event{
		Target: addr,
		Result: SENT,
	}
}

func (p *ICMPProber) success(r chan *Event, runCnt int, addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, table := range p.tables {
		if k.runCnt != runCnt {
			continue
		}
		if _, ok := table[addr]; !ok {
			return
		}
		if table[addr] {
			continue
		}
		table[addr] = true
		elapse := time.Since(k.sentTime)
		r <- &Event{
			Target:   addr,
			Result:   SUCCESS,
			SentTime: k.sentTime,
			Rtt:      elapse,
		}
		return
	}
}

func (p *ICMPProber) failed(r chan *Event, runCnt int, addr string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, table := range p.tables {
		if k.runCnt != runCnt {
			continue
		}
		if _, ok := table[addr]; ok {
			table[addr] = true
		}
		r <- &Event{
			Target:   addr,
			Result:   FAILED,
			SentTime: k.sentTime,
			Rtt:      0,
			Message:  err.Error(),
		}
		return
	}
}

func (p *ICMPProber) checkTimeout(r chan *Event) {
	now := time.Now()
	var cleanTargets []runTime
	p.mu.Lock()
	defer p.mu.Unlock()
	for rt, table := range p.tables {
		if rt.sentTime.Add(p.timeout).After(now) {
			continue
		}
		for t, res := range table {
			if !res {
				r <- &Event{
					Target:   t,
					Result:   TIMEOUT,
					SentTime: rt.sentTime,
					Rtt:      p.timeout,
					Message:  "timeout",
				}
			}
		}
		cleanTargets = append(cleanTargets, rt)
	}
	for _, rt := range cleanTargets {
		delete(p.tables, rt)
	}
}

func (p *ICMPProber) makeEchoMsg() icmp.Message {
	var t icmp.Type
	if p.version == ICMPV4 {
		t = ipv4.ICMPTypeEcho
	} else {
		t = ipv6.ICMPTypeEchoRequest
	}
	return icmp.Message{
		Type: t,
		Code: 0,
		Body: &icmp.Echo{
			ID:   p.runID,
			Seq:  p.runCnt,
			Data: p.body,
		},
	}
}

func (p *ICMPProber) probe(r chan *Event) {
	p.runCnt++
	m := p.makeEchoMsg()

	b, err := m.Marshal(nil)
	if err != nil {
		fmt.Printf("Failed ICMP encode: %s\n", err)
		os.Exit(1)
	}

	n := time.Now()
	p.addTable(p.runCnt, n)
	for _, t := range p.targets {
		_, err := p.c.WriteTo(b, t)
		p.sent(r, t.String())
		if err != nil {
			p.failed(r, p.runCnt, t.String(), err)
		}
	}
}

func (p *ICMPProber) recvPkts(r chan *Event) {
	pktbuf := make([]byte, maxPacketSize)
	for {
		n, ip, err := p.c.ReadFrom(pktbuf)
		if err != nil {
			fmt.Printf("Error reading ICMP packet: %s\n", err)
			os.Exit(1)
		}
		proto := ipv4.ICMPTypeEchoReply.Protocol()
		if p.version == ICMPV6 {
			proto = ipv6.ICMPTypeEchoReply.Protocol()
		}
		rm, err := icmp.ParseMessage(proto, pktbuf[:n])
		if err != nil {
			fmt.Printf("Error parsing ICMP message: %s\n", err)
			os.Exit(1)
		}
		offset := 0
		var pkt = rcvdPkt{
			target: ip.String(),
			id:     binary.BigEndian.Uint16(pktbuf[offset+4 : offset+6]),
			seq:    binary.BigEndian.Uint16(pktbuf[offset+6 : offset+8]),
		}
		if pkt.id != uint16(p.runID) {
			continue
		}
		if rm.Code == 0 {
			switch rm.Type {
			case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
				p.success(r, int(pkt.seq), pkt.target)
			}
		}
	}
}

func (p *ICMPProber) Start(r chan *Event, interval, timeout time.Duration) error {
	p.timeout = timeout
	ticker := time.NewTicker(interval)
	go p.recvPkts(r)
	for {
		select {
		case <-ticker.C:
			p.probe(r)
			go p.checkTimeout(r)
		}
	}
}
