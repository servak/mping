package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/servak/mping"
)

const version = "v0.4"

func main() {
	var filename string
	var title string
	var interval int
	var ver bool
	var ipv6 bool
	flag.StringVar(&filename, "f", "", "use contents of file")
	flag.StringVar(&title, "t", "", "print title")
	flag.IntVar(&interval, "i", 1000, "interval(ms)")
	flag.BoolVar(&ver, "v", false, "print version of mping")
	flag.BoolVar(&ipv6, "6", false, "use ip v6")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [options] [host ...]\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Example:\n  %s localhost 8.8.8.8\n  %s -f hostslist\n", os.Args[0], os.Args[0])
	}
	flag.Parse()

	if ver {
		fmt.Printf("%s: %s\n", os.Args[0], version)
		os.Exit(0)
	}

	_, err := os.Stat(filename)
	if err != nil && flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	uid := syscall.Geteuid()
	if uid != 0 {
		p := os.Args[0]
		msg := "mping need to 'root' privileges.\n"
		msg += "try to issue the following command:\n"
		msg += "1. sudo chown root %s\n"
		msg += "2. sudo chmod u+s %s\n"
		fmt.Printf(msg, p, p)
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

	for _, h := range flag.Args() {
		hosts = append(hosts, h)
	}

	if len(hosts) == 0 {
		fmt.Println("Host not found.")
		os.Exit(1)
	}

	if interval == 0 {
		flag.Usage()
		os.Exit(1)
	}

	mping.Run(hosts, interval, title, ipv6)
}

func file2hostnames(fp *os.File) []string {
	hosts := []string{}
	reader := bufio.NewReaderSize(fp, 4096)
	r := regexp.MustCompile(`[#;/].*`)

	for {
		lb, _, err := reader.ReadLine()
		if err == io.EOF {
			break
		}

		line := r.ReplaceAllString(string(lb), "")
		line = strings.Trim(line, " \n")
		if line == "" {
			continue
		}
		hosts = append(hosts, line)
	}

	return hosts
}
