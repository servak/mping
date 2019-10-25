package mping

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/nsf/termbox-go"
	"github.com/servak/go-fastping"
)

var totalStats statistics = []*stats{}

type response struct {
	addr *net.IPAddr
	rtt  time.Duration
}

func Run(hostnames []string, maxRtt int, count int, _title string, ipv6 bool) {
	hostnames = parseCidr(hostnames)
	doCount := count != 0
	p := fastping.NewPinger()
	results := make(map[string]*response)
	onRecv, onIdle := make(chan *response), make(chan bool)
	p.OnRecv = func(addr *net.IPAddr, t time.Duration) {
		onRecv <- &response{addr: addr, rtt: t}
	}
	p.OnIdle = func() {
		onIdle <- true
	}
	var (
		ra  *net.IPAddr
		err error
	)

	title = _title
	i := 1
	for _, hostname := range hostnames {
		ip := net.ParseIP(hostname)
		if ip == nil {
			// hostname is not ipaddr.
			if ipv6 {
				ra, err = net.ResolveIPAddr("ip6:icmp", hostname)
				if err != nil {
					ra, err = net.ResolveIPAddr("ip4:icmp", hostname)
				}
			} else {
				ra, err = net.ResolveIPAddr("ip4:icmp", hostname)
			}

			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else {
			// hostname is ipaddr
			ra = &net.IPAddr{IP: ip}
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

	is_tick_end := make(chan bool)
	done := make(chan struct{})
	screenInit()
	defer printScreenValues()
	defer screenClose()
	screenRedraw()

	refreshTime := 200
	if maxRtt > refreshTime {
		refreshTime = maxRtt / 2
	}
	p.MaxRTT = time.Millisecond * time.Duration(maxRtt)
	p.RunLoop()
	go func() {
		t := time.NewTicker(time.Millisecond * time.Duration(refreshTime))
		for {
			select {
			case <-is_tick_end:
				return
			case <-t.C:
				screenRedraw()
			}
		}
	}()
	go func() {
		idleCount := 0
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
				if doCount {
					idleCount += 1
					if idleCount >= count {
						close(done)
						return
					}
				}
			case <-p.Done():
				if err := p.Err(); err != nil {
					log.Println("Ping failed:", err)
				}
				close(done)
				return
			}
		}
	}()

	go func() {
		for {
			switch ev := termbox.PollEvent(); ev.Type {
			case termbox.EventKey:
				switch ev.Ch {
				case 'q':
					is_tick_end <- true
					close(done)
					return
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
	}()

	<-done
}

func parseCidr(_hosts []string) []string {
	hosts := []string{}
	for _, h := range _hosts {
		ip, ipnet, err := net.ParseCIDR(h)
		if err != nil {
			hosts = append(hosts, h)
			continue
		}

		for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); ipInc(ip) {
			hosts = append(hosts, ip.String())
		}
	}

	return hosts
}

func ipInc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
