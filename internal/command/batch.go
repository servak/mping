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
)

func NewPingBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch [IP or HOSTNAME]...",
		Short: "Disables TUI and performs probing for a set number of iterations",
		Args:  cobra.MinimumNArgs(0),
		Example: `mping batch 1.1.1.1 8.8.8.8
mping batch icmpv6:google.com
mping batch http://google.com
mping batch dns://8.8.8.8/google.com`,
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
			
			// Start probing with timeout context
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(counter)*_interval)
			defer cancel()
			
			cmd.Print("probe")
			go func() {
				if err := probeManager.Run(ctx, _interval, _timeout); err != nil {
					fmt.Printf("ProbeManager error: %v\n", err)
				}
			}()
			
			// Wait for specified duration
			for counter > 0 {
				counter--
				cmd.Print(".")
				time.Sleep(_interval)
			}
			
			// Stop probing
			probeManager.Stop()
			cmd.Print("\r")
			metrics := metricsManager.SortBy(stats.Success, true)
			tableData := shared.NewTableData(metrics, stats.Success, true)
			t := tableData.ToGoPrettyTable()
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
