package mping

import (
	"log"
	"net"
	"time"

	"github.com/nsf/termbox-go"
	"github.com/tatsushid/go-fastping"
)

var totalStats map[string]*stats

type response struct {
	addr *net.IPAddr
	rtt  time.Duration
}

func Run(hostnames []string) {
	p := fastping.NewPinger()
	totalStats = make(map[string]*stats)
	results := make(map[string]*response)
	onRecv, onIdle := make(chan *response), make(chan bool)
	p.OnRecv = func(addr *net.IPAddr, t time.Duration) {
		onRecv <- &response{addr: addr, rtt: t}
	}
	p.OnIdle = func() {
		onIdle <- true
	}

	i := 1
	for _, hostname := range hostnames {
		ra, err := net.ResolveIPAddr("ip4:icmp", hostname)
		if err != nil {
			panic(err)
		}
		p.AddIPAddr(ra)
		results[ra.String()] = nil

		totalStats[ra.String()] = &stats{
			order:    i,
			hostname: hostname,
		}
		i++
	}

	screenInit()
	defer screenClose()
	screenRedraw()

	p.MaxRTT = time.Second
	p.RunLoop()
	go updateView()
	go func() {
		for {
			select {
			case res := <-onRecv:
				if _, ok := results[res.addr.String()]; ok {
					results[res.addr.String()] = res
				}
			case <-onIdle:
				for host, r := range results {
					if r == nil {
						totalStats[host].failed()
					} else {
						totalStats[host].succeed(r.rtt)
					}
					results[host] = nil
				}
			case <-p.Done():
				if err := p.Err(); err != nil {
					log.Println("Ping failed:", err)
				}
				return
			}
		}
	}()

mainloop:
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			if ev.Ch == 'q' {
				break mainloop
			}
		case termbox.EventError:
			panic(ev.Err)
		}
	}
}
