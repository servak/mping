package tui

import (
	"fmt"
	"time"

	"github.com/rivo/tview"

	"github.com/servak/mping/internal/stats"
	"github.com/servak/mping/internal/ui/shared"
)

// Renderer generates display content from data + state
type Renderer struct {
	mm       *stats.MetricsManager
	config   *shared.Config
	interval time.Duration
	timeout  time.Duration
}

// NewRenderer creates a new Renderer instance
func NewRenderer(mm *stats.MetricsManager, cfg *shared.Config, interval, timeout time.Duration) *Renderer {
	return &Renderer{
		mm:       mm,
		config:   cfg,
		interval: interval,
		timeout:  timeout,
	}
}


// RenderHostDetail generates detailed information for a host
func (r *Renderer) RenderHostDetail(metric stats.Metrics) string {
	return fmt.Sprintf(`Host Details: %s

Total Probes: %d
Successful: %d
Failed: %d
Loss Rate: %.1f%%
Last RTT: %s
Average RTT: %s
Minimum RTT: %s
Maximum RTT: %s
Last Success: %s
Last Failure: %s
Last Error: %s`,
		metric.Name,
		metric.Total,
		metric.Successful,
		metric.Failed,
		metric.Loss,
		shared.DurationFormater(metric.LastRTT),
		shared.DurationFormater(metric.AverageRTT),
		shared.DurationFormater(metric.MinimumRTT),
		shared.DurationFormater(metric.MaximumRTT),
		shared.TimeFormater(metric.LastSuccTime),
		shared.TimeFormater(metric.LastFailTime),
		metric.LastFailDetail,
	)
}

// CreateHelpModal creates help modal content
func (r *Renderer) CreateHelpModal() *tview.Modal {
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

