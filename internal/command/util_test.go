package command

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/servak/mping/internal/target"
)

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

			result, err := ExpandTargets(tt.args, fpath)
			if err != nil {
				t.Fatal(err)
			}
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
	result, err := ExpandTargets(args, tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

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

			result, err := collectTargets(tt.args, filePath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("CollectTargets() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestFile2HostnamesDirectoryDoesNotHang(t *testing.T) {
	_, err := ExpandTargets(nil, t.TempDir())
	if err == nil {
		t.Fatal("expected error when the target file is a directory")
	}
}

func TestFile2HostnamesLongLine(t *testing.T) {
	long := "http://example.com/" + strings.Repeat("a", 10000)
	path := filepath.Join(t.TempDir(), "hosts.txt")
	if err := os.WriteFile(path, []byte(long+"\nnext.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ExpandTargets(nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != long || got[1] != "next.example.com" {
		t.Errorf("long line was split: got %d targets", len(got))
	}
}

func TestExpandTargetsMissingFile(t *testing.T) {
	if _, err := ExpandTargets([]string{"1.1.1.1"}, "/nonexistent/hosts.txt"); err == nil {
		t.Fatal("expected error for missing target file")
	}
}

func TestExpandTargetsSizeCap(t *testing.T) {
	for _, in := range []string{"0.0.0.0/0", "10.0.0.0/8", "::/0", "host[1-100000000]", "a[1-300]b[1-300]"} {
		if _, err := ExpandTargets([]string{in}, ""); err == nil || !strings.Contains(err.Error(), "too many targets") {
			t.Errorf("%s: expected too many targets error, got %v", in, err)
		}
	}
	// The largest allowed expansion still works.
	got, err := ExpandTargets([]string{"10.0.0.0/16"}, "")
	if err != nil || len(got) != 65536 {
		t.Errorf("/16: got %d targets, err %v", len(got), err)
	}
	// The last addresses of the IPv4 space must not wrap around.
	got, err = ExpandTargets([]string{"255.255.255.252/30"}, "")
	if err != nil || len(got) != 4 || got[3] != "255.255.255.255" {
		t.Errorf("top of range: got %v, err %v", got, err)
	}
}

func TestExpandTargetsEnforcesOverallCap(t *testing.T) {
	// Plain hosts first, then a bracket range: the total exceeds the cap even
	// though no single expansion does.
	plain := make([]string, target.MaxTargets)
	for i := range plain {
		plain[i] = "h" + strconv.Itoa(i)
	}
	if _, err := ExpandTargets(append(plain, "x[1-5]"), ""); err == nil {
		t.Error("plain hosts + bracket range over the cap should fail")
	}
	// Only plain hosts over the cap.
	if _, err := ExpandTargets(append(plain, "extra"), ""); err == nil {
		t.Error("plain hosts over the cap should fail")
	}
	if got, err := ExpandTargets(plain, ""); err != nil || len(got) != target.MaxTargets {
		t.Errorf("exactly the cap should pass, got %d, %v", len(got), err)
	}
}

func TestCIDRCapErrorReportsRealSize(t *testing.T) {
	_, err := ExpandTargets([]string{"10.0.0.0/8"}, "")
	if err == nil || !strings.Contains(err.Error(), "2^24") {
		t.Errorf("error = %v, want mention of 2^24", err)
	}
	_, err = ExpandTargets([]string{"::/0"}, "")
	if err == nil || !strings.Contains(err.Error(), "2^128") {
		t.Errorf("error = %v, want mention of 2^128", err)
	}
}
