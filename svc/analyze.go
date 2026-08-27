package svc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
)

// Info 是 analyze 對單一檔案的完整報告。所有欄位都只由檔頭推得，
// 過程中不解碼像素資料。
type Info struct {
	Path        string `json:"path"`
	Format      Format `json:"format"`
	MIME        string `json:"mime"`
	ExtFormat   Format `json:"ext_format,omitempty"`
	Mismatch    bool   `json:"mismatch"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Pixels      int    `json:"pixels"`
	Ratio       string `json:"ratio"`
	ColorModel  string `json:"color_model"`
	BitDepth    int    `json:"bit_depth"`
	HasAlpha    bool   `json:"has_alpha"`
	Orientation int    `json:"orientation"`
	Frames      int    `json:"frames"`
	Progressive bool   `json:"progressive,omitempty"`
	Bytes       int64  `json:"bytes"`
}

// Inspect 讀取檔案並回報其影像屬性。格式一律以 magic bytes 判定，
// 副檔名只用來標示 Mismatch。
func Inspect(path string) (Info, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	if stat.IsDir() {
		return Info{}, fmt.Errorf("%s is a directory", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}

	cfg, name, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Info{}, fmt.Errorf("not a supported image (%w)", err)
	}

	format := Format(name)
	meta := parseHeader(format, data)

	info := Info{
		Path:        path,
		Format:      format,
		MIME:        format.MIME(),
		ExtFormat:   FormatFromExt(path),
		Width:       cfg.Width,
		Height:      cfg.Height,
		Pixels:      cfg.Width * cfg.Height,
		Ratio:       aspectRatio(cfg.Width, cfg.Height),
		ColorModel:  colorModelName(cfg.ColorModel),
		BitDepth:    meta.BitDepth,
		HasAlpha:    meta.HasAlpha,
		Orientation: meta.Orientation,
		Frames:      meta.Frames,
		Progressive: meta.Progressive,
		Bytes:       stat.Size(),
	}
	info.Mismatch = info.ExtFormat != "" && info.ExtFormat != format
	return info, nil
}

// InspectAll 逐一檢視多個路徑，回傳成功的報告與失敗的錯誤。
// 單一檔案失敗不中斷其餘檔案。
func InspectAll(paths []string) ([]Info, []error) {
	infos := make([]Info, 0, len(paths))
	var errs []error
	for _, p := range paths {
		info, err := Inspect(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(p), err))
			continue
		}
		infos = append(infos, info)
	}
	return infos, errs
}

// colorModelName 把 image.Config 的 color model 轉成可讀名稱。
func colorModelName(m color.Model) string {
	switch m {
	case color.RGBAModel:
		return "RGBA"
	case color.RGBA64Model:
		return "RGBA64"
	case color.NRGBAModel:
		return "NRGBA"
	case color.NRGBA64Model:
		return "NRGBA64"
	case color.AlphaModel, color.Alpha16Model:
		return "Alpha"
	case color.GrayModel:
		return "Gray"
	case color.Gray16Model:
		return "Gray16"
	case color.CMYKModel:
		return "CMYK"
	case color.YCbCrModel:
		return "YCbCr"
	case color.NYCbCrAModel:
		return "NYCbCrA"
	}
	if _, ok := m.(color.Palette); ok {
		return "Palette"
	}
	return "unknown"
}

// aspectRatio 以最大公因數約分長寬。約不出小整數時（例如 3021x4032）
// 改用倍率表示，避免輸出 1007:1344 這種無助於判讀的數字。
func aspectRatio(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	g := gcd(w, h)
	rw, rh := w/g, h/g
	if rw <= 32 && rh <= 32 {
		return fmt.Sprintf("%d:%d", rw, rh)
	}
	return fmt.Sprintf("%.2f:1", float64(w)/float64(h))
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
