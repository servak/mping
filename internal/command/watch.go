package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/client"
	"github.com/servak/mping/internal/ui/shared"
)

const (
	stopPoll       = 100 * time.Millisecond
	followInterval = time.Second
)

// stopGrace is how much longer than its timeout a watch may take to stop. A
// variable for tests.
var stopGrace = 10 * time.Second

// watchCmd is a run of one of the watch commands.
type watchCmd struct {
	*remote
	cmd     *cobra.Command
	ctx     context.Context
	args    []string
	out     io.Writer // results
	stderr  io.Writer // all else
	json    bool
	version string
}

// NewWatchCmd returns the watch command. Its warnings go to stderr.
func NewWatchCmd(version string) *cobra.Command { return newWatchCmd(version, os.Stderr) }

func newWatchCmd(version string, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "watch",
		Short: "Probes in the background, with results to read at any time",
		Long: `Probes in the background: a watch goes on until its end when the terminal is
closed, and its results can be read during and after it. "mping server" does this.`,
		Example: `mping watch start --name switch-update --for 2h 10.0.0.0/28 https://example.com
mping watch ls
mping watch show switch-update
mping watch events switch-update -F
mping watch stop switch-update`,
		Args:              cobra.NoArgs,
		PersistentPreRunE: checkPlatform,
		RunE:              func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	addServerFlag(root)
	add := func(use, short string, args cobra.PositionalArgs, run func(*watchCmd) error) *cobra.Command {
		cmd := &cobra.Command{Use: use, Short: short, Args: args, RunE: func(cmd *cobra.Command, args []string) error {
			w := &watchCmd{cmd: cmd, ctx: cmd.Context(), args: args, out: cmd.OutOrStdout(), stderr: stderr, version: version}
			return w.run(run)
		}}
		root.AddCommand(cmd)
		return cmd
	}
	name := cobra.MatchAll(cobra.ExactArgs(1), func(_ *cobra.Command, args []string) error {
		if args[0] == "" {
			return errors.New("the name is empty")
		}
		return nil
	})

	start := add("start --name NAME TARGET...", "Starts a watch", cobra.ArbitraryArgs, (*watchCmd).start)
	start.Flags().String("name", "", "name of the watch")
	start.MarkFlagRequired("name")
	start.Flags().String("for", "2h", "duration, e.g. 90m, 2h, 1d12h (at most 7d)")
	start.Flags().StringP("filename", "f", "", "use contents of file")
	start.Flags().StringP("interface", "I", "", "source interface (name or IP address)")
	start.Flags().IntP("interval", "i", 1000, "interval(ms)")
	start.Flags().IntP("timeout", "t", 1000, "timeout(ms)")

	ls := add("ls", "Lists the watches", cobra.NoArgs, (*watchCmd).ls)
	show := add("show NAME", "Shows the state of a watch", name, (*watchCmd).show)
	show.Flags().BoolP("all", "a", false, "show all targets, also those that are ok")
	events := add("events NAME", "Shows what has happened in a watch", name, (*watchCmd).events)
	events.Flags().Int64("after", 0, "only events with a higher number")
	events.Flags().BoolP("follow", "F", false, "go on until the watch has stopped")
	extend := add("extend NAME --for DURATION", "Moves the end of a watch to that long from now", name, (*watchCmd).extend)
	extend.Flags().String("for", "", "duration from now, e.g. 90m, 2h, 1d12h (at most 7d)")
	extend.MarkFlagRequired("for")
	add("stop NAME", "Stops a watch, whose results stay for 24 hours", name, (*watchCmd).stop)
	rm := add("rm NAME", "Removes a watch that has stopped", name, (*watchCmd).rm)
	rm.Flags().BoolP("force", "f", false, "stop the watch if it is running")

	for _, cmd := range []*cobra.Command{start, ls, show, events} {
		cmd.Flags().StringP("output", "o", shared.FormatTable, "output format (table, json)")
	}
	return root
}

func (w *watchCmd) run(f func(*watchCmd) error) error {
	if output := w.cmd.Flags().Lookup("output"); output != nil {
		switch output.Value.String() {
		case shared.FormatTable:
		case shared.FormatJSON:
			w.json = true
		default:
			return fmt.Errorf("unsupported output format %q (supported: table, json)", output.Value)
		}
	}
	var err error
	if w.remote, err = newRemote(w.cmd); err != nil {
		return err
	}
	if w.json {
		w.announce(w.stderr)
	} else {
		w.announce(w.out)
	}
	err = f(w)
	w.warnVersion()
	return w.explain(err)
}

// warnVersion warns once.
func (w *watchCmd) warnVersion() {
	w.remote.warnVersion(w.stderr, w.version)
	w.version = ""
}

func (w *watchCmd) flag(name string) string { return w.cmd.Flag(name).Value.String() }

func (w *watchCmd) isSet(name string) bool { return w.flag(name) == "true" }

func (w *watchCmd) printf(format string, args ...any) { fmt.Fprintf(w.out, format, args...) }

func (w *watchCmd) printJSON(v any) error {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// parseFor returns the seconds of a duration, which may have days.
func parseFor(s string) (int64, error) {
	var total time.Duration
	days, rest, ok := strings.Cut(s, "d")
	if !ok {
		days, rest = "0", s
	}
	n, err := strconv.ParseUint(days, 10, 16)
	if total = time.Duration(n) * 24 * time.Hour; err == nil && rest != "" {
		var d time.Duration
		d, err = time.ParseDuration(rest)
		total += d
	}
	if err != nil || total <= 0 || strings.ContainsAny(rest, "+-") {
		return 0, fmt.Errorf("invalid duration %q: use e.g. 90m, 2h, 1d or 1d12h", s)
	}
	return int64((total + time.Second - 1) / time.Second), nil
}

func (w *watchCmd) start() error {
	req := api.CreateRequest{Name: w.flag("name"), SourceInterface: w.flag("interface")}
	interval, timeout, err := parseTimingFlags(w.cmd.Flags())
	if err != nil {
		return err
	}
	req.IntervalMs, req.TimeoutMs = interval.Milliseconds(), timeout.Milliseconds()
	if req.DurationS, err = parseFor(w.flag("for")); err != nil {
		return err
	}
	if req.Targets, err = collectTargets(w.args, w.flag("filename")); err != nil {
		return err
	}
	if req.Name == "" {
		return errors.New("the name is empty")
	}
	if len(req.Targets) == 0 {
		return errors.New("no targets: please set hostname or ip")
	}
	created, err := w.Create(w.ctx, req)
	if err != nil {
		return err
	}
	if w.json {
		return w.printJSON(created)
	}
	w.printf("started %s: %s, ends in %s (%s)\n", watchID(created), plural(created.TargetCount, "target"),
		left(created, time.Now()), clock(created.ExpiresAt))
	return nil
}

func (w *watchCmd) ls() error {
	list, err := w.List(w.ctx)
	switch {
	case errors.Is(err, client.ErrNotRunning): // so there are none
		fmt.Fprintln(w.stderr, err)
		list.Watches = []api.Watch{}
	case err != nil:
		return err
	case len(list.Watches) == 0:
		fmt.Fprintln(w.stderr, "no watches")
	}
	if w.json {
		return w.printJSON(list)
	}
	if len(list.Watches) > 0 {
		w.printf("%s\n", watchTable(list.Watches, time.Now()))
	}
	return nil
}

func (w *watchCmd) show() error {
	sum, err := w.Summary(w.ctx, w.args[0], w.isSet("all"))
	if err != nil {
		return err
	}
	if w.json {
		return w.printJSON(sum)
	}
	w.printf("%s", summaryText(sum, time.Now()))
	return nil
}

// events prints JSON lines, so that following is no special case.
func (w *watchCmd) events() error {
	after, err := w.cmd.Flags().GetInt64("after")
	if err != nil {
		return err
	}
	if after < 0 {
		return errors.New("--after must not be negative")
	}
	enc := json.NewEncoder(w.out)
	for id := w.args[0]; ; {
		// Before the events, which are then all if the watch has stopped
		watch, err := w.Get(w.ctx, id)
		if err != nil {
			return err
		}
		id = watch.ID // the name may pass to another watch
		w.warnVersion()
		for more := true; more; {
			page, err := w.Events(w.ctx, id, after)
			if err != nil {
				return err
			}
			if page.Truncated {
				fmt.Fprintf(w.stderr, "warning: events after #%d have been dropped, the next one is #%d\n", after, page.Next-int64(len(page.Events))+1)
			}
			for _, e := range page.Events {
				if !w.json {
					w.printf("%s\n", eventLine(e))
				} else if err := enc.Encode(e); err != nil {
					return err
				}
			}
			after, more = page.Next, page.More
		}
		if !w.isSet("follow") || watch.State == api.StateStopped {
			return nil
		}
		time.Sleep(followInterval)
	}
}

func (w *watchCmd) extend() error {
	seconds, err := parseFor(w.flag("for"))
	if err != nil {
		return err
	}
	watch, err := w.Extend(w.ctx, w.args[0], seconds)
	if err != nil {
		return err
	}
	w.printf("%s ends in %s (%s)\n", watchID(watch), left(watch, time.Now()), clock(watch.ExpiresAt))
	return nil
}

func (w *watchCmd) stop() error {
	watch, err := w.Stop(w.ctx, w.args[0])
	if err != nil {
		return err
	}
	for deadline := time.Now().Add(time.Duration(watch.TimeoutMs)*time.Millisecond + stopGrace); watch.State != api.StateStopped; {
		if time.Now().After(deadline) {
			return fmt.Errorf("%s has not stopped yet", watchID(watch))
		}
		time.Sleep(stopPoll)
		detail, err := w.Get(w.ctx, watch.ID)
		if err != nil {
			return err
		}
		watch = detail.Watch
	}
	w.printf("%s %s: %s\n", watchID(watch), watchState(watch), countsText(watch))
	return nil
}

func (w *watchCmd) rm() error {
	watch, err := w.Get(w.ctx, w.args[0])
	if err != nil {
		return err
	}
	err = w.Delete(w.ctx, watch.ID, w.isSet("force"))
	var e *api.Error
	if errors.As(err, &e) && e.Code == api.CodeConflict {
		return fmt.Errorf("%w; use -f to stop and remove it", err)
	}
	if err != nil {
		return err
	}
	w.printf("removed %s\n", watchID(watch.Watch))
	return nil
}
