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
	probeTargets := splitProber(addDefaultProbeType(hostnames), cfg)
	res := make(chan *prober.Event)
	manager := stats.NewMetricsManager()
	startProbers(probeTargets, res, interval, timeout, manager)

	manager.Subscribe(res)
	startUI(manager, cfg.UI.CUI, interval)
}

func startProbers(probeTargets map[*prober.ProberConfig][]string, res chan *prober.Event, interval, timeout time.Duration, manager *stats.MetricsManager) {
	for cfg, targets := range probeTargets {
		prober, err := newProber(cfg, manager, targets)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		go prober.Start(res, interval, timeout)
	}
}

func startUI(manager *stats.MetricsManager, cui *ui.CUIConfig, interval time.Duration) {
	r, err := ui.NewCUI(manager, cui, interval)
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

func addDefaultProbeType(hostnames []string) []string {
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
		probeTypeAndTarget := strings.SplitN(t, ":", 2)
		if len(probeTypeAndTarget) != 2 {
			continue
		}
		for k, c := range cfg.Prober {
			if probeTypeAndTarget[0] == k {
				rules[c] = append(rules[c], probeTypeAndTarget[1])
				break
			}
		}
	}
	return rules
}

func newProber(cfg *prober.ProberConfig, manager *stats.MetricsManager, targets []string) (prober.Prober, error) {
	var (
		probe prober.Prober
		err   error
	)
	switch cfg.Probe {
	case prober.ICMPV4, prober.ICMPV6:
		resolvType := "ip4"
		if cfg.Probe == prober.ICMPV6 {
			resolvType = "ip6"
		}
		var addrs []*net.IPAddr
		for _, h := range targets {
			ip, err := net.ResolveIPAddr(resolvType, h)
			if err != nil {
				continue
			}
			addrs = append(addrs, ip)
			name := ip.String()
			if net.ParseIP(h) == nil {
				name = fmt.Sprintf("%s(%s)", h, name)
			}
			manager.Register(ip.String(), name)
		}
		probe, err = prober.NewICMPProber(cfg.Probe, uniqueStringer(addrs), cfg.ICMP)
	case prober.HTTP:
		var ts []string
		for _, h := range targets {
			t := "http:" + h
			ts = append(ts, t)
			manager.Register(t, t)
		}
		probe = prober.NewHTTPProber(unique(ts), cfg.HTTP)
	default:
		err = fmt.Errorf("%s not found. please set implement prober.", cfg.Probe)
	}
	return probe, err
}

func unique[T comparable](s []T) []T {
	inResult := make(map[T]bool)
	var result []T
	for _, str := range s {
		if _, ok := inResult[str]; !ok {
			inResult[str] = true
			result = append(result, str)
		}
	}
	return result
}

func uniqueStringer[T fmt.Stringer](s []T) []T {
	inResult := make(map[string]bool)
	var result []T
	for _, str := range s {
		if _, ok := inResult[str.String()]; !ok {
			inResult[str.String()] = true
			result = append(result, str)
		}
	}
	return result
}
