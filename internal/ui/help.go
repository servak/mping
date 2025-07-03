package ui

import (
	"github.com/rivo/tview"
)

// HelpModal はヘルプモーダルを管理
type HelpModal struct {
	modal *tview.Modal
}

// NewHelpModal は新しい HelpModal を作成
func NewHelpModal() *HelpModal {
	helpText := `mping - Multi-target Ping Tool

KEYBINDINGS:
  Navigation:
    j, ↓         Move down
    k, ↑         Move up  
    g            Go to top
    G            Go to bottom
    u, Page Up   Page up
    d, Page Down Page down

  Sorting:
    s            Next sort key
    S            Previous sort key

  Other:
    h            Show/hide this help
    R            Reset all metrics
    q, Ctrl+C    Quit application

SORT KEYS:
  Host, Sent, Success, Fail, Loss%, Last RTT, 
  Avg RTT, Best RTT, Worst RTT, Last Success, Last Fail

PROBE TYPES:
  icmpv4:host  - ICMP IPv4 ping
  icmpv6:host  - ICMP IPv6 ping  
  http://url   - HTTP probe
  https://url  - HTTPS probe
  tcp://host   - TCP connection probe
  dns://host   - DNS query probe
  ntp://host   - NTP time probe

Press 'h' or 'Esc' to close this help.`

	modal := tview.NewModal().
		SetText(helpText).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// ボタンが押されたときの処理は親で処理
		})

	return &HelpModal{
		modal: modal,
	}
}

// Modal はモーダルウィジェットを返す
func (hm *HelpModal) Modal() *tview.Modal {
	return hm.modal
}