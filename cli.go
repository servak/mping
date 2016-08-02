package mping

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/nsf/termbox-go"
	"github.com/tatsushid/go-fastping"
)

var totalStats statistics = []*stats{}

type response struct {
	addr *net.IPAddr
	rtt  time.Duration
}

func Run(hostnames []string, maxRtt int, _title string) {
	p := fastping.NewPinger()
	results := make(map[string]*response)
	onRecv, onIdle := make(chan *response), make(chan bool)
	p.OnRecv = func(addr *net.IPAddr, t time.Duration) {
		onRecv <- &response{addr: addr, rtt: t}
	}
	p.OnIdle = func() {
		onIdle <- true
	}

	title = _title
	i := 1
	for _, hostname := range hostnames {
		ra, err := net.ResolveIPAddr("ip4:icmp", hostname)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		p.AddIPAddr(ra)
		results[ra.String()] = nil

		totalStats = append(totalStats, &stats{
			order:    i,
			hostname: hostname,
			ip:       ra.String(),
		})
		i++
	}

	screenInit()
	defer printScreenValues()
	defer screenClose()
	screenRedraw()

	p.MaxRTT = time.Millisecond * time.Duration(maxRtt)
	p.RunLoop()
	go func() {
		t := time.NewTicker(time.Millisecond * 200)
		for {
			select {
			case <-t.C:
				screenRedraw()
			}
		}
	}()
	go func() {
		for {
			select {
			case res := <-onRecv:
				if _, ok := results[res.addr.String()]; ok {
					results[res.addr.String()] = res
				}
			case <-onIdle:
				for ip, r := range results {
					if r == nil {
						totalStats.setFailed(ip)
					} else {
						if r.rtt == 0 {
							r.rtt = p.MaxRTT
						}
						totalStats.setSucceed(ip, r.rtt)
					}
					results[ip] = nil
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
			switch ev.Ch {
			case 'q':
				break mainloop
			case 'n':
				currentPage++
				if currentPage > pageLength {
					currentPage = 0
				}
			case 's':
				sortType++
			case 'r':
				if reverse {
					reverse = false
				} else {
					reverse = true
				}
			case 'R':
				for _, x := range totalStats {
					x.init()
				}
			case 'p':
				currentPage--
				if currentPage < 0 {
					currentPage = pageLength
				}
			}
		case termbox.EventError:
			panic(ev.Err)
		}
	}
}
