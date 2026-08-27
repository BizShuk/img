package svc

import (
	"encoding/binary"
	"image"
	"image/draw"
)

// ORIENTATION_NONE 表示檔案沒有帶 EXIF orientation 資訊。
const ORIENTATION_NONE = 0

// TAG_ORIENTATION 是 EXIF IFD0 中 Orientation 的 tag id。
const TAG_ORIENTATION = 0x0112

// orientationFromTIFF 解析一段 TIFF header（EXIF payload 去掉 "Exif\x00\x00" 之後的內容），
// 取出 IFD0 的 Orientation tag。只讀這一個 tag —— 本工具不需要其他 EXIF 欄位，
// 為此引入完整 EXIF 函式庫並不划算。找不到或格式不合時回傳 ORIENTATION_NONE。
func orientationFromTIFF(b []byte) int {
	if len(b) < 8 {
		return ORIENTATION_NONE
	}

	var order binary.ByteOrder
	switch string(b[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return ORIENTATION_NONE
	}
	if order.Uint16(b[2:4]) != 42 {
		return ORIENTATION_NONE
	}

	offset := int(order.Uint32(b[4:8]))
	if offset < 8 || offset+2 > len(b) {
		return ORIENTATION_NONE
	}

	entries := int(order.Uint16(b[offset : offset+2]))
	offset += 2
	for i := 0; i < entries; i++ {
		if offset+12 > len(b) {
			return ORIENTATION_NONE
		}
		entry := b[offset : offset+12]
		offset += 12

		if order.Uint16(entry[0:2]) != TAG_ORIENTATION {
			continue
		}
		// Orientation 的型別是 SHORT(3)，值就地放在 entry 的最後四個位元組中。
		if order.Uint16(entry[2:4]) != 3 {
			return ORIENTATION_NONE
		}
		v := int(order.Uint16(entry[8:10]))
		if v < 1 || v > 8 {
			return ORIENTATION_NONE
		}
		return v
	}
	return ORIENTATION_NONE
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

// toDrawable 把任意 image.Image 轉成可寫入的緩衝，供縮放與裁切使用。
func toDrawable(src image.Image) draw.Image {
	if d, ok := src.(draw.Image); ok {
		return d
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}
