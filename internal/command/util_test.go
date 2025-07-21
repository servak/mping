package command

import (
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
