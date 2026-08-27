package svc

import (
	"encoding/binary"
	"sort"
)

// TAG_GPS_IFD 指向 GPS 子目錄。
const TAG_GPS_IFD = 0x8825

// keepIFD0 與 keepExif 是重建時保留的欄位。取捨標準是「使用者為什麼要留著
// EXIF」：拍攝時間、相機與鏡頭、曝光參數。刻意`不`保留的是把區塊撐爆的
// 兩個東西 —— MakerNote（廠商私有格式，動輒數百 KB）與內嵌縮圖。
var keepIFD0 = map[uint16]bool{
	TAG_IMAGE_WIDTH:  true,
	TAG_IMAGE_LENGTH: true,
	TAG_ORIENTATION:  true,
	0x010F:           true, // Make
	0x0110:           true, // Model
	0x011A:           true, // XResolution
	0x011B:           true, // YResolution
	0x0128:           true, // ResolutionUnit
	0x0131:           true, // Software
	0x0132:           true, // DateTime
	0x013B:           true, // Artist
	0x8298:           true, // Copyright
}

var keepExif = map[uint16]bool{
	0x829A:          true, // ExposureTime
	0x829D:          true, // FNumber
	0x8822:          true, // ExposureProgram
	0x8827:          true, // ISOSpeedRatings
	0x9000:          true, // ExifVersion
	0x9003:          true, // DateTimeOriginal
	0x9004:          true, // DateTimeDigitized
	0x9201:          true, // ShutterSpeedValue
	0x9202:          true, // ApertureValue
	0x9204:          true, // ExposureBiasValue
	0x9207:          true, // MeteringMode
	0x9209:          true, // Flash
	0x920A:          true, // FocalLength
	0x9291:          true, // SubSecTime
	0x9292:          true, // SubSecTimeOriginal
	TAG_PIXEL_X_DIM: true,
	TAG_PIXEL_Y_DIM: true,
	0xA402:          true, // ExposureMode
	0xA403:          true, // WhiteBalance
	0xA405:          true, // FocalLengthIn35mmFilm
	0xA406:          true, // SceneCaptureType
	0xA430:          true, // CameraOwnerName
	0xA431:          true, // BodySerialNumber
	0xA432:          true, // LensSpecification
	0xA433:          true, // LensMake
	0xA434:          true, // LensModel
}

// tiffEntry 是要寫出去的一筆欄位。data 是完整的值位元組，
// 四個位元組以內就地存放，超過則另闢資料區並記在 offset。
type tiffEntry struct {
	tag    uint16
	typ    uint16
	count  uint32
	data   []byte
	offset int
}

func (e tiffEntry) inline() bool { return len(e.data) <= 4 }

// CompactExif 重建一份只含必要欄位的 EXIF 區塊。
//
// 這是 `RetargetExif` 的例外路徑：平常一律就地改寫、絕不重新序列化，
// 因為 TIFF 到處是絕對偏移量。但區塊塞不進 JPEG 的段落上限時，
// 「只留必要欄位」遠優於「整段丟掉」。
//
// 重建無可避免會丟掉 MakerNote —— 它的內部偏移量多半是相對於原 TIFF
// header 的，搬到新位置就失效了，因此`必須`丟而不是能不能留的問題。
// 這也是重建不能當成預設路徑的原因。
//
// 縮不小時原樣回傳，讓呼叫端據此判斷值不值得換。
func CompactExif(block []byte) []byte {
	ifd0, ok := readTIFF(block)
	if !ok {
		return block
	}

	main := collectEntries(block, ifd0, keepIFD0)
	exif := collectSubIFD(block, ifd0, TAG_EXIF_IFD, keepExif)
	// GPS 目錄整個留下：欄位都很小，而且只留一半的座標沒有意義。
	gps := collectSubIFD(block, ifd0, TAG_GPS_IFD, nil)

	out := buildTIFF(ifd0.order, main, exif, gps)
	if len(out) >= len(block) {
		return block
	}
	return out
}

// collectSubIFD 讀出由 pointerTag 指向的子目錄。keep 為 nil 表示全留。
func collectSubIFD(block []byte, parent *tiffIFD, pointerTag uint16, keep map[uint16]bool) []tiffEntry {
	at, ok := parent.uint(block, pointerTag)
	if !ok {
		return nil
	}
	sub, ok := readIFD(block, parent.order, int(at))
	if !ok {
		return nil
	}
	return collectEntries(block, sub, keep)
}

// collectEntries 依 keep 挑出欄位並複製其值。子目錄指標由 buildTIFF
// 重新產生，因此這裡一律跳過。
func collectEntries(block []byte, ifd *tiffIFD, keep map[uint16]bool) []tiffEntry {
	entries := make([]tiffEntry, 0, len(ifd.fields))
	for tag, f := range ifd.fields {
		if tag == TAG_EXIF_IFD || tag == TAG_GPS_IFD {
			continue
		}
		if keep != nil && !keep[tag] {
			continue
		}
		size := tiffTypeSize[f.typ] * int(f.count)
		if size <= 0 || f.valueAt+size > len(block) {
			continue
		}
		entries = append(entries, tiffEntry{
			tag:   tag,
			typ:   f.typ,
			count: f.count,
			data:  append([]byte{}, block[f.valueAt:f.valueAt+size]...),
		})
	}
	sortEntries(entries)
	return entries
}

// buildTIFF 把三組欄位組成一個完整的 TIFF 區塊。
//
// `位元組序沿用來源`：欄位的值是照抄過來的原始位元組，換了位元組序就會
// 被讀成完全不同的數字。
func buildTIFF(order binary.ByteOrder, ifd0, exif, gps []tiffEntry) []byte {
	ifdSize := func(n int) int { return 2 + 12*n + 4 }

	count0 := len(ifd0)
	if len(exif) > 0 {
		count0++
	}
	if len(gps) > 0 {
		count0++
	}

	pos := 8
	ifd0At := pos
	pos += ifdSize(count0)

	exifAt, gpsAt := 0, 0
	if len(exif) > 0 {
		exifAt = pos
		pos += ifdSize(len(exif))
	}
	if len(gps) > 0 {
		gpsAt = pos
		pos += ifdSize(len(gps))
	}

	// 逐一指派資料區位置。TIFF 的偏移量必須落在偶數位址。
	for _, group := range [][]tiffEntry{ifd0, exif, gps} {
		for i := range group {
			if group[i].inline() {
				continue
			}
			group[i].offset = pos
			pos += len(group[i].data)
			if pos%2 == 1 {
				pos++
			}
		}
	}

	out := make([]byte, pos)
	if order == binary.BigEndian {
		copy(out[0:2], "MM")
	} else {
		copy(out[0:2], "II")
	}
	order.PutUint16(out[2:4], 42)
	order.PutUint32(out[4:8], 8)

	full := append([]tiffEntry{}, ifd0...)
	if len(exif) > 0 {
		full = append(full, pointerEntry(order, TAG_EXIF_IFD, exifAt))
	}
	if len(gps) > 0 {
		full = append(full, pointerEntry(order, TAG_GPS_IFD, gpsAt))
	}
	sortEntries(full)

	writeIFD(out, order, ifd0At, full)
	if len(exif) > 0 {
		writeIFD(out, order, exifAt, exif)
	}
	if len(gps) > 0 {
		writeIFD(out, order, gpsAt, gps)
	}
	return out
}

// pointerEntry 造一筆指向子目錄的 LONG 欄位。
func pointerEntry(order binary.ByteOrder, tag uint16, at int) tiffEntry {
	data := make([]byte, 4)
	order.PutUint32(data, uint32(at))
	return tiffEntry{tag: tag, typ: 4, count: 1, data: data}
}

// writeIFD 寫出一個目錄及其外部值。
func writeIFD(out []byte, order binary.ByteOrder, at int, entries []tiffEntry) {
	order.PutUint16(out[at:at+2], uint16(len(entries)))

	p := at + 2
	for _, e := range entries {
		order.PutUint16(out[p:p+2], e.tag)
		order.PutUint16(out[p+2:p+4], e.typ)
		order.PutUint32(out[p+4:p+8], e.count)
		if e.inline() {
			copy(out[p+8:p+12], e.data)
		} else {
			order.PutUint32(out[p+8:p+12], uint32(e.offset))
			copy(out[e.offset:e.offset+len(e.data)], e.data)
		}
		p += 12
	}
	order.PutUint32(out[p:p+4], 0) // 無下一個 IFD
}

// sortEntries 依 tag 遞增排序。TIFF 規格要求目錄內的欄位依此順序排列。
func sortEntries(entries []tiffEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].tag < entries[j].tag })
}
