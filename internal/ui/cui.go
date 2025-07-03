package ui

import (
	"time"

	"github.com/servak/mping/internal/stats"
)

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