package trace

import "testing"

func TestHostFromTarget(t *testing.T) {
	tests := []struct {
		in       string
		host     string
		preferV6 bool
	}{
		{"8.8.8.8", "8.8.8.8", false},
		{"example.com", "example.com", false},
		{"example.com(93.184.216.34)", "93.184.216.34", false},
		{"example.com(2606:2800:220:1::1)", "2606:2800:220:1::1", true},
		{"icmpv4://example.com", "example.com", false},
		{"icmpv6://example.com", "example.com", true},
		{"icmpv6:example.com", "example.com", true},
		{"tcp://example.com:443", "example.com", false},
		{"tcp://[2001:db8::1]:443", "2001:db8::1", false},
		{"https://example.com/health?x=1", "example.com", false},
		{"http://127.0.0.1:8080/", "127.0.0.1", false},
		{"dns://8.8.8.8/example.com/A", "8.8.8.8", false},
		{"dns://1.1.1.1:5353/example.com", "1.1.1.1", false},
		{"ntp://pool.ntp.org", "pool.ntp.org", false},
		{"2001:db8::1", "2001:db8::1", false},
		{"my-ping://10.0.0.1", "10.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			host, v6 := HostFromTarget(tt.in)
			if host != tt.host || v6 != tt.preferV6 {
				t.Errorf("HostFromTarget(%q) = (%q, %v), want (%q, %v)", tt.in, host, v6, tt.host, tt.preferV6)
			}
		})
	}
}

func TestResolveLiteral(t *testing.T) {
	ip, err := Resolve("192.0.2.1", true)
	if err != nil || ip.String() != "192.0.2.1" {
		t.Errorf("Resolve literal = %v, %v", ip, err)
	}
}
