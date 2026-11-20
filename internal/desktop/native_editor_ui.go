//go:build desktop && (darwin || windows)

package desktop

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/i18n"
	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/receipt"
)

type nativeDrag struct{ ID, Kind string }

type nativeEditor struct {
	doc                               *receipt.NativeDocument
	encoder                           *receipt.Encoder
	plan                              receipt.NativePlan
	preview                           *nativeReceiptPreview
	bytes                             []byte
	resizeID                          string
	resizeWidth, resizeX, resizeDelta float64
	cache, error, dataText, tab       string
	zoom                              float64
	profileExpanded                   bool
	post                              func(func())
	onSave                            func(json.RawMessage, json.RawMessage) error
	onClose                           func()
	language                          string
	languageToggle                    func(*ui.Context)
}

func newNativeEditor(enc *receipt.Encoder, t, data json.RawMessage) (*nativeEditor, error) {
	d, err := receipt.OpenNativeDocument(t, data)
	if err != nil {
		return nil, err
	}
	return &nativeEditor{doc: d, encoder: enc, zoom: .8, dataText: prettyJSON(data), tab: "Dizajn"}, nil
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func textValue(v any) string { s, _ := v.(string); return s }

func numberValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func (e *nativeEditor) refresh() {
	key := string(e.doc.JSON()) + "\x00" + string(e.doc.Data)
	if key == e.cache {
		return
	}
	e.cache = key
	plan, b, err := e.encoder.NativeCompile(e.doc)
	if err != nil {
		e.error = err.Error()
		e.bytes = nil
		return
	}
	preview, err := nativeReceiptSVG(plan, e.doc)
	if err != nil {
		e.error = err.Error()
		e.bytes = nil
		return
	}
	e.plan, e.bytes, e.preview = plan, b, preview
	e.error = ""
}

func (e *nativeEditor) drop(v nativeDrag, before string) {
	if v.Kind != "" {
		el, err := e.encoder.NativeElement(v.Kind, "element-"+model.Secret()[:12])
		if err != nil {
			e.error = err.Error()
			return
		}
		e.doc.Insert(el, before)
	} else {
		e.doc.Move(v.ID, before)
	}
}

func (e *nativeEditor) field(c *ui.Context, label, key string, element map[string]any, multi bool) {
	value := textValue(element[key])
	studioLabel(c, label)
	var input ui.Element
	if multi {
		input = studioTextArea(c.Key(key), &value).Height(95)
	} else {
		input = ui.TextInput(c.Key(key), &value).Height(34)
	}
	input.Label(tr(c, label)).FillWidth()
	if input.Changed() {
		e.doc.Set(textValue(element["id"]), key, value)
	}
}

func (e *nativeEditor) number(c *ui.Context, label, key string, element map[string]any, min, max, step float64) {
	value := numberValue(element[key])
	studioLabel(c, label)
	if studioNumber(c.Key(key), &value, min, max, step).Label(tr(c, label)).FillWidth().Height(34).Changed() {
		e.doc.Set(textValue(element["id"]), key, value)
	}
}

func (e *nativeEditor) choice(c *ui.Context, label, key string, element map[string]any, options []string) {
	value := textValue(element[key])
	if key == "language" && value == "" {
		value = "esc-pos"
	}
	previous := value
	labels := map[string]string{"wide": "80 mm · 576 dots", "narrow": "58 mm · 384 dots", "esc-pos": "ESC/POS", "star-prnt": "StarPRNT", "star-line": "Star Line", "left": "Lijevo", "center": "Sredina", "right": "Desno", "native": "Komanda printera", "image": "Slika QR koda"}
	names := make([]string, len(options))
	selected := value
	for i, option := range options {
		names[i] = option
		if translated := labels[option]; translated != "" {
			names[i] = translated
		}
		if option == value {
			selected = names[i]
		}
	}
	studioLabel(c, label)
	if studioSelect(c.Key(key), &selected, names).Label(tr(c, label)).FillWidth().Height(34).Changed() {
		for i, name := range names {
			if name == selected {
				value = options[i]
			}
		}
		e.doc.Change(func() {
			element[key] = value
			if key == "language" && (previous == "esc-pos") != (value == "esc-pos") {
				page := float64(5)
				if value == "esc-pos" {
					page = 18
				}
				e.doc.Template["codePage"] = page
			}
		})
	}
}

func (e *nativeEditor) check(c *ui.Context, label, key string, element map[string]any) {
	value, _ := element[key].(bool)
	if ui.Checkbox(c.Key(key), &value, tr(c, label)).Changed() {
		e.doc.Set(textValue(element["id"]), key, value)
	}
}

func (e *nativeEditor) view(c *ui.Context) {
	*nativeLanguage(c) = e.language
	c.SetTheme(&studioTheme)
	e.refresh()
	if !c.FocusTakesText() && e.resizeID == "" {
		if c.Shortcut(0, ui.KeyBackspace) || c.Shortcut(0, ui.KeyDelete) {
			e.doc.Delete()
		}
		if c.Shortcut(ui.Cmd, ui.KeyZ) {
			e.doc.Undo()
		}
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) {
			e.doc.Redo()
		}
	}

	leftWidth, rightWidth := nativeEditorSides(c)
	closeRequested := false
	ui.Column(c).FillWidth().Height(0).Grow(1).MinHeight(0).Background(studioWhite).Children(func() {
		ui.Row(c).FillWidth().Height(65).Shrink(0).PaddingEnd(20).Gap(8).AlignItems(ui.Center).Border(1, studioLine).Children(func() {
			name := textValue(e.doc.Template["name"])
			nameInput := ui.TextInputBase(c.Key("template-name"), &name).Label(tr(c, "Naziv dokumenta")).Width(leftWidth).Shrink(0).Height(36).PaddingX(20).FontSize(12).FontWeight(600)
			nameInput.DrawOver(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 7, W: r.W, H: 1}, studioLine, 0)
			})
			if nameInput.Changed() {
				e.doc.Set("", "name", name)
			}
			ui.Box(c).Width(0).Grow(1)
			if studioButton(c, "Izvezi komande", false).Disabled(e.error != "").Clicked() {
				go e.exportBytes(append([]byte(nil), e.bytes...), e.language)
			}
			if studioButton(c, "Sačuvaj predložak", true).Disabled(e.error != "").Clicked() && e.onSave != nil {
				if err := e.onSave(e.doc.JSON(), e.doc.Data); err != nil {
					e.error = err.Error()
				}
			}
			if e.onClose != nil && studioButton(c, "Zatvori editor", false).Clicked() {
				closeRequested = true
			}
			if e.languageToggle != nil {
				e.languageToggle(c)
			}
		})
		ui.Row(c).FillWidth().Height(0).Grow(1).MinHeight(0).Children(func() {
			ui.Scroll(c).Width(leftWidth).FillHeight().PaddingX(16).PaddingY(24).Background(studioWhite).Border(1, studioLine).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					studioText(c, "ELEMENTI").FontSize(9).FontWeight(650).TextColor(studioInk)
					ui.Row(c).FillWidth().Gap(12).PaddingY(12).Children(func() {
						for _, kind := range []string{"text", "table", "qr", "image"} {
							label := map[string]string{"text": "Tekst", "table": "Tabela", "qr": "QR", "image": "Slika"}[kind]
							b := studioIconButton(c, label, kind).Size(28, 32).Background(studioBG).Border(0, studioLine).Drag(nativeDrag{Kind: kind})
							if b.Clicked() {
								e.drop(nativeDrag{Kind: kind}, "")
							}
						}
					})
					studioText(c, "RASPORED").FontSize(9).FontWeight(650).PaddingY(16)
					for _, v := range e.doc.Elements() {
						el := v.(map[string]any)
						id := textValue(el["id"])
						kind := textValue(el["type"])
						fullName := e.doc.ElementName(el)
						label := strings.Join(strings.Fields(fullName), " ")
						runes := []rune(label)
						if len(runes) > 23 {
							label = string(runes[:22]) + "…"
						}
						b := ui.ButtonBase(c.Key(id)).Label(tr(c, "Sloj "+id)).Tooltip(fullName).FillWidth().Height(36).PaddingX(8).Gap(8).Radius(7).Justify(ui.Start).Drag(nativeDrag{ID: id})
						if e.doc.Selected == id {
							b.Background(ui.RGB(238, 245, 255))
						} else if b.Hovered() {
							b.Background(studioBG)
						}
						b.Children(func() {
							if kind == "qr" {
								studioText(c, "QR").FontSize(10)
							} else {
								ui.Icon(c, studioIcons[kind]).Size(15, 15).TextColor(studioMuted)
							}
							ui.Text(c, label).FontSize(11).SingleLine()
						})
						if b.Clicked() {
							e.doc.Selected = id
						}
						if item, over := ui.DragOver[nativeDrag](b); over && item.ID != id {
							b.DrawOver(drawNativeDropLine)
						}
						if item, ok := ui.Drop[nativeDrag](b); ok {
							e.drop(item, id)
						}
					}
					end := ui.Box(c).Label(tr(c, "Kraj rasporeda")).Height(35).FillWidth()
					if _, over := ui.DragOver[nativeDrag](end); over {
						end.DrawOver(drawNativeDropLine)
					}
					if item, ok := ui.Drop[nativeDrag](end); ok {
						e.drop(item, "")
					}
				})
			})

			ui.Column(c).Width(0).Grow(1).MinWidth(0).FillHeight().Children(func() {
				e.toolbar(c)
				if e.error != "" {
					studioText(c, e.error).FillWidth().Padding(12).TextColor(ui.RGB(190, 55, 45))
				}
				e.centerContent(c)
			})
			ui.Scroll(c).Width(rightWidth).FillHeight().PaddingX(16).PaddingY(24).Border(1, studioLine).Background(studioWhite).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					ui.Column(c).Key("printer-profile").FillWidth().Gap(9).Children(func() {
						header := ui.ButtonBase(c.Key("toggle")).Label(tr(c, "Profil printera")).Expanded(e.profileExpanded).FillWidth().Height(30).Justify(ui.Start)
						header.Children(func() {
							studioText(c, "PROFIL PRINTERA").FontSize(9).FontWeight(650).Grow(1)
							chevron := "›"
							if e.profileExpanded {
								chevron = "⌄"
							}
							studioText(c, chevron).FontSize(16).TextColor(studioMuted)
						})
						if header.Clicked() {
							e.profileExpanded = !e.profileExpanded
						}
						if e.profileExpanded {
							ui.Column(c).Key("profile-fields").FillWidth().Gap(9).Children(func() { e.profileProperties(c) })
						}
					})
					ui.Divider(c)
					ui.Column(c).Key("properties-" + e.doc.Selected).FillWidth().Gap(9).Children(func() {
						studioText(c, "SVOJSTVA").FontSize(9).FontWeight(650).PaddingY(4)
						e.properties(c)
					})
				})
			})
		})
	})
	if closeRequested {
		e.onClose()
	}
}

func (e *nativeEditor) toolbar(c *ui.Context) {
	ui.Row(c).FillWidth().Height(59).Shrink(0).PaddingX(22).Gap(12).AlignItems(ui.Center).Background(ui.RGB(251, 252, 252)).Border(1, studioLine).Children(func() {
		ui.Row(c).Height(58).Gap(22).Children(func() {
			for _, tab := range []string{"Dizajn", "Podaci", "Komande"} {
				b := ui.ButtonBase(c.Key(tab)).Label(tr(c, tab)).Height(58).PaddingX(1)
				color := studioMuted
				if e.tab == tab {
					color = studioBlue
					b.DrawOver(func(p *ui.Painter, r ui.Rect) { p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 2, W: r.W, H: 2}, studioBlue, 0) })
				}
				b.Children(func() { studioText(c, tab).FontSize(12).TextColor(color) })
				if b.Clicked() {
					e.tab = tab
				}
			}
		})
		ui.Box(c).Width(0).Grow(1)
		if studioIconButton(c, "Poništi", "undo").Size(28, 32).Border(0, studioLine).Disabled(!e.doc.CanUndo()).Clicked() {
			e.doc.Undo()
		}
		if studioIconButton(c, "Vrati", "redo").Size(28, 32).Border(0, studioLine).Disabled(!e.doc.CanRedo()).Clicked() {
			e.doc.Redo()
		}
		value := fmt.Sprintf("%.0f%%", e.zoom*100)
		input := ui.TextInput(c, &value).Label(tr(c, "Zoom")).Width(68).Height(32).PaddingX(4).TextAlign(ui.Center).FontSize(11)
		input.OnTextKey(func(k ui.TextKey) bool {
			if k.Mods != 0 || (k.Key != ui.KeyUp && k.Key != ui.KeyDown) {
				return false
			}
			v, err := strconv.ParseFloat(strings.TrimSuffix(k.Text(), "%"), 64)
			if err != nil {
				v = e.zoom * 100
			}
			delta := 10.
			if k.Key == ui.KeyDown {
				delta = -10
			}
			e.zoom = math.Max(30, math.Min(200, v+delta)) / 100
			k.SetText(fmt.Sprintf("%.0f%%", e.zoom*100))
			return true
		})
		if input.Changed() {
			if n, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64); err == nil && n >= 30 && n <= 200 {
				e.zoom = math.Round(n) / 100
			}
		}
	})
}

func (e *nativeEditor) centerContent(c *ui.Context) {
	if e.tab == "Podaci" {
		ui.Column(c).FillWidth().Height(0).Grow(1).MinHeight(0).Padding(24).Gap(12).Children(func() {
			studioText(c, "Primjer JSON podataka")
			studioTextArea(c.Key("sample-data"), &e.dataText).Font("monospace").FontSize(12).FillWidth().Height(0).Grow(1).MinHeight(0).Label(tr(c, "Podaci računa"))
			if studioButton(c, "Primijeni podatke", true).Clicked() {
				if err := e.doc.SetData(e.dataText); err != nil {
					e.error = err.Error()
				} else {
					e.cache = ""
				}
			}
		})
		return
	}
	if e.tab == "Komande" {
		ui.Column(c).FillWidth().Height(0).Grow(1).MinHeight(0).Padding(24).Gap(12).Children(func() {
			v := hex.Dump(e.bytes)
			studioText(c, fmt.Sprintf("%d bajtova · ReceiptPrinterEncoder 4.0.1", len(e.bytes)))
			studioTextArea(c.Key("command-bytes"), &v).ReadOnly(true).Font("monospace").FontSize(12).FillWidth().Height(0).Grow(1).MinHeight(0).Label(tr(c, "ESC/POS komande"))
			if studioButton(c, "Izvezi komande", false).Clicked() {
				go e.exportBytes(append([]byte(nil), e.bytes...), e.language)
			}
		})
		return
	}
	ui.ScrollBoth(c).FillWidth().Height(0).Grow(1).MinHeight(0).MinWidth(0).Background(ui.RGB(242, 242, 245)).Draw(func(p *ui.Painter, r ui.Rect) {
		for x := r.X + 8; x < r.X+r.W; x += 16 {
			for y := r.Y + 8; y < r.Y+r.H; y += 16 {
				p.Fill(ui.Rect{X: x, Y: y, W: 1, H: 1}, ui.RGB(216, 216, 224), .5)
			}
		}
	}).Children(func() {
		paperWidth := float32(receipt.PaperWidth(int(e.plan.Width))) * float32(e.zoom)
		ui.Column(c).Width(max(nativeCanvasWidth(c), paperWidth+48)).PaddingX(24).PaddingY(40).Gap(22).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Width(paperWidth).Children(func() {
				studioText(c, "PREVIEW RAČUNA").FontSize(9).TextColor(studioMuted).Grow(1)
				paper := "80 mm"
				if e.doc.Template["profile"] == "narrow" {
					paper = "58 mm"
				}
				studioText(c, fmt.Sprintf("%s / %g dots", paper, e.plan.Width)).FontSize(9).TextColor(studioMuted)
			})
			e.canvas(c)
		})
	})

}

func nativeEditorSides(c *ui.Context) (float32, float32) {
	width, _ := c.Size()
	if width <= 1000 {
		return 170, 230
	}
	return 210, 260
}

func nativeCanvasWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	left, right := nativeEditorSides(c)
	return width - left - right
}

func drawNativeDropLine(p *ui.Painter, r ui.Rect) {
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 3}, studioBlue, 1)
}

func (e *nativeEditor) canvas(c *ui.Context) {
	if e.preview == nil {
		studioText(c, "Ispravi podatke za pregled računa.")
		return
	}
	z := float32(e.zoom)
	paperWidth := float32(receipt.PaperWidth(int(e.plan.Width)))
	margin := (paperWidth - float32(e.plan.Width)) / 2
	ui.Box(c).Key("paper").Size(paperWidth*z, float32(e.plan.Height)*z).Background(studioWhite).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, .10)).Draw(func(p *ui.Painter, r ui.Rect) {
		for _, tile := range e.preview.Tiles {
			p.Image(tile.SVG, ui.Rect{X: r.X, Y: r.Y + tile.Y*z, W: r.W, H: tile.Height * z}, ui.Contain)
		}
	}).Children(func() {
		dropY := float32(-1)
		contentEnd := float32(0)
		for _, block := range e.plan.Blocks {
			b := block
			contentEnd = float32(b.Y + b.Height)
			id := b.ID
			box := ui.Box(c).Key(id).Label(tr(c, "Element "+id)).Absolute().Left((margin+float32(b.X))*z).Top(float32(b.Y)*z).Size(float32(b.Width)*z, max(18, float32(b.Height)*z)).Focusable().Drag(nativeDrag{ID: id})
			if e.doc.Selected == id {
				box.Border(1, ui.RGB(0, 122, 255))
			}
			if box.Clicked() {
				e.doc.Selected = id
			}
			if value, over := ui.DragOver[nativeDrag](box); over && value.ID != id {
				dropY = float32(b.Y)
			}
			if value, ok := ui.Drop[nativeDrag](box); ok {
				e.drop(value, id)
			}
			if e.doc.Selected == id {
				box.Children(func() {
					handle := ui.Box(c).Key("resize").Label(tr(c, "Promijeni širinu "+id)).Absolute().Right(-4).Top(4).Size(9, 18).Background(ui.RGB(0, 122, 255))
					dx, _, held := handle.Dragged()
					if held {
						if e.resizeID == "" {
							el := e.doc.Element(id)
							e.resizeID, e.resizeWidth, e.resizeX, e.resizeDelta = id, numberValue(el["width"]), numberValue(el["x"]), 0
							e.doc.BeginGesture()
						}
						e.resizeDelta += float64(dx)
						columns := e.plan.Width / 12
						start := math.Min(math.Floor(columns*.9), math.Round(columns*e.resizeX/100))
						span := math.Round((e.resizeWidth + e.resizeDelta/float64(z)/e.plan.Width*100) * columns / 100)
						span = math.Max(math.Ceil(columns*.1), math.Min(columns-start, span))
						if e.resizeDelta != 0 {
							e.doc.Geometry(id, math.Round(start/columns*100), math.Round(span/columns*100))
						}
					} else if e.resizeID == id {
						e.doc.EndGesture(handle.PressCanceled())
						e.resizeID = ""
					}
				})
			}
		}
		tail := ui.Box(c).Key("end").Label(tr(c, "Kraj računa")).Absolute().Left(0).Top(contentEnd*z).Size(paperWidth*z, max(1, (float32(e.plan.Height)-contentEnd)*z))
		if _, over := ui.DragOver[nativeDrag](tail); over {
			dropY = contentEnd
		}
		if value, ok := ui.Drop[nativeDrag](tail); ok {
			e.drop(value, "")
		}
		if dropY >= 0 {
			ui.Box(c).Key("drop-marker").Label(tr(c, "Mjesto umetanja")).Absolute().Left(margin*z).Top(max(0, dropY*z-1)).Size(float32(e.plan.Width)*z, 3).Background(studioBlue).PassThrough()
		}
	})
}

func (e *nativeEditor) properties(c *ui.Context) {
	d := e.doc
	el := d.Element(d.Selected)
	if el == nil {
		studioText(c, "Odaberi element na računu ili dodaj novi.").FontSize(11).TextColor(studioMuted).FillWidth()
		return
	}
	kind := textValue(el["type"])
	title := map[string]string{"text": "Tekstualni blok", "table": "Tabela stavki", "qr": "QR kod", "image": "Slika / logo"}[kind]
	studioText(c, title).FontSize(18).FontWeight(600).PaddingY(8)
	studioLabel(c, "Naziv elementa")
	name := d.ElementName(el)
	if ui.TextInput(c.Key("element-name"), &name).Label(tr(c, "Uredi naziv elementa")).FillWidth().Height(34).Changed() {
		d.Set(d.Selected, "name", name)
	}
	if kind == "text" {
		e.field(c, "Tekst", "text", el, true)
	}
	if kind == "qr" {
		e.field(c, "Sadržaj QR koda", "content", el, true)
		e.number(c, "Veličina modula", "moduleSize", el, 1, 8, 1)
		e.choice(c, "Korekcija greške", "errorLevel", el, []string{"l", "m", "q", "h"})
		e.choice(c, "QR režim", "qrMode", el, []string{"native", "image"})
	}
	if kind == "image" {
		if studioButton(c, "Učitaj logo", false).Clicked() {
			go e.importImage(d.Selected, e.language)
		}
		e.number(c, "Širina slike (dots)", "imageWidth", el, 8, 576, 1)
		e.number(c, "Prag crne boje", "threshold", el, 1, 255, 1)
	}
	if kind != "table" {
		studioLabel(c, "Poravnanje")
		align := textValue(el["align"])
		if studioAlignment(c, "Poravnanje", &align) {
			d.Set(d.Selected, "align", align)
		}
	}
	studioLabel(c, "Veličina fonta")
	ui.Row(c).FillWidth().Gap(5).Children(func() {
		for _, scale := range []int{1, 2} {
			name := fmt.Sprintf("%d×", scale)
			if studioOption(c.Key(scale), "Veličina fonta · "+name, name, numberValue(el["scale"]) == float64(scale)).Clicked() {
				d.Set(d.Selected, "scale", scale)
			}
		}
	})
	studioLabel(c, "Formatiranje")
	ui.Row(c).FillWidth().Gap(5).Children(func() {
		for i, key := range []string{"bold", "underline"} {
			on, _ := el[key].(bool)
			if studioOption(c.Key(key), []string{"Podebljano", "Podvučeno"}[i], []string{"B", "U"}[i], on).Clicked() {
				d.Set(d.Selected, key, !on)
			}
		}
	})
	e.number(c, "Razmak ispod (dots)", "gap", el, 0, 120, 1)
	x, w := numberValue(el["x"]), numberValue(el["width"])
	studioLabel(c, "X (%)")
	if studioNumber(c, &x, 0, 90, 1).Label(tr(c, "X (%)")).FillWidth().Height(34).Changed() {
		d.Geometry(d.Selected, x, w)
	}
	studioLabel(c, "Širina (%)")
	if studioNumber(c, &w, 10, 100-x, 1).Label(tr(c, "Širina (%)")).FillWidth().Height(34).Changed() {
		d.Geometry(d.Selected, x, w)
	}
	if kind == "table" {
		e.field(c, "Izvor stavki", "source", el, false)
		columns, _ := el["columns"].([]any)
		for i, v := range columns {
			col := v.(map[string]any)
			ui.Column(c).Key(i).FillWidth().Padding(10).Gap(7).Radius(8).Border(1, studioLine).Children(func() {
				studioText(c, fmt.Sprintf("KOLONA %d", i+1)).FontSize(9).TextColor(studioMuted)
				for _, key := range []string{"label", "key", "align"} {
					value := textValue(col[key])
					studioLabel(c, map[string]string{"label": "Naslov", "key": "Polje", "align": "Poravnanje"}[key])
					var input ui.Element
					if key == "align" {
						if studioAlignment(c, fmt.Sprintf("Kolona %d poravnanje", i+1), &value) {
							d.Change(func() { col[key] = value })
						}
						continue
					} else {
						input = ui.TextInput(c.Key(key), &value)
					}
					input.Label(tr(c, fmt.Sprintf("Kolona %d %s", i+1, key))).FillWidth().Height(32)
					if input.Changed() {
						d.Change(func() { col[key] = value })
					}
				}
				studioLabel(c, "Udio (%)")
				width := numberValue(col["width"])
				if studioNumber(c, &width, 5, 100, 1).Label(tr(c, fmt.Sprintf("Kolona %d širina", i+1))).Changed() {
					d.Change(func() { col["width"] = width })
				}
				if len(columns) > 1 && studioButton(c, "Ukloni kolonu", false).Clicked() {
					d.Change(func() {
						cols := append(columns[:i:i], columns[i+1:]...)
						normalizeNativeColumns(cols)
						el["columns"] = cols
					})
				}
			})
		}
		if len(columns) < 6 && studioButton(c, "Dodaj kolonu", false).Clicked() {
			d.Change(func() {
				cols := append(columns, map[string]any{"label": "Kolona", "key": "name", "align": "left", "width": float64(25)})
				normalizeNativeColumns(cols)
				el["columns"] = cols
			})
		}
		studioText(c, "Širine kolona ukupno: 100%.").FontSize(11)
	}
	deleteButton := studioButton(c, "Obriši element", true)
	deleteColor := ui.RGB(220, 38, 38)
	if deleteButton.Hovered() {
		deleteColor = ui.RGB(185, 28, 28)
	}
	if deleteButton.Background(deleteColor).Border(1, deleteColor).Clicked() {
		d.Delete()
	}
}

func (e *nativeEditor) profileProperties(c *ui.Context) {
	d := e.doc
	e.choice(c, "Protokol printera", "language", d.Template, []string{"esc-pos", "star-prnt", "star-line"})
	e.choice(c, "Papir / područje za printanje", "profile", d.Template, []string{"wide", "narrow"})
	e.number(c, "CP852 broj stranice", "codePage", d.Template, 0, 255, 1)
	e.check(c, "Rez papira nakon računa", "cut", d.Template)
}

func normalizeNativeColumns(cols []any) {
	for i, v := range cols {
		w := mathFloor(100 / float64(len(cols)))
		if i == len(cols)-1 {
			w = 100 - w*float64(i)
		}
		v.(map[string]any)["width"] = w
	}
}

func mathFloor(v float64) float64 { return float64(int(v)) }

func (e *nativeEditor) exportBytes(data []byte, language string) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: i18n.Translate(language, "Izvezi komande"), DefaultPath: "racun.bin"})
	if err == nil && path != "" {
		_ = os.WriteFile(path, data, 0600)
	}
}

func (e *nativeEditor) importImage(id string, language string) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: i18n.Translate(language, "Učitaj logo"), Filters: []mygo.FileFilter{{Name: i18n.Translate(language, "Slike"), Extensions: []string{"png", "jpg", "jpeg"}}}})
	if err != nil || len(paths) == 0 {
		return
	}
	f, err := os.Open(paths[0])
	if err != nil {
		return
	}
	defer f.Close()
	im, _, err := image.Decode(f)
	if err != nil {
		return
	}
	bounds := im.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	scale := min(1.0, 256/float64(max(w, h)))
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	var gray strings.Builder
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			r, g, b, a := im.At(bounds.Min.X+x*w/nw, bounds.Min.Y+y*h/nh).RGBA()
			lum := (299*r+587*g+114*b)/1000 + (65535 - a)
			fmt.Fprintf(&gray, "%02x", min(255, lum>>8))
		}
	}
	e.post(func() { e.doc.Set(id, "image", map[string]any{"width": nw, "height": nh, "gray": gray.String()}) })
}
