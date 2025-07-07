package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui"
)

func NewPingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "mping [IP or HOSTNAME]...",
		Short:         "",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.MinimumNArgs(0),
		Example: `mping 1.1.1.1 8.8.8.8
mping icmpv6:google.com
mping http://google.com
mping dns://8.8.8.8/google.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
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
			title, err := flags.GetString("title")
			if err != nil {
				return err
			}
			path, err := flags.GetString("config")
			if err != nil {
				return err
			}
			filename, err := flags.GetString("filename")
			if err != nil {
				return err
			}
			sourceInterface, err := flags.GetString("interface")
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
			cfg.SetTitle(title)
			cfg.SetSourceInterface(sourceInterface)
			_interval := time.Duration(interval) * time.Millisecond
			_timeout := time.Duration(timeout) * time.Millisecond

			// Create ProbeManager and MetricsManager
			probeManager := prober.NewProbeManager(cfg.Prober, cfg.Default)
			metricsManager := stats.NewMetricsManager()

			// Add targets
			err = probeManager.AddTargets(hosts...)
			if err != nil {
				return fmt.Errorf("failed to add targets: %w", err)
			}

			// Subscribe to events for metrics collection
			metricsManager.Subscribe(probeManager.Events())

			// Start probing in background
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				if err := probeManager.Run(ctx, _interval, _timeout); err != nil {
					fmt.Printf("ProbeManager error: %v\n", err)
				}
			}()

			// Start TUI
			startTUI(metricsManager, cfg.UI, _interval, _timeout)

			// Stop probing when TUI exits
			probeManager.Stop()

			// Final results
			metrics := metricsManager.SortByWithReader(stats.Fail, false)
			tableData := shared.NewTableData(metrics, stats.Fail, false)
			t := tableData.ToGoPrettyTable()
			t.SetStyle(table.StyleLight)
			cmd.Println(t.Render())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("filename", "f", "", "use contents of file")
	flags.StringP("title", "n", "", "print title")
	flags.StringP("config", "c", "~/.mping.yml", "config path")
	flags.StringP("interface", "I", "", "source interface (name or IP address)")
	flags.IntP("interval", "i", 1000, "interval(ms)")
	flags.IntP("timeout", "t", 1000, "timeout(ms)")

	return cmd
}

func startTUI(manager *stats.MetricsManager, cfg *shared.Config, interval, timeout time.Duration) {
	app := tui.NewTUIApp(manager, cfg, interval, timeout)

	refreshTime := time.Millisecond * 250 // Minimum refresh time that can be set
	if refreshTime < (interval / 2) {
		refreshTime = interval / 2
	}
	go func() {
		for {
			app.Update()
			time.Sleep(refreshTime)
		}
	}()
	app.Run()
}
