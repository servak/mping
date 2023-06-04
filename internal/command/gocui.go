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
	manager := stats.NewMetricsManager()

	res := make(chan *prober.Event)
	manager.Subscribe(res)
	go probe.Start(res)

	r, err := ui.NewCUI(manager, cfg.UI.CUI)
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		for {
			time.Sleep(time.Millisecond * 100)
			r.Update()
		}
	}()
	r.Run()
}
