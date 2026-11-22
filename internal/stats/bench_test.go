package stats

import (
	"fmt"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
)

// benchSent returns the send time of the i-th probe: 1 ms apart and all in
// the past, so that each result is settled and applied as it arrives.
func benchSent(i int) time.Time { return t0.Add(time.Duration(i) * time.Millisecond) }

func benchDetails() map[string]*prober.ProbeDetails {
	return map[string]*prober.ProbeDetails{
		"icmp": {ProbeType: "icmpv4", ICMP: &prober.ICMPDetails{ReplyTTL: 54}},
		"https": {ProbeType: "https", HTTP: &prober.HTTPDetails{
			StatusCode: 200, RemoteAddr: "192.0.2.1:443",
			TLS: &prober.TLSDetails{Version: "TLS 1.3", SubjectCN: "example.com", Issuer: "CA", NotAfter: t0},
		}},
		"tcp": nil,
	}
}

func BenchmarkSuccess(b *testing.B) {
	for name, d := range benchDetails() {
		b.Run(name, func(b *testing.B) {
			mm := NewMetricsManager().(*metricsManager)
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				mm.SuccessWithDetails("h", time.Millisecond, benchSent(i), d)
			}
		})
	}
}

func BenchmarkFailed(b *testing.B) {
	mm := NewMetricsManager().(*metricsManager)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		mm.FailedWithDetails("h", benchSent(i), "timeout", nil)
	}
}

func BenchmarkSortBy(b *testing.B) {
	for name, d := range benchDetails() {
		b.Run(name, func(b *testing.B) {
			mm := NewMetricsManager().(*metricsManager)
			for t := range 1000 {
				for i := range 5 {
					mm.SuccessWithDetails(fmt.Sprintf("h%d", t), time.Millisecond, benchSent(i), d)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				mm.SortBy(Host, true)
			}
		})
	}
}
