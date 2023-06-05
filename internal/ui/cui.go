package ui

import (
	"errors"
	"fmt"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/awesome-gocui/gocui"
	"github.com/servak/mping/internal/stats"
)

const MAIN_VIEW = "main"

type (
	CUI struct {
		g        *gocui.Gui
		mm       *stats.MetricsManager
		config   *CUIConfig
		interval time.Duration
		key      stats.Key
	}

	CUIConfig struct {
		Border bool `yaml:"border"`
	}
)

func NewCUI(mm *stats.MetricsManager, interval time.Duration, cfg *CUIConfig) (*CUI, error) {
	g, err := gocui.NewGui(gocui.OutputNormal, true)
	if err != nil {
		return nil, err
	}
	return &CUI{
		g:        g,
		mm:       mm,
		config:   cfg,
		key:      stats.Success,
		interval: interval,
	}, nil
}

func (c *CUI) render() string {
	t := table.NewWriter()
	t.AppendHeader(table.Row{stats.Host, stats.Sent, stats.Success, stats.Fail, stats.Loss, stats.Last, stats.Avg, stats.Best, stats.Worst, stats.LastSuccTime, stats.LastFailTime})
	df := durationFormater
	tf := timeFormater
	for _, m := range c.mm.GetSortedMetricsByKey(c.key) {
		t.AppendRow(table.Row{
			m.Name,
			m.Total,
			m.Successful,
			m.Failed,
			fmt.Sprintf("%5.1f%%", m.Loss),
			df(m.LastRTT),
			df(m.AverageRTT),
			df(m.MinimumRTT),
			df(m.MaximumRTT),
			tf(m.LastSuccTime),
			tf(m.LastFailTime),
		})
	}
	if c.config.Border {
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

func (c *CUI) Run() error {
	layout := func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		if v, err := g.SetView("header", 0, -1, maxX, 1, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Frame = false
			v.Clear()
			fmt.Fprintln(v, fmt.Sprintf("Sort: %s, Interval: %dms", c.key, c.interval.Milliseconds()))
		}
		if v, err := g.SetView(MAIN_VIEW, 0, 0, maxX, maxY-1, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			if _, err := g.SetCurrentView(MAIN_VIEW); err != nil {
				return err
			}
			v.Frame = false
			v.Clear()
			fmt.Fprintln(v, c.render())
		}
		if v, err := g.SetView("footer", 0, maxY-2, maxX, maxY, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Frame = false
			v.Clear()
			fmt.Fprintln(v, "q: quit, j: down, k: up, s: sort, R: reset")
		}
		return nil
	}

	c.g.SetManagerFunc(layout)
	err := c.keybindings()
	if err != nil {
		return err
	}
	if err = c.g.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}
	return nil
}

func (c *CUI) Update() {
	c.g.Update(func(g *gocui.Gui) error {
		v, err := g.View(MAIN_VIEW)
		if err != nil {
			return err
		}
		ox, oy := v.Origin()
		v.Clear()
		v.SetOrigin(ox, oy)
		fmt.Fprint(v, c.render())
		return nil
	})
}

func (c *CUI) Close() {
	c.g.Close()
}

func (c *CUI) keybindings() error {
	keymaps := map[string]func(*gocui.Gui, *gocui.View) error{
		"q": c.quit,
		"s": c.changeSort,
		"j": originDown,
		"k": originUp,
		"R": c.reset,
	}
	for k, v := range keymaps {
		keyForced, modForced := gocui.MustParse(k)
		if err := c.g.SetKeybinding("", keyForced, modForced, v); err != nil {
			return err
		}
	}
	return nil
}

func (c CUI) quit(g *gocui.Gui, v *gocui.View) error {
	c.Close()
	fmt.Println(c.render())
	return gocui.ErrQuit
}

func (c *CUI) changeSort(g *gocui.Gui, v *gocui.View) error {
	if int(c.key+1) < len(stats.Keys()) {
		c.key++
	} else {
		c.key = 0
	}
	c.g.Update(func(g *gocui.Gui) error {
		v, err := g.View("header")
		if err != nil {
			return err
		}
		v.Clear()
		fmt.Fprintln(v, fmt.Sprintf("Sort: %s, Interval: %dms", c.key, c.interval.Milliseconds()))
		return nil
	})
	return nil
}

func (c *CUI) reset(g *gocui.Gui, v *gocui.View) error {
	c.mm.ResetAllMetrics()
	return nil
}

func originDown(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	_, wy := v.Size()
	ox, oy := v.Origin()
	bottom := len(v.ViewBufferLines())
	if (bottom - oy) <= wy {
		return nil
	}
	return v.SetOrigin(ox, oy+1)
}

func originUp(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	ox, oy := v.Origin()
	if oy == 0 {
		return nil
	}
	return v.SetOrigin(ox, oy-1)
}
