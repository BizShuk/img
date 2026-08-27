package svc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/gen2brain/webp"

	// 只註冊解碼器：gif 與 tiff 讀得進來，但沒有值得引入的純 Go 編碼器，
	// 因此不在 EncodableFormats 之列。
	_ "golang.org/x/image/tiff"
	_ "image/gif"
)

// QUALITY_DEFAULT 在呼叫端沒給品質時使用，與 libwebp 的預設一致。
const QUALITY_DEFAULT = 75

// WEBP_METHOD_DEFAULT 是 WebP 的壓縮耗時／品質取捨（0 最快、6 最慢最好）。
const WEBP_METHOD_DEFAULT = 4

// EncodeOptions 是輸出端的編碼參數。Lossless 僅對 WebP 有意義，
// Quality 對 JPEG 與 lossy WebP 生效，PNG 一律無損。
type EncodeOptions struct {
	Quality  int
	Lossless bool
	Method   int
}

// Decode 解出影像本體。格式由 magic bytes 判定，與副檔名無關。
// 認得出格式卻解不開時（heic、avif），錯誤訊息要講出是哪一種格式，
// 而不是丟一句無從追查的「格式不明」。
func Decode(data []byte) (image.Image, Format, error) {
	img, name, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if f := SniffUndecodable(data); f != "" {
			return nil, f, fmt.Errorf("%s is recognised but img cannot decode it", f)
		}
		return nil, "", fmt.Errorf("decode failed: %w", err)
	}
	return img, Format(name), nil
}

// Encode 依目標格式寫出影像。轉成 JPEG 時會先把透明區域壓在白底上，
// 否則 image/jpeg 會直接忽略 alpha，讓透明處變成非預期的顏色。
func Encode(w io.Writer, img image.Image, f Format, opt EncodeOptions) error {
	if opt.Quality <= 0 || opt.Quality > 100 {
		opt.Quality = QUALITY_DEFAULT
	}
	if opt.Method <= 0 || opt.Method > 6 {
		opt.Method = WEBP_METHOD_DEFAULT
	}

	switch f {
	case FORMAT_JPEG:
		return jpeg.Encode(w, FlattenAlpha(img, color.White), &jpeg.Options{Quality: opt.Quality})
	case FORMAT_PNG:
		return png.Encode(w, img)
	case FORMAT_WEBP:
		return webp.Encode(w, img, webp.Options{
			Quality:  opt.Quality,
			Lossless: opt.Lossless,
			Method:   opt.Method,
		})
	default:
		return fmt.Errorf("unsupported target format %q", f)
	}
}

// FlattenAlpha 把影像疊在指定底色上。已經不透明的影像原樣回傳。
func FlattenAlpha(src image.Image, bg color.Color) image.Image {
	if opaque, ok := src.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}
