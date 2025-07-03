package ui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
)

// MainLayout はメイン画面のレイアウトを管理
type MainLayout struct {
	root     *tview.Flex
	header   *tview.TextView
	mainView *tview.TextView
	footer   *tview.TextView
	mm       *stats.MetricsManager
	config   *Config
	interval time.Duration
	sortKey  stats.Key
}

// NewMainLayout は新しい MainLayout を作成
func NewMainLayout(mm *stats.MetricsManager, cfg *Config, interval time.Duration) *MainLayout {
	layout := &MainLayout{
		mm:       mm,
		config:   cfg,
		interval: interval,
		sortKey:  stats.Success,
	}
	
	layout.setupViews()
	layout.setupLayout()
	layout.updateContent()
	
	return layout
}

// setupViews は各ビューを初期化
func (ml *MainLayout) setupViews() {
	// ヘッダー
	ml.header = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(ml.getHeaderText())
	
	// メインビュー（テーブル表示エリア）
	ml.mainView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText(ml.getMainText())
	
	// フッター
	ml.footer = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(ml.getFooterText())
}

// setupLayout はレイアウトを構成
func (ml *MainLayout) setupLayout() {
	ml.root = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(ml.header, 1, 0, false).
		AddItem(ml.mainView, 0, 1, true).
		AddItem(ml.footer, 1, 0, false)
}

// Root はレイアウトのルート要素を返す
func (ml *MainLayout) Root() tview.Primitive {
	return ml.root
}

// Update は表示内容を更新
func (ml *MainLayout) Update() {
	ml.updateContent()
}

// SetSortKey はソートキーを設定
func (ml *MainLayout) SetSortKey(key stats.Key) {
	ml.sortKey = key
	ml.updateContent()
}

// HandleKeyEvent はキーイベントを処理
func (ml *MainLayout) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'j':
		ml.scrollDown()
		return nil
	case 'k':
		ml.scrollUp()
		return nil
	case 'g':
		ml.scrollToTop()
		return nil
	case 'G':
		ml.scrollToBottom()
		return nil
	case 'u':
		ml.pageUp()
		return nil
	case 'd':
		ml.pageDown()
		return nil
	}
	return event
}

// スクロール操作メソッド
func (ml *MainLayout) scrollDown() {
	row, col := ml.mainView.GetScrollOffset()
	ml.mainView.ScrollTo(row+1, col)
}

func (ml *MainLayout) scrollUp() {
	row, col := ml.mainView.GetScrollOffset()
	if row > 0 {
		ml.mainView.ScrollTo(row-1, col)
	}
}

func (ml *MainLayout) scrollToTop() {
	ml.mainView.ScrollToBeginning()
}

func (ml *MainLayout) scrollToBottom() {
	ml.mainView.ScrollToEnd()
}

func (ml *MainLayout) pageDown() {
	_, _, _, height := ml.mainView.GetRect()
	row, col := ml.mainView.GetScrollOffset()
	ml.mainView.ScrollTo(row+height, col)
}

func (ml *MainLayout) pageUp() {
	_, _, _, height := ml.mainView.GetRect()
	row, col := ml.mainView.GetScrollOffset()
	if row >= height {
		ml.mainView.ScrollTo(row-height, col)
	} else {
		ml.mainView.ScrollToBeginning()
	}
}

// コンテンツ更新メソッド
func (ml *MainLayout) updateContent() {
	ml.header.SetText(ml.getHeaderText())
	ml.mainView.SetText(ml.getMainText())
	ml.footer.SetText(ml.getFooterText())
}

func (ml *MainLayout) getHeaderText() string {
	if ml.config.EnableColors && ml.config.Colors.Header != "" {
		sortText := fmt.Sprintf("[%s]Sort: %s[-]", ml.config.Colors.Header, ml.sortKey)
		intervalText := fmt.Sprintf("[%s]Interval: %dms[-]", ml.config.Colors.Header, ml.interval.Milliseconds())
		titleText := fmt.Sprintf("[%s]%s[-]", ml.config.Colors.Header, ml.config.Title)
		return fmt.Sprintf("%s    %s    %s", sortText, intervalText, titleText)
	} else {
		return fmt.Sprintf("Sort: %s    Interval: %dms    %s", ml.sortKey, ml.interval.Milliseconds(), ml.config.Title)
	}
}

func (ml *MainLayout) getMainText() string {
	return ml.renderTable()
}

func (ml *MainLayout) getFooterText() string {
	if ml.config.EnableColors && ml.config.Colors.Footer != "" {
		helpText := fmt.Sprintf("[%s]h:help[-]", ml.config.Colors.Footer)
		quitText := fmt.Sprintf("[%s]q:quit[-]", ml.config.Colors.Footer)
		sortText := fmt.Sprintf("[%s]s:sort[-]", ml.config.Colors.Footer)
		resetText := fmt.Sprintf("[%s]R:reset[-]", ml.config.Colors.Footer)
		moveText := fmt.Sprintf("[%s]j/k/g/G/u/d:move[-]", ml.config.Colors.Footer)
		return fmt.Sprintf("%s  %s  %s  %s  %s", helpText, quitText, sortText, resetText, moveText)
	} else {
		return "h:help  q:quit  s:sort  R:reset  j/k/g/G/u/d:move"
	}
}

func (ml *MainLayout) renderTable() string {
	t := TableRender(ml.mm, ml.sortKey)
	if ml.config.Border {
		t.SetStyle(table.StyleLight)
	} else {
		t.SetStyle(table.Style{
			Box: table.StyleBoxLight,
			Options: table.Options{
				DrawBorder:      false,
				SeparateColumns: false,
			},
		})
	}
	return t.Render()
}