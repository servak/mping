package ui

import (
	"errors"
	"fmt"
	"strings"
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
		Title  string `yaml:"-"`
		Border bool   `yaml:"border"`
	}
)

func NewCUI(mm *stats.MetricsManager, cfg *CUIConfig, interval time.Duration) (*CUI, error) {
	g, err := gocui.NewGui(gocui.OutputNormal, true)
	if err != nil {
		return nil, err
	}
	return &CUI{
		g:        g,
		mm:       mm,
		config:   cfg,
		interval: interval,
		key:      stats.Success,
	}, nil
}

func (c *CUI) render() string {
	t := TableRender(c.mm, c.key)
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
			v.Highlight = true
			v.Frame = false
			v.Clear()
			v.SelFgColor = gocui.ColorMagenta
			msg := fmt.Sprintf("Sort: %s, Interval: %dms", c.key, c.interval.Milliseconds())
			marginN := (maxX/2 - len(c.config.Title)/2) - len(msg)
			if marginN < 0 {
				marginN = 1
			}
			margin := strings.Repeat(" ", marginN)
			fmt.Fprintln(v, msg+margin+c.config.Title)
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
			fmt.Fprintln(v, "q:quit, s:sort, R:reset, move(k:up, j:down, g:top, G:bottom, u:pageUp, d:pageDown)")
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
		"s": c.changeSort(false),
		"S": c.changeSort(true),
		"j": originDown,
		"k": originUp,
		"g": originGoTop,
		"G": originGoBottom,
		"u": originPageUp,
		"d": originPageDown,
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
	return gocui.ErrQuit
}

func (c *CUI) changeSort(reverse bool) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if reverse {
			if int(c.key) == 0 {
				c.key = stats.Key(len(stats.Keys()) - 1)
			} else {
				c.key--
			}
		} else {
			if int(c.key+1) < len(stats.Keys()) {
				c.key++
			} else {
				c.key = 0
			}
		}

		c.g.Update(func(g *gocui.Gui) error {
			v, err := g.View("header")
			if err != nil {
				return err
			}
			maxX, _ := g.Size()
			v.Clear()
			msg := fmt.Sprintf("Sort: %s, Interval: %dms", c.key, c.interval.Milliseconds())
			marginN := (maxX/2 - len(c.config.Title)/2) - len(msg)
			if marginN < 0 {
				marginN = 1
			}
			margin := strings.Repeat(" ", marginN)
			fmt.Fprintln(v, msg+margin+c.config.Title)
			return nil
		})
		return nil
	}
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
	_, oy := v.Origin()
	bottom := len(v.ViewBufferLines())
	if (bottom - oy) <= wy {
		return nil
	}
	return v.SetOrigin(0, oy+1)
}

func originUp(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	_, oy := v.Origin()
	if oy == 0 {
		return nil
	}
	return v.SetOrigin(0, oy-1)
}

func originGoTop(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	return v.SetOrigin(0, 0)
}

func originGoBottom(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	_, wy := v.Size()
	bottom := len(v.ViewBufferLines())
	return v.SetOrigin(0, bottom-wy)
}

func originPageDown(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	_, wy := v.Size()
	_, oy := v.Origin()
	slide := wy + oy
	bottom := len(v.ViewBufferLines())
	if (bottom - wy) < slide {
		return v.SetOrigin(0, (bottom - wy))
	}
	return v.SetOrigin(0, slide)
}

func originPageUp(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	_, wy := v.Size()
	_, oy := v.Origin()
	slide := oy - wy
	if 0 > slide {
		return v.SetOrigin(0, 0)
	}
	return v.SetOrigin(0, slide)
}
