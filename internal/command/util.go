package command

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
)

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

func file2hostnames(fp *os.File) []string {
	hosts := []string{}
	reader := bufio.NewReaderSize(fp, 4096)
	// Fixed regex: only match # or ; at start of line or after whitespace
	// This prevents breaking URLs like http://example.com
	r := regexp.MustCompile(`(?:^|\s)[#;].*`)

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

func parseHostnames(args []string, fpath string) []string {
	hosts := []string{}
	
	// Only attempt to open file if path is not empty
	if fpath != "" {
		fp, err := os.Open(fpath)
		if err == nil {
			hosts = file2hostnames(fp)
			fp.Close() // Critical fix: close file to prevent resource leak
		}
	}

	hosts = append(hosts, args...)
	return parseCidr(hosts)
}
