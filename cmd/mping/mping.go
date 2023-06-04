package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/servak/mping/internal/command"
	"github.com/servak/mping/internal/config"
	"github.com/servak/mping/internal/prober"
	"github.com/servak/mping/internal/ui"
)

var (
	Version   string
	Revision  string
	GoVersion = runtime.Version()
)

func main() {
	var (
		filename string
		title    string
		interval int
		count    int
		size     int
		quiet    bool
		ver      bool
		ipv6     bool
	)
	flag.StringVar(&filename, "f", "", "use contents of file")
	flag.StringVar(&title, "t", "", "print title")
	flag.IntVar(&interval, "i", 1000, "interval(ms)")
	flag.IntVar(&size, "s", 56, "Specifies the number of data bytes to be sent.  The default is 56, which translates into 64 ICMP data bytes when combined with the 8 bytes of ICMP header data.")
	flag.IntVar(&count, "c", 0, "stop after receiving <count> response packets")
	flag.BoolVar(&quiet, "q", false, "quiet mode")
	flag.BoolVar(&ver, "v", false, "print version of mping")
	flag.BoolVar(&ipv6, "6", false, "use ip v6")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [options] [host ...]\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Example:\n  %s localhost 8.8.8.8\n  %s -f hostslist\n", os.Args[0], os.Args[0])
	}
	flag.Parse()

	if ver {
		fmt.Printf("mping, version: %s (revision: %s, goversion: %s)", Version, Revision, GoVersion)
		os.Exit(0)
	}

	_, err := os.Stat(filename)
	if err != nil && flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	hosts := []string{}
	if err == nil {
		fp, err := os.Open(filename)
		if err != nil {
			panic(err)
		}
		hosts = file2hostnames(fp)
	}

	hosts = append(hosts, flag.Args()...)

	if len(hosts) == 0 {
		fmt.Println("Host not found.")
		os.Exit(1)
	}

	if interval == 0 {
		flag.Usage()
		os.Exit(1)
	}

	hosts = parseCidr(hosts)
	cfg := &config.Config{
		Prober: &prober.ProberConfig{
			ICMP: &prober.ICMPConfig{
				Interval: "100ms",
				Timeout:  "1s",
			},
		},
		UI: &ui.UIConfig{
			CUI: &ui.CUIConfig{
				Border: true,
			},
		},
	}
	command.GocuiRun(hosts, cfg)
}

func file2hostnames(fp *os.File) []string {
	hosts := []string{}
	reader := bufio.NewReaderSize(fp, 4096)
	r := regexp.MustCompile(`[#;/].*`)

	for {
		lb, _, err := reader.ReadLine()
		if errors.Is(err, io.EOF) {
			break
		}

		line := r.ReplaceAllString(string(lb), "")
		line = strings.Trim(line, "\t \n")
		if line == "" {
			continue
		}
		hosts = append(hosts, line)
	}

	return hosts
}

func parseCidr(_hosts []string) []string {
	hosts := []string{}
	for _, h := range _hosts {
		ip, ipnet, err := net.ParseCIDR(h)
		if err != nil {
			hosts = append(hosts, h)
			continue
		}

		for i := ip.Mask(ipnet.Mask); ipnet.Contains(i); ipInc(i) {
			hosts = append(hosts, i.String())
		}
	}

	return hosts
}

func ipInc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
