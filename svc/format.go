package svc

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Format 是本工具支援的影像編碼格式。字面值刻意與 image.DecodeConfig
// 回傳的 registered format name 一致，讓偵測結果可以直接轉型。
type Format string

const (
	FORMAT_JPEG Format = "jpeg"
	FORMAT_PNG  Format = "png"
	FORMAT_WEBP Format = "webp"
)

// SupportedFormats 是 convert 的合法目標格式，供旗標說明與錯誤訊息共用。
var SupportedFormats = []Format{FORMAT_JPEG, FORMAT_PNG, FORMAT_WEBP}

// ParseFormat 將使用者輸入的格式名正規化。jpg 與 jpeg 視為同一格式。
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), ".")) {
	case "jpg", "jpeg":
		return FORMAT_JPEG, nil
	case "png":
		return FORMAT_PNG, nil
	case "webp":
		return FORMAT_WEBP, nil
	default:
		return "", fmt.Errorf("unsupported format %q (want one of jpg, png, webp)", s)
	}
}

// Ext 回傳該格式的慣用副檔名（含點）。
func (f Format) Ext() string {
	switch f {
	case FORMAT_JPEG:
		return ".jpg"
	case FORMAT_PNG:
		return ".png"
	case FORMAT_WEBP:
		return ".webp"
	default:
		return ""
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
