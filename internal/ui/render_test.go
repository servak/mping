package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/servak/mping/internal/stats"
)


func TestNewRenderer(t *testing.T) {
	// stats.MetricsManager を作成してテスト
	mm := stats.NewMetricsManager()
	cfg := DefaultConfig()
	interval := 1 * time.Second

	renderer := NewRenderer(mm, cfg, interval)

	if renderer.mm != mm {
		t.Error("Expected mm to be set correctly")
	}

	if renderer.config != cfg {
		t.Error("Expected config to be set correctly")
	}

	if renderer.interval != interval {
		t.Error("Expected interval to be set correctly")
	}

	if renderer.sortKey != stats.Success {
		t.Error("Expected sortKey to default to stats.Success")
	}
}

func TestRenderer_SetSortKey(t *testing.T) {
	renderer := NewRenderer(stats.NewMetricsManager(), DefaultConfig(), time.Second)

	renderer.SetSortKey(stats.Host)

	if renderer.sortKey != stats.Host {
		t.Errorf("Expected sortKey to be %v, got %v", stats.Host, renderer.sortKey)
	}
}

func TestRenderer_RenderHeader(t *testing.T) {
	tests := []struct {
		name           string
		enableColors   bool
		headerColor    string
		expectedParts  []string
		unexpectedParts []string
	}{
		{
			name:         "with colors enabled",
			enableColors: true,
			headerColor:  "blue",
			expectedParts: []string{
				"[blue]Sort: Succ ^[-]",
				"[blue]Interval: 1000ms[-]",
				"[blue]mping[-]",
			},
		},
		{
			name:         "with colors disabled",
			enableColors: false,
			expectedParts: []string{
				"Sort: Succ ^",
				"Interval: 1000ms",
				"mping",
			},
			unexpectedParts: []string{
				"[blue]",
				"[-]",
			},
		},
		{
			name:         "with empty header color",
			enableColors: true,
			headerColor:  "",
			expectedParts: []string{
				"Sort: Succ ^",
				"Interval: 1000ms",
				"mping",
			},
			unexpectedParts: []string{
				"[",
				"]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.EnableColors = tt.enableColors
			cfg.Colors.Header = tt.headerColor

			renderer := NewRenderer(stats.NewMetricsManager(), cfg, time.Second)
			result := renderer.RenderHeader()

			for _, part := range tt.expectedParts {
				if !strings.Contains(result, part) {
					t.Errorf("Expected header to contain '%s', got: %s", part, result)
				}
			}

			for _, part := range tt.unexpectedParts {
				if strings.Contains(result, part) {
					t.Errorf("Expected header NOT to contain '%s', got: %s", part, result)
				}
			}
		})
	}
}

func TestRenderer_RenderFooter(t *testing.T) {
	tests := []struct {
		name           string
		enableColors   bool
		footerColor    string
		expectedParts  []string
		unexpectedParts []string
	}{
		{
			name:         "with colors enabled",
			enableColors: true,
			footerColor:  "gray",
			expectedParts: []string{
				"[gray]h:help[-]",
				"[gray]q:quit[-]",
				"[gray]s:sort[-]",
				"[gray]r:reverse[-]",
				"[gray]R:reset[-]",
				"[gray]j/k/g/G/u/d:move[-]",
			},
		},
		{
			name:         "with colors disabled",
			enableColors: false,
			expectedParts: []string{
				"h:help",
				"q:quit",
				"s:sort",
				"r:reverse",
				"R:reset",
				"j/k/g/G/u/d:move",
			},
			unexpectedParts: []string{
				"[gray]",
				"[-]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.EnableColors = tt.enableColors
			cfg.Colors.Footer = tt.footerColor

			renderer := NewRenderer(stats.NewMetricsManager(), cfg, time.Second)
			result := renderer.RenderFooter()

			for _, part := range tt.expectedParts {
				if !strings.Contains(result, part) {
					t.Errorf("Expected footer to contain '%s', got: %s", part, result)
				}
			}

			for _, part := range tt.unexpectedParts {
				if strings.Contains(result, part) {
					t.Errorf("Expected footer NOT to contain '%s', got: %s", part, result)
				}
			}
		})
	}
}

func TestRenderer_RenderMain(t *testing.T) {
	tests := []struct {
		name     string
		border   bool
		metrics  []stats.Metrics
		expected []string
	}{
		{
			name:   "with border enabled",
			border: true,
			metrics: []stats.Metrics{
				{
					Name:           "example.com",
					Total:          10,
					Successful:     8,
					Failed:         2,
					Loss:           20.0,
					LastRTT:        50 * time.Millisecond,
					AverageRTT:     45 * time.Millisecond,
					MinimumRTT:     30 * time.Millisecond,
					MaximumRTT:     60 * time.Millisecond,
					LastSuccTime:   time.Now(),
					LastFailTime:   time.Now(),
					LastFailDetail: "timeout",
				},
			},
			expected: []string{
				"example.com",
				"HOST", // テーブルヘッダー
				"SENT",
				"SUCC ↑", // ソート矢印付き
				"FAIL",
			},
		},
		{
			name:   "with border disabled",
			border: false,
			metrics: []stats.Metrics{
				{
					Name:       "test.com",
					Total:      5,
					Successful: 5,
					Failed:     0,
					Loss:       0.0,
				},
			},
			expected: []string{
				"test.com",
				"Host",
				"Sent", 
				"Succ ↑", // ソート矢印付き
				"Fail",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Border = tt.border

			mm := stats.NewMetricsManager()
			// テスト用のメトリクスを登録
			for _, metric := range tt.metrics {
				mm.Register(metric.Name, metric.Name)
			}
			renderer := NewRenderer(mm, cfg, time.Second)
			result := renderer.RenderMain()

			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected main content to contain '%s', got: %s", expected, result)
				}
			}

			// テーブルヘッダーの確認（ソート矢印付きヘッダー）
			// Borderによってヘッダーの大文字小文字が変わる
			var expectedHeaders []string
			if tt.border {
				expectedHeaders = []string{"HOST", "SENT", "SUCC ↑", "FAIL", "LOSS"}
			} else {
				expectedHeaders = []string{"Host", "Sent", "Succ ↑", "Fail", "Loss"}
			}
			for _, header := range expectedHeaders {
				if !strings.Contains(result, header) {
					t.Errorf("Expected main content to contain header '%s', got: %s", header, result)
				}
			}
		})
	}
}

func TestTableRender(t *testing.T) {
	metrics := []stats.Metrics{
		{
			Name:           "example.com",
			Total:          100,
			Successful:     95,
			Failed:         5,
			Loss:           5.0,
			LastRTT:        25 * time.Millisecond,
			AverageRTT:     30 * time.Millisecond,
			MinimumRTT:     20 * time.Millisecond,
			MaximumRTT:     40 * time.Millisecond,
			LastSuccTime:   time.Now(),
			LastFailTime:   time.Now(),
			LastFailDetail: "timeout",
		},
		{
			Name:           "google.com",
			Total:          50,
			Successful:     50,
			Failed:         0,
			Loss:           0.0,
			LastRTT:        15 * time.Millisecond,
			AverageRTT:     18 * time.Millisecond,
			MinimumRTT:     10 * time.Millisecond,
			MaximumRTT:     25 * time.Millisecond,
			LastSuccTime:   time.Now(),
			LastFailTime:   time.Time{},
			LastFailDetail: "",
		},
	}

	mm := stats.NewMetricsManager()
	// テスト用のメトリクスを登録
	for _, metric := range metrics {
		mm.Register(metric.Name, metric.Name)
	}
	table := TableRender(mm, stats.Success)

	result := table.Render()

	// テーブルの基本的な内容を確認
	expectedContents := []string{
		"example.com",
		"google.com",
		"HOST",
		"SENT",
		"SUCC",
		"FAIL",
	}

	for _, content := range expectedContents {
		if !strings.Contains(result, content) {
			t.Errorf("Expected table to contain '%s', got: %s", content, result)
		}
	}

	// ヘッダーの確認（実際のヘッダー名に合わせる）
	expectedHeaders := []string{"HOST", "SENT", "SUCC", "FAIL"}
	for _, header := range expectedHeaders {
		if !strings.Contains(result, header) {
			t.Errorf("Expected table to contain header '%s', got: %s", header, result)
		}
	}
}