package ui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
)

// UI is an interface for UI components
type UI interface {
	Run() error
	Update()
	Close()
}

// Config manages UI settings
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

// App is the main controller for tview application
type App struct {
	app      *tview.Application
	pages    *tview.Pages
	layout   *Layout
	renderer *Renderer
	mm       *stats.MetricsManager
	config   *Config
	interval time.Duration
	timeout  time.Duration
	sortKey  stats.Key
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewApp creates a new App instance
func NewApp(mm *stats.MetricsManager, cfg *Config, interval, timeout time.Duration) *App {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := tview.NewApplication()
	pages := tview.NewPages()

	renderer := NewRenderer(mm, cfg, interval, timeout)
	layout := NewLayout(renderer)

	// Add main page and help modal
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
		timeout:  timeout,
		sortKey:  stats.Success,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Run starts the application
func (a *App) Run() error {
	a.setupKeyBindings()
	a.renderer.SetSortKey(a.sortKey)
	a.app.SetRoot(a.pages, true).SetFocus(a.layout.Root())
	return a.app.Run()
}

// Update refreshes the display content
func (a *App) Update() {
	a.app.QueueUpdateDraw(func() {
		a.layout.Update()
	})
}

// Close terminates the application
func (a *App) Close() {
	a.cancel()
	a.app.Stop()
}

// setupKeyBindings configures key bindings
func (a *App) setupKeyBindings() {
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

		// Main screen key bindings
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
		case 'r':
			a.reverseSort()
			return nil
		case 'R':
			a.resetMetrics()
			return nil
		}

		// Delegate scroll operations to layout
		return a.layout.HandleKeyEvent(event)
	})
}

// Sort-related methods
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

func (a *App) reverseSort() {
	a.renderer.ReverseSort()
}

func (a *App) resetMetrics() {
	a.mm.ResetAllMetrics()
}

// Help modal related methods
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

// createHelpModal creates help modal
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
  r            Reverse sort order     
  R            Reset all metrics      
  h            Show/hide this help    
  q, Ctrl+C    Quit application       

Press 'h' or Esc to close           `

	return tview.NewModal().
		SetText(helpText).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// Button press handling is done by parent
		})
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	cfg := &Config{
		Title:        "mping",
		Border:       true,
		EnableColors: true, // Enable colors by default
	}

	// Use color names available in tview
	cfg.Colors.Header = "dodgerblue"
	cfg.Colors.Footer = "gray"
	cfg.Colors.Success = "green"
	cfg.Colors.Warning = "yellow"
	cfg.Colors.Error = "red"
	cfg.Colors.ModalBorder = "white"

	return cfg
}
