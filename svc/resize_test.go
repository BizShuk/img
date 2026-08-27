package svc

import "testing"

func TestResizeDimensions(t *testing.T) {
	cases := []struct {
		name       string
		srcW, srcH int
		spec       ResizeSpec
		wantW      int
		wantH      int
	}{
		{"width only keeps ratio", 1600, 1200, ResizeSpec{Width: 800, Mode: MODE_FIT}, 800, 600},
		{"height only keeps ratio", 1600, 1200, ResizeSpec{Height: 300, Mode: MODE_FIT}, 400, 300},
		{"fit shrinks into the box", 1600, 1200, ResizeSpec{Width: 800, Height: 800, Mode: MODE_FIT}, 800, 600},
		{"cover fills the box exactly", 1600, 1200, ResizeSpec{Width: 400, Height: 400, Mode: MODE_COVER}, 400, 400},
		{"exact stretches", 1600, 1200, ResizeSpec{Width: 300, Height: 100, Mode: MODE_EXACT}, 300, 100},
		{"scale halves both sides", 800, 600, ResizeSpec{Scale: 0.5, Mode: MODE_FIT}, 400, 300},
		{"no spec is a no-op", 800, 600, ResizeSpec{Mode: MODE_FIT}, 800, 600},
		{"never upscales by default", 100, 100, ResizeSpec{Width: 500, Mode: MODE_FIT}, 100, 100},
		{"upscales when asked", 100, 100, ResizeSpec{Width: 500, Mode: MODE_FIT, Upscale: true}, 500, 500},
		{"exact never upscales by default", 100, 100, ResizeSpec{Width: 500, Height: 500, Mode: MODE_EXACT}, 100, 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resize(makeImage(tc.srcW, tc.srcH, true), tc.spec).Bounds()
			if got.Dx() != tc.wantW || got.Dy() != tc.wantH {
				t.Errorf("got %dx%d, want %dx%d", got.Dx(), got.Dy(), tc.wantW, tc.wantH)
			}
		})
	}
}

// cover 在原圖比目標框小、又不允許放大時，只能裁到原圖能給的大小，
// 不該憑空放大或回傳空白邊。
func TestResizeCoverClampsWhenSourceIsSmaller(t *testing.T) {
	got := Resize(makeImage(200, 100, true), ResizeSpec{Width: 400, Height: 400, Mode: MODE_COVER}).Bounds()
	if got.Dx() != 200 || got.Dy() != 100 {
		t.Errorf("got %dx%d, want 200x100", got.Dx(), got.Dy())
	}
}

func TestResizeSpecValidate(t *testing.T) {
	cases := []struct {
		name    string
		spec    ResizeSpec
		wantErr bool
	}{
		{"scale with width", ResizeSpec{Scale: 0.5, Width: 100, Mode: MODE_FIT}, true},
		{"negative scale", ResizeSpec{Scale: -1, Mode: MODE_FIT}, true},
		{"cover needs both sides", ResizeSpec{Width: 100, Mode: MODE_COVER}, true},
		{"exact needs both sides", ResizeSpec{Height: 100, Mode: MODE_EXACT}, true},
		{"fit with one side is fine", ResizeSpec{Width: 100, Mode: MODE_FIT}, false},
		{"scale alone is fine", ResizeSpec{Scale: 2, Mode: MODE_FIT}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.spec.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseMode(t *testing.T) {
	for _, in := range []string{"fit", "COVER", " exact "} {
		if _, err := ParseMode(in); err != nil {
			t.Errorf("ParseMode(%q) unexpected error: %v", in, err)
		}
	}
	if _, err := ParseMode("squish"); err == nil {
		t.Error("ParseMode(squish) should fail")
	}
}
