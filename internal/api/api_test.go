package api

import (
	"encoding/json"
	"testing"

	"github.com/servak/mping/internal/ui/shared"
)

func TestClean(t *testing.T) {
	for _, tc := range []struct {
		in    string
		limit int
		want  string
	}{
		{"", 99, ""},
		{"connect: connection refused", 99, "connect: connection refused"},
		{"日本語 ok", 99, "日本語 ok"},
		{" a\r\n\tb  c\n", 99, "a b c"},
		{"a\x00b\x07c\x7fd\u0085e", 99, "abcd e"},
		{"\x1b[31mred\x1b[0m", 99, "[31mred[0m"},
		{"\x1b]0;title\x07x", 99, "]0;titlex"},
		{"abc\u202edef\u2066g\u200b\u200fh\ufeff", 99, "abcdefgh"},
		{"tag\U000e0041\U000e0042 vs\ufe0f\U000e0100 f\u3164\u115f\u034f", 99, "tag vs f"},
		{"bad\xff\xfe utf\ufffd", 99, "bad utf"},
		{"line\u2028sep\u2029x", 99, "line sep x"},
		{"12345", 5, "12345"},
		{"12345\x00 ", 5, "12345"},
		{"123456", 5, "12345…"},
		{"日本語日本語", 5, "日本語日本…"},
		{"1234 56", 5, "1234…"},
	} {
		if got := Clean(tc.in, tc.limit); got != tc.want {
			t.Errorf("Clean(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
		}
	}
}

// The outages of a target are those with the end marked, under the name they
// have in a report.
func TestTargetSummaryJSON(t *testing.T) {
	in := TargetSummary{
		TargetReport: shared.TargetReport{Host: "h", Outages: []shared.OutageReport{{}, {}}},
		Outages:      []Outage{{OutageReport: shared.OutageReport{LostProbes: 7}, EndUnknown: true}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Host    string
		Outages []map[string]any
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Host != "h" || len(out.Outages) != 1 || out.Outages[0]["lost_probes"] != 7.0 || out.Outages[0]["end_unknown"] != true {
		t.Errorf("got %s", b)
	}
}
