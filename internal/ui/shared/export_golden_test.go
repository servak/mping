package shared

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/stats/statstest"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden files")

// goldenMetrics returns deterministic metrics covering every column: a
// healthy host, a down host and a host with two recovered outages (no
// ongoing outage, so no duration depends on the current time).
func goldenMetrics(t *testing.T) []stats.Metrics {
	t.Helper()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	events := []*prober.Event{{Key: "h", DisplayName: "flappy.example.com", Result: prober.REGISTER}}
	for i, ok := range []bool{true, false, false, false, true, false, false, true, true} {
		ev := &prober.Event{Key: "h", SentTime: base.Add(time.Duration(i) * time.Second), Rtt: time.Duration(i+1) * time.Millisecond}
		if ok {
			ev.Result = prober.SUCCESS
		} else {
			ev.Result, ev.Message = prober.TIMEOUT, "timeout"
		}
		events = append(events, ev)
	}
	mm := statstest.FromEvents(t, events...)
	return append(exportTestMetrics(), mm.SortBy(stats.Host, true)...)
}

// TestWriteReportGolden pins the exact bytes of every export format so that
// refactoring the table/report code cannot change the output unnoticed.
// Run `go test ./internal/ui/shared -run Golden -update` to regenerate.
func TestWriteReportGolden(t *testing.T) {
	cases := []struct {
		file      string
		format    string
		sortKey   stats.Key
		ascending bool
	}{
		{"table_host_asc.golden", FormatTable, stats.Host, true},
		{"table_fail_desc.golden", FormatTable, stats.Fail, false},
		{"table_lastfail_asc.golden", FormatTable, stats.LastFailTime, true},
		{"report.json.golden", FormatJSON, stats.Host, true},
		{"report.csv.golden", FormatCSV, stats.Host, true},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteReport(&buf, tc.format, goldenMetrics(t), tc.sortKey, tc.ascending); err != nil {
				t.Fatalf("WriteReport() error = %v", err)
			}
			path := filepath.Join("testdata", tc.file)
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
		})
	}
}
