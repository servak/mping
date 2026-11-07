package stats

import (
	"testing"

	"github.com/servak/mping/internal/prober"
)

func TestRegisterKeepsProbeType(t *testing.T) {
	mm := NewMetricsManager()
	ch := make(chan *prober.Event, 4)
	ch <- &prober.Event{Key: "192.0.2.1", DisplayName: "a(192.0.2.1)", Result: prober.REGISTER, ProbeType: "icmpv4"}
	ch <- &prober.Event{Key: "https://b.example/", DisplayName: "https://b.example/", Result: prober.REGISTER, ProbeType: "https"}
	ch <- &prober.Event{Key: "unknown", Result: prober.SUCCESS} // created on demand
	close(ch)
	<-mm.Subscribe(ch)

	got := map[string]string{}
	for _, m := range mm.SortBy(Host, true) {
		got[m.GetName()] = m.GetProbeType()
	}
	want := map[string]string{"a(192.0.2.1)": "icmpv4", "https://b.example/": "https", "unknown": ""}
	for name, pt := range want {
		if got[name] != pt {
			t.Errorf("GetProbeType() of %s = %q, want %q", name, got[name], pt)
		}
	}
}
