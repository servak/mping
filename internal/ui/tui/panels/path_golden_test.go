package panels

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/trace"
	"github.com/servak/mping/internal/ui/shared"
)

func pathMs(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func resp(addr, name string, recv int, last, avg, best, worst int, stale bool) trace.Responder {
	return trace.Responder{Addr: addr, Name: name, Recv: recv, Last: pathMs(last), Avg: pathMs(avg),
		Best: pathMs(best), Worst: pathMs(worst), Stale: stale}
}

func hopOf(ttl, sent, recv int, rs ...trace.Responder) trace.Hop {
	h := trace.Hop{TTL: ttl, Sent: sent, Recv: recv, Responders: rs}
	if len(rs) > 0 {
		// dominant: most replies, earliest wins ties
		d := 0
		for i, r := range rs {
			if r.Recv > rs[d].Recv {
				d = i
			}
		}
		h.Addr, h.Name, h.Others = rs[d].Addr, rs[d].Name, len(rs)-1
		h.Last, h.Avg, h.Best, h.Worst = rs[d].Last, rs[d].Avg, rs[d].Best, rs[d].Worst
	}
	return h
}

func pathFixtures() map[string]*trace.Result {
	dst := net.ParseIP("192.0.2.10")
	mk := func(hops ...trace.Hop) *trace.Result {
		return &trace.Result{Dst: dst, Rounds: 42, Reached: true, Privileged: true, Hops: hops}
	}
	dest := hopOf(9, 42, 42, resp("192.0.2.10", "", 42, 8, 8, 7, 9, false))
	many := []trace.Responder{resp("10.0.1.1", "", 20, 2, 2, 1, 3, false)}
	for i, a := range []string{"10.0.1.2", "10.0.1.3", "10.0.1.4", "10.0.1.5", "10.0.1.6", "10.0.1.7"} {
		many = append(many, resp(a, "", 4-i/3, 3, 3, 2, 4, false))
	}
	return map[string]*trace.Result{
		"single": mk(
			hopOf(1, 42, 42, resp("192.0.2.1", "gw.example.net", 42, 1, 1, 1, 2, false)),
			hopOf(2, 42, 0),
			dest),
		"multiple": mk(
			hopOf(1, 42, 42, resp("192.0.2.1", "", 42, 1, 1, 1, 2, false)),
			hopOf(2, 42, 42,
				resp("10.0.0.1", "", 24, 1, 1, 1, 2, false),
				resp("10.0.0.5", "", 18, 1, 2, 1, 2, false)),
			dest),
		"stale": mk(
			hopOf(3, 42, 41,
				resp("ae1.r1.example.net", "", 21, 5, 5, 4, 6, false),
				resp("ae2.r2.example.net", "", 20, 5, 5, 4, 7, false)),
			hopOf(4, 42, 41,
				resp("203.0.113.1", "", 40, 5, 5, 4, 6, false),
				resp("192.0.2.9", "", 1, 9, 9, 9, 9, true)),
			dest),
		"truncated": mk(
			hopOf(1, 42, 42, many...),
			dest),
	}
}

// TestFormatPathGolden pins the rendered path view (with tview color tags).
// Run with -update to regenerate after an intended change.
func TestFormatPathGolden(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]
	for name, res := range pathFixtures() {
		t.Run(name, func(t *testing.T) {
			got := FormatPath("192.0.2.10", res, nil, &theme) + "\n"
			path := filepath.Join("testdata", "path_"+name+".golden")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden file (run with -update to create): %v", err)
			}
			if !bytes.Equal([]byte(got), want) {
				t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}

func TestFormatPathResponderRows(t *testing.T) {
	theme := shared.PredefinedThemes["dark"]
	fx := pathFixtures()

	// Single responder: no share value
	if strings.Contains(FormatPath("t", fx["single"], nil, &theme), "100%") {
		t.Error("single responder must not show a share")
	}

	// Stale row: dim color and marker, and the marker survives truncation
	got := FormatPath("t", fx["stale"], nil, &theme)
	if !strings.Contains(got, "["+theme.Secondary+"]") || !strings.Contains(got, "192.0.2.9 (stale)") {
		t.Errorf("stale row not dimmed/marked:\n%s", got)
	}
	long := &trace.Result{Dst: net.ParseIP("192.0.2.10"), Hops: []trace.Hop{hopOf(1, 5, 5,
		resp("a", "x", 4, 1, 1, 1, 1, false),
		resp("b", strings.Repeat("long-name.", 8), 1, 1, 1, 1, 1, true))}}
	if got := FormatPath("t", long, nil, &theme); !strings.Contains(got, "…"+staleSuffix) {
		t.Errorf("truncated stale label lost its marker:\n%s", got)
	}

	// Truncation: 4 sub-rows then "+2 more"
	got = FormatPath("t", fx["truncated"], nil, &theme)
	if !strings.Contains(got, "+2 more") || strings.Contains(got, "10.0.1.7") || !strings.Contains(got, "10.0.1.5") {
		t.Errorf("expected 4 sub-rows and +2 more:\n%s", got)
	}
}
