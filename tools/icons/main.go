package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

func save(path string, b []byte) {
	if err := os.WriteFile(path, b, 0644); err != nil {
		panic(err)
	}
}

// Resize the checked-in master; never recreate the artwork procedurally.
func appIcon(source image.Image, size int) []byte {
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(icon, icon.Bounds(), source, source.Bounds(), draw.Src, nil)
	var b bytes.Buffer
	if err := png.Encode(&b, icon); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func windowsIcon(source image.Image) []byte {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	images := make([][]byte, len(sizes))
	var b bytes.Buffer
	for _, v := range []uint16{0, 1, uint16(len(sizes))} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	offset := uint32(6 + 16*len(sizes))
	for i, size := range sizes {
		images[i] = appIcon(source, size)
		// ICO encodes 256 pixels as zero in its one-byte dimensions.
		b.Write([]byte{byte(size % 256), byte(size % 256), 0, 0})
		for _, v := range []uint16{1, 32} {
			_ = binary.Write(&b, binary.LittleEndian, v)
		}
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(images[i])))
		_ = binary.Write(&b, binary.LittleEndian, offset)
		offset += uint32(len(images[i]))
	}
	for _, icon := range images {
		b.Write(icon)
	}
	return b.Bytes()
}

func main() {
	f, err := os.Open("packaging/assets/app-icon.png")
	if err != nil {
		panic(err)
	}
	source, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		panic(err)
	}
	if source.Bounds().Dx() != source.Bounds().Dy() || source.Bounds().Dx() < 1024 {
		panic("app-icon.png must be a square PNG at least 1024 pixels wide")
	}
	save("packaging/assets/app-icon.ico", windowsIcon(source))
	dir := "packaging/assets/ReceiptStudio.iconset"
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}
	names := map[string]int{"icon_16x16.png": 16, "icon_16x16@2x.png": 32, "icon_32x32.png": 32, "icon_32x32@2x.png": 64, "icon_128x128.png": 128, "icon_128x128@2x.png": 256, "icon_256x256.png": 256, "icon_256x256@2x.png": 512, "icon_512x512.png": 512, "icon_512x512@2x.png": 1024}
	for name, size := range names {
		save(filepath.Join(dir, name), appIcon(source, size))
	}
}
