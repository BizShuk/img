package svc

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"io"

	"github.com/gen2brain/webp"
)

// DELAY_DEFAULT_MS 是畫格沒有指定顯示時間時採用的值。
const DELAY_DEFAULT_MS = 100

// GIF_MIN_DELAY_HUNDREDTHS 之下的延遲一律當成「未指定」。GIF 用 0 或 1
// 表示「盡快播放」，各家瀏覽器對此的一致做法是改用 100ms；照抄原值會
// 得到一段快到看不清楚的動畫。
const GIF_MIN_DELAY_HUNDREDTHS = 2

// Animation 是一組`已合成為完整畫布`的畫格。GIF 的原始畫格常常只是
// 局部矩形，必須先依 disposal 規則疊起來才能單獨拿出來用。
type Animation struct {
	Frames []image.Image
	Delays []int // 每格顯示時間，毫秒
	Loops  int   // 0 表示無限循環
}

// Animatable 回報這個格式的動畫`讀得出來`。APNG 雖然帶得動張數，
// 但標準庫的 png 解碼器只給第一張，因此不在此列。
func (f Format) Animatable() bool {
	return f == FORMAT_GIF || f == FORMAT_WEBP
}

// DecodeAnimation 解出所有畫格。
func DecodeAnimation(data []byte, f Format) (*Animation, error) {
	switch f {
	case FORMAT_WEBP:
		return decodeWebPAnimation(data)
	case FORMAT_GIF:
		return decodeGIFAnimation(data)
	default:
		return nil, fmt.Errorf("cannot read %s as an animation", f)
	}
}

// EncodeAnimation 寫出動畫。所有畫格必須同尺寸。
func EncodeAnimation(w io.Writer, a *Animation, f Format, opt EncodeOptions) error {
	if f != FORMAT_WEBP {
		return fmt.Errorf("%s cannot hold an animation", f)
	}
	if a == nil || len(a.Frames) == 0 {
		return fmt.Errorf("no frames to encode")
	}
	if opt.Quality <= 0 || opt.Quality > 100 {
		opt.Quality = QUALITY_DEFAULT
	}
	if opt.Method <= 0 || opt.Method > 6 {
		opt.Method = WEBP_METHOD_DEFAULT
	}

	return webp.EncodeAll(w, &webp.WEBP{
		Image:     a.Frames,
		Delay:     a.Delays,
		LoopCount: a.Loops,
	}, webp.Options{
		Quality:  opt.Quality,
		Lossless: opt.Lossless,
		Method:   opt.Method,
	})
}

// Transform 對每一格套用同一個轉換，回傳新的 Animation。
// 每格都走同樣的路徑，輸出因此保持同尺寸 —— 這是編碼器的硬性要求。
func (a *Animation) Transform(fn func(image.Image) image.Image) *Animation {
	out := &Animation{
		Frames: make([]image.Image, len(a.Frames)),
		Delays: a.Delays,
		Loops:  a.Loops,
	}
	for i, frame := range a.Frames {
		out.Frames[i] = fn(frame)
	}
	return out
}

// Bounds 回傳第一格的範圍，代表整段動畫的畫布尺寸。
func (a *Animation) Bounds() image.Rectangle {
	if len(a.Frames) == 0 {
		return image.Rectangle{}
	}
	return a.Frames[0].Bounds()
}

// decodeWebPAnimation 交給 libwebp 的 demuxer，它回傳的已經是合成好的畫格。
func decodeWebPAnimation(data []byte) (*Animation, error) {
	w, err := webp.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode webp animation: %w", err)
	}

	anim := &Animation{Frames: w.Image, Loops: w.LoopCount}
	anim.Delays = make([]int, len(w.Image))
	for i := range w.Image {
		if i < len(w.Delay) && w.Delay[i] > 0 {
			anim.Delays[i] = w.Delay[i]
			continue
		}
		anim.Delays[i] = DELAY_DEFAULT_MS
	}
	return anim, nil
}

// decodeGIFAnimation 依 disposal 規則把局部畫格疊成完整畫布。
//
// GIF 的每一格通常只重畫變動的矩形區域，直接拿 gif.DecodeAll 給的
// *image.Paletted 當畫格用，會得到一堆殘缺的碎片。必須維護一張畫布，
// 逐格疊上去並在疊完後依 disposal 決定如何還原。
func decodeGIFAnimation(data []byte) (*Animation, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode gif animation: %w", err)
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("gif has no frames")
	}

	rect := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	if rect.Empty() {
		// 少數 GIF 沒有寫對 logical screen descriptor，改用畫格的聯集。
		for _, frame := range g.Image {
			rect = rect.Union(frame.Bounds())
		}
	}

	canvas := image.NewNRGBA(rect)
	anim := &Animation{Loops: g.LoopCount}

	for i, frame := range g.Image {
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}

		var saved *image.NRGBA
		if disposal == gif.DisposalPrevious {
			saved = image.NewNRGBA(rect)
			copy(saved.Pix, canvas.Pix)
		}

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)

		snapshot := image.NewNRGBA(rect)
		copy(snapshot.Pix, canvas.Pix)
		anim.Frames = append(anim.Frames, snapshot)
		anim.Delays = append(anim.Delays, gifDelayMillis(g.Delay, i))

		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return anim, nil
}

// gifDelayMillis 把 GIF 的百分之一秒換算成毫秒，並補上「盡快播放」的預設值。
func gifDelayMillis(delays []int, i int) int {
	if i >= len(delays) || delays[i] < GIF_MIN_DELAY_HUNDREDTHS {
		return DELAY_DEFAULT_MS
	}
	return delays[i] * 10
}
