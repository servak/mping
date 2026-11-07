package panels

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
	"github.com/servak/mping/internal/ui/shared"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden files")

// hostDetailFixtures returns deterministic metrics exercising every branch of
// FormatHostDetail (loss colors, zero values, outages, history details)
func hostDetailFixtures(t *testing.T) []stats.Metrics {
	t.Helper()
	ts := time.Date(2024, 1, 1, 15, 30, 45, 0, time.UTC)
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	out := []stats.Metrics{
		stats.NewMetricsForTest("example.com", 1, 100, 95, 5, 5.0, ms(25), ms(30), ms(20), ms(40), ms(25), ts, ts.Add(time.Second), "timeout"),
		stats.NewMetricsForTest("zero.com", 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, time.Time{}, time.Time{}, ""),
		stats.NewMetricsForTest("warn.com", 1, 10, 8, 2, 20, ms(2), ms(1), ms(1), ms(3), ms(1), ts, ts, "refused"),
		stats.NewMetricsForTest("bad.com", 1, 10, 2, 8, 80, 2*time.Second, time.Second, time.Second, time.Second, 500*time.Microsecond, ts, ts, "unreachable"),
	}

	// Recovered outages and history with probe details, built from events
	events := []*prober.Event{{Key: "h", DisplayName: "events.example", Result: prober.REGISTER}}
	details := []*prober.ProbeDetails{
		{ProbeType: "icmpv4", ICMP: &prober.ICMPDetails{Sequence: 7, PacketSize: 64, ICMPType: 0, Payload: "abc"}},
		{ProbeType: "http", HTTP: &prober.HTTPDetails{StatusCode: 200, ResponseSize: 1234}},
		{ProbeType: "dns", DNS: &prober.DNSDetails{UseTCP: true, ResponseCode: 0, AnswerCount: 1, Answers: []string{"example.com. 300 IN A 192.0.2.1"}}},
		{ProbeType: "ntp", NTP: &prober.NTPDetails{Stratum: 2, Offset: 1500}},
		{ProbeType: "tcp"},
		nil,
	}
	pattern := "SFFFSSFFFFSSSSSS"
	for i, c := range pattern {
		ev := &prober.Event{Key: "h", SentTime: ts.Add(time.Duration(i) * time.Second)}
		if c == 'S' {
			ev.Result, ev.Rtt, ev.Details = prober.SUCCESS, ms(i+1), details[i%len(details)]
		} else {
			ev.Result, ev.Message = prober.TIMEOUT, "timeout"
		}
		events = append(events, ev)
	}
	mm := statstest.FromEvents(t, events...)
	return append(out, mm.SortBy(stats.Host, true)...)
}

// TestFormatHostDetailGolden pins the exact host detail markup. The golden
// files were generated while the former single-Sprintf implementation still
// existed and verified to match it byte for byte. Run with -update to
// regenerate after an intended change.
func TestFormatHostDetailGolden(t *testing.T) {
	for _, name := range []string{"dark", "light"} {
		t.Run(name, func(t *testing.T) {
			theme := shared.PredefinedThemes[name]
			var buf bytes.Buffer
			for _, m := range hostDetailFixtures(t) {
				buf.WriteString("==== " + m.GetName() + "\n")
				buf.WriteString(FormatHostDetail(m, &theme))
				buf.WriteString("\n")
			}
			path := filepath.Join("testdata", "host_detail_"+name+".golden")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden file (run with -update to create): %v", err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, buf.String(), want)
			}
			// The fixtures must cover outages and history with details
			for _, s := range []string{"Outages:[", "Recent History", "seq=7", "status=200", "tcp code=0 ans=1 192.0.2.1", "stratum=2", "connection"} {
				if !strings.Contains(buf.String(), s) {
					t.Errorf("fixtures do not cover %q", s)
				}
			}
		})
	}
}
