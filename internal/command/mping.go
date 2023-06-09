package command

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui"
)

func Run(hostnames []string, cfg *config.Config, interval, timeout time.Duration) {
	probeTargets := splitProber(taintDefaultProbe(hostnames), cfg)
	entryList := make(map[string]string) // for metricsManager
	res := make(chan *prober.Event)
	for cfg, targets := range probeTargets {
		prober, entries, err := newProber(cfg, targets)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		for k, v := range entries {
			entryList[k] = v
		}
		go prober.Start(res, interval, timeout)
	}
	manager := stats.NewMetricsManager(entryList)
	manager.Subscribe(res)
	r, err := ui.NewCUI(manager, cfg.UI.CUI, interval)
	if err != nil {
		fmt.Println(err)
		return
	}
	refreshTime := time.Millisecond * 250 // Minimum refresh time that can be set
	if refreshTime < (interval / 2) {
		refreshTime = interval / 2
	}

	go func() {
		for {
			time.Sleep(refreshTime)
			r.Update()
		}
	}()
	r.Run()
}

func taintDefaultProbe(hostnames []string) []string {
	var res []string
	for _, h := range hostnames {
		if strings.Contains(h, ":") {
			res = append(res, h)
			continue
		}
		addr := net.ParseIP(h)
		if addr == nil || addr.To4() != nil {
			res = append(res, fmt.Sprintf("%s:%s", prober.ICMPV4, h))
		} else {
			res = append(res, fmt.Sprintf("%s:%s", prober.ICMPV6, h))
		}
	}
	return res
}

func splitProber(targets []string, cfg *config.Config) map[*prober.ProberConfig][]string {
	rules := make(map[*prober.ProberConfig][]string)
	for _, t := range targets {
		for k, c := range cfg.Prober {
			if strings.HasPrefix(t, k) {
				idx := strings.Index(t, ":")
				rules[c] = append(rules[c], t[idx+1:])
				break
			}
		}
	}
	return rules
}

func newProber(cfg *prober.ProberConfig, targets []string) (prober.Prober, map[string]string, error) {
	var (
		probe prober.Prober
		err   error
	)
	entryList := make(map[string]string)
	switch cfg.Probe {
	case prober.ICMPV4:
		var addrs []*net.IPAddr
		for _, h := range targets {
			ip, err := net.ResolveIPAddr("ip4", h)
			if err != nil {
				continue
			}
			addrs = append(addrs, ip)
			entryList[ip.String()] = h
		}
		probe, err = prober.NewICMPProber(prober.ICMPV4, addrs, cfg.ICMP)
	case prober.ICMPV6:
		var addrs []*net.IPAddr
		for _, h := range targets {
			ip, err := net.ResolveIPAddr("ip6", h)
			if err != nil {
				continue
			}
			addrs = append(addrs, ip)
			entryList[ip.String()] = h
		}
		probe, err = prober.NewICMPProber(prober.ICMPV6, addrs, cfg.ICMP)
	case prober.HTTP:
		var ts []string
		for _, h := range targets {
			t := "http:" + h
			ts = append(ts, t)
			entryList[t] = t
		}
		probe = prober.NewHTTPProber(ts, cfg.HTTP)
	default:
		err = fmt.Errorf("%s not found. please set implement prober.", cfg.Probe)
	}
	return probe, entryList, err
}
