package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

const (
	timeFormat = "2006-01-02 15:04:05"
	maxReason  = 80 // characters in a table
)

// clean is for text from the server that others may have determined.
func clean(s string) string { return api.Clean(s, 1000) }

func clock(t time.Time) string { return t.Local().Format(timeFormat) }

func ms(v float64) string {
	return shared.FormatOutageDuration(time.Duration(v * float64(time.Millisecond)))
}

func plural(n int, what string) string {
	if n != 1 {
		what += "s"
	}
	return fmt.Sprintf("%d %s", n, what)
}

func watchID(w api.Watch) string { return fmt.Sprintf("%s (%s)", clean(w.Name), clean(w.ID)) }

func watchState(w api.Watch) string {
	s := clean(w.State)
	if w.Reason != "" {
		s += " (" + clean(w.Reason) + ")"
	}
	return s
}

// left returns the time until the end of a watch.
func left(w api.Watch, now time.Time) string {
	return shared.FormatShortDuration(max(w.ExpiresAt.Sub(now).Round(time.Second), 0))
}

func countsText(w api.Watch) string {
	return fmt.Sprintf("%s: %d down, %d degraded, %d pending, %d ok",
		plural(w.TargetCount, "target"), w.Counts.Down, w.Counts.Degraded, w.Counts.Pending, w.Counts.OK)
}

func newTable(header ...any) table.Writer {
	text.OverrideRuneWidthEastAsianWidth(false)
	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.AppendHeader(header)
	return t
}

func watchTable(watches []api.Watch, now time.Time) string {
	t := newTable("Name", "ID", "State", "Targets", "Down", "Degraded", "OK/Pending", "Left/Stopped")
	for _, w := range watches {
		end := left(w, now)
		if w.StoppedAt != nil {
			end = clock(*w.StoppedAt)
		}
		t.AppendRow(table.Row{clean(w.Name), clean(w.ID), watchState(w), w.TargetCount, w.Counts.Down, w.Counts.Degraded,
			fmt.Sprintf("%d/%d", w.Counts.OK, w.Counts.Pending), end})
	}
	return t.Render()
}

func summaryText(s api.Summary, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", watchID(s.Watch), watchState(s.Watch))
	if s.Error != "" {
		fmt.Fprintf(&b, "error:    %s\n", clean(s.Error))
	}
	fmt.Fprintf(&b, "started:  %s\n", clock(s.StartedAt))
	if s.StoppedAt != nil {
		fmt.Fprintf(&b, "stopped:  %s\n", clock(*s.StoppedAt))
	} else {
		fmt.Fprintf(&b, "ends:     %s (in %s)\n", clock(s.ExpiresAt), left(s.Watch, now))
	}
	fmt.Fprintf(&b, "probes:   every %v, timeout %v", time.Duration(s.IntervalMs)*time.Millisecond, time.Duration(s.TimeoutMs)*time.Millisecond)
	if s.SourceInterface != "" {
		fmt.Fprintf(&b, ", from %s", clean(s.SourceInterface))
	}
	fmt.Fprintf(&b, "\nresult:   %s\n", countsText(s.Watch))
	if s.GapCount > 0 && s.LastGap != nil {
		fmt.Fprintf(&b, "gaps:     %d without probes, the last one %s\n", s.GapCount, gapText(s.LastGap))
	}
	if s.DroppedEvents > 0 {
		fmt.Fprintf(&b, "events:   %d, of which the first %d have been dropped\n", s.LastEventSeq, s.DroppedEvents)
	}
	if len(s.Targets) == 0 {
		return b.String()
	}
	t := newTable("Status", "Host", "Loss", "Consecutive Fails", "Outages", "Last Fail", "Reason")
	for _, r := range s.Targets {
		outages, lastFail := "-", "-"
		if r.OutageCount > 0 {
			outages = fmt.Sprintf("%d/%s", r.OutageCount, ms(r.TotalDowntimeMs))
		}
		if r.LastFailAt != nil {
			lastFail = clock(*r.LastFailAt)
		}
		t.AppendRow(table.Row{clean(r.Status), clean(r.Host), fmt.Sprintf("%.1f%%", r.LossPercent), r.ConsecutiveFailures,
			outages, lastFail, api.Clean(r.LastFailReason, maxReason)})
	}
	b.WriteString(t.Render() + "\n")
	if s.OmittedTargets > 0 {
		fmt.Fprintf(&b, "and %d more that are not ok, see --all\n", s.OmittedTargets)
	}
	return b.String()
}

func gapText(g *api.Gap) string {
	return fmt.Sprintf("%s to %s (%s)", clock(g.Start), clock(g.End), ms(float64(g.DurationMs)))
}

// eventLine returns an event on one line.
func eventLine(e api.Event) string {
	s := fmt.Sprintf("%s #%d %s", e.Time.Local().Format(timeFormat+".000"), e.Seq, clean(e.Kind))
	if e.Target != "" {
		s += " " + clean(e.Target)
	}
	if d := eventDetail(e); d != "" {
		s += ": " + d
	}
	return s
}

func eventDetail(e api.Event) string {
	switch {
	case e.Outage != nil:
		o := *e.Outage
		s := fmt.Sprintf("%d lost", o.LostProbes)
		if e.Kind == string(stats.EventOutageEnd) {
			s = fmt.Sprintf("down for %s, %s", ms(o.DurationMs), s)
			if o.EndUnknown {
				s = "still " + s
			}
		}
		if o.LastError != "" {
			s += ", " + clean(o.LastError)
		}
		return s
	case e.TTL != nil:
		s := fmt.Sprintf("%d -> %d", e.TTL.Old, e.TTL.New)
		if h := e.TTL.HopsDelta; h != nil {
			s += fmt.Sprintf(" (%+d hops)", *h)
		}
		return s
	case e.Peer != nil:
		return clean(e.Peer.Old) + " -> " + clean(e.Peer.New)
	case e.Cert != nil:
		return fmt.Sprintf("expires %s -> %s", clock(e.Cert.OldNotAfter), clock(e.Cert.NewNotAfter))
	case e.Gap != nil:
		return "no probes until " + clock(e.Gap.End) + " (" + ms(float64(e.Gap.DurationMs)) + ")"
	case e.Started != nil:
		return "until " + clock(e.Started.ExpiresAt)
	case e.Extended != nil:
		return fmt.Sprintf("until %s, was %s", clock(e.Extended.NewExpiresAt), clock(e.Extended.OldExpiresAt))
	case e.Stopped != nil:
		s := clean(e.Stopped.Reason)
		if e.Stopped.Error != "" {
			s += ", " + clean(e.Stopped.Error)
		}
		return s
	}
	return ""
}
