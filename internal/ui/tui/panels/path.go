package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/trace"
	"github.com/servak/mping/internal/ui/shared"
)

const (
	maxPathHostWidth = 40

	// maxPathSubRows is how many additional responders are listed under a
	// hop before the rest is summarized as "+N more"
	maxPathSubRows = 4

	staleSuffix = " (stale)"
)

// PathPanel shows an MTR-style live path trace for the selected host
type PathPanel struct {
	view      *tview.TextView
	container *tview.Flex
	config    *shared.Config
}

// NewPathPanel creates a new PathPanel
func NewPathPanel(config *shared.Config) *PathPanel {
	view := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	container := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, false)
	return &PathPanel{view: view, container: container, config: config}
}

// GetView returns the underlying tview component
func (p *PathPanel) GetView() tview.Primitive {
	return p.container
}

// Update renders the trace state
func (p *PathPanel) Update(target string, res *trace.Result, err error) {
	theme := p.config.GetTheme()
	p.container.
		SetBorder(true).
		SetTitle(fmt.Sprintf(" [%s]Path ", theme.Primary)).
		SetBackgroundColor(tcell.GetColor(theme.Background)).
		SetBorderColor(tcell.GetColor(theme.Primary))
	p.view.SetBackgroundColor(tcell.GetColor(theme.Background))
	p.view.SetText(FormatPath(target, res, err, theme))
}

// FormatPath renders a trace snapshot with tview color tags
func FormatPath(target string, res *trace.Result, err error, theme *shared.Theme) string {
	switch {
	case target == "":
		return "Select a host to trace its path"
	case err != nil:
		return fmt.Sprintf("[%s]Path trace to %s unavailable:[%s]\n%s",
			theme.Error, tview.Escape(target), theme.Primary, tview.Escape(err.Error()))
	case res == nil:
		return fmt.Sprintf("Starting path trace to %s ...", tview.Escape(target))
	}

	var sb strings.Builder
	title := tview.Escape(target)
	if dst := res.Dst.String(); !strings.Contains(target, dst) {
		title += " (" + dst + ")"
	}
	fmt.Fprintf(&sb, "[%s]%s[%s] · %d rounds\n", theme.Accent, title, theme.Primary, res.Rounds)

	rows := make([][]pathRow, len(res.Hops))
	width := len("Host")
	for i, h := range res.Hops {
		rows[i] = pathHopRows(h)
		for _, r := range rows[i] {
			width = max(width, min(len(r.host()), maxPathHostWidth))
		}
	}

	fmt.Fprintf(&sb, "[%s]%3s  %-*s  %5s  %6s %4s %6s %6s %6s %6s[%s]\n",
		theme.TableHeader, "Hop", width, "Host", "Share", "Loss", "Snt", "Last", "Avg", "Best", "Wrst", theme.Primary)
	df := shared.DurationFormater
	rateLimited := res.HasRateLimitedHops()
	for i, h := range res.Hops {
		loss := h.Loss()
		lossColor := theme.Primary
		switch {
		case h.Sent > 0 && h.Recv == 0:
			lossColor = theme.Secondary
		case loss > 0 && rateLimited:
			// Loss that does not reach the destination is router ICMP
			// rate limiting; don't make it look like a problem
			lossColor = theme.Secondary
		case loss > 10:
			lossColor = theme.Error
		case loss > 0:
			lossColor = theme.Warning
		}
		for j, r := range rows[i] {
			if r.more > 0 {
				fmt.Fprintf(&sb, "[%s]%3s  %-*s[%s]\n", theme.Secondary, "", width, fmt.Sprintf("+%d more", r.more), theme.Primary)
				continue
			}
			share := ""
			if r.share >= 0 {
				share = fmt.Sprintf("%4.0f%%", r.share)
			}
			last := df(r.last)
			rowColor := theme.Primary
			if r.stale {
				last = "-" // the last RTT is old news
				rowColor = theme.Secondary
			}
			host := tview.Escape(r.fit(width))
			if j == 0 {
				fmt.Fprintf(&sb, "[%s]%3d  %-*s  %5s[%s]  [%s]%5.1f%%[%s] %4d [%s]%6s %6s %6s %6s[%s]\n",
					rowColor, h.TTL, width, host, share, theme.Primary, lossColor, loss, theme.Primary, h.Sent,
					rowColor, last, df(r.avg), df(r.best), df(r.worst), theme.Primary)
				continue
			}
			fmt.Fprintf(&sb, "[%s]%3s  %-*s  %5s  %6s %4s %6s %6s %6s %6s[%s]\n",
				rowColor, "", width, host, share, "", "", last, df(r.avg), df(r.best), df(r.worst), theme.Primary)
		}
	}
	if !res.Reached && res.Rounds > 0 {
		fmt.Fprintf(&sb, "[%s]destination has not replied yet[%s]\n", theme.Secondary, theme.Primary)
	}
	if rateLimited {
		fmt.Fprintf(&sb, "[%s]loss only at intermediate hops is usually router ICMP rate limiting[%s]\n", theme.Secondary, theme.Primary)
	}
	if !res.Privileged {
		fmt.Fprintf(&sb, "[%s]unprivileged ICMP: intermediate hops may be missing on Linux[%s]\n", theme.Secondary, theme.Primary)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// pathRow is one rendered line of a hop: the dominant responder (or the hop
// placeholder), another responder, or the "+N more" summary
type pathRow struct {
	label       string
	stale       bool
	share       float64 // percent of the hop's replies; -1 = not shown
	last, avg   time.Duration
	best, worst time.Duration
	more        int // > 0: this is a "+N more" line
}

func (r pathRow) host() string {
	if r.stale {
		return r.label + staleSuffix
	}
	return r.label
}

// fit shortens the label to limit columns, always keeping the stale marker
func (r pathRow) fit(limit int) string {
	suffix := ""
	if r.stale {
		suffix = staleSuffix
	}
	if len(r.label)+len(suffix) <= limit {
		return r.label + suffix
	}
	return r.label[:max(limit-len(suffix)-1, 0)] + "…" + suffix
}

// pathHopRows lays out a hop: the dominant responder first (it carries the
// hop totals), then the other responders in first-seen order
func pathHopRows(h trace.Hop) []pathRow {
	if h.Addr == "" {
		label := "???" // no reply yet, at least one probe timed out
		if h.Sent == 0 {
			label = "waiting" // first probe still in flight
		}
		return []pathRow{{label: label, share: -1, last: h.Last, avg: h.Avg, best: h.Best, worst: h.Worst}}
	}
	multi := len(h.Responders) > 1
	mk := func(r trace.Responder) pathRow {
		row := pathRow{label: responderLabel(r.Addr, r.Name), stale: r.Stale, share: -1,
			last: r.Last, avg: r.Avg, best: r.Best, worst: r.Worst}
		if multi && h.Recv > 0 {
			row.share = float64(r.Recv) / float64(h.Recv) * 100
		}
		return row
	}
	var first pathRow
	var others []pathRow
	found := false
	for _, r := range h.Responders {
		if !found && r.Addr == h.Addr {
			first, found = mk(r), true
			continue
		}
		others = append(others, mk(r))
	}
	if !found { // snapshot without Responders: fall back to the hop itself
		first = pathRow{label: responderLabel(h.Addr, h.Name), share: -1,
			last: h.Last, avg: h.Avg, best: h.Best, worst: h.Worst}
	}
	rows := []pathRow{first}
	rows = append(rows, others[:min(len(others), maxPathSubRows)]...)
	if len(others) > maxPathSubRows {
		rows = append(rows, pathRow{more: len(others) - maxPathSubRows})
	}
	return rows
}

func responderLabel(addr, name string) string {
	if name != "" {
		return name
	}
	return addr
}
