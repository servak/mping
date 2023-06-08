package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/servak/mping/internal/command"
	"github.com/servak/mping/internal/config"
)

var (
	Version   string
	Revision  string
	GoVersion = runtime.Version()
)

func main() {
	var (
		help     bool
		filename string
		title    string
		path     string
		interval int
		timeout  int
		version  bool
	)
	pflag.BoolVarP(&help, "help", "h", false, "Display help and exit")
	pflag.StringVarP(&filename, "fiilename", "f", "", "use contents of file")
	pflag.StringVarP(&title, "title", "n", "", "print title")
	pflag.StringVarP(&path, "config", "c", "~/.mping.yml", "config path")
	pflag.IntVarP(&interval, "interval", "i", 1000, "interval(ms)")
	pflag.IntVarP(&timeout, "timeout", "t", 1000, "timeout(ms)")
	pflag.BoolVarP(&version, "version", "v", false, "print version")
	pflag.Parse()

	if help {
		usage(os.Args[0])
		return
	}

	if version {
		fmt.Printf("mping, version: %s (revision: %s, goversion: %s)\n", Version, Revision, GoVersion)
		os.Exit(0)
	}

	if interval == 0 || timeout == 0 {
		usage(os.Args[0])
		return
	}

	_, err := os.Stat(filename)
	if err != nil && pflag.NArg() == 0 {
		usage(os.Args[0])
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

	hosts = append(hosts, pflag.Args()...)

	if len(hosts) == 0 {
		fmt.Println("Host not found.")
		os.Exit(1)
	}

	hosts = parseCidr(hosts)
	cfgPath, _ := filepath.Abs(path)
	cfg, _ := config.LoadFile(cfgPath)
	cfg.UI.CUI.Title = title
	_interval := time.Duration(interval) * time.Millisecond
	_timeout := time.Duration(timeout) * time.Millisecond
	command.Run(hosts, cfg, _interval, _timeout)
}

func usage(progname string) {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] [TARGET...]\n", progname)
	fmt.Fprintln(os.Stderr, "Options:")
	pflag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "Examples:\n  %s localhost google.com 8.8.8.8 192.168.1.0/24\n  %s google.com icmpv6:google.com\n  %s -f hostslist\n", progname, progname, progname)
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
