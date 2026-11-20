//go:build desktop && (darwin || windows)

package desktop

import (
	"fmt"
	"html"
	"strings"

	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/receipt"
)

// Paint the command-derived RPE plan as vectors, like the browser editor.
// Each character has an explicit printer-cell X coordinate: SVG font metrics
// cannot move a price or change wrapping. PNG encoding is only for API/archive.
type nativeReceiptTile struct {
	SVG       *ui.SVG
	Y, Height float32
}

type nativeReceiptPreview struct{ Tiles []nativeReceiptTile }

func nativeReceiptSVG(plan receipt.NativePlan, doc *receipt.NativeDocument) (*nativeReceiptPreview, error) {
	preview := &nativeReceiptPreview{}
	// Small SVG tiles avoid MyGo's 4096-pixel image limit and only visible tiles
	// are painted. Use Image, not the icon mask atlas: text SVGs reenter the font
	// shaper and cannot safely use its locked mask rasterization callback.
	for y := 0.; y < plan.Height; y += 512 {
		bottom := min(y+512, plan.Height)
		tile, err := nativeReceiptTileSVG(plan, doc, y, bottom)
		if err != nil {
			return nil, err
		}
		preview.Tiles = append(preview.Tiles, nativeReceiptTile{tile, float32(y), float32(bottom - y)})
	}
	return preview, nil
}

func nativeReceiptTileSVG(plan receipt.NativePlan, doc *receipt.NativeDocument, top, bottom float64) (*ui.SVG, error) {
	var out strings.Builder
	width := receipt.PaperWidth(int(plan.Width))
	margin := float64(width-int(plan.Width)) / 2
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 %g %d %g" xml:space="preserve"><g fill="#26352e" font-family="Courier New,monospace">`, top, width, bottom-top)
	for _, block := range plan.Blocks {
		for _, line := range block.Lines {
			if float64(line.Y+line.Height) < top || float64(line.Y) >= bottom {
				continue
			}
			for _, run := range line.Runs {
				if run.Text == "" {
					continue
				}
				weight := 400
				if run.Bold {
					weight = 700
				}
				fmt.Fprint(&out, `<text xml:space="preserve" x="`)
				var visible strings.Builder
				for i, r := range []rune(run.Text) {
					// Position visible glyphs directly; SVG whitespace normalization
					// must never collapse the padding between receipt columns.
					if r == ' ' {
						continue
					}
					visible.WriteRune(r)
					fmt.Fprintf(&out, "%g ", margin+float64(run.X+i*run.CharWidth))
				}
				fmt.Fprintf(&out, `" y="%g" font-size="%g" font-weight="%d">%s</text>`, float64(line.Y)+float64(run.CharHeight)*.8, float64(run.CharHeight)*.85, weight, html.EscapeString(visible.String()))
				if run.Underline {
					fmt.Fprintf(&out, `<path d="M%g %d h%d" stroke="#26352e" stroke-width="1"/>`, margin+float64(run.X), line.Y+run.CharHeight-2, run.Width)
				}
			}
		}
		if el := doc.Element(block.ID); el != nil && el["type"] == "qr" && len(block.Graphics) > 0 {
			if block.Y+block.Height < top || block.Y >= bottom {
				continue
			}
			// The editor intentionally uses the same generic QR placeholder as web.
			left, top, right, bottom := 1<<30, 1<<30, 0, 0
			for _, g := range block.Graphics {
				left = min(left, g.X)
				top = min(top, g.Y)
				right = max(right, g.X+g.Width)
				bottom = max(bottom, g.Y+g.Height)
			}
			fmt.Fprintf(&out, `<g transform="translate(%g %d) scale(%g %g)"><rect width="23" height="23" fill="white" stroke="#b8c8c0" stroke-width=".3"/>`, margin+float64(left), top, float64(right-left)/23, float64(bottom-top)/23)
			for _, p := range [][2]int{{1, 1}, {15, 1}, {1, 15}} {
				fmt.Fprintf(&out, `<rect x="%d" y="%d" width="7" height="7"/><rect x="%d" y="%d" width="5" height="5" fill="white"/><rect x="%d" y="%d" width="3" height="3"/>`, p[0], p[1], p[0]+1, p[1]+1, p[0]+2, p[1]+2)
			}
			fmt.Fprint(&out, `<text x="15" y="16" text-anchor="middle" font-family="sans-serif" font-size="5" font-weight="700">QR</text></g>`)
			continue
		}
		for _, g := range block.Graphics {
			if float64(g.Y+g.Height) < top || float64(g.Y) >= bottom {
				continue
			}
			stride := (g.Width + 7) / 8
			if g.Width <= 0 || g.Height <= 0 || len(g.Data) != stride*g.Height {
				return nil, fmt.Errorf("Neispravna grafika elementa %s", block.ID)
			}
			fmt.Fprint(&out, `<path fill="#000" d="`)
			for y := 0; y < g.Height; y++ {
				for x := 0; x < g.Width; {
					if g.Data[y*stride+x/8]&(128>>uint(x%8)) == 0 {
						x++
						continue
					}
					start := x
					for x < g.Width && g.Data[y*stride+x/8]&(128>>uint(x%8)) != 0 {
						x++
					}
					fmt.Fprintf(&out, "M%g %d h%d v1 h-%d z", margin+float64(g.X+start), g.Y+y, x-start, x-start)
				}
			}
			fmt.Fprint(&out, `"/>`)
		}
	}
	fmt.Fprint(&out, `</g></svg>`)
	return ui.ParseSVG([]byte(out.String()))
}
