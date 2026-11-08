package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/prober"
)

func TestLoadMergesProberDefaults(t *testing.T) {
	t.Run("partial https entry keeps default tls", func(t *testing.T) {
		cfg, err := Load("prober:\n  https:\n    probe: http\n    http:\n      expect_codes: \"200\"\n")
		if err != nil {
			t.Fatal(err)
		}
		h := cfg.Prober["https"].HTTP
		if h.ExpectCodes != "200" {
			t.Errorf("expect_codes = %q, want 200", h.ExpectCodes)
		}
		if h.TLS == nil || !h.TLS.ShouldSkipVerify() {
			t.Errorf("default tls skip_verify lost: %+v", h.TLS)
		}
	})

	t.Run("https with only probe passes validation", func(t *testing.T) {
		cfg, err := Load("prober:\n  https:\n    probe: http\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Prober["https"].HTTP == nil || cfg.Prober["https"].HTTP.ExpectCodes != "200-299" {
			t.Errorf("default http section not kept: %+v", cfg.Prober["https"].HTTP)
		}
	})

	t.Run("empty tls block keeps skip_verify default", func(t *testing.T) {
		cfg, err := Load("prober:\n  https:\n    probe: http\n    http:\n      expect_codes: \"200-299\"\n      tls: {}\n")
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Prober["https"].HTTP.TLS.ShouldSkipVerify() {
			t.Error("tls: {} must not enable certificate verification")
		}
	})

	t.Run("explicit skip_verify false is honored", func(t *testing.T) {
		cfg, err := Load("prober:\n  https:\n    http:\n      tls:\n        skip_verify: false\n")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Prober["https"].HTTP.TLS.ShouldSkipVerify() {
			t.Error("skip_verify: false should verify certificates")
		}
	})

	t.Run("icmp and dns defaults are merged", func(t *testing.T) {
		cfg, err := Load("prober:\n  icmpv4:\n    icmp:\n      ttl: 32\n  dns:\n    dns:\n      server: 1.1.1.1\n")
		if err != nil {
			t.Fatal(err)
		}
		icmp := cfg.Prober["icmpv4"]
		if icmp.Probe != prober.ICMPV4 || icmp.ICMP.Body != DefaultICMPBody || icmp.ICMP.TTL != 32 {
			t.Errorf("icmpv4 not merged: probe=%s %+v", icmp.Probe, icmp.ICMP)
		}
		dns := cfg.Prober["dns"].DNS
		if dns.Server != "1.1.1.1" || dns.Port != 53 || dns.RecordType != "A" || !dns.RecursionDesired {
			t.Errorf("dns not merged: %+v", dns)
		}
	})

	t.Run("http headers are merged and other defaults kept", func(t *testing.T) {
		cfg, err := Load("prober:\n  http:\n    http:\n      headers:\n        Host: vhost.example.test\n")
		if err != nil {
			t.Fatal(err)
		}
		h := cfg.Prober["http"].HTTP
		if h.Header["Host"] != "vhost.example.test" || h.ExpectCodes != "200-299" {
			t.Errorf("http not merged: %+v", h)
		}
	})

	t.Run("changing probe family replaces the default", func(t *testing.T) {
		cfg, err := Load("prober:\n  http:\n    probe: tcp\n    tcp: {}\n")
		if err != nil {
			t.Fatal(err)
		}
		if p := cfg.Prober["http"]; p.Probe != prober.TCP || p.HTTP != nil {
			t.Errorf("expected fresh tcp config, got %+v", p)
		}
	})

	t.Run("new prober names are not merged", func(t *testing.T) {
		cfg, err := Load("prober:\n  api-check:\n    probe: http\n    http:\n      expect_codes: \"200,201\"\n")
		if err != nil {
			t.Fatal(err)
		}
		if h := cfg.Prober["api-check"].HTTP; h.ExpectCodes != "200,201" || h.TLS != nil {
			t.Errorf("unexpected api-check config: %+v", h)
		}
		if _, ok := cfg.Prober["icmpv4"]; !ok {
			t.Error("default probers should remain")
		}
	})

	t.Run("loading does not mutate later defaults", func(t *testing.T) {
		if _, err := Load("prober:\n  https:\n    http:\n      expect_codes: \"418\"\n"); err != nil {
			t.Fatal(err)
		}
		if got := DefaultConfig().Prober["https"].HTTP.ExpectCodes; got != "200-299" {
			t.Errorf("DefaultConfig mutated: %q", got)
		}
	})

	t.Run("marshal prints merged defaults", func(t *testing.T) {
		cfg, err := Load("prober:\n  https:\n    http:\n      expect_codes: \"200\"\n")
		if err != nil {
			t.Fatal(err)
		}
		out := Marshal(cfg)
		for _, want := range []string{"skip_verify: true", "expect_codes: \"200\"", "body: mping"} {
			if !strings.Contains(out, want) {
				t.Errorf("Marshal output missing %q:\n%s", want, out)
			}
		}
	})
}

// TestLoadedHTTPSConfigProbesTLSServer runs real probes through the ProbeManager
// using configs loaded from YAML against an httptest TLS server (self-signed cert).
func TestLoadedHTTPSConfigProbesTLSServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	tests := []struct {
		name   string
		yaml   string
		target string
	}{
		{"no config", "", "https://" + host},
		{"https override without tls", "prober:\n  https:\n    probe: http\n    http:\n      expect_codes: \"200\"\n", "https://" + host},
		{"https with empty tls block", "prober:\n  https:\n    probe: http\n    http:\n      tls: {}\n", "https://" + host},
		{"probe https without tls", "prober:\n  https:\n    probe: https\n    http:\n      expect_codes: \"200-299\"\n", "https://" + host},
		{"custom prefix with https url", "prober:\n  api-check:\n    probe: http\n    http:\n      expect_codes: \"200,201\"\n", "api-check://https://" + host},
		{"unrelated ui override", "ui:\n  border: false\n", "https://" + host},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.yaml)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			pm := prober.NewProbeManager(cfg.Prober, cfg.Default)
			if err := pm.AddTargets(tt.target); err != nil {
				t.Fatalf("AddTargets: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = pm.Run(ctx, time.Hour, 2*time.Second) }()
			defer pm.Stop()

			deadline := time.After(5 * time.Second)
			for {
				select {
				case e := <-pm.Events():
					if e == nil || e.Result == prober.SENT || e.Result == prober.REGISTER {
						continue
					}
					if e.Result != prober.SUCCESS {
						t.Fatalf("%s => %v %s", e.Key, e.Result, e.Message)
					}
					return
				case <-deadline:
					t.Fatal("timed out waiting for probe result")
				}
			}
		})
	}
}

func TestLoadKeepAliveOverride(t *testing.T) {
	for _, name := range []string{"http", "https"} {
		t.Run("builtin "+name, func(t *testing.T) {
			cfg, err := Load("prober:\n  " + name + ":\n    http:\n      keep_alive: true\n")
			if err != nil {
				t.Fatal(err)
			}
			h := cfg.Prober[name].HTTP
			if !h.KeepAlive {
				t.Error("keep_alive override lost")
			}
			if h.ExpectCodes != "200-299" {
				t.Errorf("expect_codes = %q, want default 200-299", h.ExpectCodes)
			}
			if name == "https" && (h.TLS == nil || !h.TLS.ShouldSkipVerify()) {
				t.Errorf("default tls lost: %+v", h.TLS)
			}
		})
	}

	t.Run("custom prober", func(t *testing.T) {
		cfg, err := Load("prober:\n  api:\n    probe: https\n    http:\n      expect_codes: \"200\"\n      keep_alive: true\n      tls:\n        skip_verify: false\n")
		if err != nil {
			t.Fatal(err)
		}
		h := cfg.Prober["api"].HTTP
		if !h.KeepAlive || h.ExpectCodes != "200" || h.TLS == nil || h.TLS.ShouldSkipVerify() {
			t.Errorf("custom prober config wrong: %+v", h)
		}
	})

	t.Run("keep_alive is visible in printed and default configs", func(t *testing.T) {
		if out := Marshal(DefaultConfig()); !strings.Contains(out, "keep_alive: false") {
			t.Errorf("default config does not show keep_alive:\n%s", out)
		}
		cfg, err := Load("prober:\n  http:\n    http:\n      keep_alive: true\n")
		if err != nil {
			t.Fatal(err)
		}
		if out := Marshal(cfg); !strings.Contains(out, "keep_alive: true") {
			t.Errorf("printed config does not show keep_alive: true:\n%s", out)
		}
	})
}

func TestLoadRejectsDuplicateHostHeader(t *testing.T) {
	if _, err := Load("prober:\n  http:\n    http:\n      headers:\n        Host: a.example\n        host: b.example\n"); err == nil {
		t.Error("expected an error for two Host headers")
	}
}
