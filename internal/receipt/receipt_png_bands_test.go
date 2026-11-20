package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

// Deliberately fixed at the original 512-dot proposal, independent of the
// implementation constant. Exercise both sides of seams and receipt edges.

func pngPixelCases(t *testing.T) map[string]*model.ReceiptPNG {
	t.Helper()
	result := map[string]*model.ReceiptPNG{}
	for _, width := range []int{384, 576} {
		for _, height := range []int{1, 511, 512, 513, 1024, 1025, 1601} {
			d := receiptDrawing{Width: width, Height: height}
			for i, y := range []int{-8, 0, 493, 510, 512, 1010, 1022, 1534, height - 17} {
				cw, ch := []int{12, 24, 9}[i%3], []int{24, 48, 36}[i%3]
				d.Lines = append(d.Lines, receiptLine{Y: y, Runs: []receiptRun{
					{Text: "ČćŽšđ Ag_", X: -2 + i*3, Width: 9 * cw, CharWidth: cw, CharHeight: ch, Bold: i%2 == 0, Underline: i%3 == 0},
					{Text: "x O!", X: width - 24, Width: 4 * 12, CharWidth: 12, CharHeight: 24, Underline: true},
				}})
			}
			for _, y := range []int{-4, 502, 1017, height - 9} {
				data := make([]byte, 3*29)
				for i := range data {
					data[i] = byte(i*37 + 101)
				}
				d.Graphics = append(d.Graphics, receiptGraphic{X: width/2 - 9, Y: y, Width: 19, Height: 29, Data: data})
			}
			name := fmt.Sprintf("seams-%d-%d", width, height)
			p, err := renderReceiptPNG(d)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			result[name] = p
		}
	}
	e, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/encoder.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Template json.RawMessage
		Hex      string
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		commands, err := hex.DecodeString(f.Hex)
		if err != nil {
			t.Fatal(err)
		}
		p, err := e.RenderPNG(f.Template, commands)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		result["commands-"+f.Name] = p
	}
	return result
}

func pngPixelHash(t *testing.T, p *model.ReceiptPNG) string {
	t.Helper()
	im, err := png.Decode(bytes.NewReader(p.Data))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d:", im.Bounds().Dx(), im.Bounds().Dy())
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < im.Bounds().Dx(); x++ {
			r, g, b, a := im.At(x, y).RGBA()
			h.Write([]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)})
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Frozen hashes compare decoded RGBA pixels with the original full-canvas
// renderer, not file size. x/image/vector's Bezier interpolation uses fused
// multiply-add on ARM64 and separate operations on baseline AMD64. Six text
// fixtures differ by exactly ten pixels, each by 1/255, with identical drawings.
// The unfused hashes were verified against BOTH the original full-canvas renderer
// and this banded renderer on AMD64; disabling vector FMA on ARM64 reproduces them.
// Preserve the original hashes and allow only these exact alternative captures,
// not an arbitrary pixel tolerance or an architecture-wide test skip.
func TestPNGBandsMatchOriginalPixels(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/png-pixels.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]string
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../testdata/png-pixels-unfused.json")
	if err != nil {
		t.Fatal(err)
	}
	var unfused map[string]string
	if err := json.Unmarshal(raw, &unfused); err != nil {
		t.Fatal(err)
	}
	for name, hash := range unfused {
		decoded, err := hex.DecodeString(hash)
		if expected[name] == "" || hash == expected[name] || err != nil || len(decoded) != sha256.Size {
			t.Fatalf("invalid unfused pixel fixture %q", name)
		}
	}
	actual := pngPixelCases(t)
	if len(actual) != len(expected) {
		t.Fatal("pixel fixture set changed")
	}
	for name, p := range actual {
		if hash := pngPixelHash(t, p); hash != expected[name] && hash != unfused[name] {
			t.Errorf("%s: changed pixels, got %s want %s (unfused alternative: %s)", name, hash, expected[name], unfused[name])
		}
	}
}

func TestPNGSizeBounds(t *testing.T) {
	// Test boundary validation without allocating the largest possible image.
	for _, c := range []struct {
		w, h  int
		valid bool
	}{{576, 100000, true}, {384, 100000, true}, {576, 100001, false}, {576, 0, false}, {576, -1, false}, {575, 500, false}} {
		if validReceiptPNGSize(c.w, c.h) != c.valid {
			t.Fatalf("wrong bounds check for %dx%d", c.w, c.h)
		}
	}
}
