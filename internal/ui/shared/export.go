package shared

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

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
	P50RTTMs       float64    `json:"p50_rtt_ms"`
	P95RTTMs       float64    `json:"p95_rtt_ms"`
	P99RTTMs       float64    `json:"p99_rtt_ms"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastFailAt     *time.Time `json:"last_fail_at,omitempty"`
	LastFailReason string     `json:"last_fail_reason,omitempty"`

	OutageCount     int            `json:"outage_count"`
	TotalDowntimeMs float64        `json:"total_downtime_ms"`
	LongestOutageMs float64        `json:"longest_outage_ms"`
	Outages         []OutageReport `json:"outages"`
}

// OutageReport is the machine-readable representation of an outage
type OutageReport struct {
	Start      time.Time  `json:"start"`
	End        *time.Time `json:"end,omitempty"` // omitted while ongoing
	DurationMs float64    `json:"duration_ms"`
	LostProbes int        `json:"lost_probes"`
	Ongoing    bool       `json:"ongoing"`
	LastError  string     `json:"last_error,omitempty"`
}

// csvHeader lists CSV columns; it must stay in sync with TargetReport.csvRecord
var csvHeader = []string{
	"host", "sent", "success", "fail", "loss_percent",
	"last_rtt_ms", "avg_rtt_ms", "min_rtt_ms", "max_rtt_ms", "jitter_ms",
	"p50_rtt_ms", "p95_rtt_ms", "p99_rtt_ms",
	"last_success_at", "last_fail_at", "last_fail_reason",
	"outage_count", "total_downtime_ms", "longest_outage_ms",
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
		formatFloat(r.P50RTTMs),
		formatFloat(r.P95RTTMs),
		formatFloat(r.P99RTTMs),
		formatRFC3339(r.LastSuccessAt),
		formatRFC3339(r.LastFailAt),
		r.LastFailReason,
		strconv.Itoa(r.OutageCount),
		formatFloat(r.TotalDowntimeMs),
		formatFloat(r.LongestOutageMs),
	}
}

// NewTargetReport converts metrics into a TargetReport
func NewTargetReport(m stats.Metrics) TargetReport {
	now := time.Now()
	pct := m.GetRTTPercentiles(50, 95, 99)
	summary := m.GetOutageSummary()
	outages := make([]OutageReport, 0)
	for _, o := range m.GetOutages() {
		outages = append(outages, OutageReport{
			Start:      o.Start,
			End:        timePtr(o.End),
			DurationMs: durationToMs(o.Duration(now)),
			LostProbes: o.LostProbes,
			Ongoing:    o.Ongoing(),
			LastError:  o.LastError,
		})
	}
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
		P50RTTMs:       durationToMs(pct[0]),
		P95RTTMs:       durationToMs(pct[1]),
		P99RTTMs:       durationToMs(pct[2]),
		LastSuccessAt:  timePtr(m.GetLastSuccTime()),
		LastFailAt:     timePtr(m.GetLastFailTime()),
		LastFailReason: m.GetLastFailDetail(),

		OutageCount:     summary.Count,
		TotalDowntimeMs: durationToMs(summary.Total),
		LongestOutageMs: durationToMs(summary.Longest),
		Outages:         outages,
	}
}

// WriteReport renders metrics to w in the given format
func WriteReport(w io.Writer, format string, metrics []stats.Metrics, sortKey stats.Key, ascending bool) error {
	switch format {
	case FormatTable:
		t := NewTableData(metrics, sortKey, ascending).ToGoPrettyTable()
		t.SetStyle(table.StyleLight)
		if _, err := fmt.Fprintln(w, t.Render()); err != nil {
			return err
		}
		return WriteOutageTimeline(w, metrics)
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

// WriteOutageTimeline renders every outage across all targets in start-time
// order. Nothing is written when no outage occurred.
func WriteOutageTimeline(w io.Writer, metrics []stats.Metrics) error {
	type row struct {
		host string
		o    stats.Outage
	}
	var rows []row
	for _, m := range metrics {
		for _, o := range m.GetOutages() {
			rows = append(rows, row{host: m.GetName(), o: o})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].o.Start.Before(rows[j].o.Start)
	})

	now := time.Now()
	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetTitle(fmt.Sprintf("Outages (%d)", len(rows)))
	t.AppendHeader(table.Row{"Start", "End", "Duration", "Lost", "Host", "Last Error"})
	for _, r := range rows {
		end := "(ongoing)"
		if !r.o.Ongoing() {
			end = FormatClock(r.o.End)
		}
		t.AppendRow(table.Row{
			FormatClock(r.o.Start), end, FormatOutageDuration(r.o.Duration(now)),
			r.o.LostProbes, r.host, r.o.LastError,
		})
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 3, Align: text.AlignRight},
		{Number: 4, Align: text.AlignRight},
	})
	_, err := fmt.Fprintln(w, t.Render())
	return err
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
