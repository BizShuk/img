package svc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"golang.org/x/image/tiff"
)

// encodeGIF 與 encodeTIFF 只在測試裡用。production code 不註冊這兩個
// 編碼器 —— 它們不是 convert 的合法目標。
func encodeGIF(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func encodeAnimatedGIF(t *testing.T, frames int) []byte {
	t.Helper()
	anim := &gif.GIF{}
	for i := 0; i < frames; i++ {
		var one bytes.Buffer
		if err := gif.Encode(&one, makeImage(8, 8, true), nil); err != nil {
			t.Fatalf("encode gif frame: %v", err)
		}
		decoded, err := gif.Decode(&one)
		if err != nil {
			t.Fatalf("decode gif frame: %v", err)
		}
		anim.Image = append(anim.Image, decoded.(*image.Paletted))
		anim.Delay = append(anim.Delay, 10)
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		t.Fatalf("encode animated gif: %v", err)
	}
	return buf.Bytes()
}

func encodeTIFF(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := tiff.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode tiff: %v", err)
	}
	return buf.Bytes()
}

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

// jpegWithOrientation 產生一張帶指定 EXIF orientation 的 JPEG。
func jpegWithOrientation(t *testing.T, img image.Image, orientation uint16) []byte {
	t.Helper()
	b := img.Bounds()
	return jpegWithExif(t, img, buildExifBlock(exifFixture{
		Orientation: orientation,
		Width:       uint32(b.Dx()),
		Height:      uint32(b.Dy()),
		Make:        "TestCam",
		DateTime:    "2026:08:27 10:30:00",
	}))
}

// exifFixture 描述測試用 EXIF 區塊的內容。
type exifFixture struct {
	Orientation   uint16
	Width, Height uint32
	Make          string
	DateTime      string
}

// buildExifBlock 組出一個小而完整的 TIFF 區塊：IFD0 帶尺寸、orientation
// 與相機廠牌，並透過 ExifIFD 指標掛上拍攝時間與像素尺寸。
// 這是驗證 RetargetExif 的最小樣本 —— 它必須改到 sub-IFD 裡的欄位。
func buildExifBlock(f exifFixture) []byte {
	const (
		IFD0_ENTRIES = 5
		SUB_ENTRIES  = 3
	)
	order := binary.LittleEndian

	// IFD0 從偏移 8 開始；entry count(2) + entries(12n) + next offset(4)。
	extraAt := 8 + 2 + 12*IFD0_ENTRIES + 4
	makeBytes := append([]byte(f.Make), 0)
	if len(makeBytes)%2 == 1 {
		makeBytes = append(makeBytes, 0)
	}
	subAt := extraAt + len(makeBytes)
	subExtraAt := subAt + 2 + 12*SUB_ENTRIES + 4
	dateBytes := append([]byte(f.DateTime), 0)
	if len(dateBytes)%2 == 1 {
		dateBytes = append(dateBytes, 0)
	}

	entry := func(tag, typ uint16, count uint32, value []byte) []byte {
		b := make([]byte, 12)
		order.PutUint16(b[0:2], tag)
		order.PutUint16(b[2:4], typ)
		order.PutUint32(b[4:8], count)
		copy(b[8:12], value)
		return b
	}
	u32 := func(v uint32) []byte {
		b := make([]byte, 4)
		order.PutUint32(b, v)
		return b
	}
	u16 := func(v uint16) []byte {
		b := make([]byte, 4)
		order.PutUint16(b[0:2], v)
		return b
	}

	out := new(bytes.Buffer)
	out.WriteString("II")
	binary.Write(out, order, uint16(42))
	binary.Write(out, order, uint32(8))

	binary.Write(out, order, uint16(IFD0_ENTRIES))
	out.Write(entry(TAG_IMAGE_WIDTH, 4, 1, u32(f.Width)))
	out.Write(entry(TAG_IMAGE_LENGTH, 4, 1, u32(f.Height)))
	out.Write(entry(0x010F, 2, uint32(len(makeBytes)), u32(uint32(extraAt)))) // Make
	out.Write(entry(TAG_ORIENTATION, 3, 1, u16(f.Orientation)))
	out.Write(entry(TAG_EXIF_IFD, 4, 1, u32(uint32(subAt))))
	binary.Write(out, order, uint32(0)) // 無 IFD1

	out.Write(makeBytes)

	binary.Write(out, order, uint16(SUB_ENTRIES))
	out.Write(entry(0x9003, 2, uint32(len(dateBytes)), u32(uint32(subExtraAt)))) // DateTimeOriginal
	out.Write(entry(TAG_PIXEL_X_DIM, 4, 1, u32(f.Width)))
	out.Write(entry(TAG_PIXEL_Y_DIM, 4, 1, u32(f.Height)))
	binary.Write(out, order, uint32(0))

	out.Write(dateBytes)
	return out.Bytes()
}

// jpegWithExif 把一段 EXIF 區塊包成 APP1 塞進 JPEG。
func jpegWithExif(t *testing.T, img image.Image, block []byte) []byte {
	t.Helper()
	base := encodeTo(t, img, FORMAT_JPEG, EncodeOptions{Quality: 80})

	payload := append(append([]byte{}, exifPrefix...), block...)
	out := new(bytes.Buffer)
	out.Write(base[:2])
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(base[2:])
	return out.Bytes()
}

// asciiTag 讀出 TIFF ASCII 欄位的字串值，用來確認相機資訊有沒有活著到終點。
func asciiTag(t *testing.T, block []byte, ifdTag, tag uint16) string {
	t.Helper()
	ifd, ok := readTIFF(block)
	if !ok {
		t.Fatal("not a valid tiff block")
	}
	if ifdTag != 0 {
		sub, ok := ifd.uint(block, ifdTag)
		if !ok {
			t.Fatalf("sub-ifd pointer %#x missing", ifdTag)
		}
		if ifd, ok = readIFD(block, ifd.order, int(sub)); !ok {
			t.Fatalf("sub-ifd at %d unreadable", sub)
		}
	}
	f, ok := ifd.fields[tag]
	if !ok {
		return ""
	}
	return string(bytes.TrimRight(block[f.valueAt:f.valueAt+int(f.count)], "\x00"))
}

// fakeHEIC 造一個只有 ftyp box 的 ISO BMFF 檔頭。analyze 只靠它認出格式，
// 不需要真的能解碼。
func fakeHEIC(major string, compatible ...string) []byte {
	size := 16 + 4*len(compatible)
	out := make([]byte, 0, size)
	out = append(out, byte(size>>24), byte(size>>16), byte(size>>8), byte(size))
	out = append(out, "ftyp"...)
	out = append(out, major...)
	out = append(out, 0, 0, 0, 0)
	for _, b := range compatible {
		out = append(out, b...)
	}
	return out
}
