package command

import (
	"fmt"
	"os"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui"
)

func GocuiRun(hostnames []string, cfg *config.Config) {
	probe, err := prober.NewICMPProber(hostnames, cfg.Prober.ICMP)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	interval, _ := cfg.Prober.ICMP.GetInterval() // already error checked in NewICMPProber
	manager := stats.NewMetricsManager()

	res := make(chan *prober.Event)
	manager.Subscribe(res)
	go probe.Start(res)

	r, err := ui.NewCUI(manager, interval, cfg.UI.CUI)
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
