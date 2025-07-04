package ui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
)

// UI は UI コンポーネントのインターフェース
type UI interface {
	Run() error
	Update()
	Close()
}

// Config は UI 設定を管理
type Config struct {
	Title        string `yaml:"-"`
	Border       bool   `yaml:"border"`
	EnableColors bool   `yaml:"enable_colors"`
	Colors       struct {
		Header      string `yaml:"header"`
		Footer      string `yaml:"footer"`
		Success     string `yaml:"success"`
		Warning     string `yaml:"warning"`
		Error       string `yaml:"error"`
		ModalBorder string `yaml:"modal_border"`
	} `yaml:"colors"`
}

// UIConfig は UI 全体の設定を管理
type UIConfig struct {
	CUI *Config `yaml:"cui"`
}

// App は tview アプリケーションのメインコントローラー
type App struct {
	app      *tview.Application
	pages    *tview.Pages
	layout   *Layout
	renderer *Renderer
	mm       *stats.MetricsManager
	config   *Config
	interval time.Duration
	sortKey  stats.Key
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewApp は新しい App インスタンスを作成
func NewApp(mm *stats.MetricsManager, cfg *Config, interval time.Duration) *App {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := tview.NewApplication()
	pages := tview.NewPages()

	renderer := NewRenderer(mm, cfg, interval)
	layout := NewLayout(renderer)

	// メインページとヘルプモーダルを追加
	pages.AddPage("main", layout.Root(), true, true)
	pages.AddPage("help", createHelpModal(), true, false)

	return &App{
		app:      app,
		pages:    pages,
		layout:   layout,
		renderer: renderer,
		mm:       mm,
		config:   cfg,
		interval: interval,
		sortKey:  stats.Success,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Run はアプリケーションを開始
func (a *App) Run() error {
	a.setupKeyBindings()
	a.renderer.SetSortKey(a.sortKey)
	a.app.SetRoot(a.pages, true).SetFocus(a.layout.Root())
	return a.app.Run()
}

// Update は表示内容を更新
func (a *App) Update() {
	a.app.QueueUpdateDraw(func() {
		a.layout.Update()
	})
}

// Close はアプリケーションを終了
func (a *App) Close() {
	a.cancel()
	a.app.Stop()
}

// setupKeyBindings はキーバインディングを設定
func (a *App) setupKeyBindings() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// ヘルプモーダルが表示されている場合
		if a.isHelpVisible() {
			switch event.Rune() {
			case 'h':
				a.hideHelp()
				return nil
			}
			switch event.Key() {
			case tcell.KeyEscape:
				a.hideHelp()
				return nil
			}
			return event
		}

		// メイン画面のキーバインディング
		switch event.Rune() {
		case 'q':
			a.Close()
			return nil
		case 'h':
			a.showHelp()
			return nil
		case 's':
			a.nextSort()
			return nil
		case 'S':
			a.prevSort()
			return nil
		case 'R':
			a.resetMetrics()
			return nil
		}

		// スクロール操作はレイアウトに委譲
		return a.layout.HandleKeyEvent(event)
	})
}

// ソート関連のメソッド
func (a *App) nextSort() {
	keys := stats.Keys()
	if int(a.sortKey+1) < len(keys) {
		a.sortKey++
	} else {
		a.sortKey = 0
	}
	a.renderer.SetSortKey(a.sortKey)
}

func (a *App) prevSort() {
	keys := stats.Keys()
	if int(a.sortKey) == 0 {
		a.sortKey = stats.Key(len(keys) - 1)
	} else {
		a.sortKey--
	}
	a.renderer.SetSortKey(a.sortKey)
}

func (a *App) resetMetrics() {
	a.mm.ResetAllMetrics()
}

// ヘルプモーダル関連のメソッド
func (a *App) showHelp() {
	a.pages.ShowPage("help")
	a.app.SetFocus(a.pages)
}

func (a *App) hideHelp() {
	a.pages.HidePage("help")
	a.app.SetFocus(a.layout.Root())
}

func (a *App) isHelpVisible() bool {
	frontPageName, _ := a.pages.GetFrontPage()
	return a.pages.HasPage("help") && frontPageName == "help"
}

// createHelpModal はヘルプモーダルを作成
func createHelpModal() *tview.Modal {
	helpText := `mping - Multi-target Ping Tool      

NAVIGATION:                          
  j, ↓         Move down              
  k, ↑         Move up                
  g            Go to top              
  G            Go to bottom           
  u, Page Up   Page up                
  d, Page Down Page down              
  s            Next sort key          
  S            Previous sort key      
  R            Reset all metrics      
  h            Show/hide this help    
  q, Ctrl+C    Quit application       

Press 'h' or Esc to close           `

	return tview.NewModal().
		SetText(helpText).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// ボタンが押されたときの処理は親で処理
		})
}

// GetCUIConfig は CUI 設定を返す（デフォルト値付き）
func (uc *UIConfig) GetCUIConfig() *Config {
	// デフォルト値をマージ
	cfg := DefaultConfig()
	if uc == nil || uc.CUI == nil {
		return cfg
	}
	if uc.CUI.Title != "" {
		cfg.Title = uc.CUI.Title
	}
	cfg.Border = uc.CUI.Border
	cfg.EnableColors = uc.CUI.EnableColors

	// カラー設定をマージ
	if uc.CUI.Colors.Header != "" {
		cfg.Colors.Header = uc.CUI.Colors.Header
	}
	if uc.CUI.Colors.Footer != "" {
		cfg.Colors.Footer = uc.CUI.Colors.Footer
	}
	if uc.CUI.Colors.Success != "" {
		cfg.Colors.Success = uc.CUI.Colors.Success
	}
	if uc.CUI.Colors.Warning != "" {
		cfg.Colors.Warning = uc.CUI.Colors.Warning
	}
	if uc.CUI.Colors.Error != "" {
		cfg.Colors.Error = uc.CUI.Colors.Error
	}
	if uc.CUI.Colors.ModalBorder != "" {
		cfg.Colors.ModalBorder = uc.CUI.Colors.ModalBorder
	}

	return cfg
}

// DefaultConfig はデフォルトの設定を返す
func DefaultConfig() *Config {
	cfg := &Config{
		Title:        "mping",
		Border:       true,
		EnableColors: true, // デフォルトで色を有効化
	}

	// tviewで使える色名を使用
	cfg.Colors.Header = "dodgerblue"
	cfg.Colors.Footer = "gray"
	cfg.Colors.Success = "green"
	cfg.Colors.Warning = "yellow"
	cfg.Colors.Error = "red"
	cfg.Colors.ModalBorder = "white"

	return cfg
}

// 互換性のための型と関数

// CUI は旧来の CUI インターフェースとの互換性のためのラッパー
type CUI struct {
	app *App
}

// CUIConfig は旧来の設定との互換性のための型
type CUIConfig = Config

// NewCUI は新しい CUI インスタンスを作成（互換性のため）
func NewCUI(mm *stats.MetricsManager, cfg *CUIConfig, interval time.Duration) (*CUI, error) {
	app := NewApp(mm, cfg, interval)
	return &CUI{app: app}, nil
}

// Run はアプリケーションを実行
func (c *CUI) Run() error {
	return c.app.Run()
}

// Update は表示を更新
func (c *CUI) Update() {
	c.app.Update()
}

// Close はアプリケーションを終了
func (c *CUI) Close() {
	c.app.Close()
}
