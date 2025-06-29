package command

import (
	"errors"
	"fmt"
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
			
			// Create all available probers
			manager := stats.NewMetricsManager()
			allProbers, err := createAllProbers(cfg)
			if err != nil {
				return fmt.Errorf("failed to create probers: %w", err)
			}
			
			// Use TargetRouter for cleaner target handling
			router := prober.NewTargetRouter(allProbers)
			registrations, err := router.RouteTargets(hosts)
			if err != nil {
				return fmt.Errorf("failed to route targets: %w", err)
			}
			
			// Register metrics
			for target, displayName := range registrations {
				manager.Register(target, displayName)
			}
			
			probers := router.GetActiveProbers()
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






// createAllProbers creates all available probers based on config
func createAllProbers(cfg *config.Config) ([]prober.Prober, error) {
	var probers []prober.Prober
	
	// Create ICMPv4 prober (skip if permission denied)
	if icmpv4Cfg, exists := cfg.Prober[string(prober.ICMPV4)]; exists {
		icmpv4, err := prober.NewICMPProber(prober.ICMPV4, icmpv4Cfg.ICMP)
		if err != nil {
			fmt.Printf("Warning: failed to create ICMPv4 prober: %v\n", err)
		} else {
			probers = append(probers, icmpv4)
		}
	}
	
	// Create ICMPv6 prober (skip if permission denied)
	if icmpv6Cfg, exists := cfg.Prober[string(prober.ICMPV6)]; exists {
		icmpv6, err := prober.NewICMPProber(prober.ICMPV6, icmpv6Cfg.ICMP)
		if err != nil {
			fmt.Printf("Warning: failed to create ICMPv6 prober: %v\n", err)
		} else {
			probers = append(probers, icmpv6)
		}
	}
	
	// Create HTTP prober (handles both HTTP and HTTPS)
	if httpCfg, exists := cfg.Prober[string(prober.HTTP)]; exists {
		http := prober.NewHTTPProber(httpCfg.HTTP)
		probers = append(probers, http)
	}
	
	// Create TCP prober
	if tcpCfg, exists := cfg.Prober[string(prober.TCP)]; exists {
		tcp := prober.NewTCPProber(tcpCfg.TCP)
		probers = append(probers, tcp)
	}
	
	return probers, nil
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
