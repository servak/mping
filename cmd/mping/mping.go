package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/servak/mping"
)

func isPipeExists() bool {
	s, _ := os.Stdin.Stat()
	if s.Size() > 0 {
		return true
	}

	return false
}

func main() {
	var filename string
	flag.StringVar(&filename, "file", "", "use contents of file")
	flag.StringVar(&filename, "f", "", "use contents of file (shorthand)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [options]\n  cat filename | %s \n\nOptions:\n", os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	_, err := os.Stat(filename)
	if err != nil && !isPipeExists() {
		flag.Usage()
		os.Exit(1)
	}

	var fp *os.File
	if isPipeExists() {
		fp = os.Stdin
	} else {
		fp, err = os.Open(filename)
		if err != nil {
			panic(err)
		}
	}

	hostnames := []string{}
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
		hostnames = append(hostnames, line)
	}

	mping.Run(hostnames)
}
