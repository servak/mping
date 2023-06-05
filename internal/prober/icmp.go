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
)

type (
	ICMPProber struct {
		c        *icmp.PacketConn
		body     []byte
		targets  []*net.IPAddr
		interval time.Duration
		timeout  time.Duration
		runCnt   int
		runID    int
		tables   map[runTime]map[string]bool
		mu       sync.Mutex
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

func NewICMPProber(addrs []*net.IPAddr, cfg *ICMPConfig) (*ICMPProber, error) {
	c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	interval, err := cfg.GetInterval()
	if err != nil {
		return nil, err
	}
	timeout, err := cfg.GetTimeout()
	if err != nil {
		return nil, err
	}
	return &ICMPProber{
		c:        c,
		timeout:  timeout,
		tables:   make(map[runTime]map[string]bool),
		targets:  addrs,
		interval: interval,
		runID:    os.Getpid() & 0xffff,
		runCnt:   0,
		body:     []byte(cfg.Body),
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
		Rtt:    0,
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

func (p *ICMPProber) failed(r chan *Event, runCnt int, addr string) {
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
	return icmp.Message{
		Type: ipv4.ICMPTypeEcho,
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
			p.failed(r, p.runCnt, t.String())
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
		rm, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), pktbuf[:n])
		if err != nil {
			fmt.Printf("Error parsing ICMP message: %s\n", err)
			os.Exit(1)
		}
		offset := 0
		var pkt = rcvdPkt{
			target: ip.String(),
			// ICMP packet body starts from the 5th byte
			id:  binary.BigEndian.Uint16(pktbuf[offset+4 : offset+6]),
			seq: binary.BigEndian.Uint16(pktbuf[offset+6 : offset+8]),
		}
		if pkt.id != uint16(p.runID) {
			continue
		}

		if rm.Type == ipv4.ICMPTypeEchoReply && rm.Code == 0 {
			p.success(r, int(pkt.seq), pkt.target)
		} else {
			fmt.Printf("Received unexpected ICMP message from %s: %+v\n", ip.String(), rm)
			os.Exit(1)
		}
	}
}

func (p *ICMPProber) Start(r chan *Event) error {
	ticker := time.NewTicker(p.interval)
	go p.recvPkts(r)
	for {
		select {
		case <-ticker.C:
			p.probe(r)
			go p.checkTimeout(r)
		}
	}
}
