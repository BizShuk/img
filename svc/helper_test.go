package svc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// makeImage 產生一張帶漸層的測試圖。opaque 為 false 時左側 1/4 完全透明，
// 用來驗證 alpha 的偵測與 JPEG 的白底壓平。
func makeImage(w, h int, opaque bool) image.Image {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if !opaque && x < w/4 {
				a = 0
			}
			m.Set(x, y, color.NRGBA{uint8(x % 256), uint8(y % 256), 128, a})
		}
	}
	return m
}

// encodeTo 把測試圖編成指定格式的位元組。
func encodeTo(t *testing.T, img image.Image, f Format, opt EncodeOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, img, f, opt); err != nil {
		t.Fatalf("encode %s: %v", f, err)
	}
	return buf.Bytes()
}

// jpegWithOrientation 在一張 JPEG 的 SOI 之後插入帶 Orientation tag 的
// APP1 段落，用來驗證 EXIF 解析而不需要放二進位 fixture。
func jpegWithOrientation(t *testing.T, img image.Image, orientation uint16) []byte {
	t.Helper()
	base := encodeTo(t, img, FORMAT_JPEG, EncodeOptions{Quality: 80})

	tiff := new(bytes.Buffer)
	tiff.WriteString("II")
	binary.Write(tiff, binary.LittleEndian, uint16(42))
	binary.Write(tiff, binary.LittleEndian, uint32(8))
	binary.Write(tiff, binary.LittleEndian, uint16(1))     // IFD0 entry count
	binary.Write(tiff, binary.LittleEndian, uint16(0x112)) // Orientation
	binary.Write(tiff, binary.LittleEndian, uint16(3))     // SHORT
	binary.Write(tiff, binary.LittleEndian, uint32(1))     // count
	binary.Write(tiff, binary.LittleEndian, orientation)
	binary.Write(tiff, binary.LittleEndian, uint16(0)) // value 欄位補滿 4 位元組
	binary.Write(tiff, binary.LittleEndian, uint32(0)) // 無下一個 IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)

	out := new(bytes.Buffer)
	out.Write(base[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(base[2:])
	return out.Bytes()
}
