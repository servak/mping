package shared

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
)

func exportTestMetrics() []stats.Metrics {
	succ := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return []stats.Metrics{
		stats.NewMetricsForTest("healthy.example.com", 10, 3, 3, 0, 0,
			30*time.Millisecond, 10*time.Millisecond, 5*time.Millisecond, 15*time.Millisecond, 12500*time.Microsecond,
			succ, time.Time{}, ""),
		stats.NewMetricsForTest("down.example.com", 10, 3, 0, 3, 100,
			0, 0, 0, 0, 0,
			time.Time{}, succ, "timeout, no reply"),
	}
}

func TestWriteReportJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReport(&buf, FormatJSON, exportTestMetrics(), stats.Host, true); err != nil {
		t.Fatalf("WriteReport() error = %v", err)
	}

	var got []TargetReport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(got))
	}
	h := got[0]
	if h.Host != "healthy.example.com" || h.Sent != 3 || h.Success != 3 || h.AvgRTTMs != 10 || h.LastRTTMs != 12.5 {
		t.Errorf("unexpected healthy report: %+v", h)
	}
	if h.LastSuccessAt == nil || h.LastFailAt != nil {
		t.Errorf("timestamps: want last_success_at set and last_fail_at nil, got %+v", h)
	}
	d := got[1]
	if d.LossPercent != 100 || d.LastFailReason != "timeout, no reply" || d.LastSuccessAt != nil {
		t.Errorf("unexpected down report: %+v", d)
	}
	// Zero timestamps must be omitted, not rendered as 0001-01-01
	if strings.Contains(buf.String(), "0001-01-01") {
		t.Errorf("zero time leaked into JSON output:\n%s", buf.String())
	}
}

func TestWriteReportCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReport(&buf, FormatCSV, exportTestMetrics(), stats.Host, true); err != nil {
		t.Fatalf("WriteReport() error = %v", err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected header + 2 rows, got %d", len(records))
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[h] = i
	}
	for _, h := range []string{"host", "jitter_ms", "p95_rtt_ms", "outage_count", "last_fail_reason"} {
		if _, ok := col[h]; !ok {
			t.Errorf("header missing %q: %v", h, records[0])
		}
	}
	healthy := records[1]
	if healthy[col["avg_rtt_ms"]] != "10" || healthy[col["last_success_at"]] != "2026-01-02T03:04:05Z" || healthy[col["last_fail_at"]] != "" {
		t.Errorf("unexpected healthy row: %v", healthy)
	}
	// Fail reason containing a comma must round-trip intact
	if got := records[2][col["last_fail_reason"]]; got != "timeout, no reply" {
		t.Errorf("fail reason = %q", got)
	}
}

// metricsWithOutage builds metrics through the public event API:
// ok at t+0s, fail at t+1s..t+3s, ok at t+4s, fail at t+5s..t+7s (ongoing).
func metricsWithOutage(t *testing.T) []stats.Metrics {
	t.Helper()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mm := stats.NewMetricsManager()
	mm.ToggleBeep()
	ch := make(chan *prober.Event, 16)
	done := mm.Subscribe(ch)
	ch <- &prober.Event{Key: "h", DisplayName: "flappy.example.com", Result: prober.REGISTER}
	for i, ok := range []bool{true, false, false, false, true, false, false, false} {
		ev := &prober.Event{Key: "h", SentTime: base.Add(time.Duration(i) * time.Second), Rtt: time.Millisecond}
		if ok {
			ev.Result = prober.SUCCESS
		} else {
			ev.Result, ev.Message = prober.TIMEOUT, "timeout"
		}
		ch <- ev
	}
	close(ch)
	<-done
	return mm.SortBy(stats.Host, true)
}

func TestWriteReportJSONOutages(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReport(&buf, FormatJSON, metricsWithOutage(t), stats.Host, true); err != nil {
		t.Fatal(err)
	}
	var got []TargetReport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.OutageCount != 2 || len(r.Outages) != 2 {
		t.Fatalf("want 2 outages, got %+v", r)
	}
	first, second := r.Outages[0], r.Outages[1]
	if first.DurationMs != 3000 || first.LostProbes != 3 || first.Ongoing || first.End == nil {
		t.Errorf("first outage = %+v", first)
	}
	if !second.Ongoing || second.End != nil {
		t.Errorf("second outage should be ongoing: %+v", second)
	}
}

func TestWriteReportTableOutageTimeline(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReport(&buf, FormatTable, metricsWithOutage(t), stats.Host, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Outages (2)", "03:04:06.000", "03:04:09.000", "3.000s", "(ongoing)"} {
		if !strings.Contains(out, want) {
			t.Errorf("timeline missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	if err := WriteReport(&buf, FormatTable, exportTestMetrics(), stats.Host, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Outages") {
		t.Error("timeline must be omitted when there are no outages")
	}
}

func TestWriteReportTable(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReport(&buf, FormatTable, exportTestMetrics(), stats.Host, true); err != nil {
		t.Fatalf("WriteReport() error = %v", err)
	}
	if !strings.Contains(buf.String(), "healthy.example.com") {
		t.Errorf("table output missing host:\n%s", buf.String())
	}
}

func TestWriteReportUnknownFormat(t *testing.T) {
	if err := WriteReport(&bytes.Buffer{}, "xml", nil, stats.Host, true); err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestCSVHeaderMatchesRecord(t *testing.T) {
	if got, want := len(TargetReport{}.csvRecord()), len(csvHeader); got != want {
		t.Errorf("csvRecord has %d columns, header has %d", got, want)
	}
}
