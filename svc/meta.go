package svc

import (
	"bytes"
	"encoding/binary"
)

// headerMeta 是只靠檔頭就能取得、而 image.Config 不提供的資訊。
// 三種格式各有自己的容器結構，這裡逐一走訪 chunk / marker，不做像素解碼。
type headerMeta struct {
	BitDepth    int  // 每個色彩通道的位元數
	HasAlpha    bool // 是否帶透明通道
	Orientation int  // EXIF orientation；ORIENTATION_NONE 表示未帶
	Frames      int  // 動畫張數；靜態圖為 1
	Progressive bool // 僅 JPEG：是否為 progressive 編碼
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
	default:
		return headerMeta{Frames: 1}
	}
}

// parseJPEGHeader 走訪 JPEG 的 marker 串列，取出 SOF 的取樣精度與
// APP1 中的 EXIF orientation，遇到 SOS（影像資料起點）即停止。
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
		case marker == 0xE1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")):
			meta.Orientation = orientationFromTIFF(payload[6:])
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
// tRNS（調色盤透明）與 acTL（APNG 動畫張數）。
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
			meta.Orientation = orientationFromTIFF(payload)
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
	if len(data) < 12 {
		return meta
	}

	frames := 0
	for i := 12; i+8 <= len(data); {
		// RIFF chunk 的順序是 FourCC 在前、長度在後，與 PNG 相反。
		name := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if size < 0 || i+8+size > len(data) {
			break
		}
		payload := data[i+8 : i+8+size]

		switch name {
		case "VP8X":
			if len(payload) >= 1 {
				meta.HasAlpha = payload[0]&0x10 != 0
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
			meta.Orientation = orientationFromTIFF(bytes.TrimPrefix(payload, []byte("Exif\x00\x00")))
		}

		i += 8 + size
		if size%2 == 1 {
			i++ // RIFF chunk 補齊到偶數長度
		}
	}

	if frames > 0 {
		meta.Frames = frames
	}
	return meta
}
