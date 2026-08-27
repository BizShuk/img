package svc

import (
	"bytes"
	"encoding/binary"
)

// headerMeta 是只靠檔頭就能取得、而 image.Config 不提供的資訊。
// 三種容器各有自己的結構，這裡逐一走訪 chunk / marker，不做像素解碼。
type headerMeta struct {
	BitDepth    int    // 每個色彩通道的位元數
	HasAlpha    bool   // 是否帶透明通道
	Orientation int    // EXIF orientation；ORIENTATION_NONE 表示未帶
	Frames      int    // 動畫張數；靜態圖為 1
	Progressive bool   // 僅 JPEG：是否為 progressive 編碼
	Exif        []byte // 原始 TIFF 區塊；沒有時為 nil
}

// parseHeader 依格式分派到對應的容器走訪函式。無法辨識的欄位維持零值，
// 呼叫端據此判斷「未知」而不是「沒有」。
func parseHeader(f Format, data []byte) headerMeta {
	switch f {
	case FORMAT_JPEG:
		return parseJPEGHeader(data)
	case FORMAT_PNG:
		return parsePNGHeader(data)
	case FORMAT_WEBP:
		return parseWebPHeader(data)
	case FORMAT_GIF:
		return parseGIFHeader(data)
	case FORMAT_TIFF:
		return parseTIFFHeader(data)
	default:
		return headerMeta{Frames: 1}
	}
}

// parseJPEGHeader 走訪 JPEG 的 marker 串列，取出 SOF 的取樣精度與
// APP1 中的 EXIF 區塊，遇到 SOS（影像資料起點）即停止。
func parseJPEGHeader(data []byte) headerMeta {
	meta := headerMeta{BitDepth: 8, Frames: 1, Orientation: ORIENTATION_NONE}

	i := 2 // 跳過 SOI (FFD8)
	for i+3 < len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		i += 2

		switch {
		case marker == 0xFF || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD9):
			continue // 無 payload 的 marker
		case marker == 0xDA:
			return meta // SOS：後面是壓縮影像資料
		}

		if i+2 > len(data) {
			return meta
		}
		size := int(binary.BigEndian.Uint16(data[i : i+2]))
		if size < 2 || i+size > len(data) {
			return meta
		}
		payload := data[i+2 : i+size]
		i += size

		switch {
		case marker == 0xE1 && meta.Exif == nil && bytes.HasPrefix(payload, exifPrefix):
			meta.Exif = payload[len(exifPrefix):]
			meta.Orientation = orientationFromTIFF(meta.Exif)
		case isSOFMarker(marker):
			if len(payload) > 0 {
				meta.BitDepth = int(payload[0])
			}
			meta.Progressive = marker == 0xC2 || marker == 0xC6 || marker == 0xCA || marker == 0xCE
		}
	}
	return meta
}

// isSOFMarker 判斷是否為 Start Of Frame。C4 (DHT)、C8 (JPG)、CC (DAC)
// 落在同一區間但不是 SOF。
func isSOFMarker(m byte) bool {
	return m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC
}

// parsePNGHeader 讀 IHDR 的色彩型別與位元深度，並走訪 chunk 找出
// tRNS（調色盤透明）、acTL（APNG 動畫張數）與 eXIf。
func parsePNGHeader(data []byte) headerMeta {
	meta := headerMeta{BitDepth: 8, Frames: 1, Orientation: ORIENTATION_NONE}
	if len(data) < 26 {
		return meta
	}

	meta.BitDepth = int(data[24])
	colorType := data[25]
	meta.HasAlpha = colorType == 4 || colorType == 6

	for i := 8; i+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[i : i+4]))
		if size < 0 || i+12+size > len(data) {
			break
		}
		name := string(data[i+4 : i+8])
		payload := data[i+8 : i+8+size]

		switch name {
		case "tRNS":
			meta.HasAlpha = true
		case "acTL":
			if len(payload) >= 4 {
				meta.Frames = int(binary.BigEndian.Uint32(payload[0:4]))
			}
		case "eXIf":
			meta.Exif = bytes.TrimPrefix(payload, exifPrefix)
			meta.Orientation = orientationFromTIFF(meta.Exif)
		case "IDAT":
			return meta // 影像資料開始，之後只剩 fdAT/IEND
		}
		i += 12 + size // length + type + payload + crc
	}
	return meta
}

// parseWebPHeader 走訪 RIFF chunk。VP8X 的旗標位元同時說明透明與動畫，
// 舊式的單張 VP8 / VP8L 則各自從自己的 bitstream header 判斷。
func parseWebPHeader(data []byte) headerMeta {
	meta := headerMeta{BitDepth: 8, Frames: 1, Orientation: ORIENTATION_NONE}

	frames := 0
	forEachRIFFChunk(data, func(name string, payload []byte) bool {
		switch name {
		case "VP8X":
			if len(payload) >= 1 {
				meta.HasAlpha = payload[0]&WEBP_FLAG_ALPHA != 0
			}
		case "ALPH":
			meta.HasAlpha = true
		case "VP8L":
			// 簽章 0x2F 之後的 4 個位元組中，第 28 個位元是 alpha_is_used。
			if len(payload) >= 5 && payload[0] == 0x2F {
				bits := binary.LittleEndian.Uint32(payload[1:5])
				meta.HasAlpha = bits&(1<<28) != 0
			}
		case "ANMF":
			frames++
		case "EXIF":
			meta.Exif = bytes.TrimPrefix(payload, exifPrefix)
			meta.Orientation = orientationFromTIFF(meta.Exif)
		}
		return true
	})

	if frames > 0 {
		meta.Frames = frames
	}
	return meta
}

// parseGIFHeader 走訪 GIF 的 block 串列，數出動畫張數，並從 Graphic
// Control Extension 的旗標判斷有沒有透明色。GIF 不帶 EXIF。
func parseGIFHeader(data []byte) headerMeta {
	meta := headerMeta{BitDepth: 8, Frames: 1, Orientation: ORIENTATION_NONE}
	if len(data) < 13 {
		return meta
	}

	i := 13
	if packed := data[10]; packed&0x80 != 0 { // global color table
		i += 3 << ((packed & 0x07) + 1)
	}

	frames := 0
	for i < len(data) {
		switch data[i] {
		case 0x21: // extension
			if i+2 >= len(data) {
				i = len(data)
				continue
			}
			// Graphic Control Extension 的第一個 sub-block：長度、旗標、
			// 延遲時間、透明色索引。旗標的 bit0 表示有沒有透明色。
			if data[i+1] == 0xF9 && i+3 < len(data) && data[i+3]&0x01 != 0 {
				meta.HasAlpha = true
			}
			i = skipGIFSubBlocks(data, i+2)
		case 0x2C: // image descriptor
			frames++
			if i+10 > len(data) {
				i = len(data)
				continue
			}
			j := i + 10
			if lct := data[i+9]; lct&0x80 != 0 { // local color table
				j += 3 << ((lct & 0x07) + 1)
			}
			i = skipGIFSubBlocks(data, j+1) // +1 跳過 LZW minimum code size
		default: // 0x3B trailer 或無法辨識的位元組
			i = len(data)
		}
	}

	if frames > 0 {
		meta.Frames = frames
	}
	return meta
}

// skipGIFSubBlocks 跳過一串長度前綴的 sub-block，回傳終止位元組之後的位置。
func skipGIFSubBlocks(data []byte, i int) int {
	for i < len(data) {
		n := int(data[i])
		if n == 0 {
			return i + 1
		}
		i += n + 1
	}
	return len(data)
}

// parseTIFFHeader 直接讀 IFD0。TIFF 檔本身就是一個 TIFF 區塊，
// 因此位元深度與 orientation 都在同一張表裡。
//
// 這裡`不`把整個檔案當成 EXIF 區塊帶進 convert：TIFF 的 IFD 夾雜著指向
// 影像資料的偏移量，整份複製到 JPEG 的 APP1 裡既超長也沒有意義。
func parseTIFFHeader(data []byte) headerMeta {
	meta := headerMeta{BitDepth: 8, Frames: 1, Orientation: ORIENTATION_NONE}

	ifd, ok := readTIFF(data)
	if !ok {
		return meta
	}
	if v, ok := ifd.uint(data, TAG_BITS_PER_SAMP); ok && v > 0 {
		meta.BitDepth = int(v)
	}
	if v, ok := ifd.uint(data, TAG_ORIENTATION); ok && v >= 1 && v <= 8 {
		meta.Orientation = int(v)
	}
	if _, ok := ifd.fields[TAG_EXTRA_SAMPLES]; ok {
		meta.HasAlpha = true
	} else if v, ok := ifd.uint(data, TAG_SAMPLES_PER_PIXEL); ok && v >= 4 {
		meta.HasAlpha = true
	}
	return meta
}
