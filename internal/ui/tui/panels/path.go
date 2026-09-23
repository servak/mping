package panels

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/trace"
	"github.com/servak/mping/internal/ui/shared"
)

const maxPathHostWidth = 40

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

	hosts := make([]string, len(res.Hops))
	width := len("Host")
	for i, h := range res.Hops {
		hosts[i] = pathHopHost(h)
		width = max(width, min(len(hosts[i]), maxPathHostWidth))
	}

	fmt.Fprintf(&sb, "[%s]%3s  %-*s  %6s %4s %6s %6s %6s %6s[%s]\n",
		theme.TableHeader, "Hop", width, "Host", "Loss", "Snt", "Last", "Avg", "Best", "Wrst", theme.Primary)
	df := shared.DurationFormater
	rateLimited := res.HasRateLimitedHops()
	for i, h := range res.Hops {
		host := hosts[i]
		if len(host) > width {
			host = host[:width-1] + "…"
		}
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
		fmt.Fprintf(&sb, "%3d  %-*s  [%s]%5.1f%%[%s] %4d %6s %6s %6s %6s\n",
			h.TTL, width, tview.Escape(host), lossColor, loss, theme.Primary, h.Sent,
			df(h.Last), df(h.Avg), df(h.Best), df(h.Worst))
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

func pathHopHost(h trace.Hop) string {
	if h.Addr == "" {
		if h.Sent == 0 {
			return "waiting" // first probe still in flight
		}
		return "???" // no reply yet, at least one probe timed out
	}
	s := h.Addr
	if h.Name != "" {
		s = h.Name
	}
	if h.Others > 0 {
		s += fmt.Sprintf(" +%d", h.Others)
	}
	return s
}
