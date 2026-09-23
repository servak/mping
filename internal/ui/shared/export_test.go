package shared

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	if records[0][0] != "host" || records[0][9] != "jitter_ms" {
		t.Errorf("unexpected header: %v", records[0])
	}
	if records[1][6] != "10" || records[1][10] != "2026-01-02T03:04:05Z" || records[1][11] != "" {
		t.Errorf("unexpected healthy row: %v", records[1])
	}
	// Fail reason containing a comma must round-trip intact
	if records[2][12] != "timeout, no reply" {
		t.Errorf("fail reason = %q", records[2][12])
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
