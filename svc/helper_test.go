package svc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"sort"
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
	// MakerNote 模擬廠商私有區塊。實機上動輒數百 KB，是把 EXIF 撐爆
	// 超過 JPEG 段落上限的主因。
	MakerNote []byte
	// GPS 為真時加一個帶座標的 GPS 子目錄。
	GPS bool
}

// tiffFixture 是測試端獨立實作的 TIFF 組裝器。刻意`不`共用 production 的
// buildTIFF —— 兩邊若共用同一份版面計算，計算本身有錯時測試也看不出來。
type tiffFixture struct {
	tag    uint16
	typ    uint16
	count  uint32
	data   []byte
	offset int
}

// buildExifBlock 組出一個小而完整的 TIFF 區塊：IFD0 帶尺寸、orientation
// 與相機廠牌，並透過 ExifIFD 指標掛上拍攝時間與像素尺寸。
func buildExifBlock(f exifFixture) []byte {
	order := binary.LittleEndian

	ascii := func(s string) []byte {
		b := append([]byte(s), 0)
		return b
	}
	long := func(v uint32) []byte {
		b := make([]byte, 4)
		order.PutUint32(b, v)
		return b
	}
	short := func(v uint16) []byte {
		b := make([]byte, 2)
		order.PutUint16(b, v)
		return b
	}

	entry := func(tag, typ uint16, data []byte) tiffFixture {
		count := uint32(len(data))
		if typ == 3 {
			count = uint32(len(data) / 2)
		} else if typ == 4 {
			count = uint32(len(data) / 4)
		} else if typ == 5 {
			count = uint32(len(data) / 8)
		}
		return tiffFixture{tag: tag, typ: typ, count: count, data: data}
	}

	ifd0 := []tiffFixture{
		entry(TAG_IMAGE_WIDTH, 4, long(f.Width)),
		entry(TAG_IMAGE_LENGTH, 4, long(f.Height)),
		entry(0x010F, 2, ascii(f.Make)),
		entry(TAG_ORIENTATION, 3, short(f.Orientation)),
	}
	exif := []tiffFixture{
		entry(0x9003, 2, ascii(f.DateTime)),
		entry(TAG_PIXEL_X_DIM, 4, long(f.Width)),
		entry(TAG_PIXEL_Y_DIM, 4, long(f.Height)),
	}
	if len(f.MakerNote) > 0 {
		exif = append(exif, entry(0x927C, 7, f.MakerNote))
	}
	var gps []tiffFixture
	if f.GPS {
		gps = []tiffFixture{
			entry(0x0001, 2, ascii("N")),
			entry(0x0002, 5, make([]byte, 24)), // 三組 RATIONAL
		}
	}

	ifdSize := func(n int) int { return 2 + 12*n + 4 }
	count0 := len(ifd0) + 1 // ExifIFD 指標
	if f.GPS {
		count0++
	}

	pos := 8
	ifd0At := pos
	pos += ifdSize(count0)
	exifAt := pos
	pos += ifdSize(len(exif))
	gpsAt := pos
	if f.GPS {
		pos += ifdSize(len(gps))
	}

	place := func(entries []tiffFixture) {
		for i := range entries {
			if len(entries[i].data) <= 4 {
				continue
			}
			entries[i].offset = pos
			pos += len(entries[i].data)
			if pos%2 == 1 {
				pos++
			}
		}
	}
	place(ifd0)
	place(exif)
	place(gps)

	out := make([]byte, pos)
	copy(out[0:2], "II")
	order.PutUint16(out[2:4], 42)
	order.PutUint32(out[4:8], 8)

	write := func(at int, entries []tiffFixture) {
		order.PutUint16(out[at:at+2], uint16(len(entries)))
		p := at + 2
		for _, e := range entries {
			order.PutUint16(out[p:p+2], e.tag)
			order.PutUint16(out[p+2:p+4], e.typ)
			order.PutUint32(out[p+4:p+8], e.count)
			if len(e.data) <= 4 {
				copy(out[p+8:p+12], e.data)
			} else {
				order.PutUint32(out[p+8:p+12], uint32(e.offset))
				copy(out[e.offset:e.offset+len(e.data)], e.data)
			}
			p += 12
		}
		order.PutUint32(out[p:p+4], 0)
	}

	full := append([]tiffFixture{}, ifd0...)
	full = append(full, tiffFixture{tag: TAG_EXIF_IFD, typ: 4, count: 1, data: long(uint32(exifAt))})
	if f.GPS {
		full = append(full, tiffFixture{tag: TAG_GPS_IFD, typ: 4, count: 1, data: long(uint32(gpsAt))})
	}
	sort.Slice(full, func(i, j int) bool { return full[i].tag < full[j].tag })

	write(ifd0At, full)
	write(exifAt, exif)
	if f.GPS {
		write(gpsAt, gps)
	}
	return out
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

// pngWithExif 把一段 EXIF 區塊包成 eXIf chunk 塞進 PNG。PNG 的 chunk 長度
// 是 32 位元，因此可以裝下遠超過 JPEG 段落上限的區塊。
func pngWithExif(t *testing.T, img image.Image, block []byte) []byte {
	t.Helper()
	base := encodeTo(t, img, FORMAT_PNG, EncodeOptions{})

	out, err := EmbedExif(base, FORMAT_PNG, block, img.Bounds().Dx(), img.Bounds().Dy())
	if err != nil {
		t.Fatalf("embed exif into png: %v", err)
	}
	return out
}
