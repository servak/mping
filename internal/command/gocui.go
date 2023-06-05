package command

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui"
)

func GocuiRun(hostnames []string, cfg *config.Config) {
	addrs := make(map[string]*net.IPAddr)
	for _, h := range hostnames {
		ip, err := net.ResolveIPAddr("ip4", h)
		if err != nil {
			continue
		}
		addrs[h] = ip
	}

	probe, err := prober.NewICMPProber(mapValues(addrs), cfg.Prober.ICMP)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	interval, _ := cfg.Prober.ICMP.GetInterval() // already error checked in NewICMPProber
	manager := stats.NewMetricsManager(convertString(addrs))
	res := make(chan *prober.Event)
	manager.Subscribe(res)
	go probe.Start(res)

	cfg.UI.CUI.Interval = interval
	r, err := ui.NewCUI(manager, cfg.UI.CUI)
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

func mapValues[T any](ms map[string]T) []T {
	var res []T
	for _, v := range ms {
		res = append(res, v)
	}
	return res
}

func convertString[T fmt.Stringer](ms map[string]T) map[string]string {
	res := make(map[string]string)
	for k, v := range ms {
		res[v.String()] = k
	}
	return res
}
