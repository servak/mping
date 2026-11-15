package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

// maxDetailTTLChanges is the number of recent TTL changes listed in host details
const maxDetailTTLChanges = 10

// formatConnInfo renders what is known about the path and the peer of a
// host: the ICMP reply TTL and its changes, and the remote address and TLS
// certificate of the latest HTTP response. It returns "" when nothing is
// known, e.g. on Windows (no reply TTL) or for other probe types.
func formatConnInfo(metric stats.Metrics, theme *shared.Theme, now time.Time) string {
	var sections []string
	if s := formatReplyTTL(metric.GetReplyTTL(), theme); s != "" {
		sections = append(sections, s)
	}
	if ci, ok := metric.GetConnInfo(); ok {
		sections = append(sections, formatHTTPConn(ci, theme, now))
	}
	return strings.Join(sections, "\n\n")
}

// formatReplyTTL renders "Reply TTL: 54 (≈10 hops, est.), range 52–54"
// followed by the recent TTL changes, newest first.
func formatReplyTTL(s stats.ReplyTTLStats, theme *shared.Theme) string {
	if s.Last <= 0 {
		return ""
	}
	value := fmt.Sprintf("%d", s.Last)
	if hops, ok := prober.EstimateHops(s.Last); ok {
		value += fmt.Sprintf(" (≈%d %s, est.)", hops, plural(hops, "hop", "hops"))
	}
	if s.Min != s.Max {
		value += fmt.Sprintf(", [%s]range %d–%d[%s]", theme.Warning, s.Min, s.Max, theme.Primary)
	}
	var sb strings.Builder
	sb.WriteString(formatDetailRows([]detailRow{{"Reply TTL", theme.Accent, value}}, theme))
	if s.ChangeCount == 0 {
		return sb.String()
	}
	fmt.Fprintf(&sb, "\n[%s]TTL Changes:[%s] %d", theme.Accent, theme.Primary, s.ChangeCount)
	shown := 0
	for i := len(s.Changes) - 1; i >= 0 && shown < maxDetailTTLChanges; i-- {
		c := s.Changes[i]
		d := c.HopsDelta()
		fmt.Fprintf(&sb, "\n[%s]%s[%s]  [%s]TTL %d→%d (%+d %s)[%s]",
			theme.Timestamp, shared.TimeFormater(c.Time), theme.Primary,
			theme.Warning, c.Old, c.New, d, plural(d, "hop", "hops"), theme.Primary)
		shown++
	}
	if more := s.ChangeCount - shown; more > 0 {
		fmt.Fprintf(&sb, "\n[%s]... %d more[%s]", theme.Secondary, more, theme.Primary)
	}
	return sb.String()
}

// formatHTTPConn renders the remote address and the TLS certificate of the
// latest HTTP(S) response, which may have been a rejected one (e.g. 503)
func formatHTTPConn(h stats.ConnInfo, theme *shared.Theme, now time.Time) string {
	var rows []detailRow
	if h.RemoteAddr != "" {
		rows = append(rows, detailRow{"Remote", theme.Accent, tview.Escape(h.RemoteAddr)})
	}
	if t := h.TLS; t != nil {
		if t.Version != "" {
			rows = append(rows, detailRow{"TLS", theme.Accent, tview.Escape(t.Version)})
		}
		var parts []string
		if t.SubjectCN != "" {
			parts = append(parts, "CN "+tview.Escape(t.SubjectCN))
		}
		if t.Issuer != "" {
			parts = append(parts, "Issuer "+tview.Escape(t.Issuer))
		}
		if !t.NotAfter.IsZero() {
			parts = append(parts, formatCertExpiry(t.NotAfter, now, theme))
		}
		if len(parts) > 0 {
			rows = append(rows, detailRow{"Certificate", theme.Accent, strings.Join(parts, " / ")})
		}
	}
	return formatDetailRows(rows, theme)
}

// formatCertExpiry renders "expires 2027-01-02 (in 48d)": red when expired
// or within 7 days, yellow within 30 days.
func formatCertExpiry(notAfter, now time.Time, theme *shared.Theme) string {
	left := notAfter.Sub(now)
	var rel string
	switch {
	case left <= 0:
		rel = "expired"
	case left < 24*time.Hour:
		rel = fmt.Sprintf("in %dh", int(left.Hours()))
	default:
		rel = fmt.Sprintf("in %dd", int(left.Hours()/24))
	}
	s := fmt.Sprintf("expires %s (%s)", notAfter.UTC().Format("2006-01-02"), rel)
	switch {
	case left < 7*24*time.Hour:
		return fmt.Sprintf("[%s]%s[%s]", theme.Error, s, theme.Primary)
	case left < 30*24*time.Hour:
		return fmt.Sprintf("[%s]%s[%s]", theme.Warning, s, theme.Primary)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 || n == -1 {
		return one
	}
	return many
}
