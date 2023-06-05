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
	t := c.genTable()
	_, y := c.g.Size()
	if c.config.Border {
		t.SetStyle(table.StyleLight)
		t.SetPageSize(y - 6)
	} else {
		t.SetStyle(table.Style{
			Options: table.OptionsNoBordersAndSeparators,
		})
		t.SetPageSize(y - 3)
	}
	return t.Render()
}

func (c *CUI) genTable() table.Writer {
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
	return t
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
			fmt.Fprintln(v, fmt.Sprintf("Sort: %s, Interval: %s", c.key, durationFormater(c.interval)))
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
		v.Clear()
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
	t := c.genTable()
	t.SetStyle(table.StyleLight)
	fmt.Println(t.Render())
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
		fmt.Fprintln(v, fmt.Sprintf("Sort: %s, Interval: %s", c.key, durationFormater(c.interval)))
		return nil
	})
	return nil
}

func (c *CUI) reset(g *gocui.Gui, v *gocui.View) error {
	c.mm.ResetAllMetrics()
	return nil
}
