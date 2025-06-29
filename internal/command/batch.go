package command

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui"
)

func NewPingBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch [IP or HOSTNAME]...",
		Short: "Disables TUI and performs probing for a set number of iterations",
		Args:  cobra.MinimumNArgs(0),
		Example: `mping batch 1.1.1.1 8.8.8.8
mping batch icmpv6:google.com
mping batch http://google.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			counter, err := flags.GetInt("count")
			if err != nil {
				return err
			}
			interval, err := flags.GetInt("interval")
			if err != nil {
				return err
			}
			timeout, err := flags.GetInt("timeout")
			if err != nil {
				return err
			}
			if interval == 0 && timeout == 0 {
				return errors.New("both interval and timeout can't be zero")
			} else if interval == 0 {
				return errors.New("interval can't be zero")
			} else if timeout == 0 {
				return errors.New("timeout can't be zero")
			}
			path, err := flags.GetString("config")
			if err != nil {
				return err
			}
			filename, err := flags.GetString("filename")
			if err != nil {
				return err
			}

			hosts := parseHostnames(args, filename)
			if len(hosts) == 0 {
				cmd.Println("Please set hostname or ip.")
				cmd.Help()
				return nil
			}

			cfg, _ := config.LoadFile(path)
			_interval := time.Duration(interval) * time.Millisecond
			_timeout := time.Duration(timeout) * time.Millisecond

			res := make(chan *prober.Event)
			
			// Create all available probers
			manager := stats.NewMetricsManager()
			allProbers, err := createAllProbers(cfg)
			if err != nil {
				return fmt.Errorf("failed to create probers: %w", err)
			}
			
			// Use TargetRouter for cleaner target handling
			router := prober.NewTargetRouter(allProbers)
			registrations, err := router.RouteTargets(hosts)
			if err != nil {
				return fmt.Errorf("failed to route targets: %w", err)
			}
			
			// Register metrics
			for target, displayName := range registrations {
				manager.Register(target, displayName)
			}
			
			probers := router.GetActiveProbers()
			manager.Subscribe(res)
			var wg sync.WaitGroup
			for _, p := range probers {
				wg.Add(1)
				go func(p prober.Prober) {
					p.Start(res, _interval, _timeout)
					wg.Done()
				}(p)
			}
			cmd.Print("probe")
			for counter > 0 {
				counter--
				cmd.Print(".")
				time.Sleep(_interval)
			}
			for _, p := range probers {
				p.Stop()
			}
			wg.Wait()
			cmd.Print("\r")
			t := ui.TableRender(manager, stats.Success)
			t.SetStyle(table.StyleLight)
			cmd.Println(t.Render())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("filename", "f", "", "use contents of file")
	flags.StringP("config", "c", "~/.mping.yml", "config path")
	flags.IntP("interval", "i", 1000, "interval(ms)")
	flags.IntP("timeout", "t", 1000, "timeout(ms)")
	flags.IntP("count", "", 10, "repeat count")

	return cmd
}
