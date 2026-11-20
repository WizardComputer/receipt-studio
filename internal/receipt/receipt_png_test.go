package receipt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image/png"
	"os"
	"testing"
)

func TestPNGContainsActualGraphicsAndTextForEveryProtocol(t *testing.T) {
	e, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/encoder.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name           string
		Template, Data json.RawMessage
		Plan           struct {
			Width, Height int
			Blocks        []receiptDrawing
		}
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if fixture.Name != "graphics" && fixture.Name != "graphics-star-prnt" && fixture.Name != "graphics-star-line" {
			continue
		}
		t.Run(fixture.Name, func(t *testing.T) {
			commands, err := e.Compile(fixture.Template, fixture.Data)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := e.RenderPNG(fixture.Template, commands)
			if err != nil {
				t.Fatal(err)
			}
			im, err := png.Decode(bytes.NewReader(artifact.Data))
			if err != nil {
				t.Fatal(err)
			}
			if im.Bounds().Dx() != PaperWidth(fixture.Plan.Width) || im.Bounds().Dy() != fixture.Plan.Height*receiptPNGScale {
				t.Fatal("PNG dimensions differ from command layout")
			}
			for _, block := range fixture.Plan.Blocks {
				for _, g := range block.Graphics {
					for y := 0; y < g.Height; y++ {
						for x := 0; x < g.Width; x++ {
							expected := g.Data[y*((g.Width+7)/8)+x/8]&(128>>uint(x%8)) != 0
							r, _, _, _ := im.At(artifact.MarginLeft+(g.X+x)*receiptPNGScale, (g.Y+y)*receiptPNGScale).RGBA()
							if (r == 0) != expected {
								t.Fatalf("graphic changed at %d,%d", g.X+x, g.Y+y)
							}
						}
					}
				}
			}
			if destination := os.Getenv("RECEIPT_PNG_TEST_OUTPUT"); fixture.Name == "graphics" && destination != "" {
				if err := os.WriteFile(destination, artifact.Data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPNGBoundsAndTextRendering(t *testing.T) {
	if _, err := renderReceiptPNG(receiptDrawing{Width: 576, Height: 100001}); err == nil {
		t.Fatal("oversize PNG accepted")
	}
	d := receiptDrawing{Width: 384, Height: 100, Lines: []receiptLine{{Y: 10, Runs: []receiptRun{{Text: "Čćžšđ", X: 24, Width: 60, CharWidth: 12, CharHeight: 24, Bold: true, Underline: true}}}}}
	artifact, err := renderReceiptPNG(d)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(artifact.Data))
	if err != nil {
		t.Fatal(err)
	}
	for x := 24; x < 84; x++ {
		r, _, _, _ := im.At(artifact.MarginLeft+x*receiptPNGScale, 33*receiptPNGScale).RGBA()
		if r != 0 {
			t.Fatal("underline not rendered in its dot position")
		}
	}
	ink := 0
	for y := 10; y < 33; y++ {
		for x := 24; x < 84; x++ {
			r, _, _, _ := im.At(artifact.MarginLeft+x*receiptPNGScale, y*receiptPNGScale).RGBA()
			if r == 0 {
				ink++
			}
		}
	}
	if ink < 30 {
		t.Fatal("text glyphs missing")
	}
}

func TestPNGTextHasAntialiasedEdgesAtFinalResolution(t *testing.T) {
	for _, scale := range []int{1, 2} {
		d := receiptDrawing{Width: 576, Height: 100, Lines: []receiptLine{{Y: 0, Runs: []receiptRun{{Text: "ČaO", X: 0, Width: 36 * scale, CharWidth: 12 * scale, CharHeight: 24 * scale}}}}}
		artifact, err := renderReceiptPNG(d)
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(artifact.Data))
		if err != nil {
			t.Fatal(err)
		}
		intermediate := 0
		for y := 0; y < 24*scale*receiptPNGScale; y++ {
			for x := 0; x < 36*scale*receiptPNGScale; x++ {
				r, _, _, _ := im.At(artifact.MarginLeft+x, y).RGBA()
				if r > 0 && r < 65535 {
					intermediate++
				}
			}
		}
		if intermediate < 30 {
			t.Fatalf("text scale %d has no smooth edges: %d", scale, intermediate)
		}
	}
}

func TestPNGPaperMarginsAndPhysicalDimensions(t *testing.T) {
	for _, width := range []int{384, 576} {
		artifact, err := renderReceiptPNG(receiptDrawing{Width: width, Height: 20, Graphics: []receiptGraphic{{X: 0, Y: 0, Width: 8, Height: 1, Data: []byte{255}}}})
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(artifact.Data))
		if err != nil {
			t.Fatal(err)
		}
		if artifact.Width != PaperWidth(width) || artifact.MarginLeft != (artifact.Width-width)/2 || artifact.PaperWidthMM*8 != artifact.Width {
			t.Fatal("wrong physical geometry")
		}
		for y := 0; y < artifact.Height; y++ {
			for x := 0; x < artifact.Width; x++ {
				if x < artifact.MarginLeft || x >= artifact.Width-artifact.MarginRight {
					r, _, _, _ := im.At(x, y).RGBA()
					if r != 65535 {
						t.Fatal("nonwhite paper margin")
					}
				}
			}
		}
		chunk := artifact.Data[33:54]
		if string(chunk[4:8]) != "pHYs" || binary.BigEndian.Uint32(chunk[8:12]) != 8000 || binary.BigEndian.Uint32(chunk[12:16]) != 8000 || chunk[16] != 1 {
			t.Fatal("missing physical PNG density")
		}
		if binary.BigEndian.Uint32(chunk[17:21]) != crc32.ChecksumIEEE(chunk[4:17]) {
			t.Fatal("invalid density CRC")
		}
	}
}
