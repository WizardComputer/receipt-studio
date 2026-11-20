package receipt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/draw"
	"image/png"
	"unicode/utf8"

	imagedraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"receiptstudio.local/agent/internal/model"
)

type receiptRun struct {
	Text                            string
	X, Width, CharWidth, CharHeight int
	Bold, Underline                 bool
}

type receiptLine struct {
	Y, Height int
	Runs      []receiptRun
}

type receiptGraphic struct {
	X, Y, Width, Height int
	Data                []byte
}

type receiptDrawing struct {
	Width, Height int
	Lines         []receiptLine
	Graphics      []receiptGraphic
}

func (e *Encoder) RenderPNG(template json.RawMessage, commands []byte) (*model.ReceiptPNG, error) {
	// Decode the immutable bytes sent to the printer, not the current template or
	// the editor's generic QR placeholder. Only profile metadata comes from JSON.
	result, err := e.runWithBytes(`JSON.stringify(new EscPosPreview(profiles[jobTemplate.profile], jobTemplate.codePage, jobTemplate.language || 'esc-pos', {commands:false}).read(new Uint8Array(jobBytes)).block)`, map[string]json.RawMessage{"jobTemplate": template}, map[string][]byte{"jobBytes": commands})
	if err != nil {
		return nil, err
	}
	var drawing receiptDrawing
	if err := json.Unmarshal([]byte(result.String()), &drawing); err != nil {
		return nil, err
	}
	return renderReceiptPNG(drawing)
}

const receiptPNGScale = 1

const receiptPNGRenderScale = 3

const receiptPNGBandHeight = 512

// Catmull-Rom needs two output pixels of support at a 3:1 reduction. Four
// pixels of overlap keep that support strictly inside every internal edge.
const receiptPNGBandHalo = 4

const receiptPNGMaxPixels = 64000000

func validReceiptPNGSize(width, height int) bool {
	return (width == 384 || width == 576) && height > 0 && height <= 100000 &&
		int64(PaperWidth(width))*int64(height) <= receiptPNGMaxPixels
}

func PaperWidth(printWidth int) int {
	if printWidth == 384 {
		return 464
	}
	return 640
}

func renderReceiptPNG(d receiptDrawing) (*model.ReceiptPNG, error) {
	if !validReceiptPNGSize(d.Width, d.Height) {
		return nil, fmt.Errorf("Račun je prevelik za PNG (najviše 64 miliona izlaznih piksela).")
	}
	// Validate every run, including text clipped outside the visible receipt.
	for _, line := range d.Lines {
		for _, run := range line.Runs {
			if run.CharWidth < 1 || run.CharWidth > 96 || run.CharHeight < 1 || run.CharHeight > 192 || run.Width != utf8.RuneCountInString(run.Text)*run.CharWidth {
				return nil, fmt.Errorf("Neispravna geometrija teksta za PNG.")
			}
		}
	}
	normalFont, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	boldFont, err := opentype.Parse(gomonobold.TTF)
	if err != nil {
		return nil, err
	}
	// Render outlines at their final resolution. Never enlarge a low-resolution
	// thresholded glyph. Separate X/Y scaling still follows printer dot geometry.
	type faceKey struct {
		height int
		bold   bool
	}
	faces := map[faceKey]font.Face{}
	defer func() {
		for _, face := range faces {
			_ = face.Close()
		}
	}()
	type glyphKey struct {
		rune          rune
		width, height int
		bold          bool
	}
	glyphs := map[glyphKey]*image.Gray{}
	// Reuse one supersampled strip and one scaler (including its pooled filter
	// workspace) for the whole receipt. Shift the last strip upward rather than
	// allocating a differently sized scaler. The output stays at final resolution.
	bandHeight := min(d.Height, receiptPNGBandHeight+2*receiptPNGBandHalo)
	band := image.NewGray(image.Rect(0, 0, d.Width*receiptPNGRenderScale, bandHeight*receiptPNGRenderScale))
	scaler := imagedraw.CatmullRom.NewScaler(d.Width, bandHeight, band.Bounds().Dx(), band.Bounds().Dy())
	drawText := func(top int) error {
		for i := range band.Pix {
			band.Pix[i] = 255
		}
		for _, line := range d.Lines {
			for _, run := range line.Runs {
				if line.Y >= top+bandHeight || line.Y+run.CharHeight <= top {
					continue
				}
				x := run.X
				for _, r := range run.Text {
					key := glyphKey{r, run.CharWidth, run.CharHeight, run.Bold}
					glyph, ok := glyphs[key]
					if !ok {
						fk := faceKey{run.CharHeight, run.Bold}
						face, exists := faces[fk]
						if !exists {
							f := normalFont
							if run.Bold {
								f = boldFont
							}
							face, err = opentype.NewFace(f, &opentype.FaceOptions{Size: 20 * float64(run.CharHeight) / 24 * receiptPNGRenderScale, DPI: 72, Hinting: font.HintingNone})
							if err != nil {
								return err
							}
							faces[fk] = face
						}
						naturalWidth := run.CharHeight * receiptPNGRenderScale / 2
						natural := image.NewGray(image.Rect(0, 0, naturalWidth, run.CharHeight*receiptPNGRenderScale))
						draw.Draw(natural, natural.Bounds(), image.White, image.Point{}, draw.Src)
						drawer := font.Drawer{Dst: natural, Src: image.Black, Face: face, Dot: fixed.Point26_6{X: 0, Y: fixed.Int26_6(float64(run.CharHeight) * 19 / 24 * receiptPNGRenderScale * 64)}}
						drawer.DrawString(string(r))
						glyph = natural
						if naturalWidth != run.CharWidth*receiptPNGRenderScale {
							glyph = image.NewGray(image.Rect(0, 0, run.CharWidth*receiptPNGRenderScale, run.CharHeight*receiptPNGRenderScale))
							imagedraw.CatmullRom.Scale(glyph, glyph.Bounds(), natural, natural.Bounds(), draw.Src, nil)
						}
						glyphs[key] = glyph
					}
					draw.Draw(band, image.Rect(x*receiptPNGRenderScale, (line.Y-top)*receiptPNGRenderScale, (x+run.CharWidth)*receiptPNGRenderScale, (line.Y+run.CharHeight-top)*receiptPNGRenderScale), glyph, image.Point{}, draw.Src)
					x += run.CharWidth
				}
			}
		}
		return nil
	}

	// Downsample text only. Clip each write to the band's core, retaining the
	// overlapping source pixels for filtering. Global top/bottom edges therefore
	// use exactly the same filter weights as the original full-image renderer.
	paperWidth := PaperWidth(d.Width)
	margin := (paperWidth - d.Width) / 2
	paper := image.NewGray(image.Rect(0, 0, paperWidth, d.Height))
	draw.Draw(paper, paper.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 0; y < d.Height; y += receiptPNGBandHeight {
		top := min(max(0, y-receiptPNGBandHalo), d.Height-bandHeight)
		if err := drawText(top); err != nil {
			return nil, err
		}
		core := paper.SubImage(image.Rect(margin, y, margin+d.Width, min(y+receiptPNGBandHeight, d.Height))).(*image.Gray)
		scaler.Scale(core, image.Rect(margin, top, margin+d.Width, top+bandHeight), band, band.Bounds(), draw.Src, nil)
	}
	// Underlines, QR and logos remain sharp at native dot resolution.

	for _, line := range d.Lines {
		for _, run := range line.Runs {
			if !run.Underline {
				continue
			}
			thickness := run.CharHeight / 24
			if thickness < 1 {
				thickness = 1
			}
			draw.Draw(paper, image.Rect(margin+run.X, line.Y+run.CharHeight-thickness, margin+run.X+run.Width, line.Y+run.CharHeight), image.Black, image.Point{}, draw.Src)
		}
	}
	for _, g := range d.Graphics {
		stride := (g.Width + 7) / 8
		if g.Width < 1 || g.Width > d.Width || g.Height < 1 || g.Height > 768 || len(g.Data) != stride*g.Height {
			return nil, fmt.Errorf("Neispravna grafika za PNG.")
		}
		for y := 0; y < g.Height; y++ {
			for x := 0; x < g.Width; x++ {
				if g.Data[y*stride+x/8]&(128>>uint(x%8)) != 0 {
					draw.Draw(paper, image.Rect(margin+g.X+x, g.Y+y, margin+g.X+x+1, g.Y+y+1), image.Black, image.Point{}, draw.Src)
				}
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, paper); err != nil {
		return nil, err
	}
	// PNG pHYs records 8000 pixels/metre (8 dots/mm): 640 px = 80 mm.
	encoded := output.Bytes()
	chunk := make([]byte, 21)
	binary.BigEndian.PutUint32(chunk[0:4], 9)
	copy(chunk[4:8], "pHYs")
	binary.BigEndian.PutUint32(chunk[8:12], 8000)
	binary.BigEndian.PutUint32(chunk[12:16], 8000)
	chunk[16] = 1
	binary.BigEndian.PutUint32(chunk[17:21], crc32.ChecksumIEEE(chunk[4:17]))
	// Insert after the mandatory IHDR chunk, before IDAT.
	data := make([]byte, 0, len(encoded)+len(chunk))
	data = append(data, encoded[:33]...)
	data = append(data, chunk...)
	data = append(data, encoded[33:]...)
	return &model.ReceiptPNG{ContentType: "image/png", Encoding: "base64", Width: paperWidth, Height: d.Height, DPI: 203.2, Scale: 1, PaperWidthMM: paperWidth / 8, MarginLeft: margin, MarginRight: paperWidth - d.Width - margin, Data: data}, nil
}
