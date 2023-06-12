package command

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui"
	"github.com/spf13/cobra"
)

func NewPingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "mping [IP or HOSTNAME]...",
		Short:         "",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.MinimumNArgs(0),
		Example: `mping 1.1.1.1 8.8.8.8
mping icmpv6:google.com
mping http://google.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			interval, err := flags.GetInt("interval")
			if err != nil {
				return err
			}
			timeout, err := flags.GetInt("timeout")
			if err != nil {
				return err
			}
			if interval == 0 && timeout == 0 {
				return errors.New("both interval and timeout can't be zero")
			} else if interval == 0 {
				return errors.New("interval can't be zero")
			} else if timeout == 0 {
				return errors.New("timeout can't be zero")
			}
			title, err := flags.GetString("title")
			if err != nil {
				return err
			}
			path, err := flags.GetString("config")
			if err != nil {
				return err
			}
			filename, err := flags.GetString("filename")
			if err != nil {
				return err
			}

			hosts := parseHostnames(args, filename)
			if len(hosts) == 0 {
				cmd.Println("Please set hostname or ip.")
				cmd.Help()
				return nil
			}

			cfgPath, _ := filepath.Abs(path)
			cfg, _ := config.LoadFile(cfgPath)
			cfg.SetTitle(title)
			_interval := time.Duration(interval) * time.Millisecond
			_timeout := time.Duration(timeout) * time.Millisecond

			res := make(chan *prober.Event)
			probeTargets := splitProber(addDefaultProbeType(hosts), cfg)
			manager := stats.NewMetricsManager()
			startProbers(probeTargets, res, _interval, _timeout, manager)
			manager.Subscribe(res)
			startCUI(manager, cfg.UI.CUI, _interval)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("filename", "f", "", "use contents of file")
	flags.StringP("title", "n", "", "print title")
	flags.StringP("config", "c", "~/.mping.yml", "config path")
	flags.IntP("interval", "i", 1000, "interval(ms)")
	flags.IntP("timeout", "t", 1000, "timeout(ms)")

	return cmd
}

func startProbers(probeTargets map[*prober.ProberConfig][]string, res chan *prober.Event, interval, timeout time.Duration, manager *stats.MetricsManager) {
	for cfg, targets := range probeTargets {
		prober, err := newProber(cfg, manager, targets)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		go prober.Start(res, interval, timeout)
	}
}

func startCUI(manager *stats.MetricsManager, cui *ui.CUIConfig, interval time.Duration) {
	r, err := ui.NewCUI(manager, cui, interval)
	if err != nil {
		fmt.Println(err)
		return
	}

	refreshTime := time.Millisecond * 250 // Minimum refresh time that can be set
	if refreshTime < (interval / 2) {
		refreshTime = interval / 2
	}

	go func() {
		for {
			time.Sleep(refreshTime)
			r.Update()
		}
	}()

	r.Run()
}

func addDefaultProbeType(hostnames []string) []string {
	var res []string
	for _, h := range hostnames {
		if strings.Contains(h, ":") {
			res = append(res, h)
			continue
		}
		addr := net.ParseIP(h)
		if addr == nil || addr.To4() != nil {
			res = append(res, fmt.Sprintf("%s:%s", prober.ICMPV4, h))
		} else {
			res = append(res, fmt.Sprintf("%s:%s", prober.ICMPV6, h))
		}
	}
	return res
}

func splitProber(targets []string, cfg *config.Config) map[*prober.ProberConfig][]string {
	rules := make(map[*prober.ProberConfig][]string)
	for _, t := range targets {
		probeTypeAndTarget := strings.SplitN(t, ":", 2)
		if len(probeTypeAndTarget) != 2 {
			continue
		}
		for k, c := range cfg.Prober {
			if probeTypeAndTarget[0] == k {
				rules[c] = append(rules[c], probeTypeAndTarget[1])
				break
			}
		}
	}
	return rules
}

func newProber(cfg *prober.ProberConfig, manager *stats.MetricsManager, targets []string) (prober.Prober, error) {
	var (
		probe prober.Prober
		err   error
	)
	switch cfg.Probe {
	case prober.ICMPV4, prober.ICMPV6:
		resolvType := "ip4"
		if cfg.Probe == prober.ICMPV6 {
			resolvType = "ip6"
		}
		var addrs []*net.IPAddr
		for _, h := range targets {
			ip, err := net.ResolveIPAddr(resolvType, h)
			if err != nil {
				continue
			}
			addrs = append(addrs, ip)
			name := ip.String()
			if net.ParseIP(h) == nil {
				name = fmt.Sprintf("%s(%s)", h, name)
			}
			manager.Register(ip.String(), name)
		}
		probe, err = prober.NewICMPProber(cfg.Probe, uniqueStringer(addrs), cfg.ICMP)
	case prober.HTTP:
		var ts []string
		for _, h := range targets {
			t := "http:" + h
			ts = append(ts, t)
			manager.Register(t, t)
		}
		probe = prober.NewHTTPProber(unique(ts), cfg.HTTP)
	default:
		err = fmt.Errorf("%s not found. please set implement prober.", cfg.Probe)
	}
	return probe, err
}

func unique[T comparable](s []T) []T {
	inResult := make(map[T]bool)
	var result []T
	for _, str := range s {
		if _, ok := inResult[str]; !ok {
			inResult[str] = true
			result = append(result, str)
		}
	}
	return result
}

func uniqueStringer[T fmt.Stringer](s []T) []T {
	inResult := make(map[string]bool)
	var result []T
	for _, str := range s {
		if _, ok := inResult[str.String()]; !ok {
			inResult[str.String()] = true
			result = append(result, str)
		}
	}
	return result
}
