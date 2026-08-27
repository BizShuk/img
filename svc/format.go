package svc

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// Format 是本工具認得的影像格式。字面值刻意與 image.DecodeConfig
// 回傳的 registered format name 一致，讓偵測結果可以直接轉型。
type Format string

const (
	FORMAT_JPEG Format = "jpeg"
	FORMAT_PNG  Format = "png"
	FORMAT_WEBP Format = "webp"
	FORMAT_GIF  Format = "gif"
	FORMAT_TIFF Format = "tiff"
	FORMAT_HEIC Format = "heic"
	FORMAT_AVIF Format = "avif"
)

// EncodableFormats 是 convert 的合法目標格式，供旗標說明與錯誤訊息共用。
var EncodableFormats = []Format{FORMAT_JPEG, FORMAT_PNG, FORMAT_WEBP}

// DecodableFormats 是能讀進來的格式。比 EncodableFormats 多出 gif 與 tiff：
// 兩者都有純 Go 的解碼器，但沒有值得引入的編碼器。
var DecodableFormats = []Format{FORMAT_JPEG, FORMAT_PNG, FORMAT_WEBP, FORMAT_GIF, FORMAT_TIFF}

// formatNames 是使用者輸入與格式的對應。副檔名與格式名共用同一張表。
var formatNames = map[string]Format{
	"jpg":  FORMAT_JPEG,
	"jpeg": FORMAT_JPEG,
	"png":  FORMAT_PNG,
	"webp": FORMAT_WEBP,
	"gif":  FORMAT_GIF,
	"tif":  FORMAT_TIFF,
	"tiff": FORMAT_TIFF,
	"heic": FORMAT_HEIC,
	"heif": FORMAT_HEIC,
	"avif": FORMAT_AVIF,
}

// ParseFormat 把格式名或副檔名正規化成 Format，涵蓋所有認得的格式
// （包含不能解碼的 heic / avif）。判斷 mismatch 時用它。
func ParseFormat(s string) (Format, error) {
	key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "."))
	if f, ok := formatNames[key]; ok {
		return f, nil
	}
	return "", fmt.Errorf("unrecognised format %q", s)
}

// ParseTargetFormat 解析 convert 的 -f。只接受能寫出來的格式。
func ParseTargetFormat(s string) (Format, error) {
	f, err := ParseFormat(s)
	if err != nil || !f.Encodable() {
		return "", fmt.Errorf("unsupported target format %q (want one of jpg, png, webp)", s)
	}
	return f, nil
}

// Encodable 回報這個格式能不能作為 convert 的輸出。
func (f Format) Encodable() bool {
	switch f {
	case FORMAT_JPEG, FORMAT_PNG, FORMAT_WEBP:
		return true
	default:
		return false
	}
}

// Decodable 回報這個格式能不能讀進來。heic 與 avif 只認得出名字。
func (f Format) Decodable() bool {
	switch f {
	case FORMAT_JPEG, FORMAT_PNG, FORMAT_WEBP, FORMAT_GIF, FORMAT_TIFF:
		return true
	default:
		return false
	}
}

// Ext 回傳該格式的慣用副檔名（含點）。
func (f Format) Ext() string {
	switch f {
	case FORMAT_JPEG:
		return ".jpg"
	case FORMAT_TIFF:
		return ".tif"
	case "":
		return ""
	default:
		return "." + string(f)
	}
}

// MIME 回傳該格式的 media type。
func (f Format) MIME() string {
	switch f {
	case FORMAT_JPEG:
		return "image/jpeg"
	case FORMAT_PNG:
		return "image/png"
	case FORMAT_WEBP:
		return "image/webp"
	case FORMAT_GIF:
		return "image/gif"
	case FORMAT_TIFF:
		return "image/tiff"
	case FORMAT_HEIC:
		return "image/heic"
	case FORMAT_AVIF:
		return "image/avif"
	default:
		return "application/octet-stream"
	}
}

// FormatFromExt 由副檔名猜測格式，只用於偵測「副檔名與實際內容不符」，
// 絕不用於決定如何解碼。無法辨識時回傳空字串。
func FormatFromExt(path string) Format {
	f, err := ParseFormat(filepath.Ext(path))
	if err != nil {
		return ""
	}
	return f
}

// heicBrands 與 avifBrands 是 ISO BMFF 的品牌識別碼。同一個容器可以宣告
// 多個相容品牌，因此 major brand 與相容清單都要比對。
var (
	heicBrands = map[string]bool{
		"heic": true, "heix": true, "heim": true, "heis": true,
		"hevc": true, "hevx": true, "mif1": true, "msf1": true,
	}
	avifBrands = map[string]bool{"avif": true, "avis": true}
)

// SniffUndecodable 辨識那些 Go 的 image 註冊表認不出來、本工具也解不開的格式。
// 目的只有一個：讓 analyze 能講出「這是 heic」，而不是「不是支援的影像」。
func SniffUndecodable(data []byte) Format {
	if len(data) < 16 || !bytes.Equal(data[4:8], []byte("ftyp")) {
		return ""
	}

	// ftyp box：major brand 之後是 4 位元組的 minor version，再來是相容品牌清單。
	size := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if size < 16 || size > len(data) {
		size = len(data)
	}

	brands := []string{string(data[8:12])}
	for i := 16; i+4 <= size; i += 4 {
		brands = append(brands, string(data[i:i+4]))
	}

	for _, b := range brands {
		if avifBrands[b] {
			return FORMAT_AVIF
		}
	}
	for _, b := range brands {
		if heicBrands[b] {
			return FORMAT_HEIC
		}
	}
	return ""
}
