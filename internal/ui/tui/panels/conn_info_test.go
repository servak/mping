package panels

import (
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/ui/shared"
)

// plainText strips the tview color tags
func plainText(s string) string {
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetText(s)
	return tv.GetText(true)
}

func onlyMetrics(t *testing.T, mm stats.MetricsManager) stats.Metrics {
	t.Helper()
	ms := mm.SortBy(stats.Host, true)
	if len(ms) != 1 {
		t.Fatalf("%d metrics, want 1", len(ms))
	}
	return ms[0]
}

func TestFormatConnInfoReplyTTL(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]
	ts := time.Date(2026, 1, 1, 9, 12, 0, 0, time.Local)
	ev := func(i, ttl int) *prober.Event {
		return &prober.Event{Key: "h", DisplayName: "h", Result: prober.SUCCESS, SentTime: ts.Add(time.Duration(i) * time.Second), Rtt: time.Millisecond,
			Details: &prober.ProbeDetails{ProbeType: "icmpv4", ICMP: &prober.ICMPDetails{ReplyTTL: ttl}}}
	}
	// A change is recorded once the new TTL is seen on 3 consecutive replies
	mm := statstest.FromEvents(t, ev(0, 54), ev(3, 52), ev(4, 52), ev(5, 52), ev(6, 53), ev(7, 53), ev(8, 53))
	got := plainText(formatConnInfo(onlyMetrics(t, mm), &theme, ts))
	for _, want := range []string{
		"Reply TTL: 53 (≈11 hops, est.), range 52–54",
		"TTL Changes: 2",
		"09:12:06  TTL 52→53 (-1 hop)",
		"09:12:03  TTL 54→52 (+2 hops)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if i, j := strings.Index(got, "09:12:06"), strings.Index(got, "09:12:03"); i > j {
		t.Errorf("changes not newest first:\n%s", got)
	}

	// Truncated list shows how many more there are
	var events []*prober.Event
	for i := 0; i < 16*3; i++ {
		events = append(events, ev(i, 100+i/3))
	}
	got = plainText(formatConnInfo(onlyMetrics(t, statstest.FromEvents(t, events...)), &theme, ts))
	if !strings.Contains(got, "TTL Changes: 15") || !strings.Contains(got, "... 5 more") {
		t.Errorf("truncation not shown:\n%s", got)
	}

	// No hop count across different initial TTLs
	got = plainText(formatConnInfo(onlyMetrics(t, statstest.FromEvents(t, ev(0, 54), ev(1, 245), ev(2, 245), ev(3, 245))), &theme, ts))
	if !strings.Contains(got, "09:12:01  TTL 54→245 (different initial TTL)") {
		t.Errorf("initial TTL change not shown:\n%s", got)
	}

	// Constant TTL: no range, no changes
	got = plainText(formatConnInfo(onlyMetrics(t, statstest.FromEvents(t, ev(0, 64), ev(1, 64))), &theme, ts))
	if got != "Reply TTL: 64 (≈0 hops, est.)" {
		t.Errorf("constant TTL = %q", got)
	}

	// Unknown TTL (e.g. Windows): nothing at all
	if got := formatConnInfo(onlyMetrics(t, statstest.FromEvents(t, ev(0, 0))), &theme, ts); got != "" {
		t.Errorf("unknown TTL = %q, want empty", got)
	}
}

func TestFormatConnInfoHTTP(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	ev := func(i int, h *prober.HTTPDetails) *prober.Event {
		return &prober.Event{Key: "h", DisplayName: "h", Result: prober.SUCCESS, SentTime: now.Add(time.Duration(i) * time.Second), Rtt: time.Millisecond,
			Details: &prober.ProbeDetails{ProbeType: "https", HTTP: h}}
	}
	tlsd := &prober.TLSDetails{Version: "TLS 1.3", SubjectCN: "example.com", Issuer: "R3", NotAfter: now.Add(48 * 24 * time.Hour)}
	mm := statstest.FromEvents(t,
		ev(0, &prober.HTTPDetails{StatusCode: 200, RemoteAddr: "192.0.2.9:443"}),
		ev(1, &prober.HTTPDetails{StatusCode: 200, RemoteAddr: "1.1.1.1:443", TLS: tlsd}),
		ev(2, &prober.HTTPDetails{StatusCode: 200}), // no connection info
	)
	raw := formatConnInfo(onlyMetrics(t, mm), &theme, now)
	got := plainText(raw)
	for _, want := range []string{
		"Remote: 1.1.1.1:443",
		"TLS: TLS 1.3",
		"Certificate: CN example.com / Issuer R3 / expires 2027-01-02 (in 48d)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(raw, "["+theme.Warning+"]expires") || strings.Contains(raw, "["+theme.Error+"]expires") {
		t.Errorf("48 days left must not be highlighted: %q", raw)
	}

	// A rejected response (503 from a new peer) replaces the shown peer
	rejected := ev(3, &prober.HTTPDetails{StatusCode: 503, RemoteAddr: "198.51.100.7:443"})
	rejected.Result, rejected.Message = prober.FAILED, "unexpected status code: 503"
	statstest.Feed(t, mm, rejected)
	got = plainText(formatConnInfo(onlyMetrics(t, mm), &theme, now))
	if !strings.Contains(got, "Remote: 198.51.100.7:443") || strings.Contains(got, "Certificate") {
		t.Errorf("after a rejected response:\n%s", got)
	}

	tests := []struct {
		left  time.Duration
		color string
		rel   string
	}{
		{20 * 24 * time.Hour, theme.Warning, "(in 20d)"},
		{3 * 24 * time.Hour, theme.Error, "(in 3d)"},
		{5 * time.Hour, theme.Error, "(in 5h)"},
		{-time.Hour, theme.Error, "(expired)"},
	}
	for _, tt := range tests {
		s := formatCertExpiry(now.Add(tt.left), now, &theme)
		if !strings.HasPrefix(s, "["+tt.color+"]expires") || !strings.Contains(s, tt.rel) {
			t.Errorf("expiry in %v = %q, want color %s and %s", tt.left, s, tt.color, tt.rel)
		}
	}
}
