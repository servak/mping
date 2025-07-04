package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Layout manages the main screen layout
type Layout struct {
	root     *tview.Flex
	header   *tview.TextView
	mainView *tview.TextView
	footer   *tview.TextView
	renderer *Renderer
}

// NewLayout creates a new Layout
func NewLayout(renderer *Renderer) *Layout {
	layout := &Layout{
		renderer: renderer,
	}
	
	layout.setupViews()
	layout.setupLayout()
	layout.Update()
	
	return layout
}

// setupViews initializes each view
func (l *Layout) setupViews() {
	// Header
	l.header = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	
	// Main view (table display area)
	l.mainView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	
	// Footer
	l.footer = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
}

// setupLayout configures the layout
func (l *Layout) setupLayout() {
	l.root = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(l.header, 1, 0, false).
		AddItem(l.mainView, 0, 1, true).
		AddItem(l.footer, 1, 0, false)
}

// Root returns the root element of the layout
func (l *Layout) Root() tview.Primitive {
	return l.root
}

// Update refreshes the display content
func (l *Layout) Update() {
	l.header.SetText(l.renderer.RenderHeader())
	l.mainView.SetText(l.renderer.RenderMain())
	l.footer.SetText(l.renderer.RenderFooter())
}

// HandleKeyEvent handles key events
func (l *Layout) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'j':
		l.scrollDown()
		return nil
	case 'k':
		l.scrollUp()
		return nil
	case 'g':
		l.scrollToTop()
		return nil
	case 'G':
		l.scrollToBottom()
		return nil
	case 'u':
		l.pageUp()
		return nil
	case 'd':
		l.pageDown()
		return nil
	}
	return event
}

// Scroll operation methods
func (l *Layout) scrollDown() {
	row, col := l.mainView.GetScrollOffset()
	l.mainView.ScrollTo(row+1, col)
}

func (l *Layout) scrollUp() {
	row, col := l.mainView.GetScrollOffset()
	if row > 0 {
		l.mainView.ScrollTo(row-1, col)
	}
}

func (l *Layout) scrollToTop() {
	l.mainView.ScrollToBeginning()
}

func (l *Layout) scrollToBottom() {
	l.mainView.ScrollToEnd()
}

func (l *Layout) pageDown() {
	_, _, _, height := l.mainView.GetRect()
	row, col := l.mainView.GetScrollOffset()
	l.mainView.ScrollTo(row+height, col)
}

func (l *Layout) pageUp() {
	_, _, _, height := l.mainView.GetRect()
	row, col := l.mainView.GetScrollOffset()
	if row >= height {
		l.mainView.ScrollTo(row-height, col)
	} else {
		l.mainView.ScrollToBeginning()
	}
}