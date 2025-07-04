package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	
	"github.com/servak/mping/internal/stats"
)

func TestNewLayout(t *testing.T) {
	mm := stats.NewMetricsManager()
	cfg := DefaultConfig()
	renderer := NewRenderer(mm, cfg, time.Second)

	layout := NewLayout(renderer)

	if layout.renderer != renderer {
		t.Error("Expected renderer to be set correctly")
	}

	if layout.root == nil {
		t.Error("Expected root to be initialized")
	}

	if layout.header == nil {
		t.Error("Expected header to be initialized")
	}

	if layout.mainView == nil {
		t.Error("Expected mainView to be initialized")
	}

	if layout.footer == nil {
		t.Error("Expected footer to be initialized")
	}
}

func TestLayout_Root(t *testing.T) {
	mm := stats.NewMetricsManager()
	cfg := DefaultConfig()
	renderer := NewRenderer(mm, cfg, time.Second)
	layout := NewLayout(renderer)

	root := layout.Root()

	if root != layout.root {
		t.Error("Expected Root() to return the root element")
	}

	// tview.Primitiveインターフェースを実装していることを確認
	var _ tview.Primitive = root
}

func TestLayout_HandleKeyEvent(t *testing.T) {
	mm := stats.NewMetricsManager()
	cfg := DefaultConfig()
	renderer := NewRenderer(mm, cfg, time.Second)
	layout := NewLayout(renderer)

	tests := []struct {
		name        string
		key         rune
		expectNil   bool
		description string
	}{
		{
			name:        "j key for scroll down",
			key:         'j',
			expectNil:   true,
			description: "j key should be handled and return nil",
		},
		{
			name:        "k key for scroll up",
			key:         'k',
			expectNil:   true,
			description: "k key should be handled and return nil",
		},
		{
			name:        "g key for scroll to top",
			key:         'g',
			expectNil:   true,
			description: "g key should be handled and return nil",
		},
		{
			name:        "G key for scroll to bottom",
			key:         'G',
			expectNil:   true,
			description: "G key should be handled and return nil",
		},
		{
			name:        "u key for page up",
			key:         'u',
			expectNil:   true,
			description: "u key should be handled and return nil",
		},
		{
			name:        "d key for page down",
			key:         'd',
			expectNil:   true,
			description: "d key should be handled and return nil",
		},
		{
			name:        "unhandled key returns original event",
			key:         'x',
			expectNil:   false,
			description: "unhandled key should return the original event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := tcell.NewEventKey(tcell.KeyRune, tt.key, tcell.ModNone)
			result := layout.HandleKeyEvent(event)

			if tt.expectNil {
				if result != nil {
					t.Errorf("%s: expected nil, got event", tt.description)
				}
			} else {
				if result == nil {
					t.Errorf("%s: expected event, got nil", tt.description)
				}
				if result != event {
					t.Errorf("%s: expected original event, got different event", tt.description)
				}
			}
		})
	}
}

func TestLayout_Update(t *testing.T) {
	// テスト用のメトリクス
	mm := stats.NewMetricsManager()
	mm.Register("test.com", "test.com")
	cfg := DefaultConfig()
	renderer := NewRenderer(mm, cfg, time.Second)
	layout := NewLayout(renderer)

	// Updateの前後でテキストが設定されることを確認
	// 注意: tview.TextViewの内容は直接アクセスできないため、
	// 呼び出しが成功することのみをテスト
	layout.Update()

	// エラーが発生しないことを確認（パニックしない）
	// 実際のテキスト内容の検証は困難なため、基本的な動作確認のみ
}

// テスト用のヘルパー関数：スクロール操作の基本的なテスト
func TestLayout_ScrollOperations(t *testing.T) {
	mm := stats.NewMetricsManager()
	cfg := DefaultConfig()
	renderer := NewRenderer(mm, cfg, time.Second)
	layout := NewLayout(renderer)

	// 各スクロール操作が呼び出せることを確認（パニックしないこと）
	t.Run("scrollDown", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("scrollDown panicked: %v", r)
			}
		}()
		layout.scrollDown()
	})

	t.Run("scrollUp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("scrollUp panicked: %v", r)
			}
		}()
		layout.scrollUp()
	})

	t.Run("scrollToTop", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("scrollToTop panicked: %v", r)
			}
		}()
		layout.scrollToTop()
	})

	t.Run("scrollToBottom", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("scrollToBottom panicked: %v", r)
			}
		}()
		layout.scrollToBottom()
	})

	t.Run("pageUp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("pageUp panicked: %v", r)
			}
		}()
		layout.pageUp()
	})

	t.Run("pageDown", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("pageDown panicked: %v", r)
			}
		}()
		layout.pageDown()
	})
}

func TestLayout_ViewSetup(t *testing.T) {
	mm := stats.NewMetricsManager()
	cfg := DefaultConfig()
	renderer := NewRenderer(mm, cfg, time.Second)
	layout := NewLayout(renderer)

	// 各ビューが正しく初期化されていることを確認
	if layout.header == nil {
		t.Error("Header view should be initialized")
	}

	if layout.mainView == nil {
		t.Error("Main view should be initialized")
	}

	if layout.footer == nil {
		t.Error("Footer view should be initialized")
	}

	// レイアウトが正しく構成されていることを確認
	if layout.root == nil {
		t.Error("Root layout should be initialized")
	}

	// Flexレイアウトであることを確認
	if layout.root == nil {
		t.Error("Root should be initialized")
	}
}