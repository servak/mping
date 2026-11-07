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
			interval, timeout, err := parseTimingFlags(flags)
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

			cfg, err := loadConfig(path)
			if err != nil {
				return err
			}

			sess, err := newSession(sessionParams{
				Cfg:             cfg,
				Hosts:           hosts,
				SourceInterface: sourceInterface,
				Interval:        interval,
				Timeout:         timeout,
				// No failure hook: the terminal bell would corrupt
				// machine-readable output and is pointless in non-interactive runs.
				Stats: stats.Options{},
			})
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Progress goes to stderr (only when it is a terminal) so that
			// stdout stays clean for piping into other tools.
			progress := progressWriter()
			fmt.Fprint(progress, "probe")
			probeErr := sess.start(ctx)

			// Probers send the first round immediately and then one round per
			// interval, so round N goes out at (N-1)*interval. Stopping half an
			// interval after the last round yields exactly `count` probes without
			// racing the next tick. Stop waits for in-flight probes to finish.
			// Deadlines are measured from a fixed start so sleep overshoot does
			// not accumulate across rounds (the prober's ticker does not drift).
			start := time.Now()
			fmt.Fprint(progress, ".")
			for i := 1; i < counter; i++ {
				time.Sleep(time.Until(start.Add(time.Duration(i) * interval)))
				fmt.Fprint(progress, ".")
			}
			time.Sleep(time.Until(start.Add(time.Duration(counter-1)*interval + interval/2)))

			// Stop probing
			sess.stop()
			fmt.Fprint(progress, "\r\033[K")
			select {
			case err := <-probeErr:
				fmt.Fprintf(os.Stderr, "ProbeManager error: %v\n", err)
			default:
			}

			metrics := sess.metrics.SortBy(stats.Success, true)
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
