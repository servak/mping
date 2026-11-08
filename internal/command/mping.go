package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/servak/mping/internal/beep"
	"github.com/servak/mping/internal/config"
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

			hosts, err := ExpandTargets(args, filename)
			if err != nil {
				return err
			}
			if len(hosts) == 0 {
				if filename != "" {
					// Explicit input that yields nothing is an error, as in batch.
					return errors.New("no targets: please set hostname or ip")
				}
				cmd.Println("Please set hostname or ip.")
				cmd.Help()
				return nil
			}

			cfg, err := loadConfig(path, flags.Changed("config"))
			if err != nil {
				return err
			}
			if err := applyTitle(cfg, flags); err != nil {
				return err
			}

			// The notifier rings the bell on failures unless the host was muted
			// with the "m" key in the TUI.
			notifier := beep.NewNotifier()
			defer notifier.Close()

			sess, err := newSession(sessionParams{
				Cfg:             cfg,
				Hosts:           hosts,
				SourceInterface: sourceInterface,
				Interval:        interval,
				Timeout:         timeout,
				Stats:           stats.Options{OnFailure: notifier.OnFailure},
			})
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probeErr := sess.start(ctx)

			startTUI(sess.metrics, notifier, cfg.UI, interval, timeout)

			// Stop probing when TUI exits
			sess.stop()

			// A ProbeManager failure is reported only now: printing while
			// the TUI owns the screen would corrupt the display.
			select {
			case err := <-probeErr:
				return fmt.Errorf("probe manager: %w", err)
			default:
			}

			// Final results
			metrics := sess.metrics.SortBy(stats.Fail, false)
			return shared.WriteReport(cmd.OutOrStderr(), shared.FormatTable, metrics, stats.Fail, false)
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

// applyTitle overrides the configured UI title only when --title was given,
// so the config file's (or the default) title is kept otherwise.
func applyTitle(cfg *config.Config, flags *pflag.FlagSet) error {
	if !flags.Changed("title") {
		return nil
	}
	title, err := flags.GetString("title")
	if err != nil {
		return err
	}
	cfg.SetTitle(title)
	return nil
}

func startTUI(manager stats.MetricsManager, notifier tui.Notifier, cfg *shared.Config, interval, timeout time.Duration) {
	app := tui.NewTUIApp(manager, notifier, cfg, interval, timeout)

	refreshTime := time.Millisecond * 250 // Minimum refresh time that can be set
	if refreshTime < (interval / 2) {
		refreshTime = interval / 2
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(refreshTime)
		defer ticker.Stop()
		for {
			app.Update()
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	app.Run()
}
