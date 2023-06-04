package command

import (
	"fmt"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

func TestRun(probe prober.Prober, hostnames []string) {
	hostnames = parseCidr(hostnames)
	manager := stats.NewMetricsManager()

	res := make(chan *prober.Event)
	manager.Subscribe(res)
	go probe.Start(res)
	for {
		fmt.Println("--------")
		for k, v := range manager.GetAllMetrics() {
			fmt.Println(k, v.Values())
		}
		time.Sleep(time.Second)
	}
}
