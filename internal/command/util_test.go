package command

import (
	"net"
	"os"
	"reflect"
	"testing"
)

func TestParseBrackets(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no brackets",
			input:    []string{"example.com", "google.com"},
			expected: []string{"example.com", "google.com"},
		},
		{
			name:     "simple numeric range",
			input:    []string{"server[1-3].example.com"},
			expected: []string{"server1.example.com", "server2.example.com", "server3.example.com"},
		},
		{
			name:     "zero-padded range",
			input:    []string{"web[01-03].example.com"},
			expected: []string{"web01.example.com", "web02.example.com", "web03.example.com"},
		},
		{
			name:     "three-digit padding",
			input:    []string{"host[001-003].example.com"},
			expected: []string{"host001.example.com", "host002.example.com", "host003.example.com"},
		},
		{
			name:     "character range",
			input:    []string{"server[a-c].example.com"},
			expected: []string{"servera.example.com", "serverb.example.com", "serverc.example.com"},
		},
		{
			name:     "uppercase character range",
			input:    []string{"server[A-C].example.com"},
			expected: []string{"serverA.example.com", "serverB.example.com", "serverC.example.com"},
		},
		{
			name:     "list expansion",
			input:    []string{"server[web,db,cache].example.com"},
			expected: []string{"serverweb.example.com", "serverdb.example.com", "servercache.example.com"},
		},
		{
			name:     "list with spaces",
			input:    []string{"server[web, db, cache].example.com"},
			expected: []string{"serverweb.example.com", "serverdb.example.com", "servercache.example.com"},
		},
		{
			name:     "single value",
			input:    []string{"server[5].example.com"},
			expected: []string{"server5.example.com"},
		},
		{
			name:  "multiple brackets - combination",
			input: []string{"server[1-2].[dev,prod].example.com"},
			expected: []string{
				"server1.dev.example.com", "server1.prod.example.com",
				"server2.dev.example.com", "server2.prod.example.com",
			},
		},
		{
			name:     "protocol with brackets",
			input:    []string{"https://api[1-2].example.com"},
			expected: []string{"https://api1.example.com", "https://api2.example.com"},
		},
		{
			name:     "invalid range - start > end",
			input:    []string{"server[5-3].example.com"},
			expected: []string{"server[5-3].example.com"},
		},
		{
			name:     "invalid range format",
			input:    []string{"server[1-2-3].example.com"},
			expected: []string{"server[1-2-3].example.com"},
		},
		{
			name:     "mixed valid and invalid",
			input:    []string{"server[1-3].example.com", "invalid[brackets", "normal.com"},
			expected: []string{"server1.example.com", "server2.example.com", "server3.example.com", "invalid[brackets", "normal.com"},
		},
		{
			name:     "range with hyphenated domain",
			input:    []string{"server[1-3].my-domain.com"},
			expected: []string{"server1.my-domain.com", "server2.my-domain.com", "server3.my-domain.com"},
		},
		{
			name:     "list with hyphens",
			input:    []string{"server[web-1,db-2,cache-3].example.com"},
			expected: []string{"serverweb-1.example.com", "serverdb-2.example.com", "servercache-3.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBrackets(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseBrackets() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestExpandRange(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple range",
			input:    "1-3",
			expected: []string{"1", "2", "3"},
		},
		{
			name:     "zero-padded range",
			input:    "01-03",
			expected: []string{"01", "02", "03"},
		},
		{
			name:     "three-digit range",
			input:    "001-003",
			expected: []string{"001", "002", "003"},
		},
		{
			name:     "character range",
			input:    "a-c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "uppercase character range",
			input:    "A-C",
			expected: []string{"A", "B", "C"},
		},
		{
			name:     "invalid numeric range",
			input:    "3-1",
			expected: []string{"3-1"},
		},
		{
			name:     "invalid character range",
			input:    "z-a",
			expected: []string{"z-a"},
		},
		{
			name:     "single number",
			input:    "5",
			expected: []string{"5"},
		},
		{
			name:     "invalid format",
			input:    "1-2-3",
			expected: []string{"1-2-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandRange(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expandRange() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestExpandList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple list",
			input:    "web,db,cache",
			expected: []string{"web", "db", "cache"},
		},
		{
			name:     "list with spaces",
			input:    "web, db, cache",
			expected: []string{"web", "db", "cache"},
		},
		{
			name:     "single item",
			input:    "web",
			expected: []string{"web"},
		},
		{
			name:     "empty items",
			input:    "web,,cache",
			expected: []string{"web", "", "cache"},
		},
		{
			name:     "hyphenated items",
			input:    "web-server,db-master,cache-redis",
			expected: []string{"web-server", "db-master", "cache-redis"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandList(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expandList() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestParseCidr(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no CIDR notation",
			input:    []string{"example.com", "8.8.8.8"},
			expected: []string{"example.com", "8.8.8.8"},
		},
		{
			name:     "single host /32",
			input:    []string{"192.168.1.1/32"},
			expected: []string{"192.168.1.1"},
		},
		{
			name:     "small subnet /30",
			input:    []string{"192.168.1.0/30"},
			expected: []string{"192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
		{
			name:     "IPv6 single host /128",
			input:    []string{"2001:db8::1/128"},
			expected: []string{"2001:db8::1"},
		},
		{
			name:     "mixed input with and without CIDR",
			input:    []string{"example.com", "192.168.1.0/31", "8.8.8.8"},
			expected: []string{"example.com", "192.168.1.0", "192.168.1.1", "8.8.8.8"},
		},
		{
			name:     "invalid CIDR notation",
			input:    []string{"192.168.1.0/33", "invalid/cidr"},
			expected: []string{"192.168.1.0/33", "invalid/cidr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCIDR(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseCidr() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIpInc(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "IPv4 increment",
			input:    "192.168.1.1",
			expected: "192.168.1.2",
		},
		{
			name:     "IPv4 overflow to next octet",
			input:    "192.168.1.255",
			expected: "192.168.2.0",
		},
		{
			name:     "IPv6 increment",
			input:    "2001:db8::1",
			expected: "2001:db8::2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.input)
			if ip == nil {
				t.Fatalf("Invalid IP address: %s", tt.input)
			}
			ipInc(ip)
			if ip.String() != tt.expected {
				t.Errorf("ipInc() = %v, expected %v", ip.String(), tt.expected)
			}
		})
	}
}

func TestFile2hostnames(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "simple hostnames",
			content:  "example.com\ngoogle.com\n8.8.8.8",
			expected: []string{"example.com", "google.com", "8.8.8.8"},
		},
		{
			name:     "with comments",
			content:  "# This is a comment\nexample.com  # inline comment\n; semicolon comment\ngoogle.com",
			expected: []string{"example.com", "google.com"},
		},
		{
			name:     "with empty lines and whitespace",
			content:  "\n  example.com  \n\n\tgoogle.com\t\n\n",
			expected: []string{"example.com", "google.com"},
		},
		{
			name:     "URLs with # in them",
			content:  "http://example.com#anchor\nhttps://test.com/path#hash",
			expected: []string{"http://example.com#anchor", "https://test.com/path#hash"},
		},
		{
			name:     "mixed comments and URLs",
			content:  "# Configuration file\nhttp://api.example.com  # API endpoint\n; Another comment\nhttps://web.example.com#main",
			expected: []string{"http://api.example.com", "https://web.example.com#main"},
		},
		{
			name:     "protocol prefixes",
			content:  "icmpv4://example.com\nhttps://api.example.com\ntcp://db.example.com:5432",
			expected: []string{"icmpv4://example.com", "https://api.example.com", "tcp://db.example.com:5432"},
		},
		{
			name:     "CIDR notation",
			content:  "192.168.1.0/24\n10.0.0.0/16  # Internal network",
			expected: []string{"192.168.1.0/24", "10.0.0.0/16"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file with test content
			tmpfile, err := os.CreateTemp("", "test_hostnames_*.txt")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpfile.Name())
			defer tmpfile.Close()

			if _, err := tmpfile.WriteString(tt.content); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}

			// Reset file pointer to beginning
			if _, err := tmpfile.Seek(0, 0); err != nil {
				t.Fatalf("Failed to seek temp file: %v", err)
			}

			result := file2hostnames(tmpfile)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("file2hostnames() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestProcessTargets(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		fileContent string
		expected    []string
	}{
		{
			name:     "args only",
			args:     []string{"example.com", "google.com"},
			expected: []string{"example.com", "google.com"},
		},
		{
			name:        "file only",
			args:        []string{},
			fileContent: "example.com\ngoogle.com",
			expected:    []string{"example.com", "google.com"},
		},
		{
			name:        "args and file combined",
			args:        []string{"cli-host.com"},
			fileContent: "file-host.com",
			expected:    []string{"file-host.com", "cli-host.com"},
		},
		{
			name:        "with bracket expansion",
			args:        []string{"server[1-2].example.com"},
			fileContent: "web[01-02].example.com",
			expected:    []string{"web01.example.com", "web02.example.com", "server1.example.com", "server2.example.com"},
		},
		{
			name:        "with CIDR expansion",
			args:        []string{"192.168.1.0/30"},
			fileContent: "10.0.0.0/31",
			expected:    []string{"10.0.0.0", "10.0.0.1", "192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
		{
			name:        "combined bracket and CIDR expansion",
			args:        []string{"server[1-2].example.com", "192.168.1.0/30"},
			fileContent: "web[a-b].example.com",
			expected:    []string{"weba.example.com", "webb.example.com", "server1.example.com", "server2.example.com", "192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fpath string
			if tt.fileContent != "" {
				// Create temporary file
				tmpfile, err := os.CreateTemp("", "test_parse_*.txt")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer os.Remove(tmpfile.Name())

				if _, err := tmpfile.WriteString(tt.fileContent); err != nil {
					t.Fatalf("Failed to write to temp file: %v", err)
				}
				tmpfile.Close()
				fpath = tmpfile.Name()
			}

			result := ExpandTargets(tt.args, fpath)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ProcessTargets() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestProcessTargetsIntegration(t *testing.T) {
	// Test the complete flow: file reading -> bracket expansion -> CIDR expansion
	fileContent := `# Test configuration
# Web servers with bracket expansion
web[01-02].prod.example.com

# Database subnet
10.0.1.0/30  # DB cluster IPs

# Mixed protocols
https://api[1-2].example.com
icmpv4://monitor.example.com
`

	tmpfile, err := os.CreateTemp("", "test_integration_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(fileContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpfile.Close()

	args := []string{"server[a-b].test.com", "192.168.1.0/31"}
	result := ExpandTargets(args, tmpfile.Name())

	expected := []string{
		// From file: bracket expansion
		"web01.prod.example.com", "web02.prod.example.com",
		// From file: CIDR expansion
		"10.0.1.0", "10.0.1.1", "10.0.1.2", "10.0.1.3",
		// From file: protocols with bracket expansion
		"https://api1.example.com", "https://api2.example.com",
		// From file: plain hostname
		"icmpv4://monitor.example.com",
		// From args: bracket expansion
		"servera.test.com", "serverb.test.com",
		// From args: CIDR expansion
		"192.168.1.0", "192.168.1.1",
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Integration test failed.\nGot: %v\nExpected: %v", result, expected)

		// Helper debug output
		t.Logf("Result length: %d, Expected length: %d", len(result), len(expected))
		for i, item := range result {
			if i < len(expected) {
				if item != expected[i] {
					t.Logf("  [%d] Got: %q, Expected: %q", i, item, expected[i])
				}
			} else {
				t.Logf("  [%d] Extra item: %q", i, item)
			}
		}
	}
}

func TestCollectTargets(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		fileContent string
		expected    []string
	}{
		{
			name:     "args only",
			args:     []string{"server1.com", "server2.com"},
			expected: []string{"server1.com", "server2.com"},
		},
		{
			name:        "file only",
			args:        []string{},
			fileContent: "file1.com\nfile2.com",
			expected:    []string{"file1.com", "file2.com"},
		},
		{
			name:        "file and args combined",
			args:        []string{"arg.com"},
			fileContent: "file.com",
			expected:    []string{"file.com", "arg.com"},
		},
		{
			name:        "file with comments",
			args:        []string{},
			fileContent: "# Comment\nserver.com  # inline\n; another comment\nvalid.com",
			expected:    []string{"server.com", "valid.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filePath string
			if tt.fileContent != "" {
				tmpfile, err := os.CreateTemp("", "test_collect_*.txt")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer os.Remove(tmpfile.Name())

				if _, err := tmpfile.WriteString(tt.fileContent); err != nil {
					t.Fatalf("Failed to write to temp file: %v", err)
				}
				tmpfile.Close()
				filePath = tmpfile.Name()
			}

			result := collectTargets(tt.args, filePath)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("CollectTargets() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
