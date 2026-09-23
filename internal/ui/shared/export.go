package shared

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/servak/mping/internal/stats"
)

// Output formats supported by WriteReport
const (
	FormatTable = "table"
	FormatJSON  = "json"
	FormatCSV   = "csv"
)

// OutputFormats lists all supported output formats
var OutputFormats = []string{FormatTable, FormatJSON, FormatCSV}

// ValidateOutputFormat reports an error if format is not one of OutputFormats
func ValidateOutputFormat(format string) error {
	if slices.Contains(OutputFormats, format) {
		return nil
	}
	return fmt.Errorf("unsupported output format %q (supported: %s)", format, strings.Join(OutputFormats, ", "))
}

// TargetReport is the machine-readable representation of a target's statistics.
// RTT values are expressed in milliseconds so they can be consumed directly by
// tools such as jq or spreadsheet software.
type TargetReport struct {
	Host           string     `json:"host"`
	Sent           int        `json:"sent"`
	Success        int        `json:"success"`
	Fail           int        `json:"fail"`
	LossPercent    float64    `json:"loss_percent"`
	LastRTTMs      float64    `json:"last_rtt_ms"`
	AvgRTTMs       float64    `json:"avg_rtt_ms"`
	MinRTTMs       float64    `json:"min_rtt_ms"`
	MaxRTTMs       float64    `json:"max_rtt_ms"`
	JitterMs       float64    `json:"jitter_ms"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastFailAt     *time.Time `json:"last_fail_at,omitempty"`
	LastFailReason string     `json:"last_fail_reason,omitempty"`
}

// csvHeader lists CSV columns; it must stay in sync with TargetReport.csvRecord
var csvHeader = []string{
	"host", "sent", "success", "fail", "loss_percent",
	"last_rtt_ms", "avg_rtt_ms", "min_rtt_ms", "max_rtt_ms", "jitter_ms",
	"last_success_at", "last_fail_at", "last_fail_reason",
}

// csvRecord renders the report as a CSV row matching csvHeader
func (r TargetReport) csvRecord() []string {
	return []string{
		r.Host,
		strconv.Itoa(r.Sent),
		strconv.Itoa(r.Success),
		strconv.Itoa(r.Fail),
		formatFloat(r.LossPercent),
		formatFloat(r.LastRTTMs),
		formatFloat(r.AvgRTTMs),
		formatFloat(r.MinRTTMs),
		formatFloat(r.MaxRTTMs),
		formatFloat(r.JitterMs),
		formatRFC3339(r.LastSuccessAt),
		formatRFC3339(r.LastFailAt),
		r.LastFailReason,
	}
}

// NewTargetReport converts metrics into a TargetReport
func NewTargetReport(m stats.Metrics) TargetReport {
	return TargetReport{
		Host:           m.GetName(),
		Sent:           m.GetTotal(),
		Success:        m.GetSuccessful(),
		Fail:           m.GetFailed(),
		LossPercent:    math.Round(m.GetLoss()*100) / 100,
		LastRTTMs:      durationToMs(m.GetLastRTT()),
		AvgRTTMs:       durationToMs(m.GetAverageRTT()),
		MinRTTMs:       durationToMs(m.GetMinimumRTT()),
		MaxRTTMs:       durationToMs(m.GetMaximumRTT()),
		JitterMs:       durationToMs(m.GetJitter()),
		LastSuccessAt:  timePtr(m.GetLastSuccTime()),
		LastFailAt:     timePtr(m.GetLastFailTime()),
		LastFailReason: m.GetLastFailDetail(),
	}
}

// WriteReport renders metrics to w in the given format
func WriteReport(w io.Writer, format string, metrics []stats.Metrics, sortKey stats.Key, ascending bool) error {
	switch format {
	case FormatTable:
		t := NewTableData(metrics, sortKey, ascending).ToGoPrettyTable()
		t.SetStyle(table.StyleLight)
		_, err := fmt.Fprintln(w, t.Render())
		return err
	case FormatJSON:
		return writeJSON(w, metrics)
	case FormatCSV:
		return writeCSV(w, metrics)
	default:
		return ValidateOutputFormat(format)
	}
}

func writeJSON(w io.Writer, metrics []stats.Metrics) error {
	reports := make([]TargetReport, 0, len(metrics))
	for _, m := range metrics {
		reports = append(reports, NewTargetReport(m))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(reports)
}

func writeCSV(w io.Writer, metrics []stats.Metrics) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, m := range metrics {
		if err := cw.Write(NewTargetReport(m).csvRecord()); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func durationToMs(d time.Duration) float64 {
	// Round to microsecond precision to keep output readable
	return float64(d.Microseconds()) / 1000
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func formatRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}
