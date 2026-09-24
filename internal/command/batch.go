package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
mping batch dns://8.8.8.8/google.com
mping batch -o json 10.0.0.0/29 | jq '.[] | select(.loss_percent > 0)'
mping batch --max-loss 20 -f hosts.txt || echo "some targets are unhealthy"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			counter, err := flags.GetInt("count")
			if err != nil {
				return err
			}
			if counter <= 0 {
				return errors.New("count must be greater than zero")
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
			sourceInterface, err := flags.GetString("interface")
			if err != nil {
				return err
			}
			output, err := flags.GetString("output")
			if err != nil {
				return err
			}
			if err := shared.ValidateOutputFormat(output); err != nil {
				return err
			}
			maxLoss := -1.0 // disabled unless explicitly set
			if flags.Changed("max-loss") {
				if maxLoss, err = flags.GetFloat64("max-loss"); err != nil {
					return err
				}
				if maxLoss < 0 || maxLoss > 100 {
					return fmt.Errorf("max-loss must be between 0 and 100, got %v", maxLoss)
				}
			}

			hosts := ExpandTargets(args, filename)
			if len(hosts) == 0 {
				cmd.Println("Please set hostname or ip.")
				cmd.Help()
				return nil
			}

			cfg, err := config.LoadFile(path)
			if cfg == nil {
				return fmt.Errorf("failed to load config %q: %w", path, err)
			}
			cfg.SetSourceInterface(sourceInterface)
			_interval := time.Duration(interval) * time.Millisecond
			_timeout := time.Duration(timeout) * time.Millisecond

			// Create ProbeManager and MetricsManager
			probeManager := prober.NewProbeManager(cfg.Prober, cfg.Default)
			metricsManager := stats.NewMetricsManagerWithOptions(stats.Options{
				SettleTime: stats.SettleTimeFor(_interval, _timeout),
				// The terminal bell would corrupt machine-readable output and
				// is pointless in non-interactive runs.
				DisableBeep: true,
			})

			// Add targets
			err = probeManager.AddTargets(hosts...)
			if err != nil {
				return fmt.Errorf("failed to add targets: %w", err)
			}

			// Subscribe to events for metrics collection
			done := metricsManager.Subscribe(probeManager.Events())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Progress goes to stderr (only when it is a terminal) so that
			// stdout stays clean for piping into other tools.
			progress := progressWriter()
			fmt.Fprint(progress, "probe")
			go func() {
				if err := probeManager.Run(ctx, _interval, _timeout); err != nil {
					fmt.Fprintf(os.Stderr, "ProbeManager error: %v\n", err)
				}
			}()

			// Probers send the first round immediately and then one round per
			// interval, so round N goes out at (N-1)*interval. Stopping half an
			// interval after the last round yields exactly `count` probes without
			// racing the next tick. Stop waits for in-flight probes to finish.
			// Deadlines are measured from a fixed start so sleep overshoot does
			// not accumulate across rounds (the prober's ticker does not drift).
			start := time.Now()
			fmt.Fprint(progress, ".")
			for i := 1; i < counter; i++ {
				time.Sleep(time.Until(start.Add(time.Duration(i) * _interval)))
				fmt.Fprint(progress, ".")
			}
			time.Sleep(time.Until(start.Add(time.Duration(counter-1)*_interval + _interval/2)))

			// Stop probing
			probeManager.Stop()
			<-done // wait for in-flight metric updates to settle
			fmt.Fprint(progress, "\r\033[K")

			metrics := metricsManager.SortBy(stats.Success, true)
			if err := shared.WriteReport(cmd.OutOrStdout(), output, metrics, stats.Success, true); err != nil {
				return err
			}
			return checkMaxLoss(metrics, maxLoss)
		},
	}

	flags := cmd.Flags()
	flags.StringP("filename", "f", "", "use contents of file")
	flags.StringP("config", "c", "~/.mping.yml", "config path")
	flags.StringP("interface", "I", "", "source interface (name or IP address)")
	flags.IntP("interval", "i", 1000, "interval(ms)")
	flags.IntP("timeout", "t", 1000, "timeout(ms)")
	flags.IntP("count", "", 10, "repeat count")
	flags.StringP("output", "o", shared.FormatTable, "output format ("+strings.Join(shared.OutputFormats, ", ")+")")
	flags.Float64("max-loss", 0, fmt.Sprintf("exit with status %d if any target's loss(%%) exceeds this value", ExitCodeThresholdExceeded))

	return cmd
}

// checkMaxLoss returns an ExitError listing targets whose loss exceeds maxLoss.
// A negative maxLoss disables the check.
func checkMaxLoss(metrics []stats.Metrics, maxLoss float64) error {
	if maxLoss < 0 {
		return nil
	}
	var violated []string
	for _, m := range metrics {
		if m.GetLoss() > maxLoss {
			violated = append(violated, fmt.Sprintf("%s(%.1f%%)", m.GetName(), m.GetLoss()))
		}
	}
	if len(violated) == 0 {
		return nil
	}
	return &ExitError{
		Code: ExitCodeThresholdExceeded,
		Err: fmt.Errorf("%d target(s) exceeded max loss %.1f%%: %s",
			len(violated), maxLoss, strings.Join(violated, ", ")),
	}
}

// progressWriter returns stderr when it is attached to a terminal,
// otherwise a writer that discards progress output.
func progressWriter() io.Writer {
	fi, err := os.Stderr.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return io.Discard
	}
	return os.Stderr
}
