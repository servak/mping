package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/client"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/server"
)

// NewServerCmd returns the server command. Its warnings and logs go to stderr.
func NewServerCmd(version string) *cobra.Command {
	return newServerCmd(version, os.Stderr, osSignals)
}

func newServerCmd(version string, stderr io.Writer, notify signalSource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Runs the server of the watches in the foreground",
		Long: `Runs the server that probes for "mping watch", in the foreground until it is
interrupted or stopped.`,
		Args:              cobra.NoArgs,
		PersistentPreRunE: checkPlatform,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cmd.Flags()
			o := server.Options{Version: version, Logger: slog.New(slog.NewTextHandler(stderr, nil))}
			o.IdleExit, _ = flags.GetDuration("idle-exit")
			if flags.Changed("listen") {
				address, _ := flags.GetString("listen")
				var err error
				if o.Socket, err = parseAddress(address); err != nil {
					return err
				}
			}
			path, _ := flags.GetString("config")
			path, err := serverConfig(path, flags.Changed("config"))
			if err != nil {
				return err
			}
			o.LoadConfig = func() (*config.Config, error) { return loadConfig(path, flags.Changed("config")) }
			if _, err := o.LoadConfig(); err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			sigs, stopSignals := notify()
			defer stopSignals()
			go func() {
				select {
				case <-sigs:
					stopSignals() // a second signal kills
					cancel()
				case <-ctx.Done():
				}
			}()
			return server.Run(ctx, o)
		},
	}
	flags := cmd.Flags()
	flags.StringP("config", "c", "~/.mping.yml", "config path, read again for each watch")
	flags.String("listen", "", "address to listen on, unix:///PATH (default unix://~/.mping/server.sock)")
	flags.Duration("idle-exit", 0, "end when there has been no watch for that long")
	flags.MarkHidden("idle-exit")
	cmd.AddCommand(newServerStopCmd(stderr))
	return cmd
}

// serverConfig returns the absolute path of a config file, which the server
// reads again later. The default one is in the home directory that also has the
// socket.
func serverConfig(path string, explicit bool) (string, error) {
	if home, err := os.UserHomeDir(); err == nil && !explicit {
		return filepath.Join(home, ".mping.yml"), nil
	}
	path, err := config.ExpandHome(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func newServerStopCmd(stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Ends the server, and with it all of its results",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, _ := cmd.Flags().GetBool("force")
			r, err := newRemote(cmd)
			if err != nil {
				return err
			}
			r.announce(cmd.OutOrStdout())
			err = r.Shutdown(cmd.Context(), force)
			var e *api.Error
			switch {
			case errors.Is(err, client.ErrNotRunning):
				fmt.Fprintln(stderr, err) // which was to be achieved
				return nil
			case errors.As(err, &e) && e.Code == api.CodeConflict:
				return r.stillRunning(cmd.Context(), e)
			case err != nil:
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "the server has ended")
			return nil
		},
	}
	addServerFlag(cmd)
	cmd.Flags().BoolP("force", "f", false, "stop running watches, too")
	return cmd
}

// stillRunning adds the watches that keep the server from ending to err.
func (r *remote) stillRunning(ctx context.Context, err *api.Error) error {
	var b strings.Builder
	if list, err := r.List(ctx); err == nil {
		for _, w := range list.Watches {
			if w.State != api.StateStopped {
				fmt.Fprintf(&b, "\n  %s, %s", watchID(w), watchState(w))
				if w.State == api.StateRunning {
					fmt.Fprintf(&b, ", ends in %s", left(w, time.Now()))
				}
			}
		}
	}
	return fmt.Errorf("%w; use -f to stop them%s", err, b.String())
}
