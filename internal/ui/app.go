package ui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
)

// App は tview アプリケーションのメインコントローラー
type App struct {
	app        *tview.Application
	pages      *tview.Pages
	mainLayout *MainLayout
	helpModal  *HelpModal
	mm         *stats.MetricsManager
	config     *Config
	interval   time.Duration
	sortKey    stats.Key
	ctx        context.Context
	cancel     context.CancelFunc
}

// Config は UI 設定を管理
type Config struct {
	Title      string `yaml:"-"`
	Border     bool   `yaml:"border"`
	EnableColors bool `yaml:"enable_colors"`
	Colors struct {
		Header      string `yaml:"header"`
		Footer      string `yaml:"footer"`
		Success     string `yaml:"success"`
		Warning     string `yaml:"warning"`
		Error       string `yaml:"error"`
		ModalBorder string `yaml:"modal_border"`
	} `yaml:"colors"`
}

// NewApp は新しい App インスタンスを作成
func NewApp(mm *stats.MetricsManager, cfg *Config, interval time.Duration) *App {
	if cfg == nil {
		cfg = defaultConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	app := tview.NewApplication()
	pages := tview.NewPages()
	
	mainLayout := NewMainLayout(mm, cfg, interval)
	helpModal := NewHelpModal()
	
	// メインページとヘルプモーダルを追加
	pages.AddPage("main", mainLayout.Root(), true, true)
	pages.AddPage("help", helpModal.Modal(), true, false)
	
	return &App{
		app:        app,
		pages:      pages,
		mainLayout: mainLayout,
		helpModal:  helpModal,
		mm:         mm,
		config:     cfg,
		interval:   interval,
		sortKey:    stats.Success,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Run はアプリケーションを開始
func (a *App) Run() error {
	// キーバインディングを設定
	a.setupKeyBindings()
	
	// ソートキーを初期化
	a.mainLayout.SetSortKey(a.sortKey)
	
	// アプリケーションのルートを設定
	a.app.SetRoot(a.pages, true).SetFocus(a.mainLayout.Root())
	
	return a.app.Run()
}

// Update は表示内容を更新
func (a *App) Update() {
	a.app.QueueUpdateDraw(func() {
		a.mainLayout.Update()
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
		
		// スクロール操作はメインレイアウトに委譲
		return a.mainLayout.HandleKeyEvent(event)
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
	a.mainLayout.SetSortKey(a.sortKey)
}

func (a *App) prevSort() {
	keys := stats.Keys()
	if int(a.sortKey) == 0 {
		a.sortKey = stats.Key(len(keys) - 1)
	} else {
		a.sortKey--
	}
	a.mainLayout.SetSortKey(a.sortKey)
}

func (a *App) resetMetrics() {
	a.mm.ResetAllMetrics()
}

// ヘルプモーダル関連のメソッド
func (a *App) showHelp() {
	a.pages.ShowPage("help")
	a.app.SetFocus(a.helpModal.Modal())
}

func (a *App) hideHelp() {
	a.pages.HidePage("help")
	a.app.SetFocus(a.mainLayout.Root())
}

func (a *App) isHelpVisible() bool {
	frontPageName, _ := a.pages.GetFrontPage()
	return a.pages.HasPage("help") && frontPageName == "help"
}

// defaultConfig はデフォルトの設定を返す
func defaultConfig() *Config {
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