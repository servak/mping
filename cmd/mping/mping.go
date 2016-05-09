package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/servak/mping"
)

func main() {
	var filename string
	flag.StringVar(&filename, "file", "", "use contents of file")
	flag.StringVar(&filename, "f", "", "use contents of file (shorthand)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [options] [host ...]\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Example:\n  %s localhost 8.8.8.8\n  %s -f hostslist\n", os.Args[0], os.Args[0])
	}
	flag.Parse()

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
		msg += "        1. sudo chown root %s\n"
		msg += "(Mac)   2. sudo chmod u+t %s\n"
		msg += "(Linux) 2. sudo chmod u+s %s\n"
		fmt.Printf(msg, p, p, p)
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

	mping.Run(hosts)
}

func file2hostnames(fp *os.File) []string {
	hosts := []string{}
	reader := bufio.NewReaderSize(fp, 4096)
	for {
		lb, _, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		line := string(lb)
		if line == "" {
			continue
		}
		hosts = append(hosts, strings.Trim(line, " \n"))
	}

	return hosts
}
