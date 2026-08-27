package svc

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

// 測試用畫布與調色盤。index 0 必須是全透明，Go 的 gif 編碼器才會在
// Graphic Control Extension 裡寫出透明色索引。
const (
	CANVAS_W = 8
	CANVAS_H = 4
)

var (
	testPalette = color.Palette{
		color.NRGBA{0, 0, 0, 0},     // 0 透明
		color.NRGBA{0, 0, 255, 255}, // 1 藍
		color.NRGBA{255, 0, 0, 255}, // 2 紅
	}
	BLUE  = color.NRGBA{0, 0, 255, 255}
	RED   = color.NRGBA{255, 0, 0, 255}
	CLEAR = color.NRGBA{0, 0, 0, 0}
)

// partialFrameGIF 造一段刻意使用`局部畫格`的 GIF：第一格鋪滿藍色，
// 之後兩格各只重畫一個小紅方塊。直接拿 gif.DecodeAll 的畫格當結果用，
// 會得到兩張 2x2 的碎片而不是完整畫面 —— 這正是要測的陷阱。
func partialFrameGIF(t *testing.T, disposal byte) []byte {
	t.Helper()

	fill := func(r image.Rectangle, idx uint8) *image.Paletted {
		p := image.NewPaletted(r, testPalette)
		for i := range p.Pix {
			p.Pix[i] = idx
		}
		return p
	}

	g := &gif.GIF{
		Image: []*image.Paletted{
			fill(image.Rect(0, 0, CANVAS_W, CANVAS_H), 1), // 全藍
			fill(image.Rect(0, 0, 2, 2), 2),               // 左上紅方塊
			fill(image.Rect(4, 0, 6, 2), 2),               // 右上紅方塊
		},
		Delay:     []int{10, 10, 10},
		Disposal:  []byte{gif.DisposalNone, disposal, gif.DisposalNone},
		LoopCount: 0,
		Config:    image.Config{ColorModel: testPalette, Width: CANVAS_W, Height: CANVAS_H},
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func atNRGBA(t *testing.T, img image.Image, x, y int) color.NRGBA {
	t.Helper()
	r, g, b, a := img.At(x, y).RGBA()
	if a == 0 {
		return CLEAR
	}
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

// 每一格都必須是`完整畫布`，而不是原始的局部矩形。
func TestDecodeGIFAnimationCompositesPartialFrames(t *testing.T) {
	anim, err := DecodeAnimation(partialFrameGIF(t, gif.DisposalNone), FORMAT_GIF)
	if err != nil {
		t.Fatalf("DecodeAnimation: %v", err)
	}
	if len(anim.Frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(anim.Frames))
	}

	for i, frame := range anim.Frames {
		b := frame.Bounds()
		if b.Dx() != CANVAS_W || b.Dy() != CANVAS_H {
			t.Errorf("frame %d is %dx%d, want the full %dx%d canvas", i, b.Dx(), b.Dy(), CANVAS_W, CANVAS_H)
		}
	}

	// 第一格全藍。
	if got := atNRGBA(t, anim.Frames[0], 0, 0); got != BLUE {
		t.Errorf("frame 0 at (0,0) = %v, want blue", got)
	}
	// 第二格：左上被紅方塊蓋掉，其餘仍是第一格留下的藍。
	if got := atNRGBA(t, anim.Frames[1], 0, 0); got != RED {
		t.Errorf("frame 1 at (0,0) = %v, want red", got)
	}
	if got := atNRGBA(t, anim.Frames[1], 7, 3); got != BLUE {
		t.Errorf("frame 1 at (7,3) = %v, want the blue carried over from frame 0", got)
	}
}

// disposal 決定畫完一格之後畫布要不要還原，三種規則的結果完全不同。
func TestDecodeGIFAnimationHonoursDisposal(t *testing.T) {
	cases := []struct {
		name     string
		disposal byte
		wantAt00 color.NRGBA // 第三格的 (0,0)：第二格的紅方塊留下什麼
	}{
		{"none keeps the previous frame", gif.DisposalNone, RED},
		{"background clears the region", gif.DisposalBackground, CLEAR},
		{"previous restores the canvas", gif.DisposalPrevious, BLUE},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anim, err := DecodeAnimation(partialFrameGIF(t, tc.disposal), FORMAT_GIF)
			if err != nil {
				t.Fatalf("DecodeAnimation: %v", err)
			}

			if got := atNRGBA(t, anim.Frames[2], 0, 0); got != tc.wantAt00 {
				t.Errorf("frame 2 at (0,0) = %v, want %v", got, tc.wantAt00)
			}
			// 第三格自己畫的紅方塊在哪種 disposal 下都必須在。
			if got := atNRGBA(t, anim.Frames[2], 4, 0); got != RED {
				t.Errorf("frame 2 at (4,0) = %v, want red", got)
			}
		})
	}
}

// GIF 的延遲以百分之一秒計，WebP 以毫秒計。換算錯了動畫就會快十倍。
func TestGIFDelayConversion(t *testing.T) {
	cases := []struct {
		name   string
		delays []int
		index  int
		want   int
	}{
		{"hundredths become milliseconds", []int{12}, 0, 120},
		{"zero means as fast as possible", []int{0}, 0, DELAY_DEFAULT_MS},
		{"one is also treated as unset", []int{1}, 0, DELAY_DEFAULT_MS},
		{"missing entry falls back", []int{}, 0, DELAY_DEFAULT_MS},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gifDelayMillis(tc.delays, tc.index); got != tc.want {
				t.Errorf("gifDelayMillis = %d, want %d", got, tc.want)
			}
		})
	}
}

// 動畫寫成 WebP 再讀回來，張數、尺寸與延遲都要一致。
func TestAnimationRoundTripsThroughWebP(t *testing.T) {
	anim, err := DecodeAnimation(partialFrameGIF(t, gif.DisposalNone), FORMAT_GIF)
	if err != nil {
		t.Fatalf("DecodeAnimation: %v", err)
	}

	var buf bytes.Buffer
	if err := EncodeAnimation(&buf, anim, FORMAT_WEBP, EncodeOptions{Lossless: true}); err != nil {
		t.Fatalf("EncodeAnimation: %v", err)
	}

	back, err := DecodeAnimation(buf.Bytes(), FORMAT_WEBP)
	if err != nil {
		t.Fatalf("DecodeAnimation(webp): %v", err)
	}
	if len(back.Frames) != len(anim.Frames) {
		t.Fatalf("got %d frames back, want %d", len(back.Frames), len(anim.Frames))
	}
	for i := range back.Frames {
		if back.Delays[i] != anim.Delays[i] {
			t.Errorf("frame %d delay = %d, want %d", i, back.Delays[i], anim.Delays[i])
		}
	}

	// 無損編碼必須逐格逐點相同 —— 這同時證明合成邏輯本身是精確的。
	for i := range anim.Frames {
		for _, p := range []image.Point{{0, 0}, {4, 0}, {7, 3}} {
			want := atNRGBA(t, anim.Frames[i], p.X, p.Y)
			got := atNRGBA(t, back.Frames[i], p.X, p.Y)
			if got != want {
				t.Errorf("frame %d at %v = %v, want %v", i, p, got, want)
			}
		}
	}
}

// Transform 對每格套用同一個轉換，輸出必須保持同尺寸 —— 編碼器要求如此。
func TestAnimationTransformKeepsFramesUniform(t *testing.T) {
	anim, err := DecodeAnimation(partialFrameGIF(t, gif.DisposalNone), FORMAT_GIF)
	if err != nil {
		t.Fatal(err)
	}

	scaled := anim.Transform(func(f image.Image) image.Image {
		return Resize(f, ResizeSpec{Width: 4, Mode: MODE_FIT})
	})

	if len(scaled.Frames) != len(anim.Frames) {
		t.Fatalf("got %d frames, want %d", len(scaled.Frames), len(anim.Frames))
	}
	for i, f := range scaled.Frames {
		if b := f.Bounds(); b.Dx() != 4 || b.Dy() != 2 {
			t.Errorf("frame %d is %dx%d, want 4x2", i, b.Dx(), b.Dy())
		}
	}
	if scaled.Loops != anim.Loops {
		t.Errorf("loop count changed: %d -> %d", anim.Loops, scaled.Loops)
	}
}

func TestFormatAnimatable(t *testing.T) {
	cases := map[Format]bool{
		FORMAT_GIF:  true,
		FORMAT_WEBP: true,
		FORMAT_PNG:  false, // APNG 的張數讀得出來，畫格序列讀不出來
		FORMAT_JPEG: false,
		FORMAT_TIFF: false,
		FORMAT_HEIC: false,
	}
	for f, want := range cases {
		if got := f.Animatable(); got != want {
			t.Errorf("%s.Animatable() = %v, want %v", f, got, want)
		}
	}
}

func TestEncodeAnimationRejectsStillFormats(t *testing.T) {
	anim := &Animation{Frames: []image.Image{makeImage(8, 8, true)}, Delays: []int{100}}
	for _, f := range []Format{FORMAT_JPEG, FORMAT_PNG, FORMAT_GIF} {
		if err := EncodeAnimation(&bytes.Buffer{}, anim, f, EncodeOptions{}); err == nil {
			t.Errorf("EncodeAnimation should reject %s", f)
		}
	}
}

func TestDecodeAnimationRejectsUnreadableFormats(t *testing.T) {
	data := encodeTo(t, makeImage(8, 8, true), FORMAT_PNG, EncodeOptions{})
	if _, err := DecodeAnimation(data, FORMAT_PNG); err == nil {
		t.Error("DecodeAnimation should reject png")
	}
}
