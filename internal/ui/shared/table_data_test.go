package shared

import (
	"testing"

	"github.com/servak/mping/internal/stats"
)

func TestNewTableDataRows(t *testing.T) {
	metrics := exportTestMetrics()
	td := NewTableData(metrics, stats.Fail, false)

	if len(td.Headers) != len(td.Columns) {
		t.Fatalf("%d headers for %d columns", len(td.Headers), len(td.Columns))
	}
	if got := td.Headers[td.ColumnIndex(ColumnFail)]; got != "Fail ↓" {
		t.Errorf("sorted header = %q, want %q", got, "Fail ↓")
	}
	if got := td.Headers[td.ColumnIndex(ColumnFailReason)]; got != "FAIL Reason" {
		t.Errorf("unsortable header = %q", got)
	}
	if len(td.Rows) != len(metrics) {
		t.Fatalf("%d rows for %d metrics", len(td.Rows), len(metrics))
	}
	for i, r := range td.Rows {
		if r.Metric.GetName() != metrics[i].GetName() {
			t.Errorf("row %d metric = %s, want %s", i, r.Metric.GetName(), metrics[i].GetName())
		}
		if len(r.Cells) != len(td.Columns) {
			t.Errorf("row %d has %d cells, want %d", i, len(r.Cells), len(td.Columns))
		}
		if got := r.Cells[td.ColumnIndex(ColumnHost)]; got != metrics[i].GetName() {
			t.Errorf("row %d host cell = %q", i, got)
		}
	}
	if m, ok := td.GetMetricAtRow(1); !ok || m.GetName() != "down.example.com" {
		t.Errorf("GetMetricAtRow(1) = %v, %v", m, ok)
	}
	if _, ok := td.GetMetricAtRow(len(metrics)); ok {
		t.Error("GetMetricAtRow out of range should fail")
	}
}

func TestColumnIndexUnknown(t *testing.T) {
	td := &TableData{}
	if got := td.ColumnIndex(ColumnHost); got != -1 {
		t.Errorf("ColumnIndex on empty table = %d, want -1", got)
	}
}
