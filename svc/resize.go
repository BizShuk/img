package svc

import (
	"fmt"
	"image"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// Mode 決定同時給定寬與高時，如何把原圖放進這個框。
type Mode string

const (
	MODE_FIT   Mode = "fit"   // 等比縮到完全落在框內，可能留白邊
	MODE_COVER Mode = "cover" // 等比縮到填滿框，超出的部分置中裁掉
	MODE_EXACT Mode = "exact" // 直接拉伸到指定尺寸，允許變形
)

// ResizeModes 是合法的模式清單，供旗標說明與錯誤訊息共用。
var ResizeModes = []Mode{MODE_FIT, MODE_COVER, MODE_EXACT}

// ParseMode 正規化使用者輸入的模式名。
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	switch m {
	case MODE_FIT, MODE_COVER, MODE_EXACT:
		return m, nil
	default:
		return "", fmt.Errorf("unsupported mode %q (want one of fit, cover, exact)", s)
	}
}

// ResizeSpec 描述使用者要求的尺寸調整。Scale 與 Width/Height 互斥。
type ResizeSpec struct {
	Width   int
	Height  int
	Scale   float64
	Mode    Mode
	Upscale bool
}

// Validate 檢查旗標組合是否自洽。
func (s ResizeSpec) Validate() error {
	if s.Scale > 0 && (s.Width > 0 || s.Height > 0) {
		return fmt.Errorf("--scale cannot be combined with --width/--height")
	}
	if s.Scale < 0 {
		return fmt.Errorf("--scale must be positive")
	}
	if s.Width < 0 || s.Height < 0 {
		return fmt.Errorf("--width/--height must be positive")
	}
	if s.Mode == MODE_COVER && (s.Width <= 0 || s.Height <= 0) {
		return fmt.Errorf("--mode cover requires both --width and --height")
	}
	if s.Mode == MODE_EXACT && (s.Width <= 0 || s.Height <= 0) {
		return fmt.Errorf("--mode exact requires both --width and --height")
	}
	return nil
}

// Requested 回報使用者是否確實要求了尺寸調整。
func (s ResizeSpec) Requested() bool {
	return s.Scale > 0 || s.Width > 0 || s.Height > 0
}

// Resize 依 spec 縮放影像。未要求調整、或在不放大的前提下算出的目標
// 不小於原圖時，原樣回傳。
func Resize(src image.Image, spec ResizeSpec) image.Image {
	if !spec.Requested() {
		return src
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}

	if spec.Mode == MODE_EXACT {
		if !spec.Upscale && spec.Width >= w && spec.Height >= h {
			return src
		}
		return scaleTo(src, spec.Width, spec.Height)
	}

	factor := scaleFactor(w, h, spec)
	if !spec.Upscale && factor >= 1 {
		factor = 1
	}

	dstW := maxInt(1, int(math.Round(float64(w)*factor)))
	dstH := maxInt(1, int(math.Round(float64(h)*factor)))

	scaled := src
	if dstW != w || dstH != h {
		scaled = scaleTo(src, dstW, dstH)
	}

	if spec.Mode == MODE_COVER {
		return centerCrop(scaled, minInt(spec.Width, dstW), minInt(spec.Height, dstH))
	}
	return scaled
}

// scaleFactor 算出等比縮放倍率。只給一邊時以該邊為準；兩邊都給時，
// fit 取較小的倍率（縮進框內），cover 取較大的倍率（填滿框）。
func scaleFactor(w, h int, spec ResizeSpec) float64 {
	if spec.Scale > 0 {
		return spec.Scale
	}

	byWidth := math.Inf(1)
	if spec.Width > 0 {
		byWidth = float64(spec.Width) / float64(w)
	}
	byHeight := math.Inf(1)
	if spec.Height > 0 {
		byHeight = float64(spec.Height) / float64(h)
	}

	switch {
	case math.IsInf(byWidth, 1):
		return byHeight
	case math.IsInf(byHeight, 1):
		return byWidth
	case spec.Mode == MODE_COVER:
		return math.Max(byWidth, byHeight)
	default:
		return math.Min(byWidth, byHeight)
	}
}

// scaleTo 用 Catmull-Rom 重取樣到指定尺寸。
func scaleTo(src image.Image, w, h int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// centerCrop 從中心裁出指定尺寸。
func centerCrop(src image.Image, w, h int) image.Image {
	b := src.Bounds()
	if w >= b.Dx() && h >= b.Dy() {
		return src
	}
	x0 := b.Min.X + (b.Dx()-w)/2
	y0 := b.Min.Y + (b.Dy()-h)/2
	rect := image.Rect(x0, y0, x0+w, y0+h)

	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.Draw(dst, dst.Bounds(), src, rect.Min, xdraw.Src)
	return dst
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
