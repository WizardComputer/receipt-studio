//go:build desktop && (darwin || windows)

package desktop

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Shared native palette and action icons. Controls retain MyGo's focus,
// keyboard and accessibility behavior.
var (
	studioBG    = ui.RGB(245, 245, 247)
	studioWhite = ui.RGB(255, 255, 255)
	studioInk   = ui.RGB(29, 29, 31)
	studioMuted = ui.RGB(134, 134, 139)
	studioLine  = ui.RGB(232, 232, 237)
	studioBlue  = ui.RGB(0, 122, 255)
	studioTheme = func() ui.Theme {
		t := *ui.LightTheme()
		t.Background, t.Surface, t.Text, t.TextMuted = studioBG, studioWhite, studioInk, studioMuted
		t.Border, t.Accent, t.InactiveAccent = studioLine, studioBlue, studioBlue
		t.Radius, t.FontSize = 8, 12
		return t
	}()
	studioIcons = func() map[string]*ui.SVG {
		paths := map[string]string{
			"diagnostics":  "M3 12h4l3-8 4 16 3-8h4",
			"calendar":     "M4 5h16v16H4zM4 10h16M8 3v4M16 3v4",
			"printer":      "M7 8V3h10v5M6 17H3V8h18v9h-3M6 14h12v7H6zM17 11h.01",
			"template":     "M5 3h10l4 4v14H5zM15 3v5h4M8 7h3M8 11h8M8 14h3M14 14h2M8 17h3M14 17h2",
			"jobs":         "M5 6h14M5 12h14M5 18h14",
			"api":          "M3 7h17m-4-4 4 4-4 4M21 17H4m4-4-4 4 4 4",
			"templateEdit": "M5 21V3h10l4 4v4M15 3v5h4M8 7h3M8 11h6M8 15h2m3 6 1-4 6-6 3 3-6 6-4 1m5-8 3 3",
			"settings":     "M4 7h16M4 17h16M8 4v6M16 14v6",
			"download":     "M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5",
			"copy":         "M8 8h12v12H8zM16 8V4H4v12h4",
			"trash":        "M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7",
			"details":      "M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h7",
			"repeat":       "M3 10a9 9 0 1 1 1 8M3 4v6h6",
			"history":      "M3 10a9 9 0 1 1 1 8M3 4v6h6M12 7v6l4 2",
			"close":        "m7 7 10 10M7 17 17 7",
			"text":         "M5 5h14M12 5v15M8 20h8M5 5v3M19 5v3",
			"table":        "M3 3h18v18H3zM9 3v18M15 3v18M3 9h18M3 15h18",
			"image":        "M3 3h18v18H3zM3 17l6-6 5 5 3-3 4 4M8 7h.01",
			"undo":         "M8 4 3 9l5 5M3 9h10a7 7 0 0 1 7 7v3",
			"redo":         "m16 4 5 5-5 5M21 9H11a7 7 0 0 0-7 7v3",
		}
		icons := map[string]*ui.SVG{}
		for name, path := range paths {
			icons[name] = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="` + path + `"/></svg>`))
		}
		return icons
	}()
)

func studioButton(c *ui.Context, label string, primary bool) ui.Element {
	b := ui.ButtonBase(c).Label(tr(c, label)).Height(36).PaddingX(15).Radius(8).Border(1, studioLine)
	bg, fg := studioWhite, studioInk
	if primary {
		bg, fg = studioBlue, studioWhite
	}
	if b.Hovered() {
		if primary {
			bg = ui.RGB(0, 108, 230)
		} else {
			bg = studioBG
		}
	}
	b.Background(bg).Children(func() { studioText(c, label).FontSize(12).FontWeight(550).TextColor(fg) })
	return b
}

func studioIconButton(c *ui.Context, label, icon string) ui.Element {
	b := ui.ButtonBase(c).Label(tr(c, label)).Tooltip(tr(c, label)).Size(36, 36).Radius(8).Border(1, studioLine).Background(studioWhite)
	if b.Hovered() {
		b.Background(studioBG)
	}
	b.Children(func() {
		if icon == "qr" {
			studioText(c, "QR").FontSize(12).Bold()
		} else {
			ui.Icon(c, studioIcons[icon]).Size(18, 18).TextColor(studioInk)
		}
	})
	return b
}

func studioCard(c *ui.Context) ui.Element {
	return ui.Column(c).FillWidth().Padding(24).Gap(14).Background(studioWhite).Border(1, studioLine).Radius(16).Shadow(0, 2, 4, 0, ui.RGBA(0, 0, 0, .02))
}

func studioGlyph(c *ui.Context, icon string) {
	ui.Box(c).Size(55, 55).Radius(13).Background(ui.RGB(245, 247, 250)).Border(1, studioLine).Center().Children(func() {
		ui.Icon(c, studioIcons[icon]).Size(28, 28).TextColor(ui.RGB(100, 108, 120))
	})
}

func studioTabs(c *ui.Context, selected *string, tabs []string) {
	ui.Row(c).Padding(4).Gap(5).Radius(9).Background(ui.RGB(242, 242, 247)).Children(func() {
		for _, tab := range tabs {
			b := ui.ButtonBase(c.Key(tab)).Label(tr(c, tab)).Height(32).PaddingX(15).Radius(7)
			if *selected == tab {
				b.Background(studioWhite).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, .06))
			}
			b.Children(func() { studioText(c, tab).FontSize(12).TextColor(studioInk) })
			if b.Clicked() {
				*selected = tab
			}
		}
	})
}

func studioLabel(c *ui.Context, label string) {
	studioText(c, label).FontSize(11).TextColor(ui.RGB(116, 116, 125))
}

// Keep an edit buffer so an empty/partial number can be typed without changing
// the receipt. Only finite numbers within the field's bounds reach the model.
func studioNumber(c *ui.Context, value *float64, lo, hi, step float64) ui.Element {
	lo = math.Ceil(lo/step) * step
	hi = math.Floor(hi/step) * step
	wrap := ui.Row(c).FillWidth().Gap(0).AlignItems(ui.Center)
	format := func() string { return strconv.FormatFloat(math.Round(*value/step)*step, 'f', -1, 64) }
	text := ui.Local(wrap, "number-text", format)
	last := ui.Local(wrap, "number-value", func() float64 { return *value })
	pending := ui.Local(wrap, "number-arrow", func() bool { return false })
	if *last != *value {
		*text, *last = format(), *value
	}
	var input ui.Element
	wrap.Children(func() {
		input = ui.TextInput(c, text).Width(0).Grow(1).MinWidth(0).Height(34)
		input.OnTextKey(func(k ui.TextKey) bool {
			if k.Mods != 0 || (k.Key != ui.KeyUp && k.Key != ui.KeyDown) {
				return false
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(k.Text()), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				n = *last
			}
			delta := step
			if k.Key == ui.KeyDown {
				delta = -step
			}
			n = math.Max(lo, math.Min(hi, math.Round(n/step)*step+delta))
			k.SetText(strconv.FormatFloat(n, 'f', -1, 64))
			*pending = true
			return true
		})
		if *pending {
			input.SetChanged()
			*pending = false
		}
		if input.Changed() {
			if n, err := strconv.ParseFloat(strings.TrimSpace(*text), 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= lo && n <= hi {
				*value = math.Round(n/step) * step
				*last = *value
			}
		}
		ui.Column(c).Width(20).Gap(0).Children(func() {
			for _, direction := range []int{1, -1} {
				label, glyph := "Povećaj", "▴"
				if direction < 0 {
					label, glyph = "Smanji", "▾"
				}
				b := ui.ButtonBase(c.Key(direction)).Label(tr(c, label)).Size(20, 17).Radius(3)
				if b.Hovered() {
					b.Background(studioBG)
				}
				b.Children(func() { studioText(c, glyph).FontSize(10).TextColor(studioMuted) })
				if b.Clicked() {
					*value = math.Max(lo, math.Min(hi, math.Round(*value/step)*step+float64(direction)*step))
					*last = *value
					*text = format()
					input.SetChanged()
				}
			}
		})
		if !input.Focused() {
			*text = format()
		}
	})
	return input
}

func studioTextArea(c *ui.Context, value *string) ui.Element {
	input := ui.TextArea(c, value)
	initialized := ui.Local(input, "initial-selection", func() bool { return false })
	if !*initialized {
		input.SetTextSelection(0, 0)
		*initialized = true
	}
	return input
}

func studioModal(c *ui.Context, key, eyebrow, title string, width float32, open *bool, body func()) {
	_, height := c.Size()
	ui.DialogBase(c.Key(key), open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(26, 32, 59, .2))
		panel.Width(width).MaxHeight(height-40).Padding(28).Radius(19).Background(studioWhite).Border(1, studioLine).Shadow(0, 16, 60, 0, ui.RGBA(20, 24, 40, .16))
		ui.Column(c).FillWidth().Gap(20).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Width(0).Grow(1).Gap(7).Children(func() {
					studioText(c, eyebrow).FontSize(9).TextColor(studioMuted)
					ui.Text(c, title).FontSize(22).FontWeight(620)
				})
				if studioIconButton(c, "Zatvori", "close").Size(28, 28).Radius(14).Background(studioBG).Clicked() {
					*open = false
				}
			})
			body()
		})
	})
}

// Direct, visibly selected formatting actions. Labels remain accessible even
// where the face is a short symbol such as B or U.
func studioOption(c *ui.Context, label, face string, on bool) ui.Element {
	b := ui.ButtonBase(c).Label(tr(c, label)).Tooltip(tr(c, label)).Height(32).Width(0).Grow(1).Radius(7).Border(1, studioLine)
	bg, fg := studioWhite, studioInk
	if on {
		bg, fg = ui.RGB(237, 245, 255), studioBlue
		b.Border(1, ui.RGB(184, 217, 255))
	} else if b.Hovered() {
		bg = studioBG
	}
	b.Background(bg).Children(func() {
		t := studioText(c, face).FontSize(11).TextColor(fg)
		if face == "B" {
			t.Bold()
		}
		if face == "U" {
			t.Underline()
		}
	})
	return b
}

func studioAlignment(c *ui.Context, label string, value *string) bool {
	changed := false
	ui.Row(c).FillWidth().Gap(5).Children(func() {
		for i, key := range []string{"left", "center", "right"} {
			name := []string{"Lijevo", "Centar", "Desno"}[i]
			if studioOption(c.Key(key), tr(c, label)+" · "+tr(c, name), name, *value == key).Clicked() {
				*value = key
				changed = true
			}
		}
	})
	return changed
}
