package svc

import (
	"encoding/binary"
	"image"
)

// ORIENTATION_NONE 表示檔案沒有帶 EXIF orientation 資訊。
const ORIENTATION_NONE = 0

// EXIF 中本工具會讀或改的 tag。其餘欄位（拍攝時間、相機、鏡頭、GPS）
// 一律原封不動地帶過去，不逐一解析。
const (
	TAG_IMAGE_WIDTH       = 0x0100
	TAG_IMAGE_LENGTH      = 0x0101
	TAG_BITS_PER_SAMP     = 0x0102
	TAG_ORIENTATION       = 0x0112
	TAG_SAMPLES_PER_PIXEL = 0x0115
	TAG_EXTRA_SAMPLES     = 0x0152
	TAG_EXIF_IFD          = 0x8769
	TAG_PIXEL_X_DIM       = 0xA002
	TAG_PIXEL_Y_DIM       = 0xA003
)

// tiffTypeSize 是 TIFF 各資料型別的單位長度（位元組）。
var tiffTypeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

// tiffField 記下一筆欄位的型別與「值放在哪裡」。保留 valueAt 是為了能
// 就地改寫，而不必把整個 IFD 重新序列化 —— 重新序列化就得搬動所有
// 外部偏移量，那是把已知能用的 EXIF 弄壞最快的方法。
type tiffField struct {
	typ     uint16
	count   uint32
	valueAt int
}

// tiffIFD 是一個已解析的 image file directory。
type tiffIFD struct {
	order     binary.ByteOrder
	fields    map[uint16]tiffField
	nextIFDAt int
}

// readTIFF 解析 TIFF header 與 IFD0。b 是完整的 TIFF 區塊
// （EXIF payload 去掉 "Exif\x00\x00" 之後的內容，或一整個 .tif 檔）。
func readTIFF(b []byte) (*tiffIFD, bool) {
	if len(b) < 8 {
		return nil, false
	}

	var order binary.ByteOrder
	switch string(b[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, false
	}
	if order.Uint16(b[2:4]) != 42 {
		return nil, false
	}

	return readIFD(b, order, int(order.Uint32(b[4:8])))
}

// readIFD 讀出指定偏移處的 IFD。sub-IFD（例如 Exif IFD）走同一條路。
func readIFD(b []byte, order binary.ByteOrder, offset int) (*tiffIFD, bool) {
	if offset < 8 || offset+2 > len(b) {
		return nil, false
	}

	count := int(order.Uint16(b[offset : offset+2]))
	ifd := &tiffIFD{order: order, fields: make(map[uint16]tiffField, count)}

	pos := offset + 2
	for i := 0; i < count; i++ {
		if pos+12 > len(b) {
			return nil, false
		}
		entry := b[pos : pos+12]
		tag := order.Uint16(entry[0:2])
		typ := order.Uint16(entry[2:4])
		n := order.Uint32(entry[4:8])

		// 值不超過四個位元組時就地存放，否則第 9-12 位元組是它的偏移量。
		valueAt := pos + 8
		if size := tiffTypeSize[typ] * int(n); size > 4 {
			valueAt = int(order.Uint32(entry[8:12]))
			if valueAt < 8 || valueAt+size > len(b) {
				pos += 12
				continue
			}
		}

		ifd.fields[tag] = tiffField{typ: typ, count: n, valueAt: valueAt}
		pos += 12
	}

	if pos+4 <= len(b) {
		ifd.nextIFDAt = pos
	}
	return ifd, true
}

// uint 讀出某個 tag 的第一個整數值。只處理 BYTE / SHORT / LONG，
// 其餘型別（RATIONAL、ASCII）本工具沒有讀的需要。
func (t *tiffIFD) uint(b []byte, tag uint16) (uint32, bool) {
	f, ok := t.fields[tag]
	if !ok || f.count == 0 {
		return 0, false
	}
	switch f.typ {
	case 1, 6, 7:
		if f.valueAt < len(b) {
			return uint32(b[f.valueAt]), true
		}
	case 3, 8:
		if f.valueAt+2 <= len(b) {
			return uint32(t.order.Uint16(b[f.valueAt : f.valueAt+2])), true
		}
	case 4, 9:
		if f.valueAt+4 <= len(b) {
			return t.order.Uint32(b[f.valueAt : f.valueAt+4]), true
		}
	}
	return 0, false
}

// setUint 就地改寫某個 tag 的第一個整數值。型別不符或欄位不存在時
// 回報 false —— 這種情況一律放著不動，不去擴充或搬移 IFD。
func (t *tiffIFD) setUint(b []byte, tag uint16, v uint32) bool {
	f, ok := t.fields[tag]
	if !ok || f.count == 0 {
		return false
	}
	switch f.typ {
	case 3, 8:
		if f.valueAt+2 > len(b) || v > 0xFFFF {
			return false
		}
		t.order.PutUint16(b[f.valueAt:f.valueAt+2], uint16(v))
		return true
	case 4, 9:
		if f.valueAt+4 > len(b) {
			return false
		}
		t.order.PutUint32(b[f.valueAt:f.valueAt+4], v)
		return true
	}
	return false
}

// orientationFromTIFF 取出 IFD0 的 Orientation。找不到或值不合理時
// 回傳 ORIENTATION_NONE。
func orientationFromTIFF(b []byte) int {
	ifd, ok := readTIFF(b)
	if !ok {
		return ORIENTATION_NONE
	}
	v, ok := ifd.uint(b, TAG_ORIENTATION)
	if !ok || v < 1 || v > 8 {
		return ORIENTATION_NONE
	}
	return int(v)
}

// RetargetExif 讓一段沿用自來源的 EXIF 與輸出影像相符。轉換過程只改動
// 影像本身，若把原封不動的 EXIF 直接寫進去，看圖軟體會依舊的
// orientation 再轉一次，尺寸欄位也會與實際像素對不上。
//
// 一律`就地`改寫既有欄位，不新增也不搬移；改不動的欄位放著不管。
// 同時把 IFD0 的 next-IFD 指標清零，丟掉指向舊縮圖的 IFD1。
func RetargetExif(block []byte, width, height int, rotated bool) []byte {
	out := make([]byte, len(block))
	copy(out, block)

	ifd0, ok := readTIFF(out)
	if !ok {
		return out
	}

	if rotated {
		ifd0.setUint(out, TAG_ORIENTATION, 1)
	}
	ifd0.setUint(out, TAG_IMAGE_WIDTH, uint32(width))
	ifd0.setUint(out, TAG_IMAGE_LENGTH, uint32(height))

	if sub, ok := ifd0.uint(out, TAG_EXIF_IFD); ok {
		if exifIFD, ok := readIFD(out, ifd0.order, int(sub)); ok {
			exifIFD.setUint(out, TAG_PIXEL_X_DIM, uint32(width))
			exifIFD.setUint(out, TAG_PIXEL_Y_DIM, uint32(height))
		}
	}

	// IFD1 只放縮圖，而縮圖是轉換前的舊畫面。指標清零等於丟掉它。
	if ifd0.nextIFDAt > 0 && ifd0.nextIFDAt+4 <= len(out) {
		ifd0.order.PutUint32(out[ifd0.nextIFDAt:ifd0.nextIFDAt+4], 0)
	}
	return out
}

// ApplyOrientation 依 EXIF orientation 把影像轉正，回傳的影像其左上角即為
// 拍攝時的左上角。orientation 為 0 或 1 時原樣回傳。
func ApplyOrientation(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	// orientation 5-8 會交換長寬。
	dstW, dstH := w, h
	if orientation >= 5 {
		dstW, dstH = h, w
	}

	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2: // 水平鏡射
				dx, dy = w-1-x, y
			case 3: // 旋轉 180 度
				dx, dy = w-1-x, h-1-y
			case 4: // 垂直鏡射
				dx, dy = x, h-1-y
			case 5: // 轉置
				dx, dy = y, x
			case 6: // 順時針 90 度
				dx, dy = h-1-y, x
			case 7: // 反轉置
				dx, dy = h-1-y, w-1-x
			case 8: // 逆時針 90 度
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
