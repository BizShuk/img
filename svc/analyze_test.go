package svc

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp 把位元組寫成暫存檔並回傳路徑。
func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestInspectReportsDimensionsAndFormat(t *testing.T) {
	cases := []struct {
		format Format
		name   string
	}{
		{FORMAT_PNG, "a.png"},
		{FORMAT_JPEG, "a.jpg"},
		{FORMAT_WEBP, "a.webp"},
	}

	for _, tc := range cases {
		t.Run(string(tc.format), func(t *testing.T) {
			data := encodeTo(t, makeImage(64, 48, true), tc.format, EncodeOptions{Quality: 80})
			info, err := Inspect(writeTemp(t, tc.name, data))
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}

			if info.Format != tc.format {
				t.Errorf("format = %q, want %q", info.Format, tc.format)
			}
			if info.Width != 64 || info.Height != 48 {
				t.Errorf("size = %dx%d, want 64x48", info.Width, info.Height)
			}
			if info.Pixels != 64*48 {
				t.Errorf("pixels = %d, want %d", info.Pixels, 64*48)
			}
			if info.Ratio != "4:3" {
				t.Errorf("ratio = %q, want 4:3", info.Ratio)
			}
			if info.Mismatch {
				t.Error("mismatch should be false when the extension matches")
			}
			if info.Bytes != int64(len(data)) {
				t.Errorf("bytes = %d, want %d", info.Bytes, len(data))
			}
		})
	}
}

// 透明通道要能在三種容器裡都被認出來：PNG 看色彩型別，
// WebP 看 VP8X 旗標或 VP8L 的 alpha 位元，JPEG 永遠沒有。
func TestInspectDetectsAlpha(t *testing.T) {
	cases := []struct {
		format Format
		opt    EncodeOptions
		want   bool
	}{
		{FORMAT_PNG, EncodeOptions{}, true},
		{FORMAT_WEBP, EncodeOptions{Quality: 80}, true},
		{FORMAT_WEBP, EncodeOptions{Lossless: true}, true},
		{FORMAT_JPEG, EncodeOptions{Quality: 80}, false},
	}

	for _, tc := range cases {
		data := encodeTo(t, makeImage(64, 48, false), tc.format, tc.opt)
		info, err := Inspect(writeTemp(t, "a"+tc.format.Ext(), data))
		if err != nil {
			t.Fatalf("Inspect %s: %v", tc.format, err)
		}
		if info.HasAlpha != tc.want {
			t.Errorf("%s lossless=%v has_alpha = %v, want %v", tc.format, tc.opt.Lossless, info.HasAlpha, tc.want)
		}
	}
}

func TestInspectFlagsExtensionMismatch(t *testing.T) {
	data := encodeTo(t, makeImage(10, 10, true), FORMAT_WEBP, EncodeOptions{Quality: 80})

	info, err := Inspect(writeTemp(t, "liar.png", data))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !info.Mismatch {
		t.Error("mismatch should be true when a webp is named .png")
	}
	if info.Format != FORMAT_WEBP {
		t.Errorf("format = %q, want webp", info.Format)
	}
}

func TestInspectReportsExifOrientation(t *testing.T) {
	data := jpegWithOrientation(t, makeImage(32, 16, true), 6)

	info, err := Inspect(writeTemp(t, "rotated.jpg", data))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.Orientation != 6 {
		t.Errorf("orientation = %d, want 6", info.Orientation)
	}
}

func TestInspectRejectsNonImages(t *testing.T) {
	if _, err := Inspect(writeTemp(t, "notes.txt", []byte("hello"))); err == nil {
		t.Error("Inspect should fail on a non-image file")
	}
}

func TestInspectAllKeepsGoingAfterAFailure(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.png")
	if err := os.WriteFile(good, encodeTo(t, makeImage(8, 8, true), FORMAT_PNG, EncodeOptions{}), 0o644); err != nil {
		t.Fatal(err)
	}

	infos, errs := InspectAll([]string{filepath.Join(dir, "missing.png"), good})
	if len(infos) != 1 {
		t.Errorf("got %d infos, want 1", len(infos))
	}
	if len(errs) != 1 {
		t.Errorf("got %d errors, want 1", len(errs))
	}
}

func TestAspectRatio(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{1600, 1200, "4:3"},
		{1920, 1080, "16:9"},
		{500, 500, "1:1"},
		{3021, 4032, "0.75:1"},
		{0, 10, ""},
	}
	for _, tc := range cases {
		if got := aspectRatio(tc.w, tc.h); got != tc.want {
			t.Errorf("aspectRatio(%d,%d) = %q, want %q", tc.w, tc.h, got, tc.want)
		}
	}
}
