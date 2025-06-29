package command

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui"
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
			sourceInterface, err := flags.GetString("interface")
			if err != nil {
				return err
			}

			hosts := parseHostnames(args, filename)
			if len(hosts) == 0 {
				cmd.Println("Please set hostname or ip.")
				cmd.Help()
				return nil
			}

			cfg, _ := config.LoadFile(path)
			cfg.SetTitle(title)
			cfg.SetSourceInterface(sourceInterface)
			_interval := time.Duration(interval) * time.Millisecond
			_timeout := time.Duration(timeout) * time.Millisecond

			res := make(chan *prober.Event)
			probeTargets := splitProber(addDefaultProbeType(hosts), cfg)
			manager := stats.NewMetricsManager()
			probers := setupProbers(probeTargets, res, manager)
			manager.Subscribe(res)
			var wg sync.WaitGroup
			for _, p := range probers {
				wg.Add(1)
				go func(p prober.Prober) {
					p.Start(res, _interval, _timeout)
					wg.Done()
				}(p)
			}
			go func() {
				startCUI(manager, cfg.UI.CUI, _interval)
				for _, p := range probers {
					p.Stop()
				}
			}()

			cmd.Print("Waiting for the results of the probe. Please stand by.")
			wg.Wait()
			cmd.Print("\r")
			t := ui.TableRender(manager, stats.Success)
			t.SetStyle(table.StyleLight)
			cmd.Println(t.Render())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("filename", "f", "", "use contents of file")
	flags.StringP("title", "n", "", "print title")
	flags.StringP("config", "c", "~/.mping.yml", "config path")
	flags.StringP("interface", "I", "", "source interface (name or IP address)")
	flags.IntP("interval", "i", 1000, "interval(ms)")
	flags.IntP("timeout", "t", 1000, "timeout(ms)")

	return cmd
}

func setupProbers(probeTargets map[*prober.ProberConfig][]string, res chan *prober.Event, manager *stats.MetricsManager) []prober.Prober {
	var probers []prober.Prober
	for cfg, targets := range probeTargets {
		p, err := newProber(cfg, manager, targets)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		probers = append(probers, p)
	}
	return probers
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
		// Handle tcp:// URLs specially
		if strings.HasPrefix(t, "tcp://") {
			target := strings.TrimPrefix(t, "tcp://")
			for k, c := range cfg.Prober {
				if k == "tcp" {
					rules[c] = append(rules[c], target)
					break
				}
			}
			continue
		}
		
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
	case prober.HTTP, prober.HTTPS:
		var ts []string
		for _, h := range targets {
			t := fmt.Sprintf("%s:%s", cfg.Probe, h)
			ts = append(ts, t)
			manager.Register(t, t)
		}
		probe = prober.NewHTTPProber(unique(ts), cfg.HTTP)
	case prober.TCP:
		var ts []string
		for _, h := range targets {
			t := fmt.Sprintf("%s://%s", cfg.Probe, h)
			ts = append(ts, t)
			// Register with host:port as key for display consistency
			manager.Register(h, h)
		}
		probe = prober.NewTCPProber(unique(ts), cfg.TCP)
	default:
		err = fmt.Errorf("%s not found, please set implement prober", cfg.Probe)
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
