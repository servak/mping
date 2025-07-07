package tui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
	"github.com/servak/mping/internal/ui/tui/state"
)

// TUIApp is the main controller for tview application
type TUIApp struct {
	app      *tview.Application
	layout   *LayoutManager
	state    *state.UIState
	mm       *stats.MetricsManager
	config   *shared.Config
	interval time.Duration
	timeout  time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewTUIApp creates a new TUIApp instance
func NewTUIApp(mm *stats.MetricsManager, cfg *shared.Config, interval, timeout time.Duration) *TUIApp {
	if cfg == nil {
		cfg = shared.DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := tview.NewApplication()
	uiState := state.NewUIState()
	layout := NewLayoutManager(uiState, mm, cfg, interval, timeout)

	tuiApp := &TUIApp{
		app:      app,
		layout:   layout,
		state:    uiState,
		mm:       mm,
		config:   cfg,
		interval: interval,
		timeout:  timeout,
		ctx:      ctx,
		cancel:   cancel,
	}

	tuiApp.setupCallbacks()
	tuiApp.setupKeyBindings()
	tuiApp.setupHelpModal()

	return tuiApp
}

// Run starts the application
func (a *TUIApp) Run() error {
	a.app.SetRoot(a.layout.GetRoot(), true).SetFocus(a.layout.GetRoot())
	return a.app.Run()
}

// Update refreshes the display content
func (a *TUIApp) Update() {
	a.app.QueueUpdateDraw(func() {
		// Update all panels - they will read state internally
		a.layout.UpdateAll()
	})
}

// Close terminates the application
func (a *TUIApp) Close() {
	a.cancel()
	a.app.Stop()
}

// setupCallbacks configures callbacks between components
func (a *TUIApp) setupCallbacks() {
	// Set focus callback for filter input
	a.layout.SetFocusCallback(func() {
		a.app.SetFocus(a.layout.GetRoot())
	})

	// Set filter input done function
	a.layout.SetFilterDoneFunc(a.handleFilterDone)

	// Set row selection callback
	a.layout.GetHostListPanel().SetSelectedFunc(a.handleRowSelection)
	
	// Set selection change callback for detail panel updates
	a.layout.GetHostListPanel().SetSelectionChangeCallback(func(metrics stats.MetricsReader) {
		a.layout.SetSelectedMetrics(metrics)
	})
}

// setupKeyBindings configures key bindings
func (a *TUIApp) setupKeyBindings() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// When help modal is visible
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


		// When filter input is visible, let it handle its own keys
		if a.layout.IsFilterShown() {
			return event
		}

		// Main screen key bindings
		switch event.Key() {
		case tcell.KeyEscape:
			// Clear filter if one is active (k9s-like behavior)
			if a.state.GetFilter() != "" {
				a.clearFilter()
				return nil
			}
		}

		switch event.Rune() {
		case 'q':
			a.Close()
			return nil
		case 'h':
			a.showHelp()
			return nil
		case 'v':
			a.toggleDetailView()
			return nil
		case 's':
			a.nextSort()
			return nil
		case 'S':
			a.prevSort()
			return nil
		case 'r':
			a.reverseSort()
			return nil
		case 'R':
			a.resetMetrics()
			return nil
		case '/':
			a.showFilter()
			return nil
		}

		// Delegate navigation to layout
		return a.layout.HandleKeyEvent(event)
	})
}

// setupHelpModal creates and adds help modal
func (a *TUIApp) setupHelpModal() {
	helpModal := a.createHelpModal()
	a.layout.AddModal("help", helpModal)
}

// createHelpModal creates help modal content
func (a *TUIApp) createHelpModal() *tview.Modal {
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
  r            Reverse sort order     
  R            Reset all metrics      
  v            Toggle detail view     
  /            Filter hosts           
  h            Show/hide this help    
  q, Ctrl+C    Quit application       

FILTER:                              
  /            Start filter input     
  Enter        Apply filter           
  Esc          Cancel/Clear filter    

Press 'h' or Esc to close           `

	return tview.NewModal().
		SetText(helpText).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// Button press handling is done by parent
		})
}

// Sort-related methods
func (a *TUIApp) nextSort() {
	keys := stats.Keys()
	currentKey := a.state.GetSortKey()
	if int(currentKey+1) < len(keys) {
		a.state.SetSortKey(currentKey + 1)
	} else {
		a.state.SetSortKey(0)
	}
}

func (a *TUIApp) prevSort() {
	keys := stats.Keys()
	currentKey := a.state.GetSortKey()
	if int(currentKey) == 0 {
		a.state.SetSortKey(stats.Key(len(keys) - 1))
	} else {
		a.state.SetSortKey(currentKey - 1)
	}
}

func (a *TUIApp) reverseSort() {
	a.state.ReverseSort()
}

func (a *TUIApp) resetMetrics() {
	a.mm.ResetAllMetrics()
}

// Filter-related methods
func (a *TUIApp) showFilter() {
	a.layout.SetFilterText(a.state.GetFilter())
	a.layout.showFilterInput()
	a.app.SetFocus(a.layout.GetFilterInput())
}

func (a *TUIApp) clearFilter() {
	a.state.ClearFilter()
}

// View toggle methods
func (a *TUIApp) toggleDetailView() {
	a.layout.ToggleDetailView()
}

func (a *TUIApp) handleFilterDone(key tcell.Key) {
	switch key {
	case tcell.KeyEnter:
		// Apply filter
		filterText := a.layout.GetFilterText()
		a.state.SetFilter(filterText)
		a.layout.HideFilterInput()
		// Don't call a.Update() here - it causes infinite loop
		// The regular update cycle will handle the refresh
		a.layout.RestoreFocus()
	case tcell.KeyEscape:
		// Cancel filter input
		a.layout.HideFilterInput()
		a.layout.RestoreFocus()
	}
}

// Help modal related methods
func (a *TUIApp) showHelp() {
	a.layout.ShowPage("help")
	a.app.SetFocus(a.layout.GetRoot())
}

func (a *TUIApp) hideHelp() {
	a.layout.HidePage("help")
	a.app.SetFocus(a.layout.GetRoot())
}

func (a *TUIApp) isHelpVisible() bool {
	frontPageName, _ := a.layout.GetFrontPage()
	return a.layout.HasPage("help") && frontPageName == "help"
}

// Row selection handling
func (a *TUIApp) handleRowSelection(row, col int) {
	if row == 0 {
		return // Skip header row
	}

	// Get filtered metrics directly
	metrics := a.getFilteredMetrics()
	tableData := shared.NewTableData(metrics, a.state.GetSortKey(), a.state.IsAscending())

	// Convert table row to data row (subtract 1 for header)
	dataRow := row - 1
	if metric, ok := tableData.GetMetricAtRow(dataRow); ok {
		// Update detail panel instead of showing modal
		a.layout.SetSelectedMetrics(metric)
	}
}


// getFilteredMetrics returns filtered metrics based on current state
func (a *TUIApp) getFilteredMetrics() []stats.MetricsReader {
	metrics := a.mm.SortByWithReader(a.state.GetSortKey(), a.state.IsAscending())
	return shared.FilterMetrics(metrics, a.state.GetFilter())
}

